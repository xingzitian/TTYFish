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
	if !m.inGame() {
		return ui.ClampFrame(m.viewLobby(), m.width, m.height)
	}
	if m.opts.Layout == ui.LayoutCompact {
		return ui.ClampFrame(m.viewCompact(), m.width, m.height)
	}
	return ui.ClampFrame(m.viewRich(), m.width, m.height)
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
		return m.warn().Render("[MSG] " + ui.CutWidth(m.errMsg, max(10, width-6)))
	}
	return m.dim().Render("[MSG] ") + ansi.Truncate(m.hint(), max(1, width-6), "")
}

// hint 按"阶段 + 选择进度"只说一件事。
func (m *Model) hint() string {
	if !m.inGame() {
		return "按 S 开局（房主）· B 加电脑 · V 投票重开"
	}
	o := m.obs()
	if !m.alive() {
		return "你已淘汰，可以观战或按 Esc 离开"
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
				response = "[N]反否决 / 否决"
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
			return fmt.Sprintf("埋雷位置: 第 %d 张（位置 %d），-= 调整，Enter 确认", m.pos+1, m.pos)
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
	return "轮到你：按数字出功能牌，或按 D 摸牌"
}

// cmdLine 是 [CMD]：分阶段的按键表 / 选择器。
//
// 超宽时按完整操作项折行，不隐藏仍可使用的键。
func (m *Model) cmdLine(width int) string {
	return m.dim().Render(strings.Join(m.commandRows(width), "\n"))
}

// cmdTokens 返回 [CMD] 行里的按键项和前缀。
func (m *Model) cmdTokens() ([]string, string) {
	o := m.obs()
	if m.step == stepChat {
		return m.chatTokens(), ">> 快捷语: "
	}

	// 选目标：整行变成选择器
	if m.step == stepTarget {
		return m.targetTokens(), ">> 选择目标: "
	}

	// 埋雷：整行变成位置选择器
	if o.Phase == engine.Reinserting && m.alive() && o.Seat == o.Turn {
		if m.step == stepDefuseCustom {
			return []string{fmt.Sprintf("[%d]", m.pos), "[-=]调整", "[Enter]确认", "[Esc]返回"},
				">> 埋雷位置: "
		}
		return []string{
			"[0]牌堆顶", "[1]第2张", "[2]第3张",
			"[B]牌堆底", "[R]随机", fmt.Sprintf("[I]指定(0-%d)", o.DeckCount),
		}, ">> 埋雷位置: "
	}

	tokens := []string{"[?]帮助"}
	if !m.inGame() {
		if m.snap.IsHost {
			tokens = append(tokens, "[S]开始", "[B]电脑")
		} else {
			tokens = append(tokens, "等待房主开始")
		}
		tokens = append(tokens, "[V]投票", "[T]聊天", "[I]界面", "[Esc]离开")
		return tokens, "[CMD] "
	}
	if !m.alive() {
		return []string{"[?]帮助", "[Tab]玩家", "[Esc]离开"}, "[CMD] "
	}

	switch o.Phase {
	case engine.Responding:
		if m.canNope() {
			tokens = append(tokens, fmt.Sprintf("[N]否决(%d)", m.nopeCount()))
		}
		if !m.passed() && m.passSent != responseKey(o) {
			tokens = append(tokens, "[P]放弃")
		}
		return append(tokens, "[T]聊天", "[Tab]玩家"), "[CMD] "
	case engine.Giving:
		if o.Pending != nil && o.Pending.Action.Target == o.Seat {
			return []string{"[?]帮助", "[1-5]交牌", "[-=]翻页", "[T]聊天", "[Tab]玩家"}, "[CMD] "
		}
	}
	if !m.myTurn() {
		return []string{"[?]帮助", "[T]聊天", "[Tab]玩家", "[-=]翻页"}, "[CMD] "
	}
	_, pairs := m.pairKind()
	tokens = []string{
		"[?]帮助", "[1-5]出牌", fmt.Sprintf("[G]对子盲抽(%d)", pairs),
		"[D]摸牌",
	}
	if len(m.revealed) > 0 && m.revealLeft() >= 0 {
		tokens = append(tokens, "[P]结束预言")
	}
	return append(tokens, "[T]聊天", "[Tab]玩家", "[-=]翻页"), "[CMD] "
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
