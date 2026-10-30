package api

import "net/http"

func (s *Server) getKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	key, err := s.accounts.Key(a.ID, r.PathValue("fingerprint"))
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, key)
}

func (s *Server) renameKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	var body struct {
		Name *string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Name == nil {
		failure(w, 400, "invalid_request", "需要 name 字段，空字符串可清除名称")
		return
	}
	key, err := s.accounts.RenameKey(a.ID, r.PathValue("fingerprint"), *body.Name)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, key)
}
