package engine

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestEliminatedSpectatorSeesOnlyLiveTopCards(t *testing.T) {
	s := testGame([]Kind{Skip}, []Kind{Attack}, []Kind{TacoCat})
	s.Alive[0] = false
	s.Deck = []Card{{ID: 1, Kind: ExplodingKitten}, {ID: 2, Kind: Skip}, {ID: 3, Kind: Attack}, {ID: 4, Kind: Nope}}
	view := Observe(s, 0)
	if !reflect.DeepEqual(view.SpectatorTop, s.Deck[:3]) || len(view.Hand) != 0 || len(view.Future) != 0 {
		t.Fatalf("spectator view: %+v", view)
	}
	view.SpectatorTop[0].Kind = Nope
	if s.Deck[0].Kind != ExplodingKitten {
		t.Fatal("spectator view aliases deck")
	}
	for _, seat := range []int{-1, 1, 2, 3} {
		if got := Observe(s, seat).SpectatorTop; len(got) != 0 {
			t.Fatalf("seat %d received deck: %v", seat, got)
		}
	}
	s.Deck = s.Deck[2:]
	if got := Observe(s, 0).SpectatorTop; !reflect.DeepEqual(got, s.Deck) {
		t.Fatalf("short changed deck: %v", got)
	}
	s.Deck = nil
	if got := Observe(s, 0).SpectatorTop; len(got) != 0 {
		t.Fatalf("empty deck: %v", got)
	}
	s.Deck = []Card{{ID: 5, Kind: Skip}}
	s.Phase = Finished
	if got := Observe(s, 0).SpectatorTop; len(got) != 0 {
		t.Fatalf("finished match exposes deck: %v", got)
	}
}

func TestReverseDLCDeckAndTurnRules(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s, err := NewGameWithReverse(3, 42, enabled)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, card := range s.Deck {
			if card.Kind == Reverse {
				count++
			}
		}
		for _, hand := range s.Hands {
			for _, card := range hand {
				if card.Kind == Reverse {
					count++
				}
			}
		}
		want := 0
		if enabled {
			want = 4
		}
		if count != want || s.Direction != 1 {
			t.Fatalf("enabled=%t count=%d direction=%d", enabled, count, s.Direction)
		}
	}
	for _, tc := range []struct {
		name                            string
		seats, debt, wantTurn, wantDebt int
	}{
		{"normal", 3, 1, 2, 1}, {"attack debt", 3, 2, 0, 1}, {"two players", 2, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hands := make([][]Kind, tc.seats)
			hands[0] = []Kind{Reverse}
			s := testGame(hands...)
			s.TurnsRemaining = tc.debt
			s.UnderAttack = tc.debt > 1
			deck := append([]Card(nil), s.Deck...)
			play(t, s, 0, []int{s.Hands[0][0].ID}, -1, "")
			settle(t, s)
			if s.Direction != -1 || s.Turn != tc.wantTurn || s.TurnsRemaining != tc.wantDebt || !reflect.DeepEqual(deck, s.Deck) {
				t.Fatalf("bad reverse: %+v", s)
			}
		})
	}
}

func TestReverseNopeAndEliminatedSeats(t *testing.T) {
	s := testGame([]Kind{Reverse}, []Kind{Nope}, []Kind{TacoCat}, []Kind{TacoCat})
	s.Alive[3] = false
	play(t, s, 0, []int{s.Hands[0][0].ID}, -1, "")
	mustApply(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{s.Hands[1][0].ID}})
	settle(t, s)
	if s.Direction == -1 || s.Turn != 0 || s.TurnsRemaining != 1 {
		t.Fatal("negated reverse changed turn")
	}
	s.Hands[0] = []Card{{ID: 999, Kind: Reverse}}
	play(t, s, 0, []int{999}, -1, "")
	settle(t, s)
	if s.Direction != -1 || s.Turn != 2 {
		t.Fatalf("did not skip eliminated seat: %+v", s)
	}
}

func testGame(hands ...[]Kind) *State {
	s := &State{
		Hands: make([][]Card, len(hands)), Alive: make([]bool, len(hands)),
		Future: make([][]Card, len(hands)), Phase: Playing, TurnsRemaining: 1, Winner: -1,
	}
	id := 100
	for seat, kinds := range hands {
		s.Alive[seat] = true
		for _, kind := range kinds {
			s.Hands[seat] = append(s.Hands[seat], Card{ID: id, Kind: kind})
			id++
		}
	}
	s.Deck = []Card{{ID: 0, Kind: TacoCat}, {ID: 1, Kind: ExplodingKitten}, {ID: 2, Kind: Shuffle}}
	return s
}

