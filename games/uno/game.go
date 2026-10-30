// Package uno 把 UNO 接到平台契约上：规则、计分、机器人决策、限时义务
// 都在这里实现，平台（internal/lobby）一行都不用知道。
package uno

import (
	"fmt"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/game"
)

// Game 是一个可以塞进大厅的 UNO 游戏定义。
type Game struct {
	Rules engine.Rules
	AI    *Brain
}

func New(rules engine.Rules) Game { return Game{Rules: rules} }

// WithAI 让机器人先问模型，问不到再退回启发式。
func (g Game) WithAI(b *Brain) Game {
	g.AI = b
	return g
}

func (g Game) Name() string  { return "uno" }
func (g Game) MinSeats() int { return 2 }
func (g Game) MaxSeats() int { return 6 }

func (g Game) NewMatch(seeds []game.PlayerSeed, seed int64) any {
	return engine.NewGame(len(seeds), seed, g.Rules)
}

func (g Game) NextRound(state any) error {
	s, ok := state.(*engine.State)
	if !ok {
		return fmt.Errorf("uno: 状态类型不对")
	}
	return s.NextRound()
}

func (g Game) Apply(state any, action any) error {
	s, ok := state.(*engine.State)
	if !ok {
		return fmt.Errorf("uno: 状态类型不对")
	}
	a, ok := action.(engine.Action)
	if !ok {
		return fmt.Errorf("uno: 动作类型不对")
	}
	return engine.Apply(s, a)
}

func (g Game) Observe(state any, seat int) any {
	s, ok := state.(*engine.State)
	if !ok {
		return nil
	}
	return engine.Observe(s, seat)
}

func (g Game) SeatOf(action any) int {
	a, ok := action.(engine.Action)
	if !ok {
		return -1
	}
	return engine.Seat(a)
}

func (g Game) Phase(state any) game.Phase {
	s, ok := state.(*engine.State)
	if !ok {
		return game.Playing
	}
	switch s.Phase {
	case engine.RoundOver:
		return game.RoundOver
	case engine.MatchOver:
		return game.MatchOver
	}
	return game.Playing
}

func (g Game) Turn(state any) int {
	if s, ok := state.(*engine.State); ok {
		return s.Turn
	}
	return 0
}

func (g Game) Bot(view any) any {
	o, ok := view.(engine.Observation)
	if !ok {
		return nil
	}
	// AI 只做增强：问不到（超时/报错/答非所问）就用启发式，绝不卡住牌桌。
	if g.AI != nil {
		if a := g.AI.Move(o); a != nil {
			return a
		}
	}
	return engine.Decide(o)
}

// TimeoutAction 是人不在（或超时）时的保底动作：
// 有 +4 待回答就认罚，刚摸过牌就跳过，否则摸一张。
func (g Game) TimeoutAction(view any) any {
	o, ok := view.(engine.Observation)
	if !ok {
		return nil
	}
	switch {
	case o.Challenge != nil && o.Challenge.By == o.Seat:
		return engine.ResolveChallenge{Seat: o.Seat, Challenge: false}
	case o.DrawnID >= 0:
		return engine.Pass{Seat: o.Seat}
	default:
		return engine.DrawCard{Seat: o.Seat}
	}
}

func (g Game) RoundWinner(state any) int {
	if s, ok := state.(*engine.State); ok {
		return s.RoundWinner
	}
	return -1
}

func (g Game) MatchWinner(state any) int {
	if s, ok := state.(*engine.State); ok {
		return s.MatchWinner
	}
	return -1
}

func (g Game) Scores(state any) []int {
	if s, ok := state.(*engine.State); ok {
		return s.Scores
	}
	return nil
}

func (g Game) MatchTarget() int { return g.Rules.MatchTo }

func (g Game) Events(state any, since int) ([]game.Event, int) {
	s, ok := state.(*engine.State)
	if !ok {
		return nil, since
	}
	var out []game.Event
	seq := since
	for _, e := range s.Log {
		if e.Seq <= since {
			continue
		}
		seq = e.Seq
		// 注意：nil 的 *Card 塞进 any 之后**不是** nil 接口，
		// 下游用 != nil 判断会漏过去，一解引用就 panic。
		var detail any
		if e.Card != nil {
			detail = e.Card
		}
		out = append(out, game.Event{
			Seq:    e.Seq,
			Seat:   e.Seat,
			Text:   e.Text,
			Detail: detail,
		})
	}
	return out, seq
}

// Pending：剩一张牌没喊 UNO，3 秒内必须按 u；超时按"被抓"处罚。
func (g Game) Pending(state any) (string, time.Duration, any) {
	if !g.Rules.UnoPenalty {
		return "", 0, nil
	}
	s, ok := state.(*engine.State)
	if !ok || s.UnoPending < 0 {
		return "", 0, nil
	}
	catcher := (s.UnoPending + 1) % s.SeatCount
	return fmt.Sprintf("uno:%d", s.UnoPending), 3 * time.Second,
		engine.CatchUno{Seat: catcher, Target: s.UnoPending}
}
