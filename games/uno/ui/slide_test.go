package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

func slideFixture(t *testing.T) *Model {
	t.Helper()
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Turn = 0
		o.Dir = 1
		o.HandCounts = []int{7, 6, 5, 4}
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	next := m.snap
	setObs(&next, func(o *engine.Observation) {
		o.Turn = 1
		o.HandCounts = []int{7, 6, 5, 4}
	})
	_, _ = m.Update(snapMsg(next))
	return m
}

// TestTurnSlideDoesNotRebuildTape 滑动过程中队列内容必须冻结。
//
// 这是这次闪烁的根因：以前换人时用新快照重建整条队列，名字被瞬间挪到新位置，
// 观众看到的就是"突然出现 + 入场动画"，没有出场。
func TestTurnSlideDoesNotRebuildTape(t *testing.T) {
	m := slideFixture(t)

	// 第一帧渲染会把"滑几个座位"换算成列数，并建立队列缓存
	_ = m.turnMarquee(m.obs(), 60)
	tape := m.tape
	key := m.tapeKey
	if tape == "" {
		t.Fatal("队列缓存没建立")
	}
	if m.turnOffset == 0 {
		t.Fatal("换人第一帧就该有偏移")
	}

	for i := 0; i < 40 && m.turnOffset != 0; i++ {
		_, _ = m.Update(turnTickMsg{model: m})
		_ = m.turnMarquee(m.obs(), 60)
		if m.tape != tape || m.tapeKey != key {
			t.Fatalf("第 %d 帧队列被重建了——名字会瞬移，不是平移", i+1)
		}
	}
	if m.turnOffset != 0 {
		t.Fatal("滑动没有收敛")
	}
}

// TestTurnSlideSettlesCentered 滑完之后当前玩家正好在中间那一格。
func TestTurnSlideSettlesCentered(t *testing.T) {
	m := slideFixture(t)
	_ = m.turnMarquee(m.obs(), 60) // 让偏移先生效
	for i := 0; i < 80 && m.turnOffset != 0; i++ {
		_, _ = m.Update(turnTickMsg{model: m})
	}

	line := strip(m.turnMarquee(m.obs(), 60))
	badge := strip(m.turnBadge(m.obs(), 1, 19))
	at := strings.Index(line, badge)
	if at < 0 {
		t.Fatalf("找不到当前玩家：%q", line)
	}
	slot := 20
	center := at + len([]rune(badge))/2
	if center < slot || center >= 2*slot {
		t.Fatalf("当前玩家没落在中间格：center=%d，line=%q", center, line)
	}
}

// TestTurnSlideMovesEveryoneOneWay 滑动过程中，屏幕上的内容整体朝一个方向移动，
// 不允许某一帧突然反向或跳变。
func TestTurnSlideMovesEveryoneOneWay(t *testing.T) {
	m := slideFixture(t)
	_ = m.turnMarquee(m.obs(), 60)

	prev := m.turnOffset
	for i := 0; i < 40 && m.turnOffset != 0; i++ {
		_, _ = m.Update(turnTickMsg{model: m})
		_ = m.turnMarquee(m.obs(), 60)
		step := m.turnOffset - prev
		if step != 1 && step != -1 {
			t.Fatalf("第 %d 帧偏移从 %d 变成 %d（步长 %d），不连续", i+1, prev, m.turnOffset, step)
		}
		prev = m.turnOffset
	}
}
