package protocol

import (
	"encoding/json"
	"testing"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

func TestLegalActionsRoundTripAndSeatAuthority(t *testing.T) {
	codec := Codec{}
	state := engine.NewGame(2, 42, engine.Rules{})
	for seat := 0; seat < 2; seat++ {
		observation := engine.Observe(state, seat)
		encoded, err := codec.EncodeView(observation)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(encoded)
		if err != nil {
			t.Fatal(err)
		}
		var view struct {
			Legal []struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			} `json:"legal"`
		}
		if err = json.Unmarshal(raw, &view); err != nil {
			t.Fatal(err)
		}
		for _, wire := range view.Legal {
			action, err := codec.DecodeAction(wire.Type, wire.Payload, seat)
			if err != nil {
				t.Fatal(err)
			}
			if engine.Seat(action.(engine.Action)) != seat {
				t.Fatal("wrong seat")
			}
		}
	}
	if _, err := codec.DecodeAction("draw_card", json.RawMessage(`{"seat":1}`), 0); err == nil {
		t.Fatal("client forged seat")
	}
	for _, raw := range []string{`{}`, `{"card_id":1}`, `{"card_id":1,"color":4}`, `{"card_id":-1,"color":0}`, `{"card_id":1,"color":0,"seat":1}`, `null`} {
		if _, err := codec.DecodeAction("play_card", json.RawMessage(raw), 0); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := codec.DecodeAction("unknown", nil, 0); err == nil {
		t.Fatal("unknown action accepted")
	}
}
