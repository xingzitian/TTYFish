// Package sshd 把游戏挂到一个 SSH 服务上：一条连接就是一个玩家。
//
// 这一层刻意做得很薄——认证、PTY、窗口尺寸转发、会话生命周期全交给 wish，
// 游戏逻辑一行都不碰。以后要加多人房间，改的是"会话怎么找到自己的房间"，
// 不是这一层的协议处理。
package sshd

import (
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
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/muesli/termenv"
	gossh "golang.org/x/crypto/ssh"

	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/session"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"github.com/xingzitian/TTYFish/sdk/game"
	sdkui "github.com/xingzitian/TTYFish/sdk/ui"
	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	unoUI "github.com/xingzitian/TTYFish/games/uno/ui"
)

// Config 是起一个 SSH 游戏服需要的全部参数。
type Config struct {
	Addr        string
	HostKeyPath string
	Rules       engine.Rules
	// Game 可以指定一个游戏实现（比如带 AI 的 UNO）；不填就按 Rules 造一个。
	Game game.Game
	// Store 是账号库。为 nil 时 `auth` 命令不可用（纯游戏模式）。
	Store       *store.Store
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
	block, err := gossh.MarshalPrivateKey(priv, "uno host key")
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

// Identity 是玩家身份：只看公钥指纹。
//
// 用户名不参与身份，它只是牌桌上的昵称。原因很实际：确认身份这件事，
// 用户唯一稳定拥有的是那把私钥。把用户名并进来，会出现"我换了个 ssh
// 用户名就回不到自己座位、牌局又已经开了所以进不去"的死角。
//
// 代价：几个人如果共用同一个密钥，服务端没法区分，互相会顶号。
// 这种情况请每人用自己的密钥（`ssh-keygen` 各自生成一份），
// 而不是靠改用户名。
func Identity(k ssh.PublicKey) string {
	return Fingerprint(k)
}

// sessionRenderer 为一条会话构造渲染器。
func sessionRenderer(s ssh.Session) *lipgloss.Renderer {
	term := ""
	if pty, _, ok := s.Pty(); ok {
		term = pty.Term
	}
	return unoUI.NewRenderer(s, term)
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

// RoomCount 返回当前房间数（诊断用）。没有服务在跑时返回 0。
func RoomCount() int {
	if r := lastRegistry.Load(); r != nil {
		return r.Count()
	}
	return 0
}

// lastRegistry 是最近一次 New 出来的房间表，只给诊断日志用。
var lastRegistry atomic.Pointer[room.Registry]

// New 组装 SSH 服务。返回的 server 需要调用方自己 ListenAndServe。
func New(cfg Config) (*ssh.Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":2222"
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}
	if err := EnsureHostKey(cfg.HostKeyPath); err != nil {
		return nil, err
	}

	// 房间表是进程级的：所有连接共享，房间码才能跨连接索引到同一局。
	g := cfg.Game
	if g == nil {
		g = uno.New(cfg.Rules)
	}
	reg := room.NewRegistry(g, room.Options{BotLag: cfg.BotDelay})
	lastRegistry.Store(reg)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for range t.C {
			reg.Sweep()
		}
	}()

	// 个人偏好跟 host key 放一起（一般是 /var/lib/uno）。
	pref := loadPrefs(filepath.Join(filepath.Dir(cfg.HostKeyPath), "uno-prefs.json"))

	return wish.NewServer(
		wish.WithAddress(cfg.Addr),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		wish.WithIdleTimeout(cfg.IdleTimeout),
		wish.WithVersion("uno"),

		// 只认公钥：既是安全边界，也是"你是谁"的来源之一。
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			fp := Fingerprint(key)
			if cfg.Allow != nil && !cfg.Allow(fp) {
				slog.Warn("拒绝公钥", "user", ctx.User(), "fingerprint", fp, "remote", ctx.RemoteAddr())
				return false
			}
			slog.Info("登录", "user", ctx.User(), "fingerprint", fp, "remote", ctx.RemoteAddr())
			return true
		}),

		wish.WithMiddleware(
			// 网页登录的终端一侧：`ssh hub auth <码>` 走 exec 通道，
			// 在这里消化掉，绝不进游戏。
			func(next ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					if handleAuth(s, cfg.Store) {
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

				fp := Identity(s.PublicKey())
				conn := session.New(reg, fp, s.User())
				conn.Resume() // 断线重连：这个身份还在房间里就直接回去

				model := unoUI.NewModel(unoUI.Identity{Key: fp, Name: s.User()}, conn, unoUI.Options{
					Rules:   cfg.Rules,
					Mode:    cfg.Mode,
					ModeOf:  pref.mode,
					SetMode: pref.setMode,
				}, sessionRenderer(s))

				p := tea.NewProgram(model, append(bm.MakeOptions(s), tea.WithAltScreen())...)

				// 会话一结束就退订并通知房间"这人掉线了"。
				// 座位会保留 60 秒，期间由电脑代打，重连即接管。
				go func() {
					<-s.Context().Done()
					model.Close()
				}()
				return p
			}, termenv.ANSI256),
		),
	)
}

// handleAuth 处理 `ssh hub auth <码>`：这是网页登录在终端一侧的确认。
//
// 返回 true 表示"这条会话已经被消化掉了"，不要再进游戏。
func handleAuth(s ssh.Session, st *store.Store) bool {
	cmd := strings.TrimSpace(s.RawCommand())
	if cmd == "" {
		return false
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 || fields[0] != "auth" {
		return false
	}
	if st == nil {
		fmt.Fprintln(s, "这台服务器没有开账号服务")
		return true
	}
	if len(fields) < 2 {
		fmt.Fprintln(s, "用法：ssh <主机> auth <网页上显示的码>")
		return true
	}

	code := strings.ToUpper(fields[1])
	fp := Identity(s.PublicKey())
	pub := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(s.PublicKey())))

	purpose, target, err := st.Confirm(code, fp)
	if err != nil {
		slog.Warn("挑战确认失败", "fingerprint", fp, "err", err)
		fmt.Fprintf(s, "确认失败：%v\n", err)
		return true
	}

	switch purpose {
	case "link":
		if err := st.Link(target, fp, pub); err != nil {
			fmt.Fprintf(s, "绑定公钥失败：%v\n", err)
			return true
		}
		slog.Info("绑定公钥", "fingerprint", fp, "account", target)
		fmt.Fprintln(s, "已把这把公钥绑定到你的账号，回浏览器继续。")
	default:
		acc, err := st.EnsureAccount(fp, pub, s.User())
		if err != nil {
			fmt.Fprintf(s, "登录失败：%v\n", err)
			return true
		}
		slog.Info("网页登录", "fingerprint", fp, "account", acc.ID, "handle", acc.Handle)
		fmt.Fprintf(s, "登录成功，欢迎 @%s\n回浏览器继续。\n", acc.Handle)
	}
	return true
}
