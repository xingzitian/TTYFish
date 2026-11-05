// Package tui renders Exploding Kittens using only the player's filtered snapshot.
//
// 交互原则和 UNO 保持一致：**一个动作一次按键**。没有输入框、没有命令语法，
// 所有选择都靠数字键推进，界面上始终写着"现在能按什么"。
package tui

import (
	"fmt"
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

// step 是"玩家正在选什么"。牌局阶段由快照决定，step 只描述进行中的选择，
// 这样一个动作不会同时有两种解释。
type step int

const (
	stepIdle       step = iota
	stepCombo           // 已选一张猫牌，等第二/第三张同色
	stepTarget          // 等选目标玩家
	stepRequest         // 三张组合：等选要哪种牌
	stepDefusePick      // 拆弹第一步：选哪张拆弹牌
	stepDefusePos       // 拆弹第二步：选放回第几张
)

const (
	handPageSize   = 5
	tickInterval   = 200 * time.Millisecond
	errTTL         = 2 * time.Second
	playerPanelTTL = 5 * time.Second
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

	page      int
	step      step
	picked    []engine.Card
	target    int
	pos       int
	deadline  time.Time
	playersAt time.Time
}

type closedMsg struct{}
type tickMsg struct{}

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
		m.anchorDeadline()
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

func (m *Model) Init() tea.Cmd {
	return tea.Batch(wait(m.sub), m.tick())
}

// tick 只在有倒计时的时候继续排下一帧，避免空闲时也每 200ms 重绘。
func (m *Model) tick() tea.Cmd {
	if m.deadline.IsZero() {
		return nil
	}
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) Close() {}

// HasModal 告诉网关：现在有弹窗/选择占着键盘，Esc 先归游戏处理。
func (m *Model) HasModal() bool {
	return m.boss || m.help || m.step != stepIdle
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case client.SnapshotMsg:
		m.applySnapshot(client.Snapshot(v))
		return m, tea.Batch(wait(m.sub), m.tick())
	case closedMsg:
		m.closed = true
		return m, nil
	case tickMsg:
		return m, m.tick()
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(v)
	}
	return m, nil
}

// applySnapshot 在快照变化时收敛界面状态：阶段变了就丢掉未完成的选择，
// 倒计时按服务端给的剩余毫秒重新锚定。
func (m *Model) applySnapshot(next client.Snapshot) {
	prevPhase := m.obs().Phase
	m.snap = next
	o := m.obs()
	if o.Phase != prevPhase || next.Phase != client.Playing {
		m.reset()
	}
	m.anchorDeadline()
}

// anchorDeadline 把"服务端剩余毫秒"换成截止时刻；本地每 200ms 只是重算差值，
// 不依赖服务端继续推快照。
func (m *Model) anchorDeadline() {
	if m.snap.PendingLeftMS > 0 {
		m.deadline = time.Now().Add(time.Duration(m.snap.PendingLeftMS) * time.Millisecond)
		return
	}
	m.deadline = time.Time{}
}

