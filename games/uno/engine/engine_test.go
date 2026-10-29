package engine

import (
	"encoding/json"
	"reflect"
	"testing"
)

func mustApply(t *testing.T, s *State, a Action) {
	t.Helper()
	if err := Apply(s, a); err != nil {
		t.Fatalf("Apply(%T) 失败: %v", a, err)
	}
}

// testState 手工搭一个可控牌局，避免测试依赖洗牌结果。
func testState(rules Rules, top Card, turn int, hands ...[]Card) *State {
	s := &State{
		Rules:       rules,
		SeatCount:   len(hands),
		Hands:       hands,
		Discard:     []Card{top},
		Active:      top.Color,
		Turn:        turn,
		Dir:         1,
		DrawnID:     -1,
		Phase:       Playing,
		RoundWinner: -1,
		MatchWinner: -1,
		UnoPending:  -1,
		Scores:      make([]int, len(hands)),
		UnoCalled:   make([]bool, len(hands)),
	}
	return s
}

func TestDeckComposition(t *testing.T) {
	deck := NewDeck()
	if len(deck) != 108 {
		t.Fatalf("牌堆应当是 108 张，实际 %d", len(deck))
	}

	ids := make(map[int]bool, 108)
	perColor := map[Color]int{}
	kinds := map[string]int{}
	for _, c := range deck {
		if ids[c.ID] {
			t.Fatalf("牌 ID 重复: %d", c.ID)
		}
		ids[c.ID] = true
		perColor[c.Color]++
		key := c.Color.String() + "/" + c.String()
		kinds[key]++
	}

	for _, c := range []Color{Red, Yellow, Green, Blue} {
		if perColor[c] != 25 {
			t.Fatalf("%s 色应有 25 张，实际 %d", c, perColor[c])
		}
	}
	if perColor[NoColor] != 8 {
		t.Fatalf("无色牌应有 8 张，实际 %d", perColor[NoColor])
	}
	if kinds["红/红0"] != 1 {
		t.Fatalf("每色只有一张 0，红0 实际 %d 张", kinds["红/红0"])
	}
	if kinds["蓝/蓝7"] != 2 {
		t.Fatalf("每色有两张 7，蓝7 实际 %d 张", kinds["蓝/蓝7"])
	}
	if kinds["绿/绿禁止"] != 2 || kinds["绿/绿加二"] != 2 || kinds["绿/绿反转"] != 2 {
		t.Fatalf("功能牌数量不对: %v", kinds)
	}
	if kinds["无色/变色"] != 4 || kinds["无色/加四变色"] != 4 {
		t.Fatalf("黑牌数量不对: %v", kinds)
	}
}

func TestDealIsDeterministic(t *testing.T) {
	a := NewGame(4, 2024, DefaultRulesForTest())
	b := NewGame(4, 2024, DefaultRulesForTest())
	if !reflect.DeepEqual(a, b) {
		t.Fatal("同一 seed 应当发出完全相同的牌")
	}
	c := NewGame(4, 2025, DefaultRulesForTest())
	if reflect.DeepEqual(a, c) {
		t.Fatal("不同 seed 却发出了一样的牌")
	}
	for _, s := range []*State{a, b, c} {
		if top := s.TopDiscard(); top.Kind == WildDrawFour {
			t.Fatal("首张不能是加四变色牌")
		}
		for i, h := range s.Hands {
			if len(h) != 7 {
				t.Fatalf("座位 %d 应该发 7 张，实际 %d", i, len(h))
			}
		}
	}
}

func DefaultRulesForTest() Rules { return Rules{} }

func TestPlayable(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 5}
	s := testState(Rules{}, top, 0, []Card{})
	cases := []struct {
		name string
		card Card
		want bool
	}{
		{"同色数字", Card{Color: Red, Kind: Number, Num: 3}, true},
		{"同数字异色", Card{Color: Blue, Kind: Number, Num: 5}, true},
		{"异色异数字", Card{Color: Blue, Kind: Number, Num: 3}, false},
		{"同色功能牌", Card{Color: Red, Kind: Skip}, true},
		{"异色功能牌", Card{Color: Green, Kind: Skip}, false},
		{"变色牌", Card{Color: NoColor, Kind: Wild}, true},
		{"加四变色", Card{Color: NoColor, Kind: WildDrawFour}, true},
	}
	for _, tc := range cases {
		if got := s.Playable(tc.card); got != tc.want {
			t.Errorf("%s: 期望 %v，实际 %v", tc.name, tc.want, got)
		}
	}
}

