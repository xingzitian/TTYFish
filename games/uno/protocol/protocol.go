// Package protocol translates UNO actions and player observations to a stable JSON contract.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

type Action struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Codec struct{}

func parse(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("payload 必须是对象")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("payload 格式错误")
	}
	return nil
}

// DecodeAction injects the authenticated player's seat; no wire action accepts a seat.
func (Codec) DecodeAction(kind string, raw json.RawMessage, seat int) (any, error) {
	switch kind {
	case "play_card":
		var p struct {
			CardID *int          `json:"card_id"`
			Color  *engine.Color `json:"color"`
			Uno    bool          `json:"uno,omitempty"`
		}
		if err := parse(raw, &p); err != nil {
			return nil, err
		}
		if p.CardID == nil || *p.CardID < 0 || p.Color == nil || !p.Color.Valid() {
			return nil, errors.New("出牌需要 card_id 和 0–3 的 color")
		}
		return engine.PlayCard{Seat: seat, CardID: *p.CardID, Color: *p.Color, Uno: p.Uno}, nil
	case "draw_card", "pass", "call_uno":
		if err := parse(raw, &struct{}{}); err != nil {
			return nil, err
		}
		switch kind {
		case "draw_card":
			return engine.DrawCard{Seat: seat}, nil
		case "pass":
			return engine.Pass{Seat: seat}, nil
		default:
			return engine.CallUno{Seat: seat}, nil
		}
	case "catch_uno":
		var p struct {
			Target *int `json:"target"`
		}
		if err := parse(raw, &p); err != nil {
			return nil, err
		}
		if p.Target == nil || *p.Target < 0 {
			return nil, errors.New("需要 target")
		}
		return engine.CatchUno{Seat: seat, Target: *p.Target}, nil
	case "resolve_challenge":
		var p struct {
			Challenge *bool `json:"challenge"`
		}
		if err := parse(raw, &p); err != nil {
			return nil, err
		}
		if p.Challenge == nil {
			return nil, errors.New("需要 challenge")
		}
		return engine.ResolveChallenge{Seat: seat, Challenge: *p.Challenge}, nil
	default:
		return nil, fmt.Errorf("未知 UNO 动作 %q", kind)
	}
}

func encodeAction(a engine.Action) Action {
	switch v := a.(type) {
	case engine.PlayCard:
		return Action{"play_card", map[string]any{"card_id": v.CardID, "color": v.Color, "uno": v.Uno}}
	case engine.DrawCard:
		return Action{"draw_card", struct{}{}}
	case engine.Pass:
		return Action{"pass", struct{}{}}
	case engine.CallUno:
		return Action{"call_uno", struct{}{}}
	case engine.CatchUno:
		return Action{"catch_uno", map[string]any{"target": v.Target}}
	case engine.ResolveChallenge:
		return Action{"resolve_challenge", map[string]any{"challenge": v.Challenge}}
	default:
		panic("unsupported UNO action")
	}
}

func (Codec) EncodeView(view any) (any, error) {
	o, ok := view.(engine.Observation)
	if !ok {
		return nil, errors.New("UNO 视图类型错误")
	}
	legal := make([]Action, 0, len(o.Legal))
	for _, a := range o.Legal {
		legal = append(legal, encodeAction(a))
	}
	// The explicit legal field shadows Observation.Legal, which has no action tags.
	return struct {
		engine.Observation
		Legal []Action `json:"legal"`
	}{o, legal}, nil
}
