// Package gateway 是一条 SSH 连接上的界面总控。
//
// 它把几个屏幕拼成一个 bubbletea 模型：主菜单 → 游戏列表 → 单个游戏 → 房间。
// 一条连接只有一个 tea.Program，所以切换屏幕就是换一个字段，而不是起新程序。
package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/menu"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/remote"
	"github.com/xingzitian/TTYFish/sdk/client"
	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
)

// Deps 是网关界面运行所需的外部依赖。
type Deps struct {
	Accounts  *backend.Client
	Backend   string
	Registry  *registry.Registry
	Player    backend.Player
	Output    io.Writer
	Term      string
	Mode      sdkui.ColorMode
	ModeOf    func(string) (sdkui.ColorMode, bool)
	SetMode   func(string, sdkui.ColorMode)
	Layout    sdkui.Layout
	LayoutOf  func(string) (sdkui.Layout, bool)
	SetLayout func(string, sdkui.Layout)
	Renderer  *lipgloss.Renderer
	// Version 显示在标题栏，构建时注入；空值显示 dev。
	Version string
	// WebURL 是"进入 Web 设置"要打开的地址；没配置就提示未配置。
	WebURL string
	// ResumeMode 为 true 时先尝试接回原来的房间。
	ResumeMode bool
	// OpenRoom 决定"创建/加入房间"走哪条链路。
	//
	// 生产用 HTTP 客户端（remote）。留空时用默认实现；
	// 离线预览（cmd/preview）会注入本地房间，一个网络请求都不发。
	OpenRoom func(gameID, code string) (client.Client, error)
}

// level 是当前显示的屏幕。
type level int

const (
	levelHome level = iota
	levelGames
	levelGame
	levelJoin
	levelSettings
	levelTable
)

func (l level) title() string {
	switch l {
	case levelGames:
		return "Gamelist"
	case levelGame:
		return "Game"
	case levelJoin:
		return "Join"
	case levelSettings:
		return "Settings"
	case levelTable:
		return "Room"
	default:
		return "Home"
	}
}

// Root 是这条连接的根模型。
type Root struct {
	// mu 保护界面状态：bubbletea 的 Update/View 跑在事件循环里，
	// 会话结束时 sshd 会从另一个 goroutine 调 Close()。
	mu sync.Mutex

	deps Deps

	level           level
	menu            *menu.Model
	join            textinput.Model
	joinErr         string
	settingsMessage string
	// balancePT 是标题栏显示的 PT 余额；空表示还没查到（显示 —pt）。
	balancePT string

	game registry.Model
	conn *remote.Client
	// roomClient 是当前房间的客户端（生产是 remote，预览是本地 session）。
	// 退回上一层时要靠它离开房间。
	roomClient client.Client
	gameID     string
	// confirmLeave 表示"正在确认是否离开这局"。
	// 牌桌上按 Esc/q 先问一句，避免手快把对局丢了。
	confirmLeave bool

	// layoutNow 是当前选中的界面密度。必须自己留一份：
	// 偏好回调只是"持久化"，没有回调时（离线预览）也得能切。
	layoutNow sdkui.Layout

	width, height int
	closed        bool
	quit          bool
}

func New(deps Deps) *Root {
	r := &Root{deps: deps, width: 80, height: 24, layoutNow: deps.Layout}
	if deps.LayoutOf != nil {
		if l, ok := deps.LayoutOf(r.prefKey()); ok {
			r.layoutNow = l
		}
	}
	r.menu = menu.New(deps.Renderer, deps.Mode, r.width, r.height)

	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 6
	in.Width = 10
	r.join = in

	return r
}

func (r *Root) Init() tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()

	var cmds []tea.Cmd
	if c := r.refreshPoints(); c != nil {
		cmds = append(cmds, c)
	}
	if r.deps.ResumeMode && r.resume() {
		cmds = append(cmds, r.game.Init())
		return tea.Batch(cmds...)
	}
	r.gotoLevel(levelHome)
	return tea.Batch(cmds...)
}

// pointsMsg 是异步查回来的余额。
type pointsMsg struct {
	pt  string
	err error
}

