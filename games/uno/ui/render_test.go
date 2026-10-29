package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/xingzitian/TTYFish/games/uno/engine"
)

// TestRendererEmitsColor 验证注入的渲染器真的会被用上。
// 这是 SSH 场景的关键回归点：如果样式偷偷用回包级默认渲染器，
// 客户端拿到的就是纯文本，服务端却一切正常。
func TestRendererEmitsColor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile termenv.Profile
		want    string
	}{
		{"truecolor", termenv.TrueColor, "48;"},
		{"ansi256", termenv.ANSI256, "48;5;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := lipgloss.NewRenderer(&buf)
			// 必须显式设置：否则 lipgloss 会重新探测，非 TTY 下永远是 Ascii。
			r.SetColorProfile(tc.profile)

			m := NewModel(
				Identity{Key: "test", Name: "你"},
				testClient(sampleSnap(20241029)),
				Options{Bots: 3, Rules: engine.Rules{}, BotLag: time.Hour},
				r,
			)
			defer m.Close()
			m.SetSize(80, 24)

			out := m.View()
			t.Logf("请求 profile=%v 实际 profile=%v", tc.profile, r.ColorProfile())
			if !strings.Contains(out, tc.want) {
				t.Fatalf("没有产生背景色转义 %q，输出:\n%q", tc.want, out)
			}
		})
	}
}
