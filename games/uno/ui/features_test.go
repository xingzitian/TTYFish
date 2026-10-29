package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestTimelineEntryWithoutCardDoesNotPanic 记录里没有牌面时不能崩。
//
// 真实触发过一次：明细字段放的是 (*engine.Card)(nil)，接口层判断 != nil
// 会通过，一解引用就 panic，整个会话直接断掉。
func TestTimelineEntryWithoutCardDoesNotPanic(t *testing.T) {
	snap := sampleSnap(1)
	snap.Timeline = []client.TimelineEntry{
		{AtMS: 1000, Seat: 0, Text: "摸了一张牌"},
		{AtMS: 2000, Seat: 0, Detail: (*engine.Card)(nil), Text: "被加二，摸 2 张"},
		{AtMS: 3000, Seat: -1, Text: "方向已改变"},
	}
	m := testModel(t, 100, snap)
	m.height = 24

	out := strip(m.View())
	if !strings.Contains(out, "摸了一张牌") {
		t.Fatalf("普通事件应该照常显示:\n%s", out)
	}
}

// TestBossKey 老板键：Ctrl+B 整屏换成假 shell，回车恢复。
func TestBossKey(t *testing.T) {
	m := testModel(t, 100, sampleSnap(1))
	m.height = 24

	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	out := strip(m.View())
	if !strings.Contains(out, "tail -f") {
		t.Fatalf("老板键应该显示假 shell:\n%s", out)
	}
	if strings.Contains(out, "你的手牌") || strings.Contains(out, "玩家") {
		t.Fatalf("老板键应该把牌桌完全盖住:\n%s", out)
	}
	// 别在假象里露出"按回车返回"这种提示。
	if strings.Contains(out, "恢复") || strings.Contains(out, "回车") {
		t.Fatalf("老板键不该带操作提示:\n%s", out)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(strip(m.View()), "tail -f") {
		t.Fatal("回车应该恢复牌桌")
	}
}

// TestWildMenuHidesHand 打变色牌时手牌暂时藏起来，只留那一行选色菜单。
func TestWildMenuHidesHand(t *testing.T) {
	m := testModel(t, 100, sampleSnap(20241029))
	m.height = 24
	m.choosing = true

	out := strip(m.View())
	if strings.Contains(out, "你的手牌") {
		t.Fatalf("选色时应该把手牌藏起来:\n%s", out)
	}
	if !strings.Contains(out, "选颜色") {
		t.Fatalf("应该显示选色菜单:\n%s", out)
	}
	// 菜单一行就够，别把手牌挤成两行还留着。
	if strings.Count(out, "选颜色") != 1 {
		t.Fatalf("选色菜单应该只出现一次:\n%s", out)
	}
}

// TestUnoPromptShown 剩一张牌没喊 UNO 时要有醒目的倒计时提示。
func TestUnoPromptShown(t *testing.T) {
	snap := sampleSnap(1)
	setObs(&snap, func(o *engine.Observation) { o.UnoPending = snap.MySeat })
	snap.PendingLeftMS = 2400

	out := strip(testModel(t, 100, snap).View())
	if !strings.Contains(out, "按 u 喊 UNO") {
		t.Fatalf("没有提示喊 UNO:\n%s", out)
	}
	if !strings.Contains(out, "3 秒") {
		t.Fatalf("倒计时取整不对（2400ms 应该显示 3 秒）:\n%s", out)
	}
}
