package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebSessionRotationTakeoverAndReplay(t *testing.T) {
	s := openTest(t)
	a, err := s.Register("webuser", "test-password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	device := strings.Repeat("a", 64)
	newAccess := func() string {
		t.Helper()
		token, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	first := newAccess()
	refresh, err := s.StartWebSession(first, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Expired access tokens may be swept without destroying the renewable family.
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(first)); err != nil {
		t.Fatal(err)
	}
	second, rotated, err := s.RotateWebSession(refresh, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(second); err != nil || !ok {
		t.Fatalf("rotated access invalid: %v", err)
	}
	if _, _, err := s.RotateWebSession(refresh, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("replay accepted: %v", err)
	}
	if _, ok, err := s.SessionAccount(second); err != nil || ok {
		t.Fatalf("replay did not revoke access: %v", err)
	}
	if _, _, err := s.RotateWebSession(rotated, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("family survived replay: %v", err)
	}
	third := newAccess()
	oldRefresh, err := s.StartWebSession(third, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fourth := newAccess()
	if _, err := s.StartWebSession(fourth, device, time.Minute, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(third); err != nil || ok {
		t.Fatalf("old access survived takeover: %v", err)
	}
	if _, _, err := s.RotateWebSession(oldRefresh, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old refresh survived takeover: %v", err)
	}
}

func TestWebAccessOwnershipBackfilledOnUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := challengeAccount(t, s)
	device := strings.Repeat("a", 64)
	access, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := s.StartWebSession(access, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM web_access_owners`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(access)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeWebSession(access, refresh); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RotateWebSession(refresh, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("backfilled token did not revoke family: %v", err)
	}
}

func TestRevokeOtherWebDeviceAfterAccessExpired(t *testing.T) {
	s := openTest(t)
	a, err := s.Register("webuser", "test-password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(device string) (string, string) {
		t.Helper()
		token, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
		if err != nil {
			t.Fatal(err)
		}
		refresh, err := s.StartWebSession(token, device, time.Minute, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token, refresh
	}
	current, currentRefresh := create(strings.Repeat("a", 64))
	other, otherRefresh := create(strings.Repeat("b", 64))
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(other)); err != nil {
		t.Fatal(err)
	}
	items, err := s.Sessions(a.ID, current)
	if err != nil || len(items) != 2 {
		t.Fatalf("renewable device not listed: %d %v", len(items), err)
	}
	count, err := s.RevokeOtherSessions(a.ID, current)
	if err != nil || count != 1 {
		t.Fatalf("revoke count %d: %v", count, err)
	}
	if _, _, err := s.RotateWebSession(otherRefresh, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("other device can still refresh: %v", err)
	}
	if _, _, err := s.RotateWebSession(currentRefresh, time.Minute); err != nil {
		t.Fatalf("current device was revoked: %v", err)
	}
}

func TestDeletingKeyRevokesRenewableChallengeSession(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	device := strings.Repeat("a", 64)
	token, err := s.NewSessionWithInfo(a.ID, "api-ssh-challenge", "ssh_challenge", "fp-alice", time.Hour, ClientInfo{DeviceID: device})
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := s.StartWebSession(token, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(token)); err != nil {
		t.Fatal(err)
	}
	removed, err := s.RemoveIdentity(a.ID, "fp-alice")
	if err != nil || !removed {
		t.Fatalf("delete key: %v", err)
	}
	if _, _, err := s.RotateWebSession(refresh, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("deleted key can still refresh: %v", err)
	}
}

func TestLogoutWebSessionWithSweptAccessAndMismatchedCredentials(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	b, err := s.Register("bob", "bob-password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	issue := func(id, device string) (string, string) {
		t.Helper()
		access, err := s.NewSessionWithInfo(id, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
		if err != nil {
			t.Fatal(err)
		}
		refresh, err := s.StartWebSession(access, device, time.Minute, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return access, refresh
	}
	accessA, refreshA := issue(a.ID, strings.Repeat("a", 64))
	accessB, refreshB := issue(b.ID, strings.Repeat("b", 64))
	if err := s.RevokeWebSession(accessA, refreshB); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("mismatched credentials accepted: %v", err)
	}
	if _, ok, err := s.SessionAccount(accessA); err != nil || !ok {
		t.Fatalf("mismatch revoked first account: %v", err)
	}
	if _, ok, err := s.SessionAccount(accessB); err != nil || !ok {
		t.Fatalf("mismatch revoked second account: %v", err)
	}
	if err := s.RevokeWebSession("unknown", refreshB); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("unknown access accepted: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash(accessA)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeWebSession(accessA, refreshB); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("swept access lost ownership: %v", err)
	}
	if err := s.RevokeWebSession(accessA, refreshA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RotateWebSession(refreshA, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("logged-out refresh still works: %v", err)
	}
	if _, _, err := s.RotateWebSession(refreshB, time.Minute); err != nil {
		t.Fatalf("other account was revoked: %v", err)
	}
}

func TestAccountLookupFailureRollsBackRefreshRotation(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	device := strings.Repeat("a", 64)
	access, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := s.StartWebSession(access, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a database failure during the account read, after token writes.
	if _, err = s.db.Exec(`ALTER TABLE identities RENAME TO identities_unavailable`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.RotateWebSessionWithAccount(refresh, time.Minute); err == nil {
		t.Fatal("account lookup unexpectedly succeeded")
	}
	if _, err = s.db.Exec(`ALTER TABLE identities_unavailable RENAME TO identities`); err != nil {
		t.Fatal(err)
	}
	_, _, got, err := s.RotateWebSessionWithAccount(refresh, time.Minute)
	if err != nil || got.ID != a.ID || !got.HasSSHKey || !got.PasswordLoginEnabled {
		t.Fatalf("rollback or account data incorrect: %+v %v", got, err)
	}
}
