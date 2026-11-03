package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/games/uno/protocol"
	"github.com/xingzitian/TTYFish/hub/internal/account"
	"github.com/xingzitian/TTYFish/hub/internal/play"
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"github.com/xingzitian/TTYFish/sdk/game"
)

type fixedUNO struct{ uno.Game }

func (g fixedUNO) NewMatch(seeds []game.PlayerSeed, seed int64) any {
	s := g.Game.NewMatch(seeds, seed).(*engine.State)
	s.Turn = 0
	s.Active = engine.Red
	s.Discard = []engine.Card{{ID: 90, Color: engine.Red, Kind: engine.Number, Num: 3}}
	s.Hands = [][]engine.Card{
		{{ID: 91, Color: engine.Red, Kind: engine.Number, Num: 7}},
		{
			{ID: 92, Kind: engine.Wild},
			{ID: 93, Color: engine.Blue, Kind: engine.Number, Num: 9},
			{ID: 94, Color: engine.Green, Kind: engine.Number, Num: 9},
		},
	}
	return s
}

func TestPTAPIAndUNORewardEndToEnd(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "points.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	games, err := play.New([]play.Game{{
		ID: "uno", Rules: fixedUNO{uno.New(engine.Rules{})},
		Codec: protocol.Codec{}, Settle: play.UNORewards(st),
	}}, room.Options{NoLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(games.Close)
	accounts := account.New(st)
	server, err := New(accounts, Config{Games: games, GatewayToken: gatewaySecret})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{t, server.Handler(), accounts}
	alice := loginPlayer(t, f, "alice")
	bob := loginPlayer(t, f, "bob")
	f.request("GET", "/api/v1/me/points", "", nil, 401)
	f.request("GET", "/api/v1/me/points", "invalid", nil, 401)
	f.request("GET", "/api/v1/me/points", gatewaySecret, nil, 401)
	assertPoints := func(token string, tenths int64, pt string) {
		t.Helper()
		w := f.request("GET", "/api/v1/me/points", token, nil, 200)
		got := read[account.Points](t, w)
		if got.BalanceTenths != tenths || got.BalancePT != pt {
			t.Fatalf("balance=%+v want=%d %s", got, tenths, pt)
		}
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &shape); err != nil || len(shape) != 2 {
			t.Fatalf("incorrect response shape: %s", w.Body.String())
		}
	}
	assertPoints(alice, 0, "0.0")
	assertPoints(bob, 0, "0.0")
	created := read[play.Snapshot](t, f.request("POST", "/api/v1/rooms", alice, map[string]string{"game_id": "uno"}, 201))
	base := "/api/v1/rooms/" + created.Code
	f.request("POST", base+"/join", bob, nil, 200)
	for match, want := range []struct {
		tenths int64
		pt     string
	}{{68, "6.8"}, {136, "13.6"}} {
		started := read[play.Snapshot](t, f.request("POST", base+"/start", alice, nil, 200))
		body := map[string]any{
			"type": "play_card", "expected_revision": started.Revision,
			"payload": map[string]any{"card_id": 91, "color": 0, "uno": false},
		}
		ended := read[play.Snapshot](t, f.request("POST", base+"/actions", alice, body, 200))
		if ended.Phase != "over" {
			t.Fatalf("match %d not over: %+v", match, ended)
		}
		assertPoints(alice, want.tenths, want.pt)
		assertPoints(bob, 0, "0.0")
		f.request("POST", base+"/actions", alice, body, 409)
		f.request("GET", base, bob, nil, 200)
		games.Sweep()
		assertPoints(alice, want.tenths, want.pt)
	}
	a, err := accounts.Current(alice)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AwardUNO("small-reward", a.ID, 1); err != nil {
		t.Fatal(err)
	}
	assertPoints(alice, 137, "13.7")
	if err := st.AwardUNO("whole-reward", a.ID, 3); err != nil {
		t.Fatal(err)
	}
	assertPoints(alice, 140, "14.0")
	f.request("PUT", "/api/v1/me/points", alice, map[string]int{"balance_tenths": 999}, http.StatusMethodNotAllowed)
	f.request("POST", "/api/v1/auth/logout", alice, nil, 204)
	f.request("GET", "/api/v1/me/points", alice, nil, 401)
}

func TestPTReadFailureIsNotZeroBalance(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "points.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	accounts := account.New(unavailablePoints{Repository: st})
	_, err = accounts.Register("alice", "test-password-123", "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := accounts.LoginPassword("alice", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(accounts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{t, server.Handler(), accounts}
	w := f.request("GET", "/api/v1/me/points", session.AccessToken, nil, 500)
	got := read[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, w)
	if got.Error.Code != "internal_error" {
		t.Fatalf("database failure hidden as balance: %s", w.Body.String())
	}
}

type unavailablePoints struct{ account.Repository }

func (unavailablePoints) PTTenths(string) (int64, error) {
	return 0, errors.New("points database unavailable")
}
