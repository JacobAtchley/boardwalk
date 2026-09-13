package ui

import (
	"errors"
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
	// Scope moved off the short line and into the panel behind "?" — see
	// WorkItems.Keys — once the footer was found overflowing 80 columns with
	// every one of this view's own bindings on it at once.
	if !strings.Contains(helpPanel(m.Keys()), "^t scope") {
		t.Errorf("help panel = %q, missing the scope toggle", helpPanel(m.Keys()))
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
	// fetched; entering it must not ask for the project all over again.
	c, items := fixture()
	m := NewWorkItems(c, items, false, false)

	if got := m.Body(testWidth, testHeight); strings.Contains(got, "fetching work items") {
		t.Errorf("a view built with items showed the fetching placeholder:\n%s", got)
	}
	if cmd := m.Init(); cmd != nil {
		t.Error("a view built with items fetched anyway")
	}
}

func TestWorkItemsFailedFetchReplacesTheFetchingPlaceholder(t *testing.T) {
	c, _ := fixture()
	m := NewWorkItems(c, nil, false, false)

	updated, _ := m.Update(ErrMsg{Err: errTest})
	m = updated.(*WorkItems)

	got := m.Body(testWidth, testHeight)
	if strings.Contains(got, "fetching work items") {
		t.Errorf("the body still claims to be fetching after the fetch failed:\n%s", got)
	}
	if !strings.Contains(got, errTest.Error()) {
		t.Errorf("the body does not say what went wrong:\n%s", got)
	}
}

func TestAssignToMeSendsTheCommandAndSyncsTheRowOnSuccess(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	// Select the row that starts unassigned entirely, so a row moving into
	// the mine scope proves the fix rather than a row that was already
	// there under a different display name.
	m.browser.list.Select(1) // 4020, "(unassigned)"

	m, cmd := press(t, m, runes("m"))
	if cmd == nil {
		t.Fatal("m produced no command")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "assigning #4020") {
		t.Errorf("status = %q, isErr = %v; want the assign in flight reported", status, isErr)
	}

	// The command itself talks to the real client (proven separately by
	// TestSetAssigneeSendsAJSONPatch); what matters here is what the view
	// does with the message assignCmd resolves to.
	updated, _ := m.Update(assigneeSetMsg{ID: 4020, Assigned: "dev@acme.test", AssignedKey: "dev@acme.test"})
	m = updated.(*WorkItems)

	if status, isErr := m.Status(); isErr || !strings.Contains(status, "assigned to you") {
		t.Errorf("status = %q, isErr = %v; want the assignment reported", status, isErr)
	}
	// The scope filter reads AssignedKey, not Assigned, so proving the count
	// moved is what proves the filter — not just the display text — was kept
	// honest without a refetch.
	if !strings.Contains(m.Title(), "all 3") {
		t.Errorf("title = %q, want the all count unchanged", m.Title())
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.Title(), "mine 3") {
		t.Errorf("title = %q, want mine to have picked up the newly assigned row", m.Title())
	}
}

func TestAssignToMeRefusesWhenTheSignedInUserIsUnknown(t *testing.T) {
	_, items := fixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"} // az account show failed
	m := sized(t, NewWorkItems(c, items, false, false))

	m, cmd := press(t, m, runes("m"))
	if cmd != nil {
		t.Fatal("m sent a command despite Client.Me being empty — this would silently unassign the item")
	}
	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, "unknown") {
		t.Errorf("status = %q, isErr = %v; want the unknown-identity message", status, isErr)
	}
}

func TestAssigneeSetErrorGoesToTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(assigneeSetMsg{ID: 4020, Err: errTest})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v; want the failure reported", status, isErr)
	}
}

func TestWorkItemsRefetchesOnR(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	m, cmd := press(t, m, runes("r"))
	if cmd == nil {
		t.Fatal("r did not refetch the work items")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "refresh") {
		t.Errorf("status = %q, isErr = %v; want the refresh reported", status, isErr)
	}
	// Refresh is never on the short line for any list view now — see
	// listKeys's own doc — so it is the panel that has to still offer it.
	if !strings.Contains(helpPanel(m.Keys()), "r refresh") {
		t.Errorf("help panel = %q, want the refresh key offered", helpPanel(m.Keys()))
	}
}

func TestWorkItemsEnterOpensTheFullItem(t *testing.T) {
	c, items := fixture()
	m := NewWorkItems(c, items, false, false)
	m.Body(testWidth, testHeight)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "#4021") {
		t.Errorf("pushed view = %q, want the selected work item", push.View.Title())
	}
}

func TestWorkItemsSummaryOmitsTheLongText(t *testing.T) {
	// The long text is what enter is for; repeating it here costs the room the
	// five identifying fields need.
	c, items := fixture()
	m := NewWorkItems(c, items, false, false)
	view := m.Body(testWidth, testHeight)

	for _, want := range []string{"#4021", "assigned:", "tags:", "iteration:", "enter for the full item"} {
		if !strings.Contains(view, want) {
			t.Errorf("the summary is missing %q:\n%s", want, view)
		}
	}
	for _, gone := range []string{"description", "acceptance criteria", "discussion"} {
		if strings.Contains(view, gone) {
			t.Errorf("the summary still carries %q, which belongs to the full item:\n%s", gone, view)
		}
	}
}

func TestWorkItemsEmptyStateSaysWhatIsMissing(t *testing.T) {
	// A board with nothing on it and a fetch that broke both draw an empty
	// list otherwise, and they want opposite reactions from the user.
	c, _ := fixture()

	for _, tc := range []struct {
		name          string
		mineOnly      bool
		includeClosed bool
		want          string
	}{
		{"everyone's open items", false, false, "no open work items"},
		{"everyone's items with -all", false, true, "no work items in this project"},
		{"just mine", true, false, "nothing assigned to you"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sized(t, NewWorkItems(c, nil, tc.mineOnly, tc.includeClosed))
			updated, _ := m.Update(workItemsMsg{})
			m = updated.(*WorkItems)

			out := body(t, m)
			if !strings.Contains(out, tc.want) {
				t.Errorf("empty view is missing %q:\n%s", tc.want, out)
			}
			if !strings.Contains(out, "⌒v⌒") {
				t.Errorf("empty view did not draw the gulls:\n%s", out)
			}
		})
	}
}

func TestWorkItemsStillFetchingDoesNotDrawTheEmptyState(t *testing.T) {
	// Before the batch lands there is nothing to say is missing — the list is
	// empty because the fetch has not finished, which the spinner already says.
	c, _ := fixture()
	m := sized(t, NewWorkItems(c, nil, false, false))

	out := body(t, m)
	if strings.Contains(out, "⌒v⌒") {
		t.Errorf("the empty state drew while the fetch was still in flight:\n%s", out)
	}
	if !strings.Contains(out, "fetching") {
		t.Errorf("view = %q, want the fetching placeholder", out)
	}
}
