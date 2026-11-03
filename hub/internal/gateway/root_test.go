package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// stubGame 只实现菜单需要的那几个元信息。
type stubGame struct{ game.Game }

func (stubGame) Name() string  { return "UNO" }
func (stubGame) MinSeats() int { return 2 }
func (stubGame) MaxSeats() int { return 6 }

func newRoot(t *testing.T) (*Root, *bytes.Buffer) {
	t.Helper()
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	r := New(Deps{
		Backend:  "http://127.0.0.1:1",
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "sakitami"}},
		Output:   out,
		Mode:     ui.ColorNone,
		Layout:   ui.LayoutRich,
		Version:  "v0.1.9",
	})
	// 模拟一次窗口尺寸，让菜单知道宽度
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return r, out
}

func press(r *Root, key string) {
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func pressEnter(r *Root) { r.Update(tea.KeyMsg{Type: tea.KeyEnter}) }
func pressEsc(r *Root)   { r.Update(tea.KeyMsg{Type: tea.KeyEsc}) }

// TestHomeMenu 主菜单三项，标题栏带版本号和用户名。
func TestHomeMenu(t *testing.T) {
	r, _ := newRoot(t)
	r.Init()

	view := strip(r.View())
	for _, want := range []string{"v0.1.9", "sakitami", "Home", "Game", "Settings", "Exit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("主菜单缺少 %q：\n%s", want, view)
		}
	}
}

// TestNavigateIntoGameList 进游戏列表、再进游戏菜单，Esc 逐层返回。
func TestNavigateIntoGameList(t *testing.T) {
	r, _ := newRoot(t)
	r.Init()

	pressEnter(r) // Game
	if r.level != levelGames {
		t.Fatalf("应该在游戏列表，实际 %v", r.level)
	}
	view := strip(r.View())
	if !strings.Contains(view, "UNO") || !strings.Contains(view, "个房间") {
		t.Fatalf("游戏列表缺少游戏名或房间数占位：\n%s", view)
	}
	if !strings.Contains(view, "Gamelist") {
		t.Fatalf("标题栏应该显示层级 Gamelist：\n%s", view)
	}

	pressEnter(r) // UNO
	if r.level != levelGame {
		t.Fatalf("应该在游戏菜单，实际 %v", r.level)
	}
	view = strip(r.View())
	for _, want := range []string{"创建房间", "加入房间", "浏览房间"} {
		if !strings.Contains(view, want) {
			t.Fatalf("游戏菜单缺少 %q：\n%s", want, view)
		}
	}
	// 浏览房间是占位，必须明确不可用
	if !strings.Contains(view, "暂不可用") {
		t.Fatalf("占位项要标出来：\n%s", view)
	}

	pressEsc(r)
	if r.level != levelGames {
		t.Fatalf("Esc 应该回到游戏列表，实际 %v", r.level)
	}
	pressEsc(r)
	if r.level != levelHome {
		t.Fatalf("再按 Esc 应该回到主菜单，实际 %v", r.level)
	}
}

// TestSettingsTogglesLayout 设置里回车切换显示效果，并写回偏好。
func TestSettingsTogglesLayout(t *testing.T) {
	saved := ui.LayoutRich
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry:  reg,
		Player:    backend.Player{Account: backend.Account{ID: "acc-1", Username: "x"}},
		Output:    &bytes.Buffer{},
		Mode:      ui.ColorNone,
		LayoutOf:  func(string) (ui.Layout, bool) { return saved, true },
		SetLayout: func(_ string, l ui.Layout) { saved = l },
	})
	r.Init()

	// Home → Settings（下移一格）
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r)
	if r.level != levelSettings {
		t.Fatalf("应该在设置，实际 %v", r.level)
	}
	if view := strip(r.View()); !strings.Contains(view, "显示效果：丰富") {
		t.Fatalf("初始应该是丰富：\n%s", view)
	}

	pressEnter(r) // 切到极简
	if saved != ui.LayoutCompact {
		t.Fatalf("应该写回极简，实际 %v", saved)
	}
	if view := strip(r.View()); !strings.Contains(view, "显示效果：极简") {
		t.Fatalf("标签应该跟着变：\n%s", view)
	}
}

func TestSettingsTogglesPasswordLogin(t *testing.T) {
	requested := false
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/me/password-login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer player-token" {
			t.Error("player token was not sent")
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		called = true
		requested = body.Enabled
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Accounts: backend.New(server.URL, ""),
		Registry: reg,
		Player: backend.Player{
			Token: "player-token",
			Account: backend.Account{
				ID: "acc-1", Username: "sakitami", PasswordLoginEnabled: true, HasSSHKey: true,
			},
		},
		Output: &bytes.Buffer{},
		Mode:   ui.ColorNone,
	})
	r.Init()
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r)
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r)

	view := strip(r.View())
	if !called || requested || r.deps.Player.Account.PasswordLoginEnabled {
		t.Fatalf("should disable password login via backend: requested=%t account=%+v", requested, r.deps.Player.Account)
	}
	if !strings.Contains(view, "密码登录：关闭") || !strings.Contains(view, "密码登录已关闭") {
		t.Fatalf("setting state and result should be shown:\n%s", view)
	}
}

func TestSettingsCannotDisableWithoutSSHKey(t *testing.T) {
	r, _ := newRoot(t)
	r.deps.Player.Account.PasswordLoginEnabled = true
	r.deps.Player.Account.HasSSHKey = false
	r.deps.Player.Token = "player-token"
	r.deps.Accounts = backend.New("http://127.0.0.1:1", "")
	r.Init()
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r)
	if r.menu.Items()[1].Enabled {
		t.Fatal("password login should not be disabled without an SSH key")
	}
	if view := strip(r.View()); !strings.Contains(view, "需先绑定 SSH 公钥") {
		t.Fatalf("settings should explain the key requirement:\n%s", view)
	}
}

// TestJoinScreen 加入房间：输入房间码，Esc 返回。
func TestJoinScreen(t *testing.T) {
	r, _ := newRoot(t)
	r.Init()
	pressEnter(r) // Game
	pressEnter(r) // UNO
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r) // 加入房间

	if r.level != levelJoin {
		t.Fatalf("应该在输入房间码的界面，实际 %v", r.level)
	}
	out := strip(r.View())
	if !strings.Contains(out, "房间号") {
		t.Fatalf("应该提示输入房间号：\n%s", out)
	}

	pressEsc(r)
	if r.level != levelGame {
		t.Fatalf("Esc 应该回到游戏菜单，实际 %v", r.level)
	}
}

// TestUnknownRoomKeepsError 房间码错了要留在输入界面并给出提示。
func TestUnknownRoomKeepsError(t *testing.T) {
	r, _ := newRoot(t)
	r.Init()
	pressEnter(r)
	pressEnter(r)
	r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("=")})
	pressEnter(r)

	for _, ch := range "ZZZZZZ" {
		r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	pressEnter(r)

	if r.level != levelJoin {
		t.Fatalf("房间码不存在时应该留在输入界面，实际 %v", r.level)
	}
	if r.joinErr == "" {
		t.Fatal("应该给出错误提示")
	}
}

func strip(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