// refreshPoints 发一个查余额的命令；拿不到令牌时返回 nil（预览模式）。
func (r *Root) refreshPoints() tea.Cmd {
	if r.deps.Accounts == nil || r.deps.Player.Token == "" {
		return nil
	}
	accounts := r.deps.Accounts
	token := r.deps.Player.Token
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		p, err := accounts.PlayerPoints(ctx, token)
		if err != nil {
			return pointsMsg{err: err}
		}
		return pointsMsg{pt: p.BalancePT}
	}
}

// prefKey 是偏好读写用的键，必须和游戏界面用的那个一致。
func (r *Root) prefKey() string { return "account:" + r.deps.Player.Account.ID }

func (r *Root) layout() sdkui.Layout { return r.layoutNow }

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
	r.conn, r.gameID = conn, entry.ID
	r.game = r.newGameUI(entry, conn)
	r.level = levelTable
	slog.Info("恢复房间", "account", r.deps.Player.Account.ID, "game", entry.ID)
	return true
}

func (r *Root) newGameUI(entry registry.Entry, client_ client.Client) registry.Model {
	return entry.NewUI(registry.UIContext{
		Key: r.prefKey(), Name: r.deps.Player.Account.Username,
		Client: client_, Output: r.deps.Output, Term: r.deps.Term,
		Mode: r.deps.Mode, ModeOf: r.deps.ModeOf, SetMode: r.deps.SetMode,
		Layout: r.layout(), LayoutOf: r.deps.LayoutOf, SetLayout: r.deps.SetLayout,
	})
}

// gotoLevel 切换屏幕并重建对应的条目。
func (r *Root) gotoLevel(l level) {
	if l != levelSettings {
		r.settingsMessage = ""
	}
	r.level = l
	r.menu.SetSize(r.width, r.height)
	// 每个菜单都从第一项开始，别把上一屏的光标带过来
	r.menu.Reset()
	switch l {
	case levelHome:
		r.menu.SetItems([]menu.Item{
			{Label: "Game", Enabled: true},
			{Label: "Settings", Enabled: true},
			{Label: "Exit", Enabled: true},
		})
	case levelGames:
		var items []menu.Item
		for _, e := range r.deps.Registry.List() {
			// 房间数要等后端提供公开房间目录，先放占位
			items = append(items, menu.Item{Label: gameName(e), Hint: "（— 个房间）", Enabled: true})
		}
		if len(items) == 0 {
			items = append(items, menu.Item{Label: "（这个网关没有挂任何游戏）", Enabled: false})
		}
		r.menu.SetItems(items)
	case levelGame:
		r.menu.SetItems([]menu.Item{
			{Label: "创建房间", Enabled: true},
			{Label: "加入房间", Enabled: true},
			// 后端目前没有公开房间目录，这一项先占位
			{Label: "浏览房间", Hint: "（— 个公开房间）", Enabled: false},
		})
	case levelSettings:
		layout := "丰富"
		if r.layout() == sdkui.LayoutCompact {
			layout = "极简"
		}
		passwordLogin := r.deps.Player.Account.PasswordLoginEnabled
		passwordLabel := "关闭"
		if passwordLogin {
			passwordLabel = "开启"
		}
		canTogglePasswordLogin := r.deps.Accounts != nil && r.deps.Player.Token != ""
		passwordHint := ""
		if !canTogglePasswordLogin {
			passwordHint = "（不可用）"
		} else if passwordLogin && !r.deps.Player.Account.HasSSHKey {
			canTogglePasswordLogin = false
			passwordHint = "（需先绑定 SSH 公钥）"
		}
		r.menu.SetItems([]menu.Item{
			{Label: "显示效果：" + layout, Enabled: true},
			{Label: "密码登录：" + passwordLabel, Hint: passwordHint, Enabled: canTogglePasswordLogin},
			{Label: "进入 Web 设置", Enabled: true},
		})
	case levelJoin:
		r.join.Focus()
		r.join.SetValue("")
		r.joinErr = ""
	}
}

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		r.width, r.height = size.Width, size.Height
		r.menu.SetSize(r.width, r.height)
	}

	// 牌桌上时把消息交给游戏界面；高度留一行给标题栏。
	if r.level == levelTable && r.game != nil {
		// Esc / q 在牌桌上是"退出房间、回上一级"，不是断线。
		// 但游戏正在弹窗（选色/聊天/质疑/老板键）时，Esc 归游戏处理，
		// 否则玩家连弹窗都退不出去。
		if key, ok := msg.(tea.KeyMsg); ok && !r.gameHasModal() {
			switch {
			case r.confirmLeave:
				// 确认中只认几个键，其它一律忽略——这时最怕误触。
				// q 仍然保留：万一确认框让人以为卡死了，老板键必须还能用。
				switch key.String() {
				case "y":
					return r, r.leaveRoom()
				case "n", "esc":
					return r, r.setConfirmLeave(false)
				case "q":
					r.quit = true
					return r, tea.Quit
				}
				return r, nil
			case key.String() == "esc":
				// 对局进行中才问：手快按一下不至于把打了一半的局丢了。
				// 大厅里（还没开局）离开是低风险操作，直接走，别让人以为卡住了。
				if r.inGame() {
					return r, r.setConfirmLeave(true)
				}
				return r, r.leaveRoom()
			case key.String() == "q":
				// 老板键：立刻断开这条连接，终端回到本地提示符，
				// 旁人看到的就是"这人干完活退出了"。不留确认，要的就是快。
				r.quit = true
				return r, tea.Quit
			}
		}

		next, cmd := r.game.Update(r.gameMsg(msg))
		if m, ok := next.(registry.Model); ok {
			r.game = m
		}
		// 玩家在游戏里离开房间（客户端不再有房间码）→ 回到游戏菜单
		if r.conn != nil && r.conn.Code() == "" {
			r.game = nil
			r.conn = nil
			r.gotoLevel(levelGame)
		}
		return r, cmd
	}

	if p, ok := msg.(pointsMsg); ok {
		if p.err == nil {
			r.balancePT = p.pt
		} else {
			slog.Warn("查询 PT 余额失败", "err", p.err)
		}
		return r, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		return r, r.handleKey(key)
	}
	return r, nil
}

