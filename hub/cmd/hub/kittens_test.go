package main

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

type kittensLayoutClient struct{ layoutClient }

func (kittensLayoutClient) Snapshot() client.Snapshot {
	s, _ := engine.NewGame(2, 1)
	return client.Snapshot{Code: "ABC234", Phase: client.Playing, MySeat: 0, View: engine.Observe(s, 0)}
}

func TestSettingsInitializeProductionKittensLayout(t *testing.T) {
	for _, layout := range []ui.Layout{ui.LayoutRich, ui.LayoutCompact} {
		saved := layout
		m := newKittensUI(registry.UIContext{
			Key: "account:alice", Output: &bytes.Buffer{}, Mode: ui.ColorNone,
			Client: kittensLayoutClient{}, Layout: layout,
			SetLayout: func(key string, l ui.Layout) {
				if key != "account:alice" {
					t.Fatal("wrong settings key")
				}
				saved = l
			},
		})
		rich := func() bool { return strings.Contains(m.View(), "triple") }
		if rich() != (layout == ui.LayoutRich) {
			t.Fatal("production UI did not receive display setting")
		}
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m.Update(client.SnapshotMsg(kittensLayoutClient{}.Snapshot()))
		if saved == layout || rich() != (saved == ui.LayoutRich) {
			t.Fatal("snapshot overwrote game-local choice")
		}
	}
}
