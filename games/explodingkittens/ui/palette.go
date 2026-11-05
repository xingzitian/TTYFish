package tui

import (
	"sort"

	"github.com/charmbracelet/lipgloss"

	"github.com/xingzitian/TTYFish/games/explodingkittens/engine"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

// cardStyle 是一张牌在界面上的样子。
//
// 五张猫牌在规则里完全等价（凑对子/三张只认"同一种"），名字对玩家没有信息量，
// 所以用颜色区分，名字取本地化里最直观的叫法（橘猫、三花猫……）。
type cardStyle struct {
	// Label 是完整中文名，用于提示、日志这类需要说清楚的地方。
	Label string
	// Short 是牌面里那一个字：`[橙|橘]` 的"橘"。
	Short string
	// Ink 是颜色名，用于无色模式下的文字说明。
	Ink string
	// Bg/Fg 是 256 色里的色号。
	Bg lipgloss.Color
	Fg lipgloss.Color
	// Rank 决定手牌排序：同类挨在一起，凑组合时一眼能看出有几张。
	Rank int
}

var palettes = map[engine.Kind]cardStyle{
	engine.ExplodingKitten: {"爆炸猫", "爆", "红", "196", "231", 0},
	engine.Defuse:          {"拆弹", "拆", "绿", "41", "16", 1},
	engine.Nope:            {"反制", "否", "紫", "129", "231", 2},
	engine.Attack:          {"攻击", "攻", "橙", "208", "16", 3},
	engine.Skip:            {"跳过", "跳", "黄", "220", "16", 4},
	engine.Favor:           {"索要", "要", "青", "44", "16", 5},
	engine.Shuffle:         {"洗牌", "洗", "蓝", "33", "231", 6},
	engine.SeeTheFuture:    {"预知未来", "预", "蓝", "33", "231", 7},

	// 五只猫：颜色即身份，名字带本地化味道
	engine.TacoCat:        {"橘猫", "橘", "橙", "208", "16", 10},
	engine.Cattermelon:    {"三花猫", "花", "品红", "199", "231", 11},
	engine.HairyPotatoCat: {"奶牛猫", "奶", "白", "255", "16", 12},
	engine.BeardCat:       {"金渐层", "金", "金", "220", "16", 13},
	engine.RainbowCat:     {"蓝猫", "蓝", "蓝", "33", "231", 14},
}

// fallback 给未知牌兜底：宁可显示原名，也不要空着。
func (m *Model) style(kind engine.Kind) cardStyle {
	if st, ok := palettes[kind]; ok {
		return st
	}
	name := kind.Name()
	return cardStyle{Label: name, Short: "?", Ink: "?", Bg: "240", Fg: "231", Rank: 99}
}

// token 画一张牌：`[橙|橘]`。块状模式是色块，文字模式只上色，无色模式是纯文本。
func (m *Model) token(kind engine.Kind) string {
	st := m.style(kind)
	text := "[" + st.Ink + "|" + st.Short + "]"
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

// name 是这张牌的完整名字（需要说清楚时用，例如"你摸到了爆炸猫"）。
func (m *Model) name(kind engine.Kind) string { return m.style(kind).Label }

// sortHand 按 Rank 排序，同 rank 保持原来的相对顺序（ID 小在前），
// 这样同色的猫牌永远挨在一起：按 1、2 就是一对。
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
