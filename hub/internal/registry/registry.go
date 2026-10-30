// Package registry 定义平台挂载游戏所需的规则和界面工厂。
package registry

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// Model 是一个游戏会话的界面；Close 释放订阅并通知房间断线。
type Model interface {
	tea.Model
	Close()
}

// UIContext 是平台提供给游戏界面的会话依赖，不包含 SSH 协议类型。
type UIContext struct {
	Key, Name string
	Client    client.Client
	Output    io.Writer
	Term      string
	Mode      ui.ColorMode
	ModeOf    func(string) (ui.ColorMode, bool)
	SetMode   func(string, ui.ColorMode)
	Layout    ui.Layout
	LayoutOf  func(string) (ui.Layout, bool)
	SetLayout func(string, ui.Layout)
}

// Entry 将游戏标识、规则实现和界面工厂绑定在一起，由组装入口创建。
type Entry struct {
	ID    string
	Game  game.Game
	NewUI func(UIContext) Model
}

// Validate 在启动服务前检查挂载配置。
func (e Entry) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("游戏标识不能为空")
	}
	if e.Game == nil {
		return fmt.Errorf("游戏 %q 缺少规则实现", e.ID)
	}
	if e.NewUI == nil {
		return fmt.Errorf("游戏 %q 缺少界面工厂", e.ID)
	}
	return nil
}
