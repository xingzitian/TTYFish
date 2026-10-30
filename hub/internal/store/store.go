// Package store 是平台的数据层：账号、凭证、会话、登录挑战。
//
// 现在用 SQLite（纯 Go 驱动，不用 cgo，静态编译）。接口设计成"换 Postgres
// 只改实现"的样子——以后要把账号服务分到另一台机器时不用动调用方。
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store 是数据库句柄。
type Store struct {
	db        *sql.DB
	attemptMu sync.Mutex
	attempts  map[string]loginAttempts
}

// Account 是一个账号。账号 ID 和公钥是分开的：换钥匙不该丢账号。
type Account struct {
	ID        string
	Handle    string
	Display   string
	CreatedAt time.Time
}

// Identity 是一把挂在账号下的公钥。
type Identity struct {
	Name        string
	Fingerprint string
	AccountID   string
	PublicKey   string
	Kind        string
	CreatedAt   time.Time
	LastUsed    time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS passwords (
  account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  digest TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS accounts (
  id         TEXT PRIMARY KEY,
  handle     TEXT NOT NULL UNIQUE,
  display    TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS identities (
  fingerprint  TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  public_key   TEXT NOT NULL,
  name         TEXT NOT NULL DEFAULT '',
  kind         TEXT NOT NULL DEFAULT 'ssh',
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS identities_account ON identities(account_id);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  note       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS sessions_account ON sessions(account_id);

CREATE TABLE IF NOT EXISTS challenges (
  code_hash    TEXT PRIMARY KEY,
  poll_hash    TEXT NOT NULL UNIQUE,
  purpose      TEXT NOT NULL,
  account_id   TEXT NOT NULL DEFAULT '',
  state        TEXT NOT NULL,
  confirmed_fp TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
`

// Open 打开（必要时创建）数据库。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("建目录: %w", err)
		}
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 单写者，串行省心
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表: %w", err)
	}
	if err := migrateKeyNames(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("升级公钥元数据: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- 账号与公钥 ----

// EnsureAccount 按指纹找账号，找不到就建一个（TOFU：第一次见就是注册）。
//
// suggest 是名字建议（一般是 SSH 用户名），被占用会自动加后缀。
func (s *Store) EnsureAccount(fp, publicKey, suggest string) (Account, error) {
	if a, ok, err := s.ByFingerprint(fp); err != nil || ok {
		if err == nil {
			_ = s.touch(fp)
		}
		return a, err
	}

	now := time.Now()
	id := randomID()
	handle, err := s.uniqueHandle(suggest)
	if err != nil {
		return Account{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO accounts(id, handle, display, created_at) VALUES(?,?,?,?)`,
		id, handle, handle, now.UnixMilli(),
	); err != nil {
		return Account{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO identities(fingerprint, account_id, public_key, kind, created_at, last_used_at)
		 VALUES(?,?,?,?,?,?)`,
		fp, id, publicKey, "ssh", now.UnixMilli(), now.UnixMilli(),
	); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return Account{ID: id, Handle: handle, Display: handle, CreatedAt: now}, nil
}

// ByFingerprint 按公钥指纹找账号。
func (s *Store) ByFingerprint(fp string) (Account, bool, error) {
	var a Account
	var created int64
	err := s.db.QueryRow(
		`SELECT a.id, a.handle, a.display, a.created_at
		   FROM accounts a JOIN identities i ON i.account_id = a.id
		  WHERE i.fingerprint = ?`, fp,
	).Scan(&a.ID, &a.Handle, &a.Display, &created)
	if err == sql.ErrNoRows {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	a.CreatedAt = time.UnixMilli(created)
	return a, true, nil
}

// ByID 按账号 ID 取账号。
func (s *Store) ByID(id string) (Account, bool, error) {
	var a Account
	var created int64
	err := s.db.QueryRow(
		`SELECT id, handle, display, created_at FROM accounts WHERE id = ?`, id,
	).Scan(&a.ID, &a.Handle, &a.Display, &created)
	if err == sql.ErrNoRows {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	a.CreatedAt = time.UnixMilli(created)
	return a, true, nil
}

// Identities 列出账号下的所有公钥。
func (s *Store) Identities(accountID string) ([]Identity, error) {
	rows, err := s.db.Query(
		`SELECT fingerprint, account_id, public_key, kind, created_at, last_used_at, name
		   FROM identities WHERE account_id = ? ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Identity
	for rows.Next() {
		var it Identity
		var created, used int64
		if err := rows.Scan(&it.Fingerprint, &it.AccountID, &it.PublicKey, &it.Kind, &created, &used, &it.Name); err != nil {
			return nil, err
		}
		it.CreatedAt = time.UnixMilli(created)
		if used > 0 {
			it.LastUsed = time.UnixMilli(used)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Link 把一把公钥挂到已有账号上。
func (s *Store) Link(accountID, fp, publicKey string) error {
	return s.LinkNamed(accountID, fp, publicKey, nil)
}

// Unlink 摘掉一把公钥。不允许摘掉最后一把——那样人就再也进不来了。
func (s *Store) Unlink(accountID, fp string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM identities WHERE account_id = ?`, accountID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return fmt.Errorf("这是最后一把公钥，删了就进不来了")
	}
	_, err := s.db.Exec(`DELETE FROM identities WHERE account_id = ? AND fingerprint = ?`, accountID, fp)
	return err
}

func (s *Store) touch(fp string) error {
	_, err := s.db.Exec(`UPDATE identities SET last_used_at = ? WHERE fingerprint = ?`, time.Now().UnixMilli(), fp)
	return err
}

func (s *Store) uniqueHandle(suggest string) (string, error) {
	base := sanitizeHandle(suggest)
	for i := 0; i < 100; i++ {
		h := base
		if i > 0 {
			h = fmt.Sprintf("%s%d", base, i+1)
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE handle = ?`, h).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return h, nil
		}
	}
	return "", fmt.Errorf("名字被占用太多了，换个用户名试试")
}

func sanitizeHandle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_")
	if len(out) < 3 {
		out = "player"
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// ---- 会话 ----

// NewSession 发一个网页会话，返回明文 token（库里只存哈希）。
func (s *Store) NewSession(accountID, note string, ttl time.Duration) (string, error) {
	token := randomToken()
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO sessions(token_hash, account_id, created_at, expires_at, note) VALUES(?,?,?,?,?)`,
		hash(token), accountID, now.UnixMilli(), now.Add(ttl).UnixMilli(), note,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionAccount 用会话 token 换账号。
func (s *Store) SessionAccount(token string) (Account, bool, error) {
	var accountID string
	var expires int64
	err := s.db.QueryRow(
		`SELECT account_id, expires_at FROM sessions WHERE token_hash = ?`, hash(token),
	).Scan(&accountID, &expires)
	if err == sql.ErrNoRows {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	if time.Now().UnixMilli() > expires {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash(token))
		return Account{}, false, nil
	}
	return s.ByID(accountID)
}

// DropSession 注销一个会话。
func (s *Store) DropSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash(token))
	return err
}

// ---- 登录挑战 ----

// Challenge 是一次网页登录/绑定公钥的挑战。
type Challenge struct {
	Code      string // 给终端敲的（只返回一次）
	Poll      string // 浏览器轮询用的（只返回一次）
	Purpose   string // login | link
	AccountID string // link 时的目标账号
}

// NewChallenge 生成一次挑战。两个值分开：终端只拿到 code，
// 浏览器只拿到 poll，谁也拿不到对方的。
func (s *Store) NewChallenge(purpose, accountID string, ttl time.Duration) (Challenge, error) {
	c := Challenge{Code: shortCode(), Poll: randomToken(), Purpose: purpose, AccountID: accountID}
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO challenges(code_hash, poll_hash, purpose, account_id, state, created_at, expires_at)
		 VALUES(?,?,?,?,?,?,?)`,
		hash(c.Code), hash(c.Poll), purpose, accountID, "pending", now.UnixMilli(), now.Add(ttl).UnixMilli(),
	)
	if err != nil {
		return Challenge{}, err
	}
	return c, nil
}

// Confirm 用终端里的 code 确认挑战：记录是谁的指纹确认的。
// 返回 purpose 和 link 时的目标账号。
func (s *Store) Confirm(code, fp string) (purpose, accountID string, err error) {
	var expires int64
	var state string
	err = s.db.QueryRow(
		`SELECT purpose, account_id, state, expires_at FROM challenges WHERE code_hash = ?`, hash(code),
	).Scan(&purpose, &accountID, &state, &expires)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("这个码不对，或者已经用过了")
	}
	if err != nil {
		return "", "", err
	}
	if state != "pending" {
		return "", "", fmt.Errorf("这个码已经用过了")
	}
	if time.Now().UnixMilli() > expires {
		return "", "", fmt.Errorf("这个码过期了，回网页刷新一下")
	}
	result, err := s.db.Exec(
		`UPDATE challenges SET state = 'confirmed', confirmed_fp = ? WHERE code_hash = ? AND state = 'pending' AND expires_at >= ?`,
		fp, hash(code), time.Now().UnixMilli(),
	)
	if err == nil {
		n, e := result.RowsAffected()
		if e != nil {
			err = e
		} else if n != 1 {
			err = fmt.Errorf("验证码已使用或过期")
		}
	}
	return purpose, accountID, err
}

// Poll 查挑战状态。返回的 accountID 在 confirmed 之后才有意义。
func (s *Store) Poll(poll string) (state, accountID, fp string, ok bool, err error) {
	var expires int64
	err = s.db.QueryRow(
		`SELECT state, account_id, confirmed_fp, expires_at FROM challenges WHERE poll_hash = ? AND purpose IN ('login','link')`, hash(poll),
	).Scan(&state, &accountID, &fp, &expires)
	if err == sql.ErrNoRows {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	if time.Now().UnixMilli() > expires {
		_, _ = s.db.Exec(`DELETE FROM challenges WHERE poll_hash = ?`, hash(poll))
		return "", "", "", false, nil
	}
	return state, accountID, fp, true, nil
}

// Consume 用完即删。
func (s *Store) Consume(poll string) error {
	_, err := s.db.Exec(`DELETE FROM challenges WHERE poll_hash = ?`, hash(poll))
	return err
}

// Sweep 清掉过期的挑战和会话。
func (s *Store) Sweep() {
	now := time.Now().UnixMilli()
	_, _ = s.db.Exec(`DELETE FROM challenges WHERE expires_at < ?`, now)
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
}

// ---- 小工具 ----

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// shortCode 是给终端敲的短码：去掉了容易看错的字符。
func shortCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	// 分成 4-4，方便念给别人听
	return string(b[:4]) + "-" + string(b[4:8])
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
