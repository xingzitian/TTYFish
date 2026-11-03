package gateway

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestHeaderShowsBalance 标题栏要显示真实 PT 余额；没查到时用 — 占位。
func TestHeaderShowsBalance(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "sakitami"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	r.Init()

	// 还没查到余额时是占位
	if view := strip(r.View()); !strings.Contains(view, "—pt") {
		t.Fatalf("没查到余额时应该显示占位：\n%s", view)
	}

	// 收到余额消息后换成真实值
	r.Update(pointsMsg{pt: "6.8"})
	if view := strip(r.View()); !strings.Contains(view, "6.8pt") {
		t.Fatalf("应该显示 6.8pt：\n%s", view)
	}
	if view := strip(r.View()); strings.Contains(view, "—pt") {
		t.Fatalf("查到之后不该再有占位：\n%s", view)
	}
}

// TestPointsQuerySkippedWithoutToken 没有后端/令牌时（预览模式）不能去发请求。
func TestPointsQuerySkippedWithoutToken(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "preview", Username: "预览"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	if cmd := r.refreshPoints(); cmd != nil {
		t.Fatal("没有令牌时不该发请求")
	}
}

// TestPointsErrorDoesNotClearBalance 查余额失败时保留上一次的值，别把数字弄丢。
func TestPointsErrorDoesNotClearBalance(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "x"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	r.Init()
	r.Update(pointsMsg{pt: "12.3"})
	r.Update(pointsMsg{err: errTest{}})

	if view := strip(r.View()); !strings.Contains(view, "12.3pt") {
		t.Fatalf("失败时应该保留旧值：\n%s", view)
	}
}

var _ = tea.Quit
