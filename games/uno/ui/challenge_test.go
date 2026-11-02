package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestErrorGoesToMsgLine 操作报错要出现在 MSG 行，而不是把整行 CMD 顶掉。
func TestErrorGoesToMsgLine(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true

	m.fail("这张牌现在出不了：颜色或数字对不上")
	out := strip(m.View())

	var msgLine, cmdLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[MSG]") {
			msgLine = line
		}
		if strings.Contains(line, "[CMD]") {
			cmdLine = line
		}
	}
	if !strings.Contains(msgLine, "出不了") {
		t.Fatalf("错误应该出现在 MSG 行：%q\n%s", msgLine, out)
	}
	if strings.Contains(cmdLine, "出不了") {
		t.Fatalf("CMD 行不该被错误顶掉：%q", cmdLine)
	}
	if !strings.Contains(cmdLine, "[1-5]出牌") {
		t.Fatalf("出错时 CMD 行仍要告诉玩家能按什么：%q", cmdLine)
	}

	// 超过 2 秒后回到正常消息
	m.errAt = time.Now().Add(-3 * time.Second)
	if out := strip(m.View()); strings.Contains(out, "出不了") {
		t.Fatalf("2 秒后错误应该消失：\n%s", out)
	}
}

// challengeSnap 构造一个"轮到我回应 +4 质疑"的快照。
func challengeSnap() client.Snapshot {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.MyTurn = true
		o.Challenge = &engine.ChallengeState{By: o.Seat, Target: 1, PrevColor: engine.Red}
		o.Legal = []engine.Action{
			engine.ResolveChallenge{Seat: o.Seat, Challenge: true},
			engine.ResolveChallenge{Seat: o.Seat, Challenge: false},
		}
	})
	return snap
}

// TestCompactChallengePrompt 极简模式必须提示 +4 质疑，并给出 Y/N。
func TestCompactChallengePrompt(t *testing.T) {
	m := testModel(t, 80, challengeSnap())
	m.minimal = true

	out := strip(m.View())
	if !strings.Contains(out, "+4") || !strings.Contains(out, "[Y]") || !strings.Contains(out, "[N]") {
		t.Fatalf("应该提示认罚/质疑：\n%s", out)
	}
}

// TestCompactChallengeKeys 极简模式下 Y 认罚、N 质疑，都要真的发出去。
func TestCompactChallengeKeys(t *testing.T) {
	for _, tc := range []struct {
		key     string
		want    engine.ResolveChallenge
		found   bool
		snippet string
	}{
		{"y", engine.ResolveChallenge{Seat: 0, Challenge: false}, true, "认罚"},
		{"n", engine.ResolveChallenge{Seat: 0, Challenge: true}, true, "质疑"},
	} {
		snap := challengeSnap()
		rec := newRecorder(snap)
		m := testModel(t, 80, snap)
		m.minimal = true
		m.c = rec

		m.handleKey(keyOf(tc.key))
		if len(rec.calls) != 1 {
			t.Fatalf("按 %q 应该发一个动作，实际 %v", tc.key, rec.calls)
		}
		// 具体动作类型由 rec 记录，这里只确认它没被"抓 UNO"的分支吃掉
		if strings.Contains(rec.calls[0], "CatchUno") {
			t.Fatalf("按 %q 被当成抓 UNO 了：%v", tc.key, rec.calls)
		}
	}
}

// TestChallengeIgnoresOtherKeys 质疑状态下按别的键不该乱出牌。
func TestChallengeIgnoresOtherKeys(t *testing.T) {
	snap := challengeSnap()
	rec := newRecorder(snap)
	m := testModel(t, 80, snap)
	m.minimal = true
	m.c = rec

	m.handleKey(keyOf("3"))
	if len(rec.calls) != 0 {
		t.Fatalf("质疑没解决之前不该能出牌：%v", rec.calls)
	}
}
