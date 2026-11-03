package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestFirstLoginAsksLayout 第一次登录（偏好里没记录）要先问一次界面密度。
func TestFirstLoginAsksLayout(t *testing.T) {
	saved := map[string]ui.Layout{}
	opts := Options{
		LayoutOf:  func(k string) (ui.Layout, bool) { l, ok := saved[k]; return l, ok },
		SetLayout: func(k string, l ui.Layout) { saved[k] = l },
	}

	m := NewModel(Identity{Key: "k1", Name: "n"}, testClient(sampleSnap(1)), opts, nil)
	if !m.needLayoutChoice {
		t.Fatal("没有偏好记录时应该进入选择状态")
	}
	out := strip(m.View())
	if !strings.Contains(out, "[SETUP]") || !strings.Contains(out, "[1]") || !strings.Contains(out, "[2]") {
		t.Fatalf("应该画出选择界面：\n%s", out)
	}

	// 选择 2 = 极简，并且记住
	m.handleKey(keyOf("2"))
	if m.needLayoutChoice {
		t.Fatal("选完之后不该再问")
	}
	if !m.minimal {
		t.Fatal("选 2 应该进极简模式")
	}
	if saved["k1"] != ui.LayoutCompact {
		t.Fatalf("选择应该写回偏好，实际 %v", saved["k1"])
	}
}

// TestSecondLoginDoesNotAsk 偏好里已经有记录就不再问。
func TestSecondLoginDoesNotAsk(t *testing.T) {
	opts := Options{
		LayoutOf:  func(k string) (ui.Layout, bool) { return ui.LayoutCompact, true },
		SetLayout: func(k string, l ui.Layout) {},
	}

	m := NewModel(Identity{Key: "k1", Name: "n"}, testClient(sampleSnap(1)), opts, nil)
	if m.needLayoutChoice {
		t.Fatal("已经有偏好就不该再问")
	}
	if !m.minimal {
		t.Fatal("应该恢复上次选的极简模式")
	}
}

func TestRoomUpdateUsesSharedSnapshotMessage(t *testing.T) {
	c := testClient(sampleSnap(1))
	m := NewModel(Identity{Key: "alice"}, c, Options{}, nil)
	snap := sampleSnap(2)
	snap.SettlementRevision = 1
	c.ch <- snap
	msg := m.Init()()
	if update, ok := msg.(client.SnapshotMsg); !ok || update.SettlementRevision != 1 {
		t.Fatalf("room update is not visible to gateway: %#v", msg)
	}
	m.Update(msg)
	if m.snap.SettlementRevision != 1 {
		t.Fatal("shared update not applied to game")
	}
}

// TestSetupIgnoresOtherKeys 选择界面里别的键不能把玩家放进牌桌。
func TestSetupIgnoresOtherKeys(t *testing.T) {
	opts := Options{
		LayoutOf:  func(k string) (ui.Layout, bool) { return ui.LayoutRich, false },
		SetLayout: func(k string, l ui.Layout) {},
	}
	rec := newRecorder(sampleSnap(1))
	m := NewModel(Identity{Key: "k1", Name: "n"}, rec, opts, nil)
	m.handleKey(keyOf("s"))
	if len(rec.calls) != 0 {
		t.Fatalf("选择界面不该触发任何游戏动作，实际 %v", rec.calls)
	}
	if !m.needLayoutChoice {
		t.Fatal("按 s 不该跳过选择")
	}
}

// TestCompactEnterAndChat 极简模式下输码界面和聊天菜单也要有极简版本。
func TestCompactEnterAndChat(t *testing.T) {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	snap.Code = ""
	m := testModel(t, 80, snap)
	m.minimal = true
	m.entering = true

	out := strip(m.View())
	if !strings.Contains(out, "[ROOM]") {
		t.Fatalf("极简输码界面应该有 ROOM 行：\n%s", out)
	}
	if strings.Count(out, "\n") > 1 {
		t.Errorf("极简输码界面最多两行：\n%s", out)
	}

	m.entering = false
	m.snap = sampleSnap(20241029)
	m.handleKey(keyOf("t"))
	if out := strip(m.View()); !strings.Contains(out, "[CHAT]") {
		t.Fatalf("极简聊天菜单应该用一行：\n%s", out)
	}
}
