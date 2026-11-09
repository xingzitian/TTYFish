package ui

import (
	"fmt"
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

func TestWrapTokensPreservesLongChineseAction(t *testing.T) {
	token := "[X]这是一个特别长的中文操作说明需要完整显示"
	rows := WrapTokens([]string{token}, "[CMD] ", 12)
	joined := strings.Join(rows, "")
	if !strings.Contains(joined, token) {
		t.Fatalf("long action lost content: %q", joined)
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

func TestIndexedActionSupportsNonNumericBindings(t *testing.T) {
	action := Action{ID: "card", Key: "X", Range: true, Aliases: []string{"Y", "Z"}}
	if got := action.DisplayKey(); got != "X/Y/Z" {
		t.Fatalf("display = %q", got)
	}
	for _, tc := range []struct {
		key   string
		index int
	}{{"x", 0}, {"Y", 1}, {"z", 2}} {
		if got, ok := ResolveIndexedAction(action, tc.key); !ok || got != tc.index {
			t.Fatalf("%q resolved to %d, %v", tc.key, got, ok)
		}
	}
	if _, ok := ResolveIndexedAction(action, "1"); ok {
		t.Fatal("unbound key matched")
	}
}

func TestValidateActionsDetectsConflictingKeysAndIDs(t *testing.T) {
	if err := ValidateActions([]Action{{ID: "draw", Key: "D"}, {ID: "other", Key: "X", Aliases: []string{"d"}}}); err == nil {
		t.Fatal("case-insensitive alias collision must be rejected")
	}
	if err := ValidateActions([]Action{{ID: "a", Key: "s"}, {ID: "b", Key: "ſ"}}); err == nil {
		t.Fatal("registration must use the same Unicode case folding as dispatch")
	}
	if err := ValidateActions([]Action{{ID: "draw", Key: "D"}, {ID: "draw", Key: "X"}}); err == nil {
		t.Fatal("duplicate action ID must be rejected")
	}
	if err := ValidateActions([]Action{{ID: "draw", Key: "D", Aliases: []string{"down"}}, {Label: "waiting"}, {ID: "cancel", Key: "Esc"}}); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
}

func TestRegistrationRejectsConflictsAndCopiesBindings(t *testing.T) {
	if _, err := RegisterActions([]Action{{ID: "a", Key: "X"}, {ID: "b", Key: "x"}}); err == nil {
		t.Fatal("registration accepted conflicting keys")
	}
	original := []Action{{ID: "a", Key: "X", Aliases: []string{"Y"}}}
	registered, err := RegisterActions(original)
	if err != nil {
		t.Fatal(err)
	}
	original[0].Aliases[0] = "Z"
	if _, ok := ResolveAction(registered, "y"); !ok {
		t.Fatal("registered aliases changed with input")
	}
	modal := []Action{{ID: "modal", Key: "Esc"}}
	ordinary := []Action{{ID: "leave", Key: "Esc"}}
	if _, err := RegisterActions(modal); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterActions(ordinary); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterActions(append(modal, ordinary...)); err == nil {
		t.Fatal("same-scope conflict was accepted")
	}
}

func TestActionUnionChecksSimultaneouslyActiveScopes(t *testing.T) {
	common := []Action{{ID: "cancel", Key: "Esc", Label: "取消"}, {ID: "help", Key: "?"}}
	view := []Action{{ID: "cancel", Key: "Esc", Label: "返回"}, {ID: "play", Key: "1"}}
	if err := ValidateActionUnion(common, view); err != nil {
		t.Fatalf("same binding rejected: %v", err)
	}
	view[1].Key = "?"
	if err := ValidateActionUnion(common, view); err == nil {
		t.Fatal("cross-scope key collision accepted")
	}
	view[1].Key = "1"
	view[0].Key = "X"
	if err := ValidateActionUnion(common, view); err == nil {
		t.Fatal("same ID with different bindings accepted")
	}
}

func TestCheckedActionsShowsErrorWithoutDispatchOrPanic(t *testing.T) {
	actions := CheckedActions("example", []Action{{ID: "a", Key: "X"}}, []Action{{ID: "b", Key: "x"}})
	if len(actions) != 1 || !strings.Contains(actions[0].Token(), "按键配置错误") {
		t.Fatalf("missing error: %+v", actions)
	}
	if !ActionScopeInvalid(actions) {
		t.Fatal("invalid scope was not marked")
	}
	if _, ok := ResolveAction(actions, "x"); ok {
		t.Fatal("invalid scope dispatched a key")
	}
}

func TestStaticRegistrationReportsScopeAndConflict(t *testing.T) {
	defer func() {
		value := recover()
		if value == nil || !strings.Contains(fmt.Sprint(value), "example") || !strings.Contains(fmt.Sprint(value), "x") {
			t.Fatalf("missing registration error detail: %v", value)
		}
	}()
	MustRegisterActions("example", []Action{{ID: "a", Key: "X"}, {ID: "b", Key: "x"}})
}

func TestHelpViewportShowsAllLinesAndClampsOffset(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\nsix"
	view, last := HelpViewport(content, 40, 4, 0)
	if last != 3 || !strings.Contains(view, "one") || !strings.Contains(view, "翻看") {
		t.Fatalf("first page: %q last=%d", view, last)
	}
	offset := MoveHelpOffset("help-page-down", 0, last, 4)
	view, _ = HelpViewport(content, 40, 4, offset)
	if !strings.Contains(view, "six") || strings.Contains(view, "one") {
		t.Fatalf("last page: %q", view)
	}
	if MoveHelpOffset("help-page-down", offset, last, 4) != last {
		t.Fatal("offset exceeded end")
	}
	if MoveHelpOffset("help-page-up", offset, last, 4) != 0 {
		t.Fatal("page up did not return to start")
	}
	var screens strings.Builder
	for offset := 0; offset <= last; offset++ {
		page, _ := HelpViewport(content, 40, 4, offset)
		screens.WriteString(page)
		screens.WriteByte('\n')
	}
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(screens.String(), line) {
			t.Fatalf("line %q is unreachable", line)
		}
	}
}

func TestFrameViewportExposesEveryLine(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive"
	view, last := FrameViewport(content, 40, 3, 0)
	if last != 3 || !strings.Contains(view, "Ctrl+") {
		t.Fatalf("viewport: %q last=%d", view, last)
	}
	var seen strings.Builder
	for offset := 0; offset <= last; offset++ {
		page, _ := FrameViewport(content, 40, 3, offset)
		seen.WriteString(page)
	}
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(seen.String(), line) {
			t.Fatalf("line %q unreachable", line)
		}
	}
	if MoveFrameOffset("frame-page-down", 0, last, 3) != 2 {
		t.Fatal("frame page movement is incorrect")
	}
}

func TestFrameViewportWrapsLongLinesWithoutLosingText(t *testing.T) {
	content := "这是一个非常长的中文状态说明需要完整显示"
	_, last := FrameViewport(content, 10, 2, 0)
	var seen strings.Builder
	for offset := 0; offset <= last; offset++ {
		page, _ := FrameViewport(content, 10, 2, offset)
		lines := strings.Split(page, "\n")
		seen.WriteString(lines[0])
	}
	if !strings.Contains(seen.String(), content) {
		t.Fatalf("wrapped content lost: %q", seen.String())
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
