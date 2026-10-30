package api

import "net/http"

func (s *Server) startSSHChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &body) {
		return
	}
	challenge, err := s.accounts.StartSSHChallenge(body.Username)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, http.StatusCreated, challenge)
}

func (s *Server) confirmSSHChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w, r) || !s.allow(w, r) {
		return
	}
	var body struct {
		Username  string `json:"username"`
		PublicKey string `json:"public_key"`
		Code      string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.accounts.ConfirmSSHChallenge(body.Username, body.PublicKey, body.Code); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pollSSHChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.AllowPollAttempt(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "rate_limited", "查询过于频繁，请稍后重试")
		return
	}
	var body struct {
		PollToken string `json:"poll_token"`
	}
	if !decode(w, r, &body) {
		return
	}
	result, err := s.accounts.PollSSHChallenge(body.PollToken)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}
