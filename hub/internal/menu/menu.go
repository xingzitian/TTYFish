// Package menu 是一个最小的单列菜单控件：标题栏下面的 "> 1. xxx" 列表。
//
// 只做三件事：上下移动、选中、跳转；不碰房间也不碰网络。
// 网关把它和游戏界面拼成一条连接上的多个屏幕。
package menu

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/sdk/ui"
)

// Item 是菜单里的一行。
type Item struct {
	Label string
	// Hint 是行尾的补充说明，例如房间数 "（—）"。
	Hint string
	// Enabled 为 false 时仍然显示但不可选，用来占位"后端还没提供"的功能。
	Enabled bool
}

// Model 是一个菜单屏幕。
type Model struct {
	items    []Item
	cursor   int
	width    int
	height   int
	renderer *lipgloss.Renderer
	mode     ui.ColorMode
}

func New(r *lipgloss.Renderer, mode ui.ColorMode, width, height int) *Model {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return &Model{renderer: r, mode: mode, width: width, height: height}
}

// SetItems 换一批条目。光标尽量保持在原来的位置，越界就夹回来。
func (m *Model) SetItems(items []Item) {
	m.items = items
	if len(items) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	// 光标落在不可选项上就往下找第一个能选的
	if !m.items[m.cursor].Enabled {
		for i := 0; i < len(items); i++ {
			next := (m.cursor + i) % len(items)
			if m.items[next].Enabled {
				m.cursor = next
				return
			}
		}
	}
}

func (m *Model) SetSize(width, height int) {
	if width > 0 {
		m.width = width
	}
	if height > 0 {
		m.height = height
	}
}

func (m *Model) Items() []Item { return m.items }

// Cursor 是当前高亮的下标（可能指向不可选项，见 Selected）。
func (m *Model) Cursor() int { return m.cursor }

// Reset 把光标放回第一项（换菜单时用，避免把上一屏的位置带进新菜单）。
func (m *Model) Reset() { m.cursor = 0 }

// Move 上下移动一格；不可选项会被跳过。找不到可选项时原地不动。
func (m *Model) Move(delta int) {
	if len(m.items) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := 1; i <= len(m.items); i++ {
		next := ((m.cursor+step*i)%len(m.items) + len(m.items)) % len(m.items)
		if m.items[next].Enabled {
			m.cursor = next
			return
		}
	}
}

// Select 按序号选中（1 起算）；越界或者不可选返回 false。
func (m *Model) Select(n int) bool {
	idx := n - 1
	if idx < 0 || idx >= len(m.items) || !m.items[idx].Enabled {
		return false
	}
	m.cursor = idx
	return true
}

// Selected 返回当前高亮项；不可选时 ok=false。
func (m *Model) Selected() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return Item{}, false
	}
	item := m.items[m.cursor]
	return item, item.Enabled
}

// View 画条目列表。标题栏由上层拼（它要知道当前层级）。
func (m *Model) View() string {
	dim := m.renderer.NewStyle().Foreground(lipgloss.Color("244"))
	hot := m.renderer.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	if m.mode == ui.ColorNone {
		dim = m.renderer.NewStyle()
		hot = m.renderer.NewStyle().Bold(true)
	}

	lines := make([]string, 0, len(m.items))
	for i, item := range m.items {
		marker := "  "
		style := dim
		if i == m.cursor {
			marker = "> "
			style = hot
		}
		label := fmt.Sprintf("%d. %s", i+1, item.Label)
		if item.Hint != "" {
			label += " " + item.Hint
		}
		// 不可选要有标记；但条目自己已经写了"不可用"就别重复，
		// 以前会叠成"（不可用）（暂不可用）"，像坏了两次。
		if !item.Enabled && !strings.Contains(item.Hint, "不可用") {
			label += "（暂不可用）"
		}
		lines = append(lines, style.Render(ui.CutWidth(marker+label, m.width)))
	}
	if len(lines) == 0 {
		lines = append(lines, dim.Render("（没有内容）"))
	}
	return strings.Join(lines, "\n")
}
