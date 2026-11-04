// Package tui renders Exploding Kittens using only the player's filtered snapshot.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

type Options struct {
	Key       string
	Layout    ui.Layout
	LayoutOf  func(string) (ui.Layout, bool)
	SetLayout func(string, ui.Layout)
	Mode      ui.ColorMode
	ModeOf    func(string) (ui.ColorMode, bool)
	SetMode   func(string, ui.ColorMode)
}

type Model struct {
	c        client.Client
	snap     client.Snapshot
	sub      <-chan client.Snapshot
	input    textinput.Model
	renderer *lipgloss.Renderer
	opts     Options
	width    int
	height   int
	err      string
	closed   bool
	boss     bool
	help     bool
	handPage int
}

type closedMsg struct{}

func NewModel(c client.Client, opts Options, r *lipgloss.Renderer) *Model {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	if opts.LayoutOf != nil {
		if layout, ok := opts.LayoutOf(opts.Key); ok {
			opts.Layout = layout
		}
	}
	if opts.ModeOf != nil {
		if mode, ok := opts.ModeOf(opts.Key); ok {
			opts.Mode = mode
		}
	}
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 100
	input.Width = 70
	input.Focus()
	m := &Model{c: c, opts: opts, renderer: r, input: input, width: 80, height: 24}
	if c != nil {
		m.snap, m.sub = c.Snapshot(), c.Updates()
	}
	return m
}

func wait(ch <-chan client.Snapshot) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return closedMsg{}
		}
		return client.SnapshotMsg(snap)
	}
}

func (m *Model) Init() tea.Cmd { return tea.Batch(wait(m.sub), textinput.Blink) }

// The gateway owns room membership and closes its stream on disconnect.
func (m *Model) Close() {}

func (m *Model) HasModal() bool { return m.boss || m.input.Value() != "" }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case client.SnapshotMsg:
		m.snap = client.Snapshot(v)
		return m, wait(m.sub)
	case closedMsg:
		m.closed = true
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		m.input.Width = max(1, m.width-4)
		return m, nil
	case tea.KeyMsg:
		switch v.String() {
		case "pgdown":
			m.handPage++
			return m, nil
		case "pgup":
			m.handPage = max(0, m.handPage-1)
			return m, nil
		case "ctrl+b":
			m.boss = !m.boss
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.boss = false
			m.input.SetValue("")
			return m, nil
		case "enter":
			if m.boss {
				m.boss = false
			} else {
				m.err = ""
				if err := m.command(m.input.Value()); err != nil {
					m.err = err.Error()
				}
				m.input.SetValue("")
			}
			return m, nil
		}
	}
	if m.boss {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) command(text string) error {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	if m.c == nil || m.closed || m.snap.Kicked {
		return fmt.Errorf("房间连接不可用")
	}
	if len(fields) == 1 {
		switch fields[0] {
		case "start":
			return m.c.Start()
		case "bot":
			return m.c.AddBot()
		case "vote":
			return m.c.VoteRestart(true)
		case "help":
			m.help = !m.help
			return nil
		case "m":
			m.opts.Mode = m.opts.Mode.Next()
			if m.opts.SetMode != nil {
				m.opts.SetMode(m.opts.Key, m.opts.Mode)
			}
			return nil
		case "i":
			if m.opts.Layout == ui.LayoutCompact {
				m.opts.Layout = ui.LayoutRich
			} else {
				m.opts.Layout = ui.LayoutCompact
			}
			if m.opts.SetLayout != nil {
				m.opts.SetLayout(m.opts.Key, m.opts.Layout)
			}
			return nil
		}
	}
	o, ok := m.snap.View.(engine.Observation)
	if !ok || m.snap.Phase != client.Playing {
		return fmt.Errorf("输入 start 开局，bot 加电脑，vote 重开，i 切换显示")
	}
	a, err := parseCommand(fields, o)
	if err != nil {
		return err
	}
	return m.c.Act(a)
}

