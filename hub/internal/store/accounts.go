package store

import (
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var handlePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,19}$`)

type loginAttempts struct {
	count int
	until time.Time
}

// AllowPasswordAttempt shares a bounded per-IP budget between HTTP and SSH.
func (s *Store) AllowPasswordAttempt(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	if s.attempts == nil {
		s.attempts = make(map[string]loginAttempts)
	}
	now := time.Now()
	for ip, a := range s.attempts {
		if now.After(a.until) {
			delete(s.attempts, ip)
		}
	}
	a := s.attempts[host]
	if a.count >= 10 || (a.count == 0 && len(s.attempts) >= 10000) {
		return false
	}
	if a.count == 0 {
		a.until = now.Add(time.Minute)
	}
	a.count++
	s.attempts[host] = a
	return true
}

// Register reserves an exact, immutable username. It never renames on collision.
func (s *Store) Register(handle, password, fp, publicKey string) (Account, error) {
	if !handlePattern.MatchString(handle) {
		return Account{}, fmt.Errorf("用户名须为 3–20 位小写字母、数字、下划线或短横线，以字母开头")
	}
	if password == "" && fp == "" {
		return Account{}, fmt.Errorf("请提供 SSH 公钥或设置密码")
	}
	digest, err := passwordHash(password)
	if err != nil {
		return Account{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	a := Account{ID: randomID(), Handle: handle, Display: handle, CreatedAt: time.Now()}
	if _, err = tx.Exec(`INSERT INTO accounts(id,handle,display,created_at) VALUES(?,?,?,?)`, a.ID, a.Handle, a.Display, a.CreatedAt.UnixMilli()); err != nil {
		return Account{}, accountWriteError(err)
	}
	if fp != "" {
		if _, err = tx.Exec(`INSERT INTO identities(fingerprint,account_id,public_key,created_at,last_used_at) VALUES(?,?,?,?,?)`, fp, a.ID, publicKey, a.CreatedAt.UnixMilli(), a.CreatedAt.UnixMilli()); err != nil {
			return Account{}, accountWriteError(err)
		}
	}
	if digest != "" {
		if _, err = tx.Exec(`INSERT INTO passwords(account_id,digest) VALUES(?,?)`, a.ID, digest); err != nil {
			return Account{}, err
		}
	}
	return a, tx.Commit()
}

func passwordHash(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	if len(password) < 10 || len(password) > 72 {
		return "", fmt.Errorf("密码长度须为 10–72 字节")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func (s *Store) ByHandle(handle string) (Account, bool, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM accounts WHERE handle=?`, handle).Scan(&id)
	if err == sql.ErrNoRows {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	return s.ByID(id)
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-password-placeholder"), bcrypt.DefaultCost)

// ConfirmAccount only accepts a challenge addressed to the authenticated account.
func (s *Store) ConfirmAccount(code, fp, handle string) (string, string, error) {
	a, ok, err := s.ByFingerprint(fp)
	if err != nil || !ok || a.Handle != handle {
		return "", "", fmt.Errorf("账号错误或密钥不匹配")
	}
	var target string
	if err = s.db.QueryRow(`SELECT account_id FROM challenges WHERE code_hash=?`, hash(code)).Scan(&target); err != nil || target != a.ID {
		return "", "", fmt.Errorf("验证码不属于此账号或已过期")
	}
	return s.Confirm(code, fp)
}

func (s *Store) AuthenticatePassword(handle, password string) (Account, bool) {
	a, ok, err := s.ByHandle(handle)
	var digest string
	if err == nil && ok {
		_ = s.db.QueryRow(`SELECT digest FROM passwords WHERE account_id=?`, a.ID).Scan(&digest)
	}
	if digest == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return Account{}, false
	}
	return a, bcrypt.CompareHashAndPassword([]byte(digest), []byte(password)) == nil
}

func (s *Store) SetPassword(id, password string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("请输入新密码")
	}
	digest, err := passwordHash(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO passwords(account_id,digest) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET digest=excluded.digest`, id, digest)
	return err
}
