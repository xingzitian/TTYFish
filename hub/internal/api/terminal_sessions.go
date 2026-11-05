package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/account"
)

func (s *Server) onlineTerminal(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	item, err := s.accounts.OnlineTerminal(a.ID)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"terminal": item})
}

func (s *Server) terminalIdentity(w http.ResponseWriter, r *http.Request) (string, bool) {
	gateway, ok := s.passwordGateway(w, r)
	if !ok {
		return "", false
	}
	if !gateway {
		failure(w, 401, "gateway_unauthorized", "需要网关认证")
		return "", false
	}
	// Each live terminal heartbeats independently; charging all heartbeats to
	// the shared gateway budget would disconnect healthy users at scale.
	if !s.take(w, bucket("terminal-lease", bearer(r), 60)) {
		return "", false
	}
	a, ok := s.current(w, r)
	if !ok {
		return "", false
	}
	return a.ID, true
}
func (s *Server) claimTerminal(w http.ResponseWriter, r *http.Request) {
	id, ok := s.terminalIdentity(w, r)
	if !ok {
		return
	}
	lease, err := s.accounts.ClaimTerminal(id, bearer(r))
	if err != nil {
		report(w, err)
		return
	}
	s.notifyTerminal(id)
	respond(w, http.StatusOK, map[string]string{"lease_id": lease})
}

func (s *Server) notifyTerminal(id string) {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	for ch := range s.terminalWatchers[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Server) terminalEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.terminalIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		Lease string `json:"lease_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	ch := make(chan struct{}, 1)
	s.terminalMu.Lock()
	if s.terminalWatchers[id] == nil {
		s.terminalWatchers[id] = make(map[chan struct{}]struct{})
	}
	s.terminalWatchers[id][ch] = struct{}{}
	s.terminalMu.Unlock()
	defer func() {
		s.terminalMu.Lock()
		delete(s.terminalWatchers[id], ch)
		if len(s.terminalWatchers[id]) == 0 {
			delete(s.terminalWatchers, id)
		}
		s.terminalMu.Unlock()
	}()
	if err := s.accounts.HeartbeatTerminal(id, bearer(r), body.Lease); err != nil {
		report(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	send := func(event string) error {
		payload, _ := json.Marshal(map[string]string{"status": event})
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil {
			return err
		}
		_ = rc.SetWriteDeadline(time.Time{})
		return nil
	}
	if send("ready") != nil {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if err := s.accounts.HeartbeatTerminal(id, bearer(r), body.Lease); err != nil {
				if errors.Is(err, account.ErrCredentials) {
					_ = send("revoked")
				}
				return
			}
		case <-ticker.C:
			if send("heartbeat") != nil {
				return
			}
		}
	}
}
func (s *Server) terminalCommand(w http.ResponseWriter, r *http.Request, release bool) {
	id, ok := s.terminalIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		Lease string `json:"lease_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	var err error
	if release {
		err = s.accounts.ReleaseTerminal(id, bearer(r), body.Lease)
	} else {
		err = s.accounts.HeartbeatTerminal(id, bearer(r), body.Lease)
	}
	if err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) heartbeatTerminal(w http.ResponseWriter, r *http.Request) {
	s.terminalCommand(w, r, false)
}
func (s *Server) releaseTerminal(w http.ResponseWriter, r *http.Request) {
	s.terminalCommand(w, r, true)
}
