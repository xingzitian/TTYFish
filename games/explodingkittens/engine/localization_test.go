package engine

import (
	"strings"
	"testing"
)

func TestChinesePublicEventsKeepPrivateCardsHidden(t *testing.T) {
	s, _ := NewGame(2, 1)
	s.Hands = [][]Card{
		{{ID: 100, Kind: Favor}},
		{{ID: 101, Kind: Defuse}},
	}
	for _, a := range []Action{
		{Type: Play, Seat: 0, CardIDs: []int{100}, Target: 1},
		{Type: Pass, Seat: 0},
		{Type: Pass, Seat: 1},
		{Type: Give, Seat: 1, CardIDs: []int{101}},
	} {
		if err := Apply(s, a); err != nil {
			t.Fatal(err)
		}
	}
	var messages []string
	for _, event := range s.Log {
		messages = append(messages, event.Text)
	}
	log := strings.Join(messages, "\n")
	for _, want := range []string{"打出索要，目标为 P2", "反制窗口结束", "交出了一张牌"} {
		if !strings.Contains(log, want) {
			t.Fatalf("missing translated event %q: %s", want, log)
		}
	}
	if strings.Contains(log, "拆弹") || strings.Contains(log, "defuse") {
		t.Fatal("given card exposed in public log")
	}
}
