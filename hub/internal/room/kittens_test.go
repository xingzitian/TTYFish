package room

import (
	"errors"
	"sync"
	"testing"
	"time"

	kittens "github.com/xingzitian/TTYFish/games/explodingkittens"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
)

func TestKittensDLCIsRoomScopedAndHostControlled(t *testing.T) {
	r, _ := newTestRoom(t)
	r.Game = kittens.Game{}
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	if err := r.SetDLC("bob", kittens.ReverseDLC, true); !errors.Is(err, ErrNotHost) {
		t.Fatalf("non-host: %v", err)
	}
	if err := r.SetDLC("alice", "unknown", true); err == nil {
		t.Fatal("unknown DLC accepted")
	}
	if err := r.SetDLC("alice", kittens.ReverseDLC, true); err != nil {
		t.Fatal(err)
	}
	snap, _ := r.SnapshotFor("bob")
	if len(snap.DLC) != 1 || snap.DLC[0] != kittens.ReverseDLC {
		t.Fatalf("DLC not broadcast: %+v", snap.DLC)
	}
	mustStart(t, r, "alice")
	if err := r.SetDLC("alice", kittens.ReverseDLC, false); !errors.Is(err, ErrPhase) {
		t.Fatalf("in-game modification: %v", err)
	}
	s := r.state.(*engine.State)
	count := 0
	for _, c := range s.Deck {
		if c.Kind == engine.Reverse {
			count++
		}
	}
	for _, hand := range s.Hands {
		for _, c := range hand {
			if c.Kind == engine.Reverse {
				count++
			}
		}
	}
	if count != 4 {
		t.Fatalf("DLC deck contains %d reverse cards", count)
	}
	other, _ := newTestRoom(t)
	other.Game = kittens.Game{}
	mustJoin(t, other, "carol", "Carol")
	mustJoin(t, other, "dan", "Dan")
	mustStart(t, other, "carol")
	for _, c := range other.state.(*engine.State).Deck {
		if c.Kind == engine.Reverse {
			t.Fatal("DLC leaked to another room")
		}
	}
}

func TestConcurrentDLCTogglesAreAtomic(t *testing.T) {
	r, _ := newTestRoom(t)
	r.Game = kittens.Game{}
	mustJoin(t, r, "alice", "Alice")
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.ToggleDLC("alice", kittens.ReverseDLC) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	snap, err := r.SnapshotFor("alice")
	if err != nil || len(snap.DLC) != 0 {
		t.Fatalf("20 toggles must return to off: %v %v", snap.DLC, err)
	}
}

