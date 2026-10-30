// Package sshd 把游戏挂到一个 SSH 服务上：一条连接就是一个玩家。
//
// 这一层刻意做得很薄——认证、PTY、窗口尺寸转发、会话生命周期全交给 wish，
// 游戏逻辑一行都不碰。以后要加多人房间，改的是"会话怎么找到自己的房间"，
// 不是这一层的协议处理。
package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/muesli/termenv"
	gossh "golang.org/x/crypto/ssh"

	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/remote"
	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
)

// Config 是起一个 SSH 游戏服需要的全部参数。
type Config struct {
	Addr        string
	HostKeyPath string
	Game        registry.Entry
	// PrefsPath 可指定旧版偏好文件；为空时使用 host key 目录下的 prefs.json。
	PrefsPath string
	// BackendURL 是房间 API 的地址，例如 http://127.0.0.1:8081。
	BackendURL string
	// Accounts 是账号 API 的客户端；网关自己不查库。
	Accounts    *backend.Client
	BotDelay    time.Duration
	IdleTimeout time.Duration
	Mode        sdkui.ColorMode

	// Allow 返回 false 就拒绝这个公钥。nil 表示放行所有公钥。
	Allow func(fingerprint string) bool
}

// EnsureHostKey 确保 host key 存在。首次启动生成一次就够了——
// 每次重启都重新生成会让所有客户端报 host key 变更。
func EnsureHostKey(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("生成 host key: %w", err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "TTYFish host key")
	if err != nil {
		return fmt.Errorf("编码 host key: %w", err)
	}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

// Fingerprint 给出与 OpenSSH 一致的 SHA256 指纹，用来做玩家身份。
func Fingerprint(k ssh.PublicKey) string {
	if k == nil {
		return ""
	}
	sum := sha256.Sum256(k.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Identity 返回密钥身份，供无账号库的纯游戏模式使用。
// 启用账号库时，游戏身份使用 account:<id>，与登录凭证无关。
func Identity(k ssh.PublicKey) string {
	return Fingerprint(k)
}

// isInteractive 判断这条会话是不是"人坐在终端前玩"。
//
// 三个条件缺一不可：有 PTY、没有子系统请求、没有 exec 命令。
// SFTP 走 subsystem，scp / `ssh host true` 走 exec，二者都不是玩家。
func isInteractive(s ssh.Session) bool {
	if _, _, ok := s.Pty(); !ok {
		return false
	}
	if s.Subsystem() != "" {
		return false
	}
	return s.RawCommand() == ""
}

// New 组装 SSH 服务。返回的 server 需要调用方自己 ListenAndServe。
func New(cfg Config) (*ssh.Server, error) {
	if err := cfg.Game.Validate(); err != nil {
		return nil, err
	}
	if cfg.Addr == "" {
		cfg.Addr = ":2222"
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}
	if err := EnsureHostKey(cfg.HostKeyPath); err != nil {
		return nil, err
	}

	// 默认将个人偏好与 host key 放在同一目录。
	if cfg.PrefsPath == "" {
		cfg.PrefsPath = filepath.Join(filepath.Dir(cfg.HostKeyPath), "prefs.json")
	}
	pref := loadPrefs(cfg.PrefsPath)

	return wish.NewServer(
		wish.WithAddress(cfg.Addr),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		wish.WithIdleTimeout(cfg.IdleTimeout),
		wish.WithVersion("TTYFish"),
		// 横幅留空：认证失败统一回 Permission denied，不告诉对方"这个用户名存不存在"。
		// 钩子保留着，以后要加公告之类的再填。
		wish.WithBannerHandler(func(ctx ssh.Context) string { return "" }),
		// 密码登录：把用户名密码交给后端验证，网关不碰数据库。
		wish.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
			if cfg.Accounts == nil {
				return false
			}
			callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			p, err := cfg.Accounts.LoginPassword(callCtx, ctx.User(), password)
			if err != nil {
				slog.Warn("密码登录失败", "user", ctx.User(), "err", err)
				return false
			}
			storePlayer(ctx, p)
			return true
		}),

		// 公钥认证：SSH 握手已经证明对方持有私钥，这里再让后端确认
		// "这个用户名 + 这把公钥"属于同一个账号，然后换一个玩家令牌。
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			fp := Fingerprint(key)
			if cfg.Allow != nil && !cfg.Allow(fp) {
				slog.Warn("拒绝公钥", "user", ctx.User(), "fingerprint", fp, "remote", ctx.RemoteAddr())
				return false
			}
			if cfg.Accounts == nil {
				return false
			}
			// 这里只做"这把钥匙允许尝试"的判断，不调后端。
			//
			// SSH 协议会调这个回调两次（探测 + 验签），在网络请求里做认证既浪费
			// 限流预算，也会让协议层拿不到明确的成功信号。真正的身份校验放在
			// 会话里做（见 handler），失败时直接告诉玩家原因再断开。
			ctx.SetValue("publicKeyLine", strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))))
			slog.Info("公钥尝试", "user", ctx.User(), "fingerprint", fp, "remote", ctx.RemoteAddr())
			return true
		}),

		wish.WithMiddleware(
			// 网页登录的终端一侧：`ssh hub auth <码>` 走 exec 通道，
			// 在这里消化掉，绝不进游戏。
			func(next ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					if handleAuth(s, cfg.Accounts) {
						return
					}
					next(s)
				}
			},
			// 会话计时与终端信息，排障和容量观察都用得上。
			// 注意：它包在最外层，所以"会话结束"是程序真正退出的时刻，
			// 而不是 ssh 客户端断开连接的时刻。
			func(next ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					start := time.Now()
					term := ""
					if p, _, ok := s.Pty(); ok {
						term = p.Term
					}
					slog.Info("会话开始", "term", term, "user", s.User(), "identity", Identity(s.PublicKey()))
					next(s)
					slog.Info("会话结束", "at", time.Now().UnixMilli(), "ms", time.Since(start).Milliseconds())
				}
			},
			// 每条连接一个客户端模型。模型本身不持有牌局，
			// 只渲染房间推来的快照；座位由公钥指纹决定。
			bm.MiddlewareWithProgramHandler(func(s ssh.Session) *tea.Program {
				// 只有"交互式 shell"才是玩家。
				//
				// 这一步必须放在建模型之前：NewModel 里会 tryResume → 找到房间
				// → 订阅 → 顶号。而 MobaXterm 的文件侧栏、scp、`ssh host true`
				// 这类连接用的还是同一把密钥，如果也走建模型这条路，就会把
				// 正在玩的会话顶掉——表现就是"一开侧栏就被踢"。
				if !isInteractive(s) {
					slog.Info("跳过非交互会话",
						"user", s.User(), "subsystem", s.Subsystem(), "cmd", s.RawCommand())
					return nil
				}

				player := playerFrom(s.Context())
				if player.Token == "" {
					fmt.Fprintln(s, "登录状态丢失，请重新连接。")
					return nil
				}

				conn := remote.New(cfg.BackendURL, player.Token, cfg.Game.ID)
				// 断线重连：这个账号还在某个房间里就直接回去
				if resumed, err := conn.Resume(); err != nil {
					slog.Warn("恢复房间失败", "account", player.Account.ID, "err", err)
				} else if resumed {
					slog.Info("恢复房间", "account", player.Account.ID)
				}

				term := ""
				if pty, _, ok := s.Pty(); ok {
					term = pty.Term
				}
				key := "account:" + player.Account.ID
				model := cfg.Game.NewUI(registry.UIContext{
					Key: key, Name: player.Account.Username, Client: conn, Output: s, Term: term,
					Mode: cfg.Mode, ModeOf: pref.mode, SetMode: pref.setMode,
				})

				p := tea.NewProgram(model, append(bm.MakeOptions(s), tea.WithAltScreen())...)

				// 会话一结束就退订并通知房间"这人掉线了"。
				// 座位会保留 60 秒，期间由电脑代打，重连即接管。
				go func() {
					<-s.Context().Done()
					// 只断开 SSE：后端那边座位还在，重连能回来
					model.Close()
					conn.Close()
				}()
				return p
			}, termenv.ANSI256),
		),
	)
}

