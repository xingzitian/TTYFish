package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestCompactPileShowsActiveColor 打了万能牌之后，下家必须能从界面上看出
// 当前生效的是什么颜色——只画牌面 [W|+4] 的话没人知道该出什么。
func TestCompactPileShowsActiveColor(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Top = engine.Card{ID: 99, Color: engine.NoColor, Kind: engine.WildDrawFour}
		o.Active = engine.Blue
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	out := strip(m.View())
	var pile string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[PILE]") {
			pile = line
		}
	}
	if pile == "" {
		t.Fatalf("没有 PILE 行：\n%s", out)
	}
	if !strings.Contains(pile, "当前色") || !strings.Contains(pile, "B") {
		t.Fatalf("万能牌之后必须显示当前色（应为蓝 B）：%q", pile)
	}
}

// TestCompactPileColorFollowsActive 当前色跟着 obs.Active 走，不是跟着牌面。
func TestCompactPileColorFollowsActive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active engine.Color
		want   string
	}{
		{"红", engine.Red, "R"},
		{"黄", engine.Yellow, "Y"},
		{"绿", engine.Green, "G"},
		{"蓝", engine.Blue, "B"},
	} {
		snap := sampleSnap(20241029)
		setObs(&snap, func(o *engine.Observation) {
			o.Top = engine.Card{ID: 99, Color: engine.NoColor, Kind: engine.Wild}
			o.Active = tc.active
		})
		m := testModel(t, 80, snap)
		m.minimal = true

		pile := ""
		for _, line := range strings.Split(strip(m.View()), "\n") {
			if strings.Contains(line, "[PILE]") {
				pile = line
			}
		}
		// 满色模式下颜色是背景块（不带方括号），所以只断言"当前色"后面出现了对应字母
		if !strings.Contains(pile, "当前色") || !strings.Contains(pile, tc.want) {
			t.Errorf("%s：当前色没画对：%q", tc.name, pile)
		}
	}
}
