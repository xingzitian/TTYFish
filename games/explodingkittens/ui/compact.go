package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// viewCompact 是窄屏的六行版：TURN / PILE / HAND / MSG / CMD。
//
// 和 UNO 的极简模式一个思路：一行一件事，按键永远写在最后一行。
func (m *Model) viewCompact() string {
	width := m.width
	lines := []string{
		m.compactTurn(width),
		m.compactPile(width),
	}
	lines = append(lines, m.compactHand(width)...)
	lines = append(lines, m.statusLine(width), m.cmdLine(width))
	return strings.Join(lines, "\n")
}

// compactTurn 是"谁在对局、轮到谁"：当前玩家居中，左右各一位。
func (m *Model) compactTurn(width int) string {
	o := m.obs()
	if !m.inGame() {
		return m.turn().Render("[TURN] ") + m.dim().Render("等待开局")
	}

	arrow := "→"
	if o.Turn < 0 {
		arrow = "?"
	}
	tail := " | " + m.dim().Render(arrow)
	if m.showPlayers() {
		// Tab：列出所有座位，最多 5 个
		var parts []string
		for seat := range o.Alive {
			mark := m.seatName(seat)
			if !o.Alive[seat] {
				mark += "(亡)"
			} else {
				mark += fmt.Sprintf("(%d)", o.HandCounts[seat])
			}
			parts = append(parts, mark)
		}
		return m.turn().Render("[TURN] ") + ui.CutWidth(strings.Join(parts, " "), width-8)
	}

	seats := len(o.Alive)
	if seats == 0 {
		return m.turn().Render("[TURN] ") + m.dim().Render("等待开局")
	}
	var parts []string
	for _, offset := range []int{-1, 0, 1} {
		seat := ((o.Turn+offset)%seats + seats) % seats
		if !o.Alive[seat] {
			continue
		}
		mark := fmt.Sprintf("%s(%d)", m.seatName(seat), o.HandCounts[seat])
		if seat == o.Turn {
			mark = m.turn().Render("*" + mark + "*")
		}
		parts = append(parts, mark)
	}
	body := strings.Join(parts, " -> ")
	if lipgloss.Width("[TURN] "+body+tail) > width {
		tail = ""
	}
	return m.turn().Render("[TURN] ") + ui.CutWidth(body, max(4, width-8-lipgloss.Width(tail))) + tail
}

// compactPile 是"场上"：弃牌、牌堆、回合、个人状态。
func (m *Model) compactPile(width int) string {
	o := m.obs()
	var b strings.Builder
	b.WriteString(m.dim().Render("[PILE] 弃牌 "))
	if len(o.Discard) > 0 {
		b.WriteString(m.token(o.Discard[len(o.Discard)-1].Kind))
	} else {
		b.WriteString(m.dim().Render("（空）"))
	}
	b.WriteString(m.dim().Render(fmt.Sprintf("  牌堆 %d", o.DeckCount)))
	if o.UnderAttack {
		b.WriteString(m.warn().Render("  被攻击"))
	}
	if len(o.Future) > 0 {
		b.WriteString(m.dim().Render("  牌顶 "))
		for _, c := range o.Future {
			b.WriteString(m.token(c.Kind))
		}
	}
	return ui.CutWidth(b.String(), width)
}

