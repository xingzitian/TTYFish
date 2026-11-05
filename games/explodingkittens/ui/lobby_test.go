package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestLobbyAlwaysShowsOccupancyAndPlayers(t *testing.T) {
	for _, layout := range []ui.Layout{ui.LayoutCompact, ui.LayoutRich} {
		for _, width := range []int{40, 80, 120} {
			m, c := newTestModel(t, engine.Observation{}, client.Lobby)
			m.opts.Layout, m.width = layout, width
			c.snap.MaxSeats = 5
			c.snap.Players = []client.Player{
				{Seat: 0, Name: "房主甲", Host: true, Connected: true},
				{Seat: 1, Name: "电脑乙", Bot: true, Connected: true},
				{Seat: 2, Name: "玩家丙", Offline: 7},
			}
			m.Update(client.SnapshotMsg(c.snap))
			view := strip(m.View())
			for _, want := range []string{"AB12CD", "3/5 人", "房主甲", "（你）", "房主", "电脑乙", "[电脑]", "玩家丙", "掉线 7s", "[S]开始", "[B]电脑"} {
				if !strings.Contains(view, want) {
					t.Fatalf("layout=%d width=%d missing %q:\n%s", layout, width, want, view)
				}
			}
			for _, unwanted := range []string{"[TURN]", "[HAND]", "[DECK]", "出牌记录", "0张"} {
				if strings.Contains(view, unwanted) {
					t.Fatalf("lobby still displays table-only content %q", unwanted)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("lobby overflow: %q", line)
				}
			}
			c.snap.Players = c.snap.Players[:2]
			m.Update(client.SnapshotMsg(c.snap))
			if view := strip(m.View()); !strings.Contains(view, "2/5 人") || strings.Contains(view, "玩家丙") {
				t.Fatal("room population did not follow snapshot")
			}
		}
	}
}

func TestLobbyGuestChatAndFinishedResult(t *testing.T) {
	m, c := newTestModel(t, engine.Observation{}, client.Lobby)
	c.snap.IsHost = false
	c.snap.Players = c.snap.Players[:1]
	m.Update(client.SnapshotMsg(c.snap))
	view := strip(m.View())
	if !strings.Contains(view, "1/5 人") || !strings.Contains(view, "等待房主开始") ||
		strings.Contains(view, "[S]开始") || strings.Contains(view, "[B]电脑") {
		t.Fatal("guest received host controls or lost occupancy")
	}
	press(m, "t")
	if !m.HasModal() || !strings.Contains(strip(m.View()), "快捷语") {
		t.Fatal("lobby chat menu unavailable")
	}
	press(m, "1")
	if c.chat != 1 || m.step != stepIdle {
		t.Fatal("lobby preset did not send")
	}
	o := playingObs()
	o.Winner = 1
	o.Phase = engine.Finished
	c.snap.Phase, c.snap.View = client.Over, o
	m.Update(client.SnapshotMsg(c.snap))
	if view := strip(m.View()); !strings.Contains(view, "[FIN]") || !strings.Contains(view, "赢了这一局") {
		t.Fatal("finished lobby lost match result")
	}
}
