package tui

import (
	"fmt"
	"strconv"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
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

func (m *Model) targetActions() []ui.Action {
	targets := m.targets()
	pages := max(1, (len(targets)+1)/2)
	m.targetPage = clampInt(m.targetPage, 0, pages-1)
	start := m.targetPage * 2
	var actions []ui.Action
	for i, seat := range targets[start:min(start+2, len(targets))] {
		key := strconv.Itoa(i + 1)
		actions = append(actions, ui.Action{ID: strconv.Itoa(seat), Key: key, Label: fmt.Sprintf("%s(%d)", m.seatName(seat), m.obs().HandCounts[seat])})
	}
	return actions
}

func (m *Model) targetMenuActions() []ui.Action {
	actions := m.targetActions()
	pages := max(1, (len(m.targets())+1)/2)
	if pages > 1 {
		actions = append(actions, ui.CombinedKeyHint(fmt.Sprintf("%d/%d页", m.targetPage+1, pages), ui.SelectActions(commonActions, "previous", "next")...))
	}
	if m.target >= 0 {
		actions = append(actions, targetConfirmAction)
	}
	return append(actions, ui.EscapeAction("取消"))
}

func (m *Model) targetTokens() []string {
	return ui.ActionTokens(m.targetMenuActions())
}

func chatActions() []ui.Action {
	return ui.NumberedActions(client.Presets)
}

// Wrap complete command items instead of silently hiding usable keys.
func (m *Model) commandRows(width int) []string {
	actions, prefix := m.commandActions()
	return wrapTokens(ui.RenderActions(actions, m.dim().Render, m.turn().Render), m.dim().Render(prefix), width)
}

func wrapTokens(tokens []string, prefix string, width int) []string {
	return ui.WrapTokens(tokens, prefix, width)
}
