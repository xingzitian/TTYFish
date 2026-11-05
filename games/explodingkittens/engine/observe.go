package engine

type Observation struct {
	Seat           int    `json:"seat"`
	Phase          Phase  `json:"phase"`
	Turn           int    `json:"turn"`
	TurnsRemaining int    `json:"turns_remaining"`
	UnderAttack    bool   `json:"under_attack"`
	Winner         int    `json:"winner"`
	Alive          []bool `json:"alive"`
	HandCounts     []int  `json:"hand_counts"`
	Hand           []Card `json:"hand"`
	DeckCount      int    `json:"deck_count"`
	// BombsLeft 是牌堆里还剩几颗爆炸猫。这是公开信息（人人都能数），
	// 不含顺序；界面用它算"下一张摸到炸弹的概率"。
	BombsLeft int    `json:"bombs_left"`
	Discard   []Card `json:"discard"`
	// DiscardBy 是最后一张弃牌的出牌人，-1 表示还没有弃牌。
	DiscardBy int     `json:"discard_by"`
	Future    []Card  `json:"future,omitempty"`
	Pending   *Effect `json:"pending,omitempty"`
}

// Observe never exposes the deck order, other hands, random seed, removed
// cards or secret reinsertion position. Returned slices do not alias State.
func Observe(s *State, seat int) Observation {
	o := Observation{
		Seat: seat, Phase: s.Phase, Turn: s.Turn, TurnsRemaining: s.TurnsRemaining,
		UnderAttack: s.UnderAttack, Winner: s.Winner,
		Alive: append([]bool(nil), s.Alive...), HandCounts: make([]int, len(s.Hands)),
		DeckCount: len(s.Deck), Discard: append([]Card(nil), s.Discard...),
		DiscardBy: s.DiscardBy,
	}
	for i, hand := range s.Hands {
		o.HandCounts[i] = len(hand)
	}
	for _, card := range s.Deck {
		if card.Kind == ExplodingKitten {
			o.BombsLeft++
		}
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
