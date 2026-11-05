package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	kittens "github.com/xingzitian/TTYFish/games/explodingkittens"
	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

type rulesClient struct {
	fakeClient
	state *engine.State
}

func (c *rulesClient) Act(value any) error {
	a := value.(engine.Action)
	if err := engine.Apply(c.state, a); err != nil {
		return err
	}
	c.acted++
	c.last = a
	return nil
}

func (c *rulesClient) publish(m *Model) {
	c.snap.Revision++
	c.snap.View = engine.Observe(c.state, c.snap.MySeat)
	c.snap.Timeline = nil
	for _, e := range c.state.Log {
		c.snap.Timeline = append(c.snap.Timeline, client.TimelineEntry{Seat: e.Seat, Text: e.Text})
	}
	m.Update(client.SnapshotMsg(c.snap))
}

func pendingModel(t *testing.T, kind engine.Kind) (*Model, *rulesClient) {
	t.Helper()
	s, _ := engine.NewGame(3, 1)
	s.Turn = 1
	s.Hands = [][]engine.Card{
		{{ID: 100, Kind: engine.Nope}, {ID: 101, Kind: engine.Nope}},
		{{ID: 102, Kind: kind}, {ID: 103, Kind: engine.Nope}},
		{{ID: 104, Kind: engine.Skip}},
	}
	if err := engine.Apply(s, engine.Action{Type: engine.Play, Seat: 1, CardIDs: []int{102}}); err != nil {
		t.Fatal(err)
	}
	c := &rulesClient{state: s, fakeClient: fakeClient{
		snap: snapshot(t, engine.Observe(s, 0), client.Playing),
	}}
	c.snap.PendingLeftMS = 5000
	m := NewModel(c, Options{Layout: ui.LayoutCompact, Mode: ui.ColorNone}, nil)
	return m, c
}

