package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func publicKey(text string) (string, string, error) {
	return account.ParsePublicKey(text)
}

func (s *Server) handleCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "用 POST", 405)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "跨站请求被拒", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单过大或格式错误", 400)
		return
	}
	switch r.URL.Path {
	case "/register":
		if !s.store.AllowPasswordAttempt(r.RemoteAddr) {
			http.Error(w, "操作过于频繁，请一分钟后再试", 429)
			return
		}
		fp, pub, err := publicKey(r.FormValue("public_key"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		a, err := s.store.Register(r.FormValue("username"), r.FormValue("password"), fp, pub)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		token, err := s.store.NewSession(a.ID, r.UserAgent(), sessionTTL)
		if err != nil {
			http.Error(w, "创建会话失败", 500)
			return
		}
		s.setCookie(w, token)
	case "/auth/password":
		if !s.store.AllowPasswordAttempt(r.RemoteAddr) {
			http.Error(w, "操作过于频繁，请一分钟后再试", 429)
			return
		}
		a, ok := s.store.AuthenticatePassword(r.FormValue("username"), r.FormValue("password"))
		if !ok {
			http.Error(w, "账号或密码错误", 401)
			return
		}
		token, err := s.store.NewSession(a.ID, r.UserAgent(), sessionTTL)
		if err != nil {
			http.Error(w, "创建会话失败", 500)
			return
		}
		s.setCookie(w, token)
	case "/account/password", "/account/key":
		a, ok := s.current(r)
		if !ok {
			http.Error(w, "请先登录", 401)
			return
		}
		var err error
		if r.URL.Path == "/account/password" {
			err = s.store.SetPassword(a.ID, r.FormValue("password"))
		} else {
			var fp, pub string
			fp, pub, err = publicKey(r.FormValue("public_key"))
			if err == nil && fp == "" {
				err = fmt.Errorf("请输入公钥")
			}
			if err == nil {
				err = s.store.Link(a.ID, fp, pub)
			}
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// Replace the configured SSH username with the registered account name.
func (s *Server) sshHint(handle string) string {
	parts := strings.Fields(s.sshHost)
	if len(parts) == 0 {
		return ""
	}
	last := len(parts) - 1
	host := parts[last]
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	parts[last] = handle + "@" + host
	return strings.Join(parts, " ")
}
