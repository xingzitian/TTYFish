package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestCompactFeedShowsChat 别人说的话必须在极简模式里看得见——牌桌和大厅都要。
//
// 这条曾经是坏的：MSG 行读的是引擎的 Observation.Log，而聊天根本不在引擎里
// （引擎不管聊天），它由房间追加到快照的 Timeline 并打 Chat 标记。
func TestCompactFeedShowsChat(t *testing.T) {
	// 牌桌
	play := sampleSnap(20241029)
	play.Timeline = []client.TimelineEntry{{Seat: 1, Text: "快点出牌啊", Chat: true}}
	m := testModel(t, 80, play)
	m.minimal = true
	if out := strip(m.View()); !strings.Contains(out, "快点出牌啊") {
		t.Fatalf("牌桌极简必须显示聊天：\n%s", out)
	}

	// 大厅
	lobby := sampleSnap(20241029)
	lobby.Phase = client.Lobby
	setObs(&lobby, func(o *engine.Observation) { o.Hand = nil; o.RoundWinner = -1 })
	lobby.Timeline = []client.TimelineEntry{{Seat: 2, Text: "等等我", Chat: true}}
	m2 := testModel(t, 80, lobby)
	m2.minimal = true
	if out := strip(m2.View()); !strings.Contains(out, "等等我") {
		t.Fatalf("大厅极简必须显示聊天：\n%s", out)
	}

	// 没有消息时不留空行
	lobby.Timeline = nil
	lobby.Notice = ""
	m3 := testModel(t, 80, lobby)
	m3.minimal = true
	if out := strip(m3.View()); strings.Contains(out, "[MSG]") {
		t.Fatalf("没有消息时不该画 MSG 行：\n%s", out)
	}
}
