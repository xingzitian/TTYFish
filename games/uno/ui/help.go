package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/sdk/client"
)

// helpGroup 是键位一览里的一组。
type helpGroup struct {
	Title string
	Keys  []helpKey
}

type helpKey struct {
	Key  string
	Desc string
}

// keyHelp 按当前阶段给出可用按键。
//
// 两种模式共用这一份：以前键位提示散在三处，结果出现"极简写着 [Q]离开、
// 实际 q 是断线"这种自相矛盾。以后只改这里。
func (m *Model) keyHelp() []helpGroup {
	common := helpGroup{
		Title: "信息",
		Keys: []helpKey{
			{"T", "聊天"},
			{"V", "重开投票"},
			{"M", "颜色深浅"},
			{"I", "切换界面"},
		},
	}
	room := helpGroup{
		Title: "房间",
		Keys: []helpKey{
			{"L", "离开房间"},
			{"Esc", "离开（需确认）"},
			{"Q", "断开连接"},
		},
	}
	closeGroup := helpGroup{Title: "帮助", Keys: []helpKey{{"?", "关闭"}, {"/", "关闭"}, {"Esc", "关闭"}}}

	if m.snap.Phase != client.Playing {
		keys := []helpKey{}
		if m.snap.IsHost {
			keys = append(keys, helpKey{"S", "开始游戏"}, helpKey{"B", "加一个电脑"})
		} else {
			keys = append(keys, helpKey{"—", "等房主开始"})
		}
		return []helpGroup{
			{Title: "大厅", Keys: keys},
			common,
			room,
			closeGroup,
		}
	}

	return []helpGroup{
		{
			Title: "出牌",
			Keys: []helpKey{
				{"1-9", "出当前页第几张"},
				{"-=", "翻页"},
				{"D", "摸牌"},
				{"P", "过牌"},
				{"U", "喊 UNO"},
				{"C", "抓包"},
			},
		},
		common,
		room,
		closeGroup,
	}
}

// viewHelp 画键位一览。极简模式一行一组，丰富模式两组一行。
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

// helpGroupLines 把一组键折成若干行，宽度不够就换行。
func (m *Model) helpGroupLines(g helpGroup) []string {
	tokens := make([]string, 0, len(g.Keys))
	for _, k := range g.Keys {
		tokens = append(tokens, "["+k.Key+"]"+k.Desc)
	}

	prefix := fmt.Sprintf("%-5s", "["+g.Title+"]")
	var out []string
	line := m.dim().Render(prefix)
	width := lipgloss.Width(prefix)
	for _, tok := range tokens {
		w := lipgloss.Width(tok) + 1
		if width+w > m.width && width > lipgloss.Width(prefix) {
			out = append(out, line)
			line = m.dim().Render(prefix)
			width = lipgloss.Width(prefix)
		}
		line += " " + m.renderKeyToken(tok)
		width += w
	}
	return append(out, line)
}

// renderKeyToken 让按键本身亮一点、说明暗一点，扫起来更快。
func (m *Model) renderKeyToken(tok string) string {
	i := strings.Index(tok, "]")
	if i < 0 {
		return m.dim().Render(tok)
	}
	return m.turn().Render(tok[:i+1]) + tok[i+1:]
}
