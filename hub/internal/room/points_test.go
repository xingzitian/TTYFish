package room

import (
	"errors"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/game"
)

func readyToWin(r *Room) *engine.State {
	s := r.state.(*engine.State)
	s.Turn = 0
	s.Active = engine.Red
	s.Discard = []engine.Card{{ID: 90, Color: engine.Red, Kind: engine.Number, Num: 3}}
	s.Hands = [][]engine.Card{
		{{ID: 91, Color: engine.Red, Kind: engine.Number, Num: 7}},
		{
			{ID: 92, Kind: engine.Wild},
			{ID: 93, Color: engine.Blue, Kind: engine.Number, Num: 9},
			{ID: 94, Color: engine.Green, Kind: engine.Number, Num: 9},
		},
	}
	return s
}

func TestFinalSettlementAllActionPaths(t *testing.T) {
	for _, path := range []string{"human", "bot", "timeout", "pending"} {
		t.Run(path, func(t *testing.T) {
			r, clk := newTestRoom(t)
			mustJoin(t, r, "account:alice", "Alice")
			mustJoin(t, r, "account:bob", "Bob")
			calls := 0
			r.opts.OnMatchEnd = func(result MatchResult) error {
				calls++
				if result.ID == "" || result.Identity != "account:alice" || result.Score != 68 {
					t.Fatalf("unexpected result %+v", result)
				}
				return nil
			}
			mustStart(t, r, "account:alice")
			s := readyToWin(r)
			if path == "human" {
				if err := r.Act("account:alice", engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
					t.Fatal(err)
				}
			} else {
				switch path {
				case "bot":
					r.Disconnect("account:alice")
				case "timeout":
					// Exercise the room's timeout completion path with a winning action.
					r.Game = winningTimeout{Game: uno.New(engine.Rules{})}
				case "pending":
					r.pendingAct = engine.PlayCard{Seat: 0, CardID: 91}
					r.pendingEnd = clk.now()
				}
				clk.add(2 * time.Minute)
				r.tick()
			}
			if calls != 1 || r.phase != Over || s.Scores[0] != 68 || s.Scores[1] != 0 {
				t.Fatalf("calls=%d phase=%v scores=%v", calls, r.phase, s.Scores)
			}
			r.afterStateLocked()
			r.tick()
			if calls != 1 {
				t.Fatal("duplicate reward")
			}
		})
	}
}

type winningTimeout struct{ uno.Game }

func (g winningTimeout) TimeoutAction(any) any {
	return engine.PlayCard{Seat: 0, CardID: 91}
}

func TestSettlementFailureRetriesAndProtectsResult(t *testing.T) {
	r, clk := newTestRoom(t)
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	var results []MatchResult
	fail := true
	r.opts.OnMatchEnd = func(result MatchResult) error {
		results = append(results, result)
		if fail {
			return errors.New("database unavailable")
		}
		return nil
	}
	mustStart(t, r, "alice")
	readyToWin(r)
	if err := r.Act("alice", engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
		t.Fatal(err)
	}
	if !r.settlementPending || r.phase != Playing {
		t.Fatal("failed settlement did not remain pending")
	}
	if err := r.Start("alice"); err == nil {
		t.Fatal("restarted before settlement")
	}
	if err := r.VoteRestart("alice", true); err == nil {
		t.Fatal("vote erased unsettled result")
	}
	r.Disconnect("alice")
	r.Disconnect("bob")
	clk.add(time.Hour)
	if r.Abandoned(time.Minute) {
		t.Fatal("unsettled room can be swept")
	}
	r.tick()
	fail = false
	r.tick()
	if r.settlementPending || r.phase != Over || len(results) != 3 {
		t.Fatalf("settlement did not recover: %+v", results)
	}
	for _, result := range results {
		if result != results[0] {
			t.Fatal("retry changed result or match ID")
		}
	}
	mustStart(t, r, "alice")
	if r.matchID == results[0].ID || r.settled {
		t.Fatal("new match reused settlement identity")
	}
}

func TestLongMatchSettlesOnlyFinalWinnerTotal(t *testing.T) {
	r, _ := newTestRoom(t)
	r.Game = uno.New(engine.Rules{MatchTo: 100})
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	var results []MatchResult
	r.opts.OnMatchEnd = func(result MatchResult) error {
		results = append(results, result)
		return nil
	}
	mustStart(t, r, "alice")
	s := readyToWin(r)
	s.Scores[1] = 50
	if err := r.Act("alice", engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
		t.Fatal(err)
	}
	if r.Game.Phase(s) != game.RoundOver || len(results) != 0 {
		t.Fatal("intermediate round rewarded")
	}
	id := r.matchID
	mustStart(t, r, "alice")
	if r.matchID != id {
		t.Fatal("next round changed match ID")
	}
	readyToWin(r)
	if err := r.Act("alice", engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Score != 136 || results[0].Identity != "alice" || s.Scores[1] != 50 {
		t.Fatalf("incorrect final reward: %+v", results)
	}
}

func TestCloseRetriesPendingSettlement(t *testing.T) {
	r, _ := newTestRoom(t)
	mustJoin(t, r, "alice", "Alice")
	mustJoin(t, r, "bob", "Bob")
	calls := 0
	r.opts.OnMatchEnd = func(MatchResult) error {
		calls++
		if calls == 1 {
			return errors.New("temporary database failure")
		}
		return nil
	}
	mustStart(t, r, "alice")
	readyToWin(r)
	if err := r.Act("alice", engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if calls != 2 || !r.closed || !r.settled || r.settlementPending {
		t.Fatalf("close did not settle: calls=%d settled=%v pending=%v", calls, r.settled, r.settlementPending)
	}
	r.Close()
	if calls != 2 {
		t.Fatal("closing twice repeated settlement")
	}
}
