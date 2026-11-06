package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PlayerRows arranges game-provided player badges in seat order. The game
// decides each badge's meaning and style; the shared component handles rows
// and terminal cell widths.
func PlayerRows(items []string, width, perRow int, prefix string) []string {
	if perRow <= 0 {
		perRow = len(items)
	}
	if len(items) == 0 {
		return []string{prefix + "（还没有玩家）"}
	}
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	var rows []string
	for start := 0; start < len(items); start += perRow {
		rowPrefix := indent
		if start == 0 {
			rowPrefix = prefix
		}
		rows = append(rows, WrapTokens(items[start:min(start+perRow, len(items))], rowPrefix, width)...)
	}
	return rows
}
