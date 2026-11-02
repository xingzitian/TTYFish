package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestPagingKeys 翻页主键是 - 和 =（输入法常用这两个键），方向键保留为别名。
func TestPagingKeys(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = nil
		for i := 0; i < 12; i++ {
			o.Hand = append(o.Hand, engine.Card{ID: i, Color: engine.Red, Kind: engine.Number, Num: i % 10})
		}
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	if out := strip(m.View()); !strings.Contains(out, "[-=]翻页") {
		t.Fatalf("CMD 行应该提示 -= 翻页：\n%s", out)
	}

	m.handleKey(keyOf("="))
	if out := strip(m.View()); !strings.Contains(out, "2/3") {
		t.Fatalf("按 = 应该翻到第 2 页：\n%s", out)
	}
	m.handleKey(keyOf("-"))
	if out := strip(m.View()); !strings.Contains(out, "1/3") {
		t.Fatalf("按 - 应该翻回第 1 页：\n%s", out)
	}

	// 方向键仍然能用（老习惯不该断）
	m.handleKey(keyOf("right"))
	if out := strip(m.View()); !strings.Contains(out, "2/3") {
		t.Fatalf("方向键右应该仍然可用：\n%s", out)
	}
	m.handleKey(keyOf("left"))
	if out := strip(m.View()); !strings.Contains(out, "1/3") {
		t.Fatalf("方向键左应该仍然可用：\n%s", out)
	}
}

var _ = client.Snapshot{}
var _ = fmt.Sprint