// inGame 表示这一局已经开打了（不是还在大厅等人）。
func (r *Root) inGame() bool {
	if r.roomClient == nil {
		return false
	}
	return r.roomClient.Snapshot().Phase == client.Playing
}

// gameHasModal 问游戏界面现在有没有弹窗占着键盘。
func (r *Root) gameHasModal() bool {
	modal, ok := r.game.(registry.Modal)
	return ok && modal.HasModal()
}

// setConfirmLeave 打开/关闭"离开确认"，并把少了一行的高度重新告诉游戏界面。
func (r *Root) setConfirmLeave(v bool) tea.Cmd {
	if r.confirmLeave == v {
		return nil
	}
	r.confirmLeave = v
	if r.game == nil {
		return nil
	}
	extra := 1 // 标题栏
	if v {
		extra = 2 // 标题栏 + 确认行
	}
	h := r.height - extra
	if h < 1 {
		h = 1
	}
	next, cmd := r.game.Update(tea.WindowSizeMsg{Width: r.width, Height: h})
	if m, ok := next.(registry.Model); ok {
		r.game = m
	}
	return cmd
}

// leaveRoom 主动离开房间并回到游戏菜单。
//
// 这是"我退出这局"，和"关掉 SSH"不是一回事：关掉连接时座位会保留、
// 由电脑代打，回来还能接上；这里是真的走人。
func (r *Root) leaveRoom() tea.Cmd {
	client_ := r.roomClient
	game := r.game
	r.confirmLeave = false
	r.roomClient, r.conn, r.game = nil, nil, nil
	if game != nil {
		game.Close()
	}
	if client_ != nil {
		if err := client_.Leave(); err != nil {
			slog.Warn("离开房间失败", "err", err)
		}
	}
	r.gotoLevel(levelGame)
	// 一局打完可能拿到奖励，回到菜单时重新拉一次余额
	return r.refreshPoints()
}

