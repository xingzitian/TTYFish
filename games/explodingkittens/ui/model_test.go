package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

type fakeClient struct {
	client.Client
	snap   client.Snapshot
	last   engine.Action
	acted  int
	start  int
	addBot int
}

func (c *fakeClient) Snapshot() client.Snapshot       { return c.snap }
func (c *fakeClient) Updates() <-chan client.Snapshot { return nil }
func (c *fakeClient) Act(a any) error {
	if act, ok := a.(engine.Action); ok {
		c.last = act
	}
	c.acted++
	return nil
}
func (c *fakeClient) Start() error           { c.start++; return nil }
func (c *fakeClient) AddBot() error          { c.addBot++; return nil }
func (c *fakeClient) VoteRestart(bool) error { return nil }

// snapshot 造一个"我在座位 0"的快照。
func snapshot(t *testing.T, o engine.Observation, phase client.Phase) client.Snapshot {
	t.Helper()
	return client.Snapshot{
		Code: "AB12CD", Phase: phase, MySeat: 0, IsHost: true,
		Players: []client.Player{
			{Seat: 0, Name: "你", Connected: true, Host: true},
			{Seat: 1, Name: "电脑A", Connected: true},
			{Seat: 2, Name: "电脑B", Connected: true},
		},
		View: o,
	}
}

// playingObs 造一个"轮到我、手里有这些牌"的观察。
func playingObs(hand ...engine.Card) engine.Observation {
	return engine.Observation{
		Seat: 0, Phase: engine.Playing, Turn: 0, TurnsRemaining: 1,
		Alive: []bool{true, true, true}, HandCounts: []int{len(hand), 4, 3},
		Hand: hand, DeckCount: 30,
	}
}

func newTestModel(t *testing.T, o engine.Observation, phase client.Phase) (*Model, *fakeClient) {
	t.Helper()
	c := &fakeClient{snap: snapshot(t, o, phase)}
	m := NewModel(c, Options{Key: "k", Layout: ui.LayoutCompact}, lipgloss.DefaultRenderer())
	m.width, m.height = 80, 24
	return m, c
}

func press(m *Model, key string) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	_, _ = m.Update(msg)
}

// TestSingleCardPlaysDirectly 单张牌（攻击/跳过/洗牌/预知）按一下就出。
func TestSingleCardPlaysDirectly(t *testing.T) {
	for _, card := range []engine.Card{
		{ID: 11, Kind: engine.Attack},
		{ID: 12, Kind: engine.Skip},
		{ID: 13, Kind: engine.Shuffle},
		{ID: 14, Kind: engine.SeeTheFuture},
	} {
		m, c := newTestModel(t, playingObs(card), client.Playing)
		press(m, "1")
		if c.acted != 1 {
			t.Fatalf("%s 应该按下即出，实际发了 %d 个动作", card.Kind, c.acted)
		}
		if c.last.Type != engine.Play || len(c.last.CardIDs) != 1 || c.last.CardIDs[0] != card.ID {
			t.Fatalf("%s 动作用了错的牌：%+v", card.Kind, c.last)
		}
	}
}

// TestCatCardEntersCombo 猫牌：第一次按进入候选，第二次按同色才继续。
func TestCatCardEntersCombo(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1")
	if m.step != stepCombo {
		t.Fatalf("按第一张猫牌应该进入候选，实际 step=%v", m.step)
	}
	if c.acted != 0 {
		t.Fatal("进入候选时不该立即出牌")
	}

	// 同色：留在组合里（再按一张能凑三张）
	press(m, "2")
	if m.step != stepCombo {
		t.Fatalf("第二张同色应该留在组合里，实际 step=%v", m.step)
	}
	// 回车把这一对出掉，进入选目标
	press(m, "enter")
	if m.step != stepTarget {
		t.Fatalf("回车后应该进入选目标，实际 step=%v", m.step)
	}
}

// TestCatMismatchCancels 按了不同颜色的猫，立即取消并给出提示。
func TestCatMismatchCancels(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.BeardCat},
	)
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1")
	if m.step != stepCombo {
		t.Fatalf("应该进入候选，实际 step=%v", m.step)
	}
	press(m, "2")
	if m.step != stepIdle {
		t.Fatalf("颜色不同应该取消组合，实际 step=%v", m.step)
	}
	if c.acted != 0 {
		t.Fatal("不匹配时不该发动作")
	}
	if !m.errVisible() {
		t.Fatal("不匹配时要有提示")
	}
}

// TestDrawKey 按 D 摸牌。
func TestDrawKey(t *testing.T) {
	m, c := newTestModel(t, playingObs(engine.Card{ID: 1, Kind: engine.Skip}), client.Playing)
	press(m, "d")
	if c.acted != 1 || c.last.Type != engine.Draw {
		t.Fatalf("D 应该摸牌，实际 %+v", c.last)
	}
}

// TestNonPlayableCardsExplain 现在出不了的牌要说清原因，而不是静默。
func TestNonPlayableCardsExplain(t *testing.T) {
	for _, card := range []engine.Card{
		{ID: 1, Kind: engine.Defuse},
		{ID: 2, Kind: engine.Nope},
		{ID: 3, Kind: engine.ExplodingKitten},
	} {
		m, c := newTestModel(t, playingObs(card), client.Playing)
		press(m, "1")
		if c.acted != 0 {
			t.Fatalf("%s 不该被直接打出", card.Kind)
		}
		if !m.errVisible() {
			t.Fatalf("%s 应该有解释", card.Kind)
		}
	}
}

