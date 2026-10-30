// Command hub 是 SSH 网关。
//
// 它只做三件事：处理 SSH（认证、PTY、窗口尺寸）、拿玩家令牌、把后端推来的
// 快照渲染成终端界面。**不建房间、不执行游戏规则、不碰数据库**——那些都在
// cmd/backend 里。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	unoUI "github.com/xingzitian/TTYFish/games/uno/ui"
	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/sshd"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func main() {
	addr := flag.String("addr", ":2222", "SSH 监听地址")
	hostKey := flag.String("host-key", "host_ed25519", "host key 文件（不存在会自动生成）")
	backendURL := flag.String("backend", "http://127.0.0.1:8081", "后端房间 API 地址")
	gatewayToken := flag.String("gateway-token", os.Getenv("TTYFISH_GATEWAY_TOKEN"), "网关凭证（默认读环境变量 TTYFISH_GATEWAY_TOKEN）")
	color := flag.String("color", "block", "默认颜色深浅：block 满色 / text 文字上色 / plain 无色")
	idle := flag.Duration("idle-timeout", 30*time.Minute, "空闲连接超时")
	flag.Parse()

	mode, ok := ui.ParseColorMode(*color)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知的颜色深浅 %q（可选 block / text / plain）\n", *color)
		os.Exit(2)
	}
	if *gatewayToken == "" {
		fmt.Fprintln(os.Stderr, "缺少网关凭证：用 -gateway-token 或环境变量 TTYFISH_GATEWAY_TOKEN 提供")
		os.Exit(2)
	}

	accounts := backend.New(*backendURL, *gatewayToken)

	// 启动前先确认后端可达，免得玩家连上来才发现连不上。
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	if err := accounts.Health(checkCtx); err != nil {
		cancelCheck()
		slog.Error("后端不可达", "backend", *backendURL, "err", err)
		os.Exit(1)
	}
	cancelCheck()

	// 规则由后端决定；这里这份只用于界面展示（谁在出牌、剩几张）。
	displayRules := engine.Rules{Challenge: true}

	srv, err := sshd.New(sshd.Config{
		Addr:        *addr,
		HostKeyPath: *hostKey,
		BackendURL:  *backendURL,
		Accounts:    accounts,
		PrefsPath:   filepath.Join(filepath.Dir(*hostKey), "prefs.json"),
		IdleTimeout: *idle,
		Mode:        mode,
		Game: registry.Entry{
			ID:   "uno",
			Game: uno.New(displayRules),
			NewUI: func(c registry.UIContext) registry.Model {
				return unoUI.NewModel(unoUI.Identity{Key: c.Key, Name: c.Name}, c.Client, unoUI.Options{
					Rules: displayRules, Mode: c.Mode, ModeOf: c.ModeOf, SetMode: c.SetMode,
				}, unoUI.NewRenderer(c.Output, c.Term))
			},
		},
	})
	if err != nil {
		slog.Error("启动 SSH 失败", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("SSH 网关启动", "addr", *addr, "backend", *backendURL)
	if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
		slog.Error("监听失败", "err", err)
		os.Exit(1)
	}
}
