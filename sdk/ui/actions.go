package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Action describes a keyboard operation independently of the game implementing it.
// Key is the primary terminal key and its default display; Aliases are extra
// accepted keys. Range derives a numeric range label from the bound keys.
type Action struct {
	ID    string
	Key   string
	Range bool
	Label string
	// Detail is an optional longer label for help and rich views.
	Detail  string
	Aliases []string
}

// EscapeAction is the shared cancel binding; ownership is decided by the
// currently active modal or, otherwise, by the host.
func EscapeAction(label string) Action { return Action{ID: "cancel", Key: "Esc", Label: label} }

// BossAction is the common, always-available terminal concealment shortcut.
func BossAction() Action { return Action{ID: "boss", Key: "Ctrl+B", Label: "老板键"} }

func BossExitAction(enter bool) Action {
	action := EscapeAction("")
	if enter {
		action.Aliases = []string{"enter"}
	}
	return action
}

func InterruptAction() Action { return Action{ID: "interrupt", Key: "Ctrl+C"} }

// HelpActions derives modal close bindings from the game's help and quit keys.
func HelpActions(common []Action, helpID, quitID string) []Action {
	var result []Action
	for _, action := range SelectActions(common, helpID) {
		action.ID, action.Label, action.Detail = "close-help", "关闭", ""
		result = append(result, action)
	}
	result = append(result, EscapeAction("关闭"))
	return append(result, SelectActions(common, quitID)...)
}

func HelpCloseHint(actions []Action) string {
	for _, action := range actions {
		if action.ID == "close-help" {
			keys := append([]string{action.DisplayKey()}, action.Aliases...)
			return fmt.Sprintf("按 %s 或 %s 关闭", strings.Join(keys, " "), EscapeAction("").DisplayKey())
		}
	}
	return "按 Esc 关闭"
}

func (a Action) DisplayKey() string {
	if a.Range && len(a.Key) == 1 && a.Key[0] >= '0' && a.Key[0] <= '9' {
		last := a.Key[0]
		for _, alias := range a.Aliases {
			if len(alias) != 1 || alias[0] != last+1 {
				return a.Key
			}
			last = alias[0]
		}
		if last != a.Key[0] {
			return a.Key + "-" + string(last)
		}
	}
	return a.Key
}

func (a Action) HelpLabel() string {
	if a.Detail != "" {
		return a.Detail
	}
	return a.Label
}

// ActionGroup is a named set of operations for a help screen.
type ActionGroup struct {
	Title   string
	Actions []Action
}

// RenderActionGroup renders key labels with the supplied styles and wraps on
// action boundaries. Styles are supplied by the host so colour preferences are
// respected in both compact and rich layouts.
func RenderActionGroup(group ActionGroup, width int, dim, highlight func(...string) string) []string {
	prefix := fmt.Sprintf("%-5s", "["+group.Title+"]")
	tokens := make([]string, 0, len(group.Actions))
	for _, action := range group.Actions {
		tokens = append(tokens, highlight("["+action.DisplayKey()+"]")+action.HelpLabel())
	}
	return WrapTokens(tokens, dim(prefix)+" ", width)
}

// RenderHelp lays out a title, optional hint and the registered action groups.
func RenderHelp(groups []ActionGroup, width int, title, hint string, dim, highlight func(...string) string) string {
	head := highlight(title)
	if pad := width - lipgloss.Width(title) - lipgloss.Width(hint); pad > 0 {
		head += strings.Repeat(" ", pad) + dim(hint)
	}
	lines := []string{head}
	for _, group := range groups {
		lines = append(lines, RenderActionGroup(group, width, dim, highlight)...)
	}
	return strings.Join(lines, "\n")
}

// Token formats the action for a compact command line.
func (a Action) Token() string { return "[" + a.DisplayKey() + "]" + a.Label }

// ResolveAction returns the first matching action. Callers construct the list
// for the current context; putting modal actions first gives them priority.
func ResolveAction(actions []Action, key string) (string, bool) {
	for _, action := range actions {
		if action.ID == "" {
			continue
		}
		if action.Key != "" && strings.EqualFold(action.Key, key) {
			return action.ID, true
		}
		for _, candidate := range action.Aliases {
			if strings.EqualFold(candidate, key) {
				return action.ID, true
			}
		}
	}
	return "", false
}

