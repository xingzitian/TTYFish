package uno

import (
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestPendingExposesUnoCatch 开了漏喊罚牌之后，平台侧应该收到"3 秒内可以抓"的义务，
// 超时动作是下一家去抓。
//
// 平台（hub/internal/room）靠这个把"漏喊"变成自动处罚；如果这里返回空，
// 抓包在线上就等于没开。
func TestPendingExposesUnoCatch(t *testing.T) {
	g := Game{Rules: engine.Rules{UnoPenalty: true}}
	s := engine.NewGame(2, 42, g.Rules)
	s.UnoPending = 1

	token, within, action := g.Pending(s)
	if token == "" {
		t.Fatal("漏喊 UNO 时应该报告一个限时义务")
	}
	if within <= 0 {
		t.Fatalf("义务应该有时间限制，实际 %v", within)
	}
	catch, ok := action.(engine.CatchUno)
	if !ok {
		t.Fatalf("超时动作应该是抓包，实际 %T", action)
	}
	if catch.Target != 1 {
		t.Fatalf("应该抓座位 1，实际 %d", catch.Target)
	}
	// 两人局里下家就是自己，这里只要求抓的人不是被抓的人
	if catch.Seat == catch.Target && s.SeatCount > 1 {
		t.Fatalf("不能自己抓自己：%+v", catch)
	}
}

// TestPendingOffWhenRuleDisabled 没开规则时不该报告任何义务。
func TestPendingOffWhenRuleDisabled(t *testing.T) {
	g := Game{Rules: engine.Rules{}}
	s := engine.NewGame(2, 42, g.Rules)
	s.UnoPending = 1

	if token, _, _ := g.Pending(s); token != "" {
		t.Fatalf("规则关闭时不该有义务，实际 %q", token)
	}
}
