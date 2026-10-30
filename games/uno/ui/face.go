package tui

import "github.com/xingzitian/TTYFish/games/uno/engine"

// compactFace 是极简模式里牌的写法：颜色|面值，例如 [R|7]、[G|S]、[W|+4]。
//
// 和丰富模式的 cardFace 分开：极简要一行塞下 5 张，万能牌统一写 W，
// 面值用 +2/+4 而不是符号，扫一眼就知道是什么牌。
func compactFace(c engine.Card) string {
	col := colorLetter(c.Color)
	if c.Color == engine.NoColor {
		col = "W"
	}
	var v string
	switch c.Kind {
	case engine.Skip:
		v = "S"
	case engine.Reverse:
		v = "R"
	case engine.DrawTwo:
		v = "+2"
	case engine.Wild:
		v = "W"
	case engine.WildDrawFour:
		v = "+4"
	default:
		v = strconvNum(c.Num)
	}
	return col + "|" + v
}

func strconvNum(n int) string {
	if n < 0 {
		return "?"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return "?"
}
