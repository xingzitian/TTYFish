package ui

// Layout 是界面的信息密度：丰富模式画得全，极简模式只留几行。
//
// 它和 ColorMode 是正交的两件事：ColorMode 决定"怎么上色"，
// Layout 决定"画多少"。极简模式照样支持满色块，不然摸鱼时就没法一眼认牌。
type Layout int

const (
	// LayoutRich 是默认的完整界面：牌桌、玩家面板、出牌记录都在。
	LayoutRich Layout = iota
	// LayoutCompact 只留 4~5 行，适合小终端和上班摸鱼。
	LayoutCompact
)

func (l Layout) String() string {
	if l == LayoutCompact {
		return "极简"
	}
	return "丰富"
}

// Slug 是落盘用的稳定名字。
func (l Layout) Slug() string {
	if l == LayoutCompact {
		return "compact"
	}
	return "rich"
}

// ParseLayout 解析落盘值；不认识就返回 false，交给调用方用默认值。
func ParseLayout(s string) (Layout, bool) {
	switch s {
	case "rich":
		return LayoutRich, true
	case "compact":
		return LayoutCompact, true
	default:
		return LayoutRich, false
	}
}

// Switch 在两种模式之间切换。
func (l Layout) Switch() Layout {
	if l == LayoutCompact {
		return LayoutRich
	}
	return LayoutCompact
}
