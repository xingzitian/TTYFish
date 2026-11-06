package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestRegisteredDrawKeyDrivesBothLayoutsAndInput(t *testing.T) {
	old := playActions[1]
	t.Cleanup(func() { playActions[1] = old })
	playActions[1].Key = "X"
	for _, minimal := range []bool{false, true} {
		m, rec := playingState(minimal).model()
		var hint string
		if minimal {
			hint = m.compactCmd(m.obs())
		} else {
			hint = m.viewStatus()
		}
		if !strings.Contains(hint, "[X]摸牌") {
			t.Fatalf("minimal=%v: missing rebound key in %q", minimal, hint)
		}
		m.handleKey(keyOf("d"))
		if len(rec.calls) != 0 {
			t.Fatalf("old key still active: %v", rec.calls)
		}
		m.handleKey(keyOf("x"))
		if len(rec.calls) != 1 || !strings.Contains(rec.calls[0], "DrawCard") {
			t.Fatalf("new key calls = %v", rec.calls)
		}
	}
}

func TestActionScopesAreUnambiguous(t *testing.T) {
	combine := func(a, b []ui.Action) []ui.Action { return append(append([]ui.Action(nil), a...), b...) }
	for name, actions := range map[string][]ui.Action{
		"lobby":         combine(commonActions, lobbyActions),
		"playing":       combine(combine(commonActions, playActions), []ui.Action{cardAction}),
		"challenge":     challengeActions,
		"color":         colorActions,
		"layout-choice": layoutChoiceActions,
		"help":          ui.HelpActions(commonActions, "?", "q"),
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
	m, _ := playingState(false).model()
	m.handleKey(keyOf("h"))
	if !m.help || !strings.Contains(strip(m.View()), "按 H / 或 Esc 关闭") {
		t.Fatal("help prompt did not follow rebound key")
	}
	m.handleKey(keyOf("h"))
	if m.help {
		t.Fatal("rebound help key did not close help")
	}
}

func TestLayoutChoiceUsesRegisteredKey(t *testing.T) {
	old := layoutChoiceActions[0]
	t.Cleanup(func() { layoutChoiceActions[0] = old })
	layoutChoiceActions[0].Key = "X"
	m, _ := playingState(false).model()
	m.needLayoutChoice = true
	if !strings.Contains(strip(m.View()), "[X]丰富") {
		t.Fatal("layout prompt did not follow binding")
	}
	m.handleKey(keyOf("1"))
	if !m.needLayoutChoice {
		t.Fatal("old layout selection still active")
	}
	m.handleKey(keyOf("x"))
	if m.needLayoutChoice || m.minimal {
		t.Fatal("new layout key did not select rich layout")
	}
}

func TestLobbyBindingDrivesBothLayoutsAndInput(t *testing.T) {
	old := lobbyActions[0]
	t.Cleanup(func() { lobbyActions[0] = old })
	lobbyActions[0].Key = "X"
	for _, compact := range []bool{false, true} {
		m, rec := lobbyState(compact).model()
		if !strings.Contains(strip(m.View()), "[X]开始") {
			t.Fatalf("compact=%v: missing rebound lobby action", compact)
		}
		m.handleKey(keyOf("s"))
		if len(rec.calls) != 0 {
			t.Fatalf("compact=%v: old key still active", compact)
		}
		m.handleKey(keyOf("x"))
		if len(rec.calls) != 1 || rec.calls[0] != "start" {
			t.Fatalf("compact=%v: calls=%v", compact, rec.calls)
		}
	}
}

func TestActualTableActionsAreUnambiguousInBothLayouts(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m, _ := playingState(compact).model()
		if err := ui.ValidateActions(m.tableActions()); err != nil {
			t.Fatalf("compact=%v: %v", compact, err)
		}
	}
}

func TestHelpUsesCurrentTurnAndLayoutActions(t *testing.T) {
	for _, compact := range []bool{false, true} {
		state := playingState(compact)
		setObs(&state.snap, func(o *engine.Observation) { o.Turn = 1 })
		m, _ := state.model()
		m.help = true
		view := strip(m.View())
		if strings.Contains(view, "[D]摸牌") || strings.Contains(view, "出当前页第几张") {
			t.Fatalf("compact=%v: unavailable turn actions in help:\n%s", compact, view)
		}
		if !strings.Contains(view, "[U]UNO") || !strings.Contains(view, "[C]抓包") {
			t.Fatalf("compact=%v: missing off-turn actions:\n%s", compact, view)
		}
	}
}
