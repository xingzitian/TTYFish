package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/xingzitian/TTYFish/games/uno"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/play"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/hub/internal/room"
	"github.com/xingzitian/TTYFish/hub/internal/store"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// TestHeaderShowsBalance 标题栏要显示真实 PT 余额；没查到时用 — 占位。
func TestHeaderShowsBalance(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "sakitami"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	r.Init()

	// 还没查到余额时是占位
	if view := strip(r.View()); !strings.Contains(view, "—pt") {
		t.Fatalf("没查到余额时应该显示占位：\n%s", view)
	}

	// 收到余额消息后换成真实值
	r.Update(pointsMsg{pt: "6.8"})
	if view := strip(r.View()); !strings.Contains(view, "6.8pt") {
		t.Fatalf("应该显示 6.8pt：\n%s", view)
	}
	if view := strip(r.View()); strings.Contains(view, "—pt") {
		t.Fatalf("查到之后不该再有占位：\n%s", view)
	}
}

// TestPointsQuerySkippedWithoutToken 没有后端/令牌时（预览模式）不能去发请求。
func TestPointsQuerySkippedWithoutToken(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "preview", Username: "预览"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	if cmd := r.refreshPoints(); cmd != nil {
		t.Fatal("没有令牌时不该发请求")
	}
}

// TestPointsErrorDoesNotClearBalance 查余额失败时保留上一次的值，别把数字弄丢。
func TestPointsErrorDoesNotClearBalance(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(registry.Entry{
		ID: "uno", Game: stubGame{},
		NewUI: func(registry.UIContext) registry.Model { return &fakeModel{} },
	}); err != nil {
		t.Fatal(err)
	}
	r := New(Deps{
		Registry: reg,
		Player:   backend.Player{Account: backend.Account{ID: "acc-1", Username: "x"}},
		Output:   &bytes.Buffer{},
		Mode:     ui.ColorNone,
	})
	r.Init()
	r.Update(pointsMsg{pt: "12.3"})
	r.Update(pointsMsg{err: errTest{}})

	if view := strip(r.View()); !strings.Contains(view, "12.3pt") {
		t.Fatalf("失败时应该保留旧值：\n%s", view)
	}
}

var _ = tea.Quit

func TestSettlementRefreshesPersistedPointsInsideRoom(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "points.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.Register("alice", "test-password-123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/me/points" {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queries++
		balance, err := st.PTTenths(a.ID)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"balance_tenths": balance, "balance_pt": fmt.Sprintf("%d.%d", balance/10, balance%10),
		})
	}))
	defer server.Close()
	identity := "account:" + a.ID
	rm := room.New("ABC234", pointsUNO{uno.New(engine.Rules{})}, room.Options{
		NoLoop: true, OnMatchEnd: play.UNORewards(st),
	})
	defer rm.Close()
	for _, player := range []string{identity, "account:bob"} {
		if _, err := rm.Join(player, player); err != nil {
			t.Fatal(err)
		}
	}
	updates, cancel, err := rm.Subscribe(identity)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	<-updates
	start := func() client.SnapshotMsg {
		t.Helper()
		if err := rm.Start(identity); err != nil {
			t.Fatal(err)
		}
		return client.SnapshotMsg(<-updates)
	}
	win := func() client.SnapshotMsg {
		t.Helper()
		if err := rm.Act(identity, engine.PlayCard{Seat: 0, CardID: 91}); err != nil {
			t.Fatal(err)
		}
		return client.SnapshotMsg(<-updates)
	}
	model := &modalModel{}
	r := newEscRoot(t, model)
	r.deps.Accounts = backend.New(server.URL, "")
	r.deps.Player.Token = "player-token"
	r.Update(r.refreshPoints()())
	oldQuery := pointsMsg{pt: "0.0", query: r.pointsQuery}
	if r.balancePT != "0.0" {
		t.Fatal("initial balance not loaded in room")
	}
	forwarded := model.forward
	if _, cmd := r.Update(start()); cmd != nil {
		t.Fatal("unsettled snapshot queried points")
	}
	snap := win()
	_, cmd := r.Update(snap)
	if cmd == nil {
		t.Fatal("successful settlement did not schedule points query")
	}
	r.Update(cmd())
	r.Update(oldQuery)
	if r.level != levelTable || r.roomClient == nil || !strings.Contains(strip(r.View()), "10.0pt") {
		t.Fatalf("persisted balance not shown without leaving: %s", strip(r.View()))
	}
	if model.forward != forwarded+2 {
		t.Fatal("points replies should not be forwarded to game; snapshots should")
	}
	for _, next := range []client.SnapshotMsg{snap, start()} {
		if _, cmd := r.Update(next); cmd != nil {
			t.Fatal("duplicate settlement or new match queried points again")
		}
	}
	// A coalesced snapshot may already belong to a later match.
	win()
	_, cmd = r.Update(start())
	r.Update(cmd())
	if r.balancePT != "20.0" || queries != 3 {
		t.Fatalf("balance=%s queries=%d", r.balancePT, queries)
	}
}

type pointsUNO struct{ uno.Game }

func (g pointsUNO) NewMatch(players []game.PlayerSeed, seed int64) any {
	s := g.Game.NewMatch(players, seed).(*engine.State)
	s.Turn = 0
	s.Active = engine.Red
	s.Discard = []engine.Card{{ID: 90, Color: engine.Red, Kind: engine.Number, Num: 3}}
	s.Hands = [][]engine.Card{
		{{ID: 91, Color: engine.Red, Kind: engine.Number, Num: 7}},
		{{ID: 92, Kind: engine.Wild}, {ID: 93, Kind: engine.Wild}},
	}
	return s
}
