package tui

import (
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// keyHelp derives both layouts' help from the registered action descriptions.
func (m *Model) keyHelp() []ui.ActionGroup {
	common := ui.ActionGroup{Title: "信息", Actions: ui.SelectActions(commonActions, "t", "v", "m", "i")}
	room := ui.ActionGroup{Title: "房间", Actions: ui.SelectActions(lobbyActions, "l")}
	room.Actions = append(room.Actions, ui.EscapeAction("离开（需确认）"))
	room.Actions = append(room.Actions, ui.SelectActions(commonActions, "q")...)
	closeGroup := ui.ActionGroup{Title: "帮助", Actions: ui.SelectActions(ui.HelpActions(commonActions, "?", "q"), "close-help", "cancel")}
	if m.snap.Phase != client.Playing {
		var actions []ui.Action
		if m.snap.IsHost {
			actions = ui.SelectActions(lobbyActions, "s", "b")
		} else {
			actions = []ui.Action{{Key: "—", Label: "等房主开始"}}
		}
		return []ui.ActionGroup{{Title: "大厅", Actions: actions}, common, room, closeGroup}
	}
	current := m.tableActions()
	play := ui.SelectActions(current, "card")
	play = append(play, ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "-", "=")...))
	play = append(play, ui.SelectActions(current, "u", "d", "p", "c")...)
	return []ui.ActionGroup{{Title: "出牌", Actions: play}, common, room, closeGroup}
}

func (m *Model) viewHelp() string {
	return ui.RenderHelp(m.keyHelp(), m.width, "[KEY] 键位一览", ui.HelpCloseHint(ui.HelpActions(commonActions, "?", "q")), m.dim().Render, m.turn().Render)
}
