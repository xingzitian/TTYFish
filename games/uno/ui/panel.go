package tui

import "time"

// playersVisible 判断玩家面板现在该不该显示（Tab 弹出后 5 秒自动收回）。
func (m *Model) playersVisible() bool {
	return !m.playersUntil.IsZero() && time.Now().Before(m.playersUntil)
}
