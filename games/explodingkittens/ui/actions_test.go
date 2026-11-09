package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestRegisteredResponseKeyDrivesHintAndInput(t *testing.T) {
	old := responseActions[0]
	t.Cleanup(func() { responseActions[0] = old })
	responseActions[0].Key = "X"
	m, c := pendingModel(t, engine.Attack)
	if hint := m.cmdLine(120); !strings.Contains(hint, "[X]否决") {
		t.Fatalf("missing rebound key in %q", hint)
	}
	press(m, "n")
	if c.acted != 0 {
		t.Fatal("old response key still active")
	}
	press(m, "x")
	if c.acted != 1 || c.last.Type != engine.PlayNope {
		t.Fatalf("new response key did not play Nope: %+v", c.last)
	}
}

func TestHelpCanScrollWhenTerminalIsShort(t *testing.T) {
	m, _ := newTestModel(t, playingObs(), client.Playing)
	m.width, m.height = 40, 4
	press(m, "?")
	if !strings.Contains(strip(m.View()), "翻看") {
		t.Fatal("missing scroll indicator")
	}
	for i := 0; i < 30; i++ {
		m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !strings.Contains(strip(m.View()), "断开连接") {
		t.Fatalf("cannot reach end of help:\n%s", strip(m.View()))
	}
	press(m, "esc")
	if m.help || m.helpOffset != 0 {
		t.Fatal("escape did not close and reset help")
	}
}

func TestReboundCardKeysSelectTheirRegisteredPositions(t *testing.T) {
	old := cardAction
	t.Cleanup(func() { cardAction = old })
	cardAction.Key = "X"
	cardAction.Aliases = []string{"Y", "3", "4", "5"}
	m, c := newTestModel(t, playingObs(engine.Card{ID: 10, Kind: engine.Skip}, engine.Card{ID: 11, Kind: engine.Shuffle}), client.Playing)
	page, _ := m.pageCards()
	want := page[1].ID
	press(m, "2")
	if c.acted != 0 {
		t.Fatal("old key still selected a card")
	}
	press(m, "y")
	if c.acted != 1 || len(c.last.CardIDs) != 1 || c.last.CardIDs[0] != want {
		t.Fatalf("second card: %+v", c.last)
	}
}

func TestChatModalDoesNotDispatchTableKeys(t *testing.T) {
	m, c := newTestModel(t, playingObs(), client.Playing)
	m.step = stepChat
	layout, mode := m.opts.Layout, m.opts.Mode
	press(m, "i")
	press(m, "m")
	press(m, "?")
	if m.opts.Layout != layout || m.opts.Mode != mode || m.help || m.step != stepChat {
		t.Fatal("chat modal allowed an unrelated operation")
	}
	press(m, "1")
	if c.chat != 1 || m.step != stepIdle {
		t.Fatal("chat choice was not sent")
	}
	m.step = stepChat
	press(m, "esc")
	if m.step != stepIdle {
		t.Fatal("escape did not close chat")
	}
}

func TestActionScopesAreUnambiguous(t *testing.T) {
	combine := func(a, b []ui.Action) []ui.Action { return append(append([]ui.Action(nil), a...), b...) }
	// In the reinsert scope I belongs to the bomb-position action, not layout.
	var reinsertCommon []ui.Action
	for _, action := range commonActions {
		if action.ID != "layout" {
			reinsertCommon = append(reinsertCommon, action)
		}
	}
	for name, actions := range map[string][]ui.Action{
		"lobby":           combine(commonActions, lobbyActions),
		"playing":         combine(combine(commonActions, playActions), []ui.Action{cardAction}),
		"responding":      combine(commonActions, responseActions),
		"reinserting":     combine(reinsertCommon, reinsertActions),
		"custom-position": customPositionActions,
		"revealed":        combine(combine(commonActions, playActions), []ui.Action{cardAction, revealAction}),
		"help":            ui.HelpActions(commonActions, "help", "quit"),
	} {
		if err := ui.ValidateActions(actions); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestHelpKeyRebindingOpensAndClosesHelp(t *testing.T) {
	old := commonActions[0]
	t.Cleanup(func() { commonActions[0] = old })
	commonActions[0].Key = "H"
	m, _ := newTestModel(t, playingObs(), client.Playing)
	press(m, "h")
	if !m.help || !strings.Contains(strip(m.View()), "按 H / 或 Esc 关闭") {
		t.Fatal("help prompt did not follow rebound key")
	}
	press(m, "h")
	if m.help {
		t.Fatal("rebound help key did not close help")
	}
}

func TestActionOrderDoesNotChangeContextualLabels(t *testing.T) {
	oldResponse := slices.Clone(responseActions)
	oldReinsert := slices.Clone(reinsertActions)
	oldPlay := slices.Clone(playActions)
	t.Cleanup(func() { responseActions, reinsertActions, playActions = oldResponse, oldReinsert, oldPlay })
	slices.Reverse(responseActions)
	slices.Reverse(reinsertActions)
	slices.Reverse(playActions)
	response, _ := pendingModel(t, engine.Attack)
	if hint := response.cmdLine(120); !strings.Contains(hint, "[N]否决(2)") || !strings.Contains(hint, "[P]放弃") {
		t.Fatalf("response labels followed slice position: %q", hint)
	}
	o := playingObs(engine.Card{ID: 4, Kind: engine.Defuse})
	o.Phase = engine.Reinserting
	reinsert, _ := newTestModel(t, o, client.Playing)
	if hint := reinsert.cmdLine(120); !strings.Contains(hint, "[I]指定(0-14)") || strings.Contains(hint, "[0]牌堆顶(0-") {
		t.Fatalf("reinsert labels followed slice position: %q", hint)
	}
	o.Phase = engine.Playing
	playing, _ := newTestModel(t, o, client.Playing)
	if hint := playing.cmdLine(120); !strings.Contains(hint, "[G]对子盲抽(0)") || strings.Contains(hint, "[D]摸牌(0)") {
		t.Fatalf("play labels followed slice position: %q", hint)
	}
}

func TestActualCommandActionsAreUnambiguous(t *testing.T) {
	m, _ := newTestModel(t, playingObs(), client.Playing)
	for _, step := range []step{stepIdle, stepTarget, stepChat} {
		m.step = step
		actions, _ := m.commandActions()
		if err := ui.ValidateActions(actions); err != nil {
			t.Fatalf("step=%v: %v", step, err)
		}
	}
}

func TestTargetScopeRejectsConflictWithGlobalAction(t *testing.T) {
	old := commonActions[1]
	t.Cleanup(func() { commonActions[1] = old })
	commonActions[1].Key = "1"
	m, _ := newTestModel(t, playingObs(), client.Playing)
	m.step = stepTarget
	actions, _ := m.commandActions()
	if len(actions) != 1 || !strings.Contains(actions[0].Label, "按键配置错误") {
		t.Fatalf("missing active-scope error: %+v", actions)
	}
	if !strings.Contains(strip(m.View()), "按键配置错误") {
		t.Fatal("conflict was not visible to the player")
	}
	if _, ok := ui.ResolveAction(actions, "1"); ok {
		t.Fatal("conflicting scope dispatched an action")
	}
	layout := m.opts.Layout
	press(m, "1")
	if m.opts.Layout != layout || m.step != stepTarget {
		t.Fatal("common action bypassed conflicting scope")
	}
}

func TestHelpUsesCustomReinsertActions(t *testing.T) {
	o := playingObs(engine.Card{ID: 4, Kind: engine.Defuse})
	o.Phase = engine.Reinserting
	m, _ := newTestModel(t, o, client.Playing)
	m.step, m.help = stepDefuseCustom, true
	view := strip(m.View())
	if !strings.Contains(view, "[Enter]确认") || !strings.Contains(view, "[Esc]返回") {
		t.Fatalf("custom reinsert actions missing from help:\n%s", view)
	}
	if strings.Contains(view, "[0]牌堆顶") {
		t.Fatalf("preset actions must not replace custom controls:\n%s", view)
	}
}

func TestCustomPositionConfirmUsesRegisteredKey(t *testing.T) {
	old := customPositionActions[2]
	t.Cleanup(func() { customPositionActions[2] = old })
	customPositionActions[2].Key = "X"
	o := playingObs(engine.Card{ID: 5, Kind: engine.Defuse})
	o.Phase = engine.Reinserting
	m, c := newTestModel(t, o, client.Playing)
	m.step, m.pos = stepDefuseCustom, 3
	if !strings.Contains(strip(m.cmdLine(100)), "[X]确认") {
		t.Fatal("custom confirmation hint did not follow binding")
	}
	press(m, "enter")
	if c.acted != 0 {
		t.Fatal("old confirmation key still active")
	}
	press(m, "x")
	if c.acted != 1 || c.last.Type != engine.UseDefuse || c.last.Position != 3 {
		t.Fatalf("new key: %+v", c.last)
	}
}

func TestRevealDismissUsesRegisteredKey(t *testing.T) {
	old := revealAction
	t.Cleanup(func() { revealAction = old })
	revealAction.Key = "X"
	m, c := newTestModel(t, playingObs(), client.Playing)
	o := m.obs()
	o.Future = []engine.Card{{ID: 1, Kind: engine.Skip}}
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	if !strings.Contains(strip(m.cmdLine(100)), "[X]结束预言") {
		t.Fatal("reveal prompt did not follow binding")
	}
	press(m, "p")
	if len(m.revealed) == 0 {
		t.Fatal("old reveal key still active")
	}
	press(m, "x")
	if len(m.revealed) != 0 {
		t.Fatal("new reveal key did not dismiss the cards")
	}
}

func TestChatModalFitsSmallTerminalInLobbyAndGame(t *testing.T) {
	for _, phase := range []client.Phase{client.Lobby, client.Playing} {
		for _, layout := range []ui.Layout{ui.LayoutCompact, ui.LayoutRich} {
			m, _ := newTestModel(t, playingObs(), phase)
			m.width, m.height, m.step, m.opts.Layout = 40, 10, stepChat, layout
			view := strip(m.View())
			for _, text := range client.Presets {
				if !strings.Contains(view, text) {
					t.Fatalf("phase=%v layout=%v missing %q:\n%s", phase, layout, text, view)
				}
			}
			if !strings.Contains(view, "[Esc]取消") || strings.Contains(view, "[D]摸牌") {
				t.Fatalf("wrong modal controls:\n%s", view)
			}
		}
	}
}

func TestChatCanScrollInVeryShortTerminal(t *testing.T) {
	m, c := newTestModel(t, playingObs(), client.Playing)
	m.width, m.height, m.step = 40, 2, stepChat
	var seen strings.Builder
	for i := 0; i < 30; i++ {
		seen.WriteString(strip(m.View()))
		m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	}
	for _, text := range client.Presets {
		if !strings.Contains(seen.String(), text) {
			t.Fatalf("chat option %q unreachable", text)
		}
	}
	if m.step != stepChat || c.chat != 0 {
		t.Fatal("scrolling triggered a chat or closed its modal")
	}
}

func TestChatStartsAtTopAfterScrollingTable(t *testing.T) {
	m, _ := newTestModel(t, playingObs(), client.Playing)
	m.width, m.height = 40, 2
	m.View()
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	m.View()
	if m.frameOffset == 0 {
		t.Fatal("table did not scroll")
	}
	press(m, "t")
	view := strip(m.View())
	if m.frameOffset != 0 || !strings.Contains(view, "快捷语") {
		t.Fatalf("chat did not start at top: %q offset=%d", view, m.frameOffset)
	}
}

func TestLongStatusAndLobbyNoticeRemainReadable(t *testing.T) {
	message := "这是很长的错误提示必须完整显示最后的内容"
	for _, layout := range []ui.Layout{ui.LayoutCompact, ui.LayoutRich} {
		m, _ := newTestModel(t, playingObs(), client.Playing)
		m.width, m.height, m.opts.Layout = 24, 3, layout
		m.fail(message)
		status := strings.ReplaceAll(strip(m.statusLine(24)), "\n", "")
		if !strings.Contains(status, message) {
			t.Fatalf("status lost text: %q", status)
		}
		m.snap.Phase = client.Lobby
		m.snap.Notice = message
		lobby := strings.ReplaceAll(strip(m.viewLobby()), "\n", "")
		if !strings.Contains(lobby, message) {
			t.Fatalf("lobby lost text: %q", lobby)
		}
	}
}

func TestReinsertControlsFitSmallTerminal(t *testing.T) {
	for _, custom := range []bool{false, true} {
		o := playingObs(engine.Card{ID: 1, Kind: engine.Defuse}, engine.Card{ID: 2, Kind: engine.Skip}, engine.Card{ID: 3, Kind: engine.Attack}, engine.Card{ID: 4, Kind: engine.Shuffle}, engine.Card{ID: 5, Kind: engine.Favor})
		o.Phase = engine.Reinserting
		m, _ := newTestModel(t, o, client.Playing)
		m.width, m.height = 40, 10
		if custom {
			m.step = stepDefuseCustom
		}
		view := strip(m.View())
		want := []string{"[0]牌堆顶", "[B]牌堆底", "[I]指定"}
		if custom {
			want = []string{"[-]减小", "[=]增大", "[Enter]确认", "[Esc]返回"}
		}
		for _, token := range want {
			if !strings.Contains(view, token) {
				t.Fatalf("custom=%v missing %s:\n%s", custom, token, view)
			}
		}
	}
}
