package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// 卡面只用 ASCII + 背景色。emoji 和中文卡面在不同终端里占的格子宽度
// 不一致（VS Code 终端尤其明显），会把整个布局顶歪。
func colorLetter(c engine.Color) string {
	switch c {
	case engine.Red:
		return "R"
	case engine.Yellow:
		return "Y"
	case engine.Green:
		return "G"
	case engine.Blue:
		return "B"
	}
	return "*"
}

// cardFace 是牌面：颜色和值之间用 | 隔断，外面用方括号包起来。
// 之前是 "R5" 这种粘连写法，颜色一多就很难扫。
func cardFace(c engine.Card) string {
	col := colorLetter(c.Color)
	var v string
	switch c.Kind {
	case engine.Skip:
		v = "S"
	case engine.Reverse:
		v = "↺"
	case engine.DrawTwo:
		v = "+2"
	case engine.Wild:
		v = "W"
	case engine.WildDrawFour:
		v = "+4"
	default:
		v = strconv.Itoa(c.Num)
	}
	return "[" + col + "|" + v + "]"
}

func bgFor(c engine.Color) lipgloss.Color {
	switch c {
	case engine.Red:
		return lipgloss.Color("160")
	case engine.Yellow:
		return lipgloss.Color("220")
	case engine.Green:
		return lipgloss.Color("35")
	case engine.Blue:
		return lipgloss.Color("26")
	}
	return lipgloss.Color("240")
}

func fgFor(c engine.Color) lipgloss.Color {
	if c == engine.Yellow {
		return lipgloss.Color("232")
	}
	return lipgloss.Color("231")
}

// 样式全部从 m.renderer 派生。SSH 下必须按会话注入渲染器，
// 否则 lipgloss 会按服务端进程的 stdout 探测色深，一路退化成无色。
// 下面这一组样式全部由 m.mode 决定，不是只有牌面跟着变。
// 三档：
//
//	满色  文字上色 + 背景色块
//	文字  只给文字上色，背景透明
//	无色  完全不上色，靠加粗/下划线区分层次
func (m *Model) dim() lipgloss.Style {
	s := m.renderer.NewStyle()
	if m.mode == ui.ColorNone {
		return s.Faint(true)
	}
	return s.Foreground(lipgloss.Color("244"))
}

// plain 是不加任何装饰的样式，用来把"最新一条"从一堆暗色记录里拎出来。
func (m *Model) plain() lipgloss.Style {
	return m.renderer.NewStyle()
}

func (m *Model) turn() lipgloss.Style {
	s := m.renderer.NewStyle().Bold(true)
	if m.mode == ui.ColorNone {
		return s.Underline(true)
	}
	return s.Foreground(lipgloss.Color("226"))
}

func (m *Model) warn() lipgloss.Style {
	s := m.renderer.NewStyle().Bold(true)
	if m.mode == ui.ColorNone {
		return s.Underline(true)
	}
	return s.Foreground(lipgloss.Color("203"))
}

func (m *Model) ok() lipgloss.Style {
	s := m.renderer.NewStyle().Bold(true)
	if m.mode == ui.ColorNone {
		return s
	}
	return s.Foreground(lipgloss.Color("114"))
}

func (m *Model) cardStyle(c engine.Card) lipgloss.Style {
	base := m.renderer.NewStyle().
		Width(6).
		Align(lipgloss.Left).
		MarginRight(1)

	switch m.mode {
	case ui.ColorNone:
		return base
	case ui.ColorText:
		return base.Foreground(fgOnly(c.Color))
	default:
		return base.Background(bgFor(c.Color)).Foreground(fgFor(c.Color))
	}
}

// fgOnly 是"只上文字色"用的前景色：比默认调色板更亮，暗色终端上才看得清。
func fgOnly(c engine.Color) lipgloss.Color {
	switch c {
	case engine.Red:
		return lipgloss.Color("203")
	case engine.Yellow:
		return lipgloss.Color("221")
	case engine.Green:
		return lipgloss.Color("114")
	case engine.Blue:
		return lipgloss.Color("75")
	}
	return lipgloss.Color("245")
}

func (m *Model) renderCard(c engine.Card, text string, selected bool) string {
	st := m.cardStyle(c)
	if selected {
		st = st.Bold(true).Underline(true)
	}
	return st.Render(text)
}