func (m *Model) reset() {
	m.step = stepIdle
	m.picked = nil
	m.target = -1
	m.pos = 0
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

func (m *Model) errVisible() bool {
	return m.errMsg != "" && time.Since(m.errAt) < errTTL
}

// say 成功时清掉旧错误，避免提示一直挂在那里。
func (m *Model) say() { m.errMsg = "" }

func (m *Model) act(a engine.Action) {
	if m.c == nil || m.closed || m.snap.Kicked {
		m.fail("连接不可用")
		return
	}
	if err := m.c.Act(a); err != nil {
		m.fail(err.Error())
		return
	}
	m.say()
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

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()

	if key == "ctrl+b" {
		m.boss = !m.boss
		return nil
	}
	if m.boss {
		if key == "esc" {
			m.boss = false
		}
		return nil
	}
	if m.help {
		// 帮助里只认关闭键，q 依然断开
		switch key {
		case "?", "/", "esc":
			m.help = false
		case "q":
			return tea.Quit
		}
		return nil
	}

	switch key {
	case "?", "/":
		m.help = true
		return nil
	case "i":
		m.toggleLayout()
		return nil
	case "m":
		m.opts.Mode = m.opts.Mode.Next()
		if m.opts.SetMode != nil {
			m.opts.SetMode(m.opts.Key, m.opts.Mode)
		}
		return nil
	case "tab":
		m.playersAt = time.Now().Add(playerPanelTTL)
		return nil
	case "pagedown", "=":
		// 拆弹选位置时，-= 是"调整位置"而不是翻页
		if m.step == stepDefusePos {
			m.handleDefusePos("=")
			return nil
		}
		if m.step == stepRequest {
			if _, pages := m.requestPage(); m.page < pages-1 {
				m.page++
			}
			return nil
		}
		if _, pages := m.pageCards(); m.page < pages-1 {
			m.page++
		}
		return nil
	case "pageup", "-":
		if m.step == stepDefusePos {
			m.handleDefusePos("-")
			return nil
		}
		if m.step == stepRequest {
			if m.page > 0 {
				m.page--
			}
			return nil
		}
		if m.page > 0 {
			m.page--
		}
		return nil
	case "esc":
		if m.step != stepIdle {
			m.reset()
			m.say()
		}
		return nil
	case "q":
		// 老板键：不管正在选什么，按下去就是立刻断开。
		// 以前这里在"选牌中"会变成取消选择，玩家以为断开了其实没有。
		return tea.Quit
	}

	// 大厅 / 结算：房主可以开局、加电脑
	if !m.inGame() {
		m.lobbyKey(key)
		return nil
	}
	if !m.alive() {
		return nil
	}

	o := m.obs()
	switch o.Phase {
	case engine.Responding:
		return m.handleResponding(key)
	case engine.Giving:
		return m.handleGiving(key)
	case engine.Reinserting:
		return m.handleReinserting(key)
	}

	switch m.step {
	case stepCombo:
		return m.handleCombo(key)
	case stepTarget:
		return m.handleTarget(key)
	case stepRequest:
		return m.handleRequest(key)
	}

	if !m.myTurn() {
		return nil
	}
	if idx, ok := digit(key); ok {
		m.pickCard(idx)
		return nil
	}
	switch key {
	case "d":
		m.act(engine.Action{Type: engine.Draw, Seat: o.Seat, Target: -1})
	case "enter", " ":
		m.fail("先按牌前面的数字选牌；想摸牌按 D")
	}
	return nil
}

func (m *Model) lobbyKey(key string) {
	switch key {
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

// pickCard 是"按数字选牌"的统一入口：不同牌进入不同流程。
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
		m.fail("拆弹牌只在摸到爆炸猫时使用")
	case engine.Nope:
		m.fail("反制只在有人出牌时使用")
	case engine.TacoCat, engine.Cattermelon, engine.HairyPotatoCat, engine.BeardCat, engine.RainbowCat:
		m.step = stepCombo
		m.picked = []engine.Card{c}
		m.say()
	case engine.Favor:
		// 索要必须选目标
		m.page = 0
		m.step = stepTarget
		m.picked = []engine.Card{c}
		m.target = -1
		m.say()
	default:
		// 攻击 / 跳过 / 洗牌 / 预知：单张直接出
		m.act(engine.Action{Type: engine.Play, Seat: o.Seat, CardIDs: []int{c.ID}, Target: -1})
	}
}

// handleCombo：再按一张同色的牌就加进来；按错颜色立即取消。
func (m *Model) handleCombo(key string) tea.Cmd {
	if idx, ok := digit(key); ok {
		page, _ := m.pageCards()
		if idx >= len(page) || len(m.picked) == 0 {
			return nil
		}
		c := page[idx]
		if c.Kind != m.picked[0].Kind {
			m.reset()
			m.fail("颜色不一样，已取消；同色才能凑组合")
			return nil
		}
		m.picked = append(m.picked, c)
		if len(m.picked) >= 3 {
			m.page = 0
			m.step = stepTarget
			m.target = -1
		}
		m.say()
		return nil
	}
	switch key {
	case "enter", " ":
		if len(m.picked) < 2 {
			m.fail("至少要两张同色猫牌")
			return nil
		}
		m.page = 0
		m.step = stepTarget
		m.target = -1
		m.say()
	}
	return nil
}

// handleTarget：数字选座位，回车确认。
func (m *Model) handleTarget(key string) tea.Cmd {
	o := m.obs()
	if idx, ok := digit(key); ok {
		seat := idx
		if seat < 0 || seat >= len(o.Alive) || !o.Alive[seat] || seat == o.Seat {
			m.fail("选一个还活着的对手")
			return nil
		}
		if o.HandCounts[seat] == 0 {
			m.fail("这个玩家没手牌")
			return nil
		}
		m.target = seat
		m.say()
		return nil
	}
	switch key {
	case "enter", " ":
		if m.target < 0 {
			m.fail("先按数字选目标")
			return nil
		}
		if len(m.picked) >= 3 {
			m.page = 0
			m.step = stepRequest
			return nil
		}
		m.submitCombo(o)
	case "esc":
		m.reset()
		m.say()
	}
	return nil
}

func (m *Model) submitCombo(o engine.Observation) {
	m.act(engine.Action{
		Type: engine.Play, Seat: o.Seat, CardIDs: cardIDs(m.picked), Target: m.target,
	})
	m.reset()
}

// requestOptions 是三张组合可以要的牌，按色板顺序排。
var requestOptions = []engine.Kind{
	engine.Defuse, engine.Nope, engine.Attack, engine.Skip,
	engine.Favor, engine.Shuffle, engine.SeeTheFuture,
	engine.TacoCat, engine.Cattermelon, engine.HairyPotatoCat,
	engine.BeardCat, engine.RainbowCat,
}

func (m *Model) handleRequest(key string) tea.Cmd {
	o := m.obs()
	if idx, ok := digit(key); ok {
		page, _ := m.requestPage()
		if idx < 0 || idx >= len(page) {
			return nil
		}
		m.act(engine.Action{
			Type: engine.Play, Seat: o.Seat, CardIDs: cardIDs(m.picked),
			Target: m.target, Requested: page[idx],
		})
		m.reset()
	}
	switch key {
	case "esc":
		m.reset()
	}
	return nil
}

// requestPage 把可要的牌种切成每页 9 个（数字键只有 1-9）。
func (m *Model) requestPage() ([]engine.Kind, int) {
	const size = 9
	pages := (len(requestOptions) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if m.page >= pages {
		m.page = pages - 1
	}
	start := m.page * size
	end := start + size
	if end > len(requestOptions) {
		end = len(requestOptions)
	}
	return requestOptions[start:end], pages
}

func cardIDs(cards []engine.Card) []int {
	out := make([]int, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ID)
	}
	return out
}

// handleResponding：反制窗口。按数字选一张反制牌，或 P 放弃。
func (m *Model) handleResponding(key string) tea.Cmd {
	o := m.obs()
	if o.Pending != nil && o.Seat < len(o.Pending.Passed) && o.Pending.Passed[o.Seat] {
		m.fail("你已经放弃这次响应")
		return nil
	}
	if idx, ok := digit(key); ok {
		page, _ := m.pageCards()
		if idx >= len(page) {
			return nil
		}
		c := page[idx]
		if c.Kind != engine.Nope {
			m.fail("响应只能打反制（紫色那张）")
			return nil
		}
		m.act(engine.Action{Type: engine.PlayNope, Seat: o.Seat, CardIDs: []int{c.ID}, Target: -1})
		return nil
	}
	if key == "p" {
		m.act(engine.Action{Type: engine.Pass, Seat: o.Seat, Target: -1})
	}
	return nil
}

// handleGiving：别人索要，我选一张交出去。
func (m *Model) handleGiving(key string) tea.Cmd {
	o := m.obs()
	if o.Pending == nil || o.Pending.Action.Target != o.Seat {
		return nil
	}
	if idx, ok := digit(key); ok {
		page, _ := m.pageCards()
		if idx >= len(page) {
			return nil
		}
		m.act(engine.Action{Type: engine.Give, Seat: o.Seat, CardIDs: []int{page[idx].ID}, Target: -1})
	}
	return nil
}

// handleReinserting：拆弹两步走。第一步选牌，第二步选位置。
func (m *Model) handleReinserting(key string) tea.Cmd {
	o := m.obs()
	if o.Turn != o.Seat {
		return nil
	}
	if m.step == stepDefusePos {
		return m.handleDefusePos(key)
	}

	if idx, ok := digit(key); ok {
		page, _ := m.pageCards()
		if idx >= len(page) {
			return nil
		}
		c := page[idx]
		if c.Kind != engine.Defuse {
			m.fail("请选一张拆弹牌（绿色那张）")
			return nil
		}
		m.picked = []engine.Card{c}
		m.step = stepDefusePos
		m.pos = 0
		m.say()
	}
	return nil
}

func (m *Model) handleDefusePos(key string) tea.Cmd {
	o := m.obs()
	switch key {
	case "-", "pageup":
		if m.pos > 0 {
			m.pos--
		}
	case "=", "pagedown":
		if m.pos < o.DeckCount {
			m.pos++
		}
	case "enter", " ":
		if len(m.picked) == 0 {
			m.fail("先选一张拆弹牌")
			return nil
		}
		m.act(engine.Action{
			Type: engine.UseDefuse, Seat: o.Seat,
			CardIDs: []int{m.picked[0].ID}, Target: -1, Position: m.pos,
		})
		m.reset()
	case "esc":
		m.reset()
	case "d", "0":
		m.pos = 0
	}
	return nil
}

// digit 把 "1".."9" 变成 0..8；其它键返回 false。
func digit(key string) (int, bool) {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return int(key[0] - '1'), true
	}
	return 0, false
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

// remaining 是响应窗口还剩多少秒（向上取整），没有倒计时时返回 -1。
func (m *Model) remaining() int {
	if m.deadline.IsZero() {
		return -1
	}
	left := time.Until(m.deadline)
	if left <= 0 {
		return 0
	}
	return int((left + time.Second - 1) / time.Second)
}

// seatName 给"你"这类称呼用。
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

// isPicked 判断这张牌是不是已经进了当前组合/拆弹选择，界面上会打标记。
func (m *Model) isPicked(id int) bool {
	for _, c := range m.picked {
		if c.ID == id {
			return true
		}
	}
	return false
}
