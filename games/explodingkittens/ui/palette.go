package tui

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// cardStyle 是一张牌在界面上的样子。
//
// 牌面统一写成 `[类别|名字]`：类别分「保命 / 功能 / 猫牌 / 炸弹」四类。
// 这样即使终端不支持颜色（黑白模式、色弱、把 Colors 关掉），
// 靠文字也能立刻分出"这张能不能救命、是不是猫牌"。
type cardStyle struct {
	// Category 是类别：保命 / 功能 / 猫牌 / 炸弹。
	Category string
	// Name 是牌名，取本地化短名（拆弹 / 否决 / 胡子……）。
	Name string
	// Bg/Fg 是 256 色号；无色模式下不生效。
	Bg lipgloss.Color
	Fg lipgloss.Color
	// Rank 决定手牌排序：保命在最前，功能次之，猫牌按种类聚在一起。
	Rank int
}

var palettes = map[engine.Kind]cardStyle{
	engine.ExplodingKitten: {"炸弹", "爆炸猫", "196", "231", 0},
	engine.Defuse:          {"保命", "拆弹", "41", "16", 1},

	engine.Nope:         {"功能", "否决", "129", "231", 3},
	engine.Attack:       {"功能", "攻击", "208", "16", 4},
	engine.Skip:         {"功能", "跳过", "220", "16", 5},
	engine.Favor:        {"功能", "索要", "44", "16", 6},
	engine.Shuffle:      {"功能", "洗牌", "33", "231", 7},
	engine.SeeTheFuture: {"功能", "预言", "75", "16", 8},

	// 五只猫：每类一个颜色，名字保持原版图案名
	engine.TacoCat:        {"猫牌", "卷饼", "208", "16", 10},
	engine.Cattermelon:    {"猫牌", "西瓜", "77", "16", 11},
	engine.HairyPotatoCat: {"猫牌", "土豆", "180", "16", 12},
	engine.BeardCat:       {"猫牌", "胡子", "139", "16", 13},
	engine.RainbowCat:     {"猫牌", "彩虹", "199", "231", 14},
}

// style 取一张牌的样式；未知牌兜底成"功能"。
func (m *Model) style(kind engine.Kind) cardStyle {
	if st, ok := palettes[kind]; ok {
		return st
	}
	return cardStyle{Category: "功能", Name: kind.Name(), Bg: "240", Fg: "231", Rank: 99}
}

// token 画一张牌：`[保命|拆弹]`。
//
// 块状模式：类别用底色，牌子名反白；文字模式：整段上色；无色模式：纯文本。
func (m *Model) token(kind engine.Kind) string {
	st := m.style(kind)
	text := "[" + st.Category + "|" + st.Name + "]"
	switch m.opts.Mode {
	case ui.ColorNone:
		return text
	case ui.ColorText:
		return m.renderer.NewStyle().Bold(true).Foreground(st.Bg).Render(text)
	default:
		return m.renderer.NewStyle().
			Background(st.Bg).Foreground(st.Fg).Bold(true).Render(text)
	}
}

// name 是牌名（不带类别），用在提示语里："你打出了 预言 牌"。
func (m *Model) name(kind engine.Kind) string { return m.style(kind).Name }

// category 是类别名，用来做"这是一张保命牌"这类说明。
func (m *Model) category(kind engine.Kind) string { return m.style(kind).Category }

// sortHand 按 Rank 排序，同 rank 保持相对顺序：
// 保命在最前（摸到炸弹时一眼看到），功能按常用度，猫牌按种类聚在一起，
// 于是"两张同款猫"永远相邻。
func sortHand(hand []engine.Card) []engine.Card {
	out := append([]engine.Card(nil), hand...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := palettes[out[i].Kind].Rank, palettes[out[j].Kind].Rank
		if ri != rj {
			return ri < rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// pairKind 找出手里"有两张以上"的猫牌种类，返回种类和可用的对子数。
// 对子盲抽靠它自动检测，玩家不用自己挑两张。
func (m *Model) pairKind() (engine.Kind, int) {
	counts := map[engine.Kind]int{}
	for _, c := range m.obs().Hand {
		if m.style(c.Kind).Category == "猫牌" {
			counts[c.Kind]++
		}
	}
	best, pairs := engine.Kind(""), 0
	for kind, n := range counts {
		if n/2 > 0 {
			pairs += n / 2
			if best == "" || palettes[kind].Rank < palettes[best].Rank {
				best = kind
			}
		}
	}
	return best, pairs
}

// bombChance 是"下一张摸到炸弹的概率"，返回百分比字符串。
func (m *Model) bombChance() string {
	o := m.obs()
	if o.DeckCount <= 0 {
		return "—"
	}
	pct := float64(o.BombsLeft) / float64(o.DeckCount) * 100
	return trimPercent(pct)
}

// trimPercent 保留一位小数。
func trimPercent(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}
