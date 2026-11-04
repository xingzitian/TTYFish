package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
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

	// 界面密度：丰富（默认）还是极简。和颜色深浅一样按账号记住。
	Layout    ui.Layout
	LayoutOf  func(key string) (ui.Layout, bool)
	SetLayout func(key string, l ui.Layout)
}

type snapMsg = client.SnapshotMsg
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

	chat bool
	boss bool
	// errAt 是最后一条错误的时间。极简模式的 CMD 行只在 2 秒内显示错误，
	// 否则玩家会以为"只剩下这一句"，不知道还能按什么。
	errAt    time.Time
	choosing bool
	pending  engine.Card
	mode     ui.ColorMode
	// help 为 true 时显示键位一览（? 打开）
	help   bool
	width  int
	height int
	layout layoutKind

	// 极简模式
	minimal bool
	// needLayoutChoice 表示这个账号还没选过界面密度，先问一次。
	// 只在服务端提供了偏好读写（LayoutOf != nil）时才可能为 true。
	needLayoutChoice bool
	page             int
	playersUntil     time.Time
	errMsg           string
	turnFrame        int
	turnFlowTicks    int
	turnTickPending  bool
	turnOffset       int
	// tape 是 TURN 行那条"座位队列"的缓存：回合一变就重建一次，
	// 之后每帧只做字符串切片。以前每帧都重新居中+补齐，名字看着会抖。
	tape     string
	tapeKey  string
	tapeSlot int
	// slideSeats 是"这次换人要滑几个座位"。列数要等渲染时知道槽宽才能算，
	// 所以这里先记座位数，turnMarquee 第一帧再换算成 turnOffset。
	slideSeats int
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
	m.minimal = opts.Layout == ui.LayoutCompact
	if opts.LayoutOf != nil {
		if l, ok := opts.LayoutOf(id.Key); ok {
			m.minimal = l == ui.LayoutCompact
		} else {
			// 第一次登录：问一次，选完记住
			m.needLayoutChoice = true
		}
	}

	in := textinput.New()
	in.Prompt = ""
	// 不要放一个看起来像真房间码的占位符——真有人照着敲，
	// 然后收到"房间不存在"，还以为是服务端坏了。
	in.Placeholder = ""
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
	return tea.Batch(m.initRoom(), m.animateTurn())
}

