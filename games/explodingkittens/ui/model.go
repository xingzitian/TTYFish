// Package tui renders Exploding Kittens using only the player's filtered snapshot.
//
// 交互原则：**一个动作一次按键**，没有输入框、没有命令语法。
// 屏幕上的 [CMD] 行永远写着"现在能按什么"，按阶段切换。
package tui

import (
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"

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

// step 是"玩家正在选什么"。牌局阶段由快照决定，step 只描述进行中的选择。
type step int

const (
	stepIdle         step = iota
	stepTarget            // 选一个目标玩家（对子盲抽 / 索要）
	stepDefusePos         // 埋雷：选放回位置（预设键）
	stepDefuseCustom      // 埋雷：手动指定位置（-= 调，Enter 确认）
	stepChat
)

const (
	handPageSize   = 5
	tickInterval   = 200 * time.Millisecond
	errTTL         = 2 * time.Second
	playerPanelTTL = 5 * time.Second
	revealTTL      = 10 * time.Second
)

type Model struct {
	c        client.Client
	snap     client.Snapshot
	sub      <-chan client.Snapshot
	renderer *lipgloss.Renderer
	opts     Options
	width    int
	height   int

	errMsg string
	errAt  time.Time
	closed bool
	boss   bool
	help   bool

	page     int
	step     step
	target   int
	pairDraw bool        // 本次选目标是为了"对子盲抽"还是"索要"
	pairK    engine.Kind // 要打出的那一对猫牌种类
	pos      int         // 埋雷位置

	deadline     time.Time // 响应窗口截止
	playersAt    time.Time // Tab 玩家列表
	revealAt     time.Time // 预言结果展示截止
	revealed     []engine.Card
	revealBy     int // 谁打出的预言，用来给其他人显示"正在偷看"
	tickPending  bool
	responseSent string
	passSent     string
	turnOffset   int
	slideSeats   int
	targetPage   int
}

type closedMsg struct{}
type tickMsg struct{ model *Model }

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
	m := &Model{c: c, opts: opts, renderer: r, width: 80, height: 24, target: -1}
	if c != nil {
		m.snap, m.sub = c.Snapshot(), c.Updates()
		m.anchorTimers()
		if o := m.obs(); m.inGame() && o.Phase == engine.Playing && len(o.Future) > 0 {
			m.revealed = slices.Clone(o.Future)
			m.revealBy = o.Seat
			m.revealAt = time.Now().Add(revealTTL)
		} else if m.inGame() && o.Phase == engine.Playing && o.PeekSeq > 0 && o.PeekSeat >= 0 {
			m.revealBy = o.PeekSeat
			m.revealAt = time.Now().Add(revealTTL)
		}
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

func (m *Model) Init() tea.Cmd { return tea.Batch(wait(m.sub), m.tick()) }

// A single timer owns redraws; snapshots must not start parallel tick chains.
func (m *Model) tick() tea.Cmd {
	if m.tickPending || m.boss || m.help || m.closed || m.snap.Kicked {
		return nil
	}
	interval := tickInterval
	if m.turnOffset != 0 || m.slideSeats != 0 {
		interval = 16 * time.Millisecond
	} else if m.deadline.IsZero() && m.revealAt.IsZero() &&
		m.playersAt.IsZero() && !m.errVisible() {
		return nil
	}
	m.tickPending = true
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{model: m} })
}

func (m *Model) Close() {}

// HasModal 告诉网关：现在有选择占着键盘，Esc 先归游戏处理。
func (m *Model) HasModal() bool { return m.boss || m.help || m.showPlayers() || m.step != stepIdle }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	return model, tea.Batch(cmd, m.tick())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case client.SnapshotMsg:
		m.applySnapshot(client.Snapshot(v))
		return m, wait(m.sub)
	case closedMsg:
		m.closed = true
		return m, nil
	case tickMsg:
		if v.model != m || !m.tickPending {
			return m, nil
		}
		m.tickPending = false
		m.expireTransient()
		if m.boss || m.help || m.closed || m.snap.Kicked {
			m.turnOffset, m.slideSeats = 0, 0
		} else if m.turnOffset > 0 {
			m.turnOffset--
		} else if m.turnOffset < 0 {
			m.turnOffset++
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(v)
	}
	return m, nil
}

