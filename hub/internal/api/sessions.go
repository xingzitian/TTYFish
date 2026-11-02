package api

import (
	"net"
	"net/http"
	"strings"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func clientInfo(r *http.Request) account.ClientInfo {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := ""
	if parsed := net.ParseIP(host); parsed != nil {
		ip = parsed.String()
	}
	// This is descriptive client data, never an authentication signal.
	agent := []rune(strings.ToValidUTF8(r.UserAgent(), "�"))
	if len(agent) > 512 {
		agent = agent[:512]
	}
	return account.ClientInfo{SourceIP: ip, UserAgent: string(agent)}
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	items, err := s.accounts.Sessions(a.ID, bearer(r))
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	if err := s.accounts.RevokeSession(a.ID, bearer(r), r.PathValue("id")); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	count, err := s.accounts.RevokeOtherSessions(a.ID, bearer(r))
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, map[string]int64{"revoked_count": count})
}
