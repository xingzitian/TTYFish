package store

import (
	"database/sql"
	"errors"
	"regexp"
	"time"
)

var ErrSessionInvalid = errors.New("会话已失效")

// ClientInfo describes the direct HTTP client, not an independently verified device.
type ClientInfo struct {
	SourceIP  string
	UserAgent string
	// DeviceID is an untrusted, browser-generated identifier used only to coalesce
	// repeated browser logins into one revocable session.
	DeviceID string
}

var deviceIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SessionInfo struct {
	ID                    string
	AuthMethod            string
	CredentialFingerprint string
	CreatedAt             time.Time
	ExpiresAt             time.Time
	LastSeenAt            time.Time
	SourceIP              string
	UserAgent             string
	Current               bool
	DeviceID              string
}

func migrateSessions(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"session_id", `TEXT NOT NULL DEFAULT ''`}, {"auth_method", `TEXT NOT NULL DEFAULT 'unknown'`},
		{"credential_fp", `TEXT NOT NULL DEFAULT ''`}, {"last_seen_at", `INTEGER NOT NULL DEFAULT 0`},
		{"source_ip", `TEXT NOT NULL DEFAULT ''`}, {"user_agent", `TEXT NOT NULL DEFAULT ''`},
		{"device_id", `TEXT NOT NULL DEFAULT ''`},
	} {
		if !columns[column.name] {
			if _, err = tx.Exec("ALTER TABLE sessions ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return err
			}
		}
	}
	rows, err = tx.Query(`SELECT token_hash FROM sessions WHERE session_id=''`)
	if err != nil {
		return err
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err = rows.Scan(&h); err != nil {
			rows.Close()
			return err
		}
		hashes = append(hashes, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err = tx.Exec(`UPDATE sessions SET session_id=? WHERE token_hash=? AND session_id=''`, randomID(), h); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE sessions SET last_seen_at=created_at WHERE last_seen_at=0`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE sessions SET auth_method=CASE note WHEN 'api-key' THEN 'ssh_key' WHEN 'api-ssh-challenge' THEN 'ssh_challenge' ELSE 'unknown' END WHERE auth_method='unknown' AND note IN ('api-key','api-ssh-challenge')`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS sessions_public_id ON sessions(session_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

type sessionWriter interface {
	Exec(string, ...any) (sql.Result, error)
}

func insertSession(db sessionWriter, id, note, method, fp string, ttl time.Duration, info ClientInfo) (string, error) {
	if info.DeviceID != "" && !deviceIDPattern.MatchString(info.DeviceID) {
		return "", errors.New("invalid browser device identifier")
	}
	token := randomToken()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO sessions(token_hash,session_id,account_id,created_at,expires_at,note,auth_method,credential_fp,last_seen_at,source_ip,user_agent,device_id)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, hash(token), randomID(), id, now.UnixMilli(), now.Add(ttl).UnixMilli(), note, method, fp, now.UnixMilli(), info.SourceIP, info.UserAgent, info.DeviceID)
	if err != nil {
		return "", err
	}
	return token, nil
}
func (s *Store) NewSessionWithInfo(id, note, method, fp string, ttl time.Duration, info ClientInfo) (string, error) {
	return insertSession(s.db, id, note, method, fp, ttl, info)
}
func validSession(db sessionWriter, id, token string) (bool, error) {
	result, err := db.Exec(`UPDATE sessions SET last_seen_at=MAX(last_seen_at,?) WHERE account_id=? AND token_hash=? AND expires_at>?`, time.Now().UnixMilli(), id, hash(token), time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) Sessions(id, token string) ([]SessionInfo, error) {
	ok, err := validSession(s.db, id, token)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionInvalid
	}
	rows, err := s.db.Query(`SELECT session_id,auth_method,credential_fp,created_at,expires_at,last_seen_at,source_ip,user_agent,token_hash=?,device_id
 FROM sessions WHERE account_id=? AND expires_at>? ORDER BY created_at DESC,session_id DESC`, hash(token), id, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SessionInfo, 0)
	for rows.Next() {
		var item SessionInfo
		var created, expires, seen int64
		if err = rows.Scan(&item.ID, &item.AuthMethod, &item.CredentialFingerprint, &created, &expires, &seen, &item.SourceIP, &item.UserAgent, &item.Current, &item.DeviceID); err != nil {
			return nil, err
		}
		item.CreatedAt = time.UnixMilli(created)
		item.ExpiresAt = time.UnixMilli(expires)
		item.LastSeenAt = time.UnixMilli(seen)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeSession(id, token, sessionID string) (bool, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.Exec(`DELETE FROM sessions WHERE account_id=? AND expires_at>?
 AND (session_id=? OR (device_id<>'' AND device_id=(SELECT device_id FROM sessions WHERE account_id=? AND session_id=? AND device_id<>'' AND expires_at>?)))
 AND EXISTS (SELECT 1 FROM sessions caller WHERE caller.account_id=? AND caller.token_hash=? AND caller.expires_at>?)`, id, now, sessionID, id, sessionID, now, id, hash(token), now)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n > 0 {
		return n > 0, err
	}
	ok, err := validSession(s.db, id, token)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrSessionInvalid
	}
	return false, nil
}
func (s *Store) RevokeOtherSessions(id, token string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ok, err := validSession(tx, id, token)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrSessionInvalid
	}
	now := time.Now().UnixMilli()
	var deviceID string
	if err = tx.QueryRow(`SELECT device_id FROM sessions WHERE account_id=? AND token_hash=? AND expires_at>?`, id, hash(token), now).Scan(&deviceID); err != nil {
		return 0, err
	}
	var count int64
	if deviceID != "" {
		err = tx.QueryRow(`SELECT COUNT(DISTINCT CASE WHEN device_id='' THEN session_id ELSE device_id END)
 FROM sessions WHERE account_id=? AND expires_at>? AND device_id<>?`, id, now, deviceID).Scan(&count)
	} else {
		err = tx.QueryRow(`SELECT COUNT(DISTINCT CASE WHEN device_id='' THEN session_id ELSE device_id END)
 FROM sessions WHERE account_id=? AND expires_at>? AND token_hash<>?`, id, now, hash(token)).Scan(&count)
	}
	if err != nil {
		return 0, err
	}
	var result sql.Result
	if deviceID != "" {
		result, err = tx.Exec(`DELETE FROM sessions WHERE account_id=? AND expires_at>? AND device_id<>?`, id, now, deviceID)
	} else {
		result, err = tx.Exec(`DELETE FROM sessions WHERE account_id=? AND expires_at>? AND token_hash<>?`, id, now, hash(token))
	}
	if err != nil {
		return 0, err
	}
	if _, err := result.RowsAffected(); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// SetPasswordAndRevokeOthers preserves the current session and makes the policy atomic.
func (s *Store) SetPasswordAndRevokeOthers(id, token, password string) error {
	digest, err := passwordHash(password)
	if err != nil {
		return err
	}
	if digest == "" {
		return errors.New("密码不能为空")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ok, err := validSession(tx, id, token)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSessionInvalid
	}
	now := time.Now().UnixMilli()
	var deviceID string
	if err = tx.QueryRow(`SELECT device_id FROM sessions WHERE account_id=? AND token_hash=? AND expires_at>?`, id, hash(token), now).Scan(&deviceID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO passwords(account_id,digest) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET digest=excluded.digest`, id, digest); err != nil {
		return err
	}
	if deviceID != "" {
		if _, err = tx.Exec(`DELETE FROM sessions WHERE account_id=? AND device_id<>?`, id, deviceID); err != nil {
			return err
		}
	} else if _, err = tx.Exec(`DELETE FROM sessions WHERE account_id=? AND token_hash<>?`, id, hash(token)); err != nil {
		return err
	}
	return tx.Commit()
}
