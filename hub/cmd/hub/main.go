// Command hub 是 CLI_GAME 平台的服务端。
//
// 它负责账号、房间、SSH 接入，并把游戏挂进来——现在只挂了 uno。
// 游戏本身是独立模块（games/uno），平台不认识它的规则。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	unoUI "github.com/xingzitian/TTYFish/games/uno/ui"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/sshd"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"github.com/xingzitian/TTYFish/hub/internal/web"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func main() {
	addr := flag.String("addr", ":2222", "SSH 监听地址")
	httpAddr := flag.String("http", ":8080", "HTTP 监听地址（空表示不开）")
	dbPath := flag.String("db", "hub.db", "SQLite 文件")
	hostKey := flag.String("host-key", "host_ed25519", "host key 文件（不存在会自动生成）")
	color := flag.String("color", "block", "默认颜色深浅：block 满色 / text 文字上色 / plain 无色")
	stack := flag.Bool("stack", false, "允许 +2/+4 叠加")
	strict := flag.Bool("strict-draw", false, "摸牌后立即结束回合")
	unoPenalty := flag.Bool("uno", false, "开启漏喊 UNO 罚牌")
	challenge := flag.Bool("challenge", true, "允许质疑 +4")
	match := flag.Int("match", 0, "长局目标分；0 表示只打一局")
	botDelay := flag.Duration("bot-delay", 800*time.Millisecond, "电脑思考时间")
	idle := flag.Duration("idle-timeout", 30*time.Minute, "空闲连接超时")
	sshHint := flag.String("ssh-hint", "hub 你的服务器", "网页上显示的 ssh 连接提示")
	flag.Parse()

	mode, ok := ui.ParseColorMode(*color)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知的颜色深浅 %q（可选 block / text / plain）\n", *color)
		os.Exit(2)
	}

	rules := engine.Rules{
		Stacking:   *stack,
		StrictDraw: *strict,
		UnoPenalty: *unoPenalty,
		Challenge:  *challenge,
		MatchTo:    *match,
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	srv, err := sshd.New(sshd.Config{
		Addr:        *addr,
		HostKeyPath: *hostKey,
		Game: registry.Entry{
			ID:   "uno",
			Game: uno.New(rules),
			NewUI: func(c registry.UIContext) registry.Model {
				return unoUI.NewModel(unoUI.Identity{Key: c.Key, Name: c.Name}, c.Client, unoUI.Options{
					Rules: rules, Mode: c.Mode, ModeOf: c.ModeOf, SetMode: c.SetMode,
				}, unoUI.NewRenderer(c.Output, c.Term))
			},
		},
		PrefsPath:   filepath.Join(filepath.Dir(*hostKey), "uno-prefs.json"),
		Store:       st,
		BotDelay:    *botDelay,
		IdleTimeout: *idle,
		Mode:        mode,
	})
	if err != nil {
		slog.Error("启动 SSH 失败", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// HTTP：登录页 + 账号页。
	var hs *http.Server
	if *httpAddr != "" {
		hs = &http.Server{
			Addr:              *httpAddr,
			Handler:           web.New(st, *sshHint).Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			slog.Info("HTTP 已启动", "addr", *httpAddr, "db", filepath.Clean(*dbPath))
			if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTP 退出", "err", err)
			}
		}()
	}

	// 定期清理过期挑战和会话。
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st.Sweep()
			}
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if hs != nil {
			_ = hs.Shutdown(shutdownCtx)
		}
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("hub 启动", "addr", *addr, "host_key", *hostKey)
	if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
		slog.Error("监听失败", "err", err)
		os.Exit(1)
	}
}
