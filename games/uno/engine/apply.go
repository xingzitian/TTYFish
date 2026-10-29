package engine

import (
	"errors"
	"fmt"
)

// 引擎对外只暴露这几类错误，界面直接展示给玩家。
var (
	ErrPhase       = errors.New("现在不能操作")
	ErrTurn        = errors.New("还没轮到你")
	ErrNoCard      = errors.New("手里没有这张牌")
	ErrIllegal     = errors.New("这张牌现在出不了")
	ErrColor       = errors.New("出变色牌必须先指定颜色")
	ErrNoDrawn     = errors.New("现在没有摸起来的牌可以跳过")
	ErrNoCatch     = errors.New("现在没有可以抓的人")
	ErrChallenge   = errors.New("正在等 +4 的质疑决定")
	ErrNoChallenge = errors.New("现在没有待处理的质疑")
)

// Action 是玩家（或机器人）对牌局的一次操作。客户端只发意图，
// 状态推进完全在服务端完成——这是服务端权威模型的基本要求。
type Action interface{ isAction() }

// PlayCard 出一张牌。Wild 系列必须带上 Color。
// Uno 表示"出牌的同时喊了 UNO"，也就是真实牌桌上把牌拍下去时喊的那一声。
type PlayCard struct {
	Seat   int   `json:"seat"`
	CardID int   `json:"card_id"`
	Color  Color `json:"color"`
	Uno    bool  `json:"uno,omitempty"`
}

// DrawCard 摸牌。若 Pending>0 则是吃下罚牌。
type DrawCard struct {
	Seat int `json:"seat"`
}

// Pass 保留刚摸到的牌，结束回合。
type Pass struct {
	Seat int `json:"seat"`
}

// CallUno 喊 UNO。
type CallUno struct {
	Seat int `json:"seat"`
}

// CatchUno 抓一个漏喊 UNO 的人。
type CatchUno struct {
	Seat   int `json:"seat"`
	Target int `json:"target"`
}

// ResolveChallenge 是对 +4 质疑的决定。Challenge=false 就是认罚。
type ResolveChallenge struct {
	Seat      int  `json:"seat"`
	Challenge bool `json:"challenge"`
}

func (PlayCard) isAction()         {}
func (DrawCard) isAction()         {}
func (Pass) isAction()             {}
func (CallUno) isAction()          {}
func (CatchUno) isAction()         {}
func (ResolveChallenge) isAction() {}

// Seat 返回发起这个动作的座位。
func Seat(a Action) int {
	switch v := a.(type) {
	case PlayCard:
		return v.Seat
	case DrawCard:
		return v.Seat
	case Pass:
		return v.Seat
	case CallUno:
		return v.Seat
	case CatchUno:
		return v.Seat
	case ResolveChallenge:
		return v.Seat
	}
	return -1
}

// Apply 把动作作用到 s 上。成功时 s 已被就地修改，失败时 s 保持不变。
func Apply(s *State, a Action) error {
	switch v := a.(type) {
	case CallUno:
		return s.callUno(v)
	case CatchUno:
		return s.catchUno(v)
	}

	if s.Phase != Playing {
		return ErrPhase
	}

	switch v := a.(type) {
	case PlayCard:
		return s.play(v)
	case DrawCard:
		return s.draw(v)
	case Pass:
		return s.pass(v)
	case ResolveChallenge:
		return s.resolveChallenge(v)
	}
	return ErrPhase
}

// resolveChallenge 处理 +4 的质疑。
//
// 认罚：摸 4 张，回合跳过。
// 质疑成立（对方当时手里还有上一种颜色的牌）：改由他摸 4 张，质疑者继续出牌。
// 质疑失败：质疑者摸 6 张，回合跳过。
func (s *State) resolveChallenge(v ResolveChallenge) error {
	c := s.Challenge
	if c == nil {
		return ErrNoChallenge
	}
	if v.Seat != c.By {
		return ErrTurn
	}
	s.Challenge = nil
	s.TurnCount++

	if !v.Challenge {
		got := s.drawTo(c.By, 4)
		s.logf(c.By, "认罚，摸 %d 张", got)
		s.advance(1)
		return nil
	}

	if s.hasColor(c.Target, c.PrevColor) {
		got := s.drawTo(c.Target, 4)
		s.logf(-1, "质疑成立！+4 出牌人手里还有%s牌，改罚他摸 %d 张", c.PrevColor, got)
		// 回合留在质疑者手上，他接着出牌。
		return nil
	}

	got := s.drawTo(c.By, 6)
	s.logf(-1, "质疑失败，质疑者改摸 %d 张并被跳过", got)
	s.advance(1)
	return nil
}

