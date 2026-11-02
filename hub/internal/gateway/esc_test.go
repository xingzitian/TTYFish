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

// modalModel 是"有弹窗"的游戏界面，用来验证 Esc 归属。
type modalModel struct {
	modal   bool
	forward int // 收到多少次按键
}

func (m *modalModel) Init() tea.Cmd { return nil }
func (m *modalModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	m.forward++
	return m, nil
}
func (m *modalModel) View() string   { return "牌桌" }
func (m *modalModel) Close()         {}
func (m *modalModel) HasModal() bool { return m.modal }

func newEscRoot(t *testing.T, model registry.Model) *Root {
	t.Helper()
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return model },
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeClient{}
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

// TestEscLeavesRoomBackToMenu 牌桌上按 Esc 先确认，确认后离开房间回上一级。
func TestEscLeavesRoomBackToMenu(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})

	pressEsc(r)
	if r.quit {
		t.Fatal("Esc 不该退出程序")
	}
	if !r.confirmLeave {
		t.Fatal("Esc 应该先弹确认")
	}
	if r.level != levelTable {
		t.Fatalf("确认之前不该离开牌桌，实际 %v", r.level)
	}

	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if r.level != levelGame {
		t.Fatalf("Esc 应该回到游戏菜单，实际 %v", r.level)
	}
	if r.game != nil || r.roomClient != nil {
		t.Fatal("回到菜单后不该还挂着房间")
	}

	// 还能继续往下走，说明连接没断（回菜单后光标在"创建房间"，下移到"加入房间"）
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	pressEnter(r)
	if r.level != levelJoin {
		t.Fatalf("应该还能进加入房间界面，实际 %v", r.level)
	}
}

// TestQDisconnectsImmediately 牌桌上按 q 是老板键：立刻断开，不弹确认。
func TestQDisconnectsImmediately(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})

	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if r.confirmLeave {
		t.Fatal("q 是老板键，不该走确认")
	}
	if !r.quit {
		t.Fatal("q 应该直接退出连接")
	}
}

// TestEscGoesToGameWhenModal 游戏正在弹窗时，Esc 归游戏处理，不能把玩家踢出房间。
func TestEscGoesToGameWhenModal(t *testing.T) {
	model := &modalModel{modal: true}
	r := newEscRoot(t, model)

	pressEsc(r)
	if r.level != levelTable {
		t.Fatalf("弹窗时 Esc 不该离开房间，实际 %v", r.level)
	}
	if model.forward == 0 {
		t.Fatal("Esc 应该转发给游戏界面去关弹窗")
	}
}

// TestEscLeavesWhenNoModal 弹窗关掉之后，Esc 又变回"退出房间"。
func TestEscLeavesWhenNoModal(t *testing.T) {
	model := &modalModel{modal: false}
	r := newEscRoot(t, model)

	pressEsc(r)
	if !r.confirmLeave || r.level != levelTable {
		t.Fatalf("没有弹窗时 Esc 应该先弹确认，实际 confirm=%v level=%v", r.confirmLeave, r.level)
	}
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if r.level != levelGame {
		t.Fatalf("确认后应该离开房间，实际 %v", r.level)
	}
}

// TestHeaderNotInMenuAfterLeave 回到菜单后标题栏跟着换。
func TestHeaderAfterLeave(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})
	pressEsc(r)
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if view := strip(r.View()); !strings.Contains(view, "Game") {
		t.Fatalf("标题栏应该回到 Game 层级：\n%s", view)
	}
}
