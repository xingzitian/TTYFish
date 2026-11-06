package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResolveActionUsesCurrentContextAndAliases(t *testing.T) {
	actions := []Action{
		{ID: "modal-cancel", Key: "Esc", Label: "返回"},
		{ID: "leave", Key: "Esc", Label: "离开"},
		{ID: "draw", Key: "D", Label: "摸牌", Aliases: []string{"down"}},
	}
	for _, tc := range []struct{ key, want string }{{"esc", "modal-cancel"}, {"D", "draw"}, {"down", "draw"}} {
		got, ok := ResolveAction(actions, tc.key)
		if !ok || got != tc.want {
			t.Fatalf("ResolveAction(%q) = %q, %v; want %q", tc.key, got, ok, tc.want)
		}
	}
	if _, ok := ResolveAction(actions, "x"); ok {
		t.Fatal("unknown key matched")
	}
	if got := ActionTokens(actions)[2]; got != "[D]摸牌" {
		t.Fatalf("token = %q", got)
	}
}

func TestActionGroupWrapsChineseAndKeepsActions(t *testing.T) {
	group := ActionGroup{Title: "出牌", Actions: []Action{{Key: "D", Label: "摸牌"}, {Key: "P", Label: "过牌"}, {Key: "U", Label: "喊UNO"}}}
	identity := func(s ...string) string { return strings.Join(s, "") }
	rows := RenderActionGroup(group, 18, identity, identity)
	joined := strings.Join(rows, "\n")
	for _, token := range ActionTokens(group.Actions) {
		if !strings.Contains(joined, token) {
			t.Fatalf("missing %q in %q", token, joined)
		}
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 18 {
			t.Fatalf("row is too wide: %q", row)
		}
	}
}

func TestCyclicWindowWrapsForTwoSeats(t *testing.T) {
	if got := CyclicWindow("A B ", -2, 8); got != "B A B A " {
		t.Fatalf("window = %q", got)
	}
}

func TestNumberedActionsResolveChoices(t *testing.T) {
	actions := NumberedActions([]string{"你好", "再见"})
	if len(actions) != 2 || actions[1].Token() != "[2]再见" {
		t.Fatalf("unexpected choices: %+v", actions)
	}
	if id, ok := ResolveAction(actions, "2"); !ok || id != "1" {
		t.Fatalf("second choice = %q, %v", id, ok)
	}
}

func TestWrapTokensPreservesFirstActionWhenPrefixIsWide(t *testing.T) {
	rows := WrapTokens([]string{"[D]摸牌", "[P]过牌"}, ">> 快捷操作: ", 12)
	joined := strings.Join(rows, "\n")
	for _, token := range []string{"[D]摸牌", "[P]过牌"} {
		if !strings.Contains(joined, token) {
			t.Fatalf("missing %q in %q", token, joined)
		}
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 12 {
			t.Fatalf("row too wide: %q", row)
		}
	}
}

func TestRenderActionsDoesNotInterpretLabelBrackets(t *testing.T) {
	normal := func(s ...string) string { return strings.Join(s, "") }
	highlight := func(s ...string) string { return "<" + strings.Join(s, "") + ">" }
	tokens := RenderActions([]Action{{Key: "D", Label: "摸牌 [剩2]"}, {Label: "等待 [玩家]"}}, normal, highlight)
	if got := strings.Join(tokens, " "); got != "<[D]>摸牌 [剩2] 等待 [玩家]" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestPrimaryKeyAndNumericRangeDriveDisplayAndMatching(t *testing.T) {
	action := Action{ID: "play", Key: "1", Range: true, Label: "出牌", Aliases: []string{"2", "3"}}
	if got := action.Token(); got != "[1-3]出牌" {
		t.Fatalf("range label = %q", got)
	}
	if id, ok := ResolveAction([]Action{action}, "3"); !ok || id != "play" {
		t.Fatalf("range key = %q, %v", id, ok)
	}
	action = Action{ID: "draw", Key: "X", Label: "摸牌", Aliases: []string{"down"}}
	if action.Token() != "[X]摸牌" {
		t.Fatalf("key label = %q", action.Token())
	}
	if _, ok := ResolveAction([]Action{action}, "d"); ok {
		t.Fatal("previous key must not match")
	}
	if id, ok := ResolveAction([]Action{action}, "x"); !ok || id != "draw" {
		t.Fatalf("new key = %q, %v", id, ok)
	}
}

func TestValidateActionsDetectsConflictingKeysAndIDs(t *testing.T) {
	if err := ValidateActions([]Action{{ID: "draw", Key: "D"}, {ID: "other", Key: "X", Aliases: []string{"d"}}}); err == nil {
		t.Fatal("case-insensitive alias collision must be rejected")
	}
	if err := ValidateActions([]Action{{ID: "draw", Key: "D"}, {ID: "draw", Key: "X"}}); err == nil {
		t.Fatal("duplicate action ID must be rejected")
	}
	if err := ValidateActions([]Action{{ID: "draw", Key: "D", Aliases: []string{"down"}}, {Label: "waiting"}, {ID: "cancel", Key: "Esc"}}); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
}

func TestCombinedKeyHintFollowsBindings(t *testing.T) {
	actions := []Action{{ID: "previous", Key: "A"}, {ID: "next", Key: "Z"}}
	hint := CombinedKeyHint("翻页", SelectActions(actions, "previous", "next")...)
	if got := hint.Token(); got != "[AZ]翻页" {
		t.Fatalf("hint = %q", got)
	}
	if _, ok := ResolveAction([]Action{hint}, "a"); ok {
		t.Fatal("combined hint must not dispatch")
	}
}
