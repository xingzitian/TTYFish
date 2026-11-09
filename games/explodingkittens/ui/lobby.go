package tui

import (
	"fmt"
	"strings"

	kittens "github.com/xingzitian/TTYFish/games/explodingkittens"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func (m *Model) lobbyPlayer(p client.Player) string {
	name := p.Name
	if name == "" {
		name = fmt.Sprintf("P%d", p.Seat+1)
	}
	name = ui.CutWidth(name, 16)
	if p.Seat == m.snap.MySeat && name != "你" {
		name += "（你）"
	}
	if p.Host {
		name += " 房主"
	}
	switch {
	case !p.Connected:
		name += fmt.Sprintf(" [掉线 %ds，电脑代打]", p.Offline)
	case p.Bot:
		name += " [电脑]"
	}
	return name
}

func (m *Model) viewLobby() string {
	maxSeats := m.snap.MaxSeats
	if maxSeats <= 0 {
		maxSeats = (kittens.Game{}).MaxSeats()
	}
	compact := m.opts.Layout == ui.LayoutCompact
	title := fmt.Sprintf("房间 %s   %d/%d 人", m.snap.Code, len(m.snap.Players), maxSeats)
	if compact {
		title = fmt.Sprintf("[ROOM] %s  %d/%d 人", m.snap.Code, len(m.snap.Players), maxSeats)
	}
	if m.snap.IsHost {
		title += "   你是房主"
	}
	lines := []string{m.turn().Render(title)}
	dlc := "关闭"
	for _, id := range m.snap.DLC {
		if id == kittens.ReverseDLC {
			dlc = "开启"
			break
		}
	}
	lines = append(lines, m.dim().Render("DLC 反转牌："+dlc))
	if !compact {
		lines = append(lines, "")
	}
	if m.snap.Phase == client.Over {
		o := m.obs()
		if o.Winner >= 0 && o.Winner < len(o.Alive) {
			lines = append(lines, m.ok().Render("[FIN] "+m.seatName(o.Winner)+" 赢了这一局"))
		}
	}
	if compact {
		var labels []string
		for _, p := range m.snap.Players {
			labels = append(labels, m.lobbyPlayer(p))
		}
		if len(labels) == 0 {
			labels = append(labels, "（还没有玩家）")
		}
		for start := 0; start < len(labels); start += 3 {
			prefix := "         "
			if start == 0 {
				prefix = "[PLAYER] "
			}
			lines = append(lines, wrapTokens(labels[start:min(start+3, len(labels))], prefix, m.width)...)
		}
	} else {
		for _, p := range m.snap.Players {
			label := "  " + m.lobbyPlayer(p)
			if p.Seat == m.snap.MySeat {
				lines = append(lines, m.turn().Render("▶ "+m.lobbyPlayer(p)))
			} else {
				lines = append(lines, m.dim().Render(label))
			}
		}
		lines = append(lines, "")
	}
	if m.snap.Notice != "" {
		lines = append(lines, m.ok().Render(m.snap.Notice))
	}
	var chats []string
	for _, event := range m.snap.Timeline {
		if event.Chat {
			chats = append(chats, m.seatName(event.Seat)+": "+event.Text)
		}
	}
	lines = append(lines, ui.TailN(chats, 2)...)
	if m.snap.Vote != nil {
		lines = append(lines, fmt.Sprintf("重开投票：赞成 %d/%d · 反对 %d · 剩余 %ds",
			m.snap.Vote.Yes, m.snap.Vote.Need, m.snap.Vote.No, m.snap.Vote.EndsIn))
	}
	if m.errVisible() {
		lines = append(lines, m.warn().Render("! "+m.errMsg))
	}
	if m.snap.IsHost && m.snap.Phase == client.Lobby && len(m.snap.Players) < (kittens.Game{}).MinSeats() {
		for _, action := range ui.SelectActions(lobbyActions, "b") {
			lines = append(lines, m.dim().Render("至少 2 人才能开局，可按 "+action.DisplayKey()+" 添加电脑"))
		}
	}
	lines = append(lines, m.cmdLine(m.width))
	return strings.Join(lines, "\n")
}