func TestSameKindOnTopOfKind(t *testing.T) {
	// 举个禁牌压禁牌：颜色不同也应该能出。
	top := Card{ID: 1, Color: Red, Kind: Skip}
	s := testState(Rules{}, top, 0, []Card{})
	if !s.Playable(Card{Color: Blue, Kind: Skip}) {
		t.Fatal("异色禁牌应当可以压禁牌")
	}
}

func TestTurnAdvances(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	hands := [][]Card{
		{{ID: 10, Color: Red, Kind: Number, Num: 9}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		{{ID: 20, Color: Green, Kind: Number, Num: 4}},
		{{ID: 30, Color: Yellow, Kind: Number, Num: 6}},
	}
	s := testState(Rules{}, top, 0, hands...)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Turn != 1 {
		t.Fatalf("应当轮到座位 1，实际 %d", s.Turn)
	}
	if s.Active != Red {
		t.Fatalf("有效颜色应当保持红色，实际 %v", s.Active)
	}
}

func TestWrongSeatRejected(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Number, Num: 9}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	if err := Apply(s, PlayCard{Seat: 1, CardID: 20}); err != ErrTurn {
		t.Fatalf("非当前玩家出牌应当被拒绝，实际 %v", err)
	}
	if len(s.Hands[1]) != 1 {
		t.Fatal("被拒绝的动作不能改变状态")
	}
}

func TestIllegalCardRejected(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Blue, Kind: Number, Num: 9}, {ID: 11, Color: Blue, Kind: Number, Num: 2}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	if err := Apply(s, PlayCard{Seat: 0, CardID: 10}); err != ErrIllegal {
		t.Fatalf("出不了的牌应当被拒绝，实际 %v", err)
	}
}

func TestSkip(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Skip}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
		[]Card{{ID: 30, Color: Yellow, Kind: Number, Num: 6}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Turn != 2 {
		t.Fatalf("禁止牌应当跳过座位 1，实际轮到 %d", s.Turn)
	}
}

func TestReverseTwoPlayersActsAsSkip(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Reverse}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Turn != 0 {
		t.Fatalf("两人局反转应当回到自己，实际轮到 %d", s.Turn)
	}
}

func TestReverseThreePlayers(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Reverse}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
		[]Card{{ID: 30, Color: Yellow, Kind: Number, Num: 6}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Dir != -1 {
		t.Fatalf("方向应当反转为 -1，实际 %d", s.Dir)
	}
	if s.Turn != 2 {
		t.Fatalf("反转后应当轮到座位 2，实际 %d", s.Turn)
	}
}

func TestDrawTwoNoStack(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: DrawTwo}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
		[]Card{{ID: 30, Color: Yellow, Kind: Number, Num: 6}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
	}
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if len(s.Hands[1]) != 3 {
		t.Fatalf("座位 1 应当摸到 3 张，实际 %d", len(s.Hands[1]))
	}
	if s.Turn != 2 {
		t.Fatalf("座位 1 应当被跳过，实际轮到 %d", s.Turn)
	}
}

func TestStackingChain(t *testing.T) {
	rules := Rules{Stacking: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: DrawTwo}, {ID: 11, Color: Blue, Kind: Number, Num: 1}},
		[]Card{{ID: 20, Color: Green, Kind: DrawTwo}, {ID: 21, Color: Blue, Kind: Number, Num: 2}},
		[]Card{{ID: 30, Color: Yellow, Kind: Number, Num: 6}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
		{ID: 102, Color: Blue, Kind: Number, Num: 3},
		{ID: 103, Color: Blue, Kind: Number, Num: 4},
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Pending != 2 || s.Turn != 1 {
		t.Fatalf("叠加后应当 Pending=2 轮到 1，实际 Pending=%d Turn=%d", s.Pending, s.Turn)
	}

	// 座位 1 拿加二顶回去，罚牌涨到 4。
	mustApply(t, s, PlayCard{Seat: 1, CardID: 20})
	if s.Pending != 4 || s.Turn != 2 {
		t.Fatalf("再叠一次应当 Pending=4 轮到 2，实际 Pending=%d Turn=%d", s.Pending, s.Turn)
	}

	// 座位 2 顶不动，只能吃下 4 张。
	mustApply(t, s, DrawCard{Seat: 2})
	if s.Pending != 0 {
		t.Fatalf("吃下罚牌后 Pending 应当清零，实际 %d", s.Pending)
	}
	if len(s.Hands[2]) != 1+4 {
		t.Fatalf("座位 2 应当摸 4 张变成 5 张，实际 %d", len(s.Hands[2]))
	}
}

