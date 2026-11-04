package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/sdk/client"
)

func TestTurnFlowDirectionAndFixedPlayers(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true
	for _, tt := range []struct {
		dir    int
		frames []string
	}{
		{1, []string{"[>  ]", "[ > ]", "[  >]"}},
		{-1, []string{"[  <]", "[ < ]", "[<  ]"}},
	} {
		obs := m.obs()
		obs.Dir = tt.dir
		var players string
		for i, want := range tt.frames {
			m.turnFrame = i
			line := strip(m.compactTurn(obs))
			if !strings.HasPrefix(line, "[TURN] "+want+" ") {
				t.Fatalf("dir=%d frame=%d: %q", tt.dir, i, line)
			}
			tail := strings.TrimPrefix(line, "[TURN] "+want+" ")
			if i > 0 && tail != players {
				t.Fatalf("animation moved player names: %q != %q", tail, players)
			}
			players = tail
		}
	}
}

func TestTurnAnimationTickLifecycle(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true
	cmd := m.Init()
	if cmd == nil || !m.turnTickPending {
		t.Fatal("playing compact model must start animation at initialization")
	}
	if m.animateTurn() != nil {
		t.Fatal("must not schedule a second concurrent tick")
	}
	for i := 1; i <= 3; i++ {
		_, next := m.Update(turnTickMsg{model: m})
		if m.turnFrame != i%3 || next == nil || !m.turnTickPending {
			t.Fatalf("tick %d did not advance and reschedule", i)
		}
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	_, next := m.Update(turnTickMsg{model: m})
	if next != nil || m.turnTickPending {
		t.Fatal("boss screen must stop animation")
	}
	_, next = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next == nil || !m.turnTickPending {
		t.Fatal("returning to table must resume animation")
	}
	_, _ = m.Update(closedMsg{})
	_, next = m.Update(turnTickMsg{model: m})
	if next != nil || m.turnTickPending {
		t.Fatal("disconnection must stop animation")
	}
}

func TestTurnAnimationStartsWithPlayingSnapshot(t *testing.T) {
	snap := sampleSnap(20241029)
	m := testModel(t, 80, snap)
	m.minimal = true
	m.snap.Phase = client.Lobby
	if m.Init() != nil {
		t.Fatal("lobby must not animate")
	}
	_, cmd := m.Update(snapMsg(snap))
	if cmd == nil || !m.turnTickPending {
		t.Fatal("playing snapshot must start animation")
	}
	other := testModel(t, 80, snap)
	before := m.turnFrame
	_, _ = m.Update(turnTickMsg{model: other})
	if m.turnFrame != before {
		t.Fatal("tick from an old game model must be ignored")
	}

	m.snap.Phase = client.Lobby
	_, _ = m.Update(turnTickMsg{model: m})
	if m.turnTickPending {
		t.Fatal("finishing a round must stop animation")
	}
}

func TestTurnAnimationStopsForRichLayout(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true
	_ = m.Init()
	_, _ = m.Update(keyOf("i"))
	_, cmd := m.Update(turnTickMsg{model: m})
	if cmd != nil || m.turnTickPending {
		t.Fatal("rich layout must not keep compact animation running")
	}
	_, cmd = m.Update(keyOf("i"))
	if cmd == nil || !m.turnTickPending {
		t.Fatal("switching back to compact must resume animation")
	}
}
