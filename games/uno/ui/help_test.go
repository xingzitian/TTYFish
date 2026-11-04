package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestHelpOpensAndCloses ? 打开键位一览，?/Esc 关闭。
func TestHelpOpensAndCloses(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := playingState(minimal).modelWith(t)

		m.handleKey(keyOf("?"))
		if !m.help {
			t.Fatalf("[%s] ? 应该打开帮助", modeName(minimal))
		}
		if view := strip(m.View()); !strings.Contains(view, "键位一览") {
			t.Fatalf("[%s] 帮助没画出来：\n%s", modeName(minimal), view)
		}

		m.handleKey(keyOf("?"))
		if m.help {
			t.Fatalf("[%s] 再按 ? 应该关闭", modeName(minimal))
		}

		m.handleKey(keyOf("?"))
		m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
		if m.help {
			t.Fatalf("[%s] Esc 应该关闭帮助", modeName(minimal))
		}
	}
}

// TestSlashAlsoOpensHelp 斜杠键也要能呼出帮助（有人手更习惯按 /）。
func TestSlashAlsoOpensHelp(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := playingState(minimal).modelWith(t)

		m.handleKey(keyOf("/"))
		if !m.help {
			t.Fatalf("[%s] / 应该能打开帮助", modeName(minimal))
		}
		if view := strip(m.View()); !strings.Contains(view, "键位一览") {
			t.Fatalf("[%s] 帮助没画出来：\n%s", modeName(minimal), view)
		}

		m.handleKey(keyOf("/"))
		if m.help {
			t.Fatalf("[%s] 再按 / 应该关闭", modeName(minimal))
		}
	}
}

// TestHelpSwallowsGameKeys 看帮助时按出牌键不能真的出牌——最怕误触。
func TestHelpSwallowsGameKeys(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := playingState(minimal).modelWith(t)
		rec := newRecorder(m.snap)
		m.c = rec

		m.handleKey(keyOf("?"))
		for _, k := range []string{"1", "2", "d", "u", "c", "p", "v"} {
			m.handleKey(keyOf(k))
		}

		if len(rec.calls) != 0 {
			t.Fatalf("[%s] 帮助界面里按键不该发请求，实际 %v", modeName(minimal), rec.calls)
		}
		if !m.help {
			t.Fatalf("[%s] 无关按键不该关掉帮助", modeName(minimal))
		}
	}
}

// TestHelpIsModalForGateway 帮助必须算弹窗，否则网关会把 Esc 当成"离开房间"。
func TestHelpIsModalForGateway(t *testing.T) {
	m := playingState(false).modelWith(t)
	if m.HasModal() {
		t.Fatal("没开帮助时不该算弹窗")
	}
	m.handleKey(keyOf("?"))
	if !m.HasModal() {
		t.Fatal("帮助打开时必须算弹窗，否则 Esc 会被网关抢走")
	}
}

// TestHelpShowsPhaseKeys 大厅和牌桌的键表要不一样。
func TestHelpShowsPhaseKeys(t *testing.T) {
	playing := playingState(false).modelWith(t)
	playing.handleKey(keyOf("?"))
	if view := strip(playing.View()); !strings.Contains(view, "翻页") {
		t.Fatalf("牌桌帮助应该提到翻页：\n%s", view)
	}

	lobby := state{name: "大厅", snap: lobbySnap(), minimal: false}.modelWith(t)
	lobby.handleKey(keyOf("?"))
	view := strip(lobby.View())
	if strings.Contains(view, "翻页") {
		t.Fatalf("大厅里没有手牌，不该提翻页：\n%s", view)
	}
	if !strings.Contains(view, "开始游戏") {
		t.Fatalf("房主在大厅应该看到开始：\n%s", view)
	}
}

// TestHelpFitsWidth 帮助面板不能超宽。
func TestHelpFitsWidth(t *testing.T) {
	for _, w := range []int{80, 60, 45} {
		m := playingState(false).modelWith(t)
		m.width = w
		m.handleKey(keyOf("?"))
		for _, line := range strings.Split(strip(m.View()), "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("宽度 %d 帮助超宽：%q", w, line)
			}
		}
	}
}

// TestHelpListedInCmd 帮助入口必须在 CMD 里可见，而且窄屏也留得住。
func TestHelpListedInCmd(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := playingState(minimal).modelWith(t)
		m.width = 40
		if view := strip(m.View()); !strings.Contains(view, "?") {
			t.Fatalf("[%s] 窄屏下也必须有 ? 帮助入口：\n%s", modeName(minimal), view)
		}
	}
}

func (s state) modelWith(t *testing.T) *Model {
	t.Helper()
	m := testModel(t, 80, s.snap)
	m.minimal = s.minimal
	return m
}

func lobbySnap() client.Snapshot {
	snap := sampleSnap(7)
	snap.Phase = client.Lobby
	return snap
}

var _ = engine.Card{}
