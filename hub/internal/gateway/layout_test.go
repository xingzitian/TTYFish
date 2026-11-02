package gateway

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func newLayoutRoot(t *testing.T, seen *ui.Layout) *Root {
	t.Helper()
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(c registry.UIContext) registry.Model {
			*seen = c.Layout
			return &fakeModel{}
		},
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeClient{}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "预览玩家"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
		Layout:   ui.LayoutRich,
		// 故意不提供 LayoutOf / SetLayout，模拟 cmd/preview
		OpenRoom: func(_, code string) (client.Client, error) {
			return fake, fake.Join(code)
		},
	})
	r.Init()
	return r
}

// TestLayoutTogglesWithoutPersistence 没有偏好回调时（离线预览），
// 设置里的"显示效果"也必须能切——切换是内存里的状态，落盘只是顺带。
func TestLayoutTogglesWithoutPersistence(t *testing.T) {
	var seen ui.Layout
	r := newLayoutRoot(t, &seen)

	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // 移到 Settings
	pressEnter(r)
	if r.level != levelSettings {
		t.Fatalf("应该在设置界面，实际 %v", r.level)
	}
	if view := strip(r.View()); !strings.Contains(view, "显示效果：丰富") {
		t.Fatalf("默认应该是丰富：\n%s", view)
	}

	pressEnter(r) // 回车切换
	if r.layout() != ui.LayoutCompact {
		t.Fatalf("回车后应该切成极简，实际 %v", r.layout())
	}
	if view := strip(r.View()); !strings.Contains(view, "显示效果：极简") {
		t.Fatalf("标签应该跟着变：\n%s", view)
	}

	pressEnter(r) // 再切回来
	if r.layout() != ui.LayoutRich {
		t.Fatalf("再按一次应该切回丰富，实际 %v", r.layout())
	}
}

// TestChosenLayoutReachesGameUI 菜单里选的界面密度要传给游戏界面，
// 否则预览里"设置了也没用"。
func TestChosenLayoutReachesGameUI(t *testing.T) {
	var seen ui.Layout
	r := newLayoutRoot(t, &seen)

	// 设置里切成极简
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	pressEnter(r)
	pressEnter(r)
	if r.layout() != ui.LayoutCompact {
		t.Fatalf("应该已经是极简，实际 %v", r.layout())
	}

	// 回主菜单 → Game → UNO → 创建房间
	pressEsc(r)
	pressEnter(r)
	pressEnter(r)
	pressEnter(r)

	if seen != ui.LayoutCompact {
		t.Fatalf("游戏界面应该拿到极简，实际 %v", seen)
	}
}
