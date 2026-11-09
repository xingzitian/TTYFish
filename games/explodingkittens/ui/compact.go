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
	direction := "→"
	if o.Direction == -1 {
		direction = "←"
	}
	tail := fmt.Sprintf(" | %s 需摸:%d 存活:%d/%d", direction, o.TurnsRemaining, alive, seats)
	if width-lipglossWidth("[TURN] "+tail) < 37 {
		tail = fmt.Sprintf(" | %s 需摸:%d", direction, o.TurnsRemaining)
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
func (m *Model) helpContent() string {
	return ui.RenderHelp(m.keyHelp(), m.width, "[KEY] 键位一览", ui.HelpCloseHint(ui.HelpActions(commonActions, "help", "quit")), m.dim().Render, m.turn().Render)
}

func (m *Model) viewHelp() string {
	view, _ := ui.HelpViewport(m.helpContent(), m.width, m.height, m.helpOffset)
	return view
}

func (m *Model) keyHelp() []ui.ActionGroup {
	common := ui.ActionGroup{Title: "通用"}
	for _, action := range ui.SelectActions(commonActions, "help", "layout", "color", "players") {
		common.Actions = append(common.Actions, action)
		if action.ID == "help" {
			common.Actions = append(common.Actions, ui.Action{Key: "/", Label: action.HelpLabel()})
		}
	}
	common.Actions = append(common.Actions, ui.CombinedKeyHint("翻页", ui.SelectActions(commonActions, "previous", "next")...), ui.BossAction())
	room := ui.ActionGroup{Title: "房间", Actions: ui.SelectActions(commonActions, "cancel", "quit")}

	if !m.inGame() {
		keys := lobbyActions
		if !m.snap.IsHost {
			keys = []ui.Action{{Key: "—", Label: "等房主开始"}}
		}
		return []ui.ActionGroup{{Title: "大厅", Actions: keys}, common, room}
	}

	o := m.obs()
	if m.alive() {
		common.Actions = append(common.Actions, ui.SelectActions(commonActions, "chat")...)
	}
	if o.Phase == engine.Reinserting && o.Turn == o.Seat {
		for i := range common.Actions {
			if common.Actions[i].ID == "layout" {
				for _, action := range ui.SelectActions(reinsertActions, "i") {
					action.Detail = "指定埋雷位置"
					common.Actions[i] = action
				}
			}
		}
	}
	if m.step == stepTarget {
		targets := m.targetActions()
		targets = append(targets, ui.CombinedKeyHint("目标翻页", ui.SelectActions(commonActions, "previous", "next")...), ui.EscapeAction("取消"))
		return []ui.ActionGroup{{Title: "目标", Actions: targets}, common, room}
	}
	if m.step == stepChat {
		return []ui.ActionGroup{{Title: "聊天", Actions: append(chatActions(), ui.EscapeAction("取消"))}, common, room}
	}
	if !m.alive() || (o.Phase == engine.Playing && !m.myTurn()) ||
		(o.Phase == engine.Giving && (o.Pending == nil || o.Pending.Action.Target != o.Seat)) ||
		(o.Phase == engine.Reinserting && o.Turn != o.Seat) {
		return []ui.ActionGroup{common, room}
	}
	var play ui.ActionGroup
	switch o.Phase {
	case engine.Responding:
		play = ui.ActionGroup{Title: "响应"}
		if m.canNope() {
			for _, action := range ui.SelectActions(responseActions, "n") {
				action.Detail = action.Label + " / 反否决"
				play.Actions = append(play.Actions, action)
			}
		}
		if !m.passed() && m.passSent != responseKey(o) {
			play.Actions = append(play.Actions, ui.SelectActions(responseActions, "p")...)
		}
		play.Actions = append(play.Actions, ui.SelectActions(commonActions, "chat")...)
	case engine.Giving:
		play = ui.ActionGroup{Title: "交牌", Actions: []ui.Action{{Key: "1-5", Label: "把这张交出去"}}}
	case engine.Reinserting:
		if m.step == stepDefuseCustom {
			play = ui.ActionGroup{Title: "埋雷", Actions: customPositionActions}
		} else {
			play = ui.ActionGroup{Title: "埋雷", Actions: reinsertActions}
		}
	default:
		play = ui.ActionGroup{Title: "出牌", Actions: append([]ui.Action{cardAction}, playActions...)}
		play.Actions = append(play.Actions, ui.SelectActions(commonActions, "chat")...)
		play.Actions = append(play.Actions, ui.EscapeAction("取消选择"))
	}
	return []ui.ActionGroup{play, common, room}
}