// gameMsg 把窗口尺寸减掉标题栏那一行再转给游戏界面。
func (r *Root) gameMsg(msg tea.Msg) tea.Msg {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		h := size.Height - 1
		if h < 1 {
			h = 1
		}
		return tea.WindowSizeMsg{Width: size.Width, Height: h}
	}
	return msg
}

func (r *Root) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()

	if key == "ctrl+c" {
		r.quit = true
		return tea.Quit
	}

	if r.level == levelJoin {
		return r.handleJoinKey(k, key)
	}

	// 菜单：-= 和方向键移动，数字直选，回车确认，Esc 返回
	switch key {
	case "-", "up", "k":
		r.menu.Move(-1)
		return nil
	case "=", "down", "j":
		r.menu.Move(1)
		return nil
	case "esc":
		return r.back()
	case "q":
		r.quit = true
		return tea.Quit
	case "enter", " ":
		return r.pick(r.menu.Cursor())
	}

	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if r.menu.Select(idx + 1) {
			return r.pick(idx)
		}
	}
	return nil
}

// back 返回上一层；在 Home 上就是退出。
func (r *Root) back() tea.Cmd {
	switch r.level {
	case levelHome:
		r.quit = true
		return tea.Quit
	case levelGames, levelSettings:
		r.gotoLevel(levelHome)
	case levelGame:
		r.gotoLevel(levelGames)
	case levelJoin:
		r.gotoLevel(levelGame)
	}
	return nil
}

// pick 处理菜单里的回车/数字选中。
func (r *Root) pick(idx int) tea.Cmd {
	item, ok := r.menu.Selected()
	if !ok || idx != r.menu.Cursor() {
		return nil
	}

	switch r.level {
	case levelHome:
		switch item.Label {
		case "Game":
			r.gotoLevel(levelGames)
		case "Settings":
			r.gotoLevel(levelSettings)
		default:
			r.quit = true
			return tea.Quit
		}
	case levelGames:
		entries := r.deps.Registry.List()
		if idx < 0 || idx >= len(entries) {
			return nil
		}
		r.gameID = entries[idx].ID
		r.gotoLevel(levelGame)
	case levelGame:
		switch idx {
		case 0:
			return r.openRoom("")
		case 1:
			r.gotoLevel(levelJoin)
		}
	case levelSettings:
		switch idx {
		case 0:
			next := sdkui.LayoutRich
			if r.layout() == sdkui.LayoutRich {
				next = sdkui.LayoutCompact
			}
			r.layoutNow = next
			if r.deps.SetLayout != nil {
				r.deps.SetLayout(r.prefKey(), next)
			}
			r.gotoLevel(levelSettings) // 重画，让标签跟着变
		case 1:
			enabled := !r.deps.Player.Account.PasswordLoginEnabled
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := r.deps.Accounts.SetPasswordLoginEnabled(ctx, r.deps.Player.Token, enabled)
			cancel()
			if err != nil {
				r.settingsMessage = "密码登录设置失败：" + err.Error()
				r.gotoLevel(levelSettings)
				return nil
			}
			r.deps.Player.Account.PasswordLoginEnabled = enabled
			r.settingsMessage = "密码登录已关闭"
			if enabled {
				r.settingsMessage = "密码登录已开启"
			}
			r.gotoLevel(levelSettings)
		case 2:
			if r.deps.WebURL == "" {
				fmt.Fprint(r.deps.Output, "\r\n网页地址还没配置（网关启动参数 -web-url）\r\n")
				return nil
			}
			fmt.Fprintf(r.deps.Output, "\r\n在浏览器打开：%s\r\n", r.deps.WebURL)
		}
	}
	return nil
}

func (r *Root) handleJoinKey(k tea.KeyMsg, key string) tea.Cmd {
	switch key {
	case "esc":
		r.gotoLevel(levelGame)
		return nil
	case "enter":
		return r.openRoom(strings.ToUpper(strings.TrimSpace(r.join.Value())))
	}
	var cmd tea.Cmd
	r.join, cmd = r.join.Update(k)
	r.joinErr = ""
	return cmd
}

