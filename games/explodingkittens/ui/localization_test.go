package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestChineseCardRequestsKeepWireIdentifiers(t *testing.T) {
	o := engine.Observation{Seat: 0, Alive: []bool{true, true},
		Hand: []engine.Card{{ID: 10}, {ID: 20}, {ID: 30}}}
	seen := map[engine.Kind]bool{}
	for _, card := range engine.NewDeck() {
		if seen[card.Kind] {
			continue
		}
		seen[card.Kind] = true
		for _, name := range []string{card.Kind.Name(), string(card.Kind)} {
			a, err := parseCommand(strings.Fields("triple 1 2 3 2 "+name), o)
			if err != nil || a.Requested != card.Kind {
				t.Fatalf("request %q: %+v %v", name, a, err)
			}
			raw, _ := json.Marshal(a)
			if !strings.Contains(string(raw), `"requested":"`+string(card.Kind)+`"`) {
				t.Fatalf("localized protocol identifier: %s", raw)
			}
		}
	}
	if _, err := parseCommand(strings.Fields("triple 1 2 3 2 不存在的牌"), o); err == nil {
		t.Fatal("unknown Chinese card accepted")
	}
}

func TestChineseCardsPhasesAndResponseDetails(t *testing.T) {
	o := engine.Observation{Seat: 0, Turn: 0, Alive: []bool{true, true}, HandCounts: []int{13, 1},
		Future: []engine.Card{{Kind: engine.ExplodingKitten}},
		Pending: &engine.Effect{Kind: engine.TacoCat, Action: engine.Action{
			Seat: 0, Target: 1, CardIDs: []int{1, 2, 3}, Requested: engine.Defuse,
		}},
	}
	seen := map[engine.Kind]bool{}
	for _, card := range engine.NewDeck() {
		if !seen[card.Kind] {
			o.Hand = append(o.Hand, card)
			seen[card.Kind] = true
		}
	}
	for _, tc := range []struct {
		phase engine.Phase
		label string
	}{
		{engine.Playing, "出牌中"},
		{engine.Responding, "等待反制"},
		{engine.Giving, "等待交牌"},
		{engine.Reinserting, "等待拆弹放回"},
	} {
		t.Run(string(tc.phase), func(t *testing.T) {
			o.Phase = tc.phase
			c := &fakeClient{snap: client.Snapshot{Code: "ABC234", Phase: client.Playing, View: o}}
			m := NewModel(c, Options{Mode: ui.ColorNone}, nil)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			view := m.View()
			if !strings.Contains(view, tc.label) {
				t.Fatalf("missing Chinese phase: %s", view)
			}
			for _, card := range o.Hand {
				if !strings.Contains(view, card.Kind.Name()) || strings.Contains(view, "."+string(card.Kind)) {
					t.Fatalf("untranslated hand %s: %s", card.Kind, view)
				}
			}
			if tc.phase == engine.Responding && !strings.Contains(view, "索要 拆弹") {
				t.Fatal("requested card not localized")
			}
			m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
			for _, line := range strings.Split(m.View(), "\n") {
				if lipgloss.Width(line) > 40 {
					t.Fatalf("Chinese text overflow: %q", line)
				}
			}
		})
	}
}
