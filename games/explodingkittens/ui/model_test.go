package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

type fakeClient struct {
	client.Client
	snap client.Snapshot
	last any
}

func (c *fakeClient) Snapshot() client.Snapshot       { return c.snap }
func (c *fakeClient) Updates() <-chan client.Snapshot { return nil }
func (c *fakeClient) Act(a any) error                 { c.last = a; return nil }

func TestCommandUsesStableIDsAndSeats(t *testing.T) {
	o := engine.Observation{Seat: 1, DeckCount: 5, Alive: []bool{true, true, true},
		Hand: []engine.Card{{ID: 42, Kind: engine.Favor}, {ID: 7}, {ID: 19}}}
	for _, tc := range []struct {
		command string
		kind    engine.ActionType
		ids     []int
		target  int
	}{
		{"draw", engine.Draw, nil, -1},
		{"pass", engine.Pass, nil, -1},
		{"play 1 3", engine.Play, []int{42}, 2},
		{"pair 2 3 1", engine.Play, []int{7, 19}, 0},
		{"triple 1 2 3 1 defuse", engine.Play, []int{42, 7, 19}, 0},
		{"nope 2", engine.PlayNope, []int{7}, -1},
		{"give 3", engine.Give, []int{19}, -1},
		{"defuse 2 0", engine.UseDefuse, []int{7}, -1},
	} {
		t.Run(tc.command, func(t *testing.T) {
			a, err := parseCommand(strings.Fields(tc.command), o)
			if err != nil || a.Seat != 1 || a.Type != tc.kind || a.Target != tc.target || len(a.CardIDs) != len(tc.ids) {
				t.Fatalf("parsed %+v: %v", a, err)
			}
			for i, id := range tc.ids {
				if a.CardIDs[i] != id {
					t.Fatal("hand index sent in place of card ID")
				}
			}
		})
	}
	for _, command := range []string{"play 0", "play 4", "pair 1 2 4", "defuse 1 6", "defuse 1 -1", "give", "draw 1", "nope 1 extra"} {
		if _, err := parseCommand(strings.Fields(command), o); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
}

func TestLayoutInitializedOnceAndSnapshotCommands(t *testing.T) {
	for _, layout := range []ui.Layout{ui.LayoutRich, ui.LayoutCompact} {
		s, _ := engine.NewGame(2, 1)
		c := &fakeClient{snap: client.Snapshot{Code: "ABC234", Phase: client.Playing, MySeat: 0,
			View: engine.Observe(s, 0), Players: []client.Player{{Name: "Alice"}, {Name: "Bob"}}}}
		saved := layout
		m := NewModel(c, Options{Key: "a", Mode: ui.ColorNone,
			LayoutOf:  func(string) (ui.Layout, bool) { return layout, true },
			SetLayout: func(key string, l ui.Layout) { saved = l },
		}, nil)
		if m.opts.Layout != layout {
			t.Fatal("settings not propagated")
		}
		m.input.SetValue("i")
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m.Update(client.SnapshotMsg(c.snap))
		if m.opts.Layout == layout || saved == layout {
			t.Fatal("snapshot overwrote in-game layout choice")
		}
		m.input.SetValue("draw")
		if !m.HasModal() {
			t.Fatal("gateway can intercept a command in progress")
		}
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if a, ok := c.last.(engine.Action); !ok || a.Type != engine.Draw || a.Seat != 0 {
			t.Fatal("command not sent")
		}
		if m.HasModal() {
			t.Fatal("command entry did not release modal")
		}
		m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > 40 {
				t.Fatalf("overflow: %q", line)
			}
		}
	}
}

func TestPrivateFutureAndBossScreen(t *testing.T) {
	s, _ := engine.NewGame(2, 1)
	s.Future[0] = []engine.Card{{ID: 50, Kind: engine.ExplodingKitten}}
	c := &fakeClient{snap: client.Snapshot{Code: "ABC234", Phase: client.Playing, MySeat: 0,
		View: engine.Observe(s, 0)}}
	m := NewModel(c, Options{Mode: ui.ColorNone}, nil)
	if !strings.Contains(m.View(), "exploding_kitten") {
		t.Fatal("private peek not rendered")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if strings.Contains(m.View(), "exploding_kitten") || strings.Contains(m.View(), "ABC234") {
		t.Fatal("boss screen exposed game")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	c.snap.MySeat, c.snap.View = 1, engine.Observe(s, 1)
	m.Update(client.SnapshotMsg(c.snap))
	if strings.Contains(m.View(), "仅你可见") {
		t.Fatal("opponent saw a private peek")
	}
}

func TestColorPreferenceAndPagedHand(t *testing.T) {
	o := engine.Observation{Seat: 0, Turn: 0, Phase: engine.Playing,
		Alive: []bool{true, true}, HandCounts: []int{40, 1}}
	for i := 0; i < 40; i++ {
		o.Hand = append(o.Hand, engine.Card{ID: i, Kind: engine.HairyPotatoCat})
	}
	c := &fakeClient{snap: client.Snapshot{Code: "ABC234", Phase: client.Playing, View: o}}
	m := NewModel(c, Options{ModeOf: func(string) (ui.ColorMode, bool) { return ui.ColorNone, true }}, nil)
	if m.opts.Mode != ui.ColorNone {
		t.Fatal("color preference required a layout callback")
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if !strings.Contains(m.View(), "PgUp/PgDn") || strings.Contains(m.View(), "40.hairy_potato_cat") {
		t.Fatal("large hand was not paged")
	}
	for range 50 {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if !strings.Contains(m.View(), "40.hairy_potato_cat") {
		t.Fatal("last hand card inaccessible")
	}
	// Close must not leave the room; fakeClient's inherited Leave would panic.
	m.Close()
}
