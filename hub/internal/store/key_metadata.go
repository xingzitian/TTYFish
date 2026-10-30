package store

import (
	"database/sql"
	"time"
)

func migrateKeyNames(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(identities)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "name" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE identities ADD COLUMN name TEXT NOT NULL DEFAULT ''`)
	return err
}

// LinkNamed preserves usage timestamps and, when name is nil, the existing label.
func (s *Store) LinkNamed(id, fp, pub string, name *string) error {
	label := ""
	if name != nil {
		label = *name
	}
	result, err := s.db.Exec(`INSERT INTO identities(fingerprint,account_id,public_key,name,kind,created_at,last_used_at)
 VALUES(?,?,?,?,?,?,0) ON CONFLICT(fingerprint) DO UPDATE SET name=CASE WHEN ? THEN excluded.name ELSE identities.name END
 WHERE identities.account_id=excluded.account_id`, fp, id, pub, label, "ssh", time.Now().UnixMilli(), name != nil)
	if err != nil {
		return accountWriteError(err)
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}

func (s *Store) RenameIdentity(id, fp, name string) (bool, error) {
	result, err := s.db.Exec(`UPDATE identities SET name=? WHERE account_id=? AND fingerprint=?`, name, id, fp)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// KeySession rechecks ownership and records successful key authentication atomically with the session.
func (s *Store) KeySession(id, fp string, ttl time.Duration) (string, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	now := time.Now()
	result, err := tx.Exec(`UPDATE identities SET last_used_at=MAX(last_used_at,?) WHERE account_id=? AND fingerprint=?`, now.UnixMilli(), id, fp)
	if err != nil {
		return "", false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return "", false, err
	}
	token := randomToken()
	if _, err = tx.Exec(`INSERT INTO sessions(token_hash,account_id,created_at,expires_at,note) VALUES(?,?,?,?,?)`, hash(token), id, now.UnixMilli(), now.Add(ttl).UnixMilli(), "api-key"); err != nil {
		return "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return token, true, nil
}
