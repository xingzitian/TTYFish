package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/play"
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/sdk/client"
)

func (s *Server) registerRoomRoutes() {
	s.mux.HandleFunc("GET /api/v1/games", s.games)
	s.mux.HandleFunc("POST /api/v1/rooms", s.createRoom)
	s.mux.HandleFunc("GET /api/v1/me/room", s.currentRoom)
	s.mux.HandleFunc("GET /api/v1/rooms/{code}", s.getRoom)
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/join", s.joinRoom)
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/leave", s.leaveRoom)
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/start", s.roomCommand("start"))
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/bots", s.roomCommand("bot"))
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/chat", s.roomCommand("chat"))
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/restart-votes", s.roomCommand("restart"))
	s.mux.HandleFunc("POST /api/v1/rooms/{code}/actions", s.roomAction)
	s.mux.HandleFunc("GET /api/v1/rooms/{code}/events", s.roomEvents)
}
func (s *Server) gameService(w http.ResponseWriter) *play.Service {
	if s.cfg.Games == nil {
		failure(w, 503, "games_disabled", "未启用游戏服务")
	}
	return s.cfg.Games
}
func (s *Server) games(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.current(w, r); !ok {
		return
	}
	if g := s.gameService(w); g != nil {
		respond(w, 200, map[string]any{"items": g.Games(), "chat_presets": client.Presets})
	}
}
func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	var body struct {
		GameID string `json:"game_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	snap, err := g.Create(a.ID, a.Username, body.GameID)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 201, snap)
}
func (s *Server) currentRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	snap, err := g.Current(a.ID)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, snap)
}
func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	snap, err := g.Get(a.ID, r.PathValue("code"))
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, snap)
}
func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	snap, err := g.Join(a.ID, a.Username, r.PathValue("code"))
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, snap)
}
func (s *Server) leaveRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	if err := g.Leave(a.ID, r.PathValue("code")); err != nil {
		report(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) roomCommand(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.current(w, r)
		if !ok {
			return
		}
		g := s.gameService(w)
		if g == nil {
			return
		}
		var value int
		var agree bool
		if kind == "chat" {
			var body struct {
				Preset *int `json:"preset"`
			}
			if !decode(w, r, &body) {
				return
			}
			if body.Preset == nil {
				failure(w, 400, "invalid_room_request", "需要 preset")
				return
			}
			value = *body.Preset
		}
		if kind == "restart" {
			var body struct {
				Agree *bool `json:"agree"`
			}
			if !decode(w, r, &body) {
				return
			}
			if body.Agree == nil {
				failure(w, 400, "invalid_room_request", "需要 agree")
				return
			}
			agree = *body.Agree
		}
		snap, err := g.Mutate(a.ID, r.PathValue("code"), kind, value, agree)
		if err != nil {
			report(w, err)
			return
		}
		respond(w, 200, snap)
	}
}
func (s *Server) roomAction(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	var body struct {
		ExpectedRevision *uint64         `json:"expected_revision"`
		Type             string          `json:"type"`
		Payload          json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.ExpectedRevision == nil || body.Type == "" {
		failure(w, 400, "invalid_room_request", "需要 expected_revision 和 type")
		return
	}
	snap, err := g.Act(a.ID, r.PathValue("code"), body.Type, body.Payload, *body.ExpectedRevision)
	if err != nil {
		report(w, err)
		return
	}
	respond(w, 200, snap)
}

// roomEvents uses full snapshots, not deltas. Reconnect always starts with a fresh snapshot.
func (s *Server) roomEvents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.current(w, r)
	if !ok {
		return
	}
	g := s.gameService(w)
	if g == nil {
		return
	}
	stream, err := g.Subscribe(a.ID, a.Username, r.PathValue("code"))
	if err != nil {
		report(w, err)
		return
	}
	defer stream.Close()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // Override the normal HTTP write timeout for SSE.
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	send := func(event string, data any, id uint64) error {
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if id > 0 {
			if _, err = fmt.Fprintf(w, "id: %d\n", id); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return err
		}
		if err = rc.Flush(); err != nil {
			return err
		}
		_ = rc.SetWriteDeadline(time.Time{})
		return nil
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	authorized := func() bool { _, err := s.accounts.Current(bearer(r)); return err == nil }
	for {
		select {
		case <-r.Context().Done():
			return
		case snap, open := <-stream.Updates:
			if !open {
				return
			}
			if !authorized() {
				_ = send("auth_expired", map[string]string{"code": "invalid_credentials"}, 0)
				return
			}
			wire, err := stream.Encode(snap)
			if err != nil {
				return
			}
			event := "snapshot"
			if wire.Kicked {
				event = "kicked"
			}
			if send(event, wire, wire.Revision) != nil {
				return
			}
			if wire.Kicked {
				return
			}
		case <-ticker.C:
			if !authorized() {
				_ = send("auth_expired", map[string]string{"code": "invalid_credentials"}, 0)
				return
			}
			if send("heartbeat", map[string]int64{"server_time_ms": time.Now().UnixMilli()}, 0) != nil {
				return
			}
		}
	}
}

func isPlayError(err error) bool {
	for _, target := range []error{play.ErrInvalid, play.ErrMembership, play.ErrNotFound, play.ErrForbidden, play.ErrAction, play.ErrClosed, room.ErrNotFound, room.ErrNotIn, room.ErrCode, room.ErrFull, room.ErrStarted, room.ErrPhase, room.ErrNotHost} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
func reportPlayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrStale):
		failure(w, 409, "stale_state", err.Error())
	case errors.Is(err, room.ErrNotHost), errors.Is(err, play.ErrForbidden), errors.Is(err, room.ErrNotIn):
		failure(w, 403, "room_forbidden", "当前账号没有此房间的操作权限")
	case errors.Is(err, play.ErrNotFound), errors.Is(err, room.ErrNotFound):
		failure(w, 404, "room_not_found", "房间或当前成员关系不存在")
	case errors.Is(err, play.ErrInvalid), errors.Is(err, room.ErrCode):
		failure(w, 400, "invalid_room_request", err.Error())
	case errors.Is(err, play.ErrMembership):
		failure(w, 409, "already_in_room", "请先退出当前房间")
	case errors.Is(err, play.ErrClosed):
		failure(w, 503, "games_disabled", "游戏服务已停止")
	default:
		failure(w, 409, "invalid_action", err.Error())
	}
}
