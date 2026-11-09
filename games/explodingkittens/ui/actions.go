package tui

import "github.com/xingzitian/TTYFish/sdk/ui"

var commonActions = ui.MustRegisterActions("kittens.common", []ui.Action{
	{ID: "help", Key: "?", Label: "帮助", Aliases: []string{"/"}},
	{ID: "layout", Key: "I", Label: "界面", Detail: "切换界面"},
	{ID: "color", Key: "M", Label: "颜色深浅"},
	{ID: "players", Key: "Tab", Label: "玩家", Detail: "玩家列表"},
	{ID: "next", Key: "=", Label: "下一页", Aliases: []string{"pagedown", "pgdown"}},
	{ID: "previous", Key: "-", Label: "上一页", Aliases: []string{"pageup", "pgup"}},
	ui.EscapeAction("取消选择 / 离开房间"),
	{ID: "quit", Key: "Q", Label: "断开连接"},
	{ID: "chat", Key: "T", Label: "聊天", Detail: "快捷语"},
})

// playActions defines the ordinary turn actions for both layouts and input.
var playActions = ui.MustRegisterActions("kittens.play", []ui.Action{
	{ID: "g", Key: "G", Label: "对子盲抽"},
	{ID: "d", Key: "D", Label: "摸牌"},
})

var cardAction = ui.Action{ID: "card", Key: "1", Range: true, Label: "出牌", Detail: "出功能牌", Aliases: []string{"2", "3", "4", "5"}}

var responseActions = ui.MustRegisterActions("kittens.response", []ui.Action{
	{ID: "n", Key: "N", Label: "否决"},
	{ID: "p", Key: "P", Label: "放弃"},
})

var revealAction = ui.Action{ID: "reveal-done", Key: "P", Label: "结束预言"}

var targetConfirmAction = ui.Action{ID: "confirm-target", Key: "Enter", Label: "确认", Aliases: []string{" "}}

var customPositionActions = ui.MustRegisterActions("kittens.custom-position", []ui.Action{
	{ID: "previous", Key: "-", Label: "减小", Aliases: []string{"pageup", "pgup"}},
	{ID: "next", Key: "=", Label: "增大", Aliases: []string{"pagedown", "pgdown"}},
	{ID: "confirm", Key: "Enter", Label: "确认", Aliases: []string{" "}},
	ui.EscapeAction("返回"),
})

var lobbyActions = ui.MustRegisterActions("kittens.lobby", []ui.Action{
	{ID: "s", Key: "S", Label: "开始"},
	{ID: "b", Key: "B", Label: "电脑"},
	{ID: "v", Key: "V", Label: "投票"},
	{ID: "dlc", Key: "X", Label: "切换反转DLC"},
})

var reinsertActions = ui.MustRegisterActions("kittens.reinsert", []ui.Action{
	{ID: "0", Key: "0", Label: "牌堆顶"},
	{ID: "1", Key: "1", Label: "第2张"},
	{ID: "2", Key: "2", Label: "第3张"},
	{ID: "b", Key: "B", Label: "牌堆底"},
	{ID: "r", Key: "R", Label: "随机"},
	{ID: "i", Key: "I", Label: "指定"},
})

func init() {
	combined := func(first, second []ui.Action) []ui.Action {
		return append(append([]ui.Action(nil), first...), second...)
	}
	system := append([]ui.Action{ui.BossAction(), ui.InterruptAction()}, ui.FrameScrollActions()...)
	playing := append(combined(commonActions, playActions), cardAction, revealAction)
	ui.MustRegisterActions("kittens.playing", append(playing, system...))
	ui.MustRegisterActions("kittens.lobby.active", append(combined(commonActions, lobbyActions), system...))
	ui.MustRegisterActions("kittens.responding", append(combined(commonActions, responseActions), system...))
	var reinsertCommon []ui.Action
	for _, action := range commonActions {
		if action.ID != "layout" {
			reinsertCommon = append(reinsertCommon, action)
		}
	}
	ui.MustRegisterActions("kittens.reinsert.active", append(combined(reinsertCommon, reinsertActions), system...))
	help := ui.HelpActions(commonActions, "help", "quit")
	ui.MustRegisterActions("kittens.help.active", append(help, ui.BossAction()))
}
