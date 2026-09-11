package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func fixture() (*azdo.Client, []azdo.WorkItem) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	return c, []azdo.WorkItem{
		{ID: 4021, Title: "Retry webhook delivery on 5xx", Type: "User Story", State: "Active",
			Assigned: "Dev Example", AssignedKey: "dev@acme.test",
			Tags: "webhooks; reliability", Iteration: `Platform\Sprint 42`,
			Description: "Deliveries that fail with a 5xx should retry with exponential backoff rather than dropping on the floor."},
		{ID: 4020, Title: "Tidy up the settings page copy", Type: "Enhancement",
			State: "Needs Refinement", Assigned: "(unassigned)"},
		{ID: 3998, Title: "Cache tenant lookups", Type: "Feature", State: "Pending QA",
			Assigned: "Dev Example", AssignedKey: "dev@acme.test"},
	}
}

func sized(t *testing.T, m WorkItems, w, h int) WorkItems {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(WorkItems)
}

func press(t *testing.T, m WorkItems, key tea.KeyMsg) (WorkItems, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key)
	return updated.(WorkItems), cmd
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestViewRendersListAndDetailSideBySide(t *testing.T) {
	c, items := fixture()
	out := sized(t, NewWorkItems(c, items, false), 120, 20).View()
	t.Logf("\n%s", out)

	for _, want := range []string{"work items (all 3)", "acme/Platform", "4021", "Retry webhook delivery", "assigned", "^t mine/all"} {
		if !strings.Contains(out, want) {
			t.Errorf("view is missing %q", want)
		}
	}

	// The detail pane must sit beside the list, not beneath it: the first row
	// and the detail title have to share a line.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "▸ 4021") && strings.Contains(line, "#4021") {
			return
		}
	}
	t.Error("expected the first list row and the detail title to share a line")
}

func TestToggleSwitchesScope(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 20)
	if got := len(m.list.Items()); got != 3 {
		t.Fatalf("all scope: got %d items, want 3", got)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := len(m.list.Items()); got != 2 {
		t.Fatalf("mine scope: got %d items, want 2", got)
	}
	if !strings.Contains(m.View(), "work items (mine 2)") {
		t.Error("header should report the mine scope")
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := len(m.list.Items()); got != 3 {
		t.Fatalf("back to all: got %d items, want 3", got)
	}
}

func TestMineScopeStartsEmptyWhenIdentityUnknown(t *testing.T) {
	_, items := fixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"} // az account show failed
	m := sized(t, NewWorkItems(c, items, true), 120, 20)

	if got := len(m.list.Items()); got != 0 {
		t.Errorf("got %d items, want 0 — an empty identity must not match every item", got)
	}
}

func TestBranchActionHandsACommandToTheShell(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 20)

	m, cmd := press(t, m, runes("b"))
	if got, want := m.Command, "azdo-branch 4021"; got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Error("branch action should quit so the shell can run the command")
	}
}

func TestActionKeysAreInertWhileFiltering(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 20)

	m, _ = press(t, m, runes("/"))
	m, _ = press(t, m, runes("b"))

	if m.Command != "" {
		t.Error("typing 'b' into the filter must not trigger the branch action")
	}
}

func TestSlackLinkNeutralisesBrackets(t *testing.T) {
	got := SlackLink("[draft] retry webhooks", "https://example.test/1")
	want := "[(draft) retry webhooks](https://example.test/1)"
	if got != want {
		t.Errorf("SlackLink = %q, want %q", got, want)
	}
}

func TestTruncateMarksTheCut(t *testing.T) {
	if got := truncate("abcdefgh", 5); got != "abcd…" {
		t.Errorf("truncate = %q, want %q", got, "abcd…")
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("truncate should leave short strings alone, got %q", got)
	}
}

func TestWordwrapBreaksOnWords(t *testing.T) {
	got := wordwrap("one two three four", 9)
	want := "one two\nthree\nfour"
	if got != want {
		t.Errorf("wordwrap = %q, want %q", got, want)
	}
}