// applySnapshot 收敛界面状态：阶段变了就丢掉未完成的选择；
// 预言结果第一次出现时开始 10 秒展示。
func (m *Model) applySnapshot(next client.Snapshot) {
	previous := m.snap
	prev := m.obs()
	m.snap = next
	o := m.obs()
	if o.Phase != prev.Phase || o.Turn != prev.Turn || next.Code != previous.Code ||
		next.Phase != client.Playing {
		m.reset()
	}
	if responseKey(o) != responseKey(prev) || next.Code != previous.Code {
		m.responseSent = ""
		m.passSent = ""
	}
	m.updateTurnOffset(previous)
	// A peek is presentation, not an extra turn. Any subsequent action that
	// changes the deck/actor/phase invalidates it, including a cancelled peek.
	if o.Turn != prev.Turn || o.DeckCount != prev.DeckCount ||
		o.Phase != engine.Playing || next.Phase != client.Playing ||
		next.Code != previous.Code || !slices.Equal(o.Future, prev.Future) ||
		o.PeekSeq != prev.PeekSeq || o.PeekSeat != prev.PeekSeat {
		m.clearReveal()
	}
	peekResolved := o.PeekSeq > prev.PeekSeq && o.PeekSeat >= 0 &&
		o.PeekSeat == o.Turn && o.Phase == engine.Playing && next.Phase == client.Playing
	if next.Phase == client.Playing && o.Phase == engine.Playing &&
		len(o.Future) > 0 && (!slices.Equal(o.Future, prev.Future) ||
		(peekResolved && o.PeekSeat == o.Seat)) {
		m.revealed = append([]engine.Card(nil), o.Future...)
		m.revealBy = m.snap.MySeat
		m.revealAt = time.Now().Add(revealTTL)
	}
	// 别人打的预言：我看不到内容，但要知道有人在看牌堆
	if peekResolved &&
		o.PeekSeat != m.snap.MySeat && m.revealAt.IsZero() {
		m.revealBy = o.PeekSeat
		m.revealAt = time.Now().Add(revealTTL)
	}
	m.anchorTimers()
}

func (m *Model) clearReveal() {
	m.revealAt = time.Time{}
	m.revealed = nil
	m.revealBy = -1
}

func (m *Model) expireTransient() {
	now := time.Now()
	if !m.revealAt.IsZero() && !now.Before(m.revealAt) {
		m.clearReveal()
	}
	if !m.deadline.IsZero() && !now.Before(m.deadline) {
		m.deadline = time.Time{}
	}
	if !m.playersAt.IsZero() && !now.Before(m.playersAt) {
		m.playersAt = time.Time{}
	}
}

func (m *Model) anchorTimers() {
	if m.snap.PendingLeftMS > 0 {
		m.deadline = time.Now().Add(time.Duration(m.snap.PendingLeftMS) * time.Millisecond)
	} else {
		m.deadline = time.Time{}
	}
}

func (m *Model) reset() {
	m.step = stepIdle
	m.target = -1
	m.pairDraw = false
	m.pairK = ""
	m.pos = 0
	m.targetPage = 0
}

func (m *Model) obs() engine.Observation {
	o, _ := m.snap.View.(engine.Observation)
	return o
}

func (m *Model) inGame() bool { return m.snap.Phase == client.Playing }

func (m *Model) myTurn() bool {
	o := m.obs()
	return m.inGame() && o.Turn == m.snap.MySeat && o.Phase == engine.Playing
}

func (m *Model) alive() bool {
	o := m.obs()
	return m.snap.MySeat >= 0 && m.snap.MySeat < len(o.Alive) && o.Alive[m.snap.MySeat]
}

func (m *Model) showPlayers() bool { return time.Now().Before(m.playersAt) }

func (m *Model) fail(msg string) {
	m.errMsg = msg
	m.errAt = time.Now()
}

func (m *Model) errVisible() bool { return m.errMsg != "" && time.Since(m.errAt) < errTTL }
func (m *Model) say()             { m.errMsg = "" }

func (m *Model) act(a engine.Action) bool {
	if m.c == nil || m.closed || m.snap.Kicked {
		m.fail("连接不可用")
		return false
	}
	if err := m.c.Act(a); err != nil {
		m.fail(err.Error())
		return false
	}
	m.say()
	return true
}

// pageCards 返回当前页的手牌（已排序）和总页数。
func (m *Model) pageCards() ([]engine.Card, int) {
	hand := sortHand(m.obs().Hand)
	pages := (len(hand) + handPageSize - 1) / handPageSize
	if pages < 1 {
		pages = 1
	}
	if m.page >= pages {
		m.page = pages - 1
	}
	if m.page < 0 {
		m.page = 0
	}
	start := m.page * handPageSize
	end := start + handPageSize
	if start > len(hand) {
		start = len(hand)
	}
	if end > len(hand) {
		end = len(hand)
	}
	return hand[start:end], pages
}

