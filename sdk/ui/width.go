// Package ui 是所有游戏界面共用的那点东西：终端宽度计算、颜色深浅、整帧裁剪。
//
// 它不认识任何具体游戏，也不认识平台。
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// CutWidth 按显示宽度截断，中文算 2 格。
func CutWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// PadRight 按显示宽度补空格。
func PadRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// PadBetween 把左右两段拉开到整行宽度；放不下时至少留一格。
func PadBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// JoinWrapped 按宽度把若干片段折行，避免窄终端下被终端自己硬折。
func JoinWrapped(parts []string, width int) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	line := 0
	for i, p := range parts {
		w := lipgloss.Width(p)
		if i > 0 {
			if line+1+w > width {
				b.WriteString("\n")
				line = 0
			} else {
				b.WriteString(" ")
				line++
			}
		}
		b.WriteString(p)
		line += w
	}
	return b.String()
}

// TailN 取最后 n 个元素。
func TailN[T any](xs []T, n int) []T {
	if n <= 0 {
		return nil
	}
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

// ClampFrame 把整帧裁进终端尺寸。
//
// 任何一行超宽、或者总行数超过终端高度，终端就会自己折行/滚动，
// 顶上那几行被顶歪——表现就是"消息一长，界面就乱了"。
// 每个游戏的 View 出口都该套一层。
func ClampFrame(s string, width, height int) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	if width > 0 {
		for i, l := range lines {
			if lipgloss.Width(l) > width {
				lines[i] = ansi.Truncate(l, width, "")
			}
		}
	}
	return strings.Join(lines, "\n")
}
