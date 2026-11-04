package tui

import (
	"strings"
	"testing"

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