// TestRespondingNopeAndPass 响应窗口：按数字打反制，P 放弃。
func TestRespondingNopeAndPass(t *testing.T) {
	o := playingObs(engine.Card{ID: 9, Kind: engine.Nope})
	o.Phase = engine.Responding
	o.Pending = &engine.Effect{
		Kind: engine.Attack, Passed: []bool{false, true, true},
		Action: engine.Action{Seat: 1, Type: engine.Play},
	}

	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	if c.last.Type != engine.PlayNope {
		t.Fatalf("应该打反制，实际 %+v", c.last)
	}

	m2, c2 := newTestModel(t, o, client.Playing)
	press(m2, "p")
	if c2.last.Type != engine.Pass {
		t.Fatalf("P 应该放弃，实际 %+v", c2.last)
	}
}

// TestDefuseTwoSteps 拆弹两步走：先选牌，再选位置。
func TestDefuseTwoSteps(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 5, Kind: engine.Defuse},
		engine.Card{ID: 6, Kind: engine.Skip},
	)
	o.Phase = engine.Reinserting
	o.DeckCount = 12
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1") // 拆弹排最前
	if m.step != stepDefusePos {
		t.Fatalf("选完拆弹牌应该进入选位置，实际 step=%v", m.step)
	}
	if c.acted != 0 {
		t.Fatal("还没选位置就发动作了")
	}
	if m.pos != 0 {
		t.Fatalf("位置应该从 0 开始，实际 %d", m.pos)
	}
	press(m, "=")
	press(m, "=")
	if m.pos != 2 {
		t.Fatalf("按两次 = 应该到第 2 张，实际 %d", m.pos)
	}
	press(m, "enter")
	if c.last.Type != engine.UseDefuse || c.last.Position != 2 ||
		len(c.last.CardIDs) != 1 || c.last.CardIDs[0] != 5 {
		t.Fatalf("拆弹动作不对：%+v", c.last)
	}
}

// TestDefuseRejectsWrongCard 拆弹第一步选错牌要给提示，不能进入第二步。
func TestDefuseRejectsWrongCard(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 5, Kind: engine.Defuse},
		engine.Card{ID: 6, Kind: engine.Skip},
	)
	o.Phase = engine.Reinserting
	m, c := newTestModel(t, o, client.Playing)

	press(m, "2") // 跳过牌
	if m.step == stepDefusePos {
		t.Fatal("选错牌不该进入选位置")
	}
	if c.acted != 0 || !m.errVisible() {
		t.Fatal("选错牌应该给提示且不发动作")
	}
}

// TestGivingPicksCard 交牌阶段按数字交出去。
func TestGivingPicksCard(t *testing.T) {
	o := playingObs(engine.Card{ID: 7, Kind: engine.Skip})
	o.Phase = engine.Giving
	o.Pending = &engine.Effect{
		Kind: engine.Favor, Passed: []bool{true, true, true},
		Action: engine.Action{Seat: 1, Target: 0, Type: engine.Play},
	}
	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	if c.last.Type != engine.Give || c.last.CardIDs[0] != 7 {
		t.Fatalf("应该交出手牌 7，实际 %+v", c.last)
	}
}

// TestCountdownUsesServerDeadline 倒计时按服务端剩余毫秒走，并且会真的减少。
func TestCountdownUsesServerDeadline(t *testing.T) {
	o := playingObs(engine.Card{ID: 9, Kind: engine.Nope})
	o.Phase = engine.Responding
	o.Pending = &engine.Effect{Kind: engine.Attack, Passed: []bool{false, true, true}}
	snap := snapshot(t, o, client.Playing)
	snap.PendingLeftMS = 5000
	c := &fakeClient{snap: snap}
	m := NewModel(c, Options{Key: "k", Layout: ui.LayoutCompact}, lipgloss.DefaultRenderer())
	m.width, m.height = 80, 24

	if got := m.remaining(); got != 5 {
		t.Fatalf("应该显示 5 秒，实际 %d", got)
	}
	if !strings.Contains(m.hint(), "5 秒") {
		t.Fatalf("提示里要带秒数：%q", m.hint())
	}
	m.deadline = time.Now().Add(2400 * time.Millisecond)
	if got := m.remaining(); got != 3 {
		t.Fatalf("2.4 秒应该进位成 3 秒，实际 %d", got)
	}
	m.deadline = time.Time{}
	if got := m.remaining(); got != -1 {
		t.Fatalf("没有倒计时应该返回 -1，实际 %d", got)
	}
}

// TestHelpListsPhaseKeys 帮助里的键要跟着阶段变。
func TestHelpListsPhaseKeys(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Skip})
	m, _ := newTestModel(t, o, client.Playing)
	press(m, "?")
	if !m.help {
		t.Fatal("? 应该打开帮助")
	}
	view := strip(m.View())
	if !strings.Contains(view, "出牌") {
		t.Fatalf("帮助里应该有出牌一组：\n%s", view)
	}
	press(m, "esc")
	if m.help {
		t.Fatal("Esc 应该关闭帮助")
	}
}

// TestLobbyStartAndBot 大厅里房主能开局、加电脑。
func TestLobbyStartAndBot(t *testing.T) {
	m, c := newTestModel(t, engine.Observation{}, client.Lobby)
	press(m, "s")
	press(m, "b")
	if c.start != 1 || c.addBot != 1 {
		t.Fatalf("S/B 应该分别调用开局和加电脑：start=%d bot=%d", c.start, c.addBot)
	}
}

// TestCompactFitsWidth 极简模式每行都不能超宽。
func TestCompactFitsWidth(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
		engine.Card{ID: 3, Kind: engine.Defuse},
	)
	for _, w := range []int{80, 60, 40, 30} {
		m, _ := newTestModel(t, o, client.Playing)
		m.width, m.height = w, 24
		out := strip(m.View())
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("宽度 %d 有超宽行：%q", w, line)
			}
		}
	}
}

// strip 去掉 ANSI 转义，方便断言文本。
func strip(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