func TestKittensResponseWindowAndDisconnectedFavorTarget(t *testing.T) {
	r, clock := newTestRoom(t)
	r.Game = kittens.Game{}
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	mustStart(t, r, "alice")
	s := r.state.(*engine.State)
	s.Hands = [][]engine.Card{
		{{ID: 100, Kind: engine.Favor}},
		{{ID: 101, Kind: engine.TacoCat}, {ID: 102, Kind: engine.Nope}},
	}
	r.opts.BotLag = 10 * time.Second
	if err := r.Act("alice", engine.Action{Type: engine.Play, Seat: 0, CardIDs: []int{100}, Target: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := r.pendingEnd
	clock.add(time.Second)
	if err := r.Act("alice", engine.Action{Type: engine.Pass, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	if r.pendingEnd != deadline {
		t.Fatal("passing extended response deadline")
	}
	if err := r.Act("bob", engine.Action{Type: engine.PlayNope, Seat: 1, CardIDs: []int{102}}); err != nil {
		t.Fatal(err)
	}
	if !r.pendingEnd.After(deadline) {
		t.Fatal("Nope deadline not renewed")
	}
	// Counter-Nope returns to even parity so Favor must resolve.
	s.Hands[0] = []engine.Card{{ID: 103, Kind: engine.Nope}}
	if err := r.Act("alice", engine.Action{Type: engine.PlayNope, Seat: 0, CardIDs: []int{103}}); err != nil {
		t.Fatal(err)
	}
	clock.add(6 * time.Second)
	r.tick()
	if s.Phase != engine.Giving || r.Game.Turn(s) != 1 {
		t.Fatalf("Favor did not reach target after deadline: %s", s.Phase)
	}
	r.Disconnect("bob")
	clock.add(11 * time.Second)
	r.tick()
	if s.Phase != engine.Playing || len(s.Hands[0]) != 1 || s.Hands[0][0].Kind != engine.TacoCat {
		t.Fatal("disconnected Favor target blocked the room")
	}
	snap, err := r.SnapshotFor("alice")
	if err != nil || len(snap.Timeline) < 4 || snap.PendingLeftMS != 0 {
		t.Fatalf("timeline/pending not published: %+v %v", snap, err)
	}
}

func TestKittensEliminatedSpectatorSnapshotIsPrivate(t *testing.T) {
	r, _ := newTestRoom(t)
	r.Game = kittens.Game{}
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	mustJoin(t, r, "carol", "Carol")
	mustStart(t, r, "alice")
	s := r.state.(*engine.State)
	s.Hands[0] = nil
	s.Deck = []engine.Card{{ID: 100, Kind: engine.ExplodingKitten}, {ID: 101, Kind: engine.Skip}, {ID: 102, Kind: engine.Attack}, {ID: 103, Kind: engine.Nope}}
	if err := r.Act("alice", engine.Action{Type: engine.Draw, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want int
	}{{"alice", 3}, {"bob", 0}, {"carol", 0}} {
		snap, err := r.SnapshotFor(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		view := snap.View.(engine.Observation)
		if len(view.SpectatorTop) != tc.want {
			t.Fatalf("%s sees %v", tc.name, view.SpectatorTop)
		}
		if tc.want > 0 && view.SpectatorTop[0].Kind != engine.Skip {
			t.Fatalf("wrong deck top: %v", view.SpectatorTop)
		}
	}
	if err := r.Act("bob", engine.Action{Type: engine.Draw, Seat: 1}); err != nil {
		t.Fatal(err)
	}
	snap, err := r.SnapshotFor("alice")
	if err != nil {
		t.Fatal(err)
	}
	if top := snap.View.(engine.Observation).SpectatorTop; len(top) != 2 || top[0].Kind != engine.Attack {
		t.Fatalf("top not refreshed: %v", top)
	}
	r.Disconnect("alice")
	if _, err := r.Join("alice", "Alice"); err != nil {
		t.Fatalf("spectator reconnect: %v", err)
	}
	snap, err = r.SnapshotFor("alice")
	if err != nil {
		t.Fatal(err)
	}
	if top := snap.View.(engine.Observation).SpectatorTop; len(top) != 2 || top[0].Kind != engine.Attack {
		t.Fatalf("spectator view lost on reconnect: %v", top)
	}
}

func TestKittensEliminationFinishAndRestart(t *testing.T) {
	r, _ := newTestRoom(t)
	r.Game = kittens.Game{}
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	mustStart(t, r, "alice")
	s := r.state.(*engine.State)
	s.Hands[0] = nil
	s.Deck = []engine.Card{{ID: 100, Kind: engine.ExplodingKitten}}
	if err := r.Act("alice", engine.Action{Type: engine.Draw, Seat: 1}); err != ErrSeat {
		t.Fatal("seat forgery accepted")
	}
	if err := r.Act("alice", engine.Action{Type: engine.Draw, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	if r.phase != Over || s.Winner != 1 || s.Alive[0] {
		t.Fatal("elimination did not end match")
	}
	snap, _ := r.SnapshotFor("alice")
	o := snap.View.(engine.Observation)
	if len(o.Hand) != 0 || o.Winner != 1 {
		t.Fatal("dead player's snapshot wrong")
	}
	if err := r.VoteRestart("alice", true); err != nil {
		t.Fatal(err)
	}
	if err := r.VoteRestart("bob", true); err != nil {
		t.Fatal(err)
	}
	if r.phase != Lobby {
		t.Fatal("restart vote did not return to lobby")
	}
	mustStart(t, r, "alice")
	next := r.state.(*engine.State)
	if r.phase != Playing || next == s || !next.Alive[0] || len(next.Hands[0]) != 8 {
		t.Fatal("restart did not create a fresh game")
	}
}
