package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func fixture() (*azdo.Client, []azdo.WorkItem) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	return c, []azdo.WorkItem{
		{ID: 4021, Title: "Retry webhook delivery on 5xx", Type: "User Story", State: "Active",
			Assigned: "Dev Example", AssignedKey: "dev@acme.test",
			Tags: "webhooks; reliability", Iteration: `Platform\Sprint 42`,
			Description:        "Deliveries that fail with a 5xx should retry with exponential backoff rather than dropping on the floor.",
			AcceptanceCriteria: "Retries three times with exponential backoff before giving up."},
		{ID: 4020, Title: "Tidy up the settings page copy", Type: "Enhancement",
			State: "Needs Refinement", Assigned: "(unassigned)"},
		{ID: 3998, Title: "Cache tenant lookups", Type: "Feature", State: "Pending QA",
			Assigned: "Dev Example", AssignedKey: "dev@acme.test"},
	}
}

// testWidth and testHeight are the one terminal size the work item tests drive
// at. body() used to paint at a height of its own, unrelated to the one the
// view had been driven at, so a test could assert against a frame no sequence
// of messages could actually have produced.
const testWidth, testHeight = 120, 24

func sized(t *testing.T, m *WorkItems) *WorkItems {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return updated.(*WorkItems)
}

func press(t *testing.T, m *WorkItems, key tea.KeyMsg) (*WorkItems, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key)
	return updated.(*WorkItems), cmd
}

func body(t *testing.T, m *WorkItems) string {
	t.Helper()
	return m.Body(testWidth, testHeight)
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestViewRendersListAndDetailSideBySide(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	out := body(t, m)
	t.Logf("\n%s", out)

	for _, want := range []string{"4021", "Retry webhook delivery", "assigned"} {
		if !strings.Contains(out, want) {
			t.Errorf("view is missing %q", want)
		}
	}
	if !strings.Contains(m.Title(), "work items (all 3)") || !strings.Contains(m.Title(), "acme/Platform") {
		t.Errorf("title = %q, missing expected pieces", m.Title())
	}
	if !strings.Contains(m.Hints(), "^t mine/all") {
		t.Errorf("hints = %q, missing toggle hint", m.Hints())
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
	m := sized(t, NewWorkItems(c, items, false, false))
	if !strings.Contains(m.Title(), "all 3") {
		t.Fatalf("title = %q, want all 3", m.Title())
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.Title(), "mine 2") {
		t.Error("header should report the mine scope")
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.Title(), "all 3") {
		t.Error("back to all: want all 3")
	}
}

func TestMineScopeStartsEmptyWhenIdentityUnknown(t *testing.T) {
	_, items := fixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"} // az account show failed
	m := sized(t, NewWorkItems(c, items, true, false))

	if !strings.Contains(m.Title(), "mine 0") {
		t.Errorf("title = %q, want mine 0 — an empty identity must not match every item", m.Title())
	}
}

func TestActionKeysAreInertWhileFiltering(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	m, _ = press(t, m, runes("/"))
	m, _ = press(t, m, runes("o"))

	status, isErr := m.Status()
	if strings.Contains(status, "opened") || isErr {
		t.Error("typing 'o' into the filter must not trigger the open action")
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

func TestWorkItemsShowsAcceptanceCriteria(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	if !strings.Contains(body(t, m), "Retries three times") {
		t.Errorf("the detail pane is missing the acceptance criteria:\n%s", body(t, m))
	}
}

func TestWorkItemsRequestsTheDiscussionForTheSelectedItem(t *testing.T) {
	c, items := fixture()
	// Deliberately not routed through sized(): that helper drives Update,
	// which also kicks off the fetch as a side effect (so the cursor can
	// pick up a newly-scrolled-to row's discussion). Calling it first would
	// mark the fetch in flight before Init ever ran, defeating the point of
	// this test. In production Init runs first, before any other message.
	m := NewWorkItems(c, items, false, false)

	// The fetch is a command rather than a call, so the test can run it or not.
	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the selected item's discussion")
	}
}

func TestWorkItemsRendersAFetchedDiscussion(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(commentsMsg{ID: 4021, Comments: []azdo.Comment{
		{Author: "Other Dev", Created: time.Now().Add(-2 * time.Hour), Text: "Why the retry cap?"},
	}})
	m = updated.(*WorkItems)

	view := body(t, m)
	if !strings.Contains(view, "Why the retry cap?") || !strings.Contains(view, "Other Dev") {
		t.Errorf("the discussion did not render:\n%s", view)
	}
}

func TestWorkItemsIgnoresADiscussionForAnotherItem(t *testing.T) {
	// A slow fetch can land after the cursor has moved on.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(commentsMsg{ID: 3998, Comments: []azdo.Comment{
		{Author: "Other Dev", Text: "stale comment"},
	}})
	m = updated.(*WorkItems)

	if strings.Contains(body(t, m), "stale comment") {
		t.Error("a discussion for an unselected item was rendered")
	}
}

