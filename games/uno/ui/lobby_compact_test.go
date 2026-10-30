package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestCompactLobbyRenders 大厅极简视图：房间码、玩家、按键，且只有几行。
func TestCompactLobbyRenders(t *testing.T) {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = nil
		o.RoundWinner = -1
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	for _, want := range []string{"[ROOM]", snap.Code, "[PLAYER]", "[CMD]", "[S]开始", "[C]+电脑", "[I]界面"} {
		if !strings.Contains(out, want) {
			t.Errorf("大厅极简缺少 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "Public") {
		t.Errorf("不该出现 (Public)：后端没有公开房间这个概念\n%s", out)
	}
	if strings.Contains(out, "-电脑") {
		t.Errorf("不该出现减电脑：后端没有这个接口\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got > 4 {
		t.Errorf("大厅极简最多 4 行，实际 %d 行\n%s", got, out)
	}
}

// TestCompactLobbyFinLine 打完一局才显示 FIN，刚开房不占这一行。
func TestCompactLobbyFinLine(t *testing.T) {
	snap := sampleSnap(20241029)
	snap.Phase = client.Over
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = nil
		o.RoundWinner = 1
		o.Scores = []int{3, 99, 10, 4}
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	if !strings.Contains(out, "[FIN]") {
		t.Fatalf("打完一局应该显示 FIN：\n%s", out)
	}
	if !strings.Contains(out, "99") {
		t.Fatalf("FIN 应该带上得分：\n%s", out)
	}

	// 刚开房（没有赢家）不该有 FIN 行
	snap2 := sampleSnap(20241029)
	snap2.Phase = client.Lobby
	setObs(&snap2, func(o *engine.Observation) { o.Hand = nil; o.RoundWinner = -1 })
	m2 := testModel(t, 80, snap2)
	m2.minimal = true
	if strings.Contains(strip(m2.View()), "[FIN]") {
		t.Fatal("刚开房不该显示 FIN 行")
	}
}

// TestLayoutSharedBetweenLobbyAndTable 大厅和牌桌共用同一个 i 开关：
// 大厅切成极简，进牌桌必须还是极简，反过来也一样。
func TestLayoutSharedBetweenLobbyAndTable(t *testing.T) {
	lobbySnap := sampleSnap(20241029)
	lobbySnap.Phase = client.Lobby
	setObs(&lobbySnap, func(o *engine.Observation) { o.Hand = nil })

	m := testModel(t, 80, lobbySnap)
	m.handleKey(keyOf("i"))
	if !m.minimal {
		t.Fatal("大厅按 i 应该切到极简")
	}

	// 切到牌桌：同一个 Model，只是快照变了
	playSnap := sampleSnap(20241029)
	m.snap = playSnap
	if out := strip(m.View()); !strings.Contains(out, "[HAND]") {
		t.Fatalf("大厅切成极简后，牌桌也应该保持极简：\n%s", out)
	}

	// 再切回丰富，牌桌立刻恢复老界面
	m.handleKey(keyOf("i"))
	if m.minimal {
		t.Fatal("再按 i 应该切回丰富")
	}
	if out := strip(m.View()); strings.Contains(out, "[HAND]") {
		t.Fatalf("丰富模式不该有极简的 HAND 行：\n%s", out)
	}
}

var _ = fmt.Sprint
