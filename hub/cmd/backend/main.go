// Command backend runs the standalone account API without Web pages or SSH listeners.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/api"
	"github.com/xingzitian/TTYFish/hub/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("后端退出", "err", err)
		os.Exit(1)
	}
}
func run() error {
	addr := flag.String("addr", "127.0.0.1:8081", "账号 API 监听地址")
	db := flag.String("db", "backend.db", "账号数据库文件")
	origins := flag.String("cors-origins", "", "允许的浏览器来源，逗号分隔，例如 http://localhost:5173")
	flag.Parse()
	st, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg := api.Config{GatewayToken: os.Getenv("TTYFISH_GATEWAY_TOKEN")}
	if *origins != "" {
		for _, origin := range strings.Split(*origins, ",") {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, strings.TrimSpace(origin))
		}
	}
	handler, err := api.New(account.New(st), cfg)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: handler.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { slog.Info("账号 API 已启动", "addr", *addr); done <- srv.ListenAndServe() }()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			st.Sweep()
		case err := <-done:
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return srv.Shutdown(shutdown)
		}
	}
}
