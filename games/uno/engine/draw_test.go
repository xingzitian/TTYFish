package engine

import "testing"

// 场景一：手里有能出的牌（红 5，当前红），我故意不出，摸一张。
// 摸到红 7（能出）之后，引擎只允许出"摸到的那张"，手里原来的红 5 被禁掉了。
func TestDrawThenPlayOtherCards(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{
			{ID: 10, Color: Red, Kind: Number, Num: 5}, // 本来就能出
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 30, Color: Red, Kind: Number, Num: 7}} // 摸到红 7

	mustApply(t, s, DrawCard{Seat: 0})
	if s.DrawnID != 30 {
		t.Fatalf("摸到能出的牌应该给一次出牌机会，DrawnID=%d", s.DrawnID)
	}

	legal := LegalMoves(s, 0)
	var canPlayOld, canPlayNew, canPass bool
	for _, a := range legal {
		switch v := a.(type) {
		case PlayCard:
			if v.CardID == 10 {
				canPlayOld = true
			}
			if v.CardID == 30 {
				canPlayNew = true
			}
		case Pass:
			canPass = true
		}
	}
	if !canPlayOld {
		t.Errorf("摸牌之后，手里原有的红 5 应该还能出（现在被禁掉了）")
	}
	if !canPlayNew {
		t.Errorf("摸到的红 7 应该能出")
	}
	if !canPass {
		t.Errorf("摸牌之后应该可以选择跳过")
	}
}

// 场景二：手里没有能出的牌，摸一张，摸到的牌能出。
// 期望：既能出这张，也能选择跳过。
func TestDrawPlayableStillCanPass(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{
			{ID: 10, Color: Green, Kind: Number, Num: 5}, // 出不了
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 30, Color: Red, Kind: Number, Num: 7}}

	mustApply(t, s, DrawCard{Seat: 0})

	legal := LegalMoves(s, 0)
	var canPlay, canPass bool
	for _, a := range legal {
		switch v := a.(type) {
		case PlayCard:
			canPlay = v.CardID == 30
		case Pass:
			canPass = true
		}
	}
	if !canPlay {
		t.Errorf("摸到的红 7 应该能出")
	}
	if !canPass {
		t.Errorf("摸了能出的牌也应该能选择跳过")
	}

	// 真的跳过：回合应该交给下一家，牌留在手里。
	mustApply(t, s, Pass{Seat: 0})
	if s.Turn != 1 {
		t.Fatalf("跳过后应该轮到下一家，实际 %d", s.Turn)
	}
	if len(s.Hands[0]) != 2 {
		t.Fatalf("跳过后牌应该留在手里（2 张），实际 %d", len(s.Hands[0]))
	}
}

// 场景三：摸到的牌能出，但我想出另一张——这条要能通过 engine 的校验。
func TestPlayOtherCardAfterDraw(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Number, Num: 5}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 30, Color: Red, Kind: Number, Num: 7}}

	mustApply(t, s, DrawCard{Seat: 0})
	if err := Apply(s, PlayCard{Seat: 0, CardID: 10}); err != nil {
		t.Fatalf("摸过牌之后出原来的红 5 应该被允许，实际报错：%v", err)
	}
	if len(s.Hands[0]) != 1 {
		t.Fatalf("出掉一张后应该剩 1 张，实际 %d", len(s.Hands[0]))
	}
}