func TestDrawAndPlayDrawnCard(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Blue, Kind: Number, Num: 9}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 100, Color: Red, Kind: Number, Num: 7}}

	// 摸到红色 7，可以立刻出，所以回合不能推进。
	mustApply(t, s, DrawCard{Seat: 0})
	if s.Turn != 0 || s.DrawnID != 100 {
		t.Fatalf("摸到能出的牌应当留在自己回合，实际 Turn=%d DrawnID=%d", s.Turn, s.DrawnID)
	}

	// 此时只能出摸到的那张。
	if err := Apply(s, PlayCard{Seat: 0, CardID: 10}); err != ErrIllegal {
		t.Fatalf("摸牌后只能出摸到的那张，实际 %v", err)
	}
	mustApply(t, s, PlayCard{Seat: 0, CardID: 100})
	if s.Turn != 1 || s.DrawnID != -1 {
		t.Fatalf("出完牌应当轮到 1 且清空 DrawnID，实际 Turn=%d DrawnID=%d", s.Turn, s.DrawnID)
	}
}

func TestDrawThenPass(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Blue, Kind: Number, Num: 9}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 100, Color: Red, Kind: Number, Num: 7}}
	mustApply(t, s, DrawCard{Seat: 0})
	mustApply(t, s, Pass{Seat: 0})
	if s.Turn != 1 || s.DrawnID != -1 {
		t.Fatalf("跳过之后应当轮到 1，实际 Turn=%d", s.Turn)
	}
	if len(s.Hands[0]) != 2 {
		t.Fatalf("摸到的牌应当留在手里，实际 %d 张", len(s.Hands[0]))
	}
}

func TestStrictDrawEndsTurn(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{StrictDraw: true}, top, 0,
		[]Card{{ID: 10, Color: Blue, Kind: Number, Num: 9}},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{{ID: 100, Color: Red, Kind: Number, Num: 7}}
	mustApply(t, s, DrawCard{Seat: 0})
	if s.Turn != 1 || s.DrawnID != -1 {
		t.Fatalf("严格摸牌规则下摸完就结束回合，实际 Turn=%d", s.Turn)
	}
}

func TestScoring(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Number, Num: 5}},
		[]Card{
			{ID: 20, Color: Blue, Kind: Number, Num: 7},
			{ID: 21, Color: NoColor, Kind: WildDrawFour},
		},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Phase != RoundOver {
		t.Fatalf("出完牌应当结束本局，实际阶段 %v", s.Phase)
	}
	if s.RoundWinner != 0 {
		t.Fatalf("赢家应当是座位 0，实际 %d", s.RoundWinner)
	}
	// 7 + 50 = 57
	if s.Scores[0] != 57 {
		t.Fatalf("应当得 57 分，实际 %d", s.Scores[0])
	}
	if err := Apply(s, DrawCard{Seat: 0}); err != ErrPhase {
		t.Fatalf("本局结束后不能再操作，实际 %v", err)
	}
}

func TestMatchTo(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{MatchTo: 7}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Number, Num: 5}},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 7}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Phase != MatchOver || s.MatchWinner != 0 {
		t.Fatalf("7 分达到 7 分目标应当赢下整场，实际 Phase=%v Winner=%d", s.Phase, s.MatchWinner)
	}
}

