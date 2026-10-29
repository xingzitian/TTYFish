package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func strip(s string) string { return ansiRE.ReplaceAllString(s, "") }

// setObs 改快照里的 UNO 视图。平台把视图当不透明值传，测试里得转回来改。
func setObs(snap *client.Snapshot, f func(o *engine.Observation)) {
	o, _ := snap.View.(engine.Observation)
	f(&o)
	snap.View = o
}

// testModel 直接拼一帧快照来测渲染，不依赖真实房间的时序。
func testModel(t *testing.T, w int, snap client.Snapshot) *Model {
	t.Helper()
	m := &Model{
		renderer: lipgloss.DefaultRenderer(),
		width:    w,
		height:   24,
		snap:     snap,
	}
	return m
}

func sampleSnap(seed int64) client.Snapshot {
	st := engine.NewGame(4, seed, engine.Rules{})
	names := []string{"你", "电脑A", "电脑B", "电脑C"}
	players := make([]client.Player, 4)
	for i := range players {
		players[i] = client.Player{Seat: i, Name: names[i], Connected: true, Host: i == 0}
	}
	return client.Snapshot{
		Code:    "ABC234",
		Phase:   client.Playing,
		MySeat:  0,
		IsHost:  true,
		Players: players,
		View:    engine.Observe(st, 0),
	}
}

// TestViewFitsWidth 保证任何一行都不会超过终端宽度：
// 窄终端（VS Code 侧边栏）下溢出会让整个牌桌被终端硬折。
func TestViewFitsWidth(t *testing.T) {
	for _, turn := range []int{0, 1} {
		for _, w := range []int{76, 50, 40} {
			snap := sampleSnap(20241029)
			setObs(&snap, func(o *engine.Observation) { o.Turn = turn })
			m := testModel(t, w, snap)
			plain := strip(m.View())
			t.Logf("宽度 %d 回合 %d:\n%s", w, turn, plain)

			for i, line := range strings.Split(plain, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("宽度 %d 时第 %d 行超宽: %d > %d（%q）", w, i+1, got, w, line)
				}
			}
		}
	}
}

// TestViewMidGame 渲染一个打到一半的局面，检查战报和牌桌元素。
func TestViewMidGame(t *testing.T) {
	st := engine.NewGame(4, 20241029, engine.Rules{})
	for i := 0; i < 9 && st.Phase == engine.Playing; i++ {
		if a := engine.BotMove(st, st.Turn); a != nil {
			if err := engine.Apply(st, a); err != nil {
				t.Fatalf("准备局面失败: %v", err)
			}
		}
	}
	snap := sampleSnap(20241029)
	snap.View = engine.Observe(st, 0)
	setObs(&snap, func(o *engine.Observation) { o.Turn = 0 }) // 只为预览：拉回自己回合，好看清"轮到你" })

	m := testModel(t, 76, snap)
	plain := strip(m.View())
	t.Logf("\n%s", plain)

	if !strings.Contains(plain, "你的手牌") {
		t.Fatalf("没有渲染出手牌区:\n%s", plain)
	}
	for i, line := range strings.Split(plain, "\n") {
		if got := lipgloss.Width(line); got > 76 {
			t.Errorf("第 %d 行超宽: %d（%q）", i+1, got, line)
		}
	}
}

func TestViewLobby(t *testing.T) {
	snap := sampleSnap(1)
	snap.Phase = client.Lobby
	snap.View = engine.Observation{}
	snap.Players[1].Connected = false
	snap.Players[1].Offline = 12
	snap.Players[1].Bot = true

	m := testModel(t, 70, snap)
	out := strip(m.View())
	for _, want := range []string{"房间 ABC234", "电脑A", "掉线", "s 开始"} {
		if !strings.Contains(out, want) {
			t.Fatalf("大厅界面缺少 %q:\n%s", want, out)
		}
	}
}

func TestViewEnterRoomCode(t *testing.T) {
	m := newKeyModel("AB3")
	out := strip(m.View())
	if !strings.Contains(out, "房间码") || !strings.Contains(out, "AB3") {
		t.Fatalf("房间码输入界面不对:\n%s", out)
	}
	if !strings.Contains(out, "回车开一间新房") {
		t.Fatalf("缺少新建房间的提示:\n%s", out)
	}
}

func TestViewChatPresets(t *testing.T) {
	snap := sampleSnap(1)
	m := testModel(t, 70, snap)
	m.chat = true
	out := strip(m.View())
	for i, p := range client.Presets {
		want := string(rune('1'+i)) + " " + p
		if !strings.Contains(out, want) {
			t.Fatalf("快捷语列表缺少 %q:\n%s", want, out)
		}
	}
}

// TestQuitShowsNothing 验证自己按 q 退出时不画"连接已断开"。
// 之前就是这么回事：quit 先取消订阅，挂起的读取立刻返回"通道关闭"，
// 于是每次正常退出都会闪一句吓人的提示。
func TestQuitShowsNothing(t *testing.T) {
	m := testModel(t, 70, sampleSnap(1))
	m.quitting = true
	m.closed = true
	if out := strip(m.View()); out != "" {
		t.Fatalf("自己退出时不该渲染任何内容，实际:\n%s", out)
	}
}