func TestWorkItemsToggleSwitchesScope(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	if !strings.Contains(m.Title(), "all 3") {
		t.Errorf("title = %q, want the full count", m.Title())
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.Title(), "mine 2") {
		t.Errorf("title = %q, want the mine count", m.Title())
	}
}

func TestWorkItemsErrorGoesToTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(ErrMsg{Err: errors.New("no network")})
	m = updated.(*WorkItems)

	text, isErr := m.Status()
	if !isErr || !strings.Contains(text, "no network") {
		t.Errorf("status = %q, isErr = %v; want the error reported", text, isErr)
	}
	// The list must survive the failure.
	if !strings.Contains(body(t, m), "Retry webhook delivery") {
		t.Error("the rows were lost when a fetch failed")
	}
}

func TestWorkItemsRecoversFromAFailedDiscussionFetch(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(commentsErrMsg{ID: 4021, Err: errors.New("could not load the discussion for #4021: timeout")})
	m = updated.(*WorkItems)

	text, isErr := m.Status()
	if !isErr || !strings.Contains(text, "timeout") {
		t.Errorf("status = %q, isErr = %v; want the fetch error reported", text, isErr)
	}
	// The rows must survive the failure, same as any other fetch error.
	if !strings.Contains(body(t, m), "Retry webhook delivery") {
		t.Error("the rows were lost when the discussion fetch failed")
	}
	// A failed fetch is not the same as one still in flight — the pane must
	// say so honestly rather than reading "loading…" forever.
	if strings.Contains(body(t, m), "loading…") {
		t.Error("the pane should not still say loading after the fetch failed")
	}
	// The in-flight guard must be cleared so a later selection retries
	// instead of being silently skipped forever.
	if cmd := m.fetchComments(); cmd == nil {
		t.Error("a failed discussion fetch should be retried on a later selection, not skipped forever")
	}
}

func TestWorkItemsFetchesOnEntryWhenItHasNoItems(t *testing.T) {
	// The spec's Loading section: views fetch on entry through a command, so
	// the menu paints immediately and a failure reaches the status line rather
	// than exiting the program before the TUI ever starts.
	c, _ := fixture()
	m := NewWorkItems(c, nil, false, false)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("a view with no items did not fetch on entry")
	}
	if got := m.Body(testWidth, testHeight); !strings.Contains(got, "fetching work items") {
		t.Errorf("expected the fetching placeholder before the items land, got:\n%s", got)
	}
	if strings.Contains(m.Title(), "all 0") {
		t.Errorf("title = %q — a view that has not fetched yet must not claim the project is empty", m.Title())
	}
}

func TestWorkItemsRendersItemsThatArriveFromItsOwnFetch(t *testing.T) {
	c, items := fixture()
	m := NewWorkItems(c, nil, false, false)

	updated, _ := m.Update(workItemsMsg{Items: items})
	m = updated.(*WorkItems)

	if !strings.Contains(body(t, m), "Retry webhook delivery") {
		t.Errorf("the fetched items did not reach the list:\n%s", body(t, m))
	}
	if !strings.Contains(m.Title(), "all 3") {
		t.Errorf("title = %q, want the fetched count", m.Title())
	}
}

func TestWorkItemsBuiltWithItemsDoesNotRefetchThem(t *testing.T) {
	// -dump's path and the tests hand the view a batch that is already
	// fetched; entering it must ask for the selected item's discussion, not
	// for the project all over again.
	c, items := fixture()
	m := NewWorkItems(c, items, false, false)

	if got := m.Body(testWidth, testHeight); strings.Contains(got, "fetching work items") {
		t.Errorf("a view built with items showed the fetching placeholder:\n%s", got)
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the selected item's discussion")
	}
}
