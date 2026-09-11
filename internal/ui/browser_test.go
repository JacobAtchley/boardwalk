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
