package explodingkittens

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/game"
)

var _ game.Game = Game{}

func responseState(t *testing.T) *engine.State {
	t.Helper()
	s, _ := engine.NewGame(3, 1)
	s.Hands = [][]engine.Card{
		{{ID: 100, Kind: engine.Favor}},
		{{ID: 101, Kind: engine.Nope}, {ID: 102, Kind: engine.TacoCat}},
		{{ID: 103, Kind: engine.Defuse}},
	}
	if err := (Game{}).Apply(s, engine.Action{Type: engine.Play, Seat: 0, CardIDs: []int{100}, Target: 1}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResponseDeadlineAndNopeChain(t *testing.T) {
	g := Game{}
	s := responseState(t)
	token, duration, expire := g.Pending(s)
	if token == "" || duration != 5*time.Second || g.SeatOf(expire) != -1 {
		t.Fatal("response timeout missing or client-addressable")
	}
	if err := g.Apply(s, engine.Action{Type: engine.Pass, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	if same, _, _ := g.Pending(s); same != token {
		t.Fatal("pass extended the shared response deadline")
	}
	if g.Turn(s) != 1 {
		t.Fatal("room will not drive the next response bot")
	}
	if err := g.Apply(s, engine.Action{Type: engine.PlayNope, Seat: 1, CardIDs: []int{101}}); err != nil {
		t.Fatal(err)
	}
	next, _, cancel := g.Pending(s)
	if next == token {
		t.Fatal("Nope did not open a new bounded response window")
	}
	before, _ := json.Marshal(s)
	if err := g.Apply(s, expire); err == nil {
		t.Fatal("stale deadline cancelled a new response")
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("stale timeout changed state")
	}
	if err := g.Apply(s, cancel); err != nil || s.Phase != engine.Playing || s.Pending != nil {
		t.Fatalf("odd Nope must cancel the effect: %v %+v", err, s.Pending)
	}
	if s.Turn != 0 || len(s.Hands[0]) != 0 {
		t.Fatal("cancelled Favor transferred a card")
	}
}

func TestFavorAndDefuseTimeoutActor(t *testing.T) {
	g := Game{}
	s := responseState(t)
	_, _, expire := g.Pending(s)
	if err := g.Apply(s, expire); err != nil || s.Phase != engine.Giving || g.Turn(s) != 1 {
		t.Fatalf("Favor did not delegate to target: %v", err)
	}
	if err := g.Apply(s, g.TimeoutAction(g.Observe(s, 1))); err != nil || s.Phase != engine.Playing {
		t.Fatalf("target could not give: %v", err)
	}
	if s.Hands[0][0].Kind != engine.TacoCat {
		t.Fatal("bot unnecessarily gave away Nope")
	}
	s.Hands[0] = []engine.Card{{ID: 104, Kind: engine.Defuse}}
	s.Deck = []engine.Card{{ID: 105, Kind: engine.ExplodingKitten}, {ID: 106, Kind: engine.Skip}}
	if err := g.Apply(s, engine.Action{Type: engine.Draw, Seat: 0}); err != nil {
		t.Fatal(err)
	}
	if g.Turn(s) != 0 || s.Phase != engine.Reinserting {
		t.Fatal("defuse actor lost")
	}
	if err := g.Apply(s, g.Bot(g.Observe(s, 0))); err != nil || s.Turn != 1 || s.Kitten != nil {
		t.Fatalf("bot failed to reinsert: %v", err)
	}
	events, seq := g.Events(s, 0)
	if len(events) < 4 || seq == 0 || strings.Contains(events[len(events)-1].Text, "position") {
		t.Fatal("missing or private public events")
	}
	if again, current := g.Events(s, seq); len(again) != 0 || current != seq {
		t.Fatal("events repeated")
	}
}

func TestBotsFinishAndEliminatedPlayersCannotAct(t *testing.T) {
	g := Game{}
	for seats := 2; seats <= 5; seats++ {
		for seed := int64(0); seed < 10; seed++ {
			players := make([]game.PlayerSeed, seats)
			state := g.NewMatch(players, seed)
			s := state.(*engine.State)
			for moves := 0; g.Phase(state) == game.Playing && moves < 2000; moves++ {
				seat := g.Turn(state)
				if !s.Alive[seat] {
					t.Fatal("eliminated actor scheduled")
				}
				a := g.Bot(g.Observe(state, seat))
				if a == nil {
					t.Fatalf("bot stalled: %+v", g.Observe(state, seat))
				}
				if err := g.Apply(state, a); err != nil {
					t.Fatalf("seats=%d seed=%d action=%+v: %v", seats, seed, a, err)
				}
			}
			if g.Phase(state) != game.MatchOver || g.MatchWinner(state) < 0 {
				t.Fatalf("bots did not finish seats=%d seed=%d", seats, seed)
			}
			if g.Scores(state)[g.MatchWinner(state)] != 1 {
				t.Fatal("winner missing")
			}
			for seat, alive := range s.Alive {
				if !alive {
					o := g.Observe(state, seat).(engine.Observation)
					if len(o.Hand) != 0 || g.TimeoutAction(o) != nil {
						t.Fatal("eliminated seat exposed a hand or was actionable")
					}
				}
			}
		}
	}
}
