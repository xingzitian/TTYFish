package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionMigrationAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, err = db.Exec(`CREATE TABLE accounts(id TEXT PRIMARY KEY,handle TEXT NOT NULL UNIQUE,display TEXT NOT NULL,created_at INTEGER NOT NULL);
 CREATE TABLE sessions(token_hash TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),created_at INTEGER NOT NULL,expires_at INTEGER NOT NULL,note TEXT NOT NULL DEFAULT '');
 INSERT INTO accounts VALUES('alice-id','alice','alice',1);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?)`, hash("legacy-token"), "alice-id", now.UnixMilli(), now.Add(time.Hour).UnixMilli(), "api-key")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Sessions("alice-id", "legacy-token")
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	oldID := items[0].ID
	if len(oldID) != 24 || oldID == hash("legacy-token") || items[0].AuthMethod != "ssh_key" || items[0].CredentialFingerprint != "" || !items[0].Current {
		t.Fatal("migration metadata incorrect")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err = s.Sessions("alice-id", "legacy-token")
	if err != nil || items[0].ID != oldID {
		t.Fatal("migration changed session IDs")
	}
	if _, ok, err := s.SessionAccount("legacy-token"); err != nil || !ok {
		t.Fatal("migration invalidated token")
	}
}

func TestSessionActivityAndExpiry(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	first, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{SourceIP: "192.0.2.1", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.NewSession(a.ID, "other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.NewSession(a.ID, "expired", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE sessions SET last_seen_at=100`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(first); err != nil || !ok {
		t.Fatal(err)
	}
	items, err := s.Sessions(a.ID, first)
	if err != nil || len(items) != 2 {
		t.Fatal("expired sessions listed", err)
	}
	for _, item := range items {
		if item.Current {
			if item.LastSeenAt.UnixMilli() <= 100 {
				t.Fatal("activity not updated")
			}
		} else if item.LastSeenAt.UnixMilli() != 100 {
			t.Fatal("listing updated another session")
		}
	}
	if _, ok, _ := s.SessionAccount(expired); ok {
		t.Fatal("expired token valid")
	}
	if _, err = s.RevokeOtherSessions(a.ID, first); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.SessionAccount(second); ok {
		t.Fatal("other session survived")
	}
}

func TestBrowserDeviceSessionsRetainTokensAndMetadata(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	device := strings.Repeat("a", 64)
	first, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{
		SourceIP: "192.0.2.1", UserAgent: "Browser/1", DeviceID: device,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherDevice, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{
		SourceIP: "192.0.2.2", UserAgent: "Browser/1", DeviceID: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.NewSessionWithInfo(a.ID, "api", "ssh_challenge", "", time.Hour, ClientInfo{
		SourceIP: "192.0.2.3", UserAgent: "Browser/2", DeviceID: device,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(first); err != nil || !ok {
		t.Fatalf("previous browser tab session was revoked: ok=%t err=%v", ok, err)
	}
	items, err := s.Sessions(a.ID, latest)
	if err != nil || len(items) != 3 {
		t.Fatalf("got %d session tokens, err=%v", len(items), err)
	}
	if !items[0].Current || items[0].AuthMethod != "ssh_challenge" || items[0].UserAgent != "Browser/2" || items[0].SourceIP != "192.0.2.3" {
		t.Fatalf("latest device metadata was not selected: %+v", items[0])
	}
	if _, ok, err := s.SessionAccount(otherDevice); err != nil || !ok {
		t.Fatalf("different device was revoked: ok=%t err=%v", ok, err)
	}
}

func TestRevokeOtherSessionsPreservesCurrentBrowserAndCountsDevices(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	currentDevice := strings.Repeat("a", 64)
	first, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: currentDevice})
	if err != nil {
		t.Fatal(err)
	}
	sameDevice, err := s.NewSessionWithInfo(a.ID, "api", "ssh_challenge", "", time.Hour, ClientInfo{DeviceID: currentDevice})
	if err != nil {
		t.Fatal(err)
	}
	otherDevice, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.NewSession(a.ID, "legacy", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.RevokeOtherSessions(a.ID, first)
	if err != nil || count != 2 {
		t.Fatalf("revoked %d device groups, err=%v", count, err)
	}
	for _, token := range []string{first, sameDevice} {
		if _, ok, err := s.SessionAccount(token); err != nil || !ok {
			t.Fatalf("current browser token revoked: ok=%t err=%v", ok, err)
		}
	}
	for _, token := range []string{otherDevice, legacy} {
		if _, ok, err := s.SessionAccount(token); err != nil || ok {
			t.Fatalf("other device token survived: ok=%t err=%v", ok, err)
		}
	}
}

func TestConcurrentRevokeOthersKeepsWinner(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	first, _ := s.NewSession(a.ID, "one", time.Hour)
	second, _ := s.NewSession(a.ID, "two", time.Hour)
	var path string
	if err := s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	var successes int
	var mu sync.Mutex
	for i, token := range []string{first, second} {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			db := s
			if i == 1 {
				db = other
			}
			_, err := db.RevokeOtherSessions(a.ID, token)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.Is(err, ErrSessionInvalid) {
				t.Errorf("revoke: %v", err)
			}
		}(i, token)
	}
	wg.Wait()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE account_id=?`, a.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || successes != 1 {
		t.Fatalf("lost current session: count %d successes %d", count, successes)
	}
}

func TestKeyDeletionRevocationIsAtomic(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	token, ok, err := s.KeySession(a.ID, "fp-alice", time.Hour)
	if err != nil || !ok {
		t.Fatal(err)
	}
	c, err := s.NewAPIChallenge(a.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(c.Code, "alice", "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'test revocation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RemoveIdentity(a.ID, "fp-alice"); err == nil {
		t.Fatal("key deletion unexpectedly succeeded")
	}
	if _, ok, err = s.ByFingerprint("fp-alice"); err != nil || !ok {
		t.Fatal("failed revocation removed key")
	}
	if _, ok, err = s.SessionAccount(token); err != nil || !ok {
		t.Fatal("failed transaction revoked session")
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_revoke`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RemoveIdentity(a.ID, "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ = s.SessionAccount(token); ok {
		t.Fatal("key session survived removal")
	}
	if err = s.Link(a.ID, "fp-alice", "pub-alice"); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := s.RedeemAPIChallenge(c.Poll, time.Hour)
	if err != nil || state != "expired" {
		t.Fatal("re-adding a key revived its old challenge")
	}
}

func TestPasswordPolicyRollback(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	first, _ := s.NewSession(a.ID, "one", time.Hour)
	other, _ := s.NewSession(a.ID, "two", time.Hour)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_password BEFORE INSERT ON passwords BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPasswordAndRevokeOthers(a.ID, first, "replacement-password"); err == nil {
		t.Fatal("password write unexpectedly succeeded")
	}
	if _, ok, _ := s.SessionAccount(other); !ok {
		t.Fatal("revoked sessions despite password rollback")
	}
	if _, ok := s.AuthenticatePassword("alice", "alice-password-123"); !ok {
		t.Fatal("password changed despite rollback")
	}
}
