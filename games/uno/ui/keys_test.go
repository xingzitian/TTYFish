package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// keyOf 把 "tab" / "right" / "i" 这类名字变成 bubbletea 的按键消息。
// 测试里只用到几种，剩下的按单个字符处理就够。
func keyOf(name string) tea.KeyMsg {
	switch name {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}