func finishResponses(t *testing.T, s *engine.State) {
	t.Helper()
	for seat, alive := range s.Alive {
		if alive && s.Phase == engine.Responding && !s.Pending.Passed[seat] {
			if err := engine.Apply(s, engine.Action{Type: engine.Pass, Seat: seat}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNopeCancelsRealEffectAndHeldKeyCannotUndoIt(t *testing.T) {
	for _, kind := range []engine.Kind{engine.Attack, engine.Skip, engine.Shuffle, engine.SeeTheFuture} {
		t.Run(string(kind), func(t *testing.T) {
			m, c := pendingModel(t, kind)
			deck := slices.Clone(c.state.Deck)
			press(m, "n")
			press(m, "n") // Stale UI frame must not spend the second card.
			if c.acted != 1 || c.state.Pending.Nopes != 1 || len(c.state.Hands[0]) != 1 {
				t.Fatal("held N consumed multiple Nope cards")
			}
			c.publish(m)
			if !strings.Contains(m.hint(), "已被否决") || strings.Contains(m.hint(), "生效倒计时") {
				t.Fatalf("odd Nope still claims effect will apply: %s", m.hint())
			}
			press(m, "n") // Even after the acknowledgement, do not counter yourself.
			if c.acted != 1 {
				t.Fatal("player countered their own Nope")
			}
			finishResponses(t, c.state)
			c.publish(m)
			m.say()
			if c.state.Turn != 1 || c.state.UnderAttack || !slices.Equal(c.state.Deck, deck) ||
				len(c.state.Future[1]) != 0 || !strings.Contains(m.hint(), "效果已取消") {
				t.Fatalf("cancelled %s still took effect: %+v / %s", kind, c.state, m.hint())
			}
			if !m.revealAt.IsZero() {
				t.Fatal("cancelled prophecy started a reveal timer")
			}
		})
	}
}

func TestNopeAfterPassingAndCounterNopeRestoresEffect(t *testing.T) {
	m, c := pendingModel(t, engine.Attack)
	press(m, "p")
	c.publish(m)
	press(m, "N") // CMD's uppercase key also works; passing does not forbid Nope.
	if c.state.Pending.Nopes != 1 {
		t.Fatal("pass incorrectly forbids Nope")
	}
	c.publish(m)
	if err := engine.Apply(c.state, engine.Action{Type: engine.PlayNope, Seat: 1, CardIDs: []int{103}}); err != nil {
		t.Fatal(err)
	}
	c.publish(m)
	if !m.canNope() || !strings.Contains(m.hint(), "原效果将恢复") {
		t.Fatal("counter-Nope did not reopen response or show parity")
	}
	finishResponses(t, c.state)
	c.publish(m)
	if c.state.Turn != 2 || !c.state.UnderAttack || c.state.TurnsRemaining != 2 {
		t.Fatal("two Nopes failed to restore Attack")
	}
}

func TestProphecyViewTracksRealActorAndDeck(t *testing.T) {
	m, c := pendingModel(t, engine.SeeTheFuture)
	finishResponses(t, c.state)
	c.publish(m)
	if !strings.Contains(m.hint(), "正在偷看") {
		t.Fatal("resolved prophecy missing")
	}
	deadline := m.revealAt
	c.publish(m)
	if m.revealAt != deadline {
		t.Fatal("duplicate snapshot extended prophecy display")
	}
	if err := engine.Apply(c.state, engine.Action{Type: engine.Draw, Seat: 1}); err != nil {
		t.Fatal(err)
	}
	c.publish(m)
	if !m.revealAt.IsZero() || strings.Contains(m.hint(), "正在偷看") {
		t.Fatal("actor already drew but MSG still says peeking")
	}
	// Compressed snapshots can skip the Playing frame entirely. A response
	// update alone must not be interpreted as a resolved prophecy.
	m2, c2 := pendingModel(t, engine.SeeTheFuture)
	c2.publish(m2)
	if !m2.revealAt.IsZero() {
		t.Fatal("pending prophecy incorrectly treated as resolved")
	}
	c2.state.Pending.Nopes = 1
	c2.publish(m2)
	if !m2.revealAt.IsZero() {
		t.Fatal("Nope response incorrectly treated as resolved")
	}
	// Deliberately drop the Nope snapshot. Old Pending parity must not be used
	// to guess whether the effect actually resolved.
	m3, c3 := pendingModel(t, engine.SeeTheFuture)
	if err := engine.Apply(c3.state, engine.Action{Type: engine.PlayNope, Seat: 0, CardIDs: []int{100}}); err != nil {
		t.Fatal(err)
	}
	finishResponses(t, c3.state)
	c3.publish(m3)
	if !m3.revealAt.IsZero() || strings.Contains(m3.hint(), "正在偷看") {
		t.Fatal("dropped Nope snapshot made a cancelled prophecy appear successful")
	}
	// Conversely, a successful peek is visible as public metadata even if the
	// entire response window was skipped.
	o := playingObs()
	o.Turn = 1
	m4, c4 := newTestModel(t, o, client.Playing)
	o.PeekSeq, o.PeekSeat = 1, 1
	c4.snap.View = o
	m4.Update(client.SnapshotMsg(c4.snap))
	if !strings.Contains(m4.hint(), "正在偷看") || len(m4.revealed) != 0 {
		t.Fatal("compressed snapshots lost peek or leaked its contents")
	}
}

func TestOwnProphecyCanBeDismissedAndInvalidatesOnOtherAction(t *testing.T) {
	m, c := newTestModel(t, playingObs(), client.Playing)
	o := m.obs()
	o.Future = []engine.Card{{ID: 1, Kind: engine.ExplodingKitten}}
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	if !strings.Contains(m.cmdLine(100), "[P]结束预言") {
		t.Fatal("missing explicit close action")
	}
	press(m, "p")
	if !m.revealAt.IsZero() {
		t.Fatal("P did not close private peek")
	}
	o.Future = []engine.Card{{ID: 2, Kind: engine.Skip}}
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	o.Turn = 1
	o.Future = nil
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	if len(m.revealed) != 0 || !m.revealAt.IsZero() {
		t.Fatal("turn change left old private information visible")
	}
}

func TestTurnQueueSlidesOneCellAndSkipsEliminatedSeats(t *testing.T) {
	o := playingObs()
	o.Alive = []bool{true, false, true, true}
	o.HandCounts = []int{7, 0, 5, 6}
	m, c := newTestModel(t, o, client.Playing)
	c.snap.Players = append(c.snap.Players, client.Player{Seat: 3, Name: "电脑C", Connected: true})
	m.Update(client.SnapshotMsg(c.snap))
	o.Turn = 2
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	first := strip(m.compactTurn(110))
	if m.turnOffset >= 0 {
		t.Fatal("forward turn did not start a sliding window")
	}
	offset := m.turnOffset
	m.Update(tickMsg{model: m})
	if m.turnOffset != offset+1 || strip(m.compactTurn(110)) == first {
		t.Fatal("turn tape did not move by one cell")
	}
	for range 200 {
		m.View()
		if m.turnOffset == 0 {
			break
		}
		m.Update(tickMsg{model: m})
	}
	line := strip(m.compactTurn(110))
	if m.turnOffset != 0 || !strings.Contains(line, "*电脑B*(5)") ||
		strings.Contains(line, "电脑A") || strings.Count(line, ">>>") != 2 {
		t.Fatalf("settled three-player queue incorrect: %q", line)
	}
	center := strings.Index(line, "*电脑B*")
	if center < strings.Index(line, "你(7)") || center > strings.Index(line, "电脑C(6)") {
		t.Fatalf("current actor not in middle: %q", line)
	}
	narrow := strip(m.compactTurn(25))
	if !strings.Contains(narrow, "(5)") || lipgloss.Width(narrow) > 25 {
		t.Fatalf("narrow turn label broken: %q", narrow)
	}
}

func TestTabAddsUNOStyleRowsWithoutReplacingTurn(t *testing.T) {
	m, _ := newTestModel(t, playingObs(), client.Playing)
	before := strip(m.compactTurn(80))
	press(m, "tab")
	if !m.HasModal() || !strings.Contains(strip(m.View()), "[PLAYER]") ||
		strip(m.compactTurn(80)) != before {
		t.Fatal("Tab replaced the turn queue instead of showing player rows")
	}
	press(m, "tab")
	if m.showPlayers() {
		t.Fatal("Tab did not toggle closed")
	}
	press(m, "tab")
	press(m, "esc")
	if m.showPlayers() || m.HasModal() {
		t.Fatal("Esc did not close the player list")
	}
}

func TestContextCommandsMatchWorkingKeys(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Defuse})
	o.Phase = engine.Reinserting
	m, c := newTestModel(t, o, client.Playing)
	layout := m.opts.Layout
	press(m, "i")
	if m.step != stepDefuseCustom || m.opts.Layout != layout {
		t.Fatal("advertised I toggled layout instead of entering insertion")
	}
	press(m, "=")
	press(m, "esc")
	if m.step != stepDefusePos {
		t.Fatal("custom insertion Esc did not return to presets")
	}
	press(m, "i")
	press(m, "enter")
	if c.last.Type != engine.UseDefuse {
		t.Fatal("custom insertion Enter failed")
	}
	o.Turn = 1
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	if strings.Contains(m.cmdLine(100), "埋雷位置") || strings.Contains(m.hint(), "请") {
		t.Fatal("non-actor offered insertion controls")
	}
	o.Phase = engine.Playing
	c.snap.View = o
	m.Update(client.SnapshotMsg(c.snap))
	line := m.cmdLine(120)
	if strings.Contains(line, "[D]") || strings.Contains(line, "[G]") || strings.Contains(line, "[N]") {
		t.Fatal("idle player offered unavailable game actions")
	}
	press(m, "t")
	press(m, "2")
	if c.chat != 1 || m.step != stepIdle {
		t.Fatal("waiting player could not send a preset")
	}
}

func TestTargetSelectionPagesLivingOpponentsAndSendsImmediately(t *testing.T) {
	o := playingObs(engine.Card{ID: 1, Kind: engine.Favor})
	o.Alive, o.HandCounts = []bool{true, false, true, true, true}, []int{1, 0, 2, 3, 4}
	m, c := newTestModel(t, o, client.Playing)
	press(m, "1")
	if m.step != stepTarget || !strings.Contains(m.hint(), "索要") {
		t.Fatal("Favor did not start a target choice")
	}
	tokens := strings.Join(m.targetTokens(), " ")
	if strings.Contains(tokens, "电脑A") || !strings.Contains(tokens, "[-=]1/2") {
		t.Fatal("dead player selectable or target pagination absent")
	}
	press(m, "=")
	press(m, "1")
	if c.acted != 1 || c.last.Target != 4 || m.step != stepIdle {
		t.Fatalf("target digit did not immediately send correct seat: %+v", c.last)
	}
}

func TestColorsAndCompleteNarrowCommands(t *testing.T) {
	m, _ := newTestModel(t, playingObs(
		engine.Card{ID: 1, Kind: engine.Defuse},
		engine.Card{ID: 2, Kind: engine.Attack},
		engine.Card{ID: 3, Kind: engine.Favor},
		engine.Card{ID: 4, Kind: engine.Nope},
		engine.Card{ID: 5, Kind: engine.Skip},
	), client.Playing)
	renderer := lipgloss.NewRenderer(&strings.Builder{})
	renderer.SetColorProfile(termenv.ANSI256)
	m.renderer = renderer
	for _, mode := range []ui.ColorMode{ui.ColorNone, ui.ColorText, ui.ColorBlock} {
		m.opts.Mode = mode
		for _, kind := range []engine.Kind{engine.Nope, engine.Attack, engine.Skip, engine.Favor, engine.Shuffle, engine.SeeTheFuture} {
			if got := m.style(kind); got.Bg != "208" || got.Fg != "16" {
				t.Fatalf("function color not orange: %s %+v", kind, got)
			}
		}
		for _, width := range []int{40, 60, 80, 120} {
			m.width = width
			for _, layout := range []ui.Layout{ui.LayoutCompact, ui.LayoutRich} {
				m.opts.Layout = layout
				view := strip(m.View())
				for _, want := range []string{"[D]摸牌", "[Tab]玩家", "[-=]翻页", "5."} {
					if !strings.Contains(view, want) {
						t.Fatalf("hidden usable control %q at width=%d mode=%d layout=%d:\n%s", want, width, mode, layout, view)
					}
				}
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatalf("overflow at width=%d: %q", width, line)
					}
				}
			}
		}
	}
	colors := map[lipgloss.Color]bool{}
	for _, kind := range []engine.Kind{engine.TacoCat, engine.Cattermelon, engine.HairyPotatoCat, engine.BeardCat, engine.RainbowCat} {
		color := m.style(kind).Bg
		if colors[color] {
			t.Fatal("cat colors are not distinct")
		}
		colors[color] = true
	}
}