// openRoom 建房（code 为空）或加入指定房间码。
func (r *Root) openRoom(code string) tea.Cmd {
	entry, ok := r.deps.Registry.Get(r.gameID)
	if !ok {
		r.joinErr = "没有这个游戏"
		return nil
	}

	var (
		c   client.Client
		err error
	)
	if r.deps.OpenRoom != nil {
		c, err = r.deps.OpenRoom(entry.ID, code)
	} else {
		conn := remote.New(r.deps.Backend, r.deps.Player.Token, entry.ID)
		err = conn.Join(code)
		c = conn
		r.conn = conn
	}
	r.roomClient = c
	if err != nil {
		r.joinErr = err.Error()
		return nil
	}
	r.game = r.newGameUI(entry, c)
	r.level = levelTable
	slog.Info("进入房间", "account", r.deps.Player.Account.ID, "game", entry.ID, "code", code)
	return r.game.Init()
}

func (r *Root) View() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.quit {
		return ""
	}
	if r.level == levelTable && r.game != nil {
		out := r.header() + "\n" + r.game.View()
		if r.confirmLeave {
			warn := r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
			if r.deps.Mode == sdkui.ColorNone {
				warn = r.deps.Renderer.NewStyle().Bold(true)
			}
			out += "\n" + warn.Render("[ASK] 离开这局？  [Y]退出对局  [N]继续打")
		}
		return out
	}

	body := r.menu.View()
	if r.level == levelJoin {
		body = r.joinView()
	}
	return r.header() + "\n" + body + "\n" + r.cmdLine()
}

// header 是 "TTYFish v0.1.9 | [USER] 名字 (—pt)·····Home" 那一行。
func (r *Root) header() string {
	version := r.deps.Version
	if version == "" {
		version = "dev"
	}
	balance := "—pt"
	if r.balancePT != "" {
		balance = r.balancePT + "pt"
	}
	left := fmt.Sprintf("TTYFish %s | [USER] %s (%s)", version, r.deps.Player.Account.Username, balance)
	right := r.level.title()

	dim := r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("244"))
	if r.deps.Mode == sdkui.ColorNone {
		dim = r.deps.Renderer.NewStyle()
	}
	hot := r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	if r.deps.Mode == sdkui.ColorNone {
		hot = r.deps.Renderer.NewStyle().Bold(true)
	}

	pad := r.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		return dim.Render(sdkui.CutWidth(left, r.width))
	}
	return dim.Render(left) + dim.Render(strings.Repeat("·", pad)) + hot.Render(right)
}

func (r *Root) joinView() string {
	lines := []string{
		"[房间号码]：" + r.join.View(),
	}
	msg := r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("244")).Render("[MSG] 请输入房间号")
	if r.joinErr != "" {
		msg = r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("203")).Render("[MSG] " + r.joinErr)
	}
	return strings.Join(lines, "\n") + "\n" + msg
}

func (r *Root) cmdLine() string {
	if r.level == levelSettings && r.settingsMessage != "" {
		return "[MSG] " + r.settingsMessage
	}
	hint := "[CMD] -=切换 Enter选择 Esc 返回"
	if r.level == levelHome {
		hint = "[CMD] -=切换 Enter选择 Esc 退出"
	}
	if r.level == levelJoin {
		hint = "[CMD] [1-6]输入 Enter进入 Esc返回"
	}
	dim := r.deps.Renderer.NewStyle().Foreground(lipgloss.Color("244"))
	if r.deps.Mode == sdkui.ColorNone {
		dim = r.deps.Renderer.NewStyle()
	}
	return dim.Render(hint)
}

// Close 释放订阅；不离开房间（座位要留给重连）。
func (r *Root) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	game, conn := r.game, r.conn
	r.mu.Unlock()

	if game != nil {
		game.Close()
	}
	if conn != nil {
		conn.Close()
	}
}

// WaitReady 只用来在测试里等界面就绪。
func (r *Root) WaitReady(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		ready := r.game != nil || r.level != levelTable
		r.mu.Unlock()
		if ready {
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