func (m *Model) initRoom() tea.Cmd {
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
	model, cmd := m.update(msg)
	return model, tea.Batch(cmd, m.animateTurn())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case turnTickMsg:
		if v.model == m && m.turnTickPending {
			m.turnTickPending = false
			if m.turnAnimationVisible() {
				m.turnFlowTicks++
				if m.turnFlowTicks == 12 { // 16ms × 12 ≈ 192ms，和原来的 200ms 一致

					m.turnFrame = (m.turnFrame + 1) % 3
					m.turnFlowTicks = 0
				}
				// 一帧走 1 列，靠"帧多"来顺：12 列 = 12 帧 × 30ms ≈ 360ms。
				// 之前一帧 3 列是把帧数压少了——不抖是因为缓存了 tape，
				// 但 80ms 滑完实在太快，眼睛跟不上。
				switch {
				case m.turnOffset > 1:
					m.turnOffset--
				case m.turnOffset < -1:
					m.turnOffset++
				default:
					m.turnOffset = 0
				}
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		if v.Width > 0 {
			m.width = v.Width
		}
		if v.Height > 0 {
			m.height = v.Height
		}
		return m, nil

	case snapMsg:
		previous := m.snap
		m.snap = client.Snapshot(v)
		m.updateTurnOffset(previous)
		if m.snap.Kicked {
			m.kicked = true
		}
		if m.closed {
			return m, nil
		}
		return m, waitFor(m.sub)

	case hidePlayersMsg:
		m.playersUntil = time.Time{}
		return m, nil

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

	// 键位一览：只认关闭键，免得看着帮助时误触出牌。
	// ? 和 / 都能开关——有人习惯按 /（斜杠键位置更顺手）。
	if m.help {
		switch key {
		case "?", "/", "esc":
			m.help = false
		case "q":
			// 老板键在帮助界面里照样有效，不能因为看着帮助就失联
			return m.quit()
		}
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
					m.fail(err.Error())
				} else {
					m.fail("")
				}
			}
			m.chat = false
		}
		return nil
	}

	// 首次登录的界面选择：只认 1 / 2，别的键一律忽略，免得误触进了牌桌
	if m.needLayoutChoice {
		switch key {
		case "1":
			m.applyLayout(ui.LayoutRich)
		case "2":
			m.applyLayout(ui.LayoutCompact)
		}
		return nil
	}

	// 极简模式：数字直接出牌、左右翻页，其余交给共用表。
	// 放在选色之后，因为选色/聊天是两种模式共用的模态状态。
	if m.minimal && !m.choosing && m.snap.Phase == client.Playing {
		return m.handleCompactKey(key)
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
	case "?", "/":
		m.help = true
		return nil
	case "q":
		return m.quit()
	case "t":
		m.chat = true
		return nil
	case "i":
		// 切换丰富 / 极简，和颜色深浅一样记住
		m.toggleLayout()
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
				m.fail(err.Error())
			} else {
				m.fail("")
			}
		}
		return nil
	}

	// 大厅 / 结算界面：可以离开，房主可以补电脑、开局。
	if m.snap.Phase != client.Playing {
		switch key {
		case "l":
			m.leave()
		case "s":
			// 非房主按了要有回应。以前这里直接 return，屏幕上什么都没有，
			// 成员会以为"这界面的按键全坏了"。
			if !m.snap.IsHost {
				m.fail("只有房主能开局，等房主按 s")
				return nil
			}
			if m.c != nil {
				if err := m.c.Start(); err != nil {
					m.fail(err.Error())
				} else {
					m.fail("")
				}
			}
		case "b", "c":
			if !m.snap.IsHost {
				m.fail("只有房主能加电脑")
				return nil
			}
			if m.c != nil {
				if err := m.c.AddBot(); err != nil {
					m.fail(err.Error())
				} else {
					m.fail("")
				}
			}
		}
		return nil
	}

	obs := m.obs()

	// +4 质疑是模态的：轮到你决定时，其他牌桌按键都不该生效。
	if obs.Challenge != nil && obs.Challenge.By == m.snap.MySeat {
		switch key {
		case "n", "c":
			m.act(engine.ResolveChallenge{Seat: m.snap.MySeat, Challenge: true})
		case "y", "enter", " ":
			m.act(engine.ResolveChallenge{Seat: m.snap.MySeat, Challenge: false})
		}
		return nil
	}

	// 翻页不属于"出牌动作"：轮不到我也应该能翻看自己的牌。
	switch key {
	case "-", "_", "left":
		m.prevPage()
		return nil
	case "=", "+", "right":
		m.nextPage()
		return nil
	}

	if obs.Turn != m.snap.MySeat || obs.Phase != engine.Playing {
		return nil
	}

	switch key {
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
			m.fail("现在没有人漏喊 UNO")
		}
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			m.playIndex(int(key[0] - '1'))
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

// playIndex 出当前页的第 i 张牌（0 起）。
//
// 两种模式都走这里：数字键直接出牌，不再有"移动光标再回车"那一套。
func (m *Model) playIndex(i int) {
	// 分页大小按当前模式走：极简一页 5 张、丰富一页 9 张
	var page []engine.Card
	if m.minimal {
		page, _ = m.compactPage()
	} else {
		page, _ = m.handPage()
	}
	if i < 0 || i >= len(page) {
		return
	}
	c := page[i]

	if c.Kind == engine.Wild || c.Kind == engine.WildDrawFour {
		m.choosing = true
		m.pending = c
		m.fail("")
		return
	}
	if !m.canPlay(c.ID) {
		m.fail("这张牌现在出不了：颜色和数字都对不上")
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

// fail 记一条错误，并打上时间戳（极简模式的 CMD 行只显示 2 秒）。
func (m *Model) fail(msg string) {
	m.errMsg = msg
	m.errAt = time.Now()
}

func (m *Model) act(a engine.Action) {
	if m.c == nil {
		return
	}
	if err := m.c.Act(a); err != nil {
		m.fail(err.Error())
		return
	}
	m.fail("")
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
	m.fail("")
}

// Close 取消订阅并通知房间"我断线了"。可以重复调用。
// HasModal 告诉网关：现在有没有弹窗占着键盘。
//
// 有的话 Esc 应该先关弹窗（选色、聊天、质疑），而不是被当成"离开房间"。
func (m *Model) HasModal() bool {
	return m.choosing || m.chat || m.entering || m.boss || m.help
}

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
