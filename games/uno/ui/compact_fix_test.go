package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestCompactOnlyInPlaying 极简按键只在牌桌上生效：
// 大厅里 s/b/t/v/l 必须还是老键位，否则切到极简后开不了局。
func TestCompactOnlyInPlaying(t *testing.T) {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	setObs(&snap, func(o *engine.Observation) { o.Hand = nil })

	fake := newFakeClient()
	fake.snap = snap
	m := testModel(t, 80, snap)
	m.c = fake
	m.minimal = true

	// 大厅里不该被极简的按键表接管：数字和左右键不能吃掉 "s"
	m.handleKey(keyOf("s"))
	if !fake.started {
		t.Fatal("大厅里按 s 应该开局，说明极简按键表不该在大厅生效")
	}
}

// TestCompactErrorExpires 错误提示只占 CMD 行 2 秒，之后回到正常命令行。
func TestCompactErrorExpires(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true

	m.fail("这张牌现在出不了：颜色或数字对不上")
	if out := strip(m.View()); !strings.Contains(out, "出不了") {
		t.Fatalf("刚出错时应该显示提示：\n%s", out)
	}

	m.errAt = time.Now().Add(-3 * time.Second)
	out := strip(m.View())
	if strings.Contains(out, "出不了") {
		t.Fatalf("超过 2 秒应该收回提示：\n%s", out)
	}
	if !strings.Contains(out, "[1-5]出牌") {
		t.Fatalf("收回后应该回到正常命令行：\n%s", out)
	}
}

// TestCompactChatVisible 极简模式按 t 必须能看见聊天菜单。
func TestCompactChatVisible(t *testing.T) {
	m := testModel(t, 80, sampleSnap(20241029))
	m.minimal = true

	m.handleKey(keyOf("t"))
	if !m.chat {
		t.Fatal("按 t 应该进入聊天状态")
	}
	out := strip(m.View())
	if !strings.Contains(out, "快捷") && !strings.Contains(out, "1.") {
		t.Fatalf("极简模式必须把聊天菜单画出来：\n%s", out)
	}
}
