package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// drawnState 造一个"刚摸完牌、摸到的那张能出"的界面状态。
func drawnState(minimal bool) state {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = []engine.Card{
			{ID: 10, Color: engine.Red, Kind: engine.Number, Num: 5},
			{ID: 30, Color: engine.Red, Kind: engine.Number, Num: 7},
		}
		o.Active = engine.Red
		o.Turn = o.Seat
		o.Phase = engine.Playing
		o.DrawnID = 30
		// 摸完了：两张红牌都能出，而且允许跳过
		o.Legal = []engine.Action{
			engine.PlayCard{Seat: o.Seat, CardID: 10, Color: engine.Red},
			engine.PlayCard{Seat: o.Seat, CardID: 30, Color: engine.Red},
			engine.Pass{Seat: o.Seat},
		}
	})
	return state{name: "摸完牌", snap: snap, minimal: minimal}
}

// TestDrawnCardDoesNotLockHand 摸牌之后，手里原有的牌也能出。
//
// 这是一次真实投诉：手里明明有红牌，摸一张之后系统只让出摸到的那张。
func TestDrawnCardDoesNotLockHand(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := drawnState(minimal).modelWith(t)
		rec := newRecorder(m.snap)
		m.c = rec

		// 第一张（原有的红 5）应该能出
		m.handleKey(keyOf("1"))
		if len(rec.calls) == 0 {
			t.Fatalf("[%s] 摸牌后原有的牌应该还能出", modeName(minimal))
		}
		if !strings.Contains(rec.calls[0], "#10") {
			t.Fatalf("[%s] 出的应该是原有那张（#10），实际 %v", modeName(minimal), rec.calls)
		}
	}
}

// TestCanPassAfterDrawingPlayable 摸到能出的牌之后，仍然可以选择跳过。
func TestCanPassAfterDrawingPlayable(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := drawnState(minimal).modelWith(t)
		rec := newRecorder(m.snap)
		m.c = rec

		m.handleKey(keyOf("p"))
		if len(rec.calls) != 1 || !strings.Contains(rec.calls[0], "Pass") {
			t.Fatalf("[%s] 按 p 应该跳过，实际 %v", modeName(minimal), rec.calls)
		}
	}
}

// TestCompactCmdAdvertisesPass 极简模式的 CMD 要写出 [P]过。
//
// 引擎一直允许跳过，但界面上没这个键，玩家根本不知道能跳过。
func TestCompactCmdAdvertisesPass(t *testing.T) {
	m := drawnState(true).modelWith(t)
	m.width = 100
	view := strip(m.View())
	if !strings.Contains(view, "[P]过") {
		t.Fatalf("极简 CMD 应该显示 [P]过：\n%s", view)
	}
}
