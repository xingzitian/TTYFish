package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// 这个文件专门治"按了没反应"。
//
// 思路：把界面提示（大厅那行、牌桌按键表、极简 CMD 行）里写出来的按键，
// 在对应状态下一个一个按下去，断言"至少发生了一件事"——发了个动作、
// 换了模式、进了聊天、退出了房间。静默无响应直接算失败。
//
// 人肉点按钮永远会漏，这类东西就该穷举。

// recorder 记录界面对客户端发出的每一次调用。
type recorder struct {
	calls []string
	snap  client.Snapshot
	ch    chan client.Snapshot
}

func newRecorder(snap client.Snapshot) *recorder {
	return &recorder{snap: snap, ch: make(chan client.Snapshot, 1)}
}

func (r *recorder) record(name string) { r.calls = append(r.calls, name) }

func (r *recorder) Snapshot() client.Snapshot       { return r.snap }
func (r *recorder) Updates() <-chan client.Snapshot { return r.ch }
func (r *recorder) Join(code string) error          { r.record("join:" + code); return nil }
func (r *recorder) Leave() error                    { r.record("leave"); return nil }
func (r *recorder) Start() error                    { r.record("start"); return nil }
func (r *recorder) AddBot() error                   { r.record("addbot"); return nil }
func (r *recorder) Act(a any) error                 { r.record(fmt.Sprintf("act:%T", a)); return nil }
func (r *recorder) Chat(i int) error                { r.record("chat"); return nil }
func (r *recorder) VoteRestart(agree bool) error    { r.record("vote"); return nil }

// state 是一套"快照 + 模式"的组合，代表界面的一种处境。
type state struct {
	name    string
	snap    client.Snapshot
	minimal bool
}

func lobbyState(minimal bool) state {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	setObs(&snap, func(o *engine.Observation) { o.Hand = nil })
	return state{name: "大厅", snap: snap, minimal: minimal}
}

func playingState(minimal bool) state {
	snap := sampleSnap(20241029)
	// 给够 8 张牌，极简模式才有第二页可翻；否则"按左键没反应"是对的。
	setObs(&snap, func(o *engine.Observation) {
		o.Hand = nil
		for i := 0; i < 8; i++ {
			o.Hand = append(o.Hand, engine.Card{ID: i, Color: engine.Red, Kind: engine.Number, Num: i % 10})
		}
	})
	return state{name: "牌桌", snap: snap, minimal: minimal}
}

func (s state) model() (*Model, *recorder) {
	m := testModel(t0, 80, s.snap)
	m.minimal = s.minimal
	rec := newRecorder(s.snap)
	m.c = rec
	return m, rec
}

// t0 只是 testModel 需要一个 *testing.T；这里用不到它的能力。
var t0 = &testing.T{}

// TestAdvertisedKeysRespond 把界面上写出来的键全按一遍，允许的"没有调用"
// 只有一种情况：模型自身状态变了（切模式、翻页、进聊天、退出）。
func TestAdvertisedKeysRespond(t *testing.T) {
	type keyCase struct {
		key  string
		note string
	}

	// 大厅：s 开始 / b 加电脑 / i 界面 / t 聊天 / v 投票 / l 离开 / q 退出
	// 牌桌丰富：1-9 出牌 / d 摸 / p 过 / u 喊UNO / c 抓包 / t 聊天 / v 重开
	//          / m 颜色 / i 界面 / q 退出
	// 牌桌极简：1-5 出牌 / ←→ 翻页 / Tab 玩家 / 其余同上
	for _, s := range []state{lobbyState(false), lobbyState(true)} {
		for _, k := range []keyCase{{"s", "开始"}, {"b", "加电脑"}, {"t", "聊天"}, {"v", "投票"}, {"l", "离开"}, {"q", "退出"}, {"i", "界面"}} {
			m, rec := s.model()
			if s.minimal && k.key == "left" {
				m.page = 1 // 第一页按左键本来就该没反应
			}
			before := fingerprint(m)
			cmd := m.handleKey(keyOf(k.key))
			if len(rec.calls) == 0 && fingerprint(m) == before && cmd == nil {
				t.Errorf("%s 模式下大厅：按 %q（%s）毫无反应", modeName(s.minimal), k.key, k.note)
			}
		}
	}

	richKeys := []keyCase{
		{"2", "选牌"}, {"d", "摸牌"}, {"p", "过牌"}, {"u", "喊UNO"}, {"c", "抓包"},
		{"t", "聊天"}, {"v", "重开"}, {"m", "颜色"}, {"i", "界面"}, {"q", "退出"},
		{"enter", "出牌"},
	}
	compactKeys := append([]keyCase{}, richKeys...)
	compactKeys = append(compactKeys,
		keyCase{"left", "翻页"}, keyCase{"right", "翻页"}, keyCase{"tab", "玩家"})
	for i := range compactKeys {
		if compactKeys[i].key == "enter" {
			compactKeys[i].key = "3" // 极简里没有回车出牌，换成一张牌
		}
	}

	for _, s := range []state{playingState(false), playingState(true)} {
		keys := richKeys
		if s.minimal {
			keys = compactKeys
		}
		for _, k := range keys {
			m, rec := s.model()
			if s.minimal && k.key == "left" {
				m.page = 1 // 第一页按左键本来就该没反应
			}
			before := fingerprint(m)
			cmd := m.handleKey(keyOf(k.key))
			if len(rec.calls) == 0 && fingerprint(m) == before && cmd == nil {
				t.Errorf("%s 模式下牌桌：按 %q（%s）毫无反应", modeName(s.minimal), k.key, k.note)
			}
		}
	}
}

// TestNoPanicOnAnyKey 任何状态下按任何键都不能崩。
func TestNoPanicOnAnyKey(t *testing.T) {
	keys := []string{
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p",
		"q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
		"1", "2", "3", "4", "5", "6", "7", "8", "9",
		"tab", "left", "right", "enter", "esc", "ctrl+c", "ctrl+b",
	}
	states := []state{
		lobbyState(false), lobbyState(true),
		playingState(false), playingState(true),
	}
	for _, s := range states {
		for _, key := range keys {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s 模式下按 %q 崩了：%v", modeName(s.minimal), key, r)
					}
				}()
				m, _ := s.model()
				m.handleKey(keyOf(key))
				_ = m.View()
			}()
		}
	}
}

// TestEnteringKeysRespond 输房间码界面：回车、退格、Esc 都要有反应。
func TestEnteringKeysRespond(t *testing.T) {
	snap := sampleSnap(20241029)
	snap.Phase = client.Lobby
	snap.Code = ""
	m := testModel(t, 80, snap)
	rec := newRecorder(snap)
	m.c = rec
	m.entering = true

	m.codeInput.SetValue("ABC234")
	m.handleKey(keyOf("enter"))
	if len(rec.calls) == 0 {
		t.Fatal("输房间码界面按回车应该去加入房间")
	}
}

// fingerprint 把"界面状态"压成一个字符串，用来判断按键有没有造成变化。
func fingerprint(m *Model) string {
	return strings.Join([]string{
		fmt.Sprint(m.minimal), fmt.Sprint(m.chat), fmt.Sprint(m.boss),
		fmt.Sprint(m.choosing), fmt.Sprint(m.page), fmt.Sprint(m.cursor), fmt.Sprint(m.quitting),
		m.mode.Slug(), m.errMsg,
	}, "|")
}

func modeName(minimal bool) string {
	if minimal {
		return "极简"
	}
	return "丰富"
}

var _ = ui.LayoutRich
