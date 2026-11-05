package api

import "net/http"

func (s *Server) startSSHChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.aggregate(w, r, false, "public-auth", s.rates.PublicAuthIP) {
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.accountBudget(w, r, false, "challenge-start", body.Username, false) {
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
	if !s.requireGateway(w, r) || !s.aggregate(w, r, true, "", 0) {
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
	if !s.accountBudget(w, r, true, "challenge-confirm", body.Username, false) {
		return
	}
	if err := s.accounts.ConfirmSSHChallenge(body.Username, body.PublicKey, body.Code); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pollSSHChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Device-ID") != "" && !s.checkWebOrigin(w, r) {
		return
	}
	if !s.aggregate(w, r, false, "poll-source", s.rates.PollIP) {
		return
	}
	var body struct {
		PollToken string `json:"poll_token"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.PollToken) == 64 && !s.take(w, bucket("poll-token", body.PollToken, s.rates.PollToken)) {
		return
	}
	result, err := s.accounts.PollSSHChallenge(body.PollToken, s.clientInfo(r, false))
	if err != nil {
		report(w, err)
		return
	}
	if result.Session != nil && r.Header.Get("X-Device-ID") != "" {
		if !s.issueWebCookie(w, r, result.Session) {
			return
		}
	}
	respond(w, http.StatusOK, result)
}
