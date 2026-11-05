package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
)

// TestQAlwaysQuits 老板键任何时候都断开——选牌中也不能变成"取消"。
func TestQAlwaysQuits(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, _ := newTestModel(t, o, client.Playing)
	press(m, "1") // 进入组合候选
	if m.step != stepCombo {
		t.Fatalf("前置条件失败：step=%v", m.step)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("选牌中按 Q 也应该退出")
	}
}

// TestEscCancelsSelection Esc 只取消当前选择，不退房。
func TestEscCancelsSelection(t *testing.T) {
	o := playingObs(
		engine.Card{ID: 1, Kind: engine.TacoCat},
		engine.Card{ID: 2, Kind: engine.TacoCat},
	)
	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	press(m, "esc")
	if m.step != stepIdle {
		t.Fatalf("Esc 应该取消选择，实际 step=%v", m.step)
	}
	if c.acted != 0 {
		t.Fatal("取消时不该发动作")
	}
}

// TestRequestPageStartsFromFirstPage 进"选牌种"时页码要归零，
// 否则会沿用翻页后的页码，让玩家从第 10 个选项开始选。
func TestRequestPageStartsFromFirstPage(t *testing.T) {
	hand := []engine.Card{
		{ID: 1, Kind: engine.TacoCat},
		{ID: 2, Kind: engine.TacoCat},
		{ID: 3, Kind: engine.TacoCat},
	}
	o := playingObs(hand...)
	m, _ := newTestModel(t, o, client.Playing)

	// 先把"选目标"逼出来：三张同色
	press(m, "1")
	press(m, "2")
	press(m, "3")
	if m.step != stepTarget {
		t.Fatalf("三张应该进选目标，实际 step=%v", m.step)
	}
	press(m, "2") // 选座位 1（电脑A）
	press(m, "enter")
	if m.step != stepRequest {
		t.Fatalf("应该进选牌种，实际 step=%v", m.step)
	}
	if m.page != 0 {
		t.Fatalf("选牌种应该从第一页开始，实际 page=%d", m.page)
	}
	view := strip(m.View())
	if !strings.Contains(view, "拆") {
		t.Fatalf("第一页应该能选到拆弹：\n%s", view)
	}
}

// TestTripleSendsRequested 三张组合要带上 Requested。
func TestTripleSendsRequested(t *testing.T) {
	hand := []engine.Card{
		{ID: 1, Kind: engine.TacoCat},
		{ID: 2, Kind: engine.TacoCat},
		{ID: 3, Kind: engine.TacoCat},
	}
	o := playingObs(hand...)
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1")
	press(m, "2")
	press(m, "3")
	press(m, "2") // 目标：座位 1
	press(m, "enter")
	press(m, "1") // 要拆弹
	if c.acted != 1 {
		t.Fatalf("应该发出三张组合，实际 %d 次", c.acted)
	}
	if len(c.last.CardIDs) != 3 {
		t.Fatalf("应该是三张牌，实际 %v", c.last.CardIDs)
	}
	if c.last.Requested != engine.Defuse {
		t.Fatalf("应该索要拆弹，实际 %q", c.last.Requested)
	}
	if c.last.Target != 1 {
		t.Fatalf("目标应该是座位 1，实际 %d", c.last.Target)
	}
}

// TestFavorGoesThroughTarget 索要必须先选目标。
func TestFavorGoesThroughTarget(t *testing.T) {
	o := playingObs(engine.Card{ID: 4, Kind: engine.Favor})
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1")
	if m.step != stepTarget {
		t.Fatalf("索要应该先选目标，实际 step=%v", m.step)
	}
	if c.acted != 0 {
		t.Fatal("还没选目标就发动作了")
	}
	press(m, "2")
	press(m, "enter")
	if c.last.Type != engine.Play || c.last.Target != 1 {
		t.Fatalf("索要动作不对：%+v", c.last)
	}
}

// TestDeadStateIgnoresGameKeys 淘汰后按键不该发任何动作。
func TestDeadStateIgnoresGameKeys(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Skip})
	o.Alive = []bool{false, true, true}
	m, c := newTestModel(t, o, client.Playing)

	press(m, "1")
	press(m, "d")
	press(m, "p")
	if c.acted != 0 {
		t.Fatalf("淘汰后不该有动作，实际 %d 次", c.acted)
	}
}