func TestNextRoundKeepsScores(t *testing.T) {
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(Rules{MatchTo: 500}, top, 0,
		[]Card{{ID: 10, Color: Red, Kind: Number, Num: 5}},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 7}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.Phase != RoundOver {
		t.Fatalf("应当是单局结束，实际 %v", s.Phase)
	}
	before := s.Scores[0]
	if err := Apply(s, CallUno{Seat: 0}); err == nil {
		t.Fatal("本局结束后不应该还能喊 UNO")
	}
	if err := s.NextRound(); err != nil {
		t.Fatalf("开下一局失败: %v", err)
	}
	if s.Phase != Playing {
		t.Fatalf("下一局应当回到 Playing，实际 %v", s.Phase)
	}
	if s.Scores[0] != before {
		t.Fatalf("分数应当保留：%d != %d", s.Scores[0], before)
	}
	if len(s.Hands[0]) != 7 {
		t.Fatalf("下一局应当重新发 7 张，实际 %d", len(s.Hands[0]))
	}
}

func TestUnoPenalty(t *testing.T) {
	rules := Rules{UnoPenalty: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: Red, Kind: Number, Num: 5},
			{ID: 11, Color: Blue, Kind: Number, Num: 1},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
	}

	// 没喊 UNO 就出一张，剩一张 → 挂起。
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.UnoPending != 0 {
		t.Fatalf("漏喊 UNO 应当被挂起，实际 %d", s.UnoPending)
	}

	// 对手抓人，罚摸两张。
	mustApply(t, s, CatchUno{Seat: 1, Target: 0})
	if s.UnoPending != -1 {
		t.Fatalf("抓完之后应当清空挂起状态，实际 %d", s.UnoPending)
	}
	if len(s.Hands[0]) != 3 {
		t.Fatalf("被罚之后应当有 3 张，实际 %d", len(s.Hands[0]))
	}
}

func TestCallUnoSavesYou(t *testing.T) {
	rules := Rules{UnoPenalty: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: Red, Kind: Number, Num: 5},
			{ID: 11, Color: Blue, Kind: Number, Num: 1},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	// 出牌前先喊，出完就不算漏喊。
	mustApply(t, s, CallUno{Seat: 0})
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10})
	if s.UnoPending != -1 {
		t.Fatalf("提前喊过 UNO 不该被挂起，实际 %d", s.UnoPending)
	}
}

// TestUnoDeclaredWithPlay 验证"出牌同时喊 UNO"这条正常路径：
// 机器人必须走这条路，否则一旦开启罚牌规则就永远赢不了。
func TestUnoDeclaredWithPlay(t *testing.T) {
	rules := Rules{UnoPenalty: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: Red, Kind: Number, Num: 5},
			{ID: 11, Color: Blue, Kind: Number, Num: 1},
		},
		[]Card{{ID: 20, Color: Green, Kind: Number, Num: 4}},
	)
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10, Uno: true})
	if s.UnoPending != -1 {
		t.Fatalf("出牌时喊过 UNO 就不该挂起，实际 %d", s.UnoPending)
	}
}

// FuzzApply 用随机动作序列砸引擎：允许返回错误，但不许 panic、
// 不许破坏牌数守恒、不许让回合越界。
// 跑法：go test -fuzz FuzzApply -fuzztime 30s ./internal/engine
func FuzzApply(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4}, int64(7))
	f.Add([]byte{0, 0, 0, 0}, int64(99))

	f.Fuzz(func(t *testing.T, actions []byte, seed int64) {
		if len(actions) > 300 {
			actions = actions[:300]
		}
		s := NewGame(4, seed, Rules{Stacking: true, UnoPenalty: true})
		for _, b := range actions {
			if s.Phase != Playing {
				break
			}
			seat := s.Turn
			var a Action
			switch b % 5 {
			case 0:
				if len(s.Hands[seat]) > 0 {
					a = PlayCard{Seat: seat, CardID: s.Hands[seat][0].ID, Color: Color(b % 4)}
				}
			case 1:
				a = DrawCard{Seat: seat}
			case 2:
				a = Pass{Seat: seat}
			case 3:
				a = CallUno{Seat: seat}
			case 4:
				a = CatchUno{Seat: seat, Target: int(b) % s.SeatCount}
			}
			if a != nil {
				_ = Apply(s, a) // 报错是预期行为，只要不改坏状态
			}
			if n := totalCards(s); n != 108 {
				t.Fatalf("牌数不守恒: %d", n)
			}
			if s.Turn < 0 || s.Turn >= s.SeatCount {
				t.Fatalf("回合越界: %d", s.Turn)
			}
		}
	})
}

