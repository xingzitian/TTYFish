package sshd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/game"
	gossh "golang.org/x/crypto/ssh"
)

// 这个游戏只实现大厅需要的那几个元信息；规则方法故意留空——
// 仅仅打开界面不应该访问游戏状态。
type mountGame struct{ game.Game }

func (mountGame) Name() string  { return "测试游戏" }
func (mountGame) MinSeats() int { return 1 }
func (mountGame) MaxSeats() int { return 4 }

type mountModel struct{ closed chan struct{} }

func (*mountModel) Init() tea.Cmd                         { return nil }
func (m *mountModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (*mountModel) View() string                          { return "test game" }
func (m *mountModel) Close()                              { close(m.closed) }

// ---- 假后端 ----

type fakeBackend struct {
	*httptest.Server

	mu       sync.Mutex
	gateway  string
	accounts map[string]fakeAccount
}

type fakeAccount struct {
	id       string
	username string
	password string
	keys     map[string]bool // 指纹
}

func newFakeBackend(t *testing.T, gatewayToken string) *fakeBackend {
	t.Helper()
	f := &fakeBackend{gateway: gatewayToken, accounts: map[string]fakeAccount{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/v1/internal/auth/ssh", f.handleKeyLogin)
	mux.HandleFunc("/api/v1/auth/password", f.handlePasswordLogin)
	mux.HandleFunc("/api/v1/rooms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(lobbySnapshot())
	})
	mux.HandleFunc("/api/v1/me/room", func(w http.ResponseWriter, r *http.Request) {
		// 没有房间：网关应该照常进大厅
		http.Error(w, `{"error":{"code":"not_found","message":"没有房间"}}`, http.StatusNotFound)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBackend) addAccount(id, username, password string, fps ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := map[string]bool{}
	for _, fp := range fps {
		keys[fp] = true
	}
	f.accounts[username] = fakeAccount{id: id, username: username, password: password, keys: keys}
}

func (f *fakeBackend) handleKeyLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.gateway {
		unauthorized(w)
		return
	}
	var body struct {
		Username  string `json:"username"`
		PublicKey string `json:"public_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.mu.Lock()
	acc, ok := f.accounts[body.Username]
	f.mu.Unlock()
	if !ok || !acc.keys[fingerprintOf(body.PublicKey)] {
		unauthorized(w)
		return
	}
	f.writePlayer(w, acc)
}

func (f *fakeBackend) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.mu.Lock()
	acc, ok := f.accounts[body.Username]
	f.mu.Unlock()
	if !ok || acc.password == "" || acc.password != body.Password {
		unauthorized(w)
		return
	}
	f.writePlayer(w, acc)
}

func (f *fakeBackend) writePlayer(w http.ResponseWriter, acc fakeAccount) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "tok-" + acc.id,
		"token_type":   "Bearer",
		"expires_in":   86400,
		"account": map[string]any{
			"id": acc.id, "username": acc.username,
			"display_name": acc.username, "created_at": "2026-10-30T12:00:00Z",
		},
	})
}

// lobbySnapshot 是最小的大厅快照，够网关建完客户端就行。
func lobbySnapshot() map[string]any {
	return map[string]any{
		"protocol_version": 1, "game_id": "test", "code": "ABC234", "revision": 1,
		"phase": "lobby", "players": []any{}, "my_seat": 0, "max_seats": 4,
		"is_host": true, "view": nil, "timeline": []any{}, "vote": nil,
		"notice": "", "pending_left_ms": 0, "kicked": false,
	}
}

func unauthorized(w http.ResponseWriter) {
	http.Error(w, `{"error":{"code":"invalid_credentials","message":"账号或登录凭证错误"}}`, http.StatusUnauthorized)
}

func fingerprintOf(authorizedKey string) string {
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(authorizedKey)))
	if err != nil {
		return ""
	}
	return Fingerprint(pub)
}

// ---- 测试脚手架 ----

func signer(t *testing.T) (gossh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s, strings.TrimSpace(string(gossh.MarshalAuthorizedKey(s.PublicKey())))
}

// startGateway 起一个指向假后端的网关。
func startGateway(t *testing.T, fb *fakeBackend, onMount func(registry.UIContext)) string {
	t.Helper()
	games := registry.New()
	if err := games.Register(registry.Entry{
		ID: "test", Game: mountGame{},
		NewUI: func(c registry.UIContext) registry.Model {
			if onMount != nil {
				onMount(c)
			}
			return &mountModel{closed: make(chan struct{})}
		},
	}); err != nil {
		t.Fatal(err)
	}

	srv, err := New(Config{
		HostKeyPath: filepath.Join(t.TempDir(), "host"),
		BackendURL:  fb.URL,
		Accounts:    backend.New(fb.URL, fb.gateway),
		Games:       games,
		NewRenderer: func(w io.Writer, term string) *lipgloss.Renderer { return lipgloss.NewRenderer(w) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func dial(t *testing.T, addr, user string, auth gossh.AuthMethod) (*gossh.Client, error) {
	t.Helper()
	return gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
}

// openShell 打开一个交互式会话，并返回标准输入——注意 stdin 必须在 Shell() 之前拿，
// 之后 x/crypto 会报 "StdinPipe after process started"。
func openShell(t *testing.T, c *gossh.Client) (*gossh.Session, io.WriteCloser) {
	t.Helper()
	session, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RequestPty("xterm", 24, 80, nil); err != nil {
		t.Fatal(err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	return session, stdin
}

// ---- 用例 ----

// TestGatewayAuthentication 网关只认后端确认过的"用户名 + 凭证"。
func TestGatewayAuthentication(t *testing.T) {
	fb := newFakeBackend(t, "gateway-secret")
	key, keyLine := signer(t)
	other, _ := signer(t)
	fb.addAccount("acc-1", "alice", "alice-password-123", fingerprintOf(keyLine))

	mounted := make(chan registry.UIContext, 4)
	addr := startGateway(t, fb, func(c registry.UIContext) { mounted <- c })

	// 密钥这条路径故意在会话里验身份（见 sshd.go 的说明），所以"钥匙不对"不会
	// 在握手阶段被拒——它连得上来，但拿不到界面。密码路径仍然在握手阶段就拒。
	for _, tc := range []struct {
		name, user string
		auth       gossh.AuthMethod
		wantMount  bool
	}{
		{"正确密钥", "alice", gossh.PublicKeys(key), true},
		{"正确密码", "alice", gossh.Password("alice-password-123"), true},
		{"用户名不对", "bob", gossh.PublicKeys(key), false},
		{"密钥不对", "alice", gossh.PublicKeys(other), false},
		{"密码不对", "alice", gossh.Password("wrong"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := dial(t, addr, tc.user, tc.auth)
			if err != nil {
				if tc.wantMount {
					t.Fatalf("应该能登录：%v", err)
				}
				return // 握手阶段就拒了，符合预期
			}
			defer c.Close()
			session, stdin := openShell(t, c)
			defer session.Close()

			if tc.wantMount {
				// 大厅里只有一个游戏，回车即进入
				time.Sleep(100 * time.Millisecond)
				_, _ = stdin.Write([]byte("\r"))
			}

			select {
			case ctx := <-mounted:
				if !tc.wantMount {
					t.Fatalf("不该挂载界面，实际拿到 %+v", ctx)
				}
				if ctx.Key != "account:acc-1" {
					t.Fatalf("界面应该拿到账号 ID，实际 %q", ctx.Key)
				}
				if ctx.Name != "alice" {
					t.Fatalf("昵称应该来自账号，实际 %q", ctx.Name)
				}
			case <-time.After(5 * time.Second):
				if tc.wantMount {
					t.Fatal("游戏界面没有被挂载")
				}
			}
		})
	}
}

// TestMountRequiresConfig 缺少游戏挂载时应该在创建 host key 之前就失败。
func TestMountRequiresConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("缺少游戏配置应该报错")
	}
}