// handleAuth 处理 `ssh hub auth <码>`：这是网页登录在终端一侧的确认。
//
// 返回 true 表示"这条会话已经被消化掉了"，不要再进游戏。
func handleAuth(s ssh.Session, accounts *backend.Client) bool {
	cmd := strings.TrimSpace(s.RawCommand())
	if cmd == "" {
		return false
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 || fields[0] != "auth" {
		return false
	}
	if accounts == nil {
		fmt.Fprintln(s, "这台服务器没有开账号服务")
		return true
	}
	if len(fields) < 2 {
		fmt.Fprintln(s, "用法：ssh <主机> auth <网页上显示的码>")
		return true
	}

	code := strings.ToUpper(fields[1])
	if s.PublicKey() == nil {
		fmt.Fprintln(s, "此命令需要 SSH 公钥认证；请用注册时绑定的密钥重试。")
		return true
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pub := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(s.PublicKey())))
	if err := accounts.ConfirmChallenge(callCtx, s.User(), pub, code); err != nil {
		slog.Warn("挑战确认失败", "user", s.User(), "fingerprint", Fingerprint(s.PublicKey()), "err", err)
		fmt.Fprintf(s, "确认失败：%v\n", err)
		return true
	}

	fmt.Fprintf(s, "已确认，请回浏览器继续。\n")
	return true
}

// ---- 登录结果缓存（SSH 回调会问两次） ----

type loginEntry struct {
	player backend.Player
	at     time.Time
}

var (
	loginMu    sync.Mutex
	loginCache = map[string]loginEntry{}
)

// putLogin 记住一次成功的密钥登录，给紧接着的签名校验那一步复用。
func putLogin(key string, p backend.Player) {
	loginMu.Lock()
	defer loginMu.Unlock()
	now := time.Now()
	for k, v := range loginCache {
		if now.Sub(v.at) > 30*time.Second {
			delete(loginCache, k)
		}
	}
	loginCache[key] = loginEntry{player: p, at: now}
}

// takeLogin 取出并删除缓存（一次性，避免被后续连接复用）。
func takeLogin(key string) (backend.Player, bool) {
	loginMu.Lock()
	defer loginMu.Unlock()
	e, ok := loginCache[key]
	if !ok || time.Since(e.at) > 30*time.Second {
		delete(loginCache, key)
		return backend.Player{}, false
	}
	delete(loginCache, key)
	return e.player, true
}

// ---- 玩家令牌在会话里的存取 ----

// storePlayer 把认证结果塞进 SSH 会话上下文，后续 handler 用它建房间客户端。
func storePlayer(ctx ssh.Context, p backend.Player) {
	ctx.SetValue("playerToken", p.Token)
	ctx.SetValue("playerAccount", p.Account)
}

// playerFrom 取出当前会话的玩家身份。
func playerFrom(ctx ssh.Context) backend.Player {
	token, _ := ctx.Value("playerToken").(string)
	account, _ := ctx.Value("playerAccount").(backend.Account)
	return backend.Player{Token: token, Account: account}
}
