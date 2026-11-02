package engine

import "testing"

// TestCatchUnoNeedsOneCard 抓包只能抓"手里还剩一张"的人。
//
// 挂起状态只在出牌剩一张时设置，但玩家之后可能又摸到牌（比如轮到他自己时
// 选择摸牌）。这时他还被挂着，别人再按抓包就不合规则了——
// 他手里已经不是一张牌，抓他等于凭空罚人。
func TestCatchUnoNeedsOneCard(t *testing.T) {
	rules := Rules{UnoPenalty: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: Red, Kind: Number, Num: 5},
			{ID: 11, Color: Blue, Kind: Number, Num: 1},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 100, Color: Blue, Kind: Number, Num: 7}}

	// A 没喊 UNO 出了一张，剩一张 → 挂起
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.UnoPending != 0 {
		t.Fatalf("漏喊 UNO 应当被挂起，实际 %d", s.UnoPending)
	}

	// 他自己又摸到一张：现在是两张牌，不再是"剩一张没喊"
	s.Hands[0] = append(s.Hands[0], Card{ID: 101, Color: Blue, Kind: Number, Num: 9})
	if len(s.Hands[0]) != 2 {
		t.Fatalf("构造失败，手牌数 %d", len(s.Hands[0]))
	}

	if err := Apply(s, CatchUno{Seat: 1, Target: 0}); err == nil {
		t.Fatalf("目标已经有两张牌，抓包应该失败；实际成功，手牌数 %d", len(s.Hands[0]))
	}
}
