package store

import (
	"database/sql"
	"time"
)

// StartWebSession upgrades an authenticated browser access token to a renewable
// family. Authentication must have happened before calling this method.
func (s *Store) StartWebSession(access, device string, accessTTL, refreshTTL time.Duration) (string, error) {
	if !deviceIDPattern.MatchString(device) {
		return "", ErrSessionInvalid
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now()
	var accountID string
	err = tx.QueryRow(`SELECT account_id FROM sessions WHERE token_hash=? AND expires_at>? AND device_id=?`, hash(access), now.UnixMilli(), device).Scan(&accountID)
	if err == sql.ErrNoRows {
		return "", ErrSessionInvalid
	}
	if err != nil {
		return "", err
	}
	// A new authenticated login replaces all previous authorizations of this browser.
	if _, err = tx.Exec(`UPDATE web_sessions SET revoked_at=? WHERE account_id=? AND device_id=? AND revoked_at=0`, now.UnixMilli(), accountID, device); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE account_id=? AND device_id=? AND token_hash<>?`, accountID, device, hash(access)); err != nil {
		return "", err
	}
	family, refresh := randomID(), randomToken()
	if _, err = tx.Exec(`INSERT INTO web_sessions(id,account_id,device_id,created_at,expires_at,last_seen_at,auth_method,credential_fp,source_ip,user_agent)
 SELECT ?,account_id,device_id,?,?,?,auth_method,credential_fp,source_ip,user_agent FROM sessions WHERE token_hash=?`, family, now.UnixMilli(), now.Add(refreshTTL).UnixMilli(), now.UnixMilli(), hash(access)); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO refresh_tokens(token_hash,family_id) VALUES(?,?)`, hash(refresh), family); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`UPDATE sessions SET family_id=?,expires_at=? WHERE token_hash=?`, family, now.Add(accessTTL).UnixMilli(), hash(access)); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO web_access_owners(token_hash,family_id) VALUES(?,?)`, hash(access), family); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return refresh, nil
}

// RotateWebSession consumes a refresh token exactly once. Reuse revokes the
// entire family, including access tokens. The deadline is absolute, not sliding.
func (s *Store) RotateWebSession(refresh string, accessTTL time.Duration) (string, string, error) {
	return s.rotateWebSession(refresh, accessTTL, nil)
}

// RotateWebSessionWithAccount reads the account before committing rotation, so
// a failed account lookup cannot consume the only browser refresh credential.
func (s *Store) RotateWebSessionWithAccount(refresh string, accessTTL time.Duration) (string, string, Account, error) {
	var a Account
	access, next, err := s.rotateWebSession(refresh, accessTTL, &a)
	return access, next, a, err
}

func (s *Store) rotateWebSession(refresh string, accessTTL time.Duration, account *Account) (string, string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	now := time.Now()
	var family, accountID, device string
	var used, expires, revoked int64
	err = tx.QueryRow(`SELECT r.family_id,w.account_id,w.device_id,r.used_at,w.expires_at,w.revoked_at FROM refresh_tokens r JOIN web_sessions w ON w.id=r.family_id WHERE r.token_hash=?`, hash(refresh)).Scan(&family, &accountID, &device, &used, &expires, &revoked)
	if err == sql.ErrNoRows {
		return "", "", ErrSessionInvalid
	}
	if err != nil {
		return "", "", err
	}
	if used != 0 {
		if _, err = tx.Exec(`UPDATE web_sessions SET revoked_at=? WHERE id=?`, now.UnixMilli(), family); err != nil {
			return "", "", err
		}
		if _, err = tx.Exec(`DELETE FROM sessions WHERE family_id=?`, family); err != nil {
			return "", "", err
		}
		if err = tx.Commit(); err != nil {
			return "", "", err
		}
		return "", "", ErrSessionInvalid
	}
	if revoked != 0 || expires <= now.UnixMilli() {
		return "", "", ErrSessionInvalid
	}
	if _, err = tx.Exec(`UPDATE refresh_tokens SET used_at=? WHERE token_hash=?`, now.UnixMilli(), hash(refresh)); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(`UPDATE web_sessions SET last_seen_at=? WHERE id=?`, now.UnixMilli(), family); err != nil {
		return "", "", err
	}
	next, access := randomToken(), randomToken()
	if _, err = tx.Exec(`INSERT INTO refresh_tokens(token_hash,family_id) VALUES(?,?)`, hash(next), family); err != nil {
		return "", "", err
	}
	// Preserve authentication metadata, including the SSH key used by a challenge.
	result, err := tx.Exec(`INSERT INTO sessions(token_hash,session_id,account_id,created_at,expires_at,note,auth_method,credential_fp,last_seen_at,source_ip,user_agent,device_id,family_id)
	 SELECT ?,?,account_id,?,?,'web-refresh',auth_method,credential_fp,?,source_ip,user_agent,device_id,id FROM web_sessions WHERE id=?`, hash(access), randomID(), now.UnixMilli(), now.Add(accessTTL).UnixMilli(), now.UnixMilli(), family)
	if err != nil {
		return "", "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", "", err
	}
	if n != 1 {
		return "", "", ErrSessionInvalid
	}
	if _, err = tx.Exec(`INSERT INTO web_access_owners(token_hash,family_id) VALUES(?,?)`, hash(access), family); err != nil {
		return "", "", err
	}
	if account != nil {
		var created int64
		err = tx.QueryRow(`SELECT id,handle,display,created_at,password_login_enabled,EXISTS(SELECT 1 FROM identities WHERE account_id=accounts.id) FROM accounts WHERE id=?`, accountID).Scan(&account.ID, &account.Handle, &account.Display, &created, &account.PasswordLoginEnabled, &account.HasSSHKey)
		if err != nil {
			return "", "", err
		}
		account.CreatedAt = time.UnixMilli(created)
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	return access, next, nil
}

// RevokeWebSession revokes a family using either of its credentials. The
// refresh credential allows logout even after the access credential expires.
func (s *Store) RevokeWebSession(access, refresh string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var family, cookieFamily string
	if access != "" {
		err = tx.QueryRow(`SELECT family_id FROM web_access_owners WHERE token_hash=?`, hash(access)).Scan(&family)
		if err == sql.ErrNoRows {
			err = tx.QueryRow(`SELECT family_id FROM sessions WHERE token_hash=?`, hash(access)).Scan(&family)
		}
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows && refresh != "" {
			return ErrSessionInvalid
		}
	}
	if refresh != "" {
		err = tx.QueryRow(`SELECT family_id FROM refresh_tokens WHERE token_hash=?`, hash(refresh)).Scan(&cookieFamily)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if cookieFamily != "" && access != "" && cookieFamily != family {
			return ErrSessionInvalid
		}
	}
	if access == "" {
		family = cookieFamily
	}
	if family == "" {
		return nil
	}
	if _, err = tx.Exec(`UPDATE web_sessions SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().UnixMilli(), family); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE family_id=?`, family); err != nil {
		return err
	}
	return tx.Commit()
}
