// Package play owns game registrations and account-based room membership for the backend.
package play

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/game"
)

var (
	ErrInvalid    = errors.New("invalid_room_request")
	ErrMembership = errors.New("already_in_room")
	ErrNotFound   = errors.New("room_not_found")
	ErrForbidden  = errors.New("room_forbidden")
	ErrAction     = errors.New("invalid_action")
	ErrClosed     = errors.New("game_service_closed")
)

type Codec interface {
	DecodeAction(string, json.RawMessage, int) (any, error)
	EncodeView(any) (any, error)
}
type Game struct {
	ID    string
	Rules game.Game
	Codec Codec
}
type GameInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MinPlayers      int    `json:"min_players"`
	MaxPlayers      int    `json:"max_players"`
	ProtocolVersion int    `json:"protocol_version"`
}
type registered struct {
	Game
	registry *room.Registry
}
type membership struct {
	room      *room.Room
	game      *registered
	stream    uint64
	streaming bool
	lastSeen  time.Time
	cancel    func()
}
type Service struct {
	mu      sync.Mutex
	games   map[string]*registered
	rooms   map[string]*registered
	members map[string]*membership
	closed  bool
}

func New(games []Game, opts room.Options) (*Service, error) {
	s := &Service{games: make(map[string]*registered), rooms: make(map[string]*registered), members: make(map[string]*membership)}
	for _, g := range games {
		if g.ID == "" || g.Rules == nil || g.Codec == nil {
			s.Close()
			return nil, errors.New("游戏注册需要 ID、规则和协议适配器")
		}
		if _, exists := s.games[g.ID]; exists {
			s.Close()
			return nil, errors.New("游戏 ID 重复")
		}
		s.games[g.ID] = &registered{g, room.NewRegistry(g.Rules, opts)}
	}
	return s, nil
}
func identity(id string) string { return "account:" + id }
func (s *Service) Games() []GameInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]GameInfo, 0, len(s.games))
	for _, g := range s.games {
		items = append(items, GameInfo{g.ID, g.Rules.Name(), g.Rules.MinSeats(), g.Rules.MaxSeats(), 1})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

type Snapshot struct {
	ProtocolVersion int                    `json:"protocol_version"`
	GameID          string                 `json:"game_id"`
	Code            string                 `json:"code"`
	Revision        uint64                 `json:"revision"`
	Phase           string                 `json:"phase"`
	Players         []client.Player        `json:"players"`
	MySeat          int                    `json:"my_seat"`
	MaxSeats        int                    `json:"max_seats"`
	IsHost          bool                   `json:"is_host"`
	View            any                    `json:"view"`
	Timeline        []client.TimelineEntry `json:"timeline"`
	Vote            *client.Vote           `json:"vote"`
	Notice          string                 `json:"notice"`
	PendingLeftMS   int64                  `json:"pending_left_ms"`
	Kicked          bool                   `json:"kicked"`
}

func (m *membership) encode(snap client.Snapshot) (Snapshot, error) {
	var view any
	var err error
	if snap.View != nil {
		view, err = m.game.Codec.EncodeView(snap.View)
		if err != nil {
			return Snapshot{}, err
		}
	}
	phase := "lobby"
	if snap.Phase == client.Playing {
		phase = "playing"
	} else if snap.Phase == client.Over {
		phase = "over"
	}
	timeline := snap.Timeline
	if timeline == nil {
		timeline = []client.TimelineEntry{}
	}
	return Snapshot{1, m.game.ID, snap.Code, snap.Revision, phase, snap.Players, snap.MySeat, snap.MaxSeats, snap.IsHost, view, timeline, snap.Vote, snap.Notice, snap.PendingLeftMS, snap.Kicked}, nil
}
func (s *Service) currentLocked(id, code string) (*membership, error) {
	if s.closed {
		return nil, ErrClosed
	}
	m, ok := s.members[id]
	if !ok || m.room.IsClosed() || !m.room.HasMember(identity(id)) {
		delete(s.members, id)
		return nil, ErrNotFound
	}
	if code != "" && code != m.room.Code {
		return nil, ErrForbidden
	}
	m.lastSeen = time.Now()
	return m, nil
}
func (s *Service) snapshot(m *membership, id string) (Snapshot, error) {
	snap, err := m.room.SnapshotFor(identity(id))
	if err != nil {
		return Snapshot{}, err
	}
	return m.encode(snap)
}
func (s *Service) Current(id string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, "")
	if err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(m, id)
}
func (s *Service) Create(id, name, gameID string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Snapshot{}, ErrClosed
	}
	if _, err := s.currentLocked(id, ""); err == nil {
		return Snapshot{}, ErrMembership
	}
	g, ok := s.games[gameID]
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: 游戏不存在", ErrInvalid)
	}
	for i := 0; i < 10; i++ {
		rm, _, err := g.registry.Join("", identity(id), name)
		if err != nil {
			return Snapshot{}, err
		}
		if _, exists := s.rooms[rm.Code]; exists {
			rm.Close()
			continue
		}
		m := &membership{room: rm, game: g, lastSeen: time.Now()}
		s.rooms[rm.Code] = g
		s.members[id] = m
		return s.snapshot(m, id)
	}
	return Snapshot{}, errors.New("无法分配唯一房间码")
}
func validCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !room.ValidCode(code) {
		return "", fmt.Errorf("%w: 房间码须为 6 位字母数字", ErrInvalid)
	}
	return code, nil
}
func (s *Service) Join(id, name, code string) (Snapshot, error) {
	code, err := validCode(code)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Snapshot{}, ErrClosed
	}
	if m, err := s.currentLocked(id, ""); err == nil {
		if m.room.Code != code {
			return Snapshot{}, ErrMembership
		}
		if _, err = m.room.Join(identity(id), name); err != nil {
			return Snapshot{}, err
		}
		return s.snapshot(m, id)
	}
	g, ok := s.rooms[code]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	rm, err := g.registry.Enter(code, identity(id), name)
	if err != nil {
		return Snapshot{}, err
	}
	m := &membership{room: rm, game: g, lastSeen: time.Now()}
	s.members[id] = m
	return s.snapshot(m, id)
}
func (s *Service) Get(id, code string) (Snapshot, error) {
	code, err := validCode(code)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, code)
	if err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(m, id)
}
func (s *Service) Leave(id, code string) error {
	code, err := validCode(code)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, code)
	if err != nil {
		return err
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.room.Disconnect(identity(id))
	delete(s.members, id)
	return nil
}

