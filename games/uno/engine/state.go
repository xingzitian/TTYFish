package engine

import (
	"fmt"
	"math/rand"
)

// Phase 是牌局阶段。
type Phase uint8

const (
	Playing Phase = iota
	RoundOver
	MatchOver
)

// Rules 是可选规则。零值即休闲默认：不叠加、摸牌后可出、不罚 UNO、单局。
type Rules struct {
	Stacking   bool `json:"stacking"`    // +2/+4 可以叠给下家
	StrictDraw bool `json:"strict_draw"` // 摸牌后立即结束回合（官方玩法）
	UnoPenalty bool `json:"uno_penalty"` // 漏喊 UNO 会被抓、罚摸两张
	Challenge  bool `json:"challenge"`   // +4 可以被下一家质疑（官方玩法）
	MatchTo    int  `json:"match_to"`    // >0 时打长局，先到该分者赢
}

// ChallengeState 是一次还没了结的 +4 质疑。
//
// 官方规则：打出 +4 之后，下一家可以选择认罚（摸 4 张并被跳过），
// 也可以质疑。质疑成立的条件是出 +4 的人当时手里还有"上一种颜色"的牌
// （有的话就不该用 +4 挡，属于耍赖）。
type ChallengeState struct {
	By        int   `json:"by"`         // 要做决定的人（下一家）
	Target    int   `json:"target"`     // 出 +4 的人
	PrevColor Color `json:"prev_color"` // +4 之前生效的颜色
}

// LogEntry 是一条战报。Seat 为 -1 表示系统消息；Text 不含玩家名，
// 由界面按 Seat 拼上"你 / P2 / 电脑A"这类称呼。
//
// Seq 单调递增，给上层用来"只取新增的那几条"（房间据此打时间戳，
// 引擎本身仍然不碰时钟，保持可测试、可重放）。
// Card 只在出牌类战报里非空，界面按它渲染彩色牌面。
type LogEntry struct {
	Seq  int    `json:"seq"`
	Seat int    `json:"seat"`
	Card *Card  `json:"card,omitempty"`
	Text string `json:"text"`
}

// MaxLog 是保留的战报条数。
const MaxLog = 12

// State 是一局（或一整场多局赛）的全部可见状态。
// 除了 Seed 之外没有任何隐藏随机源，因此可以直接 JSON 存盘恢复。
type State struct {
	Rules       Rules           `json:"rules"`
	Seed        int64           `json:"seed"`
	SeatCount   int             `json:"seat_count"`
	Deck        []Card          `json:"deck"`
	Discard     []Card          `json:"discard"`
	Hands       [][]Card        `json:"hands"`
	Turn        int             `json:"turn"`
	Dir         int             `json:"dir"`
	Active      Color           `json:"active"`
	Pending     int             `json:"pending"`  // 叠加规则下累积的罚牌张数
	DrawnID     int             `json:"drawn_id"` // 刚摸起、可以立刻打出的那张牌，-1 表示无
	Phase       Phase           `json:"phase"`
	RoundWinner int             `json:"round_winner"`
	MatchWinner int             `json:"match_winner"`
	Scores      []int           `json:"scores"`
	UnoCalled   []bool          `json:"uno_called"`
	UnoPending  int             `json:"uno_pending"`
	Challenge   *ChallengeState `json:"challenge,omitempty"`
	Log         []LogEntry      `json:"log"`
	LogSeq      int             `json:"log_seq"`
	TurnCount   int             `json:"turn_count"`
	Shuffles    int             `json:"shuffles"`
	Dealer      int             `json:"dealer"`
}

// NewGame 开一局新牌。seats 限定在 2~10 人。
func NewGame(seats int, seed int64, rules Rules) *State {
	if seats < 2 {
		seats = 2
	}
	if seats > 10 {
		seats = 10
	}
	s := &State{
		Rules:       rules,
		Seed:        seed,
		SeatCount:   seats,
		Dir:         1,
		DrawnID:     -1,
		RoundWinner: -1,
		MatchWinner: -1,
		UnoPending:  -1,
		Hands:       make([][]Card, seats),
		Scores:      make([]int, seats),
		UnoCalled:   make([]bool, seats),
	}
	s.deal()
	return s
}

// deal 洗牌、发牌、翻首张。同一 Seed + 同一 Shuffles 得到完全相同的牌局。
func (s *State) deal() {
	s.Shuffles++
	s.Deck = shuffle(NewDeck(), s.Seed, s.Shuffles)
	for i := range s.Hands {
		s.Hands[i] = nil
		s.UnoCalled[i] = false
	}
	s.Discard = nil
	s.Pending = 0
	s.DrawnID = -1
	s.UnoPending = -1
	s.RoundWinner = -1
	s.Phase = Playing
	s.TurnCount = 0

	for round := 0; round < 7; round++ {
		for seat := 0; seat < s.SeatCount; seat++ {
			if c, ok := s.pop(); ok {
				s.Hands[seat] = append(s.Hands[seat], c)
			}
		}
	}

	// 首张不能是加四变色牌：塞回牌堆底部再翻。
	var first Card
	for {
		c, ok := s.pop()
		if !ok {
			break
		}
		if c.Kind == WildDrawFour {
			s.Deck = append(s.Deck, c)
			continue
		}
		first = c
		break
	}

	s.Discard = []Card{first}
	s.Active = first.Color
	s.Turn = s.Dealer % s.SeatCount

	switch first.Kind {
	case Wild:
		s.Active = s.bestColor(s.Turn)
		s.logf(-1, "首张是变色牌，由第一家指定 %s 色", s.Active)
	case Skip:
		s.logf(-1, "首张是禁止牌，第一家被跳过")
		s.advance(1)
	case Reverse:
		s.Dir = -s.Dir
		s.logf(-1, "首张是反转牌，出牌方向反转")
		s.advance(1)
	case DrawTwo:
		s.drawTo(s.Turn, 2)
		s.logf(-1, "首张是加二牌，第一家摸两张并被跳过")
		s.advance(1)
	}
}

