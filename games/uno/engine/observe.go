package engine

// Observation 是"某个座位此刻能看到的一切"。
//
// 它是整个项目唯一的信息裁剪点，三个使用方共用同一份结构：
//
//	界面   用它渲染（天然看不到别人的手牌）
//	网络   用它序列化（抓包也拿不到别人的手牌）
//	AI     用它决策（信息对称，AI 不可能作弊）
//
// 任何"给某人看的状态"都必须走 Observe，不要在别处另开一条裁剪逻辑。
type Observation struct {
	Seat         int        `json:"seat"`
	SeatCount    int        `json:"seat_count"`
	MyTurn       bool       `json:"my_turn"`
	Turn         int        `json:"turn"`
	Phase        Phase      `json:"phase"`
	Hand         []Card     `json:"hand"`
	HandCounts   []int      `json:"hand_counts"`
	Top          Card       `json:"top"`
	Active       Color      `json:"active"`
	Dir          int        `json:"dir"`
	Pending      int        `json:"pending"`
	DrawnID      int        `json:"drawn_id"`
	DeckCount    int        `json:"deck_count"`
	DiscardCount int        `json:"discard_count"`
	Legal        []Action   `json:"legal"`
	Log          []LogEntry `json:"log"`

	// 以下是结算与规则展示需要的公共信息。
	Scores      []int           `json:"scores"`
	RoundWinner int             `json:"round_winner"`
	MatchWinner int             `json:"match_winner"`
	MatchTo     int             `json:"match_to"`
	UnoPending  int             `json:"uno_pending"`
	Challenge   *ChallengeState `json:"challenge,omitempty"`
}

// Observe 生成一个座位的视角。seat 越界时返回一个"旁观者"视角
// （没有手牌、没有合法动作），方便观战和测试复用。
func Observe(s *State, seat int) Observation {
	o := Observation{
		Seat:         seat,
		SeatCount:    s.SeatCount,
		Turn:         s.Turn,
		Phase:        s.Phase,
		Top:          s.TopDiscard(),
		Active:       s.Active,
		Dir:          s.Dir,
		Pending:      s.Pending,
		DrawnID:      s.DrawnID,
		DeckCount:    len(s.Deck),
		DiscardCount: len(s.Discard),
		HandCounts:   make([]int, s.SeatCount),
		Log:          append([]LogEntry(nil), s.Log...),
		Scores:       append([]int(nil), s.Scores...),
		RoundWinner:  s.RoundWinner,
		MatchWinner:  s.MatchWinner,
		MatchTo:      s.Rules.MatchTo,
		UnoPending:   s.UnoPending,
	}
	for i := range s.Hands {
		o.HandCounts[i] = len(s.Hands[i])
	}
	if s.Challenge != nil {
		c := *s.Challenge
		o.Challenge = &c
	}
	if seat >= 0 && seat < s.SeatCount {
		o.MyTurn = s.Turn == seat
		o.Hand = append([]Card(nil), s.Hands[seat]...)
		o.Legal = LegalMoves(s, seat)
	}
	return o
}

// ActiveCard 返回当前牌面上真正起作用的牌（弃牌堆顶）。
func (o Observation) ActiveCard() Card { return o.Top }

// LegalMoves 列出这个座位现在所有合法的动作。
//
// 变色牌会按颜色展开成四个动作（AI 和网络协议都能直接选一个，
// 不需要理解"先出牌再补色"这种两步语义）。
func LegalMoves(s *State, seat int) []Action {
	if s.Phase != Playing || s.Turn != seat || seat < 0 || seat >= s.SeatCount {
		return nil
	}
	// 有 +4 待质疑时，这个座位只能做这一个决定。
	if s.Challenge != nil {
		if seat != s.Challenge.By {
			return nil
		}
		return []Action{
			ResolveChallenge{Seat: seat, Challenge: true},
			ResolveChallenge{Seat: seat, Challenge: false},
		}
	}

	var out []Action
	for _, c := range s.Hands[seat] {
		if s.DrawnID >= 0 && c.ID != s.DrawnID {
			continue // 摸过牌之后只能出摸到的那张
		}
		if !s.Playable(c) {
			continue
		}
		if c.Kind == Wild || c.Kind == WildDrawFour {
			for _, col := range []Color{Red, Yellow, Green, Blue} {
				out = append(out, PlayCard{Seat: seat, CardID: c.ID, Color: col})
			}
			continue
		}
		out = append(out, PlayCard{Seat: seat, CardID: c.ID, Color: c.Color})
	}

	if s.DrawnID >= 0 {
		out = append(out, Pass{Seat: seat})
	} else {
		out = append(out, DrawCard{Seat: seat})
	}
	return out
}
