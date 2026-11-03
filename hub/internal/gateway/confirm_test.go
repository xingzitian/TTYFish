package gateway

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestConfirmCancelKeepsPlaying 确认框里按 N 或 Esc 都取消，人还在牌桌上。
func TestConfirmCancelKeepsPlaying(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		r := newEscRoot(t, &fakeModel{})
		pressEsc(r)
		if !r.confirmLeave {
			t.Fatalf("[%s] 应该先弹确认", key)
		}

		if key == "esc" {
			pressEsc(r)
		} else {
			r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
		}

		if r.confirmLeave {
			t.Fatalf("[%s] 应该取消确认", key)
		}
		if r.level != levelTable || r.roomClient == nil {
			t.Fatalf("[%s] 取消后人应该还在牌桌上，实际 level=%v", key, r.level)
		}
	}
}

// TestConfirmIgnoresOtherKeys 确认框里按别的键什么都不做——这里最怕误触。
func TestConfirmIgnoresOtherKeys(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})
	pressEsc(r)

	before := strip(r.View())
	// q 不在此列：它是确认态里的逃生口，另有 TestQEscapeFromConfirm 覆盖
	for _, key := range []string{"1", "2", "u", "d", "t", "i", "enter"} {
		if key == "enter" {
			r.Update(tea.KeyMsg{Type: tea.KeyEnter})
		} else {
			r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
	}
	if !r.confirmLeave || r.level != levelTable {
		t.Fatalf("确认框不该被别的键弄掉：confirm=%v level=%v", r.confirmLeave, r.level)
	}
	if after := strip(r.View()); after != before {
		t.Fatalf("界面不该因为无关按键变化：\n前:%s\n后:%s", before, after)
	}
}

// TestConfirmShowsOptions 确认行要把两个选项写清楚。
func TestConfirmShowsOptions(t *testing.T) {
	r := newEscRoot(t, &fakeModel{})
	pressEsc(r)

	view := strip(r.View())
	for _, want := range []string{"离开这局", "[Y]", "[N]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("确认行缺少 %q：\n%s", want, view)
		}
	}
}

// TestModalEscDoesNotPrompt 弹窗时 Esc 关弹窗，不该弹离开确认。
func TestModalEscDoesNotPrompt(t *testing.T) {
	r := newEscRoot(t, &modalModel{modal: true})
	pressEsc(r)
	if r.confirmLeave {
		t.Fatal("弹窗时 Esc 只该关弹窗，不该问要不要离开")
	}
}
