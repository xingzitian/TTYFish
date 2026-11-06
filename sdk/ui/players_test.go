package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPlayerRowsWrapsWithoutLosingBadges(t *testing.T) {
	badges := []string{"小猫(5张)", "小狗(2张)", "阿狸(淘汰)", "小鱼(4张)"}
	rows := PlayerRows(badges, 24, 3, "[PLAYER] ")
	joined := strings.Join(rows, "\n")
	for _, badge := range badges {
		if !strings.Contains(joined, badge) {
			t.Fatalf("missing %q in %q", badge, joined)
		}
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 24 {
			t.Fatalf("row too wide: %q", row)
		}
	}
}
