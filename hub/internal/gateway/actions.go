package gateway

import "github.com/xingzitian/TTYFish/sdk/ui"

// menuActions is the shared definition for the menu prompt and dispatcher.
var menuActions = []ui.Action{
	{ID: "previous", Key: "-", Label: "上一项", Aliases: []string{"up", "k"}},
	{ID: "next", Key: "=", Label: "下一项", Aliases: []string{"down", "j"}},
	{ID: "select", Key: "Enter", Label: "选择", Aliases: []string{" "}},
	ui.EscapeAction("返回"),
	{ID: "quit", Key: "Q", Label: "断开"},
}

var joinActions = []ui.Action{
	{ID: "join", Key: "Enter", Label: "进入"},
	ui.EscapeAction("返回"),
}

var leaveConfirmActions = []ui.Action{
	{ID: "leave", Key: "Y", Label: "退出对局"},
	{ID: "stay", Key: "N", Label: "继续打", Aliases: []string{"esc"}},
	{ID: "quit", Key: "Q", Label: "断开"},
}
