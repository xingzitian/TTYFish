package engine

import "testing"

func FuzzApplyConservesCards(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{9, 9, 9, 255, 0, 5, 6, 8, 2, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		s, _ := NewGame(3, 42)
		for i, value := range data {
			if i >= 300 || s.Phase == Finished {
				break
			}
			seat := s.Turn
			a := Action{Type: Draw, Seat: seat}
			switch s.Phase {
			case Reinserting:
				for _, card := range s.Hands[seat] {
					if card.Kind == Defuse {
						a = Action{Type: UseDefuse, Seat: seat, CardIDs: []int{card.ID},
							Position: int(value) % (len(s.Deck) + 1)}
						break
					}
				}
			case Giving:
				seat = s.Pending.Action.Target
				index := int(value) % len(s.Hands[seat])
				a = Action{Type: Give, Seat: seat, CardIDs: []int{s.Hands[seat][index].ID}}
			case Responding:
				seat = int(value) % len(s.Hands)
				a = Action{Type: Pass, Seat: seat}
				if value%2 == 0 {
					for _, card := range s.Hands[seat] {
						if card.Kind == Nope {
							a = Action{Type: PlayNope, Seat: seat, CardIDs: []int{card.ID}}
							break
						}
					}
				}
			case Playing:
				if len(s.Hands[seat]) > 0 && value%3 != 0 {
					index := int(value) % len(s.Hands[seat])
					a = Action{Type: Play, Seat: seat, CardIDs: []int{s.Hands[seat][index].ID},
						Target: (seat + 1) % len(s.Hands)}
				}
			}
			before := string(snapshot(t, s))
			err := Apply(s, a)
			if err != nil && before != string(snapshot(t, s)) {
				t.Fatal("rejected action mutated state")
			}
			assertConserved(t, s)
		}
	})
}
