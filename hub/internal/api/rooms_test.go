package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/games/uno/protocol"
	"github.com/xingzitian/TTYFish/hub/internal/play"
	"github.com/xingzitian/TTYFish/hub/internal/room"
)

func gameFixture(t *testing.T) fixture {
	t.Helper()
	games, err := play.New([]play.Game{{ID: "uno", Rules: uno.New(engine.Rules{}), Codec: protocol.Codec{}}}, room.Options{NoLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(games.Close)
	return setup(t, Config{Games: games})
}
func loginPlayer(t *testing.T, f fixture, name string) string {
	t.Helper()
	if _, err := f.service.Register(name, "test-password-123", ""); err != nil {
		t.Fatal(err)
	}
	session, err := f.service.LoginPassword(name, "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	return session.AccessToken
}

func TestRoomAPILifecycleAndPrivateActions(t *testing.T) {
	f := gameFixture(t)
	alice := loginPlayer(t, f, "alice")
	bob := loginPlayer(t, f, "bob")
	outsider := loginPlayer(t, f, "outsider")
	f.request("GET", "/api/v1/games", "", nil, 401)
	f.request("GET", "/api/v1/games", alice, nil, 200)
	f.request("POST", "/api/v1/rooms", alice, map[string]string{"game_id": "missing"}, 400)
	created := read[play.Snapshot](t, f.request("POST", "/api/v1/rooms", alice, map[string]string{"game_id": "uno"}, 201))
	base := "/api/v1/rooms/" + created.Code
	f.request("POST", "/api/v1/rooms", alice, map[string]string{"game_id": "uno"}, 409)
	f.request("POST", "/api/v1/rooms/ZZZ999/join", bob, nil, 404)
	f.request("GET", base, outsider, nil, 404)
	f.request("POST", base+"/join", bob, nil, 200)
	f.request("POST", base+"/bots", bob, nil, 403)
	f.request("POST", base+"/start", bob, nil, 403)
	started := read[play.Snapshot](t, f.request("POST", base+"/start", alice, nil, 200))
	f.request("POST", base+"/join", outsider, nil, 409)
	raw, _ := json.Marshal(started.View)
	var view struct {
		Turn  int           `json:"turn"`
		Hand  []engine.Card `json:"hand"`
		Legal []struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"legal"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	actor := alice
	if view.Turn != started.MySeat {
		actor = bob
	}
	actorSnap := read[play.Snapshot](t, f.request("GET", base, actor, nil, 200))
	raw, _ = json.Marshal(actorSnap.View)
	_ = json.Unmarshal(raw, &view)
	if len(view.Hand) != 7 || len(view.Legal) == 0 {
		t.Fatal("missing private view or legal moves")
	}
	if strings.Contains(string(raw), `"hands"`) || strings.Contains(string(raw), `"deck"`) || strings.Contains(string(raw), `"seed"`) {
		t.Fatal("full engine state leaked")
	}
	f.request("POST", base+"/actions", actor, map[string]any{"type": "draw_card", "expected_revision": actorSnap.Revision, "payload": map[string]int{"seat": 99}}, 400)
	move := view.Legal[0]
	body := map[string]any{"type": move.Type, "payload": move.Payload, "expected_revision": actorSnap.Revision}
	f.request("POST", base+"/actions", actor, body, 200)
	replay := f.request("POST", base+"/actions", actor, body, 409)
	if !strings.Contains(replay.Body.String(), "stale_state") {
		t.Fatal("duplicate action not rejected as stale")
	}
	f.request("POST", base+"/chat", bob, map[string]int{"preset": 0}, 200)
	f.request("POST", base+"/restart-votes", bob, map[string]bool{"agree": true}, 200)
	f.request("GET", "/api/v1/me/room", alice, nil, 200)
	f.request("POST", base+"/leave", alice, nil, 204)
	f.request("GET", "/api/v1/me/room", alice, nil, 404)
}

func TestRoomSSEOverHTTPAndTakeover(t *testing.T) {
	f := gameFixture(t)
	token := loginPlayer(t, f, "alice")
	created := read[play.Snapshot](t, f.request("POST", "/api/v1/rooms", token, map[string]string{"game_id": "uno"}, 201))
	base := "/api/v1/rooms/" + created.Code
	server := httptest.NewUnstartedServer(f.handler)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()
	connect := func() (*http.Response, *bufio.Reader, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", server.URL+base+"/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatal("not an SSE stream")
		}
		return response, bufio.NewReader(response.Body), cancel
	}
	event := func(reader *bufio.Reader) (string, play.Snapshot) {
		kind := ""
		var snap play.Snapshot
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				if err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &snap); err != nil {
					t.Fatal(err)
				}
			}
			if line == "" {
				return kind, snap
			}
		}
	}
	first, reader, cancel := connect()
	defer cancel()
	defer first.Body.Close()
	kind, snap := event(reader)
	if kind != "snapshot" || snap.Code != created.Code {
		t.Fatal("missing initial full snapshot")
	}
	// An SSE stream must outlive the ordinary HTTP response write deadline.
	time.Sleep(150 * time.Millisecond)
	f.request("POST", base+"/bots", token, nil, 200)
	kind, snap = event(reader)
	if kind != "snapshot" || len(snap.Players) != 2 {
		t.Fatal("state mutation not pushed")
	}
	second, reader2, cancel2 := connect()
	defer cancel2()
	defer second.Body.Close()
	kind, _ = event(reader2)
	if kind != "snapshot" {
		t.Fatal("replacement stream has no initial state")
	}
	kind, snap = event(reader)
	if kind != "kicked" || !snap.Kicked {
		t.Fatal("old stream was not kicked")
	}
	first.Body.Close()
	cancel()
	current := read[play.Snapshot](t, f.request("GET", base, token, nil, 200))
	if !current.Players[current.MySeat].Connected {
		t.Fatal("old stream cleanup disconnected replacement")
	}
	// Revocation is checked on the next event, not only at connection setup.
	otherSession, err := f.service.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.Logout(token); err != nil {
		t.Fatal(err)
	}
	f.request("POST", base+"/chat", otherSession.AccessToken, map[string]int{"preset": 0}, 200)
	kind, _ = event(reader2)
	if kind != "auth_expired" {
		t.Fatalf("revoked session still streaming: %s", kind)
	}
	cancel2()
	second.Body.Close()
	f.request("GET", base+"/events", token, nil, 401)
}
