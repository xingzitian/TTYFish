// Package explodingkittens adapts the rules to the TTYFish game contract.
package explodingkittens

import (
	"fmt"
	"time"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/game"
)

const ID = "exploding-kittens"

type Game struct{}

func (Game) Name() string     { return "爆炸猫" }
func (Game) MinSeats() int    { return 2 }
func (Game) MaxSeats() int    { return 5 }
func (Game) MatchTarget() int { return 0 }

func (Game) NewMatch(players []game.PlayerSeed, seed int64) any {
	s, err := engine.NewGame(len(players), seed)
	if err != nil {
		panic(err) // The room validates MinSeats/MaxSeats before creating a match.
	}
	return s
}

func (Game) NextRound(any) error { return engine.ErrPhase }

// responseTimeout is a platform-only intent, never accepted by the wire codec.
type responseTimeout struct{ token string }

func responseToken(s *engine.State) string {
	return fmt.Sprintf("kittens:nope:%d", len(s.Discard))
}

func (Game) Apply(state, action any) error {
	s, ok := state.(*engine.State)
	if !ok {
		return engine.ErrPhase
	}
	if timeout, ok := action.(responseTimeout); ok {
		if s.Phase != engine.Responding || timeout.token != responseToken(s) {
			return engine.ErrPhase
		}
		for seat, alive := range s.Alive {
			if alive && s.Phase == engine.Responding && !s.Pending.Passed[seat] {
				if err := engine.Apply(s, engine.Action{Type: engine.Pass, Seat: seat}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	a, ok := action.(engine.Action)
	if !ok {
		return engine.ErrChoice
	}
	return engine.Apply(s, a)
}

func (Game) SeatOf(action any) int {
	if a, ok := action.(engine.Action); ok {
		return a.Seat
	}
	return -1
}

func (Game) Observe(state any, seat int) any {
	if s, ok := state.(*engine.State); ok {
		return engine.Observe(s, seat)
	}
	return nil
}

func (Game) Phase(state any) game.Phase {
	if s, ok := state.(*engine.State); ok && s.Phase == engine.Finished {
		return game.MatchOver
	}
	return game.Playing
}

// Turn is the player the room should drive for bots/disconnection/timeouts,
// which can differ from the owner of the drawing turn during responses.
func (Game) Turn(state any) int {
	s := state.(*engine.State)
	switch s.Phase {
	case engine.Giving:
		return s.Pending.Action.Target
	case engine.Responding:
		for seat, alive := range s.Alive {
			if alive && !s.Pending.Passed[seat] {
				return seat
			}
		}
	}
	return s.Turn
}

func (Game) RoundWinner(state any) int { return state.(*engine.State).Winner }
func (Game) MatchWinner(state any) int { return state.(*engine.State).Winner }
func (Game) Scores(state any) []int {
	s := state.(*engine.State)
	scores := make([]int, len(s.Alive))
	if s.Winner >= 0 {
		scores[s.Winner] = 1
	}
	return scores
}

func (Game) Events(state any, since int) ([]game.Event, int) {
	s := state.(*engine.State)
	var events []game.Event
	for _, event := range s.Log {
		if event.Seq > since {
			events = append(events, game.Event{Seq: event.Seq, Seat: event.Seat, Text: event.Text})
		}
	}
	return events, s.LogSeq
}

func (Game) Pending(state any) (string, time.Duration, any) {
	s, ok := state.(*engine.State)
	if !ok || s.Phase != engine.Responding {
		return "", 0, nil
	}
	token := responseToken(s)
	return token, 5 * time.Second, responseTimeout{token}
}

func (Game) TimeoutAction(view any) any {
	o, ok := view.(engine.Observation)
	if !ok || o.Seat < 0 || o.Seat >= len(o.Alive) || !o.Alive[o.Seat] {
		return nil
	}
	switch o.Phase {
	case engine.Playing:
		if o.Seat == o.Turn {
			return engine.Action{Type: engine.Draw, Seat: o.Seat}
		}
	case engine.Responding:
		if o.Pending != nil && !o.Pending.Passed[o.Seat] {
			return engine.Action{Type: engine.Pass, Seat: o.Seat}
		}
	case engine.Giving:
		if o.Pending != nil && o.Pending.Action.Target == o.Seat && len(o.Hand) > 0 {
			card := o.Hand[0]
			for _, candidate := range o.Hand {
				if candidate.Kind != engine.Defuse && candidate.Kind != engine.Nope {
					card = candidate
					break
				}
			}
			return engine.Action{Type: engine.Give, Seat: o.Seat, CardIDs: []int{card.ID}}
		}
	case engine.Reinserting:
		if o.Seat == o.Turn {
			for _, card := range o.Hand {
				if card.Kind == engine.Defuse {
					return engine.Action{Type: engine.UseDefuse, Seat: o.Seat,
						CardIDs: []int{card.ID}, Position: o.DeckCount}
				}
			}
		}
	}
	return nil
}

func (g Game) Bot(view any) any {
	o, ok := view.(engine.Observation)
	if !ok {
		return nil
	}
	// Use only the bot's own filtered view, never the authoritative deck.
	if o.Phase == engine.Playing && o.Seat == o.Turn {
		for _, kind := range []engine.Kind{engine.Attack, engine.Skip} {
			if len(o.Future) > 0 && o.Future[0].Kind == engine.ExplodingKitten {
				for _, card := range o.Hand {
					if card.Kind == kind {
						return engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: []int{card.ID}}
					}
				}
			}
		}
		if len(o.Future) == 0 {
			for _, card := range o.Hand {
				if card.Kind == engine.SeeTheFuture {
					return engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: []int{card.ID}}
				}
			}
		}
	}
	return g.TimeoutAction(view)
}
