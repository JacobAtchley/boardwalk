package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeRow is a Row with no Azure DevOps behind it, so the scaffold can be
// tested on its own.
type fakeRow struct {
	id    int
	title string
}

func (r fakeRow) FilterValue() string { return fmt.Sprintf("%d %s", r.id, r.title) }
func (r fakeRow) Render(width int) string {
	return truncate(fmt.Sprintf("%d %s", r.id, r.title), width)
}
func (r fakeRow) CopyID() string { return fmt.Sprint(r.id) }
func (r fakeRow) Label() string  { return fmt.Sprintf("#%d %s", r.id, r.title) }
func (r fakeRow) URL() string    { return fmt.Sprintf("https://example.test/%d", r.id) }

func testBrowser(t *testing.T) Browser {
	t.Helper()
	b := NewBrowser()
	b.Detail = func(r Row, width int) string { return "detail for " + r.CopyID() }
	b.SetRows([]Row{
		fakeRow{1, "first thing"},
		fakeRow{2, "second thing"},
		fakeRow{3, "third thing"},
	})
	b.SetSize(100, 20)
	return b
}

func TestBrowserRendersRowsAndTheSelectedDetail(t *testing.T) {
	b := testBrowser(t)

	view := b.View()
	for _, want := range []string{"first thing", "second thing", "detail for 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestBrowserMovingTheCursorReRendersTheDetail(t *testing.T) {
	b := testBrowser(t)
	b.Update(tea.KeyMsg{Type: tea.KeyDown})

	if !strings.Contains(b.View(), "detail for 2") {
		t.Errorf("the detail pane did not follow the cursor:\n%s", b.View())
	}
}

func TestBrowserSelectedReportsTheRow(t *testing.T) {
	b := testBrowser(t)
	row, ok := b.Selected()
	if !ok {
		t.Fatal("Selected on a populated browser returned nothing")
	}
	if row.CopyID() != "1" {
		t.Errorf("selected = %s, want 1", row.CopyID())
	}
}

func TestBrowserSelectedOnAnEmptyList(t *testing.T) {
	b := NewBrowser()
	b.Detail = func(Row, int) string { return "" }
	b.SetRows(nil)
	b.SetSize(100, 20)

	if _, ok := b.Selected(); ok {
		t.Error("Selected on an empty browser reported a row")
	}
	// Rendering an empty browser must not panic.
	b.View()
}

func TestBrowserFilteringSwallowsActionKeys(t *testing.T) {
	b := testBrowser(t)
	b.Update(runes("/"))

	if !b.Filtering() {
		t.Fatal("pressing / did not start filtering")
	}
	// While the prompt is open, "o" is a letter, not the open action.
	b.Update(runes("o"))
	if !strings.Contains(b.FilterView(), "o") {
		t.Errorf("the filter input did not take the keystroke: %q", b.FilterView())
	}
}

func TestBrowserDetailFollowsAFilterThatMovesTheRowUnderTheCursor(t *testing.T) {
	// bubbles/list answers a filter keystroke with an async command that
	// resolves, on a later tick, to a list.FilterMatchesMsg replacing the
	// matched set — without touching the cursor. Rows are picked so that only
	// one of them can possibly match the query, which makes the outcome
	// deterministic regardless of the fuzzy matcher's internal scoring: after
	// "/" the cursor sits at index 0 on the unfiltered set (row 1), and it
	// must still report index 0 once the query has narrowed the set down to
	// the single row that actually matches (row 2) — proving the pane can't
	// rely on the cursor index alone to notice its row changed underneath it.
	b := NewBrowser()
	b.Detail = func(r Row, width int) string { return "detail for " + r.CopyID() }
	b.SetRows([]Row{
		fakeRow{1, "aaa"},
		fakeRow{2, "bbb"},
		fakeRow{3, "ccc"},
	})
	b.SetSize(100, 20)

	b.Update(runes("/"))
	if !b.Filtering() {
		t.Fatal("pressing / did not start filtering")
	}
	if row, ok := b.Selected(); !ok || row.CopyID() != "1" {
		t.Fatalf("filtering should open on the unfiltered set with the cursor at the top, got %+v ok=%v", row, ok)
	}

	// Typing "b" queues a batch holding the cursor's blink command and the
	// filter command. The blink command blocks on a real timer and is
	// irrelevant here, so only the filter command's result — the last one
	// appended, per list.Model.handleFiltering — is run and fed back in, the
	// way the bubbletea runtime would deliver it on its own tick.
	cmd := b.Update(runes("b"))
	if cmd == nil {
		t.Fatal("typing into the filter should have queued the async match")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a batch of commands from the filter keystroke, got %#v", batch)
	}
	filterCmd := batch[len(batch)-1]
	if filterCmd == nil {
		t.Fatal("the filter command in the batch was nil")
	}
	b.Update(filterCmd())

	row, ok := b.Selected()
	if !ok {
		t.Fatal("Selected returned nothing once the filter narrowed to one row")
	}
	if row.CopyID() != "2" {
		t.Fatalf("selected = %s, want 2 — the only row matching %q", row.CopyID(), "b")
	}
	if !strings.Contains(b.View(), "detail for 2") {
		t.Errorf("the detail pane did not follow the filtered selection:\n%s", b.View())
	}
}

func TestBrowserDetailPaneScrollsIndependentlyOfTheList(t *testing.T) {
	// A description, acceptance criteria and discussion together can easily
	// overflow the pane; ctrl+u/ctrl+d must reach the viewport rather than
	// leaving that overflow permanently unreachable.
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")

	b := NewBrowser()
	b.Detail = func(Row, int) string { return content }
	b.SetRows([]Row{fakeRow{1, "first"}, fakeRow{2, "second"}})
	b.SetSize(100, 10)

	view := b.View()
	if !strings.Contains(view, "line 0") {
		t.Fatalf("expected the top of the content to be visible before scrolling:\n%s", view)
	}
	if strings.Contains(view, "line 30") {
		t.Fatalf("line 30 should be below the fold before scrolling:\n%s", view)
	}

	before, _ := b.Selected()
	b.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	after, ok := b.Selected()

	if !ok || after.CopyID() != before.CopyID() {
		t.Error("ctrl+d scrolled the detail pane but also moved the list selection")
	}
	if strings.Contains(b.View(), "line 0") {
		t.Error("ctrl+d should have scrolled the detail pane down, but the top line is still visible")
	}
}

func TestBrowserFilterOwnsCtrlUAndCtrlD(t *testing.T) {
	// bubbles/textinput's own keymap binds ctrl+u to DeleteBeforeCursor and
	// ctrl+d to DeleteCharacterForward, and list.handleFiltering forwards
	// every key straight to that input while filtering. The detail-scroll
	// interception must not steal those keys out from under the user's
	// line editing.
	b := testBrowser(t)

	b.Update(runes("/"))
	if !b.Filtering() {
		t.Fatal("pressing / did not start filtering")
	}
	b.Update(runes("abc"))
	if !strings.Contains(b.FilterView(), "abc") {
		t.Fatalf("filter input did not take the keystrokes: %q", b.FilterView())
	}

	viewBefore := b.View()
	b.Update(tea.KeyMsg{Type: tea.KeyCtrlU})

	if strings.Contains(b.FilterView(), "abc") {
		t.Errorf("ctrl+u should have cleared the filter input, got %q", b.FilterView())
	}
	if b.View() != viewBefore {
		t.Error("ctrl+u should edit the filter input while filtering, not scroll the detail pane")
	}
}

func TestSharedActionHandlesTheThreeCommonKeys(t *testing.T) {
	row := fakeRow{42, "a thing"}

	for _, tc := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{runes("y"), "copied id 42"},
		{runes("s"), "copied Slack link for #42"},
		{runes("o"), "opened #42"},
	} {
		got, handled := SharedAction(row, tc.key)
		if !handled {
			t.Fatalf("%v was not handled", tc.key)
		}
		if got != tc.want {
			t.Errorf("status = %q, want %q", got, tc.want)
		}
	}

	if _, handled := SharedAction(row, runes("z")); handled {
		t.Error("an unrelated key was claimed as a shared action")
	}
}

func TestSlackLinkEscapesBracketsInTheTitle(t *testing.T) {
	got := SlackLink("[#4021] a title", "https://example.test/4021")
	want := "(#4021) a title"
	if !strings.HasPrefix(got, "["+want+"]") {
		t.Errorf("SlackLink = %q, want the brackets turned into parentheses", got)
	}
}
