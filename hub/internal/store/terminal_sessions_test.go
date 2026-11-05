package store

import (
	"errors"
	"testing"
	"time"
)

func TestTerminalTakeoverAndStaleRelease(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	first, err := s.NewSession(a.ID, "ssh", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	lease1, err := s.ClaimTerminal(a.ID, first, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.NewSession(a.ID, "ssh", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	lease2, err := s.ClaimTerminal(a.ID, second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(first); err != nil || ok {
		t.Fatalf("old token survived takeover: %v", err)
	}
	if err := s.HeartbeatTerminal(a.ID, first, lease1, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old lease survived takeover: %v", err)
	}
	if err := s.ReleaseTerminal(a.ID, first, lease1); err != nil {
		t.Fatal(err)
	}
	if err := s.HeartbeatTerminal(a.ID, second, lease2, time.Minute); err != nil {
		t.Fatalf("stale release removed new lease: %v", err)
	}
	if err := s.ReleaseTerminal(a.ID, second, lease2); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(second); err != nil || ok {
		t.Fatalf("released token survived: %v", err)
	}
}

func TestExpiredTerminalCannotAuthorize(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	token, err := s.NewSession(a.ID, "ssh", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.ClaimTerminal(a.ID, token, -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.SessionAccount(token); err != nil || ok {
		t.Fatalf("expired terminal still authorized: %v", err)
	}
	if err := s.HeartbeatTerminal(a.ID, token, lease, time.Minute); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expired lease renewed: %v", err)
	}
}
