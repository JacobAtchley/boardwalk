package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func itemFixture() (*azdo.Client, azdo.WorkItem) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	return c, azdo.WorkItem{
		ID: 4021, Title: "Retry webhook delivery on 5xx", Type: "User Story",
		State: "Active", Assigned: "Dev Example", AssignedKey: "dev@acme.test",
		Tags: "webhooks; reliability", Iteration: `Platform\Sprint 42`,
		Description:        "Deliveries that fail with a 5xx should retry.",
		AcceptanceCriteria: "- Retries three times\n- Backs off exponentially",
	}
}

func newItem(t *testing.T, comments []azdo.Comment) *Item {
	t.Helper()
	c, wi := itemFixture()
	m := NewItem(c, wi, comments)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestItemShowsEveryFieldTheListSummaryOmits(t *testing.T) {
	m := newItem(t, []azdo.Comment{
		{Author: "Other Dev", Created: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC), Text: "Why the retry cap?"},
	})
	r := drive(t, m, 100, 40)
	view := r.frame()

	for _, want := range []string{
		"4021", "Retry webhook delivery on 5xx",
		"User Story", "Active", "Dev Example", "webhooks", "Sprint 42",
		"Deliveries that fail", "Retries three times", "Backs off exponentially",
		"Other Dev", "Why the retry cap?",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the item view is missing %q:\n%s", want, view)
		}
	}
}

func TestItemRendersMarkdownRatherThanPrintingIt(t *testing.T) {
	// The acceptance criteria arrive as a markdown list. Glamour turns the
	// hyphens into bullets; seeing the raw "- " back means nothing rendered.
	m := newItem(t, nil)
	view := drive(t, m, 100, 40).frame()

	if strings.Contains(view, "- Retries three times") {
		t.Errorf("the markdown was printed verbatim rather than rendered:\n%s", view)
	}
	if !strings.Contains(view, "Retries three times") {
		t.Errorf("the acceptance criteria text is missing entirely:\n%s", view)
	}
}

func TestItemFetchesTheDiscussionWhenItWasNotGivenOne(t *testing.T) {
	// The list hands over whatever it had cached, which is usually nothing.
	m := newItem(t, nil)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the discussion")
	}
	if !m.work.busy() {
		t.Error("the spinner is not running while the discussion is in flight")
	}
}

func TestItemDoesNotRefetchADiscussionItWasGiven(t *testing.T) {
	m := newItem(t, []azdo.Comment{{Author: "Other Dev", Text: "Already here"}})

	if cmd := m.Init(); cmd != nil {
		t.Error("the view refetched a discussion it had already been handed")
	}
}

func TestItemRendersADiscussionThatArrivesLater(t *testing.T) {
	m := newItem(t, nil)
	m.Init()
	r := drive(t, m, 100, 40)

	r.send(commentsMsg{ID: 4021, Comments: []azdo.Comment{
		{Author: "Third Dev", Created: time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC), Text: "Shipped"},
	}})

	view := r.frame()
	if !strings.Contains(view, "Third Dev") || !strings.Contains(view, "Shipped") {
		t.Errorf("the fetched discussion did not render:\n%s", view)
	}
	if m.work.busy() {
		t.Error("still spinning after the discussion landed")
	}
}

func TestItemSaysSoWhenTheDiscussionFails(t *testing.T) {
	m := newItem(t, nil)
	m.Init()
	r := drive(t, m, 100, 40)

	r.send(commentsErrMsg{ID: 4021, Err: errTest})

	view := r.frame()
	if strings.Contains(view, "loading") {
		t.Errorf("the pane still claims to be loading after the fetch failed:\n%s", view)
	}
	if status, isErr := m.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v; want the failure reported", status, isErr)
	}
	if m.work.busy() {
		t.Error("still spinning after the fetch failed")
	}
}

func TestItemIgnoresADiscussionForAnotherWorkItem(t *testing.T) {
	m := newItem(t, nil)
	m.Init()
	r := drive(t, m, 100, 40)

	r.send(commentsMsg{ID: 3998, Comments: []azdo.Comment{{Author: "Nobody", Text: "stale comment"}}})

	if strings.Contains(r.frame(), "stale comment") {
		t.Error("a discussion belonging to another work item was rendered")
	}
}

func TestItemScrollsAndTheScrollSurvivesARepaint(t *testing.T) {
	// The whole point of the view is content too long for the pane, so the
	// pager has to work under the runtime's repaint-after-every-message loop.
	var comments []azdo.Comment
	for i := range 40 {
		comments = append(comments, azdo.Comment{Author: "Dev Example", Text: strings.Repeat("filler ", 8) + string(rune('a'+i%26))})
	}
	m := newItem(t, comments)
	r := drive(t, m, 100, 12)

	top := r.frame()
	r.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	bottom := r.frame()

	if top == bottom {
		t.Error("G did not move the pager")
	}
	if r.frame() != bottom {
		t.Error("the scroll position was undone by the next repaint")
	}
}

func TestItemEscapeGoesBack(t *testing.T) {
	m := newItem(t, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced no command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("escape produced %T, want a PopMsg", cmd())
	}
}

func TestItemTitleNamesTheWorkItem(t *testing.T) {
	m := newItem(t, nil)

	if got := m.Title(); !strings.Contains(got, "#4021") || !strings.Contains(got, "acme/Platform") {
		t.Errorf("Title = %q", got)
	}
}
