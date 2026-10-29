package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// fakeClient 是最小可用的 Client：只用来测界面的偏好恢复和按键。
type fakeClient struct {
	snap   client.Snapshot
	ch     chan client.Snapshot
	joined string
}

func newFakeClient() *fakeClient {
	return &fakeClient{ch: make(chan client.Snapshot, 1)}
}

func (f *fakeClient) Snapshot() client.Snapshot       { return f.snap }
func (f *fakeClient) Updates() <-chan client.Snapshot { return f.ch }
func (f *fakeClient) Join(code string) error          { f.joined = code; return nil }
func (f *fakeClient) Leave() error                    { return nil }
func (f *fakeClient) Start() error                    { return nil }
func (f *fakeClient) AddBot() error                   { return nil }
func (f *fakeClient) Act(a any) error                 { return nil }
func (f *fakeClient) Chat(i int) error                { return nil }
func (f *fakeClient) VoteRestart(agree bool) error    { return nil }

// TestColorModePersists 验证颜色深浅是玩家自己的选择：
// 按 m 切换之后要写回偏好，下次连接上来恢复成上次那个。
func TestColorModePersists(t *testing.T) {
	saved := map[string]ui.ColorMode{}
	opts := Options{
		ModeOf: func(k string) (ui.ColorMode, bool) {
			f, ok := saved[k]
			return f, ok
		},
		SetMode: func(k string, m ui.ColorMode) { saved[k] = m },
	}

	m := NewModel(Identity{Key: "k1", Name: "n"}, testClient(sampleSnap(1)), opts, nil)
	if m.mode != ui.ColorBlock {
		t.Fatalf("没设置过时应该用默认满色，实际 %v", m.mode)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	if m.mode != ui.ColorText {
		t.Fatalf("按 m 应该切到文字模式，实际 %v", m.mode)
	}
	if saved["k1"] != ui.ColorText {
		t.Fatalf("选择应该被写回偏好，实际 %v", saved["k1"])
	}

	// 换一条连接（同一个身份）应该恢复上次的选择。
	saved["k1"] = ui.ColorNone
	m2 := NewModel(Identity{Key: "k1", Name: "n"}, testClient(sampleSnap(1)), opts, nil)
	if m2.mode != ui.ColorNone {
		t.Fatalf("重连应该恢复上次选择，实际 %v", m2.mode)
	}

	// 另一个身份不受影响。
	m3 := NewModel(Identity{Key: "k2", Name: "n"}, testClient(sampleSnap(1)), opts, nil)
	if m3.mode != ui.ColorBlock {
		t.Fatalf("别的玩家不该被影响，实际 %v", m3.mode)
	}
}

// testClient 返回一个已经有一帧快照的假 Client，渲染测试用。
func testClient(snap client.Snapshot) *fakeClient {
	f := newFakeClient()
	f.snap = snap
	return f
}
