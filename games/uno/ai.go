package uno

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xingzitian/TTYFish/games/uno/engine"
	"github.com/xingzitian/TTYFish/sdk/agent"
)

// aiSystem 是给模型的系统提示。只要一个数字，省 token 也省解析的麻烦。
const aiSystem = "你在玩 UNO。看清手里的牌和场上的牌，从候选动作里选一个对你最有利的。" +
	"只回复一个数字，不要解释，不要标点。"

// Brain 是 UNO 的可选 AI 决策器。
//
// 它拿到的永远是一个 Observation——和界面、网络用的是同一份裁剪结果，
// 所以 AI 不可能看到别人的手牌。
type Brain struct {
	Client  *agent.Client
	Timeout time.Duration
}

func NewBrain(c *agent.Client) *Brain {
	if c == nil {
		return nil
	}
	return &Brain{Client: c, Timeout: 1500 * time.Millisecond}
}

// Move 问模型要一个动作。任何异常都返回 nil，由调用方退回启发式。
func (b *Brain) Move(o engine.Observation) engine.Action {
	if b == nil || b.Client == nil || len(o.Legal) == 0 {
		return nil
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pick, err := b.Client.Choose(ctx, aiSystem, promptFor(o), len(o.Legal))
	if err != nil || pick < 1 || pick > len(o.Legal) {
		return nil
	}
	return o.Legal[pick-1]
}

// promptFor 把局面翻译成一段人能看懂的文字。
// 只写当前座位看得到的信息，不泄露别人的手牌。
func promptFor(o engine.Observation) string {
	var b strings.Builder

	b.WriteString("你的手牌：")
	if len(o.Hand) == 0 {
		b.WriteString("（空）")
	}
	for i, c := range o.Hand {
		if i > 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%d:%s", i+1, describeCard(c))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "场上这张牌：%s\n", describeCard(o.Top))
	fmt.Fprintf(&b, "当前有效颜色：%s\n", colorName(o.Active))

	dir := "顺时针（下一家是下一位玩家）"
	if o.Dir < 0 {
		dir = "逆时针（下一家是上一位玩家）"
	}
	fmt.Fprintf(&b, "方向：%s\n", dir)
	fmt.Fprintf(&b, "牌堆剩余：%d 张\n", o.DeckCount)

	counts := make([]string, len(o.HandCounts))
	for i, n := range o.HandCounts {
		who := fmt.Sprintf("玩家%d", i+1)
		if i == o.Seat {
			who = "你"
		}
		counts[i] = fmt.Sprintf("%s %d 张", who, n)
	}
	fmt.Fprintf(&b, "各家手牌数：%s\n", strings.Join(counts, "，"))

	if o.Pending > 0 {
		fmt.Fprintf(&b, "累积罚牌：%d 张（轮到你就要摸这么多）\n", o.Pending)
	}

	b.WriteString("候选动作：\n")
	for i, a := range o.Legal {
		fmt.Fprintf(&b, "%d. %s\n", i+1, describeAction(a, o.Hand))
	}
	return b.String()
}

func describeAction(a engine.Action, hand []engine.Card) string {
	switch v := a.(type) {
	case engine.PlayCard:
		for _, c := range hand {
			if c.ID == v.CardID {
				s := "打出 " + describeCard(c)
				if c.Kind == engine.Wild || c.Kind == engine.WildDrawFour {
					if v.Color != c.Color {
						s += "，指定颜色 " + colorName(v.Color)
					}
				}
				return s
			}
		}
		return "打出一张牌"
	case engine.DrawCard:
		return "摸一张牌"
	case engine.Pass:
		return "结束回合（不再出牌）"
	case engine.CallUno:
		return "喊 UNO"
	case engine.CatchUno:
		return "抓别人漏喊 UNO"
	case engine.ResolveChallenge:
		if v.Challenge {
			return "质疑这个 +4"
		}
		return "认罚，摸 4 张"
	}
	return "未知动作"
}

func describeCard(c engine.Card) string {
	if c.Kind == engine.Wild || c.Kind == engine.WildDrawFour {
		if c.Kind == engine.WildDrawFour {
			return "[万能|+4]"
		}
		return "[万能|变色]"
	}
	s := "[" + colorName(c.Color) + "|"
	switch c.Kind {
	case engine.Number:
		s += fmt.Sprintf("%d", c.Num)
	case engine.Skip:
		s += "禁止"
	case engine.Reverse:
		s += "反转"
	case engine.DrawTwo:
		s += "+2"
	default:
		s += "?"
	}
	return s + "]"
}

func colorName(c engine.Color) string {
	switch c {
	case engine.Red:
		return "红"
	case engine.Yellow:
		return "黄"
	case engine.Green:
		return "绿"
	case engine.Blue:
		return "蓝"
	}
	return "无"
}