func mustApply(t *testing.T, s *State, a Action) {
	t.Helper()
	if err := Apply(s, a); err != nil {
		t.Fatalf("Apply(%+v): %v", a, err)
	}
}

func play(t *testing.T, s *State, seat int, ids []int, target int, requested Kind) {
	t.Helper()
	mustApply(t, s, Action{Type: Play, Seat: seat, CardIDs: ids, Target: target, Requested: requested})
}

func settle(t *testing.T, s *State) {
	t.Helper()
	for seat, alive := range s.Alive {
		if alive && s.Phase == Responding && !s.Pending.Passed[seat] {
			mustApply(t, s, Action{Type: Pass, Seat: seat})
		}
	}
}

func snapshot(t *testing.T, s *State) []byte {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertRejected(t *testing.T, s *State, a Action, want error) {
	t.Helper()
	before := snapshot(t, s)
	if err := Apply(s, a); !errors.Is(err, want) {
		t.Fatalf("Apply(%+v) = %v, want %v", a, err, want)
	}
	if string(before) != string(snapshot(t, s)) {
		t.Fatal("rejected action mutated state")
	}
}

func assertConserved(t *testing.T, s *State) {
	t.Helper()
	cards := append([]Card(nil), s.Deck...)
	cards = append(cards, s.Discard...)
	cards = append(cards, s.Removed...)
	for _, hand := range s.Hands {
		cards = append(cards, hand...)
	}
	if s.Kitten != nil {
		cards = append(cards, *s.Kitten)
	}
	if len(cards) != 56 {
		t.Fatalf("lost cards: %d", len(cards))
	}
	seen := map[int]bool{}
	for _, card := range cards {
		if seen[card.ID] {
			t.Fatalf("duplicate card: %+v", card)
		}
		seen[card.ID] = true
	}
}

func TestSetupCurrentOriginalEdition(t *testing.T) {
	for seats := 2; seats <= 5; seats++ {
		for seed := int64(0); seed < 10; seed++ {
			s, err := NewGame(seats, seed)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := NewGame(seats, seed)
			if !reflect.DeepEqual(s, again) {
				t.Fatal("same seed produced different setup")
			}
			assertConserved(t, s)
			for _, hand := range s.Hands {
				defuses := 0
				for _, card := range hand {
					if card.Kind == ExplodingKitten {
						t.Fatal("kitten dealt into starting hand")
					}
					if card.Kind == Defuse {
						defuses++
					}
				}
				if len(hand) != 8 || defuses != 1 {
					t.Fatalf("invalid starting hand: %+v", hand)
				}
			}
			kittens, defuses := 0, 0
			for _, card := range s.Deck {
				if card.Kind == ExplodingKitten {
					kittens++
				}
				if card.Kind == Defuse {
					defuses++
				}
			}
			if kittens != seats-1 || defuses != min(2, 6-seats) {
				t.Fatalf("wrong deck composition: kittens=%d defuses=%d", kittens, defuses)
			}
		}
	}
	for _, seats := range []int{0, 1, 6} {
		if _, err := NewGame(seats, 0); err == nil {
			t.Fatal("invalid seat count accepted")
		}
	}
}

func TestAttackStackingSkipAndPartialDebt(t *testing.T) {
	s := testGame([]Kind{Attack}, []Kind{Skip, Attack}, []Kind{Attack})
	play(t, s, 0, []int{100}, 0, "")
	assertRejected(t, s, Action{Type: Draw, Seat: 0}, ErrPhase)
	settle(t, s)
	if s.Turn != 1 || s.TurnsRemaining != 2 || !s.UnderAttack || len(s.Deck) != 3 {
		t.Fatal("attack must transfer two turns without drawing")
	}
	play(t, s, 1, []int{101}, 0, "")
	settle(t, s)
	if s.Turn != 1 || s.TurnsRemaining != 1 {
		t.Fatal("skip must consume only one attacked turn")
	}
	play(t, s, 1, []int{102}, 0, "")
	settle(t, s)
	if s.Turn != 2 || s.TurnsRemaining != 3 {
		t.Fatal("attack must transfer current remaining turn plus two")
	}
	play(t, s, 2, []int{103}, 0, "")
	settle(t, s)
	if s.Turn != 0 || s.TurnsRemaining != 5 {
		t.Fatal("repeated attacks must stack")
	}
	mustApply(t, s, Action{Type: Draw, Seat: 0})
	if s.Turn != 0 || s.TurnsRemaining != 4 {
		t.Fatal("drawing must consume one attacked turn")
	}
}

func TestNopeChainsAndResponseWindow(t *testing.T) {
	for nopes := 1; nopes <= 2; nopes++ {
		s := testGame([]Kind{Attack, Nope}, []Kind{Nope})
		play(t, s, 0, []int{100}, 0, "")
		mustApply(t, s, Action{Type: Pass, Seat: 0})
		mustApply(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{102}})
		if s.Pending.Passed[0] {
			t.Fatal("Nope must reset prior passes")
		}
		if nopes == 2 {
			mustApply(t, s, Action{Type: PlayNope, Seat: 0, CardIDs: []int{101}})
		}
		settle(t, s)
		if s.Pending != nil || len(s.Discard) != nopes+1 {
			t.Fatal("countered cards must stay discarded")
		}
		if nopes == 1 && (s.Turn != 0 || s.TurnsRemaining != 1) {
			t.Fatal("odd Nope count must cancel attack")
		}
		if nopes == 2 && (s.Turn != 1 || s.TurnsRemaining != 2) {
			t.Fatal("even Nope count must restore attack")
		}
	}
}

