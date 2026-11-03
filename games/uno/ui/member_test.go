package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// lobbyAs 造一个"大厅里、我是成员但不是房主"的模型。
func lobbyAs(t *testing.T, host bool) *Model {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	snap.IsHost = host
	setObs(&snap, func(o *engine.Observation) { o.Hand = nil })
	return testModel(t, 80, snap)
}

// TestMemberKeysGiveFeedback 非房主按开局/加电脑要给出原因，而不是静默无反应。
//
// 这是一次真实投诉："在别人的房间里，感觉按键全坏了。"
// 原因就是这两个键只有房主能用，而界面对成员什么都没说。
func TestMemberKeysGiveFeedback(t *testing.T) {
	for _, k := range []string{"s", "b"} {
		m := lobbyAs(t, false)
		rec := newRecorder(m.snap)
		m.c = rec

		m.handleKey(keyOf(k))

		if len(rec.calls) != 0 {
			t.Fatalf("成员按 %q 不该真的发请求，实际 %v", k, rec.calls)
		}
		if m.errMsg == "" {
			t.Fatalf("成员按 %q 应该看到一句解释，实际什么都没提示", k)
		}
		if !strings.Contains(m.errMsg, "房主") {
			t.Fatalf("提示应该说清楚是房主才能按，实际 %q", m.errMsg)
		}
	}
}

// TestHostKeysStillWork 房主按 s / b 照旧发请求。
func TestHostKeysStillWork(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"s", "start"},
		{"b", "addbot"},
	} {
		m := lobbyAs(t, true)
		rec := newRecorder(m.snap)
		m.c = rec

		m.handleKey(keyOf(tc.key))

		if len(rec.calls) != 1 || rec.calls[0] != tc.want {
			t.Fatalf("房主按 %q 应该发 %s，实际 %v", tc.key, tc.want, rec.calls)
		}
	}
}

// TestMemberChatAndVoteStillWork 成员在大厅里聊天和投票是能用的（顺便守住）。
func TestMemberChatAndVoteStillWork(t *testing.T) {
	m := lobbyAs(t, false)
	rec := newRecorder(m.snap)
	m.c = rec

	m.handleKey(keyOf("t"))
	if !m.chat {
		t.Fatal("成员应该能打开聊天菜单")
	}
	m.chat = false

	m.handleKey(keyOf("v"))
	if len(rec.calls) != 1 || rec.calls[0] != "vote" {
		t.Fatalf("成员应该能投票，实际 %v", rec.calls)
	}
}
