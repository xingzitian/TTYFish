// Command preview 是界面离线预览：不开 SSH、不连后端，直接在本地终端跑真正的界面模型。
//
//	go run ./cmd/preview
//	go run ./cmd/preview -ui compact -color text
//
// 它和线上走同一套 gateway.Root 和游戏界面，唯一区别是"创建房间"时用本地房间
// （内存里跑规则 + 电脑），所以菜单、牌桌、聊天、投票全都能玩，一个网络请求都不发。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	unoUI "github.com/xingzitian/TTYFish/games/uno/ui"
	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/console"
	"github.com/xingzitian/TTYFish/hub/internal/gateway"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/session"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// version 只是标题栏里显示的字符串，预览时可以随便写。
var version = "dev"

func main() {
	// Windows 上先修终端：GBK 代码页会让中文乱码，不开 VT 则颜色全变成转义符
	console.Setup()

	name := flag.String("user", "预览玩家", "标题栏显示的用户名")
	color := flag.String("color", "block", "颜色深浅：block 满色 / text 文字上色 / plain 无色")
	layout := flag.String("ui", "rich", "默认界面密度：rich 丰富 / compact 极简")
	ver := flag.String("version", version, "标题栏显示的版本号")
	bots := flag.Int("bots", 3, "新建房间时预置几个电脑（0 表示不加）")
	flag.Parse()

	// Windows Terminal 不设 TERM，空值会被 ProfileFor 判成 Ascii（一片黑白）。
	// 这里给个合理默认：ConPTY 支持 256 色。
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}

	mode, ok := ui.ParseColorMode(*color)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知的颜色深浅 %q（block / text / plain）\n", *color)
		os.Exit(2)
	}
	layoutMode, ok := ui.ParseLayout(*layout)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知的界面密度 %q（rich / compact）\n", *layout)
		os.Exit(2)
	}

	rules := engine.Rules{Challenge: true, UnoPenalty: true}
	entries := registry.New()
	if err := entries.Register(registry.Entry{
		ID:   "uno",
		Game: uno.New(rules),
		NewUI: func(c registry.UIContext) registry.Model {
			return unoUI.NewModel(unoUI.Identity{Key: c.Key, Name: c.Name}, c.Client, unoUI.Options{
				Rules: rules, Mode: c.Mode, ModeOf: c.ModeOf, SetMode: c.SetMode,
				Layout: c.Layout, LayoutOf: c.LayoutOf, SetLayout: c.SetLayout,
			}, unoUI.NewRenderer(c.Output, c.Term))
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "注册游戏失败：%v\n", err)
		os.Exit(1)
	}

	// 预览用的房间表：房间就在本进程内存里，关掉进程就没了。
	rooms := room.NewRegistry(uno.New(rules), room.Options{BotLag: 400 * time.Millisecond})
	defer rooms.Close()

	openRoom := func(gameID, code string) (client.Client, error) {
		c := session.New(rooms, "preview-player", *name)
		if err := c.Join(code); err != nil {
			return nil, err
		}
		// 新建的房间自动补几个电脑，省得每次手动按 b
		if code == "" {
			for i := 0; i < *bots; i++ {
				if err := c.AddBot(); err != nil {
					break
				}
			}
		}
		return c, nil
	}

	root := gateway.New(gateway.Deps{
		Registry: entries,
		// 预览时玩家 ID / 令牌都是假的，只用于显示
		Player: backend.Player{Account: backend.Account{ID: "preview", Username: *name}},
		Output: os.Stdout,
		Term:   term,
		Mode:   mode,
		Layout: layoutMode,
		// 偏好不落盘：预览不该改掉线上记住的设置
		// 必须显式设色深：lipgloss 的默认探测在非 TTY 上永远返回 Ascii
		Renderer: unoUI.NewRenderer(os.Stdout, term),
		Version:  *ver,
		OpenRoom: openRoom,
	})

	if _, err := tea.NewProgram(root, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败：%v\n", err)
		os.Exit(1)
	}
}