func totalCards(s *State) int {
	n := len(s.Deck) + len(s.Discard)
	for _, h := range s.Hands {
		n += len(h)
	}
	return n
}

// TestGamesAlwaysTerminate 是最重要的回归测试：随机种子跑完整局，
// 牌数守恒、状态不越界、并且一定能在合理步数内结束。
func TestGamesAlwaysTerminate(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		for _, rules := range []Rules{
			{},
			{Stacking: true},
			{Stacking: true, StrictDraw: true, UnoPenalty: true},
			{MatchTo: 500},
		} {
			s := NewGame(4, seed, rules)
			steps := 0
			for s.Phase == Playing {
				if steps++; steps > 20000 {
					t.Fatalf("seed=%d rules=%+v 打不完，卡在 Turn=%d", seed, rules, s.Turn)
				}
				if n := totalCards(s); n != 108 {
					t.Fatalf("seed=%d 牌数不守恒: %d", seed, n)
				}
				if s.Turn < 0 || s.Turn >= s.SeatCount {
					t.Fatalf("seed=%d 回合越界: %d", seed, s.Turn)
				}
				a := BotMove(s, s.Turn)
				if a == nil {
					t.Fatalf("seed=%d 座位 %d 无动作可做", seed, s.Turn)
				}
				if err := Apply(s, a); err != nil {
					t.Fatalf("seed=%d 动作 %T 失败: %v", seed, a, err)
				}
			}
			if s.RoundWinner < 0 {
				t.Fatalf("seed=%d 结束了却没有赢家", seed)
			}
		}
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	s := NewGame(4, 777, Rules{Stacking: true})
	for i := 0; i < 30 && s.Phase == Playing; i++ {
		if err := Apply(s, BotMove(s, s.Turn)); err != nil {
			t.Fatalf("准备状态失败: %v", err)
		}
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	restored := &State{}
	if err := json.Unmarshal(raw, restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if !reflect.DeepEqual(s, restored) {
		t.Fatal("存盘恢复后状态不一致")
	}

	// 两边继续走同样的步子，结果必须完全一致。
	for i := 0; i < 100 && s.Phase == Playing; i++ {
		a := BotMove(s, s.Turn)
		if a == nil {
			break
		}
		if err := Apply(s, a); err != nil {
			t.Fatalf("原状态出错: %v", err)
		}
		if err := Apply(restored, a); err != nil {
			t.Fatalf("恢复状态出错: %v", err)
		}
		if !reflect.DeepEqual(s, restored) {
			t.Fatalf("第 %d 步之后状态分叉", i)
		}
	}
}

// TestChallengeSucceeds：出 +4 的人当时手里还有上一种颜色的牌，质疑成立。
func TestChallengeSucceeds(t *testing.T) {
	rules := Rules{Challenge: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: NoColor, Kind: WildDrawFour},
			{ID: 11, Color: Red, Kind: Number, Num: 9}, // 手里还有红牌，本不该用 +4 挡
		},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 5}},
	)
	s.Deck = []Card{
		{ID: 100, Color: Blue, Kind: Number, Num: 1},
		{ID: 101, Color: Blue, Kind: Number, Num: 2},
		{ID: 102, Color: Blue, Kind: Number, Num: 3},
		{ID: 103, Color: Blue, Kind: Number, Num: 4},
		{ID: 104, Color: Blue, Kind: Number, Num: 5},
		{ID: 105, Color: Blue, Kind: Number, Num: 6},
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 10, Color: Blue})
	if s.Challenge == nil {
		t.Fatal("打出 +4 之后应该挂起一个质疑")
	}
	if s.Turn != 1 {
		t.Fatalf("决定权应该在下一家，实际轮到 %d", s.Turn)
	}
	// 质疑没决定之前，别的东西都不能做。
	if err := Apply(s, DrawCard{Seat: 1}); err != ErrChallenge {
		t.Fatalf("质疑未决定时不该能摸牌，实际 %v", err)
	}

	before := len(s.Hands[0])
	mustApply(t, s, ResolveChallenge{Seat: 1, Challenge: true})
	if got := len(s.Hands[0]); got != before+4 {
		t.Fatalf("质疑成立，出 +4 的人应该摸 4 张：%d -> %d", before, got)
	}
	if s.Turn != 1 {
		t.Fatalf("质疑成立后回合应该留在质疑者手上，实际 %d", s.Turn)
	}
	if s.Challenge != nil {
		t.Fatal("质疑处理完应该清空")
	}
}

