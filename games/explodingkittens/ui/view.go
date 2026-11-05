package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// View 是整帧入口：老板键 → 断线 → 帮助 → 大厅/牌桌。
func (m *Model) View() string {
	if m.boss {
		return ui.ClampFrame("$ go test ./...\nok   ttyfish/explodingkittens (cached)\n$ ", m.width, m.height)
	}
	if m.snap.Kicked {
		return ui.ClampFrame(m.warn().Render("你已在另一台设备登录，这个连接被顶掉了。"), m.width, m.height)
	}
	if m.closed {
		return ui.ClampFrame(m.dim().Render("连接已断开。重新 ssh 上来还能回到刚才的房间。"), m.width, m.height)
	}
	if m.help {
		return ui.ClampFrame(m.viewHelp(), m.width, m.height)
	}
	if m.opts.Layout == ui.LayoutCompact {
		return ui.ClampFrame(m.viewCompact(), m.width, m.height)
	}
	return ui.ClampFrame(m.viewRich(), m.width, m.height)
}

func (m *Model) dim() lipgloss.Style {
	s := m.renderer.NewStyle()
	if m.opts.Mode == ui.ColorNone {
		return s.Faint(true)
	}
	return s.Foreground(lipgloss.Color("244"))
}

func (m *Model) turn() lipgloss.Style {
	s := m.renderer.NewStyle().Bold(true)
	if m.opts.Mode == ui.ColorNone {
		return s.Underline(true)
	}
	return s.Foreground(lipgloss.Color("226"))
}

func (m *Model) warn() lipgloss.Style {
	s := m.renderer.NewStyle().Bold(true)
	if m.opts.Mode == ui.ColorNone {
		return s.Underline(true)
	}
	return s.Foreground(lipgloss.Color("203"))
}

func (m *Model) ok() lipgloss.Style {
	s := m.renderer.NewStyle()
	if m.opts.Mode == ui.ColorNone {
		return s
	}
	return s.Foreground(lipgloss.Color("119"))
}

// viewRich 是宽屏的三栏：左边牌桌 + 手牌，右边玩家、记录、按键入口。
func (m *Model) viewRich() string {
	panelW := m.width / 3
	if panelW < 26 {
		panelW = 26
	}
	if panelW > 38 {
		panelW = 38
	}
	leftW := m.width - panelW - 2
	if leftW > 80 {
		leftW = 80
	}

	left := []string{
		m.richHeader(leftW),
		m.pileLine(leftW),
		"",
		m.handHeader(leftW),
	}
	left = append(left, m.handLines(leftW)...)
	left = append(left, "", m.statusLine(leftW), m.cmdLine(leftW))

	h := len(left)
	if m.height > h {
		h = m.height
	}
	right := m.sidePanel(panelW, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(left, "\n"), "  ", right)
}

