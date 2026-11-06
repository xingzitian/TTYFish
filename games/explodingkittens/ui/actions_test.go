package tui

import (
	"slices"
	"strings"
	"testing"

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
