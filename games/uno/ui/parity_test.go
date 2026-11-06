package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestBothModesSameKeys 两种模式的按键语义必须一致。
//
// 这是这轮改造的目的：以前丰富模式是"光标选牌 + 回车出牌"，极简是"数字直出"，
// 切个界面就得换一套肌肉记忆。现在同键同效，只是排版不同。
func TestBothModesSameKeys(t *testing.T) {
	type probe struct {
		key  string
		want string // 期望的可观察效果
	}
	probes := []probe{
		{"3", "PlayCard"}, // 数字直接出牌
		{"d", "DrawCard"}, // 摸牌
		{"p", "Pass"},     // 过牌
		{"u", "CallUno"},  // 喊 UNO
		{"?", "help"},     // 帮助
		{"-", "page"},     // 上一页（已在第二页时有效）
		{"=", "page"},     // 下一页
	}

	for _, p := range probes {
		for _, minimal := range []bool{false, true} {
			m := playingState(minimal).modelWith(t)
			rec := newRecorder(m.snap)
			m.c = rec
			if p.key == "-" {
				m.page = 1
			}
			before := fingerprint(m)

			m.handleKey(keyOf(p.key))

			got := ""
			switch {
			case len(rec.calls) > 0:
				got = rec.calls[0]
			case m.help:
				got = "help"
			case fingerprint(m) != before:
				got = "page"
			}
			ok := got == p.want ||
				(p.want != "help" && p.want != "page" && strings.Contains(got, p.want))
			if !ok {
				t.Fatalf("[%s] 按 %q 期望 %s，实际 %s（calls=%v）",
					modeName(minimal), p.key, p.want, got, rec.calls)
			}
		}
	}
}

func TestCallAndCatchRemainAvailableOutsideYourTurn(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		for _, tc := range []struct{ key, action string }{{"u", "CallUno"}, {"c", "CatchUno"}} {
			state := playingState(minimal)
			setObs(&state.snap, func(o *engine.Observation) { o.Turn = 1; o.UnoPending = 1 })
			m, rec := state.model()
			m.handleKey(keyOf(tc.key))
			if len(rec.calls) != 1 || !strings.Contains(rec.calls[0], tc.action) {
				t.Fatalf("minimal=%v key=%s: calls=%v", minimal, tc.key, rec.calls)
			}
		}
	}
}

func TestDrawIsUnavailableOutsideYourTurnInBothLayouts(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		state := playingState(minimal)
		setObs(&state.snap, func(o *engine.Observation) { o.Turn = 1 })
		m, rec := state.model()
		m.handleKey(keyOf("d"))
		if len(rec.calls) != 0 {
			t.Fatalf("minimal=%v: draw dispatched outside turn: %v", minimal, rec.calls)
		}
		if strings.Contains(strip(m.View()), "[D]摸牌") {
			t.Fatalf("minimal=%v: unavailable draw was advertised", minimal)
		}
	}
}

// TestNoCursorEnterPlay 丰富模式不再有"回车出牌"：回车什么都不做。
func TestNoCursorEnterPlay(t *testing.T) {
	m := playingState(false).modelWith(t)
	rec := newRecorder(m.snap)
	m.c = rec

	before := fingerprint(m)
	m.handleKey(keyOf("enter"))
	if len(rec.calls) != 0 {
		t.Fatalf("回车不该出牌了，实际 %v", rec.calls)
	}
	if fingerprint(m) != before {
		t.Fatal("回车不该改变界面状态")
	}
}

// TestPagingMovesThroughHand 翻页能翻到后面的牌，且能把后面的牌打出去。
func TestPagingMovesThroughHand(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := playingState(minimal).modelWith(t)
		rec := newRecorder(m.snap)
		m.c = rec

		m.handleKey(keyOf("="))
		if m.page != 1 {
			t.Fatalf("[%s] 应该翻到第二页，实际 %d", modeName(minimal), m.page)
		}
		m.handleKey(keyOf("-"))
		if m.page != 0 {
			t.Fatalf("[%s] 应该翻回第一页，实际 %d", modeName(minimal), m.page)
		}

		// 第二页的第一张牌必须能被数字 1 打出去
		m.handleKey(keyOf("="))
		page, _ := m.pageCards()
		if len(page) == 0 {
			t.Fatalf("[%s] 第二页不该是空的", modeName(minimal))
		}
		wantID := page[0].ID
		m.handleKey(keyOf("1"))
		if len(rec.calls) == 0 {
			t.Fatalf("[%s] 第二页按 1 应该出牌", modeName(minimal))
		}
		if !strings.Contains(rec.calls[0], fmt.Sprintf("#%d", wantID)) {
			t.Fatalf("[%s] 出的不是第二页第一张：%v（期望牌 %d）", modeName(minimal), rec.calls, wantID)
		}
	}
}

// pageCards 是测试用的分页取手牌。
func (m *Model) pageCards() ([]engine.Card, int) {
	if m.minimal {
		return m.compactPage()
	}
	return m.handPage()
}
