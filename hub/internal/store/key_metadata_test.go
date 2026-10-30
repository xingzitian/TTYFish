package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyNameMigrationPreservesExistingCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE accounts(id TEXT PRIMARY KEY,handle TEXT NOT NULL UNIQUE,display TEXT NOT NULL,created_at INTEGER NOT NULL);
 CREATE TABLE identities(fingerprint TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),public_key TEXT NOT NULL,kind TEXT NOT NULL DEFAULT 'ssh',created_at INTEGER NOT NULL,last_used_at INTEGER NOT NULL);
 INSERT INTO accounts VALUES('id','alice','alice',1000);
 INSERT INTO identities VALUES('fp','id','pub','ssh',1000,2000);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := s.Identities("id")
	if err != nil || len(keys) != 1 || keys[0].Name != "" || keys[0].LastUsed.UnixMilli() != 2000 {
		t.Fatal("migration lost metadata")
	}
	name := "旧电脑"
	if err = s.LinkNamed("id", "fp", "pub", &name); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, err = s.Identities("id")
	if err != nil || keys[0].Name != name || keys[0].LastUsed.UnixMilli() != 2000 {
		t.Fatal("reopen changed metadata")
	}
}

func TestKeyLoginFailureRollsBackUsage(t *testing.T) {
	s := openTest(t)
	a, err := s.Register("alice", "", "fp", "pub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_key_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.KeySession(a.ID, "fp", time.Hour); err == nil {
		t.Fatal("session unexpectedly issued")
	}
	keys, err := s.Identities(a.ID)
	if err != nil || !keys[0].LastUsed.IsZero() {
		t.Fatal("failed authentication changed usage")
	}
}
