package main

import (
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	unoUI "github.com/xingzitian/TTYFish/games/uno/ui"
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/session"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestLocalRoomRenders 验证单机链路：本地房间 + 本地客户端 + 界面能出画面。
//
// 这条路径不经过 SSH 和后端，最容易因为改动悄悄坏掉：
// 以前只要 session/room 的接口变一个签名，单机版就到不了牌桌。
func TestLocalRoomRenders(t *testing.T) {
	for _, layout := range []ui.Layout{ui.LayoutRich, ui.LayoutCompact} {
		rules := engine.Rules{Challenge: true}
		reg := room.NewRegistry(uno.New(rules), room.Options{BotLag: 10 * time.Millisecond})
		c := session.New(reg, "local-player", "你")
		if err := c.Join(""); err != nil {
			t.Fatalf("建房失败：%v", err)
		}

		snap := c.Snapshot()
		if snap.Code == "" {
			t.Fatal("建完房快照里应该有房间码")
		}

		m := unoUI.NewModel(
			unoUI.Identity{Key: "local", Name: "你"},
			c,
			unoUI.Options{Rules: rules, Mode: ui.ColorNone, Layout: layout},
			nil,
		)
		out := m.View()
		if !strings.Contains(out, snap.Code) {
			t.Fatalf("大厅界面应该显示房间码 %s：\n%s", snap.Code, out)
		}
		if !strings.Contains(out, "开始") {
			t.Fatalf("大厅应该有开局提示：\n%s", out)
		}

		reg.Close()
	}
}