func TestFavorTargetChoosesAndCannotBeNopedAfterResolution(t *testing.T) {
	s := testGame([]Kind{Favor, Nope}, []Kind{Defuse, TacoCat})
	play(t, s, 0, []int{100}, 1, "")
	settle(t, s)
	if s.Phase != Giving || s.Turn != 0 {
		t.Fatal("favor must wait for the target without ending turn")
	}
	assertRejected(t, s, Action{Type: Give, Seat: 0, CardIDs: []int{101}}, ErrSeat)
	assertRejected(t, s, Action{Type: PlayNope, Seat: 0, CardIDs: []int{101}}, ErrPhase)
	mustApply(t, s, Action{Type: Give, Seat: 1, CardIDs: []int{103}})
	if s.Phase != Playing || s.Pending != nil || s.Hands[0][1].Kind != TacoCat ||
		s.Hands[1][0].Kind != Defuse {
		t.Fatal("target's chosen card was not transferred")
	}
}

func TestPairsTriplesAndNopeOnCombos(t *testing.T) {
	t.Run("functional pair steals random", func(t *testing.T) {
		s := testGame([]Kind{Skip, Skip}, []Kind{Defuse, TacoCat})
		play(t, s, 0, []int{100, 101}, 1, "")
		settle(t, s)
		if len(s.Hands[0]) != 1 || len(s.Hands[1]) != 1 || s.Turn != 0 {
			t.Fatal("pair must steal rather than apply card text")
		}
	})
	for _, requested := range []Kind{Defuse, Attack} {
		s := testGame([]Kind{TacoCat, TacoCat, TacoCat}, []Kind{Defuse, Skip})
		play(t, s, 0, []int{100, 101, 102}, 1, requested)
		settle(t, s)
		if requested == Defuse && (len(s.Hands[0]) != 1 || s.Hands[0][0].Kind != Defuse) {
			t.Fatal("three-card combo must take named kind")
		}
		if requested == Attack && len(s.Hands[0]) != 0 {
			t.Fatal("missing named kind must not steal a different card")
		}
	}
	t.Run("Nope cancels pair", func(t *testing.T) {
		s := testGame([]Kind{Defuse, Defuse}, []Kind{Nope, Skip})
		play(t, s, 0, []int{100, 101}, 1, "")
		mustApply(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{102}})
		settle(t, s)
		if len(s.Hands[0]) != 0 || len(s.Hands[1]) != 1 {
			t.Fatal("Noped pair must not steal")
		}
	})
	t.Run("target spends last card countering Nope", func(t *testing.T) {
		for _, kinds := range [][]Kind{{TacoCat, TacoCat, Nope}, {Favor, Nope}} {
			s := testGame(kinds, []Kind{Nope})
			ids := []int{100}
			if len(kinds) == 3 {
				ids = append(ids, 101)
			}
			last := 100 + len(kinds) - 1
			play(t, s, 0, ids, 1, "")
			mustApply(t, s, Action{Type: PlayNope, Seat: 0, CardIDs: []int{last}})
			mustApply(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{last + 1}})
			settle(t, s)
			if s.Phase != Playing || s.Pending != nil {
				t.Fatal("empty target must not stall or panic")
			}
		}
	})
}

