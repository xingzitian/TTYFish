package gateway

import "github.com/xingzitian/TTYFish/sdk/ui"

// menuActions is the shared definition for the menu prompt and dispatcher.
var menuActions = ui.MustRegisterActions("gateway.menu", []ui.Action{
	{ID: "previous", Key: "-", Label: "上一项", Aliases: []string{"up", "k"}},
	{ID: "next", Key: "=", Label: "下一项", Aliases: []string{"down", "j"}},
	{ID: "select", Key: "Enter", Label: "选择", Aliases: []string{" "}},
	ui.EscapeAction("返回"),
	{ID: "quit", Key: "Q", Label: "断开"},
})

var joinActions = ui.MustRegisterActions("gateway.join", []ui.Action{
	{ID: "join", Key: "Enter", Label: "进入"},
	ui.EscapeAction("返回"),
})

var leaveConfirmActions = ui.MustRegisterActions("gateway.leave-confirm", []ui.Action{
	{ID: "leave", Key: "Y", Label: "退出对局"},
	{ID: "stay", Key: "N", Label: "继续打", Aliases: []string{"esc"}},
	{ID: "quit", Key: "Q", Label: "断开"},
})

func init() {
	ui.MustRegisterActions("gateway.menu.active", append(append([]ui.Action(nil), menuActions...), ui.FrameScrollActions()...))
	ui.MustRegisterActions("gateway.join.active", append(append([]ui.Action(nil), joinActions...), ui.FrameScrollActions()...))
}