func (s *Service) Mutate(id, code, kind string, value int, agree bool) (Snapshot, error) {
	code, err := validCode(code)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, code)
	if err != nil {
		return Snapshot{}, err
	}
	switch kind {
	case "start":
		err = m.room.Start(identity(id))
	case "bot":
		_, err = m.room.AddBotBy(identity(id))
	case "chat":
		err = m.room.Chat(identity(id), value)
	case "restart":
		err = m.room.VoteRestart(identity(id), agree)
	default:
		err = ErrInvalid
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrAction, err)
	}
	return s.snapshot(m, id)
}
func (s *Service) Act(id, code, kind string, payload json.RawMessage, revision uint64) (Snapshot, error) {
	code, err := validCode(code)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, code)
	if err != nil {
		return Snapshot{}, err
	}
	snap, err := m.room.SnapshotFor(identity(id))
	if err != nil {
		return Snapshot{}, err
	}
	action, err := m.game.Codec.DecodeAction(kind, payload, snap.MySeat)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err = m.room.ActAtRevision(identity(id), revision, action); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrAction, err)
	}
	return s.snapshot(m, id)
}

// Stream is one account's full-snapshot subscription. The newest stream takes over.
type Stream struct {
	Updates <-chan client.Snapshot
	Encode  func(client.Snapshot) (Snapshot, error)
	Close   func()
}

func (s *Service) Subscribe(id, name, code string) (Stream, error) {
	code, err := validCode(code)
	if err != nil {
		return Stream{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.currentLocked(id, code)
	if err != nil {
		return Stream{}, err
	}
	snap, err := m.room.SnapshotFor(identity(id))
	if err != nil {
		return Stream{}, err
	}
	if !snap.Players[snap.MySeat].Connected {
		if _, err = m.room.Join(identity(id), name); err != nil {
			return Stream{}, err
		}
	}
	updates, cancel, err := m.room.Subscribe(identity(id))
	if err != nil {
		return Stream{}, err
	}
	m.stream++
	generation := m.stream
	m.streaming = true
	m.cancel = cancel
	var once sync.Once
	closeStream := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			cancel()
			if s.members[id] == m && m.stream == generation {
				m.streaming = false
				m.lastSeen = time.Now()
				m.room.Disconnect(identity(id))
			}
		})
	}
	return Stream{updates, m.encode, closeStream}, nil
}

func (s *Service) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, m := range s.members {
		if !m.streaming && time.Since(m.lastSeen) > time.Minute {
			snap, err := m.room.SnapshotFor(identity(id))
			if err == nil && snap.Players[snap.MySeat].Connected {
				m.room.Disconnect(identity(id))
			}
		}
		if m.room.IsClosed() || !m.room.HasMember(identity(id)) {
			delete(s.members, id)
		}
	}
	for _, g := range s.games {
		g.registry.Sweep()
	}
	for code, g := range s.rooms {
		rm, ok := g.registry.Get(code)
		if !ok || rm.IsClosed() {
			delete(s.rooms, code)
		}
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for _, g := range s.games {
		g.registry.Close()
	}
	clear(s.members)
	clear(s.rooms)
}
