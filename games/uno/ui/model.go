package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// Identity 是玩家身份。Key 是服务端算出来的稳定标识
// （公钥指纹 + SSH 用户名），昵称只是展示用。
type Identity struct {
	Key  string
	Name string
}

// Options 是启动界面需要的外部依赖。
type Options struct {
	Rules  engine.Rules
	Bots   int
	BotLag time.Duration
	Mode   ui.ColorMode

	// 个人偏好的读写。渲染虽然发生在服务端，但"用哪种牌面"是玩家自己的选择，
	// 换个连接不该被重置回服务端默认值。
	ModeOf  func(key string) (ui.ColorMode, bool)
	SetMode func(key string, m ui.ColorMode)
}

type snapMsg client.Snapshot
type closedMsg struct{}

// Model 是纯客户端：它不持有牌局状态，只渲染服务端推来的快照，
// 并且只发送意图。所有合法性判断都在房间里做。
type Model struct {
	id       Identity
	opts     Options
	renderer *lipgloss.Renderer

	c      client.Client
	sub    <-chan client.Snapshot
	snap   client.Snapshot
	closed bool
	kicked bool

	// quitting 是"我自己按了 q"，用来和"被动断开"区分开：
	// 自己退出时不该在最后一帧画一句吓人的"连接已断开"。
	quitting bool

	entering bool
	// 房间码输入用 bubbles/textinput，而不是手写一个 6 字符缓冲。
	// 手写的那个只认一种退格编码，方向键、粘贴、Ctrl+U/W 全都不支持——
	// 不同终端发的删字符键有好几种（backspace / ctrl+h / delete），
	// 交给上游组件处理才不会出现"输入框里退格键没反应"。
	codeInput textinput.Model
	enterErr  string

	chat     bool
	boss     bool
	choosing bool
	pending  engine.Card
	mode     ui.ColorMode
	cursor   int
	width    int
	height   int
	layout   layoutKind
	errMsg   string
}

// NewModel 创建界面模型。
//
// 房间连接由平台负责：传进来的 Client 可能已经在一个房间里了（断线重连），
// 那种情况第一帧快照就带房间码，界面直接进牌桌；否则停在输码界面。
func NewModel(id Identity, c client.Client, opts Options, r *lipgloss.Renderer) *Model {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	m := &Model{id: id, opts: opts, renderer: r, width: 80, height: 24, mode: opts.Mode, c: c}
	// 上次自己选过的样式优先于服务端默认值。
	if opts.ModeOf != nil {
		if f, ok := opts.ModeOf(id.Key); ok {
			m.mode = f
		}
	}

	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "ABC234"
	in.CharLimit = 6
	in.Width = 8
	in.Focus()
	m.codeInput = in

	if c != nil {
		m.snap = c.Snapshot()
		m.sub = c.Updates()
	}
	m.entering = m.snap.Code == ""
	return m
}

// SetSize 设置可用宽高（测试用）。
func (m *Model) SetSize(w, h int) {
	if w > 0 {
		m.width = w
	}
	if h > 0 {
		m.height = h
	}
}

func (m *Model) Init() tea.Cmd {
	if m.sub != nil {
		return waitFor(m.sub)
	}
	if m.entering {
		return textinput.Blink
	}
	return nil
}

