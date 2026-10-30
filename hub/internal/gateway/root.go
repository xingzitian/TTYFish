// Package gateway 把"大厅选游戏"和"游戏界面"串成一个 bubbletea 模型。
//
// 一条 SSH 连接只有一个 tea.Program，所以两个界面得放在同一个模型里来回切换：
// 先显示大厅，选完游戏就建房间、把后续消息交给游戏界面。
package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/lobby"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/remote"
	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
)

// Deps 是网关界面运行所需的外部依赖。
type Deps struct {
	Accounts   *backend.Client
	Backend    string
	Registry   *registry.Registry
	Player     backend.Player
	Output     io.Writer
	Term       string
	Mode       sdkui.ColorMode
	ModeOf     func(string) (sdkui.ColorMode, bool)
	SetMode    func(string, sdkui.ColorMode)
	Layout     sdkui.Layout
	LayoutOf   func(string) (sdkui.Layout, bool)
	SetLayout  func(string, sdkui.Layout)
	Renderer   *lipgloss.Renderer
	ResumeMode bool // true 表示进来时先尝试恢复原房间
}

// Root 是这条连接的根模型。
type Root struct {
	deps Deps

	lob   *lobby.Model
	game  registry.Model
	conn  *remote.Client
	ready bool

	width, height int
	closed        bool
	quit          bool
}

func New(deps Deps) *Root {
	return &Root{deps: deps, width: 80, height: 24}
}

func (r *Root) Init() tea.Cmd {
	if r.deps.ResumeMode {
		if resumed := r.resume(); resumed {
			return r.game.Init()
		}
	}
	r.startLobby()
	return r.lob.Init()
}

// resume 尝试接回原来的房间；成功就返回对应的游戏界面。
func (r *Root) resume() bool {
	conn := remote.New(r.deps.Backend, r.deps.Player.Token, "")
	ok, err := conn.Resume()
	if err != nil || !ok {
		return false
	}
	entry, exists := r.deps.Registry.Get(conn.GameID())
	if !exists {
		return false
	}
	r.conn = conn
	r.game = entry.NewUI(registry.UIContext{
		Key: "account:" + r.deps.Player.Account.ID, Name: r.deps.Player.Account.Username,
		Client: conn, Output: r.deps.Output, Term: r.deps.Term,
		Mode: r.deps.Mode, ModeOf: r.deps.ModeOf, SetMode: r.deps.SetMode,
		Layout: r.deps.Layout, LayoutOf: r.deps.LayoutOf, SetLayout: r.deps.SetLayout,
	})
	slog.Info("恢复房间", "account", r.deps.Player.Account.ID, "game", entry.ID)
	return true
}

// startLobby 取"后端开放 ∩ 网关挂载"的游戏列表。
func (r *Root) startLobby() {
	var games []lobby.Game
	for _, e := range r.deps.Registry.List() {
		games = append(games, lobby.Game{ID: e.ID, Name: gameName(e), Min: e.Game.MinSeats(), Max: e.Game.MaxSeats()})
	}
	// 后端那边还开着但网关没挂 UI 的游戏不该出现——反过来也成立，
	// 所以以后后端有多个游戏时，这里要按 /games 的返回过滤。现在只有 uno，够用。
	r.lob = lobby.New(games, r.deps.Mode, r.deps.Renderer)
}

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = v.Width, v.Height
	}

	if r.game != nil {
		next, cmd := r.game.Update(msg)
		if m, ok := next.(registry.Model); ok {
			r.game = m
		}
		return r, cmd
	}

	if r.lob == nil {
		r.startLobby()
	}
	var cmd tea.Cmd
	var next tea.Model
	next, cmd = r.lob.Update(msg)
	if l, ok := next.(*lobby.Model); ok {
		r.lob = l
	}
	if !r.lob.Done() {
		return r, cmd
	}
	if r.lob.Chosen() == "" {
		// 玩家自己退出
		r.quit = true
		return r, tea.Quit
	}
	if err := r.enter(r.lob.Chosen()); err != nil {
		fmt.Fprintf(r.deps.Output, "进入游戏失败：%v\r\n", err)
		r.startLobby()
		return r, nil
	}
	return r, r.game.Init()
}

// enter 按标识建客户端并创建房间。
func (r *Root) enter(gameID string) error {
	entry, ok := r.deps.Registry.Get(gameID)
	if !ok {
		return fmt.Errorf("没有这个游戏")
	}
	// 只建客户端，不建房：建房还是加入房间，由游戏界面自己问玩家
	// （UNO 的第一屏就是"输入房间码加入 / 直接回车开新房"）。
	conn := remote.New(r.deps.Backend, r.deps.Player.Token, gameID)
	r.conn = conn
	r.game = entry.NewUI(registry.UIContext{
		Key: "account:" + r.deps.Player.Account.ID, Name: r.deps.Player.Account.Username,
		Client: conn, Output: r.deps.Output, Term: r.deps.Term,
		Mode: r.deps.Mode, ModeOf: r.deps.ModeOf, SetMode: r.deps.SetMode,
		Layout: r.deps.Layout, LayoutOf: r.deps.LayoutOf, SetLayout: r.deps.SetLayout,
	})
	slog.Info("进入游戏", "account", r.deps.Player.Account.ID, "game", gameID)
	return nil
}

func (r *Root) View() string {
	if r.quit {
		return ""
	}
	if r.game != nil {
		return r.game.View()
	}
	if r.lob != nil {
		return r.lob.View()
	}
	return ""
}

// Close 释放订阅；不离开房间（座位要留给重连）。
func (r *Root) Close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.game != nil {
		r.game.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}

// WaitReady 只用来在测试里等界面就绪。
func (r *Root) WaitReady(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if r.game != nil || r.lob != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// gameName 取游戏的展示名；规则实现没写名字时退回标识。
func gameName(e registry.Entry) string {
	if e.Game != nil {
		if n := e.Game.Name(); n != "" {
			return n
		}
	}
	return e.ID
}

var _ = context.Background
