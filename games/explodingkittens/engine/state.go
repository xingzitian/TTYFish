package engine

import (
	"errors"
	"math/rand"
)

type Phase string

const (
	Playing     Phase = "playing"
	Responding  Phase = "responding"
	Giving      Phase = "giving"
	Reinserting Phase = "reinserting"
	Finished    Phase = "finished"
)

// State is authoritative and contains secrets. Only Observation may go to clients.
// Deck[0] is the top card; Seed and RandomCount make JSON restoration reproducible.
type State struct {
	Seed           int64    `json:"seed"`
	RandomCount    int64    `json:"random_count"`
	Hands          [][]Card `json:"hands"`
	Deck           []Card   `json:"deck"`
	Discard        []Card   `json:"discard"`
	DiscardBy      int      `json:"discard_by"`
	Removed        []Card   `json:"removed"`
	Alive          []bool   `json:"alive"`
	Turn           int      `json:"turn"`
	TurnsRemaining int      `json:"turns_remaining"`
	UnderAttack    bool     `json:"under_attack"`
	Phase          Phase    `json:"phase"`
	Winner         int      `json:"winner"`
	Pending        *Effect  `json:"pending,omitempty"`
	Kitten         *Card    `json:"kitten,omitempty"`
	Future         [][]Card `json:"future"`
	Log            []Event  `json:"log,omitempty"`
	LogSeq         int      `json:"log_seq"`
}

// Event contains only public information, never drawn/given cards or positions.
type Event struct {
	Seq  int    `json:"seq"`
	Seat int    `json:"seat"`
	Text string `json:"text"`
}

// Effect waits for all living players to pass. A Nope clears the pass votes,
// opening a new response window. The caller/platform decides when to pass;
// the engine never expires responses based on wall-clock time.
type Effect struct {
	Action Action `json:"action"`
	Kind   Kind   `json:"kind"`
	Nopes  int    `json:"nopes"`
	Passed []bool `json:"passed"`
}

func NewGame(seats int, seed int64) (*State, error) {
	if seats < 2 || seats > 5 {
		return nil, errors.New("exploding kittens requires 2 to 5 players")
	}
	s := &State{
		Seed: seed, Hands: make([][]Card, seats), Alive: make([]bool, seats),
		Future: make([][]Card, seats), TurnsRemaining: 1, Phase: Playing,
		Winner: -1, DiscardBy: -1,
	}
	var kittens, defuses, deck []Card
	for _, card := range NewDeck() {
		switch card.Kind {
		case ExplodingKitten:
			kittens = append(kittens, card)
		case Defuse:
			defuses = append(defuses, card)
		default:
			deck = append(deck, card)
		}
	}
	s.random().Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	for seat := range s.Hands {
		s.Alive[seat] = true
		s.Hands[seat] = append([]Card{defuses[seat]}, deck[:7]...)
		deck = deck[7:]
	}
	extras := min(2, len(defuses)-seats)
	deck = append(deck, defuses[seats:seats+extras]...)
	deck = append(deck, kittens[:seats-1]...)
	s.Removed = append(s.Removed, defuses[seats+extras:]...)
	s.Removed = append(s.Removed, kittens[seats-1:]...)
	s.Deck = deck
	s.shuffle()
	return s, nil
}

func (s *State) random() *rand.Rand {
	s.RandomCount++
	return rand.New(rand.NewSource(s.Seed + s.RandomCount*6364136223846793005))
}

func (s *State) shuffle() {
	s.random().Shuffle(len(s.Deck), func(i, j int) { s.Deck[i], s.Deck[j] = s.Deck[j], s.Deck[i] })
	s.clearFuture()
}

func (s *State) clearFuture() {
	for seat := range s.Future {
		s.Future[seat] = nil
	}
}

func (s *State) nextSeat() int {
	for step := 1; step <= len(s.Alive); step++ {
		seat := (s.Turn + step) % len(s.Alive)
		if s.Alive[seat] {
			return seat
		}
	}
	return s.Turn
}

func (s *State) endTurn() {
	s.TurnsRemaining--
	if s.TurnsRemaining == 0 {
		s.Turn = s.nextSeat()
		s.TurnsRemaining = 1
		s.UnderAttack = false
	}
	s.Phase = Playing
}

func (s *State) explode(card Card) {
	s.Removed = append(s.Removed, s.Hands[s.Turn]...)
	s.Hands[s.Turn] = nil
	s.Discard = append(s.Discard, card)
	s.Alive[s.Turn] = false
	living := 0
	for seat, alive := range s.Alive {
		if alive {
			living++
			s.Winner = seat
		}
	}
	if living == 1 {
		s.Phase = Finished
		s.TurnsRemaining = 0
		return
	}
	s.Winner = -1
	s.Turn = s.nextSeat()
	s.TurnsRemaining = 1
	s.UnderAttack = false
}
