// Package game 定义"平台 ↔ 游戏"的契约。
//
// 平台（lobby）只认这几个方法，不认识牌、规则、计分；游戏（games/uno）
// 只实现这些方法，不认识房间、连接、玩家身份。两边通过 any 传状态和视图，
// 各自解释自己的那一半。
//
// 这样以后加第二个游戏，只需要在 games/ 下新写一个实现，平台一行都不用动。
package game

import "time"

// Phase 是游戏对外的三个阶段。
type Phase uint8

const (
	Playing Phase = iota
	RoundOver
	MatchOver
)

// PlayerSeed 是开局时传给游戏的座位信息。
type PlayerSeed struct {
	Seat int
	Name string
	Bot  bool
}

// Event 是一条给时间线用的战报。
//
// Detail 是给游戏自己的 UI 用的任意值（UNO 用它传牌面），平台只负责
// 存和排序，不解释内容。
type Event struct {
	Seq    int
	Seat   int
	Text   string
	Detail any
}

// Game 是一个游戏的全部规则接口。
type Game interface {
	Name() string
	MinSeats() int
	MaxSeats() int

	// NewMatch 开一场比赛（内部可以包含多局），NextRound 在局间推进。
	NewMatch(seeds []PlayerSeed, seed int64) any
	NextRound(state any) error

	Apply(state any, action any) error
	// Observe 做信息裁剪：返回这个座位能看到的东西。
	Observe(state any, seat int) any
	// SeatOf 从动作里取出座位号，平台用它校验"这动作是不是你这个座位发的"。
	SeatOf(action any) int

	Phase(state any) Phase
	Turn(state any) int

	// Bot 给机器人做决策，TimeoutAction 是回合超时后的保底动作。
	Bot(view any) any
	TimeoutAction(view any) any

	RoundWinner(state any) int
	MatchWinner(state any) int
	Scores(state any) []int
	MatchTarget() int

	// Events 返回 seq 之后的新事件，以及当前最新 seq。
	Events(state any, since int) ([]Event, int)

	// Pending 是"限时义务"：token 变化时平台开始计时，到点没解除就执行
	// action。UNO 用它实现"剩一张牌 3 秒内要喊 UNO，否则算被抓"。
	// 返回空 token 表示当前没有。
	Pending(state any) (token string, within time.Duration, action any)
}