// TestChallengeFails：出 +4 的人确实没有同色牌，质疑者要摸 6 张。
func TestChallengeFails(t *testing.T) {
	rules := Rules{Challenge: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: NoColor, Kind: WildDrawFour},
			{ID: 11, Color: Green, Kind: Number, Num: 9}, // 只有绿牌，没有红
		},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 5}},
	)
	for i := 0; i < 8; i++ {
		s.Deck = append(s.Deck, Card{ID: 100 + i, Color: Blue, Kind: Number, Num: 1})
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 10, Color: Blue})
	before := len(s.Hands[1])
	mustApply(t, s, ResolveChallenge{Seat: 1, Challenge: true})

	if got := len(s.Hands[1]); got != before+6 {
		t.Fatalf("质疑失败应该摸 6 张：%d -> %d", before, got)
	}
	if s.Turn != 0 {
		t.Fatalf("质疑失败后被跳过，应该轮到 0，实际 %d", s.Turn)
	}
}

// TestChallengeAccept：认罚就是摸 4 张并被跳过。
func TestChallengeAccept(t *testing.T) {
	rules := Rules{Challenge: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{
			{ID: 10, Color: NoColor, Kind: WildDrawFour},
			{ID: 11, Color: Green, Kind: Number, Num: 9},
		},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 5}},
	)
	for i := 0; i < 6; i++ {
		s.Deck = append(s.Deck, Card{ID: 100 + i, Color: Blue, Kind: Number, Num: 1})
	}

	mustApply(t, s, PlayCard{Seat: 0, CardID: 10, Color: Blue})
	before := len(s.Hands[1])
	mustApply(t, s, ResolveChallenge{Seat: 1, Challenge: false})

	if got := len(s.Hands[1]); got != before+4 {
		t.Fatalf("认罚应该摸 4 张：%d -> %d", before, got)
	}
	if s.Turn != 0 {
		t.Fatalf("认罚后被跳过，应该轮到 0，实际 %d", s.Turn)
	}
}

// TestChallengeLegalMoves：质疑期间只允许那个决定。
func TestChallengeLegalMoves(t *testing.T) {
	rules := Rules{Challenge: true}
	top := Card{ID: 1, Color: Red, Kind: Number, Num: 3}
	s := testState(rules, top, 0,
		[]Card{{ID: 10, Color: NoColor, Kind: WildDrawFour}, {ID: 11, Color: Green, Kind: Number, Num: 9}},
		[]Card{{ID: 20, Color: Blue, Kind: Number, Num: 5}},
	)
	s.Deck = []Card{{ID: 100, Color: Blue, Kind: Number, Num: 1}}
	mustApply(t, s, PlayCard{Seat: 0, CardID: 10, Color: Blue})

	legal := LegalMoves(s, 1)
	if len(legal) != 2 {
		t.Fatalf("质疑者应该只有两个选择，实际 %v", legal)
	}
	for _, a := range legal {
		if _, ok := a.(ResolveChallenge); !ok {
			t.Fatalf("不该出现别的动作: %T", a)
		}
	}
	if moves := LegalMoves(s, 0); len(moves) != 0 {
		t.Fatalf("不是质疑者不该有合法动作，实际 %v", moves)
	}

	// 桌子上的电脑一律认罚，不会卡住。
	a := Decide(Observe(s, 1))
	if a == nil {
		t.Fatal("机器人应该给出认罚动作")
	}
	if rc, ok := a.(ResolveChallenge); !ok || rc.Challenge {
		t.Fatalf("机器人应该认罚，实际 %T %+v", a, a)
	}
}
