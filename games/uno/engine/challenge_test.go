package engine

import "testing"

// 帮我把"+4 质疑"两种结果都验一遍：成立和不成立。
func TestChallengeSucceedsWhenTargetStillHasPrevColor(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{Challenge: true}, top, 0,
		[]Card{
			// A 手里除了 +4 还有一张红牌 —— 按规则他不该出 +4
			{ID: 10, Color: Red, Kind: Number, Num: 5},
			{ID: 11, Color: NoColor, Kind: WildDrawFour},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
		{ID: 102, Color: Blue, Kind: Number, Num: 3},
		{ID: 103, Color: Blue, Kind: Number, Num: 6},
		{ID: 104, Color: Blue, Kind: Number, Num: 7},
		{ID: 105, Color: Blue, Kind: Number, Num: 8},
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 11, Color: Blue})
	if s.Challenge == nil {
		t.Fatal("打 +4 之后应该等下一家决定")
	}
	if s.Challenge.By != 1 || s.Challenge.Target != 0 {
		t.Fatalf("质疑关系不对：%+v", *s.Challenge)
	}
	if s.Challenge.PrevColor != Red {
		t.Fatalf("PrevColor 应该是红，实际 %v", s.Challenge.PrevColor)
	}

	// B 质疑：A 手里还有红牌 → 质疑成立，A 摸 4
	mustApply(t, s, ResolveChallenge{Seat: 1, Challenge: true})
	if len(s.Hands[0]) != 1+4 {
		t.Fatalf("质疑成立时 A 应该摸 4 张（剩 1 张 + 4），实际 %d", len(s.Hands[0]))
	}
	if s.Turn != 1 {
		t.Fatalf("质疑成立后回合应该留给质疑者 B（座位 1），实际 %d", s.Turn)
	}
}

func TestChallengeFailsWhenTargetHasNoPrevColor(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{Challenge: true}, top, 0,
		[]Card{
			// A 手里没有红牌，只剩 +4 —— 合规
			{ID: 10, Color: Green, Kind: Number, Num: 5},
			{ID: 11, Color: NoColor, Kind: WildDrawFour},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
		{ID: 102, Color: Blue, Kind: Number, Num: 3},
		{ID: 103, Color: Blue, Kind: Number, Num: 6},
		{ID: 104, Color: Blue, Kind: Number, Num: 7},
		{ID: 105, Color: Blue, Kind: Number, Num: 8},
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 11, Color: Blue})
	mustApply(t, s, ResolveChallenge{Seat: 1, Challenge: true})

	if len(s.Hands[1]) != 1+6 {
		t.Fatalf("质疑失败时 B 应该摸 6 张（剩 1 + 6），实际 %d", len(s.Hands[1]))
	}
}
