// Package protocol defines authenticated, seat-free wire actions.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
)

type Action struct {
	Type    string  `json:"type"`
	Payload Payload `json:"payload"`
}

type Payload struct {
	CardIDs   []int       `json:"card_ids,omitempty"`
	Target    *int        `json:"target,omitempty"`
	Requested engine.Kind `json:"requested,omitempty"`
	Position  *int        `json:"position,omitempty"`
}

type Codec struct{}

func (Codec) DecodeAction(kind string, raw json.RawMessage, seat int) (any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("payload must be an object")
	}
	var p Payload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("unexpected trailing payload")
	}
	a := engine.Action{Type: engine.ActionType(kind), Seat: seat, CardIDs: p.CardIDs, Requested: p.Requested}
	switch a.Type {
	case engine.Draw, engine.Pass:
		if len(p.CardIDs) != 0 || p.Target != nil || p.Position != nil || p.Requested != "" {
			return nil, errors.New("action takes no parameters")
		}
	case engine.Play:
		if len(p.CardIDs) < 1 || len(p.CardIDs) > 3 || p.Position != nil {
			return nil, errors.New("play requires one to three card IDs")
		}
		a.Target = -1
		if p.Target != nil {
			a.Target = *p.Target
		}
	case engine.PlayNope, engine.Give, engine.UseDefuse:
		if len(p.CardIDs) != 1 || p.Target != nil || p.Requested != "" {
			return nil, errors.New("action requires one card ID")
		}
		if a.Type == engine.UseDefuse {
			if p.Position == nil || *p.Position < 0 {
				return nil, errors.New("defuse requires a nonnegative insertion position")
			}
			a.Position = *p.Position
		} else if p.Position != nil {
			return nil, errors.New("unexpected insertion position")
		}
	default:
		return nil, errors.New("unknown kittens action")
	}
	for _, id := range p.CardIDs {
		if id < 0 {
			return nil, errors.New("card IDs must be nonnegative")
		}
	}
	return a, nil
}

func (Codec) EncodeAction(a engine.Action) Action {
	p := Payload{CardIDs: a.CardIDs, Requested: a.Requested}
	if a.Type == engine.Play {
		p.Target = &a.Target
	}
	if a.Type == engine.UseDefuse {
		p.Position = &a.Position
	}
	return Action{Type: string(a.Type), Payload: p}
}

func (Codec) EncodeView(view any) (any, error) {
	o, ok := view.(engine.Observation)
	if !ok {
		return nil, errors.New("invalid kittens observation")
	}
	return o, nil
}
