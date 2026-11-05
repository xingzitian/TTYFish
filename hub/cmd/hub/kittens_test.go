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

// TestSettingsInitializeProductionKittensLayout 确认两件事：
//  1. 网关把界面密度偏好传给了游戏界面
//  2. 后续快照不会把玩家在游戏里按 I 改过的选择顶掉
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

		m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		// 丰富模式在足够宽时有右侧栏，窄屏降级不改变偏好。
		rich := func() bool { return strings.Contains(m.View(), "出牌记录") }
		if rich() != (layout == ui.LayoutRich) {
			t.Fatalf("production UI did not receive display setting (layout=%v)", layout)
		}

		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
		m.Update(client.SnapshotMsg(kittensLayoutClient{}.Snapshot()))
		if saved == layout || rich() != (saved == ui.LayoutRich) {
			t.Fatal("snapshot overwrote game-local choice")
		}
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		if rich() {
			t.Fatal("narrow window did not hide the side panel")
		}
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		if rich() != (saved == ui.LayoutRich) {
			t.Fatal("narrow fallback overwrote layout preference")
		}
	}
}
