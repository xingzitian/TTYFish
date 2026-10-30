// Command play 是单机版 UNO：不连服务器、不要账号，直接跟电脑打。
//
// 它的存在是为了"把 UNO 抽出来单独跑"——平台那一套（SSH 网关、后端 API、
// 网页）全都不参与，房间就在本进程内存里。
//
//	go run ./cmd/play            # 3 个电脑
//	go run ./cmd/play -bots 5    # 5 个电脑
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
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/session"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func main() {
	// 先修终端：Windows 默认 GBK + 不开 VT，不处理会乱码、颜色全变成转义符
	setupConsole()

	bots := flag.Int("bots", 3, "电脑玩家数量（1~5）")
	color := flag.String("color", "block", "颜色深浅：block 满色 / text 文字上色 / plain 无色")
	layout := flag.String("ui", "rich", "界面密度：rich 丰富 / compact 极简")
	stack := flag.Bool("stack", false, "允许 +2/+4 叠加")
	strict := flag.Bool("strict-draw", false, "摸牌后立即结束回合")
	unoPenalty := flag.Bool("uno", false, "开启漏喊 UNO 罚牌")
	challenge := flag.Bool("challenge", true, "允许质疑 +4")
	match := flag.Int("match", 0, "长局目标分；0 表示只打一局")
	flag.Parse()

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
	n := *bots
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}

	rules := engine.Rules{
		Stacking:   *stack,
		StrictDraw: *strict,
		UnoPenalty: *unoPenalty,
		Challenge:  *challenge,
		MatchTo:    *match,
	}

	// 房间在本进程里：不需要后端，也不需要 SSH。
	reg := room.NewRegistry(uno.New(rules), room.Options{BotLag: 300 * time.Millisecond})
	defer reg.Close()

	client := session.New(reg, "local-player", "你")
	// 建房：房间码留空表示新开一间
	if err := client.Join(""); err != nil {
		fmt.Fprintf(os.Stderr, "建房失败：%v\n", err)
		os.Exit(1)
	}
	for i := 0; i < n; i++ {
		if err := client.AddBot(); err != nil {
			fmt.Fprintf(os.Stderr, "加电脑失败：%v\n", err)
			os.Exit(1)
		}
	}

	model := unoUI.NewModel(
		unoUI.Identity{Key: "local", Name: "你"},
		client,
		unoUI.Options{
			Rules:  rules,
			Mode:   mode,
			Layout: layoutMode,
		},
		nil,
	)

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败：%v\n", err)
		os.Exit(1)
	}
}
