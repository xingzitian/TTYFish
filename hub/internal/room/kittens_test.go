package room

import (
	"testing"
	"time"

	kittens "github.com/xingzitian/TTYFish/games/explodingkittens"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
)

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
