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
	snap       client.Snapshot
	last       engine.Action
	acted      int
	start      int
	addBot     int
	chat       int
	dlcID      string
	dlcEnabled bool
}

func (c *fakeClient) SetDLC(id string, enabled bool) error {
	c.dlcID, c.dlcEnabled = id, enabled
	return nil
}

func (c *fakeClient) ToggleDLC(id string) error {
	c.dlcID, c.dlcEnabled = id, !c.dlcEnabled
	return nil
}

func TestLobbyReverseDLCToggleAndDirection(t *testing.T) {
	m, c := newTestModel(t, engine.Observation{}, client.Lobby)
	if view := strip(m.View()); !strings.Contains(view, "反转牌：关闭") || !strings.Contains(view, "[X]切换反转DLC") {
		t.Fatalf("missing DLC selector: %s", view)
	}
	press(m, "x")
	if c.dlcID != "reverse" || !c.dlcEnabled {
		t.Fatal("DLC enable not sent")
	}
	press(m, "x")
	if c.dlcEnabled {
		t.Fatal("DLC disable not sent")
	}
	m.snap.IsHost = false
	c.dlcID = ""
	press(m, "x")
	if c.dlcID != "" {
		t.Fatal("non-host changed DLC")
	}
	o := playingObs(engine.Card{ID: 10, Kind: engine.Reverse})
	o.Direction = -1
	m, c = newTestModel(t, o, client.Playing)
	view := strip(m.View())
	if !strings.Contains(view, "[功能|反转]") || !strings.Contains(view, "←") {
		t.Fatalf("missing reverse or direction: %s", view)
	}
	press(m, "1")
	if c.acted != 1 || c.last.Type != engine.Play || c.last.CardIDs[0] != 10 {
		t.Fatal("reverse card not playable")
	}
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
func (c *fakeClient) Chat(int) error         { c.chat++; return nil }

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

func playingObs(hand ...engine.Card) engine.Observation {
	return engine.Observation{
		Seat: 0, Phase: engine.Playing, Turn: 0, TurnsRemaining: 1,
		Alive: []bool{true, true, true}, HandCounts: []int{len(hand), 4, 3},
		Hand: hand, DeckCount: 14, BombsLeft: 2,
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

// TestFunctionCardsPlayDirectly 功能牌（攻击/跳过/洗牌/预言）按一下就出。
func TestFunctionCardsPlayDirectly(t *testing.T) {
	for _, card := range []engine.Card{
		{ID: 11, Kind: engine.Attack},
		{ID: 12, Kind: engine.Skip},
		{ID: 13, Kind: engine.Shuffle},
		{ID: 14, Kind: engine.SeeTheFuture},
	} {
		m, c := newTestModel(t, playingObs(card), client.Playing)
		press(m, "1")
		if c.acted != 1 {
			t.Fatalf("%s 应该按下即出，实际 %d 次", card.Kind, c.acted)
		}
		if c.last.Type != engine.Play || len(c.last.CardIDs) != 1 || c.last.CardIDs[0] != card.ID {
			t.Fatalf("%s 动作用了错的牌：%+v", card.Kind, c.last)
		}
	}
}

// TestCatCardCannotBePlayedAlone 单张猫牌不能出，要按 G。
func TestCatCardCannotBePlayedAlone(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	if c.acted != 0 {
		t.Fatal("单张猫牌不该出")
	}
	if !m.errVisible() || !strings.Contains(m.errMsg, "G") {
		t.Fatalf("应该提示按 G：%q", m.errMsg)
	}
}

// TestPairDrawAutoDetects 按 G 自动找出同款两张，只需要选目标。
func TestPairDrawAutoDetects(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
		engine.Card{ID: 3, Kind: engine.Skip},
	)
	m, c := newTestModel(t, o, client.Playing)

	if _, pairs := m.pairKind(); pairs != 1 {
		t.Fatalf("应该检测到 1 个对子，实际 %d", pairs)
	}
	press(m, "g")
	if m.step != stepTarget {
		t.Fatalf("按 G 应该进入选目标，实际 step=%v", m.step)
	}
	press(m, "1") // 第一个可选对手（座位 1），数字直选
	if c.last.Type != engine.Play || len(c.last.CardIDs) != 2 || c.last.Target != 1 {
		t.Fatalf("对子盲抽动作不对：%+v", c.last)
	}
}

// TestPairDrawWithoutPair 没有对子时按 G 要给提示。
func TestPairDrawWithoutPair(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.TacoCat})
	m, c := newTestModel(t, o, client.Playing)
	press(m, "g")
	if c.acted != 0 || m.step != stepIdle {
		t.Fatal("没有对子时不该进入选择")
	}
	if !m.errVisible() {
		t.Fatal("应该提示凑不齐一对")
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

// TestNonPlayableCardsExplain 保命/炸弹/否决要说清为什么不能主动出。
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

// TestRespondingNopeByKey 响应窗口：N 打否决，P 放弃。
func TestRespondingNopeByKey(t *testing.T) {
	o := playingObs(engine.Card{ID: 9, Kind: engine.Nope})
	o.Phase = engine.Responding
	o.Pending = &engine.Effect{
		Kind: engine.Attack, Passed: []bool{false, true, true},
		Action: engine.Action{Seat: 1, Type: engine.Play},
	}

	m, c := newTestModel(t, o, client.Playing)
	press(m, "n")
	if c.last.Type != engine.PlayNope || c.last.CardIDs[0] != 9 {
		t.Fatalf("N 应该打出否决，实际 %+v", c.last)
	}

	m2, c2 := newTestModel(t, o, client.Playing)
	press(m2, "p")
	if c2.last.Type != engine.Pass {
		t.Fatalf("P 应该放弃，实际 %+v", c2.last)
	}
}

// TestRespondingWithoutNopeCard 没有否决牌时按 N 要提示。
func TestRespondingWithoutNopeCard(t *testing.T) {
	o := playingObs(engine.Card{ID: 5, Kind: engine.Skip})
	o.Phase = engine.Responding
	o.Pending = &engine.Effect{Kind: engine.Attack, Passed: []bool{false, true, true}}
	m, c := newTestModel(t, o, client.Playing)
	press(m, "n")
	if c.acted != 0 {
		t.Fatal("没有否决牌不该发动作")
	}
	if !m.errVisible() {
		t.Fatal("应该提示没有否决牌")
	}
}

// TestDefusePositionPresets 埋雷位置预设：0/1/2/B 直接生效。
func TestDefusePositionPresets(t *testing.T) {
	base := func() engine.Observation {
		o := playingObs(
			engine.Card{ID: 5, Kind: engine.Defuse},
			engine.Card{ID: 6, Kind: engine.Skip},
		)
		o.Phase = engine.Reinserting
		o.DeckCount = 12
		return o
	}
	for _, tc := range []struct {
		key  string
		want int
	}{
		{"0", 0}, {"1", 1}, {"2", 2}, {"b", 12},
	} {
		m, c := newTestModel(t, base(), client.Playing)
		press(m, tc.key)
		if c.last.Type != engine.UseDefuse {
			t.Fatalf("按 %q 应该埋雷，实际 %+v", tc.key, c.last)
		}
		if c.last.Position != tc.want {
			t.Fatalf("按 %q 位置应该是 %d，实际 %d", tc.key, tc.want, c.last.Position)
		}
		if len(c.last.CardIDs) != 1 || c.last.CardIDs[0] != 5 {
			t.Fatalf("应该自动用拆弹牌 5，实际 %v", c.last.CardIDs)
		}
	}

	m, c := newTestModel(t, base(), client.Playing)
	press(m, "r")
	if c.last.Position < 0 || c.last.Position > 12 {
		t.Fatalf("随机位置越界：%d", c.last.Position)
	}
}

// TestDefuseCustomPosition I 进入指定位置，-= 调整，Enter 确认。
func TestDefuseCustomPosition(t *testing.T) {
	o := playingObs(engine.Card{ID: 5, Kind: engine.Defuse})
	o.Phase = engine.Reinserting
	o.DeckCount = 12
	m, c := newTestModel(t, o, client.Playing)

	press(m, "I")
	if m.step != stepDefuseCustom {
		t.Fatalf("I 应该进入指定位置，实际 step=%v", m.step)
	}
	press(m, "=")
	press(m, "=")
	press(m, "=")
	if m.pos != 3 {
		t.Fatalf("按三次 = 应该到第 3 张，实际 %d", m.pos)
	}
	press(m, "enter")
	if c.last.Type != engine.UseDefuse || c.last.Position != 3 {
		t.Fatalf("指定位置动作不对：%+v", c.last)
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

// TestCountdownShowsSeconds 响应窗口有真实倒计时。
func TestCountdownShowsSeconds(t *testing.T) {
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
	if !strings.Contains(m.hint(), "生效倒计时") {
		t.Fatalf("提示里要有倒计时：%q", m.hint())
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

// TestFutureRevealShowsCards 预言生效后展示牌堆顶，并带 10 秒倒计时。
func TestFutureRevealShowsCards(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Skip})
	m := NewModel(&fakeClient{snap: snapshot(t, o, client.Playing)},
		Options{Key: "k", Layout: ui.LayoutCompact}, lipgloss.DefaultRenderer())
	m.width, m.height = 80, 24

	next := o
	next.Future = []engine.Card{
		{ID: 99, Kind: engine.ExplodingKitten},
		{ID: 98, Kind: engine.Skip},
	}
	m.Update(client.SnapshotMsg(snapshot(t, next, client.Playing)))

	if left := m.revealLeft(); left <= 0 {
		t.Fatalf("预言结果应该有倒计时，实际 %d", left)
	}
	hint := m.hint()
	if !strings.Contains(hint, "预言结果") || !strings.Contains(hint, "炸弹") {
		t.Fatalf("预言结果提示不对：%q", hint)
	}
}

// TestHelpShowsPhaseKeys 帮助里的键跟着阶段变。
func TestHelpShowsPhaseKeys(t *testing.T) {
	m, _ := newTestModel(t, playingObs(engine.Card{ID: 1, Kind: engine.Skip}), client.Playing)
	press(m, "?")
	if !m.help {
		t.Fatal("? 应该打开帮助")
	}
	if !strings.Contains(strip(m.View()), "对子盲抽") {
		t.Fatalf("帮助里应该有对子盲抽：\n%s", strip(m.View()))
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

// TestChatKey T opens the preset chooser; a number sends the selected message.
func TestChatKey(t *testing.T) {
	m, c := newTestModel(t, playingObs(engine.Card{ID: 1, Kind: engine.Skip}), client.Playing)
	press(m, "t")
	if c.chat != 0 || m.step != stepChat {
		t.Fatal("T should open the chat chooser without sending")
	}
	press(m, "1")
	if c.chat != 1 {
		t.Fatalf("T 应该发一条快捷语，实际 %d", c.chat)
	}
}

// TestQAlwaysQuits 老板键任何时候都断开。
func TestQAlwaysQuits(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, _ := newTestModel(t, o, client.Playing)
	press(m, "g")
	if m.step != stepTarget {
		t.Fatalf("前置条件失败：step=%v", m.step)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("选目标时按 Q 也应该退出")
	}
}

// TestEscCancelsSelection Esc 只取消选择。
func TestEscCancelsSelection(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, c := newTestModel(t, o, client.Playing)
	press(m, "g")
	press(m, "esc")
	if m.step != stepIdle || c.acted != 0 {
		t.Fatalf("Esc 应该只取消选择：step=%v acted=%d", m.step, c.acted)
	}
}

// TestDeadStateIgnoresGameKeys 淘汰后按键不该发动作。
func TestDeadStateIgnoresGameKeys(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Skip})
	o.Alive = []bool{false, true, true}
	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	press(m, "d")
	press(m, "g")
	if c.acted != 0 {
		t.Fatalf("淘汰后不该有动作，实际 %d", c.acted)
	}
}

// TestCompactFitsWidth 极简模式每行都不能超宽。
func TestCompactFitsWidth(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
		engine.Card{ID: 3, Kind: engine.Defuse},
	)
	o.Discard = []engine.Card{{ID: 9, Kind: engine.Attack}}
	o.DiscardBy = 1
	for _, w := range []int{80, 60, 40} {
		m, _ := newTestModel(t, o, client.Playing)
		m.width, m.height = w, 24
		for _, line := range strings.Split(strip(m.View()), "\n") {
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