func (m *Model) colorChip(c engine.Color) string {
	s := m.renderer.NewStyle().Bold(true)
	switch m.mode {
	case ui.ColorNone:
		return s.Render("[" + colorLetter(c) + "]")
	case ui.ColorText:
		return s.Foreground(fgOnly(c)).Render("[" + colorLetter(c) + "]")
	default:
		return s.Padding(0, 1).Background(bgFor(c)).Foreground(fgFor(c)).Render(colorLetter(c))
	}
}

// panelBorder 是右侧面板那条竖线的颜色。无色模式下干脆不画颜色。
func (m *Model) panelBorder() lipgloss.Style {
	if m.mode == ui.ColorNone {
		return m.renderer.NewStyle()
	}
	return m.renderer.NewStyle().BorderForeground(lipgloss.Color("238"))
}

// 终端尺寸下限。低于这个值不再硬画——挤出来的界面比不画更糟。
//
// 40 列：紧凑提示行就要 36 格，再窄连"u 喊UNO"都摆不下；
// 10 行：轮到你 + 牌桌 + 手牌头 + 至少两行手牌 + 提示，再少必然截断。
const (
	minWidth  = 40
	minHeight = 10
)

func (m *Model) tooSmall() bool {
	return m.width < minWidth || m.height < minHeight
}

func (m *Model) viewTooSmall() string {
	return m.warn().Render("终端太小") + "\n" +
		m.dim().Render(fmt.Sprintf("当前 %d×%d，至少要 %d×%d", m.width, m.height, minWidth, minHeight)) + "\n" +
		m.dim().Render("把窗口拉大一点就行")
}

// viewBoss 是老板键的假象：一行 shell 提示符 + 几条滚动日志。
// 不写"按回车返回"之类的提示，否则一眼就露馅。
func (m *Model) viewBoss() string {
	now := time.Now()
	lines := []string{"[root@prod-db-01 ~]# tail -f /var/log/nginx/access.log"}

	ips := []string{"10.12.3.44", "10.12.3.51", "172.16.8.9", "10.12.3.77"}
	paths := []string{"/api/health", "/api/v1/orders", "/api/v1/users", "/static/js/app.js"}
	status := []string{"200 2", "200 486", "201 128", "304 0"}
	for i := 0; i < 4; i++ {
		ts := now.Add(-time.Duration((4-i)*7) * time.Second).Format("02/Jan/2006:15:04:05 -0700")
		lines = append(lines, fmt.Sprintf("%s - - [%s] \"GET %s HTTP/1.1\" %s \"-\" \"Mozilla/5.0\"",
			ips[i%len(ips)], ts, paths[i%len(paths)], status[i%len(status)]))
	}

	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 0 {
			out = append(out, m.plain().Render(l))
			continue
		}
		out = append(out, m.dim().Render(l))
	}
	return strings.Join(out, "\n")
}

// View 是给 bubbletea 的整帧。
//
// 外面套一层 clampFrame：任何一行超宽、或者总行数超过终端高度，终端就会
// 自己折行/滚动，顶上那几行被顶歪——表现就是"消息一长，界面就乱了"。
// 这一层兜住所有情况（长错误、长昵称、长聊天、窄终端），不用每个地方各自小心。
func (m *Model) View() string {
	return ui.ClampFrame(m.render(), m.width, m.height)
}

// clampFrame 把整帧裁进终端尺寸。按显示宽度裁，ANSI 颜色码不会被切断。

func (m *Model) render() string {
	// 老板键优先：整屏换成假 shell，游戏本身照常在后台跑。
	if m.boss {
		return m.viewBoss()
	}

	switch {
	case m.entering:
		return m.viewEnter()
	case m.closed && m.quitting:
		// 自己按 q 退出：什么都不画。画一句"连接已断开"只会吓自己一跳。
		return ""
	case m.closed && m.kicked:
		return m.warn().Render("你已在另一台设备登录，这个连接被顶掉了。") + "\n" +
			m.dim().Render("那边的窗口接着打；要用这个窗口就重新 ssh 上来。") + "\n"
	case m.closed:
		return m.dim().Render("连接已断开。重新 ssh 上来还能回到刚才的房间。") + "\n"
	case m.snap.Code == "":
		return m.dim().Render("连接中…") + "\n"
	case m.snap.Phase != client.Playing:
		return m.viewLobby()
	}

	if m.tooSmall() {
		return m.viewTooSmall()
	}

	// 布局按"宽度够不够"和"高度够不够"分四种，而不是一个固定阈值。
	// 终端形状千奇百怪：210×9 的横向窗口和 40×40 的竖长窗口都有人用。
	m.layout = m.chooseLayout()
	switch m.layout {
	case layoutThreeCol:
		return m.viewThreeCol()
	case layoutTwoCol:
		return m.viewTwoCol()
	case layoutStacked:
		return m.viewTable(true, playersVertical)
	default:
		return m.viewTable(true, playersHorizontal)
	}
}