func (s *State) hasColor(seat int, c Color) bool {
	for _, card := range s.Hands[seat] {
		if card.Color == c {
			return true
		}
	}
	return false
}

func (s *State) play(v PlayCard) error {
	if s.Challenge != nil {
		return ErrChallenge
	}
	if v.Seat != s.Turn {
		return ErrTurn
	}
	idx := -1
	for i, c := range s.Hands[v.Seat] {
		if c.ID == v.CardID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNoCard
	}
	// 刚摸起来的那张只能出它自己，不能换别的牌出。
	if s.DrawnID >= 0 && v.CardID != s.DrawnID {
		return ErrIllegal
	}

	c := s.Hands[v.Seat][idx]
	if !s.Playable(c) {
		return ErrIllegal
	}
	isWild := c.Kind == Wild || c.Kind == WildDrawFour
	if isWild && !v.Color.Valid() {
		return ErrColor
	}
	// 变色牌会把有效色改掉，质疑判定要用改之前那个颜色。
	prevActive := s.Active

	// 轮到这个人又开始出牌了，说明"抓他没喊 UNO"的时机已经过去。
	if s.UnoPending == v.Seat {
		s.UnoPending = -1
	}

	s.Hands[v.Seat] = append(s.Hands[v.Seat][:idx], s.Hands[v.Seat][idx+1:]...)
	s.Discard = append(s.Discard, c)
	s.DrawnID = -1

	if isWild {
		s.Active = v.Color
		s.logCard(v.Seat, c, fmt.Sprintf("，指定 %s 色", v.Color))
	} else {
		s.Active = c.Color
		s.logCard(v.Seat, c, "")
	}

	// 剩一张时的 UNO 宣言：出牌同时喊（v.Uno），或者事先按过 u
	// （s.UnoCalled）。都没有、又开着罚牌规则，才会挂起来等别人抓。
	if len(s.Hands[v.Seat]) == 1 {
		switch {
		case v.Uno:
			s.UnoCalled[v.Seat] = false
			s.logf(v.Seat, "喊了 UNO！")
		case s.UnoCalled[v.Seat]:
			s.UnoCalled[v.Seat] = false
		case s.Rules.UnoPenalty:
			s.UnoPending = v.Seat
			s.logf(-1, "只剩一张却没喊 UNO，下家可以抓")
		}
	}

	if len(s.Hands[v.Seat]) == 0 {
		s.endRound(v.Seat)
		return nil
	}

	s.TurnCount++

	switch c.Kind {
	case Skip:
		s.logf(-1, "下家被跳过")
		s.advance(2)
	case Reverse:
		if s.SeatCount == 2 {
			// 两人局反转等同禁止：又轮到自己。
			s.logf(-1, "两人局反转等同禁止，回合回到自己")
			s.advance(2)
		} else {
			s.Dir = -s.Dir
			s.logf(-1, "方向已改变！当前方向：%s", dirArrow(s.Dir))
			s.advance(1)
		}
	case DrawTwo:
		if s.Rules.Stacking {
			s.Pending += 2
			s.logf(-1, "罚牌累计到 %d 张", s.Pending)
			s.advance(1)
		} else {
			n := s.next(1)
			got := s.drawTo(n, 2)
			s.logf(n, "被加二，摸 %d 张并被跳过", got)
			s.advance(2)
		}
	case WildDrawFour:
		switch {
		case s.Rules.Stacking:
			s.Pending += 4
			s.logf(-1, "罚牌累计到 %d 张", s.Pending)
			s.advance(1)
		case s.Rules.Challenge:
			// 把决定权交给下一家：认罚摸 4 张，或者质疑。
			s.advance(1)
			s.Challenge = &ChallengeState{By: s.Turn, Target: v.Seat, PrevColor: prevActive}
			s.logf(-1, "打出 +4，下一家可以认罚，也可以质疑")
		default:
			n := s.next(1)
			got := s.drawTo(n, 4)
			s.logf(n, "被加四，摸 %d 张并被跳过", got)
			s.advance(2)
		}
	default:
		s.advance(1)
	}
	return nil
}