func TestDefusePositionsAndAttackDebt(t *testing.T) {
	for position := 0; position <= 2; position++ {
		s := testGame([]Kind{Defuse}, []Kind{Nope})
		s.Deck[0].Kind = ExplodingKitten
		s.TurnsRemaining = 2
		s.UnderAttack = true
		mustApply(t, s, Action{Type: Draw, Seat: 0})
		if s.Phase != Reinserting || s.Kitten == nil {
			t.Fatal("drawing kitten with defuse must pause for placement")
		}
		assertRejected(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{101}}, ErrPhase)
		assertRejected(t, s, Action{Type: UseDefuse, Seat: 0, CardIDs: []int{100}, Position: 3}, ErrChoice)
		mustApply(t, s, Action{Type: UseDefuse, Seat: 0, CardIDs: []int{100}, Position: position})
		if s.Deck[position].ID != 0 || s.Kitten != nil || s.Turn != 0 ||
			s.TurnsRemaining != 1 || s.Phase != Playing || s.Discard[0].Kind != Defuse {
			t.Fatal("defuse must insert into chosen gap and consume only one turn")
		}
	}
}

func TestExplosionEliminationAndWinner(t *testing.T) {
	s := testGame([]Kind{Nope, Skip}, []Kind{Defuse}, []Kind{TacoCat})
	s.Deck[0].Kind = ExplodingKitten
	mustApply(t, s, Action{Type: Draw, Seat: 0})
	if s.Alive[0] || len(s.Hands[0]) != 0 || s.Turn != 1 || s.Phase != Playing {
		t.Fatal("exploding player must be eliminated")
	}
	if len(s.Removed) != 2 || len(s.Discard) != 1 {
		t.Fatal("eliminated hand stays hidden, kitten becomes public")
	}
	mustApply(t, s, Action{Type: Draw, Seat: 1})
	mustApply(t, s, Action{Type: UseDefuse, Seat: 1, CardIDs: []int{102}, Position: 0})
	mustApply(t, s, Action{Type: Draw, Seat: 2})
	if s.Winner != 1 || s.Phase != Finished || s.Alive[2] {
		t.Fatal("last survivor must win")
	}
	assertRejected(t, s, Action{Type: Draw, Seat: 1}, ErrPhase)
}

func TestFuturePrivacyInvalidationAndSnapshotIsolation(t *testing.T) {
	s := testGame([]Kind{SeeTheFuture, Shuffle}, []Kind{TacoCat})
	play(t, s, 0, []int{100}, 0, "")
	settle(t, s)
	view := Observe(s, 0)
	if !reflect.DeepEqual(view.Future, s.Deck) {
		t.Fatal("future must reveal top three in order")
	}
	for _, seat := range []int{1, -1, 5} {
		o := Observe(s, seat)
		if len(o.Future) != 0 || (seat != 1 && len(o.Hand) != 0) {
			t.Fatal("private cards leaked")
		}
	}
	view.Future[0].Kind = Defuse
	view.Hand[0].Kind = Defuse
	view.Alive[0] = false
	if s.Future[0][0].Kind == Defuse || s.Hands[0][0].Kind == Defuse || !s.Alive[0] {
		t.Fatal("view aliases private game state")
	}
	play(t, s, 0, []int{101}, 0, "")
	pending := Observe(s, 1)
	pending.Pending.Action.CardIDs[0] = -1
	pending.Pending.Passed[0] = true
	if s.Pending.Action.CardIDs[0] < 0 || s.Pending.Passed[0] {
		t.Fatal("pending view aliases state")
	}
	settle(t, s)
	if len(Observe(s, 0).Future) != 0 {
		t.Fatal("shuffle must invalidate known deck order")
	}
}

func TestRejectedActionsAreAtomic(t *testing.T) {
	s := testGame([]Kind{TacoCat, Skip, Nope}, []Kind{Defuse})
	for _, tc := range []struct {
		action Action
		err    error
	}{
		{Action{Type: Draw, Seat: 1}, ErrSeat},
		{Action{Type: Play, Seat: 0, CardIDs: []int{100}}, ErrCard},
		{Action{Type: Play, Seat: 0, CardIDs: []int{102}}, ErrCard},
		{Action{Type: Play, Seat: 0, CardIDs: []int{101, 101}}, ErrCard},
		{Action{Type: Play, Seat: 0, CardIDs: []int{100, 101}}, ErrCard},
		{Action{Type: Play, Seat: 0, CardIDs: []int{999}}, ErrCard},
		{Action{Type: Draw, Seat: -1}, ErrSeat},
	} {
		assertRejected(t, s, tc.action, tc.err)
	}
	play(t, s, 0, []int{101}, 0, "")
	mustApply(t, s, Action{Type: Pass, Seat: 0})
	assertRejected(t, s, Action{Type: Pass, Seat: 0}, ErrChoice)
	assertRejected(t, s, Action{Type: Draw, Seat: 1}, ErrPhase)
}

