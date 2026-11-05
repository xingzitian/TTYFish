package api

import (
	"net/http"
	"strconv"
)

func (s *Server) loginHistory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	channel := r.URL.Query().Get("channel")
	if channel != "web" && channel != "ssh" {
		failure(w, 400, "invalid_request", "channel 必须为 web 或 ssh")
		return
	}
	parse := func(name string, def, min, max int) (int, bool) {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			return def, true
		}
		n, err := strconv.Atoi(raw)
		return n, err == nil && n >= min && n <= max
	}
	offset, ok := parse("offset", 0, 0, 1000)
	if !ok {
		failure(w, 400, "invalid_request", "offset 范围为 0–1000")
		return
	}
	limit, ok := parse("limit", 10, 1, 50)
	if !ok {
		failure(w, 400, "invalid_request", "limit 范围为 1–50")
		return
	}
	items, total, err := s.accounts.LoginHistory(a.ID, channel, offset, limit)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "offset": offset, "limit": limit})
}