// ValidateActions detects ambiguous bindings within one active scope. Different
// scopes may reuse keys because the caller decides which scope owns input.
// Display-only items have an empty ID and are ignored.
func ValidateActions(actions []Action) error {
	ids := make(map[string]bool)
	keys := make(map[string]string)
	for _, action := range actions {
		if action.ID == "" {
			continue
		}
		if ids[action.ID] {
			return fmt.Errorf("duplicate action ID %q", action.ID)
		}
		ids[action.ID] = true
		if action.Key == "" {
			return fmt.Errorf("action %q has no primary key", action.ID)
		}
		for _, key := range append([]string{action.Key}, action.Aliases...) {
			if key == "" {
				return fmt.Errorf("action %q has an empty key", action.ID)
			}
			normalized := strings.ToLower(key)
			if owner, ok := keys[normalized]; ok {
				return fmt.Errorf("key %q is bound by both %q and %q", key, owner, action.ID)
			}
			keys[normalized] = action.ID
		}
	}
	return nil
}

// ActionTokens produces the same labels for compact and rich views.
func ActionTokens(actions []Action) []string {
	tokens := make([]string, 0, len(actions))
	for _, action := range actions {
		tokens = append(tokens, action.Token())
	}
	return tokens
}

// RenderActions styles the key and label from their structured fields, without
// interpreting brackets that may appear in labels or game-provided text.
func RenderActions(actions []Action, normal, highlight func(...string) string) []string {
	tokens := make([]string, 0, len(actions))
	for _, action := range actions {
		if action.DisplayKey() == "" {
			tokens = append(tokens, normal(action.Label))
			continue
		}
		tokens = append(tokens, highlight("["+action.DisplayKey()+"]")+normal(action.Label))
	}
	return tokens
}

// NumberedActions maps a list of choices to the terminal's single-digit keys.
// IDs are zero-based indices so callers can look up the selected value.
func NumberedActions(labels []string) []Action {
	actions := make([]Action, 0, min(len(labels), 9))
	for i, label := range labels {
		if i == 9 {
			break
		}
		key := strconv.Itoa(i + 1)
		actions = append(actions, Action{ID: strconv.Itoa(i), Key: key, Label: label})
	}
	return actions
}

// SelectActions preserves the requested presentation order while reusing the
// same registered action definitions for different views.
func SelectActions(actions []Action, ids ...string) []Action {
	selected := make([]Action, 0, len(ids))
	for _, id := range ids {
		for _, action := range actions {
			if action.ID == id {
				selected = append(selected, action)
				break
			}
		}
	}
	return selected
}

// CombinedKeyHint displays several independently dispatched actions together,
// for example the previous/next page keys. It is display-only.
func CombinedKeyHint(label string, actions ...Action) Action {
	var keys strings.Builder
	for _, action := range actions {
		keys.WriteString(action.DisplayKey())
	}
	return Action{Key: keys.String(), Label: label}
}

// WrapTokens keeps complete items together where possible, respecting terminal
// cell widths and ANSI escape sequences.
func WrapTokens(tokens []string, prefix string, width int) []string {
	if width <= 0 {
		return []string{prefix + strings.Join(tokens, " ")}
	}
	var rows []string
	line := prefix
	indent := strings.Repeat(" ", min(lipgloss.Width(prefix), max(0, width-1)))
	hasToken := false
	for _, token := range tokens {
		sep := ""
		if hasToken {
			sep = " "
		}
		if lipgloss.Width(line+sep+token) > width && (hasToken || line == prefix && prefix != "") {
			rows = append(rows, ansi.Truncate(line, width, ""))
			line, sep = indent, ""
			hasToken = false
		}
		// Keep a complete item when it fits by dropping continuation indentation.
		if !hasToken && lipgloss.Width(line+token) > width && lipgloss.Width(token) <= width {
			line = ""
		}
		line += sep + token
		hasToken = true
	}
	return append(rows, ansi.Truncate(line, width, ""))
}
