package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func (m *Model) livingSeats() []int {
	var seats []int
	for seat, alive := range m.obs().Alive {
		if alive {
			seats = append(seats, seat)
		}
	}
	return seats
}

func (m *Model) updateTurnOffset(previous client.Snapshot) {
	old, ok := previous.View.(engine.Observation)
	o := m.obs()
	if !ok || previous.Code != m.snap.Code || previous.Phase != client.Playing ||
		m.snap.Phase != client.Playing || !slices.Equal(old.Alive, o.Alive) {
		m.turnOffset, m.slideSeats = 0, 0
		return
	}
	if old.Turn == o.Turn {
		return
	}
	seats := m.livingSeats()
	prev, next := slices.Index(seats, old.Turn), slices.Index(seats, o.Turn)
	if prev < 0 || next < 0 || len(seats) < 2 {
		m.turnOffset, m.slideSeats = 0, 0
		return
	}
	// Draw order is always forward, including the wrap from last to first.
	m.slideSeats = -((next - prev + len(seats)) % len(seats))
	m.turnOffset = 0
}

func (m *Model) turnBadge(seat, width int) string {
	o := m.obs()
	count := fmt.Sprintf("(%d)", o.HandCounts[seat])
	current := seat == o.Turn
	reserve := lipglossWidth(count)
	if current {
		reserve += 2
	}
	name := ansi.Truncate(m.seatName(seat), max(0, width-reserve), "")
	if current {
		name = "*" + name + "*"
	}
	badge := name + count
	if current {
		return m.turn().Render(badge)
	}
	return badge
}

func (m *Model) turnMarquee(width int) string {
	seats := m.livingSeats()
	current := slices.Index(seats, m.obs().Turn)
	if current < 0 || width < 1 {
		return ""
	}
	slot := (width - 10) / 3 // Two fixed-width " >>> " separators.
	if slot < 9 {
		m.turnOffset, m.slideSeats = 0, 0
		return ui.PadRight(m.turnBadge(m.obs().Turn, width), width)
	}
	stride := slot + 5
	if m.slideSeats != 0 {
		m.turnOffset = m.slideSeats * stride
		m.slideSeats = 0
	}
	var tape strings.Builder
	for _, seat := range seats {
		badge := m.turnBadge(seat, slot)
		pad := max(0, (slot-lipglossWidth(badge))/2)
		tape.WriteString(ui.PadRight(strings.Repeat(" ", pad)+badge, slot))
		tape.WriteString(" >>> ")
	}
	tapeW := stride * len(seats)
	start := ((current-1)*stride + m.turnOffset) % tapeW
	if start < 0 {
		start += tapeW
	}
	window := ui.CyclicWindow(tape.String(), start, 3*slot+10)
	return ui.PadRight(window, width)
}

func (m *Model) compactPlayers(width int) []string {
	var items []string
	o := m.obs()
	for _, p := range m.snap.Players {
		label := m.seatName(p.Seat) + "("
		if p.Seat >= 0 && p.Seat < len(o.Alive) {
			if o.Alive[p.Seat] {
				label += fmt.Sprintf("%d张", o.HandCounts[p.Seat])
			} else {
				label += "淘汰"
			}
		}
		if p.Bot {
			label += " 托管"
		} else if !p.Connected {
			label += " 掉线"
		}
		items = append(items, label+")")
	}
	return ui.PlayerRows(items, width, 3, "[PLAYER] ")
}