func (m *Model) nopeCount() int {
	n := 0
	for _, c := range m.obs().Hand {
		if c.Kind == engine.Nope {
			n++
		}
	}
	return n
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := strings.ToLower(k.String())

	if action, _ := ui.ResolveAction([]ui.Action{ui.BossAction()}, key); action == "boss" {
		m.boss = !m.boss
		return nil
	}
	if m.boss {
		if action, _ := ui.ResolveAction([]ui.Action{ui.BossExitAction(false)}, key); action == "cancel" {
			m.boss = false
		}
		return nil
	}
	if m.help {
		action, _ := ui.ResolveAction(ui.HelpActions(commonActions, "help", "quit"), key)
		switch action {
		case "close-help", "cancel":
			m.help = false
		case "quit":
			return tea.Quit
		}
		return nil
	}
	if action, _ := ui.ResolveAction([]ui.Action{ui.InterruptAction()}, key); action == "interrupt" {
		return tea.Quit
	}
	cancel, _ := ui.ResolveAction([]ui.Action{ui.EscapeAction("")}, key)
	if m.showPlayers() && cancel == "cancel" {
		m.playersAt = time.Time{}
		return nil
	}
	reinsertAction, _ := ui.ResolveAction(reinsertActions, key)
	if reinsertAction == "i" && m.inGame() && m.alive() &&
		m.obs().Phase == engine.Reinserting && m.obs().Turn == m.snap.MySeat {
		m.step = stepDefuseCustom
		m.pos = 0
		m.say()
		return nil
	}
	if m.step == stepDefuseCustom {
		action, _ := ui.ResolveAction(customPositionActions, key)
		switch action {
		case "previous":
			m.advancePage(-1)
			return nil
		case "next":
			m.advancePage(1)
			return nil
		case "confirm":
			m.useDefuse(m.obs(), m.pos)
			return nil
		case "cancel":
			m.step = stepDefusePos
			return nil
		}
	}
	commonAction, _ := ui.ResolveAction(commonActions, key)
	switch commonAction {
	case "help":
		m.help = true
		return nil
	case "layout":
		m.toggleLayout()
		return nil
	case "color":
		m.opts.Mode = m.opts.Mode.Next()
		if m.opts.SetMode != nil {
			m.opts.SetMode(m.opts.Key, m.opts.Mode)
		}
		return nil
	case "players":
		if m.showPlayers() {
			m.playersAt = time.Time{}
		} else {
			m.playersAt = time.Now().Add(playerPanelTTL)
		}
		return nil
	case "next":
		m.advancePage(1)
		return nil
	case "previous":
		m.advancePage(-1)
		return nil
	case "cancel":
		if m.step != stepIdle {
			m.reset()
			m.say()
		}
		return nil
	case "quit":
		// 老板键：任何时候都是立刻断开
		return tea.Quit
	}

	o := m.obs()
	if m.step == stepChat {
		id, ok := ui.ResolveAction(chatActions(), key)
		if idx, err := strconv.Atoi(id); ok && err == nil && m.c != nil {
			if err := m.c.Chat(idx); err != nil {
				m.fail(err.Error())
			} else {
				m.say()
				m.reset()
			}
		}
		return nil
	}
	if commonAction == "chat" && (!m.inGame() || m.alive()) {
		m.step = stepChat
		return nil
	}
	if !m.inGame() {
		m.lobbyKey(key)
		return nil
	}
	if !m.alive() {
		return nil
	}
	current, _ := m.commandActions()
	reveal, _ := ui.ResolveAction(current, key)
	if reveal == revealAction.ID && o.Phase == engine.Playing && len(m.revealed) > 0 {
		m.clearReveal()
		return nil
	}
	switch o.Phase {
	case engine.Responding:
		return m.handleResponding(key)
	case engine.Giving:
		return m.handleGiving(key)
	case engine.Reinserting:
		return m.handleReinserting(key)
	}

	if m.step == stepTarget {
		return m.handleTarget(key)
	}
	if !m.myTurn() {
		return nil
	}
	return m.handlePlaying(key)
}

// advancePage 处理 -=：埋雷自定义位置时是调数值，其余是翻手牌页。
func (m *Model) advancePage(delta int) {
	o := m.obs()
	if m.step == stepDefuseCustom {
		m.pos = clampInt(m.pos+delta, 0, o.DeckCount)
		return
	}
	if m.step == stepTarget {
		pages := max(1, (len(m.targets())+1)/2)
		m.targetPage = clampInt(m.targetPage+delta, 0, pages-1)
		return
	}
	_, pages := m.pageCards()
	m.page = clampInt(m.page+delta, 0, pages-1)
}

