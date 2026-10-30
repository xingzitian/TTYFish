package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestColorModes 三种颜色深浅各自该产生什么转义，而且是**全局**的：
// 无色模式下整个界面（含面板、表头、提示）都不该出现颜色码。
func TestColorModes(t *testing.T) {
	cases := []struct {
		mode   ui.ColorMode
		bg, fg bool
	}{
		{ui.ColorBlock, true, true},
		{ui.ColorText, false, true},
		{ui.ColorNone, false, false},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		m := testModel(t, 100, sampleSnap(20241029))
		m.renderer = NewRenderer(&buf, "xterm-256color")
		m.mode = tc.mode
		m.height = 30

		out := m.View()
		hasBg := strings.Contains(out, "48;")
		hasFg := strings.Contains(out, "38;")
		if hasBg != tc.bg || hasFg != tc.fg {
			t.Fatalf("%v 模式：期望 bg=%v fg=%v，实际 bg=%v fg=%v",
				tc.mode, tc.bg, tc.fg, hasBg, hasFg)
		}
	}
}

func TestParseColorMode(t *testing.T) {
	for _, s := range []string{"", "block", "full", "满色", "色块"} {
		if f, ok := ui.ParseColorMode(s); !ok || f != ui.ColorBlock {
			t.Fatalf("%q 应该解析成满色", s)
		}
	}
	if f, ok := ui.ParseColorMode("text"); !ok || f != ui.ColorText {
		t.Fatal("text 应该解析成文字模式")
	}
	for _, s := range []string{"plain", "none", "mono"} {
		if f, ok := ui.ParseColorMode(s); !ok || f != ui.ColorNone {
			t.Fatalf("%q 应该解析成无色模式", s)
		}
	}
	if _, ok := ui.ParseColorMode("rainbow"); ok {
		t.Fatal("非法样式应该被拒")
	}
	if ui.ColorBlock.Next() != ui.ColorText || ui.ColorText.Next() != ui.ColorNone || ui.ColorNone.Next() != ui.ColorBlock {
		t.Fatal("应该按 满色→文字→无色→满色 循环")
	}
}

// TestSidePanelAt80 是"右侧面板看不到了"那个问题的回归测试：
// 默认 80 列的终端也必须能看到玩家列表和出牌记录。
func TestSidePanelAt80(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.height = 24

	out := strip(m.View())
	for _, want := range []string{"玩家", "方向", "出牌记录"} {
		if !strings.Contains(out, want) {
			t.Fatalf("80 列下右侧面板缺少 %q:\n%s", want, out)
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Errorf("第 %d 行超宽: %d（%q）", i+1, got, line)
		}
	}
	t.Logf("\n%s", out)
}

// TestShortTerminalKeepsPanel 矮终端（宽而矮）走三栏：
// 面板是横着并排的，占的是宽度，高度只决定里面放几行。
func TestShortTerminalKeepsPanel(t *testing.T) {
	m := testModel(t, 200, sampleSnap(1))
	m.height = 12

	out := strip(m.View())
	for _, want := range []string{"玩家", "出牌记录", "方向"} {
		if !strings.Contains(out, want) {
			t.Fatalf("12 行高时也应该保留 %q:\n%s", want, out)
		}
	}
	// 三栏：玩家和出牌记录各占一栏，而不是挤在同一个面板里。
	if strings.Count(out, "出牌记录") != 1 {
		t.Fatalf("出牌记录应该只出现一次:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > 200 {
			t.Errorf("第 %d 行超宽: %d", i+1, got)
		}
	}
	t.Logf("\n%s", out)
}

// TestUltraWideIsCapped 超宽终端下内容不能跟着拉满，而且是靠左不是居中。
func TestUltraWideIsCapped(t *testing.T) {
	m := testModel(t, 200, sampleSnap(20241029))
	m.height = 30

	out := strip(m.View())
	maxW, firstIndent := 0, -1
	for i, line := range strings.Split(out, "\n") {
		w := lipgloss.Width(line)
		if w > maxW {
			maxW = w
		}
		if firstIndent < 0 && strings.TrimSpace(line) != "" {
			firstIndent = len(line) - len(strings.TrimLeft(line, " "))
		}
		if w > 200 {
			t.Fatalf("第 %d 行超出终端宽度: %d", i+1, w)
		}
	}
	if maxW > 170 {
		t.Fatalf("200 列终端下内容被拉到 %d 列，应该收窄", maxW)
	}
	if firstIndent != 0 {
		t.Fatalf("内容应该靠左（首行缩进 %d）", firstIndent)
	}
}

