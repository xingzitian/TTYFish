package tui

import (
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ProfileFor 按 TERM 推断客户端色深，不做任何终端探测。
//
// 刻意不用 wish 的 bm.MakeRenderer、也不用 lipgloss 的默认渲染器：
// 它们为了判断背景色会往终端发 OSC 11 并同步读回复。客户端要是不回
// （哑终端、mosh、某些 SSH 网关），那个读循环会把用户按下的第一个键
// 当成回复吃掉，甚至直接卡在启动阶段。我们只用到颜色，按 TERM 推断最稳。
func ProfileFor(term string) termenv.Profile {
	t := strings.ToLower(term)
	switch {
	case t == "" || t == "dumb":
		return termenv.Ascii
	case strings.Contains(t, "truecolor"), strings.Contains(t, "24bit"):
		return termenv.TrueColor
	case strings.Contains(t, "256color"):
		return termenv.ANSI256
	default:
		return termenv.ANSI
	}
}

// noProbe 把底层 writer 藏起来，让 termenv 认不出它是终端文件。
// 我们不靠探测拿色深（已经显式 set 了），留着探测反而会往终端发
// OSC 11 查询、同步等回复——在哑终端上会吞掉第一个按键甚至卡住启动。
type noProbe struct{ io.Writer }

// NewRenderer 为一条输出流构造渲染器。
//
// 注意必须用 SetColorProfile 而不是 termenv.WithProfile：lipgloss 在没被
// 显式设置过色深时会自己重新探测一遍（见 Renderer.ColorProfile），而
// SSH 会话/管道都不是 TTY，探测结果永远是 Ascii——客户端上就是一片黑白。
func NewRenderer(w io.Writer, term string) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(noProbe{w})
	r.SetColorProfile(ProfileFor(term))
	return r
}