func (m *Model) handlePlaying(key string) tea.Cmd {
	o := m.obs()
	actions, _ := m.commandActions()
	action, _ := ui.ResolveAction(actions, key)
	if action == cardAction.ID {
		idx, _ := digit(key)
		m.pickCard(idx)
		return nil
	}
	switch action {
	case "d":
		m.act(engine.Action{Type: engine.Draw, Seat: o.Seat, Target: -1})
	case "g":
		m.startPair()
	}
	return nil
}

// pickCard：手牌只能出"功能"牌；猫牌走 G，保命/炸弹不能主动出。
func (m *Model) pickCard(idx int) {
	page, _ := m.pageCards()
	if idx < 0 || idx >= len(page) {
		return
	}
	c := page[idx]
	o := m.obs()
	switch c.Kind {
	case engine.ExplodingKitten:
		m.fail("爆炸猫不能主动打出")
	case engine.Defuse:
		m.fail("拆弹牌会在你摸到炸弹时自动使用")
	case engine.Nope:
		m.fail("否决只在别人出牌时使用，按 N")
	case engine.TacoCat, engine.Cattermelon, engine.HairyPotatoCat, engine.BeardCat, engine.RainbowCat:
		m.fail("猫牌不能单张出：按 G 用同款两张盲抽别人")
	case engine.Favor:
		m.step = stepTarget
		m.target = -1
		m.pairDraw = false
		m.pairK = c.Kind
		m.targetPage = 0
		m.say()
	default:
		m.act(engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: []int{c.ID}, Target: -1})
	}
}

// startPair 是对子盲抽：系统自动找出两张同款猫牌，玩家只需要选目标。
func (m *Model) startPair() {
	kind, pairs := m.pairKind()
	if pairs == 0 || kind == "" {
		m.fail("没有两张同款的猫牌，凑不齐一对")
		return
	}
	m.pairK = kind
	m.target = -1
	m.pairDraw = true
	m.step = stepTarget
	m.targetPage = 0
	m.say()
}

func (m *Model) handleTarget(key string) tea.Cmd {
	o := m.obs()
	if _, ok := digit(key); ok {
		id, found := ui.ResolveAction(m.targetActions(), key)
		target, err := strconv.Atoi(id)
		if !found || err != nil {
			m.fail("选一个还有手牌的对手")
			return nil
		}
		m.target = target
		m.say()
		m.submitTarget(o)
		return nil
	}
	action, _ := ui.ResolveAction([]ui.Action{targetConfirmAction, ui.EscapeAction("")}, key)
	switch action {
	case "confirm-target":
		if m.target < 0 {
			m.fail("先按数字选目标")
			return nil
		}
		m.submitTarget(o)
	case "cancel":
		m.reset()
		m.say()
	}
	return nil
}

func (m *Model) submitTarget(o engine.Observation) {
	if m.pairDraw {
		var ids []int
		for _, c := range sortHand(o.Hand) {
			if c.Kind == m.pairK && len(ids) < 2 {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) < 2 {
			m.fail("手里没有两张同款猫牌了")
			m.reset()
			return
		}
		if !m.act(engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: ids, Target: m.target}) {
			return
		}
	} else {
		for _, c := range o.Hand {
			if c.Kind == engine.Favor {
				if !m.act(engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: []int{c.ID}, Target: m.target}) {
					return
				}
				break
			}
		}
	}
	m.reset()
}

// handleResponding：按 N 打出否决，按 P 放弃。
func (m *Model) handleResponding(key string) tea.Cmd {
	o := m.obs()
	action, _ := ui.ResolveAction(responseActions, key)
	switch action {
	case "n":
		if m.nopeCount() == 0 {
			m.fail("你手里没有否决牌")
			return nil
		}
		if !m.canNope() {
			return nil
		}
		for _, c := range o.Hand {
			if c.Kind == engine.Nope {
				if m.act(engine.Action{Type: engine.PlayNope, Seat: o.Seat, CardIDs: []int{c.ID}, Target: -1}) {
					m.responseSent = responseKey(o)
				}
				return nil
			}
		}
		m.fail("你手里没有否决牌")
	case "p":
		if !m.passed() && m.passSent != responseKey(o) {
			if m.act(engine.Action{Type: engine.Pass, Seat: o.Seat, Target: -1}) {
				m.passSent = responseKey(o)
			}
		}
	}
	return nil
}

