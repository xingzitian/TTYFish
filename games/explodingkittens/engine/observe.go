package engine

type Observation struct {
	Seat           int     `json:"seat"`
	Phase          Phase   `json:"phase"`
	Turn           int     `json:"turn"`
	TurnsRemaining int     `json:"turns_remaining"`
	UnderAttack    bool    `json:"under_attack"`
	Winner         int     `json:"winner"`
	Alive          []bool  `json:"alive"`
	HandCounts     []int   `json:"hand_counts"`
	Hand           []Card  `json:"hand"`
	DeckCount      int     `json:"deck_count"`
	Discard        []Card  `json:"discard"`
	Future         []Card  `json:"future,omitempty"`
	Pending        *Effect `json:"pending,omitempty"`
}

// Observe never exposes the deck order, other hands, random seed, removed
// cards or secret reinsertion position. Returned slices do not alias State.
func Observe(s *State, seat int) Observation {
	o := Observation{
		Seat: seat, Phase: s.Phase, Turn: s.Turn, TurnsRemaining: s.TurnsRemaining,
		UnderAttack: s.UnderAttack, Winner: s.Winner,
		Alive: append([]bool(nil), s.Alive...), HandCounts: make([]int, len(s.Hands)),
		DeckCount: len(s.Deck), Discard: append([]Card(nil), s.Discard...),
	}
	for i, hand := range s.Hands {
		o.HandCounts[i] = len(hand)
	}
	if seat >= 0 && seat < len(s.Hands) && s.Alive[seat] {
		o.Hand = append([]Card(nil), s.Hands[seat]...)
		o.Future = append([]Card(nil), s.Future[seat]...)
	}
	if s.Pending != nil {
		pending := *s.Pending
		pending.Passed = append([]bool(nil), pending.Passed...)
		pending.Action.CardIDs = append([]int(nil), pending.Action.CardIDs...)
		o.Pending = &pending
	}
	return o
}
