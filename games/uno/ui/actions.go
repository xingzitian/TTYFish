package tui

import (
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// playActions is shared by the two layouts and the gameplay key dispatcher.
// Rules and server calls remain in the model.
var playActions = ui.MustRegisterActions("uno.play", []ui.Action{
	{ID: "u", Key: "U", Label: "UNO"},
	{ID: "d", Key: "D", Label: "摸牌"},
	{ID: "p", Key: "P", Label: "过"},
	{ID: "c", Key: "C", Label: "抓包"},
})

var commonActions = ui.MustRegisterActions("uno.common", []ui.Action{
	{ID: "?", Key: "?", Label: "帮助", Aliases: []string{"/"}},
	{ID: "q", Key: "Q", Label: "断开", Detail: "断开连接"},
	{ID: "t", Key: "T", Label: "聊天"},
	{ID: "i", Key: "I", Label: "界面", Detail: "切换界面"},
	{ID: "m", Key: "M", Label: "颜色深浅"},
	{ID: "v", Key: "V", Label: "投票", Detail: "重开投票"},
	{ID: "tab", Key: "Tab", Label: "玩家"},
	{ID: "-", Key: "-", Label: "上一页", Aliases: []string{"_", "left"}},
	{ID: "=", Key: "=", Label: "下一页", Aliases: []string{"+", "right"}},
})

var lobbyActions = ui.MustRegisterActions("uno.lobby", []ui.Action{
	{ID: "s", Key: "S", Label: "开始", Detail: "开始游戏"},
	{ID: "b", Key: "C", Label: "+电脑", Aliases: []string{"b"}},
	{ID: "l", Key: "L", Label: "离开房间"},
})

var challengeActions = ui.MustRegisterActions("uno.challenge", []ui.Action{
	{ID: "accept", Key: "Y", Label: "认罚摸4", Aliases: []string{"enter", " "}},
	{ID: "challenge", Key: "N", Label: "质疑", Aliases: []string{"c"}},
})

var colorActions = ui.MustRegisterActions("uno.color", []ui.Action{
	{ID: "red", Key: "1", Label: "红", Aliases: []string{"r"}},
	{ID: "yellow", Key: "2", Label: "黄", Aliases: []string{"y"}},
	{ID: "blue", Key: "3", Label: "蓝", Aliases: []string{"b"}},
	{ID: "green", Key: "4", Label: "绿", Aliases: []string{"g"}},
	ui.EscapeAction("取消"),
})

var layoutChoiceActions = ui.MustRegisterActions("uno.layout-choice", []ui.Action{
	{ID: "rich", Key: "1", Label: "丰富（信息全）"},
	{ID: "compact", Key: "2", Label: "极简（摸鱼用）"},
})

var cardAction = ui.Action{ID: "card", Key: "1", Range: true, Label: "出牌", Detail: "出当前页第几张", Aliases: []string{"2", "3", "4", "5", "6", "7", "8", "9"}}

func init() {
	combined := append(append([]ui.Action(nil), commonActions...), playActions...)
	combined = append(combined, cardAction, ui.BossAction(), ui.InterruptAction())
	combined = append(combined, ui.FrameScrollActions()...)
	ui.MustRegisterActions("uno.playing", combined)
	lobby := append(append([]ui.Action(nil), commonActions...), lobbyActions...)
	lobby = append(lobby, ui.BossAction(), ui.InterruptAction())
	ui.MustRegisterActions("uno.lobby.active", append(lobby, ui.FrameScrollActions()...))
	help := ui.HelpActions(commonActions, "?", "q")
	ui.MustRegisterActions("uno.help.active", append(help, ui.BossAction()))
	system := append([]ui.Action{ui.BossAction(), ui.InterruptAction()}, ui.FrameScrollActions()...)
	ui.MustRegisterActions("uno.color.active", append(append([]ui.Action(nil), colorActions...), system...))
	ui.MustRegisterActions("uno.challenge.active", append(append(append([]ui.Action(nil), challengeActions...), commonActions...), system...))
	layout := append(append([]ui.Action(nil), layoutChoiceActions...), ui.SelectActions(commonActions, "q")...)
	ui.MustRegisterActions("uno.layout-choice.active", append(layout, system...))
}

func handAction(compact bool) ui.Action {
	action := cardAction
	if compact && len(action.Aliases) > 4 {
		action.Aliases = action.Aliases[:4]
	}
	return action
}

func commonActionKey(key string) string {
	id, _ := ui.ResolveAction(commonActions, key)
	return id
}

// tableActions supplies both renderers with the same registered game actions.
// Presentation-only differences (page size and player panel) are local here.
func (m *Model) tableActions() []ui.Action {
	actions := ui.SelectActions(commonActions, "?")
	obs := m.obs()
	if obs.Turn == m.snap.MySeat && obs.Phase == engine.Playing {
		actions = append(actions, handAction(m.minimal))
		actions = append(actions, playActions...)
	} else {
		// These two actions are valid outside the player's turn too.
		actions = append(actions, ui.SelectActions(playActions, "u", "c")...)
	}
	actions = append(actions, ui.SelectActions(commonActions, "t")...)
	if m.minimal {
		actions = append(actions, ui.SelectActions(commonActions, "tab")...)
	}
	actions = append(actions, ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "-", "=")...))
	actions = append(actions, ui.SelectActions(commonActions, "i", "q", "v")...)
	return ui.CheckedActions("uno.table", actions, commonActions, []ui.Action{ui.BossAction(), ui.InterruptAction()}, ui.FrameScrollActions())
}

func (m *Model) tableActionKey(key string) string {
	id, _ := ui.ResolveAction(m.tableActions(), key)
	return id
}

func (m *Model) currentLobbyActions() []ui.Action {
	var actions []ui.Action
	if m.snap.IsHost {
		actions = append(actions, ui.SelectActions(lobbyActions, "s", "b")...)
	} else {
		actions = append(actions, ui.Action{Label: "等房主开始"})
	}
	actions = append(actions, ui.SelectActions(commonActions, "?", "t", "v", "i")...)
	actions = append(actions, ui.SelectActions(lobbyActions, "l")...)
	actions = append(actions, ui.EscapeAction("离开"))
	actions = append(actions, ui.SelectActions(commonActions, "q")...)
	return ui.CheckedActions("uno.lobby", actions, commonActions, lobbyActions, []ui.Action{ui.BossAction(), ui.InterruptAction()}, ui.FrameScrollActions())
}
