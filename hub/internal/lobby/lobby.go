// Package lobby 是 SSH 连上来之后的第一个界面：选游戏。
//
// 它只负责"选哪个"，不碰房间和规则——选完之后由网关建房间、把界面交给对应的游戏。
package lobby

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// Game 是大厅里的一项，来自后端 /api/v1/games 与本地挂载的交集。
type Game struct {
	ID   string
	Name string
	Min  int
	Max  int
}

// Model 是选游戏界面。
type Model struct {
	games  []Game
	cursor int
	chosen string
	done   bool
	width  int
	height int
	mode   ui.ColorMode
	r      *lipgloss.Renderer
}

func New(games []Game, mode ui.ColorMode, r *lipgloss.Renderer) *Model {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	return &Model{games: games, mode: mode, r: r, width: 80, height: 24}
}

func (m *Model) Init() tea.Cmd { return nil }

// Chosen 是玩家选中的游戏标识；没选（还在选或者退出）时为空。
func (m *Model) Chosen() string { return m.chosen }

// Done 表示这个界面已经结束（选中或退出），上层该切换到下一个界面了。
func (m *Model) Done() bool { return m.done }

func (m *Model) Close() {}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		if v.Width > 0 {
			m.width = v.Width
		}
		if v.Height > 0 {
			m.height = v.Height
		}
	case tea.KeyMsg:
		switch v.String() {
		case "q", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		case "up", "k":
			if len(m.games) > 0 {
				m.cursor = (m.cursor - 1 + len(m.games)) % len(m.games)
			}
		case "down", "j":
			if len(m.games) > 0 {
				m.cursor = (m.cursor + 1) % len(m.games)
			}
		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.games) {
				m.chosen = m.games[m.cursor].ID
				m.done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *Model) View() string {
	return ui.ClampFrame(m.render(), m.width, m.height)
}

func (m *Model) render() string {
	dim := m.r.NewStyle().Foreground(lipgloss.Color("244"))
	hot := m.r.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	title := m.r.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(title.Render("TTYFish  选一个游戏") + "\n\n")

	if len(m.games) == 0 {
		b.WriteString(dim.Render("后端没有开放任何游戏（或者网关一个都没挂）") + "\n")
		return b.String()
	}

	for i, g := range m.games {
		marker := "  "
		style := dim
		if i == m.cursor {
			marker, style = "> ", hot
		}
		line := fmt.Sprintf("%s%s  %d~%d 人", marker, g.Name, g.Min, g.Max)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(dim.Render("↑↓ 选择 · Enter 进入 · q 退出"))
	return b.String()
}