func TestKickedMessage(t *testing.T) {
	m := testModel(t, 70, sampleSnap(1))
	m.closed = true
	m.kicked = true
	out := strip(m.View())
	if !strings.Contains(out, "另一台设备") {
		t.Fatalf("被顶号时应该说清楚原因:\n%s", out)
	}
}

func TestUnexpectedDisconnectMessage(t *testing.T) {
	m := testModel(t, 70, sampleSnap(1))
	m.closed = true
	out := strip(m.View())
	if !strings.Contains(out, "连接已断开") {
		t.Fatalf("被动断开时应该提示:\n%s", out)
	}
}

// TestViewShowsWhoseTurn 盯住"现在到底谁在出牌"。
// 之前只有对手名字前一个小 ▶，和自己手牌的 ▶ 混在一起，看不出重点。
func TestViewShowsWhoseTurn(t *testing.T) {
	// 别人在出牌
	snap := sampleSnap(1)
	setObs(&snap, func(o *engine.Observation) { o.Turn = 1 })
	out := strip(testModel(t, 76, snap).View())
	if !strings.Contains(out, "电脑A 正在出牌") {
		t.Fatalf("看不出是谁在出牌:\n%s", out)
	}

	// 自己在出牌
	setObs(&snap, func(o *engine.Observation) { o.Turn = 0 })
	out = strip(testModel(t, 76, snap).View())
	if !strings.Contains(out, "轮到你出牌") {
		t.Fatalf("该我出牌时没有提示:\n%s", out)
	}
}

// TestCardsNumberOutsideColor 牌号必须在色块外面，用冒号隔开。
func TestCardsNumberOutsideColor(t *testing.T) {
	m := testModel(t, 76, sampleSnap(20241029))
	out := m.View()
	// 序号后紧跟冒号，然后才是带颜色的牌面。
	if !strings.Contains(strip(out), " 1:") {
		t.Fatalf("手牌没有按「序号:」渲染:\n%s", strip(out))
	}
}

func cardPtr(c engine.Card) *engine.Card { return &c }

// TestViewTimelinePanel 宽终端下右侧要有带时间戳的出牌记录。
func TestViewTimelinePanel(t *testing.T) {
	snap := sampleSnap(1)
	snap.Timeline = []client.TimelineEntry{
		{AtMS: 1000, Seat: 0, Detail: cardPtr(engine.Card{Color: engine.Red, Kind: engine.Number, Num: 1})},
		{AtMS: 2500, Seat: 1, Detail: cardPtr(engine.Card{Color: engine.Green, Kind: engine.DrawTwo})},
		{AtMS: 2600, Seat: -1, Text: "方向已改变！当前方向：↺"},
	}
	m := testModel(t, 120, snap)
	m.height = 30

	out := strip(m.View())
	for _, want := range []string{
		"出牌记录",
		"[00:01]",
		"出牌 [R|1]",
		"[00:02]",
		"出牌 [G|+2]",
		"方向已改变！当前方向：↺",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("出牌记录缺少 %q:\n%s", want, out)
		}
	}

	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > 120 {
			t.Errorf("第 %d 行超宽: %d（%q）", i+1, got, line)
		}
	}
	t.Logf("\n%s", out)
}

// TestChatVisibleInWideLayout 双栏模式下也必须能看到快捷语菜单。
// 之前这份菜单只在"底部记录区"里画，而双栏模式没有底部记录区——
// 于是按 t 之后界面毫无反应，按键还被模态吃掉，看起来像"不让说话"。
func TestChatVisibleInWideLayout(t *testing.T) {
	m := testModel(t, 120, sampleSnap(1))
	m.height = 30
	m.chat = true

	out := strip(m.View())
	for i, p := range client.Presets {
		want := string(rune('1'+i)) + " " + p
		if !strings.Contains(out, want) {
			t.Fatalf("双栏模式下快捷语菜单缺少 %q:\n%s", want, out)
		}
	}
}

// TestCanChatWhenNotMyTurn 说话不该受回合限制。
func TestCanChatWhenNotMyTurn(t *testing.T) {
	snap := sampleSnap(1)
	setObs(&snap, func(o *engine.Observation) { o.Turn = 1 }) // 别人在出牌
	m := testModel(t, 120, snap)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !m.chat {
		t.Fatal("不在自己回合时也应该能打开快捷语")
	}
}

func TestViewRoundOver(t *testing.T) {
	snap := sampleSnap(7)
	setObs(&snap, func(o *engine.Observation) { o.Phase = engine.RoundOver })
	setObs(&snap, func(o *engine.Observation) { o.RoundWinner = 0 })
	setObs(&snap, func(o *engine.Observation) { o.Scores[0] = 42 })

	m := testModel(t, 76, snap)
	out := strip(m.View())
	if !strings.Contains(out, "打完手牌") {
		t.Fatalf("结算界面没渲染出来:\n%s", out)
	}
}
