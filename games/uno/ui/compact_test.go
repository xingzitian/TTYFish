package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestSortHandOrder 是极简模式的排序契约：
// 红 → 黄 → 蓝 → 绿 → 万能，同色内按 1..9 → S → R → +2 → +4。
func TestSortHandOrder(t *testing.T) {
	cards := []engine.Card{
		{ID: 10, Color: engine.NoColor, Kind: engine.WildDrawFour},
		{ID: 9, Color: engine.Green, Kind: engine.Skip},
		{ID: 8, Color: engine.Blue, Kind: engine.DrawTwo},
		{ID: 7, Color: engine.Blue, Kind: engine.Number, Num: 8},
		{ID: 6, Color: engine.Blue, Kind: engine.Number, Num: 5},
		{ID: 5, Color: engine.Yellow, Kind: engine.Number, Num: 9},
		{ID: 4, Color: engine.Red, Kind: engine.Number, Num: 4},
		{ID: 3, Color: engine.Red, Kind: engine.Number, Num: 3},
		{ID: 2, Color: engine.Red, Kind: engine.Number, Num: 1},
	}
	got := sortHand(cards)
	want := []int{2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("第 %d 张应该是 id=%d，实际 id=%d（顺序：%v）", i+1, id, got[i].ID, ids(got))
		}
	}
}

// TestSortHandStable 同色同面值时按 id 排，避免刷新时两张牌来回跳。
func TestSortHandStable(t *testing.T) {
	cards := []engine.Card{
		{ID: 30, Color: engine.Red, Kind: engine.Number, Num: 5},
		{ID: 10, Color: engine.Red, Kind: engine.Number, Num: 5},
		{ID: 20, Color: engine.Red, Kind: engine.Number, Num: 5},
	}
	got := sortHand(cards)
	for i, want := range []int{10, 20, 30} {
		if got[i].ID != want {
			t.Fatalf("同色同值应该按 id 升序，第 %d 张是 %d", i+1, got[i].ID)
		}
	}
}

// TestCompactLines 极简模式的形状：不超过 6 行，每行不超宽。
func TestCompactLines(t *testing.T) {
	for _, w := range []int{80, 60, 40} {
		snap := sampleSnap(20241029)
		m := testModel(t, w, snap)
		m.minimal = true

		out := strip(m.View())
		lines := strings.Split(out, "\n")
		if len(lines) > 6 {
			t.Fatalf("宽度 %d：行数 %d 太多\n%s", w, len(lines), out)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("宽度 %d 第 %d 行超宽 %d: %q", w, i+1, got, line)
			}
		}
		if !strings.Contains(out, "[TURN]") || !strings.Contains(out, "[HAND]") || !strings.Contains(out, "[CMD]") {
			t.Errorf("宽度 %d：缺少必需的段\n%s", w, out)
		}
	}
}

// TestCompactPaging 手牌超过一页时，左右键翻页并且页码正确。
func TestCompactPaging(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = nil
		for i := 0; i < 12; i++ {
			o.Hand = append(o.Hand, engine.Card{ID: i, Color: engine.Red, Kind: engine.Number, Num: i % 10})
		}
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	if !strings.Contains(out, "1/3") {
		t.Fatalf("12 张牌应该是 1/3 页：\n%s", out)
	}
	// 第一页只有 5 张
	if strings.Contains(out, "6.[") {
		t.Fatalf("第一页不该出现第 6 张：\n%s", out)
	}

	m.handleKey(keyOf("right"))
	if out := strip(m.View()); !strings.Contains(out, "2/3") {
		t.Fatalf("按右方向键应该到 2/3：\n%s", out)
	}
	m.handleKey(keyOf("right"))
	m.handleKey(keyOf("right")) // 越界不能再翻
	if out := strip(m.View()); !strings.Contains(out, "3/3") {
		t.Fatalf("最多停在 3/3：\n%s", out)
	}
	m.handleKey(keyOf("left"))
	if out := strip(m.View()); !strings.Contains(out, "2/3") {
		t.Fatalf("按左方向键应该回 2/3：\n%s", out)
	}
}

// TestCompactPlayerPanel 按 Tab 弹出玩家面板，并且能自动收回。
func TestCompactPlayerPanel(t *testing.T) {
	snap := sampleSnap(20241029)
	m := testModel(t, 80, snap)
	m.minimal = true

	if strings.Contains(strip(m.View()), "[PLAYER]") {
		t.Fatal("默认不该显示玩家面板")
	}
	m.handleKey(keyOf("tab"))
	if out := strip(m.View()); !strings.Contains(out, "[PLAYER]") {
		t.Fatalf("按 Tab 应该弹出玩家面板：\n%s", out)
	}

	// 面板过期的消息到达后应该收回去
	m.Update(hidePlayersMsg{})
	if strings.Contains(strip(m.View()), "[PLAYER]") {
		t.Fatal("定时消息到达后应该收起面板")
	}
}

// TestLayoutTogglePersists 按 i 切换界面密度，并写回偏好。
func TestLayoutTogglePersists(t *testing.T) {
	var saved ui.Layout
	m := testModel(t, 80, sampleSnap(20241029))
	m.opts.SetLayout = func(key string, l ui.Layout) { saved = l }

	if m.minimal {
		t.Fatal("默认应该是丰富模式")
	}
	m.handleKey(keyOf("i"))
	if !m.minimal || saved != ui.LayoutCompact {
		t.Fatalf("按 i 应该切到极简并写回偏好，实际 minimal=%v saved=%v", m.minimal, saved)
	}
	m.handleKey(keyOf("i"))
	if m.minimal || saved != ui.LayoutRich {
		t.Fatalf("再按一次应该切回丰富，实际 minimal=%v saved=%v", m.minimal, saved)
	}
}

// TestCompactTurnWindow 人数超过 3 时 TURN 只显示 3 个，并在自己不在窗口里时补牌数。
func TestCompactTurnWindow(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.SeatCount = 6
		o.HandCounts = []int{7, 2, 3, 4, 5, 6}
		o.Turn = 3
		o.Seat = 0
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	turnLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[TURN]") {
			turnLine = line
		}
	}
	if turnLine == "" {
		t.Fatalf("没有 TURN 行：\n%s", out)
	}
	if strings.Count(turnLine, "张") > 4 { // 3 个人 + 自己那句
		t.Fatalf("TURN 行显示的人太多：%q", turnLine)
	}
	if !strings.Contains(turnLine, "你:7张") {
		t.Fatalf("自己不在窗口里时要补一句自己的牌数：%q", turnLine)
	}
}

func ids(cards []engine.Card) []int {
	out := make([]int, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}
