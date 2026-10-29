package sshd

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/sdk/ui"
	gossh "golang.org/x/crypto/ssh"
)

// 此游戏故意不实现规则方法：仅打开界面不应访问游戏状态。
type mountGame struct{ game.Game }
type mountModel struct{ closed chan struct{} }

func (*mountModel) Init() tea.Cmd                         { return nil }
func (m *mountModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (*mountModel) View() string                          { return "test game" }
func (m *mountModel) Close()                              { close(m.closed) }

func TestMountCustomUI(t *testing.T) {
	mounted := make(chan registry.UIContext, 1)
	closed := make(chan struct{})
	srv, err := New(Config{
		HostKeyPath: filepath.Join(t.TempDir(), "host"),
		Mode:        ui.ColorText,
		Game: registry.Entry{ID: "test", Game: mountGame{}, NewUI: func(c registry.UIContext) registry.Model {
			mounted <- c
			return &mountModel{closed: closed}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve(ln)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := gossh.Dial("tcp", ln.Addr().String(), &gossh.ClientConfig{
		User: "player", Auth: []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// exec 探活不能创建游戏界面或顶掉玩家会话。
	probe, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	_ = probe.Run("true")
	probe.Close()
	select {
	case <-mounted:
		t.Fatal("non-interactive session created a game UI")
	default:
	}
	s, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RequestPty("xterm-256color", 24, 80, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-mounted:
		if c.Key != gossh.FingerprintSHA256(signer.PublicKey()) || c.Name != "player" || c.Term != "xterm-256color" || c.Mode != ui.ColorText {
			t.Fatalf("unexpected UI context: %+v", c)
		}
		if c.Client == nil || c.Output == nil || c.ModeOf == nil || c.SetMode == nil {
			t.Fatal("missing session dependencies")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UI factory was not called")
	}
	s.Close()
	conn.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("UI was not closed on disconnect")
	}
}

func TestMissingMountFailsBeforeHostKeyCreation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("missing game must be rejected")
	}
}
