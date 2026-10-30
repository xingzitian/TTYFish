// Package web 是平台的 HTTP 部分：登录页、账号页、挑战轮询。
//
// 用户先注册固定用户名，再通过 SSH 密钥挑战或账号密码登录。
package web

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/store"
)

const (
	cookieName   = "hub_session"
	sessionTTL   = 30 * 24 * time.Hour
	challengeTTL = 90 * time.Second
)

// Server 是 HTTP 处理器。
type Server struct {
	store   *store.Store
	tmpl    *template.Template
	mux     *http.ServeMux
	sshHost string // 显示"ssh 谁@哪台机器"用的提示
}

// New 组装路由。sshHint 是 SSH 参数，例如 "-p 2222 play@localhost"。
func New(st *store.Store, sshHint string) *Server {
	s := &Server{
		store:   st,
		tmpl:    template.Must(template.New("").Parse(templates)),
		mux:     http.NewServeMux(),
		sshHost: sshHint,
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/auth/start", s.handleStart)
	for _, path := range []string{"/register", "/auth/password", "/account/password", "/account/key"} {
		s.mux.HandleFunc(path, s.handleCredentials)
	}
	s.mux.HandleFunc("/auth/poll", s.handlePoll)
	s.mux.HandleFunc("/account", s.handleAccount)
	s.mux.HandleFunc("/logout", s.handleLogout)
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// ---- 页面数据 ----

type pageData struct {
	Title   string
	Handle  string
	SSHHint string
	Code    string
	Poll    string
	Error   string
	Keys    []keyView
	JustNow bool
}

type keyView struct {
	Fingerprint string
	Short       string
	Added       string
	Used        string
}

// ---- 处理器 ----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if acc, ok := s.current(r); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		_ = acc
		return
	}
	s.render(w, "login", pageData{Title: "登录", SSHHint: s.sshHost})
}

// handleStart 浏览器点"用 SSH 登录"：发一个挑战，页面显示短码并开始轮询。
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "用 POST", http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "跨站请求被拒", http.StatusForbidden)
		return
	}
	a, ok, err := s.store.ByHandle(r.FormValue("username"))
	if err != nil || !ok {
		http.Error(w, "账号错误，请先注册并填写固定用户名", http.StatusBadRequest)
		return
	}
	c, err := s.store.NewChallenge("login", a.ID, challengeTTL)
	if err != nil {
		slog.Error("发挑战失败", "err", err)
		http.Error(w, "服务器开小差了", http.StatusInternalServerError)
		return
	}
	s.render(w, "waiting", pageData{
		Title:   "等待终端确认",
		SSHHint: s.sshHint(a.Handle),
		Handle:  a.Handle,
		Code:    c.Code,
		Poll:    c.Poll,
	})
}

// handlePoll 浏览器轮询：确认了就发会话并让前端跳转。
func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	poll := r.URL.Query().Get("p")
	if poll == "" {
		http.Error(w, "缺少参数", http.StatusBadRequest)
		return
	}
	state, accountID, fp, ok, err := s.store.Poll(poll)
	if err != nil {
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeJSON(w, map[string]any{"state": "expired"})
		return
	}
	if state != "confirmed" {
		writeJSON(w, map[string]any{"state": "pending"})
		return
	}

	// 仅向挑战所属账号发放会话，不再自动注册。
	acc, found, err := s.store.ByFingerprint(fp)
	if err != nil || !found || acc.ID != accountID {
		http.Error(w, "账号与公钥不匹配", http.StatusForbidden)
		return
	}

	token, err := s.store.NewSession(acc.ID, r.UserAgent(), sessionTTL)
	if err != nil {
		http.Error(w, "发会话失败", http.StatusInternalServerError)
		return
	}
	s.setCookie(w, token)
	_ = s.store.Consume(poll)
	writeJSON(w, map[string]any{"state": "ok", "next": "/account"})
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	acc, ok := s.current(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ids, err := s.store.Identities(acc.ID)
	if err != nil {
		http.Error(w, "读取失败", http.StatusInternalServerError)
		return
	}
	keys := make([]keyView, 0, len(ids))
	for _, k := range ids {
		keys = append(keys, keyView{
			Fingerprint: k.Fingerprint,
			Short:       shortFingerprint(k.Fingerprint),
			Added:       k.CreatedAt.Format("2006-01-02 15:04"),
			Used:        k.LastUsed.Format("2006-01-02 15:04"),
		})
	}
	s.render(w, "account", pageData{
		Title:   "我的账号",
		Handle:  acc.Handle,
		SSHHint: s.sshHint(acc.Handle),
		Keys:    keys,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.store.DropSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- 辅助 ----

func (s *Server) current(r *http.Request) (store.Account, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return store.Account{}, false
	}
	acc, ok, err := s.store.SessionAccount(c.Value)
	if err != nil || !ok {
		return store.Account{}, false
	}
	return acc, true
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("渲染失败", "page", name, "err", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// sameOrigin 挡一下跨站 POST。浏览器带 Origin 时必须是自己的主机。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 非浏览器或同源 GET，交给 SameSite 兜底
	}
	h := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	return h == r.Host
}

func shortFingerprint(fp string) string {
	if len(fp) <= 24 {
		return fp
	}
	return fp[:24] + "…"
}
