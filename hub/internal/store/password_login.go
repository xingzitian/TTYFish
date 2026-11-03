package store

import "database/sql"

func migratePasswordLogin(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(accounts)`)
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
		if name == "password_login_enabled" {
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
	_, err = db.Exec(`ALTER TABLE accounts ADD COLUMN password_login_enabled INTEGER NOT NULL DEFAULT 1`)
	return err
}
