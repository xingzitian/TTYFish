package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// 极简模式：一页 5 张手牌，玩家面板弹出后 5 秒自动收回。
const (
	compactPageSize  = 5
	playerPanelTTL   = 5 * time.Second
	compactTurnPeers = 3 // TURN 行最多显示几个玩家
)

// hidePlayersMsg 是玩家面板到点自动收回的定时消息。
type hidePlayersMsg struct{}

// cardRank 是排序键：颜色 → 面值 → card_id。
//
// 颜色顺序按玩家习惯写死成"红黄蓝绿万能"，**不能直接用引擎的枚举值**：
// 引擎里 Green 排在 Blue 前面（那是规则内部的事），显示顺序跟它无关。
var colorRank = map[engine.Color]int{
	engine.Red:     0,
	engine.Yellow:  1,
	engine.Blue:    2,
	engine.Green:   3,
	engine.NoColor: 4,
}

func kindRank(c engine.Card) int {
	switch c.Kind {
	case engine.Number:
		if c.Num >= 0 && c.Num <= 9 {
			return c.Num
		}
		return 9
	case engine.Skip:
		return 10
	case engine.Reverse:
		return 11
	case engine.DrawTwo:
		return 12
	case engine.Wild:
		return 13
	case engine.WildDrawFour:
		return 14
	}
	return 15
}