func TestOneOwnedTimerAndTransientExpiry(t *testing.T) {
	m, c := pendingModel(t, engine.Attack)
	m.Init()
	if !m.tickPending {
		t.Fatal("response did not schedule redraw")
	}
	for range 10 {
		c.publish(m)
		if cmd := m.tick(); cmd != nil {
			t.Fatal("snapshot created a parallel timer chain")
		}
	}
	m.Update(tickMsg{model: &Model{}})
	if !m.tickPending {
		t.Fatal("foreign timer consumed this model's pending tick")
	}
	m.snap.Phase = client.Lobby
	m.deadline = time.Now().Add(-time.Second)
	m.playersAt = time.Now().Add(-time.Second)
	m.revealAt = time.Now().Add(-time.Second)
	m.Update(tickMsg{model: m})
	if m.tickPending || m.showPlayers() || len(m.revealed) > 0 || !m.deadline.IsZero() {
		t.Fatal("expired transient state kept idle redraws alive")
	}
}

func TestNopeAdapterDeadlineDoesNotReapplyEffect(t *testing.T) {
	// Exercise the exact automatic settlement path used by the room.
	m, c := pendingModel(t, engine.Attack)
	g := kittens.Game{}
	_, _, oldTimeout := g.Pending(c.state)
	press(m, "n")
	if err := g.Apply(c.state, oldTimeout); err == nil {
		t.Fatal("pre-Nope deadline affected the new response window")
	}
	_, _, timeout := g.Pending(c.state)
	if err := g.Apply(c.state, timeout); err != nil {
		t.Fatal(err)
	}
	if c.state.UnderAttack || c.state.Turn != 1 {
		t.Fatalf("auto settlement applied cancelled Attack: %+v", c.state)
	}
}
