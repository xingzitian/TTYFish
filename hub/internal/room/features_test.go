package room

import (
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// newUnoRoom 建一间开了"漏喊 UNO 罚牌"的房，方便测宽限窗口。
func newUnoRoom(t *testing.T) (*Room, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	r := New("ABC234", uno.New(engine.Rules{UnoPenalty: true}), Options{
		TurnTimeout: time.Minute,
		Grace:       time.Minute,
		BotLag:      time.Second,
		Now:         clk.now,
		NoLoop:      true,
	})
	t.Cleanup(r.Close)
	return r, clk
}

// forceState 直接把牌局摆成想要的样子（测试专用）。
// 先清掉 UNO 宣言状态，不然上一段测试留下的 UnoCalled 会干扰判定。
func forceState(r *Room, mutate func(*engine.State)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil {
		r.state = r.Game.NewMatch([]game.PlayerSeed{{Seat: 0}, {Seat: 1}}, 1)
	}
	s := r.state.(*engine.State)
	s.UnoCalled = make([]bool, s.SeatCount)
	s.UnoPending = -1
	mutate(s)
	r.phase = Playing
	r.turnAt = r.opts.Now()
	r.clearPendingLocked()
}

// TestUnoGraceThenPenalty 剩一张牌没喊 UNO：先给 3 秒宽限，
// 到点还没喊就按"被抓"罚摸两张。
func TestUnoGraceThenPenalty(t *testing.T) {
	r, clk := newUnoRoom(t)
	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	forceState(r, func(s *engine.State) {
		s.Hands[0] = []engine.Card{{ID: 10, Color: engine.Red, Kind: engine.Number, Num: 5}}
		s.Hands[1] = []engine.Card{
			{ID: 20, Color: engine.Blue, Kind: engine.Number, Num: 7},
			{ID: 21, Color: engine.Blue, Kind: engine.Number, Num: 8},
		}
		s.Discard = []engine.Card{{ID: 3, Color: engine.Red, Kind: engine.Number, Num: 3}}
		s.Active = engine.Red
		s.Turn = 0
		s.Deck = []engine.Card{
			{ID: 100, Color: engine.Blue, Kind: engine.Number, Num: 1},
			{ID: 101, Color: engine.Blue, Kind: engine.Number, Num: 2},
		}
	})

	// 阿甲打出最后一张，剩 0 张？不行——先给他两张，打一张留一张。
	forceState(r, func(s *engine.State) {
		s.Hands[0] = []engine.Card{
			{ID: 10, Color: engine.Red, Kind: engine.Number, Num: 5},
			{ID: 11, Color: engine.Red, Kind: engine.Number, Num: 6},
		}
	})

	if err := r.Act("fp-a", engine.PlayCard{Seat: 0, CardID: 10}); err != nil {
		t.Fatalf("出牌失败: %v", err)
	}

	snap, _ := r.SnapshotFor("fp-a")
	if obs(snap).UnoPending != 0 {
		t.Fatalf("漏喊 UNO 应该被挂起，实际 %d", obs(snap).UnoPending)
	}
	if snap.PendingLeftMS <= 0 {
		t.Fatalf("应该有倒计时，实际 %d", snap.PendingLeftMS)
	}

	// 宽限内按 u 就没事。
	if err := r.Act("fp-a", engine.CallUno{Seat: 0}); err != nil {
		t.Fatalf("喊 UNO 失败: %v", err)
	}
	clk.add(5 * time.Second)
	r.tick()
	if got := len(handOf(r, 0)); got != 1 {
		t.Fatalf("喊过 UNO 之后不该被罚，手牌 %d 张", got)
	}

	// 再来一次，这次不喊。
	forceState(r, func(s *engine.State) {
		s.Hands[0] = []engine.Card{
			{ID: 12, Color: engine.Red, Kind: engine.Number, Num: 7},
			{ID: 13, Color: engine.Red, Kind: engine.Number, Num: 8},
		}
		s.Hands[1] = []engine.Card{{ID: 22, Color: engine.Blue, Kind: engine.Number, Num: 9}}
		s.Discard = []engine.Card{{ID: 3, Color: engine.Red, Kind: engine.Number, Num: 3}}
		s.Active = engine.Red
		s.Turn = 0
		s.UnoPending = -1
		s.Deck = []engine.Card{
			{ID: 100, Color: engine.Blue, Kind: engine.Number, Num: 1},
			{ID: 101, Color: engine.Blue, Kind: engine.Number, Num: 2},
		}
	})
	if err := r.Act("fp-a", engine.PlayCard{Seat: 0, CardID: 12}); err != nil {
		t.Fatalf("出牌失败: %v", err)
	}

	clk.add(4 * time.Second) // 超过 3 秒宽限
	r.tick()

	if got := len(handOf(r, 0)); got != 3 {
		t.Fatalf("漏喊 UNO 到点应该罚摸两张（1+2=3），实际 %d 张", got)
	}
}

// handOf 读某个座位的手牌数（测试专用）。
func handOf(r *Room, seat int) []engine.Card {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]engine.Card(nil), r.state.(*engine.State).Hands[seat]...)
}

// TestMatchContinuesAcrossRounds 长局：一局打完按 s 开下一局，分数保留。
func TestMatchContinuesAcrossRounds(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	r := New("MATCH1", uno.New(engine.Rules{MatchTo: 100}), Options{
		TurnTimeout: time.Minute,
		Grace:       time.Minute,
		BotLag:      time.Second,
		Now:         clk.now,
		NoLoop:      true,
	})
	t.Cleanup(r.Close)

	mustJoin(t, r, "fp-a", "阿甲")
	mustJoin(t, r, "fp-b", "阿乙")
	mustStart(t, r, "fp-a")

	// 摆成"阿甲再出一张就赢"的局面，阿乙手里有 7 分的牌。
	forceState(r, func(s *engine.State) {
		s.Hands[0] = []engine.Card{{ID: 10, Color: engine.Red, Kind: engine.Number, Num: 5}}
		s.Hands[1] = []engine.Card{{ID: 20, Color: engine.Blue, Kind: engine.Number, Num: 7}}
		s.Discard = []engine.Card{{ID: 3, Color: engine.Red, Kind: engine.Number, Num: 3}}
		s.Active = engine.Red
		s.Turn = 0
	})

	if err := r.Act("fp-a", engine.PlayCard{Seat: 0, CardID: 10}); err != nil {
		t.Fatalf("出牌失败: %v", err)
	}
	snap, _ := r.SnapshotFor("fp-a")
	if snap.Phase != Over {
		t.Fatalf("一局打完应该是 Over，实际 %v", snap.Phase)
	}
	if obs(snap).Scores[0] != 7 {
		t.Fatalf("本局应该得 7 分，实际 %d", obs(snap).Scores[0])
	}

	// 房主开下一局：分数要保留，手牌重新发。
	if err := r.Start("fp-a"); err != nil {
		t.Fatalf("开下一局失败: %v", err)
	}
	snap, _ = r.SnapshotFor("fp-a")
	if snap.Phase != Playing {
		t.Fatalf("下一局应该回到 Playing，实际 %v", snap.Phase)
	}
	if obs(snap).Scores[0] != 7 {
		t.Fatalf("跨局分数应该保留，实际 %d", obs(snap).Scores[0])
	}
	if len(obs(snap).Hand) != 7 {
		t.Fatalf("下一局应该重新发 7 张，实际 %d", len(obs(snap).Hand))
	}
}