// NextRound 在长局赛里开下一局，保留分数。
func (s *State) NextRound() error {
	if s.Phase != RoundOver {
		return ErrPhase
	}
	s.Dealer = (s.Dealer + 1) % s.SeatCount
	s.deal()
	return nil
}

// TopDiscard 返回弃牌堆顶（当前有效牌）。
func (s *State) TopDiscard() Card {
	if len(s.Discard) == 0 {
		return Card{}
	}
	return s.Discard[len(s.Discard)-1]
}

// Playable 判断这张牌现在能不能出。
func (s *State) Playable(c Card) bool {
	// 叠加中的罚牌：只能拿加牌顶回去，或者吃下罚牌。
	if s.Pending > 0 {
		return c.Kind == DrawTwo || c.Kind == WildDrawFour
	}
	// 变色牌永远可出。
	if c.Kind == Wild || c.Kind == WildDrawFour {
		return true
	}
	// 同色可出。
	if c.Color == s.Active {
		return true
	}
	top := s.TopDiscard()
	// 同数字可出。
	if c.Kind == Number && top.Kind == Number && c.Num == top.Num {
		return true
	}
	// 同功能牌可出（举个禁牌压禁牌）。
	if c.Kind != Number && c.Kind == top.Kind {
		return true
	}
	return false
}

// next 从当前座位沿出牌方向数 n 步。
func (s *State) next(n int) int {
	return ((s.Turn+n*s.Dir)%s.SeatCount + s.SeatCount) % s.SeatCount
}

// advance 把回合推进 n 步（n=2 就是"跳过一个玩家"）。
func (s *State) advance(n int) {
	s.Turn = s.next(n)
}

// pop 从牌堆顶取一张，堆空则把弃牌堆洗回去。
func (s *State) pop() (Card, bool) {
	if len(s.Deck) == 0 {
		s.reshuffle()
	}
	if len(s.Deck) == 0 {
		return Card{}, false
	}
	c := s.Deck[0]
	s.Deck = s.Deck[1:]
	return c, true
}

func (s *State) reshuffle() {
	if len(s.Discard) <= 1 {
		return
	}
	top := s.Discard[len(s.Discard)-1]
	rest := make([]Card, len(s.Discard)-1)
	copy(rest, s.Discard[:len(s.Discard)-1])
	s.Shuffles++
	s.Deck = shuffle(rest, s.Seed, s.Shuffles)
	s.Discard = []Card{top}
	s.logf(-1, "牌堆摸空，弃牌堆洗回 %d 张", len(s.Deck))
}

// drawTo 给 seat 摸 n 张，返回实际摸到的张数。
func (s *State) drawTo(seat, n int) int {
	got := 0
	for i := 0; i < n; i++ {
		c, ok := s.pop()
		if !ok {
			break
		}
		s.Hands[seat] = append(s.Hands[seat], c)
		got++
	}
	if got > 0 {
		s.UnoCalled[seat] = false
		// 摸牌就等于又拿到新牌了，抓 UNO 的窗口关闭。
		if s.UnoPending == seat {
			s.UnoPending = -1
		}
	}
	return got
}

// bestColor 挑出该座位手里最多的颜色，用于变色牌默认宣色。
func (s *State) bestColor(seat int) Color {
	var cnt [4]int
	for _, c := range s.Hands[seat] {
		if c.Color.Valid() {
			cnt[c.Color]++
		}
	}
	best := Red
	for c := Red; c <= Blue; c++ {
		if cnt[c] > cnt[best] {
			best = c
		}
	}
	return best
}

func (s *State) logf(seat int, format string, args ...any) {
	s.LogSeq++
	s.Log = append(s.Log, LogEntry{
		Seq:  s.LogSeq,
		Seat: seat,
		Text: fmt.Sprintf(format, args...),
	})
	if len(s.Log) > MaxLog {
		s.Log = s.Log[len(s.Log)-MaxLog:]
	}
}

// logCard 记一条"谁出了哪张牌"。带上牌本身，界面才能画出彩色牌面。
func (s *State) logCard(seat int, c Card, text string) {
	s.LogSeq++
	card := c
	s.Log = append(s.Log, LogEntry{
		Seq:  s.LogSeq,
		Seat: seat,
		Card: &card,
		Text: text,
	})
	if len(s.Log) > MaxLog {
		s.Log = s.Log[len(s.Log)-MaxLog:]
	}
}

// dirArrow 是出牌方向的符号：顺时针 ↻，逆时针 ↺。
func dirArrow(dir int) string {
	if dir < 0 {
		return "↺"
	}
	return "↻"
}

// shuffle 是由 (seed, salt) 完全决定的洗牌：没有隐藏状态，
// 所以存盘重放的结果可以逐张对齐。
func shuffle(cards []Card, seed int64, salt int) []Card {
	out := make([]Card, len(cards))
	copy(out, cards)
	r := rand.New(rand.NewSource(seed*1_000_003 + int64(salt)))
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