func waitFor(ch <-chan client.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return closedMsg{}
		}
		return snapMsg(s)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		if v.Width > 0 {
			m.width = v.Width
		}
		if v.Height > 0 {
			m.height = v.Height
		}
		return m, nil

	case snapMsg:
		m.snap = client.Snapshot(v)
		if m.snap.Kicked {
			m.kicked = true
		}
		if m.closed {
			return m, nil
		}
		if m.cursor >= len(m.obs().Hand) {
			m.cursor = 0
		}
		return m, waitFor(m.sub)

	case closedMsg:
		m.closed = true
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()

	// 老板键：Ctrl+B 立刻把整屏换成一段像样的 shell 输出，回车或 Esc 回来。
	// 放在最前面，任何状态下都能用。
	if key == "ctrl+b" {
		m.boss = !m.boss
		return nil
	}
	if m.boss {
		if key == "enter" || key == "esc" {
			m.boss = false
		}
		return nil
	}

	if key == "ctrl+c" {
		return m.quit()
	}

	if m.entering {
		return m.handleEnterKey(k)
	}

	if m.chat {
		switch {
		case key == "esc":
			m.chat = false
		case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
			idx := int(key[0] - '1')
			if idx < len(client.Presets) && m.c != nil {
				if err := m.c.Chat(idx); err != nil {
					m.errMsg = err.Error()
				} else {
					m.errMsg = ""
				}
			}
			m.chat = false
		}
		return nil
	}

	// 变色牌选色是模态的，优先级最高。
	if m.choosing {
		switch key {
		case "r", "1":
			m.playWild(engine.Red)
		case "y", "2":
			m.playWild(engine.Yellow)
		case "b", "3":
			m.playWild(engine.Blue)
		case "g", "4":
			m.playWild(engine.Green)
		case "esc":
			m.choosing = false
			m.pending = engine.Card{}
		}
		return nil
	}

	switch key {
	case "q":
		return m.quit()
	case "t":
		m.chat = true
		return nil
	case "m":
		// 换全局颜色深浅：满色 / 只给文字上色 / 无色。记住这个选择，
		// 下次连上来还是你自己挑的那个。
		m.mode = m.mode.Next()
		if m.opts.SetMode != nil {
			m.opts.SetMode(m.id.Key, m.mode)
		}
		return nil
	case "v":
		if m.c != nil {
			if err := m.c.VoteRestart(true); err != nil {
				m.errMsg = err.Error()
			} else {
				m.errMsg = ""
			}
		}
		return nil
	}

	// 大厅 / 结算界面：可以离开，房主可以补电脑、开局。
	if m.snap.Phase != client.Playing {
		if key == "l" {
			m.leave()
			return nil
		}
		if m.snap.IsHost && m.c != nil {
			switch key {
			case "b":
				if err := m.c.AddBot(); err != nil {
					m.errMsg = err.Error()
				} else {
					m.errMsg = ""
				}
			case "s":
				if err := m.c.Start(); err != nil {
					m.errMsg = err.Error()
				} else {
					m.errMsg = ""
				}
			}
		}
		return nil
	}

	obs := m.obs()

	// +4 质疑是模态的：轮到你决定时，其他牌桌按键都不该生效。
	if obs.Challenge != nil && obs.Challenge.By == m.snap.MySeat {
		switch key {
		case "c":
			m.act(engine.ResolveChallenge{Seat: m.snap.MySeat, Challenge: true})
		case "enter", " ", "d":
			m.act(engine.ResolveChallenge{Seat: m.snap.MySeat, Challenge: false})
		}
		return nil
	}

	if obs.Turn != m.snap.MySeat || obs.Phase != engine.Playing {
		return nil
	}

	switch key {
	case "left", "h":
		if n := len(obs.Hand); n > 0 {
			m.cursor = (m.cursor - 1 + n) % n
		}
	case "right", "l":
		if n := len(obs.Hand); n > 0 {
			m.cursor = (m.cursor + 1) % n
		}
	case "enter", " ":
		m.playSelected()
	case "d":
		m.act(engine.DrawCard{Seat: m.snap.MySeat})
	case "p":
		m.act(engine.Pass{Seat: m.snap.MySeat})
	case "u":
		m.act(engine.CallUno{Seat: m.snap.MySeat})
	case "c":
		if obs.UnoPending >= 0 {
			m.act(engine.CatchUno{Seat: m.snap.MySeat, Target: obs.UnoPending})
		} else {
			m.errMsg = "现在没有人漏喊 UNO"
		}
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			idx := int(key[0] - '0')
			if idx == 0 {
				idx = 10
			}
			if idx--; idx >= 0 && idx < len(obs.Hand) {
				m.cursor = idx
			}
		}
	}
	return nil
}

