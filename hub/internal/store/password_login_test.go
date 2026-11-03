package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPasswordLoginMigrationDefaultsExistingAccountsToEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE accounts(id TEXT PRIMARY KEY, handle TEXT NOT NULL UNIQUE, display TEXT NOT NULL, created_at INTEGER NOT NULL);
INSERT INTO accounts VALUES('legacy-id', 'legacy', 'legacy', 1);
`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, ok, err := s.ByID("legacy-id")
	if err != nil || !ok || !a.PasswordLoginEnabled {
		t.Fatalf("legacy account should keep password login enabled: account=%+v ok=%t err=%v", a, ok, err)
	}
}