// compactHand 最多一行五张，翻页由 -= 控制。
// 选目标/选牌种时，这一行换成"可以按哪几个数字"。
func (m *Model) compactHand(width int) []string {
	if opts := m.optionTokens(width); opts != "" {
		return []string{opts}
	}
	page, pages := m.pageCards()
	head := m.dim().Render(fmt.Sprintf("[HAND] %d张", len(m.obs().Hand)))
	if pages > 1 {
		head += m.dim().Render(fmt.Sprintf(" %d/%d", m.page+1, pages))
	}
	head += " "
	if len(page) == 0 {
		return []string{head}
	}
	var parts []string
	for i, c := range page {
		tok := m.dim().Render(fmt.Sprintf("%d.", i+1)) + m.token(c.Kind)
		if m.isPicked(c.ID) {
			tok = m.turn().Render("▸") + tok
		}
		parts = append(parts, tok)
	}
	line := head + strings.Join(parts, " ")
	if lipgloss.Width(line) > width {
		// 一行放不下就只保留能放下的张数，不让终端把牌切断
		kept := parts[:0]
		for _, p := range parts {
			candidate := head + " " + strings.Join(append(kept, p), " ")
			if lipgloss.Width(candidate) > width {
				break
			}
			kept = append(kept, p)
		}
		line = head + " " + strings.Join(kept, " ")
	}
	return []string{line}
}

// viewHelp 是键位一览：按当前阶段只列能用的键。
func (m *Model) viewHelp() string {
	groups := m.keyHelp()
	title := m.turn().Render("[KEY] 键位一览")
	hint := m.dim().Render("按 ? / 或 Esc 关闭")
	head := title
	if pad := m.width - lipgloss.Width("[KEY] 键位一览") - lipgloss.Width(hint); pad > 0 {
		head += strings.Repeat(" ", pad) + hint
	}
	lines := []string{head}
	for _, g := range groups {
		lines = append(lines, m.helpGroupLines(g)...)
	}
	return strings.Join(lines, "\n")
}

type helpGroup struct {
	Title string
	Keys  [][2]string
}

func (m *Model) keyHelp() []helpGroup {
	common := helpGroup{Title: "通用", Keys: [][2]string{
		{"?", "帮助"}, {"/", "帮助"}, {"I", "切换界面"}, {"M", "颜色深浅"},
		{"Tab", "玩家列表"}, {"-=", "翻页"}, {"Ctrl+B", "老板键"},
	}}
	room := helpGroup{Title: "房间", Keys: [][2]string{
		{"Esc", "取消选择 / 离开房间"}, {"Q", "断开连接"},
	}}

	if !m.inGame() {
		keys := [][2]string{{"S", "开始游戏"}, {"B", "加电脑"}, {"V", "投票重开"}}
		if !m.snap.IsHost {
			keys = [][2]string{{"—", "等房主开始"}}
		}
		return []helpGroup{{Title: "大厅", Keys: keys}, common, room}
	}

	o := m.obs()
	var play helpGroup
	switch o.Phase {
	case engine.Responding:
		play = helpGroup{Title: "响应", Keys: [][2]string{
			{"1-5", "打出反制"}, {"P", "放弃"}, {"Tab", "看谁在等"},
		}}
	case engine.Giving:
		play = helpGroup{Title: "交牌", Keys: [][2]string{{"1-5", "把这张交出去"}}}
	case engine.Reinserting:
		play = helpGroup{Title: "拆弹", Keys: [][2]string{
			{"1-5", "选拆弹牌"}, {"-=", "选放回位置"}, {"Enter", "确认"},
		}}
	default:
		play = helpGroup{Title: "出牌", Keys: [][2]string{
			{"1-5", "选牌 / 出牌"}, {"D", "摸牌"}, {"Enter", "确认组合"},
			{"Esc", "取消选择"},
		}}
	}
	return []helpGroup{play, common, room}
}

func (m *Model) helpGroupLines(g helpGroup) []string {
	prefix := fmt.Sprintf("%-5s", "["+g.Title+"]")
	line := m.dim().Render(prefix)
	width := lipgloss.Width(prefix)
	var out []string
	for _, kv := range g.Keys {
		tok := "[" + kv[0] + "]" + kv[1]
		w := lipgloss.Width(tok) + 1
		if width+w > m.width && width > lipgloss.Width(prefix) {
			out = append(out, line)
			line = m.dim().Render(prefix)
			width = lipgloss.Width(prefix)
		}
		line += " " + m.turn().Render("["+kv[0]+"]") + kv[1]
		width += w
	}
	return append(out, line)
}
