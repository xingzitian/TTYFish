package engine

import (
	"errors"
	"fmt"
)

type ActionType string

const (
	Draw      ActionType = "draw"
	Play      ActionType = "play"
	PlayNope  ActionType = "nope"
	Pass      ActionType = "pass_response"
	Give      ActionType = "give"
	UseDefuse ActionType = "defuse"
)

// CardIDs are stable IDs, never hand indexes. Play takes one action card, or
// two/three cards of the same kind for a combo. Target is required for steals
// and Favor; Requested names the kind demanded by a three-card combo.
// Position for UseDefuse is 0..len(Deck), including the top and bottom gaps.
type Action struct {
	Type      ActionType `json:"type"`
	Seat      int        `json:"seat"`
	CardIDs   []int      `json:"card_ids,omitempty"`
	Target    int        `json:"target"`
	Requested Kind       `json:"requested,omitempty"`
	Position  int        `json:"position"`
}

var (
	ErrPhase  = errors.New("当前阶段不能执行此操作")
	ErrSeat   = errors.New("现在还不能由你行动")
	ErrCard   = errors.New("所选手牌无效或不能这样出牌")
	ErrTarget = errors.New("目标玩家无效或没有手牌")
	ErrChoice = errors.New("选择无效")
)

// Apply validates before mutating; rejected actions leave State unchanged.
func Apply(s *State, a Action) error {
	if s == nil {
		return ErrPhase
	}
	oldPhase := s.Phase
	oldPending := s.Pending
	var kind Kind
	if a.Type == Play && a.Seat >= 0 && a.Seat < len(s.Hands) && len(a.CardIDs) > 0 {
		for _, card := range s.Hands[a.Seat] {
			if card.ID == a.CardIDs[0] {
				kind = card.Kind
				break
			}
		}
	}
	if err := apply(s, a); err != nil {
		return err
	}
	text := ""
	switch a.Type {
	case Draw:
		text = "摸了一张牌"
		if !s.Alive[a.Seat] {
			text = "被爆炸猫炸中，已淘汰"
		} else if s.Phase == Reinserting {
			text = "摸到爆炸猫，等待拆弹"
		}
	case Play:
		text = "打出" + kind.Name()
		if len(a.CardIDs) > 1 {
			text += fmt.Sprintf(" ×%d", len(a.CardIDs))
		}
		if len(a.CardIDs) > 1 || kind == Favor {
			text += fmt.Sprintf("，目标为 P%d", a.Target+1)
		}
		if len(a.CardIDs) == 3 {
			text += "，索要" + a.Requested.Name()
		}
	case PlayNope:
		text = "打出否决，原效果将被取消（仍可反否决）"
		if s.Pending.Nopes%2 == 0 {
			text = "打出反否决，原效果将恢复（仍可否决）"
		}
	case Give:
		text = "交出了一张牌"
	case UseDefuse:
		text = "完成拆弹，已秘密放回爆炸猫"
	case Pass:
		if oldPhase == Responding && s.Phase != Responding {
			text = "反制窗口结束"
			if oldPending != nil && oldPending.Nopes%2 == 1 {
				text += "，" + oldPending.Kind.Name() + "效果已取消"
			} else if oldPending != nil {
				text += "，" + oldPending.Kind.Name() + "效果已生效"
			}
		}
	}
	if text != "" {
		s.LogSeq++
		s.Log = append(s.Log, Event{Seq: s.LogSeq, Seat: a.Seat, Text: text})
		if len(s.Log) > 128 {
			s.Log = append([]Event(nil), s.Log[len(s.Log)-128:]...)
		}
	}
	return nil
}

func apply(s *State, a Action) error {
	if s == nil || s.Phase == Finished {
		return ErrPhase
	}
	if a.Seat < 0 || a.Seat >= len(s.Alive) || !s.Alive[a.Seat] {
		return ErrSeat
	}
	switch s.Phase {
	case Responding:
		return s.respond(a)
	case Giving:
		return s.give(a)
	case Reinserting:
		return s.defuse(a)
	case Playing:
		if a.Seat != s.Turn {
			return ErrSeat
		}
		switch a.Type {
		case Draw:
			if len(s.Deck) == 0 {
				return ErrChoice
			}
			s.draw()
			return nil
		case Play:
			return s.play(a)
		}
	}
	return ErrPhase
}

func (s *State) cards(seat int, ids []int) ([]Card, error) {
	if len(ids) == 0 {
		return nil, ErrCard
	}
	selected := make([]Card, 0, len(ids))
	seen := map[int]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, ErrCard
		}
		seen[id] = true
		found := false
		for _, card := range s.Hands[seat] {
			if card.ID == id {
				selected = append(selected, card)
				found = true
				break
			}
		}
		if !found {
			return nil, ErrCard
		}
	}
	return selected, nil
}

func (s *State) take(seat, id int) Card {
	for i, card := range s.Hands[seat] {
		if card.ID == id {
			s.Hands[seat] = append(s.Hands[seat][:i], s.Hands[seat][i+1:]...)
			return card
		}
	}
	panic("validated card disappeared")
}

func (s *State) targetValid(actor, target int) bool {
	return target >= 0 && target < len(s.Alive) && target != actor &&
		s.Alive[target] && len(s.Hands[target]) > 0
}

