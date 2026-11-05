package tui

import (
	"fmt"
	"strings"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// viewCompact 是窄屏的五段版：TURN / DECK / HAND / MSG / CMD。
func (m *Model) viewCompact() string {
	width := m.width
	lines := []string{
		m.compactTurn(width),
		m.compactDeck(width),
	}
	lines = append(lines, m.compactHand(width)...)
	if m.showPlayers() {
		lines = append(lines, m.compactPlayers(width)...)
	}
	lines = append(lines, m.statusLine(width), m.cmdLine(width))
	return strings.Join(lines, "\n")
}

// compactTurn 是"谁在对局"：当前玩家居中，左右各一位，方向固定向右。
//
//	[TURN] 电脑3(7)  >>>  *你*(7)[需摸:1]  >>>  电脑1(7)  | 存活: 4/4
func (m *Model) compactTurn(width int) string {
	o := m.obs()
	if !m.inGame() || len(o.Alive) == 0 {
		return m.turn().Render("[TURN] ") + m.dim().Render("等待开局")
	}
	seats := len(o.Alive)
	alive := 0
	for _, ok := range o.Alive {
		if ok {
			alive++
		}
	}
	tail := fmt.Sprintf(" | 需摸:%d 存活:%d/%d", o.TurnsRemaining, alive, seats)
	if width-lipglossWidth("[TURN] "+tail) < 37 {
		tail = fmt.Sprintf(" | 需摸:%d", o.TurnsRemaining)
	}
	if width-lipglossWidth("[TURN] "+tail) < 9 {
		tail = ""
	}
	body := m.turnMarquee(max(1, width-lipglossWidth("[TURN] "+tail)))
	return m.turn().Render("[TURN] ") + body + m.dim().Render(tail)
}

// compactDeck 是场上信息：牌堆剩余、炸弹数与概率、弃牌顶。
//
//	[DECK] 牌堆: 14张 [Bomb x2|14.3%]  弃牌顶: [功能|洗牌](电脑2)
func (m *Model) compactDeck(width int) string {
	o := m.obs()
	head := m.dim().Render("[DECK] 牌堆: ") + fmt.Sprintf("%d张", o.DeckCount)

	bomb := ""
	if o.BombsLeft > 0 {
		bomb = " " + m.warn().Render(fmt.Sprintf("[Bomb x%d|%s]", o.BombsLeft, m.bombChance()))
	}

	discard := ""
	if len(o.Discard) > 0 {
		discard = m.dim().Render("  弃牌顶: ") + m.token(o.Discard[len(o.Discard)-1].Kind)
		if o.DiscardBy >= 0 {
			discard += m.dim().Render("(" + m.seatName(o.DiscardBy) + ")")
		}
	}

	// 放不下就整段丢：先丢弃牌信息，再丢炸弹概率，最后才截断
	line := head + bomb + discard
	if lipglossWidth(line) <= width {
		return line
	}
	line = head + bomb
	if lipglossWidth(line) <= width {
		return line
	}
	return ui.ClampFrame(head, width, 1)
}

// Keep all five numbered cards accessible; wrap rather than hiding later cards.
func (m *Model) compactHand(width int) []string {
	page, pages := m.pageCards()
	head := m.dim().Render(fmt.Sprintf("[HAND] %d张 ", len(m.obs().Hand)))

	pageTag := ""
	if pages > 1 {
		// 先把页码的位置留出来，再往这一行塞牌
		pageTag = fmt.Sprintf(" → %d/%d", m.page+1, pages)
	}
	var parts []string
	for i, c := range page {
		parts = append(parts, m.dim().Render(fmt.Sprintf("%d.", i+1))+m.token(c.Kind))
	}
	if pageTag != "" {
		parts = append(parts, m.dim().Render(pageTag))
	}
	return wrapTokens(parts, head, width)
}

// viewHelp 是键位一览：按当前阶段只列能用的键。
func (m *Model) viewHelp() string {
	groups := m.keyHelp()
	title := m.turn().Render("[KEY] 键位一览")
	hint := m.dim().Render("按 ? / 或 Esc 关闭")
	head := title
	if pad := m.width - lipglossWidth("[KEY] 键位一览") - lipglossWidth(hint); pad > 0 {
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
	if m.alive() {
		common.Keys = append(common.Keys, [2]string{"T", "快捷语"})
	}
	if o.Phase == engine.Reinserting && o.Turn == o.Seat {
		common.Keys[2] = [2]string{"I", "指定埋雷位置"}
	}
	if m.step == stepTarget {
		return []helpGroup{{Title: "目标", Keys: [][2]string{
			{"1-2", "立即选择本页目标"}, {"-=", "目标翻页"}, {"Esc", "取消"},
		}}, common, room}
	}
	if m.step == stepChat {
		return []helpGroup{{Title: "聊天", Keys: [][2]string{
			{"1-6", "发送对应快捷语"}, {"Esc", "取消"},
		}}, common, room}
	}
	if !m.alive() || (o.Phase == engine.Playing && !m.myTurn()) ||
		(o.Phase == engine.Giving && (o.Pending == nil || o.Pending.Action.Target != o.Seat)) ||
		(o.Phase == engine.Reinserting && o.Turn != o.Seat) {
		return []helpGroup{common, room}
	}
	var play helpGroup
	switch o.Phase {
	case engine.Responding:
		play = helpGroup{Title: "响应"}
		if m.canNope() {
			play.Keys = append(play.Keys, [2]string{"N", "否决 / 反否决"})
		}
		if !m.passed() && m.passSent != responseKey(o) {
			play.Keys = append(play.Keys, [2]string{"P", "放弃"})
		}
		play.Keys = append(play.Keys, [2]string{"T", "快捷语"})
	case engine.Giving:
		play = helpGroup{Title: "交牌", Keys: [][2]string{{"1-5", "把这张交出去"}}}
	case engine.Reinserting:
		play = helpGroup{Title: "埋雷", Keys: [][2]string{
			{"0", "牌堆顶"}, {"1", "第2张"}, {"2", "第3张"},
			{"B", "牌堆底"}, {"R", "随机"}, {"I", "指定位置"},
		}}
	default:
		play = helpGroup{Title: "出牌", Keys: [][2]string{
			{"1-5", "出功能牌"}, {"G", "对子盲抽"}, {"D", "摸牌"},
			{"T", "快捷语"}, {"Esc", "取消选择"},
		}}
	}
	return []helpGroup{play, common, room}
}

func (m *Model) helpGroupLines(g helpGroup) []string {
	prefix := fmt.Sprintf("%-5s", "["+g.Title+"]")
	line := m.dim().Render(prefix)
	width := lipglossWidth(prefix)
	var out []string
	for _, kv := range g.Keys {
		tok := "[" + kv[0] + "]" + kv[1]
		w := lipglossWidth(tok) + 1
		if width+w > m.width && width > lipglossWidth(prefix) {
			out = append(out, line)
			line = m.dim().Render(prefix)
			width = lipglossWidth(prefix)
		}
		line += " " + m.turn().Render("["+kv[0]+"]") + kv[1]
		width += w
	}
	return append(out, line)
}