func (s *State) draw(v DrawCard) error {
	if s.Challenge != nil {
		return ErrChallenge
	}
	if v.Seat != s.Turn {
		return ErrTurn
	}

	// 有罚牌在身：一次性吃下，回合结束。
	if s.Pending > 0 {
		n := s.Pending
		s.Pending = 0
		got := s.drawTo(v.Seat, n)
		s.logf(v.Seat, "吃下罚牌，摸 %d 张", got)
		s.advance(1)
		s.TurnCount++
		return nil
	}

	if s.DrawnID >= 0 {
		return ErrIllegal
	}

	c, ok := s.pop()
	if !ok {
		s.logf(-1, "牌堆已经空了，这回合跳过")
		s.advance(1)
		s.TurnCount++
		return nil
	}
	s.Hands[v.Seat] = append(s.Hands[v.Seat], c)
	s.UnoCalled[v.Seat] = false
	s.logf(v.Seat, "摸了一张牌")

	// 摸起来的牌能出就给出牌机会；官方规则不允许改出别的牌。
	if !s.Rules.StrictDraw && s.Playable(c) {
		s.DrawnID = c.ID
		return nil
	}

	s.advance(1)
	s.TurnCount++
	return nil
}

func (s *State) pass(v Pass) error {
	if s.Challenge != nil {
		return ErrChallenge
	}
	if v.Seat != s.Turn {
		return ErrTurn
	}
	if s.DrawnID < 0 {
		return ErrNoDrawn
	}
	s.DrawnID = -1
	s.logf(v.Seat, "留下摸到的牌，跳过")
	s.advance(1)
	s.TurnCount++
	return nil
}

func (s *State) callUno(v CallUno) error {
	if s.Phase != Playing {
		return ErrPhase
	}
	if v.Seat < 0 || v.Seat >= s.SeatCount {
		return ErrTurn
	}
	// 手上还剩三张以上时喊 UNO 没有意义，静默忽略而不是报错。
	if len(s.Hands[v.Seat]) > 2 {
		return nil
	}
	s.UnoCalled[v.Seat] = true
	if s.UnoPending == v.Seat {
		s.UnoPending = -1
	}
	s.logf(v.Seat, "喊了 UNO！")
	return nil
}

func (s *State) catchUno(v CatchUno) error {
	if s.Phase != Playing {
		return ErrPhase
	}
	if !s.Rules.UnoPenalty || s.UnoPending < 0 {
		return ErrNoCatch
	}
	if v.Target != s.UnoPending || v.Seat == s.UnoPending {
		return ErrNoCatch
	}
	target := s.UnoPending
	s.UnoPending = -1
	got := s.drawTo(target, 2)
	s.UnoCalled[target] = false
	s.logf(v.Seat, "抓到漏喊 UNO，对方罚摸 %d 张", got)
	return nil
}

// endRound 结算这一局：赢家拿走其他人手牌的总分。
func (s *State) endRound(winner int) {
	pts := 0
	for i := range s.Hands {
		if i == winner {
			continue
		}
		for _, c := range s.Hands[i] {
			pts += c.Value()
		}
	}
	s.Scores[winner] += pts
	s.RoundWinner = winner
	s.logf(winner, "打完手牌，本局 +%d 分（总分 %d）", pts, s.Scores[winner])

	if s.Rules.MatchTo > 0 && s.Scores[winner] >= s.Rules.MatchTo {
		s.Phase = MatchOver
		s.MatchWinner = winner
		s.logf(-1, "达到 %d 分，赢下整场", s.Rules.MatchTo)
		return
	}
	s.Phase = RoundOver
}
