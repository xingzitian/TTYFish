package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// View 是整帧入口：老板键 → 断线 → 帮助 → 极简/丰富。
func (m *Model) View() string {
	scope := fmt.Sprintf("%t/%t/%t/%t/%d/%v/%d/%d", m.boss, m.help, m.snap.Kicked, m.closed, m.snap.Phase, m.obs().Phase, m.step, m.opts.Layout)
	if scope != m.frameScope {
		m.frameOffset, m.frameLast, m.frameScope = 0, 0, scope
	}
	if m.boss {
		m.frameOffset, m.frameLast = 0, 0
		return ui.ClampFrame("$ go test ./...\nok   ttyfish/explodingkittens (cached)\n$ ", m.width, m.height)
	}
	if m.snap.Kicked {
		m.frameOffset, m.frameLast = 0, 0
		return ui.ClampFrame(m.warn().Render("你已在另一台设备登录，这个连接被顶掉了。"), m.width, m.height)
	}
	if m.closed {
		m.frameOffset, m.frameLast = 0, 0
		return ui.ClampFrame(m.dim().Render("连接已断开。重新 ssh 上来还能回到刚才的房间。"), m.width, m.height)
	}
	if m.help {
		m.frameOffset, m.frameLast = 0, 0
		return ui.ClampFrame(m.viewHelp(), m.width, m.height)
	}
	if m.step == stepChat {
		// The shortcut menu owns the keyboard; keep every choice visible even
		// when the underlying table or lobby would exhaust the terminal height.
		return m.frameView(m.cmdLine(m.width))
	}
	if !m.inGame() {
		return m.frameView(m.viewLobby())
	}
	if m.opts.Layout == ui.LayoutCompact {
		return m.frameView(m.viewCompact())
	}
	return m.frameView(m.viewRich())
}

func (m *Model) frameView(content string) string {
	view, last := ui.FrameViewport(content, m.width, m.height, m.frameOffset)
	m.frameLast = last
	m.frameOffset = min(m.frameOffset, last)
	return view
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }

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

// viewRich 是宽屏：左边牌桌，右边玩家与记录。
func (m *Model) viewRich() string {
	if m.width < 100 {
		return m.viewCompact()
	}
	panelW := m.width / 3
	if panelW < 26 {
		panelW = 26
	}
	if panelW > 38 {
		panelW = 38
	}
	leftW := m.width - panelW - 2

	left := []string{m.compactTurn(leftW), m.compactDeck(leftW)}
	left = append(left, m.compactHand(leftW)...)
	if m.showPlayers() {
		left = append(left, m.compactPlayers(leftW)...)
	}
	left = append(left, m.statusLine(leftW), m.cmdLine(leftW))
	left = strings.Split(strings.Join(left, "\n"), "\n")
	for i, line := range left {
		left[i] = ui.PadRight(line, leftW)
	}

	h := len(left)
	if m.height > h {
		h = m.height
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(left, "\n"), "  ", m.sidePanel(panelW, h))
}