// layoutKind 是界面的整体排布方式。
type layoutKind int

const (
	// layoutCompact 窄且矮：单栏，玩家横着排一行，记录压缩到几条。
	layoutCompact layoutKind = iota
	// layoutStacked 窄但高：单栏，玩家竖着排，记录放下面。
	layoutStacked
	// layoutTwoCol 宽且高：左牌桌 + 右面板（玩家/记录/按键竖着码）。
	layoutTwoCol
	// layoutThreeCol 宽但矮：把记录拆到第三栏，用宽度换高度。
	layoutThreeCol
)

const (
	playersColW = 22
	logColW     = 30
	maxLeftW    = 80
)

// chooseLayout 决定这一帧怎么排。
//
//	宽 + 高 → 两栏，右边竖着码玩家、记录、按键
//	宽 + 矮 → 三栏，玩家和记录各占一栏，都能用满整个高度
//	窄 + 高 → 单栏竖排，玩家也竖着列出来
//	窄 + 矮 → 单栏紧凑，只保留牌桌和手牌
func (m *Model) chooseLayout() layoutKind {
	panelW := m.width / 3
	if panelW < 26 {
		panelW = 26
	}
	if panelW > 38 {
		panelW = 38
	}
	leftW := m.width - panelW - 2
	if leftW > maxLeftW {
		leftW = maxLeftW
	}

	if m.width >= 72 && leftW >= 44 && m.height >= 6 {
		// 矮到面板塞不下玩家+记录时，拆成三栏。
		if m.height < 18 && m.width-playersColW-logColW-4 >= 44 {
			return layoutThreeCol
		}
		return layoutTwoCol
	}
	if m.height >= 20 {
		return layoutStacked
	}
	return layoutCompact
}

