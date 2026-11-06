package gateway

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xingzitian/TTYFish/sdk/ui"
)

func TestMenuActionScopesAreUnambiguous(t *testing.T) {
	for name, actions := range map[string][]ui.Action{"menu": menuActions, "join": joinActions, "leave-confirm": leaveConfirmActions} {
		if err := ui.ValidateActions(actions); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestMenuRebindingUpdatesPromptAndDispatch(t *testing.T) {
	old := menuActions[1]
	t.Cleanup(func() { menuActions[1] = old })
	menuActions[1].Key = "X"
	r, _ := newRoot(t)
	r.Init()
	if !strings.Contains(strip(r.cmdLine()), "[X]下一项") {
		t.Fatal("menu prompt did not follow new key")
	}
	press(r, "=")
	if r.menu.Cursor() != 0 {
		t.Fatal("old key still moved menu")
	}
	press(r, "x")
	if r.menu.Cursor() != 1 {
		t.Fatal("new key did not move menu")
	}
}

func TestLeaveConfirmationRebindingUpdatesPromptAndDispatch(t *testing.T) {
	old := leaveConfirmActions[0]
	t.Cleanup(func() { leaveConfirmActions[0] = old })
	leaveConfirmActions[0].Key = "X"
	r := newEscRoot(t, &fakeModel{})
	pressEsc(r)
	if !strings.Contains(strip(r.View()), "[X]退出对局") {
		t.Fatal("confirmation prompt did not follow binding")
	}
	press(r, "y")
	if !r.confirmLeave {
		t.Fatal("old confirmation key still active")
	}
	press(r, "x")
	if r.level != levelGame {
		t.Fatal("new confirmation key did not leave room")
	}
}

func TestMenuCommandsWrapAtNarrowWidth(t *testing.T) {
	r, _ := newRoot(t)
	r.Init()
	r.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	view := strip(r.View())
	for _, token := range []string{"[-]上一项", "[=]下一项", "[Enter]选择", "[Esc]退出"} {
		if !strings.Contains(view, token) {
			t.Fatalf("missing %q in:\n%s", token, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}
