package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func refreshCookieName(insecure bool) string {
	if insecure {
		return "ttyfish_refresh_dev"
	}
	return "__Host-ttyfish_refresh"
}

// Cookie-authenticated operations require an explicit, trusted browser origin.
func (s *Server) checkWebOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	if err == nil && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "https" || (s.cfg.InsecureWebCookies && u.Scheme == "http")) {
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		for _, allowed := range s.cfg.AllowedOrigins {
			if origin == allowed {
				return true
			}
		}
	}
	failure(w, http.StatusForbidden, "origin_denied", "不允许此来源")
	return false
}

func (s *Server) setWebCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName(s.cfg.InsecureWebCookies), Value: value, Path: "/", HttpOnly: true, Secure: !s.cfg.InsecureWebCookies, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (s *Server) clearWebCookie(w http.ResponseWriter) { s.setWebCookie(w, "", -1) }

func (s *Server) issueWebCookie(w http.ResponseWriter, r *http.Request, session *account.Session) bool {
	refresh, err := s.accounts.StartWebSession(session, r.Header.Get("X-Device-ID"))
	if err != nil {
		_ = s.accounts.Logout(session.AccessToken)
		report(w, err)
		return false
	}
	s.setWebCookie(w, refresh, int(account.WebRefreshTTL.Seconds()))
	return true
}

func (s *Server) refreshWeb(w http.ResponseWriter, r *http.Request) {
	if !s.checkWebOrigin(w, r) || !s.aggregate(w, r, false, "refresh", s.rates.PublicAuthIP) {
		return
	}
	c, err := r.Cookie(refreshCookieName(s.cfg.InsecureWebCookies))
	if err != nil || c.Value == "" {
		failure(w, 401, "invalid_credentials", "登录已过期")
		return
	}
	session, refresh, err := s.accounts.RefreshWebSession(c.Value)
	if err != nil {
		if errors.Is(err, account.ErrCredentials) {
			s.clearWebCookie(w)
		}
		report(w, err)
		return
	}
	s.setWebCookie(w, refresh, int(account.WebRefreshTTL.Seconds()))
	respond(w, http.StatusOK, session)
}