// sortHand 把一手牌按"红黄蓝绿万能 → 数字升序 → 功能牌 → card_id"排好。
//
// 纯显示层：出牌仍然用 card_id，所以怎么排都不影响规则。
func sortHand(cards []engine.Card) []engine.Card {
	out := make([]engine.Card, len(cards))
	copy(out, cards)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := colorRank[out[i].Color], colorRank[out[j].Color]
		if ci != cj {
			return ci < cj
		}
		ki, kj := kindRank(out[i]), kindRank(out[j])
		if ki != kj {
			return ki < kj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// compactPage 返回当前页的手牌和总页数，顺手把页码夹回合法范围。
func (m *Model) compactPage() ([]engine.Card, int) {
	hand := sortHand(m.obs().Hand)
	pages := (len(hand) + compactPageSize - 1) / compactPageSize
	if pages < 1 {
		pages = 1
	}
	if m.page > pages-1 {
		m.page = pages - 1
	}
	if m.page < 0 {
		m.page = 0
	}
	start := m.page * compactPageSize
	if start > len(hand) {
		start = len(hand)
	}
	end := start + compactPageSize
	if end > len(hand) {
		end = len(hand)
	}
	return hand[start:end], pages
}

// handleCompactKey 是极简模式的按键：数字直接出牌，左右翻页，Tab 看玩家。
func (m *Model) handleCompactKey(key string) tea.Cmd {
	switch key {
	case "q":
		return m.quit()
	case "m":
		// 颜色深浅在两种模式里都能换
		m.mode = m.mode.Next()
		if m.opts.SetMode != nil {
			m.opts.SetMode(m.id.Key, m.mode)
		}
		return nil
	case "i":
		m.toggleLayout()
		return nil
	case "tab":
		m.playersUntil = time.Now().Add(playerPanelTTL)
		return tea.Tick(playerPanelTTL, func(time.Time) tea.Msg { return hidePlayersMsg{} })
	case "left", "h":
		if m.page > 0 {
			m.page--
		}
		return nil
	case "right", "l":
		if _, pages := m.compactPage(); m.page < pages-1 {
			m.page++
		}
		return nil
	}

	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		hand, _ := m.compactPage()
		if idx >= len(hand) {
			return nil
		}
		m.playCardID(hand[idx])
		return nil
	}

	return m.handleSharedKey(key)
}

// handleSharedKey 是两种模式共用的动作：喊 UNO、摸牌、过牌、抓包、聊天、投票。
func (m *Model) handleSharedKey(key string) tea.Cmd {
	switch key {
	case "u":
		m.act(engine.CallUno{Seat: m.snap.MySeat})
	case "d":
		m.act(engine.DrawCard{Seat: m.snap.MySeat})
	case "p":
		m.act(engine.Pass{Seat: m.snap.MySeat})
	case "c":
		obs := m.obs()
		if obs.UnoPending >= 0 {
			m.act(engine.CatchUno{Seat: m.snap.MySeat, Target: obs.UnoPending})
		} else {
			// 没得抓也要有反馈，否则玩家会以为键坏了
			m.fail("现在没有人漏喊 UNO")
		}
	case "t":
		m.chat = true
	case "v":
		if m.c != nil {
			if err := m.c.VoteRestart(true); err != nil {
				m.fail(err.Error())
			} else {
				m.fail("")
			}
		}
	}
	return nil
}

// playCardID 按 card_id 出牌；万能牌要先选颜色。
func (m *Model) playCardID(c engine.Card) {
	if c.Kind == engine.Wild || c.Kind == engine.WildDrawFour {
		m.choosing = true
		m.pending = c
		m.fail("")
		return
	}
	if !m.canPlay(c.ID) {
		m.fail("这张牌现在出不了：颜色或数字对不上")
		return
	}
	m.act(engine.PlayCard{Seat: m.snap.MySeat, CardID: c.ID})
}

// compactToken 把一张牌画成 [R|7]。
//
// 三种颜色模式在极简下都要保持可认：满色给背景块，文字模式只染色，无色就是纯文字。
// 这里不复用 cardStyle——那个带固定宽度和右边距，会把一行撑开。
func (m *Model) compactToken(c engine.Card, marked bool) string {
	text := "[" + compactFace(c) + "]"
	st := m.renderer.NewStyle()
	switch m.mode {
	case ui.ColorText:
		st = st.Foreground(fgOnly(c.Color))
	case ui.ColorNone:
	default:
		st = st.Background(bgFor(c.Color)).Foreground(fgFor(c.Color))
	}
	if marked {
		st = st.Bold(true).Underline(true)
	}
	return st.Render(text)
}

// viewCompactLobby 是极简模式的等候界面。
//
// 和牌桌共用一个 minimal 开关：在大厅按 i 切到极简，进牌桌还是极简，
// 反过来也一样——"摸鱼模式"是玩家自己的状态，不该按界面分开记。
func (m *Model) viewCompactLobby() string {
	obs := m.obs()
	lines := make([]string, 0, 5)

	// 上一局结果：只有打完一局才有，刚开房不占这一行
	if obs.RoundWinner >= 0 && obs.RoundWinner < len(obs.HandCounts) {
		points := 0
		if obs.RoundWinner < len(obs.Scores) {
			points = obs.Scores[obs.RoundWinner]
		}
		lines = append(lines, m.ok().Render("[FIN] >> ")+
			fmt.Sprintf("%s WIN! 总分 %d", shortName(m.name(obs.RoundWinner), 10), points))
	}

	room := "[ROOM] " + m.snap.Code
	if obs.MatchTo > 0 {
		room += fmt.Sprintf("  打到 %d 分", obs.MatchTo)
	}
	lines = append(lines, m.dim().Render(room))
	lines = append(lines, m.compactLobbyPlayers()...)
	if m.chat {
		// 聊天菜单自己会盖住下面的内容
	} else if msg := m.compactFeed(); msg != "" {
		lines = append(lines, msg)
	}

	if m.chat {
		lines = append(lines, strings.Split(m.viewChat(), "\n")...)
	}
	lines = append(lines, m.dim().Render(m.compactLobbyCmd()))

	return strings.Join(lines, "\n")
}

// compactLobbyPlayers 大厅里的玩家行：只有名字，**不显示牌数**——
// 大厅还没发牌，写"7张"是假的。
func (m *Model) compactLobbyPlayers() []string {
	names := make([]string, 0, len(m.snap.Players))
	for i, p := range m.snap.Players {
		name := shortName(p.Name, 10)
		if i == m.snap.MySeat && name != "你" {
			name += "(你)"
		}
		if p.Host {
			name += "*"
		}
		if !p.Connected {
			name += "(掉线)"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return []string{m.dim().Render("[PLAYER] (还没有人)")}
	}

	var out []string
	for i := 0; i < len(names); i += 4 {
		end := i + 4
		if end > len(names) {
			end = len(names)
		}
		prefix := "         "
		if i == 0 {
			prefix = m.dim().Render("[PLAYER] ")
		}
		out = append(out, prefix+strings.Join(names[i:end], " "))
	}
	return out
}

// compactLobbyCmd 只列真的能按的键：房间设置、减电脑这些后端还没有的能力先不画。
func (m *Model) compactLobbyCmd() string {
	items := []string{}
	if m.snap.IsHost {
		items = append(items, "[S]开始", "[C]+电脑")
	} else {
		items = append(items, "等房主开始")
	}
	items = append(items, "[T]聊天", "[V]投票", "[Q]离开", "[I]界面")

	body := "[CMD] " + strings.Join(items, " ")
	for len([]rune(body)) > m.width && len(items) > 1 {
		items = items[:len(items)-1]
		body = "[CMD] " + strings.Join(items, " ")
	}
	return m.dim().Render(body)
}

// viewCompact 画 4~5 行：回合、桌面、手牌、消息、按键。
func (m *Model) viewCompact() string {
	obs := m.obs()
	lines := make([]string, 0, 6)
	lines = append(lines, m.compactTurn(obs))
	lines = append(lines, m.compactPile(obs))
	lines = append(lines, m.compactHand(obs))

	if m.chat {
		// 聊天菜单是模态的，极简模式也得画出来，不然按 t 像死了一样
		lines = append(lines, strings.Split(m.viewChat(), "\n")...)
	} else if m.playersVisible() {
		lines = append(lines, m.compactPlayers(obs)...)
	} else if msg := m.compactFeed(); msg != "" {
		lines = append(lines, msg)
	}
	lines = append(lines, m.compactCmd(obs))

	return strings.Join(lines, "\n")
}

// compactTurn 只显示以当前出牌人为圆心的 3 个人，自己不在窗口里就补一句牌数。
func (m *Model) compactTurn(obs engine.Observation) string {
	order := m.turnWindow(obs)
	parts := make([]string, 0, len(order))
	for _, seat := range order {
		parts = append(parts, m.seatBadge(obs, seat))
	}

	arrow := "→"
	if obs.Dir < 0 {
		arrow = "←"
	}
	line := ">> " + strings.Join(parts, " -> ") + " | 方向: " + arrow

	inWindow := false
	for _, seat := range order {
		if seat == obs.Seat {
			inWindow = true
			break
		}
	}
	if !inWindow {
		line += "  ⇒ 你:" + fmt.Sprintf("%d张", len(obs.Hand))
	}

	return m.turn().Render("[TURN] ") + line
}

// turnWindow 返回从当前出牌人开始的至多 3 个座位。
func (m *Model) turnWindow(obs engine.Observation) []int {
	seats := len(obs.HandCounts)
	if seats == 0 {
		return nil
	}
	out := make([]int, 0, compactTurnPeers)
	dir := 1
	if obs.Dir < 0 {
		dir = -1
	}
	for i := 0; i < compactTurnPeers && i < seats; i++ {
		out = append(out, ((obs.Turn+dir*i)%seats+seats)%seats)
	}
	return out
}

// seatBadge 是 TURN 行里的一个人：名字(几张)，自己标"你"，只剩一张标 UNO。
func (m *Model) seatBadge(obs engine.Observation, seat int) string {
	name := shortName(m.name(seat), 8)
	count := 0
	if seat >= 0 && seat < len(obs.HandCounts) {
		count = obs.HandCounts[seat]
	}
	badge := name + "(" + fmt.Sprintf("%d张", count)
	if seat == obs.Seat && name != "你" {
		badge += " 你"
	}
	if count == 1 {
		badge += " ⚠️UNO"
	}
	return badge + ")"
}

func (m *Model) compactPile(obs engine.Observation) string {
	body := m.compactToken(obs.Top, false)
	who := m.lastPlayer(obs)
	if who != "" {
		body += "  (上一手: " + shortName(who, 10) + " 打出)"
	}
	return m.dim().Render("[PILE] 当前桌面: ") + body
}

func (m *Model) compactHand(obs engine.Observation) string {
	hand, pages := m.compactPage()
	head := m.dim().Render("[HAND] ")
	if m.page > 0 {
		head += m.dim().Render("← ")
	}
	right := ""
	if m.page < pages-1 {
		right = " →"
	}

	items := make([]string, 0, len(hand))
	for i, c := range hand {
		items = append(items, fmt.Sprintf("%d.%s", i+1, m.compactToken(c, c.ID == obs.DrawnID)))
	}
	if len(items) == 0 {
		items = append(items, "(没牌了)")
	}

	tail := m.dim().Render(right)
	if pages > 1 {
		tail += m.dim().Render(fmt.Sprintf(" %d/%d", m.page+1, pages))
	}
	return head + strings.Join(items, " ") + tail
}

// compactPlayers 是 Tab 弹出的玩家面板：每行最多 3 个人，按座位顺序。
func (m *Model) compactPlayers(obs engine.Observation) []string {
	names := make([]string, 0, len(obs.HandCounts))
	for seat := range obs.HandCounts {
		name := shortName(m.name(seat), 8)
		badge := name + "(" + fmt.Sprintf("%d张", obs.HandCounts[seat])
		if seat == obs.Seat && name != "你" {
			badge += " 你"
		}
		if obs.HandCounts[seat] == 1 {
			badge += " ⚠️UNO"
		}
		names = append(names, badge+")")
	}

	var out []string
	for i := 0; i < len(names); i += 3 {
		end := i + 3
		if end > len(names) {
			end = len(names)
		}
		prefix := "         "
		if i == 0 {
			prefix = m.dim().Render("[PLAYER] ")
		}
		out = append(out, prefix+strings.Join(names[i:end], " "))
	}
	if len(out) == 0 {
		out = append(out, m.dim().Render("[PLAYER] (还没有玩家)"))
	}
	return out
}

// compactFeed 是"最近一条公开消息"那一行：聊天、战报、系统提示共用。
//
// 注意数据来源是快照里的 Timeline，不是引擎的 Observation.Log——
// 聊天根本不在引擎里（引擎不管聊天），它由房间追加到 Timeline 并打上 Chat 标记。
// 之前读错了流，所以别人说的话一条都看不见。
func (m *Model) compactFeed() string {
	if n := len(m.snap.Timeline); n > 0 {
		last := m.snap.Timeline[n-1]
		text := strings.TrimSpace(last.Text)
		if text != "" {
			body := shortName(text, 60)
			if last.Seat >= 0 && last.Seat < len(m.snap.Players) {
				who := shortName(m.name(last.Seat), 10)
				if last.Chat {
					body = who + ": " + body
				} else {
					body = who + " " + body
				}
			}
			style := m.dim()
			if last.Chat {
				style = m.plain()
			}
			return m.dim().Render("[MSG] ") + style.Render(body)
		}
	}
	if m.snap.Notice != "" {
		return m.dim().Render("[MSG] ") + m.dim().Render(shortName(m.snap.Notice, 60))
	}
	return ""
}

// compactCmd 是上下文行：有临时状态就只显示那个状态的操作。
func (m *Model) compactCmd(obs engine.Observation) string {
	if m.choosing {
		return m.warn().Render("[CMD] 选颜色 [1]红 [2]黄 [3]蓝 [4]绿  (Esc 取消)")
	}
	// 错误只短暂占位：超过 2 秒就回到正常命令行，否则玩家不知道还能按什么
	if m.errMsg != "" && time.Since(m.errAt) < 2*time.Second {
		return m.warn().Render("[CMD] " + m.errMsg)
	}

	items := []string{"[1-5]出牌", "[U]UNO", "[D]摸牌", "[C]抓包", "[T]聊天", "[Tab]玩家", "[←→]翻页", "[I]界面", "[Q]退"}
	if obs.DrawnID >= 0 {
		items = append(items, "[P]过牌")
	}
	if m.snap.Vote != nil {
		items = append(items, "[V]重开投票")
	}
	body := "[CMD] " + strings.Join(items, " ")
	// 超宽就整项丢掉，不要把一个命令切成两半
	for len([]rune(body)) > m.width && len(items) > 1 {
		items = items[:len(items)-1]
		body = "[CMD] " + strings.Join(items, " ")
	}
	return m.dim().Render(body)
}

// shortName 按显示宽度截断名字，避免长名字把整行撑爆。
func shortName(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// lastPlayer 从公共战报里找上一手是谁打的。
func (m *Model) lastPlayer(obs engine.Observation) string {
	if len(obs.Log) == 0 {
		return ""
	}
	last := obs.Log[len(obs.Log)-1]
	if last.Seat < 0 || last.Seat >= len(obs.HandCounts) {
		return ""
	}
	return m.name(last.Seat)
}

// toggleLayout 在丰富/极简之间切换，并记住这个选择。
func (m *Model) toggleLayout() {
	m.minimal = !m.minimal
	if m.opts.SetLayout != nil {
		l := ui.LayoutRich
		if m.minimal {
			l = ui.LayoutCompact
		}
		m.opts.SetLayout(m.id.Key, l)
	}
}

var _ = lipgloss.NewStyle
var _ = client.Playing