func parseCommand(fields []string, o engine.Observation) (engine.Action, error) {
	a := engine.Action{Seat: o.Seat, Target: -1}
	usage := fmt.Errorf("命令或参数错误，请按屏幕提示输入")
	if len(fields) == 0 {
		return a, usage
	}
	card := func(value string) (int, error) {
		i, err := strconv.Atoi(value)
		if err != nil || i < 1 || i > len(o.Hand) {
			return 0, fmt.Errorf("手牌序号无效")
		}
		return o.Hand[i-1].ID, nil
	}
	target := func(value string) (int, error) {
		seat, err := strconv.Atoi(value)
		if err != nil || seat < 1 || seat > len(o.Alive) {
			return -1, fmt.Errorf("玩家座位号无效")
		}
		return seat - 1, nil
	}
	var err error
	switch fields[0] {
	case "draw", "pass":
		if len(fields) != 1 {
			return a, usage
		}
		a.Type = engine.Draw
		if fields[0] == "pass" {
			a.Type = engine.Pass
		}
	case "play", "nope", "give", "defuse", "pair", "triple":
		cardCount := 1
		a.Type = engine.Play
		switch fields[0] {
		case "nope":
			a.Type = engine.PlayNope
		case "give":
			a.Type = engine.Give
		case "defuse":
			a.Type = engine.UseDefuse
		case "pair":
			cardCount = 2
		case "triple":
			cardCount = 3
		}
		expected := 1 + cardCount
		switch fields[0] {
		case "defuse", "pair":
			expected++
		case "triple":
			expected += 2
		case "play":
			// Only Favor needs an additional target for a single card.
			if len(fields) == 3 {
				expected++
			}
		}
		if len(fields) != expected {
			return a, usage
		}
		for _, value := range fields[1 : 1+cardCount] {
			id, e := card(value)
			if e != nil {
				return a, e
			}
			a.CardIDs = append(a.CardIDs, id)
		}
		switch fields[0] {
		case "play":
			if len(fields) == 3 {
				a.Target, err = target(fields[2])
			}
		case "pair", "triple":
			a.Target, err = target(fields[1+cardCount])
			if fields[0] == "triple" {
				a.Requested = engine.Kind(fields[len(fields)-1])
			}
		case "defuse":
			a.Position, err = strconv.Atoi(fields[2])
			if err == nil && (a.Position < 0 || a.Position > o.DeckCount) {
				err = fmt.Errorf("放回位置应为 0 到 %d", o.DeckCount)
			}
		}
	default:
		return a, usage
	}
	return a, err
}

