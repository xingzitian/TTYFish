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

// newLobbyRoot 造一个"已经进了房间、但还没开局"的场景。
func newLobbyRoot(t *testing.T) *Root {
	t.Helper()
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeClient{phase: client.Lobby}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "x"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
		OpenRoom: func(_, code string) (client.Client, error) { return fake, fake.Join(code) },
	})
	r.Init()
	pressEnter(r) // Game
	pressEnter(r) // UNO
	pressEnter(r) // 创建房间
	if r.level != levelTable {
		t.Fatalf("前置条件失败：应该在牌桌，实际 %v", r.level)
	}
	return r
}

// TestEscInLobbyLeavesImmediately 大厅里按 Esc 直接离开房间，不弹确认。
//
// 这条是修一个真事故：玩家在大厅按 Esc（很自然的"返回"），
// 结果进了确认态，之后除了 Y/N 什么键都不响应，看起来就是"卡死了"。
func TestEscInLobbyLeavesImmediately(t *testing.T) {
	r := newLobbyRoot(t)

	pressEsc(r)
	if r.confirmLeave {
		t.Fatal("大厅里按 Esc 不该弹确认")
	}
	if r.level != levelGame {
		t.Fatalf("应该直接回到游戏菜单，实际 %v", r.level)
	}
}

// TestEscInLobbyDoesNotSwallowKeys 大厅里按 Esc 之后，按键仍然有效。
func TestEscInLobbyDoesNotSwallowKeys(t *testing.T) {
	r := newLobbyRoot(t)
	pressEsc(r) // 离开

	// 还能重新进游戏菜单继续操作
	if !strings.Contains(strip(r.View()), "创建房间") {
		t.Fatal("回到菜单后应该还能看到菜单项")
	}
	pressEnter(r)
	if r.level != levelTable {
		t.Fatalf("应该能重新建房，实际 %v", r.level)
	}
}

// TestEscWhilePlayingAsksConfirm 对局进行中按 Esc 才要确认。
func TestEscWhilePlayingAsksConfirm(t *testing.T) {
	r := newEscRoot(t, &fakeModel{}) // 这个 helper 里 phase 是 Playing
	pressEsc(r)
	if !r.confirmLeave {
		t.Fatal("对局中按 Esc 应该弹确认")
	}
}

// TestQEscapeFromConfirm 确认态里 q 仍然能断开，作为"以为卡死了"的逃生口。
func TestQEscapeFromConfirm(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})
	pressEsc(r)
	if !r.confirmLeave {
		t.Fatal("应该先弹确认")
	}
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !r.quit {
		t.Fatal("确认态里 q 也应该能断开连接")
	}
}
