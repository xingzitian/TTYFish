package store

import (
	"database/sql"
	"time"
)

type TerminalInfo struct {
	StartedAt             time.Time `json:"started_at"`
	LastSeenAt            time.Time `json:"last_seen_at"`
	SourceIP              string    `json:"source_ip"`
	CredentialFingerprint string    `json:"credential_fingerprint"`
	AuthMethod            string    `json:"auth_method"`
	KeyName               string    `json:"key_name"`
}

func (s *Store) OnlineTerminal(accountID string) (*TerminalInfo, error) {
	var item TerminalInfo
	var started, seen int64
	now := time.Now().UnixMilli()
	err := s.db.QueryRow(`SELECT t.started_at,t.last_seen_at,t.source_ip,t.credential_fp,s.auth_method,COALESCE(i.name,'') FROM terminal_sessions t JOIN sessions s ON s.token_hash=t.token_hash LEFT JOIN identities i ON i.account_id=t.account_id AND i.fingerprint=t.credential_fp WHERE t.account_id=? AND t.expires_at>? AND s.expires_at>?`, accountID, now, now).Scan(&started, &seen, &item.SourceIP, &item.CredentialFingerprint, &item.AuthMethod, &item.KeyName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.StartedAt, item.LastSeenAt = time.UnixMilli(started), time.UnixMilli(seen)
	return &item, nil
}

// ClaimTerminal atomically replaces the account's previous interactive lease.
func (s *Store) ClaimTerminal(accountID, token string, ttl time.Duration) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	var source, fp string
	err = tx.QueryRow(`SELECT source_ip,credential_fp FROM sessions WHERE account_id=? AND token_hash=? AND expires_at>? AND family_id=''`, accountID, hash(token), now).Scan(&source, &fp)
	if err == sql.ErrNoRows {
		return "", ErrSessionInvalid
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(`UPDATE login_history SET
 ended_at=CASE WHEN terminal_sessions.expires_at>? AND EXISTS(SELECT 1 FROM sessions WHERE token_hash=terminal_sessions.token_hash AND expires_at>?) THEN ? ELSE terminal_sessions.last_seen_at END,
 end_reason=CASE WHEN terminal_sessions.expires_at>? AND EXISTS(SELECT 1 FROM sessions WHERE token_hash=terminal_sessions.token_hash AND expires_at>?) THEN 'replaced' ELSE 'lost' END
 FROM terminal_sessions WHERE login_history.lease_id=terminal_sessions.lease_id AND terminal_sessions.account_id=? AND login_history.ended_at IS NULL`, now, now, now, now, now, accountID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE token_hash IN (SELECT token_hash FROM terminal_sessions WHERE account_id=? AND token_hash<>?)`, accountID, hash(token)); err != nil {
		return "", err
	}
	lease := randomID()
	if _, err = tx.Exec(`INSERT INTO terminal_sessions(account_id,lease_id,token_hash,started_at,last_seen_at,expires_at,source_ip,credential_fp) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(account_id) DO UPDATE SET lease_id=excluded.lease_id,token_hash=excluded.token_hash,started_at=excluded.started_at,last_seen_at=excluded.last_seen_at,expires_at=excluded.expires_at,source_ip=excluded.source_ip,credential_fp=excluded.credential_fp`, accountID, lease, hash(token), now, now, now+ttl.Milliseconds(), source, fp); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`UPDATE sessions SET note='terminal' WHERE token_hash=?`, hash(token)); err != nil {
		return "", err
	}
	if err = recordLogin(tx, accountID, hash(token), "ssh", lease, time.UnixMilli(now)); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return lease, nil
}

func (s *Store) HeartbeatTerminal(accountID, token, lease string, ttl time.Duration) error {
	now := time.Now().UnixMilli()
	result, err := s.db.Exec(`UPDATE terminal_sessions SET last_seen_at=?,expires_at=? WHERE account_id=? AND token_hash=? AND lease_id=? AND expires_at>? AND EXISTS (SELECT 1 FROM sessions WHERE token_hash=? AND expires_at>?)`, now, now+ttl.Milliseconds(), accountID, hash(token), lease, now, hash(token), now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSessionInvalid
	}
	return nil
}

func (s *Store) ReleaseTerminal(accountID, token, lease string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM terminal_sessions WHERE account_id=? AND token_hash=? AND lease_id=?`, accountID, hash(token), lease)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		if _, err = tx.Exec(`UPDATE login_history SET ended_at=?,end_reason='closed' WHERE lease_id=? AND account_id=? AND ended_at IS NULL`, time.Now().UnixMilli(), lease, accountID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(token)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
