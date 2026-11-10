package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kittens "github.com/xingzitian/TTYFish/games/explodingkittens"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	kitprotocol "github.com/xingzitian/TTYFish/games/explodingkittens/protocol"
	"github.com/xingzitian/TTYFish/games/uno"
	unoengine "github.com/xingzitian/TTYFish/games/uno/engine"
	unoprotocol "github.com/xingzitian/TTYFish/games/uno/protocol"
	"github.com/xingzitian/TTYFish/hub/internal/play"
	"github.com/xingzitian/TTYFish/hub/internal/room"
)

func TestKittensRemoteRoundTripUsesJoinedGameAndAuthenticatedSeat(t *testing.T) {
	service, err := play.New([]play.Game{
		{ID: "uno", Rules: uno.New(unoengine.Rules{}), Codec: unoprotocol.Codec{}},
		{ID: kittens.ID, Rules: kittens.Game{}, Codec: kitprotocol.Codec{}},
	}, room.Options{NoLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	created, err := service.Create("alice", "Alice", kittens.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Join("bob", "Bob", created.Code); err != nil {
		t.Fatal(err)
	}
	started, err := service.Mutate("alice", created.Code, "start", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/rooms/"+created.Code+"/actions" {
			http.Error(w, "unexpected endpoint", 400)
			return
		}
		var request struct {
			Type     string          `json:"type"`
			Payload  json.RawMessage `json:"payload"`
			Revision uint64          `json:"expected_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		requests++
		next, err := service.Act("alice", created.Code, request.Type, request.Payload, request.Revision)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(next)
	}))
	defer server.Close()
	// A user can join a different game by room code, or resume without selecting
	// a game first. The snapshot, not the originally selected menu, is authoritative.
	c := New(server.URL, "token", "uno")
	var initial wireSnapshot
	raw, _ := json.Marshal(started)
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	c.apply(initial)
	o, ok := c.Snapshot().View.(engine.Observation)
	if !ok || c.GameID() != kittens.ID || len(o.Hand) != 8 || o.Seat != 0 {
		t.Fatalf("wrong observation codec: %+v", c.Snapshot())
	}
	// Seat is deliberately bogus: the codec must omit it and the backend injects 0.
	if err := c.Act(engine.Action{Type: engine.Draw, Seat: 99}); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || c.Snapshot().Revision <= initial.Revision {
		t.Fatal("action did not traverse the backend")
	}
	if _, ok := c.Snapshot().View.(engine.Observation); !ok {
		t.Fatal("response fell back to UNO decoding")
	}
	if err := c.Act(unoengine.DrawCard{Seat: 0}); err == nil || requests != 1 {
		t.Fatal("wrong-game action transmitted")
	}
}

func TestRemoteDLCSettingUpdatesSnapshot(t *testing.T) {
	service, err := play.New([]play.Game{{ID: kittens.ID, Rules: kittens.Game{}, Codec: kitprotocol.Codec{}}}, room.Options{NoLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	created, err := service.Create("alice", "Alice", kittens.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/api/v1/rooms/" + created.Code + "/dlc"
		if r.Method != "POST" || (r.URL.Path != base && r.URL.Path != base+"/toggle") {
			http.Error(w, "unexpected endpoint", 400)
			return
		}
		var req struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var snap play.Snapshot
		var err error
		if r.URL.Path == base+"/toggle" {
			snap, err = service.ToggleDLC("alice", created.Code, req.ID)
		} else {
			snap, err = service.SetDLC("alice", created.Code, req.ID, req.Enabled)
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(snap)
	}))
	defer server.Close()
	c := New(server.URL, "token", kittens.ID)
	raw, _ := json.Marshal(created)
	var wire wireSnapshot
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	c.apply(wire)
	if err := c.SetDLC(kittens.ReverseDLC, true); err != nil {
		t.Fatal(err)
	}
	if snap := c.Snapshot(); len(snap.DLC) != 1 || snap.DLC[0] != kittens.ReverseDLC || snap.Revision <= created.Revision {
		t.Fatalf("bad remote DLC snapshot: %+v", snap)
	}
	if err := c.ToggleDLC(kittens.ReverseDLC); err != nil {
		t.Fatal(err)
	}
	if snap := c.Snapshot(); len(snap.DLC) != 0 {
		t.Fatalf("toggle did not disable DLC: %+v", snap.DLC)
	}
}

func TestRemoteViewDispatchRetainsUNOAndRejectsUnknownGame(t *testing.T) {
	if decodeGameView("unknown", json.RawMessage(`{"seat":0}`), 0) != nil {
		t.Fatal("unknown game silently decoded as UNO")
	}
	if decodeGameView(kittens.ID, json.RawMessage(`null`), 0) != nil {
		t.Fatal("null view became a game")
	}
	o := unoengine.Observation{}
	wire, err := (unoprotocol.Codec{}).EncodeView(o)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(wire)
	if _, ok := decodeGameView("uno", raw, 0).(unoengine.Observation); !ok {
		t.Fatal("UNO decoding broken")
	}
}

func TestKittensSpectatorViewRoundTripsToRemoteClient(t *testing.T) {
	s, err := engine.NewGame(3, 123)
	if err != nil {
		t.Fatal(err)
	}
	s.Alive[0] = false
	s.Deck = []engine.Card{{ID: 900, Kind: engine.ExplodingKitten}, {ID: 901, Kind: engine.Skip}}
	for _, seat := range []int{0, 1} {
		view, err := (kitprotocol.Codec{}).EncodeView(engine.Observe(s, seat))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		decoded, ok := decodeGameView(kittens.ID, raw, seat).(engine.Observation)
		if !ok {
			t.Fatal("remote failed to decode spectator observation")
		}
		if seat == 0 && (len(decoded.SpectatorTop) != 2 || decoded.SpectatorTop[0].Kind != engine.ExplodingKitten) {
			t.Fatalf("spectator view lost: %+v", decoded)
		}
		if seat == 1 && (len(decoded.SpectatorTop) != 0 || strings.Contains(string(raw), "spectator_top")) {
			t.Fatalf("live player received spectator data: %s", raw)
		}
	}
}
