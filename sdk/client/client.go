// Package client 是"平台发给游戏界面的一帧"以及游戏界面能做的操作。
//
// 游戏的 UI 只依赖这个包，不依赖平台实现——所以一个游戏可以被放在
// 任何实现了 Client 的运行器里跑（SSH、本地单机、甚至将来的网页）。
package client

// Phase 是房间阶段。
type Phase uint8

const (
	Lobby Phase = iota
	Playing
	Over
)

func (p Phase) String() string {
	switch p {
	case Lobby:
		return "大厅"
	case Playing:
		return "对局中"
	default:
		return "已结束"
	}
}

// Player 是牌桌上的一个人。
type Player struct {
	Seat      int    `json:"seat"`
	Name      string `json:"name"`
	Host      bool   `json:"host"`
	Connected bool   `json:"connected"`
	Bot       bool   `json:"bot"`
	Offline   int    `json:"offline"` // 掉线秒数，0 表示在线
}

// Vote 是进行中的投票。
type Vote struct {
	Kind   string `json:"kind"`
	Yes    int    `json:"yes"`
	No     int    `json:"no"`
	Need   int    `json:"need"`
	EndsIn int    `json:"ends_in"`
}

// TimelineEntry 是一条带时间戳的战报（出牌、系统事件、聊天）。
type TimelineEntry struct {
	AtMS   int64  `json:"at_ms"` // 从开局算起的毫秒数
	Seat   int    `json:"seat"`
	Detail any    `json:"-"` // 游戏自己的细节（UNO 放牌面），平台不解释
	Chat   bool   `json:"chat,omitempty"`
	Text   string `json:"text"`
}

// Snapshot 是"这一帧玩家能看到的一切"。
//
// View 是游戏自己 Observe 出来的东西，平台不解释它的内容；
// 游戏的界面负责把它断言回自己的类型。
type Snapshot struct {
	Code     string
	Phase    Phase
	Players  []Player
	MySeat   int
	MaxSeats int
	IsHost   bool
	View     any
	Timeline []TimelineEntry
	Vote     *Vote
	Notice   string
	// PendingLeftMS 是"限时义务还剩多少毫秒"，0 表示当前没有。
	PendingLeftMS int64
	// Kicked 表示这一帧是"你在别处登录，这个连接被顶掉了"，发完就关通道。
	Kicked bool
}

// Presets 是预设快捷语。不做自由输入，避免控制字符注入。
var Presets = []string{
	"好牌",
	"快点",
	"GG",
	"别打我的红",
	"让我赢一把",
	"手气太差",
}

// Client 是游戏界面对平台的全部依赖。
//
// 界面只通过它读快照、发动作，不接触房间、连接、账号。
type Client interface {
	// Snapshot 是当前这一帧。第一次渲染前可能为空。
	Snapshot() Snapshot
	// Updates 每次状态变化推一帧；关闭表示会话结束。
	Updates() <-chan Snapshot

	// Join 进一个房间；code 为空表示新开一间。
	Join(code string) error
	// Leave 离开当前房间。
	Leave() error

	// Start 房主开局。
	Start() error
	// AddBot 房主补一个电脑。
	AddBot() error
	// Act 发一个动作（游戏自己的类型）。
	Act(action any) error
	// Chat 发第 i 条预设快捷语。
	Chat(i int) error
	// VoteRestart 投票重开。
	VoteRestart(agree bool) error
}
