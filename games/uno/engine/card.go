// Package engine 是 UNO 的纯规则实现：不碰网络、不碰终端、不碰时钟。
// 随机性全部由 State.Seed 推导，因此任意时刻的 State 都可以 JSON 序列化后
// 原样恢复，重放同一局的结果完全一致。
package engine

import "strconv"

// Color 是牌面颜色。NoColor 只用于 Wild 系列牌本身。
type Color uint8

const (
	Red Color = iota
	Yellow
	Green
	Blue
	NoColor
)

// Valid 判断是不是一个可以指定、可以匹配的真实颜色。
func (c Color) Valid() bool { return c < NoColor }

func (c Color) String() string {
	switch c {
	case Red:
		return "红"
	case Yellow:
		return "黄"
	case Green:
		return "绿"
	case Blue:
		return "蓝"
	}
	return "无色"
}

// Kind 是牌的类别。
type Kind uint8

const (
	Number Kind = iota
	Skip
	Reverse
	DrawTwo
	Wild
	WildDrawFour
)

// Card 是一张牌。ID 在同一副牌内唯一且稳定——界面和动作都按 ID 指牌，
// 绝不按手牌下标，否则摸牌后"按 3 出的不是你看到的那张"。
type Card struct {
	ID    int   `json:"id"`
	Color Color `json:"color"`
	Kind  Kind  `json:"kind"`
	Num   int   `json:"num"` // 仅 Number 有效，其余为 -1
}

func (c Card) String() string {
	switch c.Kind {
	case Skip:
		return c.Color.String() + "禁止"
	case Reverse:
		return c.Color.String() + "反转"
	case DrawTwo:
		return c.Color.String() + "加二"
	case Wild:
		return "变色"
	case WildDrawFour:
		return "加四变色"
	}
	return c.Color.String() + strconv.Itoa(c.Num)
}

// Value 是该牌的分值，用于一局结束时结算。
func (c Card) Value() int {
	switch c.Kind {
	case Number:
		return c.Num
	case Skip, Reverse, DrawTwo:
		return 20
	default:
		return 50
	}
}

// NewDeck 生成标准 108 张牌：
// 每色 1 张 0、2 张 1-9、2 张禁止、2 张反转、2 张加二 → 25 张/色 × 4 = 100，
// 再加 4 张变色、4 张加四变色 → 108。
func NewDeck() []Card {
	deck := make([]Card, 0, 108)
	id := 0
	add := func(c Color, k Kind, n, times int) {
		for i := 0; i < times; i++ {
			deck = append(deck, Card{ID: id, Color: c, Kind: k, Num: n})
			id++
		}
	}
	for c := Red; c <= Blue; c++ {
		add(c, Number, 0, 1)
		for n := 1; n <= 9; n++ {
			add(c, Number, n, 2)
		}
		add(c, Skip, -1, 2)
		add(c, Reverse, -1, 2)
		add(c, DrawTwo, -1, 2)
	}
	add(NoColor, Wild, -1, 4)
	add(NoColor, WildDrawFour, -1, 4)
	return deck
}
