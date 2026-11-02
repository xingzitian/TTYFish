package menu

import "testing"

func items() []Item {
	return []Item{
		{Label: "Game", Enabled: true},
		{Label: "浏览房间", Enabled: false},
		{Label: "Settings", Enabled: true},
		{Label: "Exit", Enabled: true},
	}
}

// TestMoveSkipsDisabled 上下移动要跳过不可选项，否则光标会卡在一个按不动的行上。
func TestMoveSkipsDisabled(t *testing.T) {
	m := New(nil, 0, 80, 24)
	m.SetItems(items())

	if _, ok := m.Selected(); !ok {
		t.Fatal("初始应该停在第一个可选项")
	}
	m.Move(1)
	item, ok := m.Selected()
	if !ok || item.Label != "Settings" {
		t.Fatalf("应该跳过不可选的「浏览房间」，实际 %+v", item)
	}
	m.Move(-1)
	if item, _ := m.Selected(); item.Label != "Game" {
		t.Fatalf("往回也应该跳过不可选项，实际 %+v", item)
	}
}

// TestSelectByNumber 数字直接选中；选到不可选项返回 false。
func TestSelectByNumber(t *testing.T) {
	m := New(nil, 0, 80, 24)
	m.SetItems(items())

	if !m.Select(3) {
		t.Fatal("第 3 项可选，应该成功")
	}
	if item, _ := m.Selected(); item.Label != "Settings" {
		t.Fatalf("应该选中 Settings，实际 %+v", item)
	}
	if m.Select(2) {
		t.Fatal("第 2 项是占位，不该选中")
	}
	if m.Select(9) {
		t.Fatal("越界不该选中")
	}
}

// TestSetItemsKeepsCursorInRange 换列表后光标不能越界。
func TestSetItemsKeepsCursorInRange(t *testing.T) {
	m := New(nil, 0, 80, 24)
	m.SetItems(items())
	m.Select(4)

	m.SetItems([]Item{{Label: "只有一个", Enabled: true}})
	if m.Cursor() != 0 {
		t.Fatalf("光标应该夹回 0，实际 %d", m.Cursor())
	}

	m.SetItems(nil)
	if _, ok := m.Selected(); ok {
		t.Fatal("空列表不该有选中项")
	}
}

// TestCursorOnDisabledWhenSetItems 换列表后光标落在不可选项上要自动挪开。
func TestCursorOnDisabledWhenSetItems(t *testing.T) {
	m := New(nil, 0, 80, 24)
	m.SetItems([]Item{{Label: "a", Enabled: true}, {Label: "b", Enabled: true}})
	m.Select(2)
	// 新列表里第 2 项不可选
	m.SetItems([]Item{{Label: "a", Enabled: true}, {Label: "b", Enabled: false}})
	if item, ok := m.Selected(); !ok {
		t.Fatalf("光标应该挪到可选项，实际 %+v", item)
	}
}

// TestView 画出来要有序号、光标和占位标记。
func TestViewMarksDisabled(t *testing.T) {
	m := New(nil, 0, 80, 24)
	m.SetItems(items())
	out := m.View()
	for _, want := range []string{"> 1.", "2. 浏览房间", "暂不可用"} {
		if !contains(out, want) {
			t.Fatalf("菜单缺少 %q：\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
