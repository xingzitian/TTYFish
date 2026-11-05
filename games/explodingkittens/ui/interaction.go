package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

func responseKey(o engine.Observation) string {
	if o.Phase != engine.Responding || o.Pending == nil {
		return ""
	}
	return fmt.Sprintf("%d:%v:%d", o.Pending.Action.Seat, o.Pending.Action.CardIDs, o.Pending.Nopes)
}

func (m *Model) passed() bool {
	o := m.obs()
	return o.Pending != nil && o.Seat >= 0 && o.Seat < len(o.Pending.Passed) &&
		o.Pending.Passed[o.Seat]
}

func (m *Model) canNope() bool {
	o := m.obs()
	if !m.inGame() || !m.alive() || o.Phase != engine.Responding ||
		o.Pending == nil || m.nopeCount() == 0 || m.responseSent == responseKey(o) {
		return false
	}
	// Do not let a held N key counter the player's own previous Nope.
	return o.Pending.Nopes == 0 || o.DiscardBy != o.Seat ||
		len(o.Discard) == 0 || o.Discard[len(o.Discard)-1].Kind != engine.Nope
}

func (m *Model) targets() []int {
	o := m.obs()
	var seats []int
	for seat, alive := range o.Alive {
		if alive && seat != o.Seat && seat < len(o.HandCounts) && o.HandCounts[seat] > 0 {
			seats = append(seats, seat)
		}
	}
	return seats
}

func (m *Model) targetTokens() []string {
	targets := m.targets()
	pages := max(1, (len(targets)+1)/2)
	m.targetPage = clampInt(m.targetPage, 0, pages-1)
	start := m.targetPage * 2
	var tokens []string
	for i, seat := range targets[start:min(start+2, len(targets))] {
		tokens = append(tokens, fmt.Sprintf("[%d]%s(%d)", i+1, m.seatName(seat), m.obs().HandCounts[seat]))
	}
	if pages > 1 {
		tokens = append(tokens, fmt.Sprintf("[-=]%d/%d页", m.targetPage+1, pages))
	}
	return append(tokens, "[Esc]取消")
}

func (m *Model) chatTokens() []string {
	var tokens []string
	for i, text := range client.Presets {
		tokens = append(tokens, fmt.Sprintf("[%d]%s", i+1, text))
	}
	return append(tokens, "[Esc]取消")
}

// Wrap complete command items instead of silently hiding usable keys.
func (m *Model) commandRows(width int) []string {
	tokens, prefix := m.cmdTokens()
	return wrapTokens(tokens, prefix, width)
}

func wrapTokens(tokens []string, prefix string, width int) []string {
	var rows []string
	line := prefix
	indent := strings.Repeat(" ", min(lipglossWidth(prefix), max(0, width-1)))
	for _, token := range tokens {
		separator := ""
		if line != prefix && line != indent {
			separator = " "
		}
		if lipglossWidth(line+separator+token) > width && line != prefix && line != indent {
			rows = append(rows, line)
			line, separator = indent, ""
		}
		line += separator + token
	}
	rows = append(rows, line)
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], width, "")
	}
	return rows
}