func (m *Model) sidePanel(width, height int) string {
	lines := m.playerLines(width - 2)
	rest := height - len(lines)
	if rest >= 3 {
		lines = append(lines, "")
		rest--
		logs := m.logLines(width-2, rest)
		lines = append(lines, logs...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = ui.PadRight(ui.CutWidth(line, width-1), width-1)
	}
	box := strings.Join(lines, "\n")
	return m.dim().Render("│ ") + strings.ReplaceAll(box, "\n", "\n"+m.dim().Render("│ "))
}

func (m *Model) richHeader(width int) string {
	title := "爆炸猫"
	if m.snap.Code != "" {
		title += "  房间 " + m.snap.Code
	}
	right := m.phaseTitle()
	pad := width - lipgloss.Width(title) - lipgloss.Width(right)
	if pad < 1 {
		return m.dim().Render(ui.CutWidth(title, width))
	}
	return m.turn().Render(title) + strings.Repeat(" ", pad) + m.dim().Render(right)
}

// phaseTitle 是右上角那句"现在是什么阶段"。
func (m *Model) phaseTitle() string {
	if !m.inGame() {
		return "大厅"
	}
	o := m.obs()
	switch o.Phase {
	case engine.Responding:
		return "响应中"
	case engine.Giving:
		return "交牌中"
	case engine.Reinserting:
		return "拆弹中"
	}
	if o.Turn == o.Seat {
		return "轮到你"
	}
	return "等对手"
}

// pileLine 是"场上"那一行：弃牌堆顶、牌堆剩余、回合数。
func (m *Model) pileLine(width int) string {
	o := m.obs()
	var b strings.Builder
	b.WriteString(m.dim().Render("弃牌 "))
	if len(o.Discard) > 0 {
		b.WriteString(m.token(o.Discard[len(o.Discard)-1].Kind))
	} else {
		b.WriteString(m.dim().Render("（空）"))
	}
	b.WriteString(m.dim().Render(fmt.Sprintf("   牌堆 %d   本回合剩 %d", o.DeckCount, o.TurnsRemaining)))
	if o.UnderAttack {
		b.WriteString(m.warn().Render("   被攻击"))
	}
	if len(o.Future) > 0 {
		var parts []string
		for _, c := range o.Future {
			parts = append(parts, m.token(c.Kind))
		}
		b.WriteString(m.dim().Render("   牌顶: ") + strings.Join(parts, " "))
	}
	return ui.CutWidth(b.String(), width)
}

func (m *Model) handHeader(width int) string {
	// 正在选目标/选牌种时，这一块不是手牌，标题也要跟着换
	switch m.step {
	case stepTarget:
		return m.turn().Render("▶ 选一个目标 —— 按数字，Enter 确认")
	case stepRequest:
		return m.turn().Render("▶ 三张组合：选要索要的牌种")
	}

	page, pages := m.pageCards()
	text := fmt.Sprintf("你的手牌 %d 张", len(m.obs().Hand))
	if pages > 1 {
		text += fmt.Sprintf("  → %d/%d 页", m.page+1, pages)
	}
	_ = page
	if m.myTurn() {
		return m.turn().Render("▶ " + text + " —— 轮到你")
	}
	return m.dim().Render(text)
}

// handLines 每行若干张，牌面对齐：`1:[橙|橘]`。
func (m *Model) handLines(width int) []string {
	if opts := m.optionTokens(width); opts != "" {
		return []string{opts}
	}
	page, _ := m.pageCards()
	if len(page) == 0 {
		return []string{m.dim().Render("（没有手牌）")}
	}
	cell := 10
	perRow := width / cell
	if perRow < 1 {
		perRow = 1
	}
	var rows []string
	for i := 0; i < len(page); i += perRow {
		end := i + perRow
		if end > len(page) {
			end = len(page)
		}
		var parts []string
		for j := i; j < end; j++ {
			tok := m.dim().Render(fmt.Sprintf("%2d:", j+1)) + m.token(page[j].Kind)
			if m.isPicked(page[j].ID) {
				tok = m.turn().Render("▸") + tok
			}
			parts = append(parts, tok)
		}
		rows = append(rows, strings.Join(parts, " "))
	}
	return rows
}

// optionTokens 在"选目标 / 选牌种"时列出所有可选项，
// 这样玩家不用猜数字对应谁——和 UNO 选颜色时把手牌换成色块是一个思路。
func (m *Model) optionTokens(width int) string {
	o := m.obs()
	switch m.step {
	case stepTarget:
		var parts []string
		for seat := range o.Alive {
			if seat == o.Seat || !o.Alive[seat] || o.HandCounts[seat] == 0 {
				continue
			}
			label := fmt.Sprintf("%d.%s(%d)", seat+1, m.seatName(seat), o.HandCounts[seat])
			if seat == m.target {
				label = m.turn().Render("▸" + label + "◂")
			}
			parts = append(parts, label)
		}
		head := m.dim().Render("[HAND] 选目标 ")
		line := head + strings.Join(parts, " ")
		if lipgloss.Width(line) > width {
			line = ui.CutWidth(line, width)
		}
		return line
	case stepRequest:
		page, pages := m.requestPage()
		var parts []string
		for i, kind := range page {
			parts = append(parts, fmt.Sprintf("%d.%s", i+1, m.token(kind)))
		}
		if pages > 1 {
			parts = append(parts, fmt.Sprintf("(%d/%d 页，-= 翻页)", m.page+1, pages))
		}
		head := m.dim().Render("[HAND] 要哪张 ")
		line := head + strings.Join(parts, " ")
		for lipgloss.Width(line) > width && len(parts) > 1 {
			parts = parts[:len(parts)-1]
			line = head + strings.Join(parts, " ")
		}
		return line
	}
	return ""
}

// statusLine 是"[MSG] 现在该干什么"。每个阶段只说一件事。
func (m *Model) statusLine(width int) string {
	if m.errVisible() {
		return m.warn().Render("[MSG] " + ui.CutWidth(m.errMsg, max(10, width-6)))
	}
	return m.dim().Render("[MSG] ") + ui.CutWidth(m.hint(), max(10, width-6))
}

// hint 按"阶段 + 选择进度"给出一句话，避免同一屏出现两种解释。
func (m *Model) hint() string {
	if !m.inGame() {
		return "按 S 开局（房主）· B 加电脑 · V 投票重开"
	}
	o := m.obs()
	if !m.alive() {
		return "你已淘汰，可以观战或按 Esc 离开"
	}
	switch o.Phase {
	case engine.Responding:
		left := m.remaining()
		if o.Pending != nil && o.Seat < len(o.Pending.Passed) && o.Pending.Passed[o.Seat] {
			return fmt.Sprintf("已放弃响应，等其他人… (%d 秒)", left)
		}
		if o.Pending != nil {
			return fmt.Sprintf("有人打出 %s —— 要反制吗？(%d 秒)", m.name(o.Pending.Kind), left)
		}
		return "等待响应…"
	case engine.Giving:
		if o.Pending != nil && o.Pending.Action.Target == o.Seat {
			return "对方索要一张牌：按数字选一张交出去"
		}
		return "等对方交牌…"
	case engine.Reinserting:
		if m.step == stepDefusePos {
			return fmt.Sprintf("放回牌堆第 %d 张（0 = 牌顶），-= 调整，Enter 确认", m.pos)
		}
		return "你摸到爆炸猫！按数字选一张拆弹牌"
	}

	switch m.step {
	case stepCombo:
		return fmt.Sprintf("已选 %d 张%s：再按一张同色凑三张，或 Enter 出这些",
			len(m.picked), m.name(m.picked[0].Kind))
	case stepTarget:
		return "按数字选目标玩家，Enter 确认（Esc 取消）"
	case stepRequest:
		return "三张组合：按数字选要索要的牌种"
	}

	if !m.myTurn() {
		return fmt.Sprintf("等 %s 出牌…", m.seatName(o.Turn))
	}
	return "轮到你：按数字出牌，或按 D 摸一张"
}

// cmdLine 只列"这个阶段真的能按"的键。
func (m *Model) cmdLine(width int) string {
	tokens := m.cmdTokens()
	body := "[CMD] " + strings.Join(tokens, " ")
	for lipgloss.Width(body) > width && len(tokens) > 1 {
		tokens = tokens[:len(tokens)-1]
		body = "[CMD] " + strings.Join(tokens, " ")
	}
	return m.dim().Render(body)
}

func (m *Model) cmdTokens() []string {
	tokens := []string{"[?]帮助"}
	if !m.inGame() {
		if m.snap.IsHost {
			tokens = append(tokens, "[S]开始", "[B]电脑")
		}
		tokens = append(tokens, "[V]投票", "[I]界面")
		return tokens
	}
	o := m.obs()
	if !m.alive() {
		return append(tokens, "[Tab]玩家")
	}
	switch o.Phase {
	case engine.Responding:
		tokens = append(tokens, "[1-5]反制", "[P]放弃", "[Tab]玩家")
	case engine.Giving:
		tokens = append(tokens, "[1-5]交牌")
	case engine.Reinserting:
		if m.step == stepDefusePos {
			tokens = append(tokens, "[-=]位置", "[Enter]确认")
		} else {
			tokens = append(tokens, "[1-5]选拆弹")
		}
	default:
		switch m.step {
		case stepCombo:
			tokens = append(tokens, "[1-5]加一张", "[Enter]出这些", "[Esc]取消")
		case stepTarget:
			tokens = append(tokens, "[1-5]选目标", "[Enter]确认", "[Esc]取消")
		case stepRequest:
			tokens = append(tokens, "[1-9]选牌种", "[Esc]取消")
		default:
			if m.myTurn() {
				tokens = append(tokens, "[1-5]出牌", "[D]摸牌", "[-=]翻页", "[Tab]玩家")
			} else {
				tokens = append(tokens, "[Tab]玩家", "[-=]翻页")
			}
		}
	}
	return append(tokens, "[I]界面")
}

// playerLines 列出每个座位：名字、手牌数、是否淘汰。
func (m *Model) playerLines(width int) []string {
	lines := []string{m.dim().Render("玩家")}
	o := m.obs()
	for _, p := range m.snap.Players {
		marker := "  "
		if p.Seat == m.snap.MySeat {
			marker = "> "
		}
		name := ui.CutWidth(p.Name, 8)
		if name == "" {
			name = fmt.Sprintf("P%d", p.Seat+1)
		}
		status := ""
		if m.inGame() && p.Seat < len(o.Alive) {
			if !o.Alive[p.Seat] {
				status = " 淘汰"
			} else {
				status = fmt.Sprintf(" %d", o.HandCounts[p.Seat])
			}
		} else if !p.Connected {
			status = " 离线"
		}
		line := marker + name + status
		if p.Seat == o.Turn && m.inGame() {
			line = m.turn().Render(ui.CutWidth("*"+line+"*", width))
		} else if p.Seat == m.snap.MySeat {
			line = m.ok().Render(line)
		} else {
			line = m.dim().Render(line)
		}
		lines = append(lines, ui.CutWidth(line, width))
	}
	return lines
}

// logLines 是出牌记录，最新的在最下面。
func (m *Model) logLines(width, height int) []string {
	lines := []string{m.dim().Render("出牌记录")}
	if height <= 1 {
		return lines
	}
	items := ui.TailN(m.snap.Timeline, height-1)
	if len(items) == 0 {
		return append(lines, m.dim().Render("（还没有记录）"))
	}
	for _, e := range items {
		line := ui.CutWidth(fmt.Sprintf("P%d %s", e.Seat+1, e.Text), width)
		lines = append(lines, m.dim().Render(line))
	}
	return lines
}
