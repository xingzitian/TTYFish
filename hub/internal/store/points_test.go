package store

import (
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"testing"
)

func TestPTMigrationPersistenceAndIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE accounts(id TEXT PRIMARY KEY,handle TEXT NOT NULL UNIQUE,
		display TEXT NOT NULL,created_at INTEGER NOT NULL);
		INSERT INTO accounts VALUES('alice','alice','Alice',0),('bob','bob','Bob',0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.PTTenths("alice"); err != nil || got != 0 {
		t.Fatalf("old account balance=%d err=%v", got, err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := st.AwardUNO("match-1", "alice", 68); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := st.AwardUNO("match-2", "alice", 1); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		match, account string
		score          int64
	}{
		{"match-1", "bob", 68}, {"match-1", "alice", 69},
		{"", "alice", 1}, {"negative", "alice", -1}, {"unknown", "missing", 68},
	} {
		if err := st.AwardUNO(invalid.match, invalid.account, invalid.score); err == nil {
			t.Fatalf("accepted invalid reward %+v", invalid)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AwardUNO("match-1", "alice", 68); err != nil {
		t.Fatal(err)
	}
	for account, want := range map[string]int64{"alice": 69, "bob": 0} {
		if got, err := st.PTTenths(account); err != nil || got != want {
			t.Fatalf("%s balance=%d want=%d err=%v", account, got, want, err)
		}
	}
	if _, err := st.PTTenths("missing"); err != sql.ErrNoRows {
		t.Fatalf("missing account error=%v", err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM uno_pt_rewards`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rewards=%d err=%v", n, err)
	}
}

func TestPTOverflowRollsBackResult(t *testing.T) {
	st := openTest(t)
	a, err := st.EnsureAccount("fp", "pub", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AwardUNO("max", a.ID, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := st.AwardUNO("overflow", a.ID, 1); err == nil {
		t.Fatal("overflow accepted")
	}
	if err := st.AwardKittens("overflow", a.ID, 50); err == nil {
		t.Fatal("kittens overflow accepted")
	}
	if got, err := st.PTTenths(a.ID); err != nil || got != math.MaxInt64 {
		t.Fatalf("balance=%d err=%v", got, err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM uno_pt_rewards WHERE match_id='overflow'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed result was not rolled back: %d %v", n, err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM kittens_pt_rewards`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed kittens result was not rolled back: %d %v", n, err)
	}
}

func TestKittensRewardsPersistAndShareBalance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "points.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	a, err := st.EnsureAccount("fp", "pub", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := st.AwardKittens("match-2", a.ID, 50); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for seats := 3; seats <= 5; seats++ {
		if err := st.AwardKittens(fmt.Sprintf("match-%d", seats), a.ID, int64(seats-1)*50); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AwardUNO("uno-match", a.ID, 68); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		match, account string
		score          int64
	}{
		{"match-2", a.ID, 100}, {"match-2", "missing", 50},
		{"", a.ID, 50}, {"unknown", "missing", 50},
		{"negative", a.ID, -50}, {"zero", a.ID, 0},
		{"fraction", a.ID, 51}, {"too-large", a.ID, 250},
	} {
		if err := st.AwardKittens(invalid.match, invalid.account, invalid.score); err == nil {
			t.Fatalf("accepted invalid kittens reward %+v", invalid)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AwardKittens("match-2", a.ID, 50); err != nil {
		t.Fatal(err)
	}
	if got, err := st.PTTenths(a.ID); err != nil || got != 568 {
		t.Fatalf("shared balance=%d want=568 err=%v", got, err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM kittens_pt_rewards`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("kittens rewards=%d want=4 err=%v", n, err)
	}
}
