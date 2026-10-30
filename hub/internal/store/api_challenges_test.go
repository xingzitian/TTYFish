package store

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func challengeAccount(t *testing.T, s *Store) Account {
	t.Helper()
	a, err := s.Register("alice", "alice-password-123", "fp-alice", "pub-alice")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAPIChallengeIsolationExpiryAndKeyRevocation(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	c, err := s.NewAPIChallenge(a.ID, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var codeHash, pollHash string
	if err = s.db.QueryRow(`SELECT code_hash,poll_hash FROM challenges WHERE account_id=?`, a.ID).Scan(&codeHash, &pollHash); err != nil {
		t.Fatal(err)
	}
	if codeHash == c.Code || pollHash == c.Poll || codeHash != hash(c.Code) || pollHash != hash(c.Poll) {
		t.Fatal("challenge secrets were not hashed")
	}
	if _, _, err = s.ConfirmAccount(c.Code, "fp-alice", "alice"); err == nil {
		t.Fatal("legacy confirmation accepted API challenge")
	}
	if _, _, _, ok, err := s.Poll(c.Poll); err != nil || ok {
		t.Fatal("API challenge exposed to legacy browser poll")
	}
	for _, pair := range [][2]string{{"wrong", "fp-alice"}, {"alice", "wrong-key"}} {
		if err = s.ConfirmAPIChallenge(c.Code, pair[0], pair[1]); err != ErrChallengeInvalid {
			t.Fatalf("incorrect identity accepted: %v", err)
		}
	}
	if err = s.ConfirmAPIChallenge(c.Code, "alice", "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(c.Code, "alice", "fp-alice"); err != ErrChallengeInvalid {
		t.Fatal("confirmation replay accepted")
	}
	if _, err = s.RemoveIdentity(a.ID, "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if state, _, token, err := s.RedeemAPIChallenge(c.Poll, time.Hour); err != nil || state != "expired" || token != "" {
		t.Fatalf("revoked key redeemed: %s %v", state, err)
	}
	if err = s.Link(a.ID, "fp-alice", "pub-alice"); err != nil {
		t.Fatal(err)
	}
	expired, err := s.NewAPIChallenge(a.ID, -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(expired.Code, "alice", "fp-alice"); err != ErrChallengeInvalid {
		t.Fatal("expired challenge confirmed")
	}
	if state, _, _, err := s.RedeemAPIChallenge(expired.Poll, time.Hour); state != "expired" || err != nil {
		t.Fatalf("expired poll: %s %v", state, err)
	}
	confirmed, err := s.NewAPIChallenge(a.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(confirmed.Code, "alice", "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE challenges SET expires_at=? WHERE code_hash=?`, time.Now().Add(-time.Second).UnixMilli(), hash(confirmed.Code)); err != nil {
		t.Fatal(err)
	}
	if state, _, _, err := s.RedeemAPIChallenge(confirmed.Poll, time.Hour); state != "expired" || err != nil {
		t.Fatal("expired confirmed challenge redeemed")
	}
	legacy, err := s.NewChallenge("login", a.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(legacy.Code, "alice", "fp-alice"); err != ErrChallengeInvalid {
		t.Fatal("API confirmed legacy challenge")
	}
	if state, _, _, err := s.RedeemAPIChallenge(legacy.Poll, time.Hour); state != "expired" || err != nil {
		t.Fatal("API redeemed legacy challenge")
	}
}

func TestAPIChallengeConcurrentRedeemIsSingleUse(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	c, err := s.NewAPIChallenge(a.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	state, _, _, err := s.RedeemAPIChallenge(c.Poll, time.Hour)
	if err != nil || state != "pending" {
		t.Fatal("expected pending")
	}
	if err = s.ConfirmAPIChallenge(c.Code, "alice", "fp-alice"); err != nil {
		t.Fatal(err)
	}
	// A second DB handle models two backend instances sharing a database.
	var path string
	if err = s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := s
			if i%2 == 1 {
				db = other
			}
			state, got, token, err := db.RedeemAPIChallenge(c.Poll, time.Hour)
			if err != nil {
				t.Errorf("redeem: %v", err)
				return
			}
			if state == "ok" {
				winners.Add(1)
				if got.ID != a.ID || token == "" {
					t.Error("incorrect session")
				}
			} else if state != "expired" {
				t.Errorf("unexpected state %s", state)
			}
		}(i)
	}
	wg.Wait()
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE account_id=?`, a.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if winners.Load() != 1 || count != 1 {
		t.Fatalf("issued %d successes, %d sessions", winners.Load(), count)
	}
}

func TestAPIChallengeSessionFailureRollsBackClaim(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	c, err := s.NewAPIChallenge(a.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmAPIChallenge(c.Code, "alice", "fp-alice"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'test session failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.RedeemAPIChallenge(c.Poll, time.Hour); err == nil {
		t.Fatal("session insertion unexpectedly succeeded")
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_session`); err != nil {
		t.Fatal(err)
	}
	state, _, token, err := s.RedeemAPIChallenge(c.Poll, time.Hour)
	if err != nil || state != "ok" || token == "" {
		t.Fatalf("claim lost after rollback: %s %v", state, err)
	}
	if _, ok, err := s.SessionAccount(token); err != nil || !ok {
		t.Fatal("issued session invalid")
	}
	if state, _, _, err = s.RedeemAPIChallenge(strings.Repeat("0", 64), time.Hour); err != nil || state != "expired" {
		t.Fatal("unknown poll token accepted")
	}
}
