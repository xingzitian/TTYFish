package ui

import "strings"

// ColorMode 是整个界面的颜色深浅，不是只作用于牌面。
//
// 不同终端对颜色的支持差别很大：有的背景色块会留缝、有的把整行染成刺眼的
// 色带、有的干脆是黑白终端。所以给三档，一个热键切换，**所有元素一起变**：
//
//	ColorBlock 满色：文字上色 + 背景色块（默认）
//	ColorText  只给文字上色，背景透明
//	ColorNone  完全不上色，靠加粗和下划线区分层次
type ColorMode int

const (
	ColorBlock ColorMode = iota
	ColorText
	ColorNone
)

func (c ColorMode) String() string {
	switch c {
	case ColorText:
		return "文字"
	case ColorNone:
		return "无色"
	default:
		return "满色"
	}
}

// Slug 是落盘用的稳定名字（别把中文写进配置文件）。
func (c ColorMode) Slug() string {
	switch c {
	case ColorText:
		return "text"
	case ColorNone:
		return "plain"
	default:
		return "block"
	}
}

// ParseColorMode 解析命令行/配置文件里的样式名。
func ParseColorMode(s string) (ColorMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "block", "full", "满色", "色块":
		return ColorBlock, true
	case "text", "文字":
		return ColorText, true
	case "plain", "none", "mono", "无色":
		return ColorNone, true
	}
	return ColorBlock, false
}

// Next 循环切换，给运行时热键用。
func (c ColorMode) Next() ColorMode {
	switch c {
	case ColorBlock:
		return ColorText
	case ColorText:
		return ColorNone
	default:
		return ColorBlock
	}
}
