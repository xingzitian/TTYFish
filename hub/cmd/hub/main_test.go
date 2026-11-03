package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/hub/internal/backend"
	"github.com/xingzitian/TTYFish/hub/internal/gateway"
	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/client"
	"github.com/xingzitian/TTYFish/sdk/game"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestSettingsInitializeProductionUNOLayout(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			t.Run(fmt.Sprintf("persisted=%t/compact=%t", persisted, compact), func(t *testing.T) {
				saved := ui.LayoutRich
				reg := registry.New()
				key := "account:alice"
				opts := gateway.Deps{
					Registry: reg, Output: &bytes.Buffer{}, Mode: ui.ColorNone,
					Player: backend.Player{Account: backend.Account{ID: "alice", Username: "Alice"}},
				}
				if persisted {
					opts.LayoutOf = func(k string) (ui.Layout, bool) {
						if k != key {
							t.Fatalf("wrong preference key %q", k)
						}
						return saved, true
					}
					opts.SetLayout = func(k string, l ui.Layout) {
						if k != key {
							t.Fatalf("wrong persistence key %q", k)
						}
						saved = l
					}
				}
				var game registry.Model
				if err := reg.Register(registry.Entry{
					ID: "uno", Game: layoutGame{},
					NewUI: func(c registry.UIContext) registry.Model {
						game = newUNOUI(c, engine.Rules{})
						return game
					},
				}); err != nil {
					t.Fatal(err)
				}
				opts.OpenRoom = func(_, _ string) (client.Client, error) { return &layoutClient{}, nil }
				r := gateway.New(opts)
				r.Init()
				r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // Settings
				if compact {
					r.Update(tea.KeyMsg{Type: tea.KeyEnter})
				}
				r.Update(tea.KeyMsg{Type: tea.KeyEsc})
				for range 3 {
					r.Update(tea.KeyMsg{Type: tea.KeyEnter})
				}
				assertCompact := func(want bool) {
					t.Helper()
					if got := strings.Contains(game.View(), "[ROOM]"); got != want {
						t.Fatalf("compact=%t want=%t: %s", got, want, game.View())
					}
				}
				assertCompact(compact)
				game.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
				game.Update(client.SnapshotMsg(layoutClient{}.Snapshot()))
				assertCompact(!compact)
				if persisted && (saved == ui.LayoutCompact) != !compact {
					t.Fatal("game toggle did not persist")
				}
			})
		}
	}
}

type layoutGame struct{ game.Game }

func (layoutGame) Name() string  { return "UNO" }
func (layoutGame) MinSeats() int { return 2 }
func (layoutGame) MaxSeats() int { return 6 }

type layoutClient struct{ client.Client }

func (layoutClient) Snapshot() client.Snapshot {
	return client.Snapshot{Code: "ABC234", Phase: client.Lobby, MaxSeats: 6}
}
func (layoutClient) Updates() <-chan client.Snapshot { return nil }