func (m *Model) viewTwoCol() string {
	panelW := m.width / 3
	if panelW < 26 {
		panelW = 26
	}
	if panelW > 38 {
		panelW = 38
	}
	leftW := m.width - panelW - 2
	if leftW > maxLeftW {
		leftW = maxLeftW
	}

	saved := m.width
	m.width = leftW
	left := m.viewTable(false, playersNone)
	m.width = saved

	// 面板高度取"左边内容高度"和"终端高度"的较大者：
	// 只看左边的话，牌桌只有 7 行，右侧那一栏就被压成 7 行，
	// 明明有 30 行高却只显示两三条记录。
	h := lipgloss.Height(left)
	if m.height > h {
		h = m.height
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", m.viewSide(panelW, h))
}

// viewThreeCol 左边牌桌、中间玩家、右边记录。高度不够时用宽度补：
// 三栏各自都能吃满整个高度，比把玩家和记录挤在一栏里强。
func (m *Model) viewThreeCol() string {
	leftW := m.width - playersColW - logColW - 4
	if leftW > maxLeftW {
		leftW = maxLeftW
	}

	saved := m.width
	m.width = leftW
	left := m.viewTable(false, playersNone)
	m.width = saved

	h := lipgloss.Height(left)
	if m.height > h {
		h = m.height
	}

	players := m.renderPanel(m.playerLines(playersColW-3), playersColW, h)
	logs := m.renderPanel(m.logLines(logColW-3, h), logColW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", players, "  ", logs)
}

func (m *Model) viewEnter() string {
	var b strings.Builder
	b.WriteString(m.dim().Render("输入 6 位房间码加入，或直接回车开一间新房") + "\n\n")
	b.WriteString(m.turn().Render("房间码 ") + m.codeInput.View() + "\n")
	b.WriteString("\n")
	if m.enterErr != "" {
		b.WriteString(m.warn().Render("! "+m.enterErr) + "\n")
	}
	b.WriteString(m.dim().Render("Enter 进入 · Esc 退出 · 退格删除 · Ctrl+U 清空"))
	return b.String()
}

func (m *Model) viewLobby() string {
	var b strings.Builder

	title := fmt.Sprintf("房间 %s   %d/%d 人", m.snap.Code, len(m.snap.Players), m.snap.MaxSeats)
	if m.snap.IsHost {
		title += "   你是房主"
	}
	b.WriteString(m.turn().Render(title) + "\n\n")

	for _, p := range m.snap.Players {
		line := "  " + p.Name
		if p.Seat == m.snap.MySeat && p.Name != "你" {
			line += "（你）"
		}
		if p.Host {
			line += "  房主"
		}
		switch {
		case !p.Connected:
			line += fmt.Sprintf("  [掉线 %ds，电脑代打]", p.Offline)
		case p.Bot && p.Seat != m.snap.MySeat:
			line += "  [电脑]"
		}
		if p.Seat == m.snap.MySeat {
			line = "▶" + line[1:]
			b.WriteString(m.turn().Render(line) + "\n")
		} else {
			b.WriteString(m.dim().Render(line) + "\n")
		}
	}

	b.WriteString("\n")
	if m.snap.Notice != "" {
		b.WriteString(m.ok().Render("  "+m.snap.Notice) + "\n")
	}
	// 大厅里也把最近两条聊天带出来，等人开局时不至于干瞪眼。
	var recentChat []string
	for _, e := range m.snap.Timeline {
		if e.Chat {
			recentChat = append(recentChat, m.entryText(e))
		}
	}
	if len(recentChat) > 2 {
		recentChat = recentChat[len(recentChat)-2:]
	}
	for _, c := range recentChat {
		b.WriteString(m.dim().Render("  "+c) + "\n")
	}
	b.WriteString("\n")

	if m.obs().Phase == engine.MatchOver || m.obs().Phase == engine.RoundOver {
		w := m.obs().RoundWinner
		b.WriteString(m.ok().Render(fmt.Sprintf("%s 赢了这一局，+%d 分", m.name(w), m.obs().Scores[w])) + "\n")
	}

	keys := "等房主开始"
	if m.snap.IsHost {
		keys = "s 开始 · b 补一个电脑"
	}
	keys += " · t 快捷语 · v 重开投票 · l 离开 · q 退出"
	b.WriteString(m.dim().Render(keys))
	if m.errMsg != "" {
		b.WriteString("\n" + m.warn().Render("! "+m.errMsg))
	}
	return b.String()
}

// playerLayout 决定牌桌上面那块玩家信息怎么摆。
type playerLayout int

const (
	playersNone       playerLayout = iota // 右侧面板里单独列，这里不画
	playersHorizontal                     // 横着一行，省高度
	playersVertical                       // 竖着排，窄而高的终端用
)

func (m *Model) viewTable(withLog bool, players playerLayout) string {
	var b strings.Builder
	b.WriteString(m.viewTurn())
	b.WriteString("\n")
	switch players {
	case playersHorizontal:
		b.WriteString(m.viewOpponents())
		b.WriteString("\n")
	case playersVertical:
		b.WriteString(strings.Join(m.playerLines(m.width), "\n"))
		b.WriteString("\n")
	}
	b.WriteString(m.viewTableCards())
	b.WriteString("\n\n")
	if m.choosing {
		// 选色时把手牌暂时藏起来，注意力留在那一行菜单上。
		b.WriteString(m.viewColorMenu())
	} else {
		b.WriteString(m.viewHand())
	}
	b.WriteString("\n")
	if m.width >= 60 && m.obs().Phase == engine.Playing {
		b.WriteString(m.dim().Render("R红 Y黄 G绿 B蓝 · S禁止 ↺反转 +2加二 W变色"))
		b.WriteString("\n")
	}
	b.WriteString(m.viewStatus())
	// 聊天菜单是模态的，和布局无关：双栏模式下也必须画出来，
	// 否则按 t 之后界面毫无反应，按键又被它吃掉，看起来像"不让你说话"。
	if m.chat {
		b.WriteString("\n")
		b.WriteString(m.viewChat())
	} else if withLog && m.height >= 20 && len(m.snap.Timeline) > 0 {
		b.WriteString("\n")
		b.WriteString(m.viewLog())
	}
	return b.String()
}

// playerLines 是竖排的玩家列表：`> 名字  7`，当前回合用 > 标出。
// 右侧面板和"窄而高"的单栏模式共用这一份。
func (m *Model) playerLines(width int) []string {
	obs := m.obs()
	lines := []string{m.dim().Render("玩家")}

	for i := 0; i < len(m.snap.Players); i++ {
		p := m.snap.Players[i]
		count := 0
		if i < len(obs.HandCounts) {
			count = obs.HandCounts[i]
		}

		marker, style := "  ", m.dim()
		if i == obs.Turn && obs.Phase == engine.Playing {
			marker, style = "> ", m.turn()
		}

		line := marker + ui.PadRight(m.name(i), 8) + fmt.Sprintf("%2d", count)
		if count == 1 {
			line += " UNO"
		}
		if !p.Connected {
			line += " 掉线"
		}
		lines = append(lines, style.Render(ui.CutWidth(line, width)))
	}

	dir := "↓"
	if obs.Dir < 0 {
		dir = "↑"
	}
	return append(lines, m.dim().Render("方向 "+dir))
}

// logLines 是出牌记录，最多放 height 行（含标题）。
func (m *Model) logLines(width, height int) []string {
	lines := []string{m.dim().Render("出牌记录")}
	if height <= 1 {
		return lines
	}
	items := ui.TailN(m.snap.Timeline, height-1)
	if len(items) == 0 {
		return append(lines, m.dim().Render("（还没有记录）"))
	}
	for i, e := range items {
		ts := fmt.Sprintf("[%02d:%02d] ", e.AtMS/60000, (e.AtMS/1000)%60)
		line := ui.CutWidth(ts+m.entryText(e), width)
		if i == len(items)-1 {
			// 最新一条不加暗色，眼睛能立刻抓到刚刚发生了什么。
			lines = append(lines, m.plain().Render(line))
		} else {
			lines = append(lines, m.dim().Render(line))
		}
	}
	return lines
}

// renderPanel 把若干行包成一个带左边框的竖栏，并补到指定高度。
func (m *Model) renderPanel(lines []string, width, height int) string {
	out := make([]string, 0, height)
	out = append(out, lines...)
	if len(out) > height {
		out = out[:height]
	}
	for len(out) < height {
		out = append(out, "")
	}
	return m.panelBorder().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		PaddingLeft(2).
		Width(width - 3).
		Render(strings.Join(out, "\n"))
}

// viewSide 是两栏布局右侧那一栏：玩家 / 出牌记录 / 按键表竖着码。
//
// 高度不够时按优先级从下往上砍：玩家 > 方向 > 出牌记录 > 按键表。
func (m *Model) viewSide(width, height int) string {
	content := width - 3
	lines := m.playerLines(content)

	rest := height - len(lines)
	keys := m.keyLines()
	showKeys := rest >= len(keys)+4
	if showKeys {
		rest -= len(keys)
	}

	if rest >= 2 {
		if rest >= 6 {
			lines = append(lines, "")
			rest--
		}
		logs := m.logLines(content, rest)
		lines = append(lines, logs...)
	}
	if showKeys {
		lines = append(lines, keys...)
	}
	return m.renderPanel(lines, width, height)
}

// keyLines 是右下角常驻的按键表。双栏模式左侧只有 50 列，底部那行提示
// 会被压成"u喊"，"UNO"三个字直接没了——完整键位得单独给块地方。
func (m *Model) keyLines() []string {
	return []string{
		"",
		m.dim().Render("按键"),
		m.dim().Render("1-9 选牌  ⏎ 出牌"),
		m.dim().Render("d 摸牌  p 跳过"),
		m.dim().Render("u 喊UNO  c 抓人"),
		m.dim().Render("t 快捷语  v 重开"),
		m.dim().Render("m 颜色深浅  q 退出"),
		m.dim().Render("Ctrl+B 老板键"),
	}
}

// tailN 取最后 n 个元素。
func tailN[T any](xs []T, n int) []T {
	if n <= 0 {
		return nil
	}
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

// padRight 按显示宽度补空格（中文占 2 格）。

// viewTurn 是"现在到底谁在出牌"那一行。之前只有一个小小的 ▶ 挂在名字前面，
// 对手区、手牌区各一个，反而看不出重点。现在单独一行写清楚。
func (m *Model) viewTurn() string {
	obs := m.obs()

	var left string
	switch {
	case obs.Phase != engine.Playing:
		left = m.ok().Render("本局结束")
	case obs.Turn == m.snap.MySeat:
		left = m.ok().Render("▶ 轮到你出牌")
	default:
		left = m.turn().Render("▶ " + m.name(obs.Turn) + " 正在出牌…")
	}
	right := m.dim().Render("房间 " + m.snap.Code)
	if m.layout == layoutCompact {
		// 紧凑模式没有玩家列表，方向只能挂在这儿。
		dir := "↻"
		if obs.Dir < 0 {
			dir = "↺"
		}
		right = m.dim().Render("方向 " + dir)
	}
	return ui.PadBetween(left, right, m.width)
}

func (m *Model) viewOpponents() string {
	var parts []string
	for i := 0; i < m.obs().SeatCount; i++ {
		if i == m.snap.MySeat {
			continue
		}
		n := 0
		if i < len(m.obs().HandCounts) {
			n = m.obs().HandCounts[i]
		}
		line := fmt.Sprintf("%s %s %d", m.name(i), bar(n), n)
		if n == 1 {
			line += " UNO!"
		}
		if i < len(m.snap.Players) && !m.snap.Players[i].Connected {
			line += " 掉线"
		}
		if m.obs().Turn == i && m.obs().Phase == engine.Playing {
			parts = append(parts, m.turn().Render("▶ "+line))
		} else {
			parts = append(parts, m.dim().Render("  "+line))
		}
	}
	return ui.JoinWrapped(parts, m.width)
}

func (m *Model) viewTableCards() string {
	obs := m.obs()
	line := "场上 " + m.renderCard(obs.Top, cardFace(obs.Top), false)
	line += m.dim().Render(fmt.Sprintf("  牌堆 %d", obs.DeckCount))
	line += "  当前色 " + m.colorChip(obs.Active)
	if obs.Pending > 0 {
		line += m.warn().Render(fmt.Sprintf("  罚牌 %d 张", obs.Pending))
	}
	return line
}

func (m *Model) viewHand() string {
	obs := m.obs()
	hand := obs.Hand
	header := fmt.Sprintf("你的手牌 %d 张", len(hand))
	if obs.Turn == m.snap.MySeat && obs.Phase == engine.Playing {
		header = m.turn().Render("▶ " + header + " —— 轮到你")
	} else {
		header = m.dim().Render(header)
	}
	if len(hand) == 0 {
		return header
	}

	cards := make([]string, 0, len(hand))
	for i, c := range hand {
		// 序号放在色块外面：色块里再塞数字，颜色和数字糊在一起很难扫。
		idx := fmt.Sprintf("%2d:", i+1)
		idxStyle := m.dim()
		if i == m.cursor {
			idxStyle = m.turn()
		}
		cards = append(cards, idxStyle.Render(idx)+m.renderCard(c, cardFace(c), i == m.cursor))
	}

	// 每张牌 = 3 格序号 + 6 格牌面 + 1 格间距
	perRow := 1
	if m.width > 0 {
		perRow = (m.width - 1) / 10
	}
	if perRow < 1 {
		perRow = 1
	}

	var rows []string
	for i := 0; i < len(cards); i += perRow {
		end := i + perRow
		if end > len(cards) {
			end = len(cards)
		}
		rows = append(rows, strings.Join(cards[i:end], ""))
	}
	return header + "\n" + strings.Join(rows, "\n")
}

func (m *Model) viewStatus() string {
	obs := m.obs()

	switch {
	case m.choosing:
		// 菜单本身画在手牌的位置上（见 viewColorMenu），这里只留取消提示。
		return m.dim().Render("esc 取消")

	case obs.UnoPending == m.snap.MySeat:
		secs := (m.snap.PendingLeftMS + 999) / 1000
		if secs < 0 {
			secs = 0
		}
		return m.warn().Render(fmt.Sprintf("你只剩一张牌！按 u 喊 UNO（还剩 %d 秒）", secs))

	case obs.Challenge != nil && obs.Challenge.By == m.snap.MySeat:
		return m.warn().Render("对方打出 [*|+4]！") +
			"   c 质疑（他手里还有同色牌就算耍赖）  ·  Enter 认罚摸 4 张"

	case obs.Challenge != nil:
		return m.dim().Render(fmt.Sprintf("等 %s 决定要不要质疑这个 +4…", m.name(obs.Challenge.By)))

	case obs.Phase == engine.MatchOver:
		return m.ok().Render(fmt.Sprintf("%s 达到 %d 分，赢下整场！", m.name(obs.MatchWinner), obs.MatchTo)) +
			m.dim().Render("   s 再来一局 / q 退出")

	case obs.Phase == engine.RoundOver:
		w := obs.RoundWinner
		line := m.ok().Render(fmt.Sprintf("%s 打完手牌！", m.name(w)))
		if w >= 0 && w < len(obs.Scores) {
			line += fmt.Sprintf("本局 +%d 分", obs.Scores[w])
		}
		if obs.MatchTo > 0 && w >= 0 && w < len(obs.Scores) {
			line += m.dim().Render(fmt.Sprintf("（累计 %d/%d）", obs.Scores[w], obs.MatchTo))
			line += m.dim().Render("   s 下一局")
		} else {
			line += m.dim().Render("   s 再来一局")
		}
		return line + m.dim().Render(" · v 重开投票 · q 退出")

	case obs.Turn != m.snap.MySeat:
		if m.width < 54 {
			return m.dim().Render("等对方出牌… t 快捷语 q 退出")
		}
		return m.dim().Render("等对方出牌…   t 快捷语 · v 重开投票 · q 退出")
	}

	narrow := m.width < 54
	hint := "数字选牌 · Enter 出 · d 摸 · p 跳过 · u 喊UNO · t 快捷语 · q 退出"
	if narrow {
		// 窄屏也别把 UNO 三个字砍掉——"u喊"没人看得懂。
		hint = "1-9选 回车出 d摸 p过 u喊UNO q退"
	}
	if obs.DrawnID >= 0 {
		if narrow {
			hint = "摸到能出的牌：回车打出 / p 留着"
		} else {
			hint = "刚摸到一张能出的牌：Enter 打出，或 p 留着跳过"
		}
	}
	if m.errMsg != "" {
		return m.warn().Render("! "+m.errMsg) + "\n" + m.dim().Render(hint)
	}
	if m.snap.Vote != nil {
		hint = fmt.Sprintf("重开投票 %d/%d（v 同意）  ", m.snap.Vote.Yes, m.snap.Vote.Need) + hint
	}
	return m.dim().Render(hint)
}

// viewColorMenu 是打变色牌时占掉手牌那几行的一行内联菜单。
func (m *Model) viewColorMenu() string {
	return m.turn().Render("打出变色牌，选颜色： ") +
		m.renderCard(engine.Card{Color: engine.Red}, "1 红", false) +
		m.renderCard(engine.Card{Color: engine.Yellow}, "2 黄", false) +
		m.renderCard(engine.Card{Color: engine.Blue}, "3 蓝", false) +
		m.renderCard(engine.Card{Color: engine.Green}, "4 绿", false)
}

func (m *Model) viewChat() string {
	var b strings.Builder
	b.WriteString(m.dim().Render("快捷语（数字选择，esc 取消）") + "\n")
	for i, p := range client.Presets {
		b.WriteString(fmt.Sprintf("  %d %s\n", i+1, p))
	}
	return strings.TrimRight(b.String(), "\n")
}

// entryText 是一行战报的纯文本形式：`名字 出牌 [R|1]` / `名字：好牌`。
// 记录区里不铺背景色——时间戳和牌面挤在一起时，色块反而更难读。
func (m *Model) entryText(e client.TimelineEntry) string {
	if e.Seat < 0 {
		return e.Text
	}
	name := ui.CutWidth(m.name(e.Seat), 6)
	// 牌面是 UNO 自己塞进 Detail 的；别的游戏可能会放别的东西，
	// 断言失败就当普通文本渲染，绝不能因为它崩掉整个界面。
	card, hasCard := e.Detail.(*engine.Card)
	if e.Chat {
		return name + "：" + e.Text
	}
	if hasCard && card != nil {
		return name + " 出牌 " + cardFace(*card) + e.Text
	}
	if e.Text == "" {
		return name
	}
	return name + " " + e.Text
}

func (m *Model) viewLog() string {
	items := ui.TailN(m.snap.Timeline, 4)
	var lines []string
	for _, e := range items {
		lines = append(lines, m.entryText(e))
	}
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == len(lines)-1 {
			out = append(out, m.plain().Render("  "+l))
			continue
		}
		out = append(out, m.dim().Render("  "+l))
	}
	return strings.Join(out, "\n")
}

// cutWidth 按显示宽度截断（中文算 2 格），防止窄面板被撑开换行。

func bar(n int) string {
	if n > 12 {
		n = 12
	}
	if n < 0 {
		n = 0
	}
	return strings.Repeat("▮", n)
}

// padBetween 把 left 和 right 分列一行两端。

// joinWrapped 按终端宽度把若干片段折行，避免窄终端下被终端自己硬折。
