package gateway

import (
	"bytes"
	"testing"

	"github.com/xingzitian/TTYFish/hub/internal/registry"
	"github.com/xingzitian/TTYFish/sdk/client"
)

type identifiedClient struct {
	fakeClient
	id string
}

func (c *identifiedClient) GameID() string { return c.id }

func TestJoinUsesActualRoomGame(t *testing.T) {
	for _, actual := range []string{"exploding-kittens", "unsupported"} {
		t.Run(actual, func(t *testing.T) {
			reg := registry.New()
			var rendered string
			for _, id := range []string{"uno", "exploding-kittens"} {
				if err := reg.Register(registry.Entry{
					ID: id, Game: stubGame{},
					NewUI: func(registry.UIContext) registry.Model {
						rendered = id
						return &fakeModel{}
					},
				}); err != nil {
					t.Fatal(err)
				}
			}
			c := &identifiedClient{fakeClient: fakeClient{joined: "ABC234"}, id: actual}
			r := New(Deps{Registry: reg, Output: &bytes.Buffer{},
				OpenRoom: func(gameID, code string) (client.Client, error) {
					if gameID != "uno" || code != "ABC234" {
						t.Fatal("wrong selected game or room code")
					}
					return c, nil
				},
			})
			r.gameID = "uno"
			r.openRoom("ABC234")
			if actual == "unsupported" {
				if rendered != "" || r.joinErr == "" || c.joined != "" || r.roomClient != nil {
					t.Fatal("unsupported room was not released")
				}
			} else if rendered != actual || r.gameID != actual || r.level != levelTable {
				t.Fatal("joined room rendered with the selected menu's codec/UI")
			}
		})
	}
}
