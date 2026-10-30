// Package api exposes account, room and game services through JSON HTTP and SSE.
package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/play"
)

type Config struct {
	Games *play.Service
	// GatewayToken authenticates trusted SSH gateways, never browser clients.
	GatewayToken   string
	AllowedOrigins []string
}
type Server struct {
	accounts *account.Service
	cfg      Config
	mux      *http.ServeMux
}

func New(accounts *account.Service, cfg Config) (*Server, error) {
	if cfg.GatewayToken != "" && len(cfg.GatewayToken) < 32 {
		return nil, errors.New("SSH 网关凭证至少需要 32 字节")
	}
	for _, origin := range cfg.AllowedOrigins {
		if origin == "*" || origin == "null" || strings.TrimSpace(origin) == "" {
			return nil, errors.New("CORS 必须配置明确的来源")
		}
	}
	s := &Server{accounts: accounts, cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(openAPISpec)
	})
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("POST /api/v1/accounts", s.register)
	s.mux.HandleFunc("POST /api/v1/auth/password", s.password)
	s.mux.HandleFunc("POST /api/v1/internal/auth/ssh", s.keyLogin)
	s.mux.HandleFunc("POST /api/v1/auth/ssh/challenges", s.startSSHChallenge)
	s.mux.HandleFunc("POST /api/v1/auth/ssh/challenges/poll", s.pollSSHChallenge)
	s.mux.HandleFunc("POST /api/v1/internal/auth/ssh/challenges/confirm", s.confirmSSHChallenge)
	s.mux.HandleFunc("GET /api/v1/me", s.me)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	s.mux.HandleFunc("PUT /api/v1/me/password", s.setPassword)
	s.mux.HandleFunc("GET /api/v1/me/keys", s.keys)
	s.mux.HandleFunc("POST /api/v1/me/keys", s.addKey)
	s.mux.HandleFunc("DELETE /api/v1/me/keys/{fingerprint}", s.deleteKey)
	s.registerRoomRoutes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Add("Vary", "Origin")
			allowed := false
			for _, v := range s.cfg.AllowedOrigins {
				if v == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				failure(w, 403, "origin_denied", "不允许此来源")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		_, pattern := s.mux.Handler(r)
		if pattern == "" {
			// Preserve the router's method handling but normalize its errors to JSON.
			if isPath(s.mux, r) {
				failure(w, 405, "method_not_allowed", "请求方法不支持")
			} else {
				failure(w, 404, "not_found", "接口不存在")
			}
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func isPath(mux *http.ServeMux, r *http.Request) bool {
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		copy := r.Clone(r.Context())
		copy.Method = method
		_, p := mux.Handler(copy)
		if p != "" {
			return true
		}
	}
	return false
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type registration struct {
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
}
type keyRequest struct {
	PublicKey string `json:"public_key"`
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		failure(w, 415, "unsupported_media_type", "请使用 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(v); err == nil {
		var extra any
		err = d.Decode(&extra)
		if err == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		failure(w, 413, "request_too_large", "请求体不能超过 16 KiB")
	} else {
		failure(w, 400, "invalid_request", "JSON 格式错误、字段不支持或包含多个值")
	}
	return false
}
func bearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
func (s *Server) current(w http.ResponseWriter, r *http.Request) (account.Account, bool) {
	a, err := s.accounts.Current(bearer(r))
	if err != nil {
		report(w, err)
		return account.Account{}, false
	}
	return a, true
}
func (s *Server) allow(w http.ResponseWriter, r *http.Request) bool {
	if !s.accounts.AllowAttempt(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "rate_limited", "请求过于频繁，请稍后重试")
		return false
	}
	return true
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	var body registration
	if !decode(w, r, &body) {
		return
	}
	a, err := s.accounts.Register(body.Username, body.Password, body.PublicKey)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 201, a)
}
func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	var body credentials
	if !decode(w, r, &body) {
		return
	}
	session, err := s.accounts.LoginPassword(body.Username, body.Password)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, session)
}
func (s *Server) requireGateway(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.GatewayToken == "" {
		failure(w, 503, "gateway_disabled", "未启用 SSH 网关认证")
		return false
	}
	supplied, expected := sha256.Sum256([]byte(bearer(r))), sha256.Sum256([]byte(s.cfg.GatewayToken))
	if subtle.ConstantTimeCompare(supplied[:], expected[:]) != 1 {
		failure(w, 401, "gateway_unauthorized", "无效的网关凭证")
		return false
	}
	return true
}
func (s *Server) keyLogin(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w, r) {
		return
	}
	if !s.allow(w, r) {
		return
	}
	var body struct {
		Username  string `json:"username"`
		PublicKey string `json:"public_key"`
	}
	if !decode(w, r, &body) {
		return
	}
	session, err := s.accounts.LoginVerifiedKey(body.Username, body.PublicKey)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, session)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.current(w, r); ok {
		respond(w, 200, a)
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.current(w, r); !ok {
		return
	}
	if err := s.accounts.Logout(bearer(r)); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	if !s.allow(w, r) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.accounts.SetPassword(a.ID, body.Password); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	keys, err := s.accounts.Keys(a.ID)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": keys})
}
func (s *Server) addKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	var body keyRequest
	if !decode(w, r, &body) {
		return
	}
	key, err := s.accounts.AddKey(a.ID, body.PublicKey)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 201, key)
}
func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	if err := s.accounts.DeleteKey(a.ID, r.PathValue("fingerprint")); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(204)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func report(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, account.ErrInvalid):
		failure(w, 400, "invalid_request", err.Error())
	case errors.Is(err, account.ErrCredentials):
		failure(w, 401, "invalid_credentials", "账号或登录凭证错误")
	case errors.Is(err, account.ErrConflict):
		failure(w, 409, "account_conflict", "用户名或公钥已被使用")
	case errors.Is(err, account.ErrNotFound):
		failure(w, 404, "not_found", "资源不存在")
	case errors.Is(err, account.ErrLastCredential):
		failure(w, 409, "last_credential", "请先设置密码或添加其他公钥，再删除最后一把公钥")
	case errors.Is(err, account.ErrChallengeInvalid):
		failure(w, 409, "challenge_invalid", "确认码无效、已使用、已过期或与账号公钥不匹配")
	case isPlayError(err):
		reportPlayError(w, err)
	default:
		slog.Error("后端 API 请求失败", "err", err)
		failure(w, 500, "internal_error", "服务暂时不可用")
	}
}