func (m *Model) sidePanel(width, height int) string {
	lines := m.playerLines(width - 2)
	rest := height - len(lines)
	if rest >= 3 {
		lines = append(lines, "")
		rest--
		lines = append(lines, m.logLines(width-2, rest)...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = ui.PadRight(ansi.Truncate(line, width-2, ""), width-2)
	}
	box := strings.Join(lines, "\n")
	return m.dim().Render("│ ") + strings.ReplaceAll(box, "\n", "\n"+m.dim().Render("│ "))
}

// statusLine 是 [MSG] 行：当前该干什么，或者刚刚发生了什么。
func (m *Model) statusLine(width int) string {
	if m.errVisible() {
		return ansi.Hardwrap(m.warn().Render("[MSG] "+m.errMsg), max(1, width), true)
	}
	return ansi.Hardwrap(m.dim().Render("[MSG] ")+m.hint(), max(1, width), true)
}

// hint 按"阶段 + 选择进度"只说一件事。
func (m *Model) hint() string {
	if !m.inGame() {
		if !m.snap.IsHost {
			return "等待房主开始"
		}
		return strings.Join(ui.ActionTokens(ui.SelectActions(lobbyActions, "s", "b", "v")), " · ")
	}
	o := m.obs()
	if !m.alive() {
		return "你已淘汰，可以观战或按 " + ui.EscapeAction("").DisplayKey() + " 离开"
	}
	if m.step == stepChat {
		return "选择快捷语，按对应数字发送"
	}

	// 预言结果展示优先：这是限时信息
	if left := m.revealLeft(); left >= 0 {
		if len(m.revealed) > 0 {
			var parts []string
			for i, c := range m.revealed {
				label := m.token(c.Kind)
				if c.Kind == engine.ExplodingKitten {
					label = m.warn().Underline(true).Render("【炸弹】")
				}
				parts = append(parts, fmt.Sprintf("%d.%s", i+1, label))
			}
			return fmt.Sprintf("预言结果: %s (%ds后结束)", strings.Join(parts, " -> "), left)
		}
		return fmt.Sprintf("%s 正在偷看牌堆顶...(%ds)", m.seatName(m.revealBy), left)
	}

	switch o.Phase {
	case engine.Responding:
		if o.Pending != nil {
			actor := m.seatName(o.Pending.Action.Seat)
			if o.Pending.Action.Seat == o.Seat {
				actor = "你"
			}
			state := "生效倒计时"
			if o.Pending.Nopes%2 == 1 {
				state = "已被否决，等待反否决"
			} else if o.Pending.Nopes > 0 {
				state = "否决被反制，原效果将恢复"
			}
			response := "等待其他玩家"
			if m.canNope() {
				for _, action := range ui.SelectActions(responseActions, "n") {
					response = action.Token() + " / 反否决"
				}
			}
			return fmt.Sprintf("%s 的 %s：%s %s (%s)",
				actor, m.token(o.Pending.Kind), state, fmtSeconds(m.deadline), response)
		}
		return "等待响应…"
	case engine.Giving:
		if o.Pending != nil && o.Pending.Action.Target == o.Seat {
			return fmt.Sprintf("%s 向你索要一张牌：按数字选一张交出去",
				m.seatName(o.Pending.Action.Seat))
		}
		if o.Pending != nil {
			return fmt.Sprintf("等 %s 交牌…", m.seatName(o.Pending.Action.Target))
		}
		return "等待交牌…"
	case engine.Reinserting:
		if o.Turn != o.Seat {
			return fmt.Sprintf("等待 %s 拆弹并秘密放回炸弹", m.seatName(o.Turn))
		}
		if m.step == stepDefuseCustom {
			keys := ui.CombinedKeyHint("", ui.SelectActions(customPositionActions, "previous", "next")...).DisplayKey()
			confirm := ""
			for _, action := range ui.SelectActions(customPositionActions, "confirm") {
				confirm = action.DisplayKey()
			}
			return fmt.Sprintf("埋雷位置: 第 %d 张（位置 %d），%s 调整，%s 确认", m.pos+1, m.pos, keys, confirm)
		}
		return "摸到【爆炸猫】！请选择秘密放回位置，确认时自动使用拆弹牌："
	}

	if m.step == stepTarget {
		if m.pairDraw {
			return "请选择你要盲抽的目标玩家（数字直选）："
		}
		return "请选择你要索要手牌的目标玩家（数字直选）："
	}
	if n := len(m.snap.Timeline); n > 0 {
		e := m.snap.Timeline[n-1]
		if e.Text != "" {
			return m.seatName(e.Seat) + " " + e.Text
		}
	}
	if !m.myTurn() {
		return fmt.Sprintf("等 %s 出牌…", m.seatName(o.Turn))
	}
	for _, action := range ui.SelectActions(playActions, "d") {
		return "轮到你：按数字出功能牌，或按 " + action.DisplayKey() + " 摸牌"
	}
	return "轮到你出牌"
}

// cmdLine 是 [CMD]：分阶段的按键表 / 选择器。
//
// 超宽时按完整操作项折行，不隐藏仍可使用的键。
func (m *Model) cmdLine(width int) string {
	return strings.Join(m.commandRows(width), "\n")
}

// commandActions returns the current contextual operations and their prefix.
func (m *Model) commandActions() (actions []ui.Action, prefix string) {
	defer func() {
		activeCommon := commonActions
		var extra []ui.Action
		if m.step == stepChat {
			activeCommon = ui.SelectActions(commonActions, "quit")
		} else if m.obs().Phase == engine.Reinserting && m.alive() && m.obs().Seat == m.obs().Turn {
			activeCommon = nil
			for _, action := range commonActions {
				if action.ID != "layout" {
					activeCommon = append(activeCommon, action)
				}
			}
			if m.step == stepDefuseCustom {
				extra = ui.SelectActions(reinsertActions, "i")
			}
		}
		actions = ui.CheckedActions("kittens", actions, activeCommon, extra, []ui.Action{ui.BossAction(), ui.InterruptAction()}, ui.FrameScrollActions())
	}()
	o := m.obs()
	if m.step == stepChat {
		return append(chatActions(), ui.EscapeAction("取消")), ">> 快捷语: "
	}

	// 选目标：整行变成选择器
	if m.step == stepTarget {
		return m.targetMenuActions(), ">> 选择目标: "
	}

	// 埋雷：整行变成位置选择器
	if o.Phase == engine.Reinserting && m.alive() && o.Seat == o.Turn {
		if m.step == stepDefuseCustom {
			return append([]ui.Action{{Label: fmt.Sprintf("[%d]", m.pos)}}, customPositionActions...),
				">> 埋雷位置: "
		}
		actions := append([]ui.Action(nil), reinsertActions...)
		for i := range actions {
			if actions[i].ID == "i" {
				actions[i].Label = fmt.Sprintf("%s(0-%d)", actions[i].Label, o.DeckCount)
			}
		}
		return actions, ">> 埋雷位置: "
	}

	actions = ui.SelectActions(commonActions, "help")
	if !m.inGame() {
		if m.snap.IsHost {
			actions = append(actions, ui.SelectActions(lobbyActions, "s", "b", "dlc")...)
		} else {
			actions = append(actions, ui.Action{Label: "等待房主开始"})
		}
		actions = append(actions, ui.SelectActions(lobbyActions, "v")...)
		actions = append(actions, ui.SelectActions(commonActions, "chat", "layout")...)
		actions = append(actions, ui.EscapeAction("离开"))
		return actions, "[CMD] "
	}
	if !m.alive() {
		return append(ui.SelectActions(commonActions, "help", "players"), ui.EscapeAction("离开")), "[CMD] "
	}

	switch o.Phase {
	case engine.Responding:
		if m.canNope() {
			for _, action := range ui.SelectActions(responseActions, "n") {
				action.Label = fmt.Sprintf("%s(%d)", action.Label, m.nopeCount())
				actions = append(actions, action)
			}
		}
		if !m.passed() && m.passSent != responseKey(o) {
			actions = append(actions, ui.SelectActions(responseActions, "p")...)
		}
		return append(actions, ui.SelectActions(commonActions, "chat", "players")...), "[CMD] "
	case engine.Giving:
		if o.Pending != nil && o.Pending.Action.Target == o.Seat {
			give := cardAction
			give.Label = "交牌"
			actions = append(actions, give, ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "previous", "next")...))
			return append(actions, ui.SelectActions(commonActions, "chat", "players")...), "[CMD] "
		}
	}
	if !m.myTurn() {
		return append(ui.SelectActions(commonActions, "help", "chat", "players"), ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "previous", "next")...)), "[CMD] "
	}
	_, pairs := m.pairKind()
	actions = append(ui.SelectActions(commonActions, "help"), cardAction)
	play := append([]ui.Action(nil), playActions...)
	for i := range play {
		if play[i].ID == "g" {
			play[i].Label = fmt.Sprintf("%s(%d)", play[i].Label, pairs)
		}
	}
	actions = append(actions, play...)
	if len(m.revealed) > 0 && m.revealLeft() >= 0 {
		actions = append(actions, revealAction)
	}
	actions = append(actions, ui.SelectActions(commonActions, "chat", "players")...)
	return append(actions, ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "previous", "next")...)), "[CMD] "
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
		if m.inGame() && p.Seat == o.Turn {
			line = m.turn().Render(ui.CutWidth("*"+line+"*", width))
		} else if p.Seat == m.snap.MySeat {
			line = m.ok().Render(line)
		} else {
			line = m.dim().Render(line)
		}
		lines = append(lines, ansi.Truncate(line, width, ""))
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
