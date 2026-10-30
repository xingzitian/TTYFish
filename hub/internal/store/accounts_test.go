package store

import "testing"

func TestFixedAccountCredentials(t *testing.T) {
	s := openTest(t)
	a, err := s.Register("alice", "long-password-123", "fp-alice", "pub-alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "Alice", "a b", "ab"} {
		if _, err := s.Register(name, "long-password-123", "", ""); err == nil {
			t.Fatalf("accepted duplicate/invalid name %q", name)
		}
	}
	if _, err := s.Register("empty", "", "", ""); err == nil {
		t.Fatal("accepted no credentials")
	}
	if _, err := s.Register("bob", "", "fp-alice", "pub"); err == nil {
		t.Fatal("key reused")
	}
	if _, ok, _ := s.ByHandle("bob"); ok {
		t.Fatal("failed registration left an account")
	}
	if got, ok := s.AuthenticatePassword("alice", "long-password-123"); !ok || got.ID != a.ID {
		t.Fatal("password login failed")
	}
	for _, name := range []string{"Alice", "other"} {
		if _, ok := s.AuthenticatePassword(name, "long-password-123"); ok {
			t.Fatal("wrong username accepted")
		}
	}
	if _, ok := s.AuthenticatePassword("alice", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if err := s.SetPassword(a.ID, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.AuthenticatePassword("alice", "long-password-123"); ok {
		t.Fatal("old password accepted")
	}
	if _, ok := s.AuthenticatePassword("alice", "replacement-password"); !ok {
		t.Fatal("new password rejected")
	}
	b, err := s.Register("bob", "bob-password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(b.ID, "fp-alice", "pub"); err == nil {
		t.Fatal("key transferred to another account")
	}
	if err := s.Link(a.ID, "fp-alice-2", "pub2"); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := s.ByFingerprint("fp-alice-2")
	if !ok || got.ID != a.ID {
		t.Fatal("second key has different identity")
	}
}
