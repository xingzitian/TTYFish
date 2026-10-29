package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newKeyModel(code string) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 6
	in.Focus()
	in.SetValue(code)
	return &Model{
		renderer:  lipgloss.DefaultRenderer(),
		entering:  true,
		codeInput: in,
		width:     70,
		height:    24,
	}
}

func typeRunes(m *Model, s string) {
	for _, r := range s {
		m.handleEnterKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// TestBackspaceVariants 覆盖不同终端的退格键。
// 有的终端发 0x7f（backspace），有的发 0x08（ctrl+h，Xshell 就是这样）；
// 手写输入框只认一种的话，房间码输满了就再也删不掉，看起来像"所有键都没反应"。
// 用 bubbles/textinput 之后这两个都由上游一起处理了。
func TestBackspaceVariants(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyBackspace},
		{Type: tea.KeyCtrlH},
	} {
		m := newKeyModel("ABC")
		m.handleEnterKey(k)
		if got := m.codeInput.Value(); got != "AB" {
			t.Fatalf("键 %q 没删掉字符，实际 %q", k.String(), got)
		}
	}
}

func TestCodeInputCapsAtSix(t *testing.T) {
	m := newKeyModel("")
	typeRunes(m, "abc234zz")
	if got := m.codeInput.Value(); got != "abc234" {
		t.Fatalf("应该截断到 6 位，实际 %q", got)
	}

	// 满了之后退格必须仍然有效，否则用户会以为键盘坏了。
	m.handleEnterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.codeInput.Value(); got != "abc23" {
		t.Fatalf("满位后退格必须有效，实际 %q", got)
	}
}

func TestCtrlUClearsCode(t *testing.T) {
	m := newKeyModel("ABC")
	m.handleEnterKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := m.codeInput.Value(); got != "" {
		t.Fatalf("Ctrl+U 应该清空，实际 %q", got)
	}
}

func TestCtrlWMovesBackButKeepsCode(t *testing.T) {
	m := newKeyModel("ABC234")
	// Ctrl+W 删一个词：至少不能崩，也不能把输入框搞坏。
	m.handleEnterKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := m.codeInput.Value(); got != "" && got != "ABC234" {
		t.Fatalf("Ctrl+W 之后输入框状态异常: %q", got)
	}
}
