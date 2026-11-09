package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestCompactLinesFitRealWidth 极简模式每一行都必须真的放得下。
//
// 这是一次实拍事故：终端比较窄时，TURN 行的"| 方向: →"被切掉，
// CMD 行被切成 "[Tab]玩家 ["。根因是当时按"字符数"判断宽度，
// 而中文和全角符号占两列，所以判断偏乐观。
func TestCompactLinesFitRealWidth(t *testing.T) {
	for _, w := range []int{80, 72, 60, 50} {
		snap := sampleSnap(20241029)
		setObs(&snap, func(o *engine.Observation) {
			o.HandCounts = []int{5, 3, 6}
			o.SeatCount = 3
			o.Turn = 1
			o.Seat = 0
		})
		m := testModel(t, w, snap)
		m.minimal = true

		out := strip(m.View())
		for i, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("宽度 %d 时第 %d 行超宽 %d：%q", w, i+1, got, line)
			}
		}
	}
}

// TestCompactTurnKeepsDirectionWhenPossible 宽度够的时候方向必须保留。
func TestCompactTurnKeepsDirectionWhenPossible(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.HandCounts = []int{5, 3, 6}
		o.SeatCount = 3
		o.Turn = 0
		o.Dir = 1
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	if !strings.Contains(out, "方向") || !strings.Contains(out, "→") {
		t.Fatalf("80 列下方向不该被丢：\n%s", out)
	}
}

// TestCompactTurnDropsDirectionFirst 窄终端上先丢方向，而不是把行截断。
func TestCompactTurnDropsDirectionFirst(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.HandCounts = []int{5, 3, 6}
		o.SeatCount = 3
		o.Turn = 0
	})
	m := testModel(t, 40, snap)
	m.minimal = true

	out := strip(m.View())
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("40 列下超宽：%q", line)
		}
	}
	// 人还在，方向可以没有（新格式是"名字(张数)"，不再带"张"字）
	if !strings.Contains(out, "[TURN]") || !strings.Contains(out, "你") {
		t.Fatalf("TURN 行应该还在：\n%s", out)
	}
}

// TestCompactCmdFitsWithCJK 中文按键提示按显示宽度收缩，不能出现半截项。
func TestCompactCmdFitsWithCJK(t *testing.T) {
	for _, w := range []int{72, 60, 50} {
		snap := sampleSnap(20241029)
		m := testModel(t, w, snap)
		m.minimal = true

		out := strip(m.View())
		var cmd string
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "[CMD]") {
				cmd = line
			}
		}
		if cmd == "" {
			t.Fatalf("宽度 %d：没有 CMD 行\n%s", w, out)
		}
		if got := lipgloss.Width(cmd); got > w {
			t.Fatalf("宽度 %d：CMD 行超宽 %d：%q", w, got, cmd)
		}
		// 不能出现被切一半的方括号
		if strings.Count(cmd, "[") != strings.Count(cmd, "]") {
			t.Fatalf("宽度 %d：CMD 行有半截项：%q", w, cmd)
		}
	}
}

var _ = client.Playing

func TestCompactCommandsWrapWithoutDroppingKeys(t *testing.T) {
	for _, height := range []int{10, 24} {
		m := testModel(t, 40, sampleSnap(20241029))
		m.minimal, m.height = true, height
		view := strip(m.View())
		for _, token := range []string{"[1-5]出牌", "[D]摸牌", "[C]抓包", "[Tab]玩家", "[I]界面"} {
			if !strings.Contains(view, token) {
				t.Fatalf("height %d: missing %s\n%s", height, token, view)
			}
		}
	}
}

func TestCompactModalCommandsWrapWithoutDroppingKeys(t *testing.T) {
	challenge := testModel(t, 40, challengeSnap())
	challenge.minimal, challenge.height = true, 10
	view := strip(challenge.View())
	for _, token := range []string{"[Y]认罚摸4", "[N]质疑"} {
		if !strings.Contains(view, token) {
			t.Fatalf("missing %s\n%s", token, view)
		}
	}
	color := testModel(t, 40, sampleSnap(20241029))
	color.minimal, color.height, color.choosing = true, 10, true
	view = strip(color.View())
	for _, token := range []string{"[1]红", "[2]黄", "[3]蓝", "[4]绿", "[Esc]取消"} {
		if !strings.Contains(view, token) {
			t.Fatalf("missing %s\n%s", token, view)
		}
	}
}

func TestRichTableCommandsFitAtMinimumTerminalSize(t *testing.T) {
	m, _ := playingState(false).model()
	m.width, m.height = 40, 10
	view := strip(m.View())
	for _, token := range []string{"[?]帮助", "[D]摸牌", "[C]抓包", "[Q]断开"} {
		if !strings.Contains(view, token) {
			t.Fatalf("missing %s at 40x10:\n%s", token, view)
		}
	}
}

func TestChatModalFitsBothLayoutsAndLobby(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, lobby := range []bool{false, true} {
			state := playingState(compact)
			if lobby {
				state = lobbyState(compact)
			}
			m, _ := state.model()
			m.width, m.height, m.chat = 40, 10, true
			view := strip(m.View())
			for _, text := range client.Presets {
				if !strings.Contains(view, text) {
					t.Fatalf("compact=%v lobby=%v missing %q:\n%s", compact, lobby, text, view)
				}
			}
			if !strings.Contains(view, "取消") || strings.Contains(view, "[D]摸牌") {
				t.Fatalf("wrong modal controls:\n%s", view)
			}
		}
	}
}

func TestCompactChatCanScrollInVeryShortTerminal(t *testing.T) {
	m, rec := playingState(true).model()
	m.width, m.height, m.chat = 40, 2, true
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
	if !m.chat || len(rec.calls) != 0 {
		t.Fatal("scrolling triggered a chat or closed its modal")
	}
}

func TestChatStartsAtTopAfterScrollingTable(t *testing.T) {
	m, _ := playingState(true).model()
	m.width, m.height = 40, 2
	m.View()
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	m.View()
	if m.frameOffset == 0 {
		t.Fatal("table did not scroll")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	view := strip(m.View())
	if m.frameOffset != 0 || !strings.Contains(view, "快捷语") {
		t.Fatalf("chat did not start at top: %q offset=%d", view, m.frameOffset)
	}
}