func (m *Model) handleGiving(key string) tea.Cmd {
	o := m.obs()
	if o.Pending == nil || o.Pending.Action.Target != o.Seat {
		return nil
	}
	actions, _ := m.commandActions()
	if action, _ := ui.ResolveAction(actions, key); action == cardAction.ID {
		idx, _ := digit(key)
		page, _ := m.pageCards()
		if idx >= len(page) {
			return nil
		}
		m.act(engine.Action{Type: engine.Give, Seat: o.Seat, CardIDs: []int{page[idx].ID}, Target: -1})
	}
	return nil
}

// handleReinserting：保命牌自动消耗，这里只负责"塞回第几张"。
//
//	0 牌堆顶   1 第2张   2 第3张   B 牌堆底   R 随机   I 指定位置
func (m *Model) handleReinserting(key string) tea.Cmd {
	o := m.obs()
	if o.Turn != o.Seat {
		return nil
	}
	if m.step == stepDefuseCustom {
		// Custom-position keys were already dispatched by handleKey's modal scope.
		return nil
	}

	action, _ := ui.ResolveAction(reinsertActions, key)
	switch action {
	case "0":
		m.useDefuse(o, 0)
	case "1":
		m.useDefuse(o, min(1, o.DeckCount))
	case "2":
		m.useDefuse(o, min(2, o.DeckCount))
	case "b":
		m.useDefuse(o, o.DeckCount)
	case "r":
		m.useDefuse(o, rand.Intn(o.DeckCount+1))
	case "i":
		m.step = stepDefuseCustom
		m.pos = 0
		m.say()
	}
	return nil
}

func (m *Model) useDefuse(o engine.Observation, position int) {
	for _, c := range o.Hand {
		if c.Kind == engine.Defuse {
			m.act(engine.Action{
				Type: engine.UseDefuse, Seat: o.Seat,
				CardIDs: []int{c.ID}, Target: -1, Position: position,
			})
			m.pos = 0
			return
		}
	}
	m.fail("你手里没有拆弹牌")
}

func (m *Model) lobbyKey(key string) {
	action, _ := ui.ResolveAction(lobbyActions, key)
	switch action {
	case "s":
		if !m.snap.IsHost || m.c == nil {
			m.fail("只有房主能开局，等房主按 S")
			return
		}
		if err := m.c.Start(); err != nil {
			m.fail(err.Error())
		} else {
			m.say()
		}
	case "b":
		if !m.snap.IsHost || m.c == nil {
			m.fail("只有房主能加电脑")
			return
		}
		if err := m.c.AddBot(); err != nil {
			m.fail(err.Error())
		} else {
			m.say()
		}
	case "v":
		if m.c == nil {
			return
		}
		if err := m.c.VoteRestart(true); err != nil {
			m.fail(err.Error())
		} else {
			m.say()
		}
	}
}

func digit(key string) (int, bool) {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return int(key[0] - '1'), true
	}
	return 0, false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *Model) toggleLayout() {
	if m.opts.Layout == ui.LayoutCompact {
		m.opts.Layout = ui.LayoutRich
	} else {
		m.opts.Layout = ui.LayoutCompact
	}
	if m.opts.SetLayout != nil {
		m.opts.SetLayout(m.opts.Key, m.opts.Layout)
	}
}

// remaining 是响应窗口还剩几秒（向上取整），没有倒计时返回 -1。
func (m *Model) remaining() int { return secondsLeft(m.deadline) }

// revealLeft 是预言结果还剩几秒；到点就清掉。
func (m *Model) revealLeft() int {
	if m.revealAt.IsZero() {
		return -1
	}
	left := secondsLeft(m.revealAt)
	if left <= 0 {
		return -1
	}
	return left
}

func secondsLeft(t time.Time) int {
	if t.IsZero() {
		return -1
	}
	left := time.Until(t)
	if left <= 0 {
		return 0
	}
	return int((left + time.Second - 1) / time.Second)
}

// fmtSeconds 把剩余时间写成 "3.6s"。
func fmtSeconds(t time.Time) string {
	if t.IsZero() {
		return "0.0s"
	}
	left := time.Until(t)
	if left < 0 {
		left = 0
	}
	return fmt.Sprintf("%.1fs", left.Seconds())
}

func (m *Model) seatName(seat int) string {
	if seat == m.snap.MySeat {
		return "你"
	}
	for _, p := range m.snap.Players {
		if p.Seat == seat {
			if p.Name != "" {
				return ui.CutWidth(p.Name, 8)
			}
			return fmt.Sprintf("P%d", seat+1)
		}
	}
	return fmt.Sprintf("P%d", seat+1)
}
