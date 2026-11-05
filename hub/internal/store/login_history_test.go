package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryUpgradeDoesNotInventOldLogins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := challengeAccount(t, s)
	old, err := s.NewSession(a.ID, "api", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`DROP TABLE login_history`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok, err := s.SessionAccount(old); err != nil || !ok {
		t.Fatalf("old session lost: %v", err)
	}
	groups, total, err := s.LoginHistory(a.ID, "web", 0, 10)
	if err != nil || total != 0 || len(groups) != 0 {
		t.Fatalf("invented old history: %+v %v", groups, err)
	}
}

func TestLoginHistoryWebGroupingAndNoRefreshEvent(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	if _, err := s.db.Exec(`UPDATE identities SET name='笔记本' WHERE fingerprint='fp-alice'`); err != nil {
		t.Fatal(err)
	}
	login := func(device, method, fp string) string {
		t.Helper()
		access, err := s.NewSessionWithInfo(a.ID, "api", method, fp, time.Hour, ClientInfo{DeviceID: device, SourceIP: "192.0.2.1", UserAgent: "Browser"})
		if err != nil {
			t.Fatal(err)
		}
		refresh, err := s.StartWebSession(access, device, time.Minute, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return refresh
	}
	device := strings.Repeat("a", 64)
	refresh := login(device, "ssh_challenge", "fp-alice")
	if _, _, err := s.RotateWebSession(refresh, time.Minute); err != nil {
		t.Fatal(err)
	}
	login(device, "password", "")
	login(strings.Repeat("b", 64), "password", "")
	if ok, err := s.RemoveIdentity(a.ID, "fp-alice"); err != nil || !ok {
		t.Fatalf("remove key: %v", err)
	}
	groups, total, err := s.LoginHistory(a.ID, "web", 0, 1)
	if err != nil || total != 2 || len(groups) != 1 {
		t.Fatalf("first page: %+v total=%d err=%v", groups, total, err)
	}
	groups2, _, err := s.LoginHistory(a.ID, "web", 1, 1)
	if err != nil || len(groups2) != 1 {
		t.Fatalf("second page: %v", err)
	}
	all := append(groups, groups2...)
	count := 0
	found := false
	for _, g := range all {
		count += g.Count
		for _, e := range g.Events {
			if e.Fingerprint == "fp-alice" && e.KeyName == "笔记本" {
				found = true
			}
		}
	}
	if count != 3 || !found {
		t.Fatalf("unexpected events or missing historical key name: %+v", all)
	}
	other, err := s.Register("bob", "password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := s.LoginHistory(other.ID, "web", 0, 10)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatal("history leaked to another account")
	}
}

func TestLoginHistoryTerminalLifecycleAndRollback(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	issue := func() string {
		t.Helper()
		token, err := s.NewSessionWithInfo(a.ID, "api-key", "ssh_key", "fp-alice", time.Hour, ClientInfo{SourceIP: "192.0.2.5"})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	first := issue()
	lease1, err := s.ClaimTerminal(a.ID, first, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second := issue()
	if _, err = s.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON login_history BEGIN SELECT RAISE(ABORT,'history unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTerminal(a.ID, second, time.Minute); err == nil {
		t.Fatal("claim succeeded without history")
	}
	if err = s.HeartbeatTerminal(a.ID, first, lease1, time.Minute); err != nil {
		t.Fatalf("failed claim replaced old terminal: %v", err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_history`); err != nil {
		t.Fatal(err)
	}
	lease2, err := s.ClaimTerminal(a.ID, second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseTerminal(a.ID, first, lease1); err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseTerminal(a.ID, second, lease2); err != nil {
		t.Fatal(err)
	}
	third := issue()
	if _, err = s.ClaimTerminal(a.ID, third, -time.Second); err != nil {
		t.Fatal(err)
	}
	beforeSweep, _, err := s.LoginHistory(a.ID, "ssh", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	seenLost := false
	for _, event := range beforeSweep[0].Events {
		if event.EndReason == "lost" && event.EndedAt != nil {
			seenLost = true
		}
	}
	if !seenLost {
		t.Fatal("expired terminal not shown as lost before cleanup")
	}
	s.Sweep()
	groups, total, err := s.LoginHistory(a.ID, "ssh", 0, 10)
	if err != nil || total != 1 || groups[0].Count != 3 {
		t.Fatalf("history: %+v %d %v", groups, total, err)
	}
	reasons := map[string]int{}
	for _, e := range groups[0].Events {
		reasons[e.EndReason]++
		if e.EndedAt == nil {
			t.Fatal("missing end time")
		}
	}
	for _, reason := range []string{"replaced", "closed", "lost"} {
		if reasons[reason] != 1 {
			t.Fatalf("reasons: %v", reasons)
		}
	}
}

func TestWebLoginHistoryFailureRollsBackTakeover(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	device := strings.Repeat("a", 64)
	issue := func() string {
		t.Helper()
		token, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: device})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	first := issue()
	refresh, err := s.StartWebSession(first, device, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second := issue()
	if _, err = s.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON login_history BEGIN SELECT RAISE(ABORT,'history unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartWebSession(second, device, time.Minute, time.Hour); err == nil {
		t.Fatal("session created without audit history")
	}
	if _, ok, err := s.SessionAccount(first); err != nil || !ok {
		t.Fatalf("failed login revoked prior access: %v", err)
	}
	if _, _, err = s.RotateWebSession(refresh, time.Minute); err != nil {
		t.Fatalf("failed login revoked prior refresh: %v", err)
	}
}

func TestRevokedTerminalIsNotReportedAsReplaced(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	first, err := s.NewSessionWithInfo(a.ID, "api-key", "ssh_key", "fp-alice", time.Hour, ClientInfo{SourceIP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTerminal(a.ID, first, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = s.DropSession(first); err != nil {
		t.Fatal(err)
	}
	second, err := s.NewSessionWithInfo(a.ID, "api-key", "ssh_key", "fp-alice", time.Hour, ClientInfo{SourceIP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTerminal(a.ID, second, time.Minute); err != nil {
		t.Fatal(err)
	}
	groups, _, err := s.LoginHistory(a.ID, "ssh", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]int{}
	for _, event := range groups[0].Events {
		reasons[event.EndReason]++
	}
	if reasons["lost"] != 1 || reasons["replaced"] != 0 {
		t.Fatalf("unexpected statuses: %v", reasons)
	}
}

func TestLoginHistoryRetentionLimit(t *testing.T) {
	s := openTest(t)
	a := challengeAccount(t, s)
	now := time.Now()
	for i := 0; i < 1001; i++ {
		_, err := s.db.Exec(`INSERT INTO login_history(account_id,channel,auth_method,started_at) VALUES(?,?,?,?)`, a.ID, "web", "password", now.Add(time.Duration(i)*time.Millisecond).UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
	}
	access, err := s.NewSessionWithInfo(a.ID, "api", "password", "", time.Hour, ClientInfo{DeviceID: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartWebSession(access, strings.Repeat("a", 64), time.Minute, time.Hour); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM login_history WHERE account_id=?`, a.ID).Scan(&count); err != nil || count != 1000 {
		t.Fatal(fmt.Sprintf("retained %d: %v", count, err))
	}
	if _, err = s.db.Exec(`UPDATE login_history SET started_at=?`, now.Add(-31*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	s.Sweep()
	if err = s.db.QueryRow(`SELECT count(*) FROM login_history WHERE account_id=?`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old events retained: %d %v", count, err)
	}
}
