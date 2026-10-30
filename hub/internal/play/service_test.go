package play

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/games/uno/protocol"
	"github.com/xingzitian/TTYFish/hub/internal/room"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s, err := New([]Game{{ID: "uno", Rules: uno.New(engine.Rules{}), Codec: protocol.Codec{}}}, room.Options{NoLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestMembershipPermissionsAndStaleActions(t *testing.T) {
	s := testService(t)
	if _, err := s.Join("bob", "bob", "ABC234"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown code created a room")
	}
	created, err := s.Create("alice", "alice", "uno")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create("alice", "alice", "uno"); !errors.Is(err, ErrMembership) {
		t.Fatal("multiple active rooms")
	}
	joined, err := s.Join("bob", "bob", created.Code)
	if err != nil {
		t.Fatal(err)
	}
	if joined.MySeat == created.MySeat {
		t.Fatal("shared seats")
	}
	if _, err = s.Mutate("bob", created.Code, "bot", 0, false); !errors.Is(err, room.ErrNotHost) {
		t.Fatal("nonhost added bot")
	}
	if _, err = s.Mutate("bob", created.Code, "start", 0, false); !errors.Is(err, room.ErrNotHost) {
		t.Fatal("nonhost started")
	}
	if _, err = s.Get("stranger", created.Code); err == nil {
		t.Fatal("nonmember got private snapshot")
	}
	started, err := s.Mutate("alice", created.Code, "start", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if started.Phase != "playing" || started.View == nil {
		t.Fatal("game not started")
	}
	var observation struct {
		Seat  int `json:"seat"`
		Turn  int `json:"turn"`
		Legal []struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"legal"`
	}
	raw, _ := json.Marshal(started.View)
	if err = json.Unmarshal(raw, &observation); err != nil {
		t.Fatal(err)
	}
	actor := "alice"
	if observation.Turn != started.MySeat {
		actor = "bob"
	}
	current, err := s.Get(actor, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(current.View)
	_ = json.Unmarshal(raw, &observation)
	if len(observation.Legal) == 0 {
		t.Fatal("no legal moves")
	}
	move := observation.Legal[0]
	if _, err = s.Act(actor, created.Code, "draw_card", json.RawMessage(`{"seat":99}`), current.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatal("forged seat accepted")
	}
	next, err := s.Act(actor, created.Code, move.Type, move.Payload, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision <= current.Revision {
		t.Fatal("revision did not advance")
	}
	if _, err = s.Act(actor, created.Code, move.Type, move.Payload, current.Revision); !errors.Is(err, room.ErrStale) {
		t.Fatal("duplicate action applied")
	}
}

func TestStreamTakeoverDisconnectAndResume(t *testing.T) {
	s := testService(t)
	created, err := s.Create("alice", "alice", "uno")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate("alice", created.Code, "bot", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate("alice", created.Code, "start", 0, false); err != nil {
		t.Fatal(err)
	}
	first, err := s.Subscribe("alice", "alice", created.Code)
	if err != nil {
		t.Fatal(err)
	}
	<-first.Updates
	second, err := s.Subscribe("alice", "alice", created.Code)
	if err != nil {
		t.Fatal(err)
	}
	<-second.Updates
	kicked := false
	for snap := range first.Updates {
		kicked = kicked || snap.Kicked
	}
	if !kicked {
		t.Fatal("old stream not notified")
	}
	first.Close()
	current, err := s.Current("alice")
	if err != nil || !current.Players[current.MySeat].Connected {
		t.Fatal("old stream disconnected new stream")
	}
	second.Close()
	current, err = s.Current("alice")
	if err != nil || current.Players[current.MySeat].Connected {
		t.Fatal("disconnected stream not marked offline")
	}
	third, err := s.Subscribe("alice", "alice", created.Code)
	if err != nil {
		t.Fatal(err)
	}
	snap := <-third.Updates
	if !snap.Players[snap.MySeat].Connected || snap.MySeat != current.MySeat {
		t.Fatal("seat not restored")
	}
	if err = s.Leave("alice", created.Code); err != nil {
		t.Fatal(err)
	}
	third.Close()
	if _, err = s.Current("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatal("explicit leave did not clear membership")
	}
}

func TestUnusedPresenceLeaseAndShutdown(t *testing.T) {
	s := testService(t)
	if _, err := s.Create("alice", "alice", "uno"); err != nil {
		t.Fatal(err)
	}
	s.members["alice"].lastSeen = time.Now().Add(-2 * time.Minute)
	s.Sweep()
	if _, err := s.Current("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unused lobby membership never expired")
	}
	s.Close()
	if _, err := s.Create("alice", "alice", "uno"); !errors.Is(err, ErrClosed) {
		t.Fatal("created after shutdown")
	}
}