// TestTooSmallTerminal 低于下限时明确拒绝，而不是画出一个残缺的界面。
func TestTooSmallTerminal(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{39, 24},
		{80, 9},
		{30, 8},
	} {
		m := testModel(t, tc.w, sampleSnap(1))
		m.height = tc.h
		out := strip(m.View())
		if !strings.Contains(out, "终端太小") {
			t.Fatalf("%d×%d 应该提示终端太小:\n%s", tc.w, tc.h, out)
		}
		if !strings.Contains(out, "至少要 40×10") {
			t.Fatalf("%d×%d 的提示里应该写明下限:\n%s", tc.w, tc.h, out)
		}
		for i, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > tc.w {
				t.Errorf("%d×%d 第 %d 行超宽（%q）", tc.w, tc.h, i+1, line)
			}
		}
	}
}

// TestMinimumSizeStillPlays 正好等于下限时要正常渲染。
func TestMinimumSizeStillPlays(t *testing.T) {
	m := testModel(t, minWidth, sampleSnap(20241029))
	m.height = minHeight
	out := strip(m.View())
	if strings.Contains(out, "终端太小") {
		t.Fatalf("40×10 已经达到下限，不该被拒:\n%s", out)
	}
	if !strings.Contains(out, "你的手牌") {
		t.Fatalf("40×10 应该能正常画出手牌:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > minWidth {
			t.Errorf("第 %d 行超宽: %d（%q）", i+1, lipgloss.Width(line), line)
		}
	}
	t.Logf("\n%s", out)
}

// TestFrameAlwaysFits 整帧永远不能超出终端尺寸。
//
// 超宽/超高的后果不是"看不清"，而是终端自己折行滚动，把顶部顶歪——
// 现场表现就是"消息一长，上面的内容就错位了"。所以这条是兜底断言。
func TestFrameAlwaysFits(t *testing.T) {
	snap := sampleSnap(1)
	snap.Players[1].Name = strings.Repeat("超长昵称", 12)
	snap.Timeline = []client.TimelineEntry{
		{AtMS: 61_000, Seat: 1, Text: strings.Repeat("很长很长的消息", 12)},
		{AtMS: 62_000, Seat: -1, Text: strings.Repeat("系统提示也很长", 12)},
	}

	m := testModel(t, 40, snap)
	m.height = 8
	m.errMsg = strings.Repeat("很长的错误信息", 20)

	out := strip(m.View())
	lines := strings.Split(out, "\n")
	if len(lines) > 8 {
		t.Fatalf("帧高 %d 超过终端 8 行", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("第 %d 行宽 %d 超过终端 40 列（%q）", i+1, w, line)
		}
	}
}

// TestFrameFitsBossAndLobby 老板键、大厅这些界面同样受尺寸约束。
func TestFrameFitsBossAndLobby(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *Model
	}{
		{"老板键", func() *Model {
			m := testModel(t, 40, sampleSnap(1))
			m.height = 6
			m.boss = true
			return m
		}()},
		{"大厅", func() *Model {
			snap := sampleSnap(1)
			snap.Phase = client.Lobby
			snap.View = engine.Observation{}
			snap.Players[1].Name = strings.Repeat("超长昵称", 12)
			m := testModel(t, 40, snap)
			m.height = 6
			return m
		}()},
	} {
		out := strip(tc.m.View())
		lines := strings.Split(out, "\n")
		if len(lines) > 6 {
			t.Fatalf("%s：帧高 %d 超过 6 行", tc.name, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(line) > 40 {
				t.Fatalf("%s：第 %d 行超宽（%q）", tc.name, i+1, line)
			}
		}
	}
}

// TestNoSidePanelWhenTooNarrow 真的放不下时才退回单栏。
func TestNoSidePanelWhenTooNarrow(t *testing.T) {
	m := testModel(t, 60, sampleSnap(20241029))
	m.height = 24
	out := strip(m.View())
	if strings.Contains(out, "出牌记录") {
		t.Fatalf("60 列不该强行塞右侧面板:\n%s", out)
	}
}
