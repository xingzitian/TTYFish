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

// fakeModel 是最小的游戏界面。
type fakeModel struct{}

func (m *fakeModel) Init() tea.Cmd                       { return nil }
func (m *fakeModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m *fakeModel) View() string                        { return "牌桌" }
func (m *fakeModel) Close()                              {}

// fakeClient 只记录 Join。
type fakeClient struct{ joined string }

func (c *fakeClient) Snapshot() client.Snapshot { return client.Snapshot{Code: c.joined} }
func (c *fakeClient) Updates() <-chan client.Snapshot {
	ch := make(chan client.Snapshot)
	close(ch)
	return ch
}
func (c *fakeClient) Join(code string) error { c.joined = code; return nil }
func (c *fakeClient) Leave() error           { c.joined = ""; return nil }
func (c *fakeClient) Start() error           { return nil }
func (c *fakeClient) AddBot() error          { return nil }
func (c *fakeClient) Act(any) error          { return nil }
func (c *fakeClient) Chat(int) error         { return nil }
func (c *fakeClient) VoteRestart(bool) error { return nil }

// TestOpenRoomUsesInjectedFactory 离线预览的入口：注入的工厂必须被调用，
// 调用之后应该真的切到牌桌屏幕。cmd/preview 就靠这条路径离线跑完整界面。
func TestOpenRoomUsesInjectedFactory(t *testing.T) {
	reg := registry.New()
	model := &fakeModel{}
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return model },
	}); err != nil {
		t.Fatal(err)
	}

	called := ""
	fake := &fakeClient{}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "预览玩家"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
		OpenRoom: func(gameID, code string) (client.Client, error) {
			called = gameID + "|" + code
			return fake, fake.Join(code)
		},
	})
	r.Init()

	pressEnter(r) // Game
	pressEnter(r) // UNO
	pressEnter(r) // 创建房间

	if called != "uno|" {
		t.Fatalf("注入的工厂没被调用，实际 %q", called)
	}
	if r.level != levelTable {
		t.Fatalf("应该在牌桌屏幕，实际 %v", r.level)
	}
	view := r.View()
	if !strings.Contains(view, "牌桌") {
		t.Fatalf("牌桌界面应该渲染出来：%q", view)
	}
	if !strings.Contains(view, "预览玩家") {
		t.Fatalf("牌桌顶部应该保留标题栏：%q", view)
	}
}

// TestInjectedFactoryError 工厂报错时留在输入界面并显示原因，不能崩。
func TestInjectedFactoryError(t *testing.T) {
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
		OpenRoom: func(string, string) (client.Client, error) { return nil, errTest{} },
	})
	r.Init()
	pressEnter(r) // Game
	pressEnter(r) // UNO
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	pressEnter(r) // 加入房间

	for _, ch := range "ABC234" {
		r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	pressEnter(r)

	if r.level != levelJoin {
		t.Fatalf("开房失败时应该留在输入界面，实际 %v", r.level)
	}
	if r.joinErr == "" {
		t.Fatal("应该把失败原因显示出来")
	}
}

type errTest struct{}

func (errTest) Error() string { return "房间里没人" }