func (m *Model) View() string {
	if m.boss {
		return ui.ClampFrame("$ go test ./...\nok   workspace (cached)", m.width, m.height)
	}
	if m.snap.Kicked {
		return "连接已被另一登录接管。"
	}
	if m.closed {
		return "房间连接已断开。"
	}
	title := m.renderer.NewStyle().Bold(true)
	if m.opts.Mode != ui.ColorNone {
		title = title.Foreground(lipgloss.Color("220"))
		if m.opts.Mode == ui.ColorBlock {
			title = title.Background(lipgloss.Color("235"))
		}
	}
	lines := []string{title.Render(fmt.Sprintf("Exploding Kittens  房间 %s", m.snap.Code))}
	o, playing := m.snap.View.(engine.Observation)
	var players []string
	for seat, p := range m.snap.Players {
		name := ui.CutWidth(p.Name, 12)
		status := ""
		if playing && seat < len(o.Alive) {
			if !o.Alive[seat] {
				status = " 淘汰"
			} else {
				status = fmt.Sprintf("(%d)", o.HandCounts[seat])
			}
		}
		if seat == m.snap.MySeat {
			name += "(你)"
		}
		players = append(players, fmt.Sprintf("%d:%s%s", seat+1, name, status))
	}
	lines = append(lines, ui.JoinWrapped(players, m.width))
	if playing && m.snap.Phase == client.Playing {
		lines = append(lines, fmt.Sprintf("回合 P%d · 剩余 %d 回合 · 牌堆 %d · %s",
			o.Turn+1, o.TurnsRemaining, o.DeckCount, o.Phase))
		if m.snap.MySeat >= 0 && m.snap.MySeat < len(o.Alive) && !o.Alive[m.snap.MySeat] {
			lines = append(lines, "你已淘汰，可观战或 Esc 离开。")
		} else {
			var hand []string
			for i, card := range o.Hand {
				hand = append(hand, fmt.Sprintf("%d.%s", i+1, card.Kind))
			}
			rows := strings.Split(ui.JoinWrapped(hand, m.width), "\n")
			pageSize := max(1, m.height-16)
			pages := max(1, (len(rows)+pageSize-1)/pageSize)
			page := min(m.handPage, pages-1)
			start := page * pageSize
			lines = append(lines, rows[start:min(len(rows), start+pageSize)]...)
			if pages > 1 {
				lines = append(lines, fmt.Sprintf("手牌 %d/%d 页 · PgUp/PgDn 翻页", page+1, pages))
			}
		}
		if len(o.Future) > 0 {
			var future []string
			for _, card := range o.Future {
				future = append(future, string(card.Kind))
			}
			lines = append(lines, "仅你可见，牌顶起: "+strings.Join(future, " / "))
		}
		switch o.Phase {
		case engine.Responding:
			if o.Pending != nil {
				a := o.Pending.Action
				desc := fmt.Sprintf("P%d: %s", a.Seat+1, o.Pending.Kind)
				if len(a.CardIDs) > 1 {
					desc += fmt.Sprintf(" · %d张组合", len(a.CardIDs))
				}
				if len(a.CardIDs) > 1 || o.Pending.Kind == engine.Favor {
					desc += fmt.Sprintf(" → P%d", a.Target+1)
				}
				if a.Requested != "" {
					desc += " 索要 " + string(a.Requested)
				}
				lines = append(lines, desc, fmt.Sprintf("Nope %d次 · 每次反制后有5秒响应窗口", o.Pending.Nopes))
			}
			lines = append(lines, "nope 手牌号  /  pass 放弃反制（回车确认）")
		case engine.Giving:
			lines = append(lines, fmt.Sprintf("P%d 选择交牌: give 手牌号", o.Pending.Action.Target+1))
		case engine.Reinserting:
			lines = append(lines, fmt.Sprintf("P%d 拆弹: defuse 手牌号 位置(0..%d，0为牌顶)", o.Turn+1, o.DeckCount))
		default:
			lines = append(lines, "draw 摸牌结束 / play 手牌号 [目标座位]")
			if m.opts.Layout != ui.LayoutCompact || m.help {
				lines = append(lines, "pair 牌号 牌号 目标 / triple 牌号 牌号 牌号 目标 英文牌名")
			}
		}
	} else {
		if playing && o.Winner >= 0 {
			lines = append(lines, fmt.Sprintf("P%d 获胜！本游戏暂不发放 PT。", o.Winner+1))
		}
		lines = append(lines, "start 开始 / bot 加电脑 / vote 投票重开")
	}
	if m.snap.Notice != "" {
		lines = append(lines, m.snap.Notice)
	}
	if len(m.snap.Timeline) > 0 && (m.opts.Layout != ui.LayoutCompact || m.help) {
		for _, event := range m.snap.Timeline[max(0, len(m.snap.Timeline)-3):] {
			lines = append(lines, fmt.Sprintf("P%d %s", event.Seat+1, event.Text))
		}
	}
	if m.err != "" {
		lines = append(lines, "! "+m.err)
	}
	lines = append(lines, "help 帮助 · i 显示 · m 颜色 · Ctrl+B 老板键 · Esc 退房")
	// Keep the command field visible even when the hand occupies many lines.
	body := ui.ClampFrame(strings.Join(lines, "\n"), m.width, max(1, m.height-1))
	return ui.ClampFrame(body+"\n"+m.input.View(), m.width, m.height)
}

var _ tea.Model = (*Model)(nil)