func TestJSONRestoreKeepsRandomEffectsDeterministic(t *testing.T) {
	for _, kind := range []Kind{Shuffle, TacoCat} {
		s := testGame([]Kind{kind, kind}, []Kind{Defuse, Skip, Favor})
		ids := []int{100}
		if kind == TacoCat {
			ids = append(ids, 101)
		}
		play(t, s, 0, ids, 1, "")
		var restored State
		if err := json.Unmarshal(snapshot(t, s), &restored); err != nil {
			t.Fatal(err)
		}
		settle(t, s)
		settle(t, &restored)
		if string(snapshot(t, s)) != string(snapshot(t, &restored)) {
			t.Fatal("restored random effect diverged")
		}
	}
}

func TestNopeCancelsInformationAndFavorEffects(t *testing.T) {
	for _, kind := range []Kind{SeeTheFuture, Shuffle, Skip, Favor} {
		s := testGame([]Kind{kind}, []Kind{Nope, Defuse})
		deck := append([]Card(nil), s.Deck...)
		play(t, s, 0, []int{100}, 1, "")
		mustApply(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{101}})
		settle(t, s)
		if s.Phase != Playing || s.Turn != 0 || !reflect.DeepEqual(s.Deck, deck) ||
			len(Observe(s, 0).Future) != 0 || len(s.Hands[0]) != 0 {
			t.Fatalf("Noped %s still took effect", kind)
		}
	}
}

func TestDeadSeatsAreSkippedAndCannotRespond(t *testing.T) {
	s := testGame([]Kind{Skip}, []Kind{Nope}, []Kind{TacoCat})
	s.Alive[1] = false
	play(t, s, 0, []int{100}, 0, "")
	assertRejected(t, s, Action{Type: PlayNope, Seat: 1, CardIDs: []int{101}}, ErrSeat)
	settle(t, s)
	if s.Turn != 2 || s.Phase != Playing {
		t.Fatal("dead players must not block responses or receive turns")
	}
}

func TestFavorAndComboValidation(t *testing.T) {
	s := testGame([]Kind{Favor, Skip, Skip, Skip}, nil, []Kind{Defuse})
	for _, target := range []int{-1, 0, 1, 3} {
		assertRejected(t, s, Action{Type: Play, Seat: 0, CardIDs: []int{100}, Target: target}, ErrTarget)
	}
	assertRejected(t, s, Action{
		Type: Play, Seat: 0, CardIDs: []int{101, 102, 103}, Target: 2, Requested: "unknown",
	}, ErrChoice)
}

func TestFutureWithShortDeckAndDrawInvalidation(t *testing.T) {
	s := testGame([]Kind{SeeTheFuture}, []Kind{Skip})
	s.Deck = s.Deck[:1]
	play(t, s, 0, []int{100}, 0, "")
	settle(t, s)
	if len(Observe(s, 0).Future) != 1 {
		t.Fatal("peek must work with fewer than three cards")
	}
	mustApply(t, s, Action{Type: Draw, Seat: 0})
	if len(Observe(s, 0).Future) != 0 {
		t.Fatal("draw must invalidate old future information")
	}
}

func TestDrawOnlyGamesAlwaysEndAndConserveCards(t *testing.T) {
	for seats := 2; seats <= 5; seats++ {
		for seed := int64(0); seed < 30; seed++ {
			s, _ := NewGame(seats, seed)
			for step := 0; s.Phase != Finished && step < 200; step++ {
				assertConserved(t, s)
				a := Action{Type: Draw, Seat: s.Turn}
				if s.Phase == Reinserting {
					a.Type = UseDefuse
					a.Position = len(s.Deck)
					for _, card := range s.Hands[s.Turn] {
						if card.Kind == Defuse {
							a.CardIDs = []int{card.ID}
							break
						}

					}
				}
				mustApply(t, s, a)
			}
			if s.Phase != Finished || s.Winner < 0 {
				t.Fatal("draw-only game did not finish")
			}
			assertConserved(t, s)
		}
	}
}
