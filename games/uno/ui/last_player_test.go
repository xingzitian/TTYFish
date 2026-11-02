package tui

import (
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestLastPlayerAfterDrawTwo 你出 +2、我被跳过之后，"上一手"必须还是你。
//
// 回归：战报最后一条是「我 被加二，摸 2 张并被跳过」，那条的 Seat 是挨罚的人。
// 早先直接取最后一条，于是牌桌上写着"上一手: 我 打出"——背锅背得莫名其妙。
func TestLastPlayerAfterDrawTwo(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		o.Seat = 0
		card := engine.Card{ID: 42, Color: engine.Red, Kind: engine.DrawTwo}
		o.Top = card
		o.Log = []engine.LogEntry{
			{Seq: 1, Seat: 2, Card: &card, Text: "打出 红+2"},
			{Seq: 2, Seat: 0, Text: "被加二，摸 2 张并被跳过"},
		}
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
	// 出 +2 的是电脑B（座位 2），不是我（座位 0）
	if !strings.Contains(pile, "电脑B") {
		t.Fatalf("上一手应该是最初出牌的人：%q", pile)
	}
	if strings.Contains(pile, "你 打出") || strings.Contains(pile, "(上一手: 你") {
		t.Fatalf("不该把挨罚的人当成出牌的人：%q", pile)
	}
}

// TestLastPlayerSkipsNonCardEntries 中间夹着纯文字战报时也能找对。
func TestLastPlayerSkipsNonCardEntries(t *testing.T) {
	snap := sampleSnap(20241029)
	setObs(&snap, func(o *engine.Observation) {
		card := engine.Card{ID: 7, Color: engine.Blue, Kind: engine.Number, Num: 5}
		o.Top = card
		o.Log = []engine.LogEntry{
			{Seq: 1, Seat: 1, Card: &card, Text: "打出 蓝5"},
			{Seq: 2, Seat: -1, Text: "系统消息"},
			{Seq: 3, Seat: 3, Text: "摸了 1 张"},
		}
	})
	m := testModel(t, 80, snap)
	m.minimal = true

	pile := ""
	for _, line := range strings.Split(strip(m.View()), "\n") {
		if strings.Contains(line, "[PILE]") {
			pile = line
		}
	}
	if !strings.Contains(pile, "电脑A") {
		t.Fatalf("应该找到座位 1（电脑A）打出的那张：%q", pile)
	}
}
