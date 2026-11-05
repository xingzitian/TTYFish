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
}

func (s *Store) OnlineTerminal(accountID string) (*TerminalInfo, error) {
	var item TerminalInfo
	var started, seen int64
	now := time.Now().UnixMilli()
	err := s.db.QueryRow(`SELECT t.started_at,t.last_seen_at,t.source_ip,t.credential_fp FROM terminal_sessions t JOIN sessions s ON s.token_hash=t.token_hash WHERE t.account_id=? AND t.expires_at>? AND s.expires_at>?`, accountID, now, now).Scan(&started, &seen, &item.SourceIP, &item.CredentialFingerprint)
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
		if _, err = tx.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(token)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