func (m *Model) handleEnterKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		return m.quit()
	case "enter":
		return m.join(client.NormalizeCode(m.codeInput.Value()))
	}

	// 其余按键原样交给 textinput：退格、Ctrl+H、Delete、Ctrl+U/W、
	// 左右移动、Home/End、粘贴都由它处理，不用自己再列一遍。
	var cmd tea.Cmd
	m.codeInput, cmd = m.codeInput.Update(k)
	m.enterErr = ""
	return cmd
}

func (m *Model) join(code string) tea.Cmd {
	if m.c == nil {
		m.enterErr = "没有可用的房间服务"
		return nil
	}
	if err := m.c.Join(code); err != nil {
		// 保留输入内容，方便直接退格改；不要一错就清空。
		m.enterErr = err.Error()
		return nil
	}
	// 订阅之后第一帧已经在 Client 里了，取出来立刻能渲染。
	m.snap = m.c.Snapshot()
	m.sub = m.c.Updates()
	m.entering = m.snap.Code == ""
	m.enterErr = ""
	if m.sub == nil {
		return nil
	}
	return waitFor(m.sub)
}

func (m *Model) playSelected() {
	obs := m.obs()
	if len(obs.Hand) == 0 {
		return
	}
	if m.cursor >= len(obs.Hand) {
		m.cursor = len(obs.Hand) - 1
	}
	c := obs.Hand[m.cursor]

	if c.Kind == engine.Wild || c.Kind == engine.WildDrawFour {
		m.choosing = true
		m.pending = c
		m.errMsg = ""
		return
	}
	if !m.canPlay(c.ID) {
		m.errMsg = "这张牌现在出不了：颜色和数字都对不上"
		return
	}
	m.act(engine.PlayCard{
		Seat:   m.snap.MySeat,
		CardID: c.ID,
	})
}

func (m *Model) playWild(c engine.Color) {
	m.choosing = false
	card := m.pending
	m.pending = engine.Card{}
	m.act(engine.PlayCard{
		Seat:   m.snap.MySeat,
		CardID: card.ID,
		Color:  c,
	})
}

// canPlay 用服务端给的合法动作列表判断，避免在客户端重复实现一套规则。
func (m *Model) canPlay(cardID int) bool {
	for _, a := range m.obs().Legal {
		if pc, ok := a.(engine.PlayCard); ok && pc.CardID == cardID {
			return true
		}
	}
	return false
}

func (m *Model) act(a engine.Action) {
	if m.c == nil {
		return
	}
	if err := m.c.Act(a); err != nil {
		m.errMsg = err.Error()
		return
	}
	m.errMsg = ""
	m.cursor = 0
}

func (m *Model) quit() tea.Cmd {
	m.quitting = true
	m.Close()
	return tea.Quit
}

// leave 在大厅里主动退房，回到输入房间码的界面。
func (m *Model) leave() {
	if m.c != nil {
		_ = m.c.Leave()
	}
	m.sub = nil
	m.snap = client.Snapshot{}
	m.codeInput.SetValue("")
	m.codeInput.Focus()
	m.entering = true
	m.errMsg = ""
}

// Close 取消订阅并通知房间"我断线了"。可以重复调用。
func (m *Model) Close() {
	if m.c != nil {
		_ = m.c.Leave()
	}
}

func (m *Model) seat() int { return m.snap.MySeat }

// obs 取出快照里的 UNO 视图。
//
// 平台把游戏视图当成不透明值传过来（Snapshot.View any），这里是 UNO 自己的
// UI，所以知道它其实是 engine.Observation。换成别的游戏时，这一层换成那个
// 游戏自己的类型就行了，平台不用动。
func (m *Model) obs() engine.Observation {
	v, _ := m.snap.View.(engine.Observation)
	return v
}

func (m *Model) name(seat int) string {
	if seat == m.snap.MySeat {
		return "你"
	}
	if seat >= 0 && seat < len(m.snap.Players) {
		return m.snap.Players[seat].Name
	}
	return fmt.Sprintf("P%d", seat+1)
}
