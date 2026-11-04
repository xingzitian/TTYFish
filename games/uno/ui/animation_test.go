package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

func TestTurnFlowAtRightOfPlayers(t *testing.T) {
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
			if !strings.Contains(line, "方向: ") || !strings.HasSuffix(line, want) {
				t.Fatalf("dir=%d frame=%d: %q", tt.dir, i, line)
			}
			tail := strings.TrimSuffix(line, want)
			if i > 0 && tail != players {
				t.Fatalf("animation moved player names: %q != %q", tail, players)
			}
			players = tail
		}
	}
}

func TestTurnShowsThreeCompleteBadges(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		m := testModel(t, width, sampleSnap(20241029))
		m.minimal = true
		m.renderer = NewRenderer(io.Discard, "xterm-256color")
		setObs(&m.snap, func(o *engine.Observation) { o.Turn = 1 })
		line := m.compactTurn(m.obs())
		plain := strip(line)
		if strings.Count(plain, "(") != 3 || strings.Count(plain, ")") != 3 {
			t.Fatalf("width=%d must show three complete counts: %q", width, plain)
		}
		if strings.Contains(plain, "张") || !strings.Contains(plain, "*") {
			t.Fatalf("unexpected active-player/count format: %q", plain)
		}
		if !strings.Contains(line, "\x1b[") || !strings.Contains(line, "226") {
			t.Fatalf("current player must use yellow: %q", line)
		}
	}
}

func TestRapidTurnChangesDoNotAccumulateScroll(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true
	for i := 0; i < 20; i++ {
		next := m.snap
		setObs(&next, func(o *engine.Observation) { o.Turn = (o.Turn + 1) % 4 })
		_, _ = m.Update(snapMsg(next))
		if m.turnOffset < -12 || m.turnOffset > 12 {
			t.Fatalf("rapid turns accumulated animation: %d", m.turnOffset)
		}
	}
	for i := 0; i < 12; i++ {
		_, _ = m.Update(turnTickMsg{model: m})
	}
	if m.turnOffset != 0 {
		t.Fatal("queue must settle within 12 ticks")
	}
}

func TestTurnMarqueeSlidesAndCenters(t *testing.T) {
	for _, dir := range []int{1, -1} {
		snap := sampleSnap(20241029)
		setObs(&snap, func(o *engine.Observation) { o.Turn = 0; o.Dir = dir })
		m := testModel(t, 80, snap)
		m.minimal = true
		m.snap.Players[1].Name = "中文玩家"
		next := m.snap
		setObs(&next, func(o *engine.Observation) {
			if dir > 0 {
				o.Turn = 1
			} else {
				o.Turn = 3
			}
		})
		_, _ = m.Update(snapMsg(next))
		if m.turnOffset*dir <= 0 {
			t.Fatalf("dir=%d wrong scroll offset %d", dir, m.turnOffset)
		}
		before := strip(m.turnMarquee(m.obs(), 60))
		offset := m.turnOffset
		_, _ = m.Update(turnTickMsg{model: m})
		if m.turnOffset != offset-dir {
			t.Fatalf("must move one cell per tick: %d -> %d", offset, m.turnOffset)
		}
		if after := strip(m.turnMarquee(m.obs(), 60)); after == before {
			t.Fatal("player queue did not scroll")
		}
		for i := 0; m.turnOffset != 0 && i < 200; i++ {
			_, _ = m.Update(turnTickMsg{model: m})
		}
		tape, centers := m.turnTape(m.obs())
		if m.turnOffset != 0 || lipgloss.Width(tape) == 0 {
			t.Fatal("queue did not settle")
		}
		line := strip(m.turnMarquee(m.obs(), 60))
		badge := strip(m.turnBadge(m.obs(), m.obs().Turn, 19))
		at := strings.Index(line, badge)
		if at < 0 || lipgloss.Width(line[:at])+lipgloss.Width(badge)/2 < 29 ||
			lipgloss.Width(line[:at])+lipgloss.Width(badge)/2 > 30 {
			t.Fatalf("current player not centered: %q (centers %v)", line, centers)
		}
		_, _ = m.Update(turnTickMsg{model: m})
		if line != strip(m.turnMarquee(m.obs(), 60)) {
			t.Fatal("settled queue must stay still")
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

func TestTurnMarqueeFitsNarrowAndWideTerminals(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		m := testModel(t, width, sampleSnap(20241029))
		m.minimal = true
		m.snap.Players[1].Name = "中文玩家"
		for offset := -20; offset <= 20; offset++ {
			m.turnOffset = offset
			line := strip(m.compactTurn(m.obs()))
			if got := lipgloss.Width(line); got != width {
				t.Fatalf("width=%d offset=%d got=%d: %q", width, offset, got, line)
			}
		}
	}
}
