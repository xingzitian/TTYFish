package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
)

func TestActionsRoundTripWithAuthenticatedSeat(t *testing.T) {
	codec := Codec{}
	for _, action := range []engine.Action{
		{Type: engine.Draw},
		{Type: engine.Pass},
		{Type: engine.Play, CardIDs: []int{12}, Target: 2},
		{Type: engine.Play, CardIDs: []int{12, 14}, Target: 1},
		{Type: engine.Play, CardIDs: []int{12, 14, 16}, Target: 1, Requested: engine.Defuse},
		{Type: engine.PlayNope, CardIDs: []int{19}},
		{Type: engine.Give, CardIDs: []int{21}},
		{Type: engine.UseDefuse, CardIDs: []int{1}, Position: 0},
	} {
		action.Seat = 99
		wire := codec.EncodeAction(action)
		raw, _ := json.Marshal(wire.Payload)
		decoded, err := codec.DecodeAction(wire.Type, raw, 2)
		action.Seat = 2
		if err != nil || !reflect.DeepEqual(decoded, action) {
			t.Fatalf("roundtrip %+v = %+v, %v", action, decoded, err)
		}
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		if _, exists := payload["seat"]; exists {
			t.Fatal("encoded client seat")
		}
	}
}

func TestRejectMalformedAndSystemActions(t *testing.T) {
	for _, tc := range []struct{ kind, payload string }{
		{"response_timeout", `{}`},
		{"draw", `{"seat":1}`},
		{"draw", `{"card_ids":[2]}`},
		{"draw", `null`},
		{"draw", `{} {}`},
		{"play", `{"card_ids":[1],"unknown":true}`},
		{"play", `{"card_ids":[]}`},
		{"nope", `{"card_ids":[1,2]}`},
		{"give", `{"card_ids":[-1]}`},
		{"defuse", `{"card_ids":[1]}`},
		{"defuse", `{"card_ids":[1],"position":-1}`},
		{"pass_response", `{"position":0}`},
	} {
		t.Run(tc.kind+tc.payload, func(t *testing.T) {
			if _, err := (Codec{}).DecodeAction(tc.kind, json.RawMessage(tc.payload), 0); err == nil {
				t.Fatal("malformed action accepted")
			}
		})
	}
}

func TestWireObservationContainsNoAuthoritySecrets(t *testing.T) {
	s, _ := engine.NewGame(3, 1234567)
	view, err := (Codec{}).EncodeView(engine.Observe(s, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, secret := range []string{"seed", "random_count", "deck", "removed", "hands", "kitten", "log"} {
		if _, exists := fields[secret]; exists {
			t.Fatalf("authority secret in view: %s", secret)
		}
	}
	o := view.(engine.Observation)
	if len(o.Hand) != 8 || o.Seat != 0 {
		t.Fatal("own hand was lost")
	}
}