func (s *State) play(a Action) error {
	cards, err := s.cards(a.Seat, a.CardIDs)
	if err != nil || len(cards) > 3 {
		return ErrCard
	}
	kind := cards[0].Kind
	for _, card := range cards {
		if card.Kind != kind || card.Kind == ExplodingKitten {
			return ErrCard
		}
	}
	if len(cards) == 1 {
		switch kind {
		case Attack, Skip, Shuffle, SeeTheFuture, Favor:
		default:
			return ErrCard
		}
	}
	if len(cards) > 1 || kind == Favor {
		if !s.targetValid(a.Seat, a.Target) {
			return ErrTarget
		}
	}
	if len(cards) == 3 && !a.Requested.valid() {
		return ErrChoice
	}
	for _, card := range cards {
		s.Discard = append(s.Discard, s.take(a.Seat, card.ID))
		s.DiscardBy = a.Seat
	}
	a.CardIDs = append([]int(nil), a.CardIDs...)
	s.Pending = &Effect{Action: a, Kind: kind, Passed: make([]bool, len(s.Alive))}
	s.Phase = Responding
	return nil
}

func (s *State) respond(a Action) error {
	switch a.Type {
	case PlayNope:
		cards, err := s.cards(a.Seat, a.CardIDs)
		if err != nil || len(cards) != 1 || cards[0].Kind != Nope {
			return ErrCard
		}
		s.Discard = append(s.Discard, s.take(a.Seat, cards[0].ID))
		s.DiscardBy = a.Seat
		s.Pending.Nopes++
		s.Pending.Passed = make([]bool, len(s.Alive))
	case Pass:
		if s.Pending.Passed[a.Seat] {
			return ErrChoice
		}
		s.Pending.Passed[a.Seat] = true
		for seat, alive := range s.Alive {
			if alive && !s.Pending.Passed[seat] {
				return nil
			}
		}
		if s.Pending.Nopes%2 == 1 {
			s.Pending = nil
			s.Phase = Playing
		} else {
			s.resolve()
		}
	default:
		return ErrPhase
	}
	return nil
}

func (s *State) resolve() {
	a := s.Pending.Action
	count := len(a.CardIDs)
	s.Phase = Playing
	// A target may spend their last card on a Nope while the effect is pending.
	if (count > 1 || s.Pending.Kind == Favor) &&
		len(s.Hands[a.Target]) == 0 {
		s.Pending = nil
		return
	}
	if count > 1 {
		if count == 2 {
			index := s.random().Intn(len(s.Hands[a.Target]))
			s.Hands[a.Seat] = append(s.Hands[a.Seat], s.take(a.Target, s.Hands[a.Target][index].ID))
		} else {
			for _, card := range s.Hands[a.Target] {
				if card.Kind == a.Requested {
					s.Hands[a.Seat] = append(s.Hands[a.Seat], s.take(a.Target, card.ID))
					break
				}
			}
		}
	} else {
		switch s.Pending.Kind {
		case Attack:
			turns := 2
			if s.UnderAttack {
				turns += s.TurnsRemaining
			}
			s.Turn = s.nextSeat()
			s.TurnsRemaining = turns
			s.UnderAttack = true
		case Skip:
			s.endTurn()
		case Shuffle:
			s.shuffle()
		case SeeTheFuture:
			s.Future[a.Seat] = append([]Card(nil), s.Deck[:min(3, len(s.Deck))]...)
			s.PeekSeq++
		case Favor:
			s.Phase = Giving
			return
		}
	}
	s.Pending = nil
}

func (s *State) give(a Action) error {
	if a.Type != Give {
		return ErrPhase
	}
	if a.Seat != s.Pending.Action.Target {
		return ErrSeat
	}
	cards, err := s.cards(a.Seat, a.CardIDs)
	if err != nil || len(cards) != 1 {
		return ErrCard
	}
	actor := s.Pending.Action.Seat
	s.Hands[actor] = append(s.Hands[actor], s.take(a.Seat, cards[0].ID))
	s.Pending = nil
	s.Phase = Playing
	return nil
}

func (s *State) draw() {
	card := s.Deck[0]
	s.Deck = s.Deck[1:]
	s.clearFuture()
	if card.Kind != ExplodingKitten {
		s.Hands[s.Turn] = append(s.Hands[s.Turn], card)
		s.endTurn()
		return
	}
	for _, held := range s.Hands[s.Turn] {
		if held.Kind == Defuse {
			s.Kitten = &card
			s.Phase = Reinserting
			return
		}
	}
	s.explode(card)
}

func (s *State) defuse(a Action) error {
	if a.Type != UseDefuse {
		return ErrPhase
	}
	if a.Seat != s.Turn {
		return ErrSeat
	}
	cards, err := s.cards(a.Seat, a.CardIDs)
	if err != nil || len(cards) != 1 || cards[0].Kind != Defuse {
		return ErrCard
	}
	if a.Position < 0 || a.Position > len(s.Deck) {
		return ErrChoice
	}
	s.Discard = append(s.Discard, s.take(a.Seat, cards[0].ID))
	s.DiscardBy = a.Seat
	s.Deck = append(s.Deck, Card{})
	copy(s.Deck[a.Position+1:], s.Deck[a.Position:])
	s.Deck[a.Position] = *s.Kitten
	s.Kitten = nil
	s.clearFuture()
	s.endTurn()
	return nil
}
