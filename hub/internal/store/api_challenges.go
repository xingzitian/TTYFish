package store

import (
	"database/sql"
	"errors"
	"time"
)

const apiChallengePurpose = "api_ssh_login"

var ErrChallengeInvalid = errors.New("挑战无效、已使用或已过期")

func (s *Store) NewAPIChallenge(accountID string, ttl time.Duration) (Challenge, error) {
	return s.NewChallenge(apiChallengePurpose, accountID, ttl)
}

// ConfirmAPIChallenge verifies the account, username, key, purpose and deadline atomically.
func (s *Store) ConfirmAPIChallenge(code, username, fp string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.Exec(`UPDATE challenges SET state='confirmed',confirmed_fp=?
 WHERE code_hash=? AND purpose=? AND state='pending' AND expires_at>?
 AND EXISTS (SELECT 1 FROM accounts a JOIN identities i ON i.account_id=a.id
 WHERE a.id=challenges.account_id AND a.handle=? AND i.fingerprint=?)`,
		fp, hash(code), apiChallengePurpose, now, username, fp)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrChallengeInvalid
	}
	if _, err = tx.Exec(`UPDATE identities SET last_used_at=MAX(last_used_at,?) WHERE fingerprint=?`, now, fp); err != nil {
		return err
	}
	return tx.Commit()
}

// RedeemAPIChallenge consumes the challenge and inserts its session in one transaction.
// The DELETE is the first statement, serializing claims even across database handles.
func (s *Store) RedeemAPIChallenge(poll string, ttl time.Duration) (state string, a Account, token string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", a, "", err
	}
	defer tx.Rollback()
	now := time.Now()
	var id, fp string
	err = tx.QueryRow(`DELETE FROM challenges WHERE poll_hash=? AND purpose=? AND state='confirmed' AND expires_at>?
 RETURNING account_id,confirmed_fp`, hash(poll), apiChallengePurpose, now.UnixMilli()).Scan(&id, &fp)
	if err == sql.ErrNoRows {
		var pending int
		err = tx.QueryRow(`SELECT COUNT(*) FROM challenges WHERE poll_hash=? AND purpose=? AND state='pending' AND expires_at>?`, hash(poll), apiChallengePurpose, now.UnixMilli()).Scan(&pending)
		if err != nil {
			return "", a, "", err
		}
		if pending > 0 {
			return "pending", a, "", nil
		}
		return "expired", a, "", nil
	}
	if err != nil {
		return "", a, "", err
	}
	var created int64
	err = tx.QueryRow(`SELECT a.id,a.handle,a.display,a.created_at FROM accounts a JOIN identities i ON i.account_id=a.id WHERE a.id=? AND i.fingerprint=?`, id, fp).Scan(&a.ID, &a.Handle, &a.Display, &created)
	if err == sql.ErrNoRows {
		return "expired", Account{}, "", tx.Commit()
	}
	if err != nil {
		return "", Account{}, "", err
	}
	a.CreatedAt = time.UnixMilli(created)
	token = randomToken()
	_, err = tx.Exec(`INSERT INTO sessions(token_hash,account_id,created_at,expires_at,note) VALUES(?,?,?,?,?)`, hash(token), a.ID, now.UnixMilli(), now.Add(ttl).UnixMilli(), "api-ssh-challenge")
	if err != nil {
		return "", Account{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return "", Account{}, "", err
	}
	return "ok", a, token, nil
}
