package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/aymanbagabas/go-udiff"
	tea "github.com/charmbracelet/bubbletea"
)

// sampleFile is a short file with one hunk in the middle: a removal and two
// additions, with untouched lines either side. Enough to tell the diff
// decoration apart from the file it decorates.
func sampleFile() (string, []*udiff.Hunk) {
	text := "package tail\n\nfunc tail() {\n\tdefer close(ch)\n\tgo poll(ctx)\n}\n"
	hunks := []*udiff.Hunk{hunk(3, 3,
		line(udiff.Equal, "func tail() {\n"),
		line(udiff.Delete, "\tgo poll()\n"),
		line(udiff.Insert, "\tdefer close(ch)\n"),
		line(udiff.Insert, "\tgo poll(ctx)\n"),
		line(udiff.Equal, "}\n"),
	)}
	return text, hunks
}

func newFileView(t *testing.T, threads []azdo.Thread) *FileView {
	t.Helper()
	c, pr := prDetailFixture()
	text, hunks := sampleFile()
	m := NewFileView(c, pr, "/internal/ui/tail.go", text, hunks, threads, filterAll)
	m.Body(100, 30)
	return m
}

func pressFile(t *testing.T, m *FileView, msg tea.KeyMsg) (*FileView, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(*FileView)
	if !ok {
		t.Fatalf("Update returned %T, want *FileView", updated)
	}
	return next, cmd
}

func TestFileViewShowsTheWholeFileNotJustTheHunk(t *testing.T) {
	m := newFileView(t, nil)
	view := m.Body(100, 30)

	// The hunk covers lines 3 to 6. The point of this view is the rest.
	for _, want := range []string{"package tail", "func tail() {", "go poll(ctx)"} {
		if !strings.Contains(view, want) {
			t.Errorf("the file is missing %q:\n%s", want, view)
		}
	}
}

func TestFileViewMarksChangedLinesInTheGutter(t *testing.T) {
	m := newFileView(t, nil)
	view := m.Body(100, 30)

	rowFor := func(text string) string {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		t.Fatalf("no row holding %q:\n%s", text, view)
		return ""
	}

	if got := rowFor("defer close(ch)"); !strings.Contains(got, "+") {
		t.Errorf("an added line is not marked: %q", got)
	}
	if got := rowFor("go poll()"); !strings.Contains(got, "-") {
		t.Errorf("a removed line is not marked: %q", got)
	}
	if got := rowFor("package tail"); strings.Contains(got, "+") || strings.Contains(got, "-") {
		t.Errorf("an untouched line is marked as changed: %q", got)
	}
}

func TestFileViewNumbersItsLines(t *testing.T) {
	m := newFileView(t, nil)
	view := m.Body(100, 30)

	for _, want := range []string{"1", "6"} {
		if !strings.Contains(view, want) {
			t.Errorf("line number %s is missing:\n%s", want, view)
		}
	}
}

// TestFileViewTogglesTheDiffOff — d leaves the file and its highlighting and
// takes away the decoration, which is what reading it as code rather than as
// a change means.
func TestFileViewTogglesTheDiffOff(t *testing.T) {
	m := newFileView(t, nil)

	m, _ = pressFile(t, m, runes("d"))
	view := m.Body(100, 30)

	if strings.Contains(view, "go poll()") {
		t.Errorf("a removed line is still shown with the diff off:\n%s", view)
	}
	if !strings.Contains(view, "go poll(ctx)") {
		t.Errorf("the file itself went with the decoration:\n%s", view)
	}

	m, _ = pressFile(t, m, runes("d"))
	if view := m.Body(100, 30); !strings.Contains(view, "go poll()") {
		t.Errorf("a second d did not bring the diff back:\n%s", view)
	}
}

func TestFileViewCursorMovesAndStops(t *testing.T) {
	m := newFileView(t, nil)
	first := m.cursor

	m, _ = pressFile(t, m, runes("j"))
	if m.cursor != first+1 {
		t.Errorf("cursor = %d after j, want %d", m.cursor, first+1)
	}

	m, _ = pressFile(t, m, runes("k"))
	m, _ = pressFile(t, m, runes("k"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d at the top, want it to stop at 0 rather than wrap", m.cursor)
	}

	for range 50 {
		m, _ = pressFile(t, m, runes("j"))
	}
	if m.cursor != len(m.lines)-1 {
		t.Errorf("cursor = %d at the bottom, want the last line %d", m.cursor, len(m.lines)-1)
	}
}

// TestFileViewCursorReportsTheLineAComment WouldAnchorTo — the number the
// next feature posts against, so it has to be the new file's, and a removed
// line has none.
func TestFileViewCursorReportsTheLineACommentWouldAnchorTo(t *testing.T) {
	m := newFileView(t, nil)

	// Walk to the removed line, which is the fourth row: two untouched, the
	// hunk's first context line, then the removal.
	for range 3 {
		m, _ = pressFile(t, m, runes("j"))
	}
	row := m.lines[m.cursor]
	if row.Kind != udiff.Delete {
		t.Fatalf("expected the removed row here, got %+v", row)
	}
	if _, ok := m.anchorLine(); ok {
		t.Error("a removed line offered a new-file line to anchor a comment to")
	}

	m, _ = pressFile(t, m, runes("j"))
	n, ok := m.anchorLine()
	if !ok {
		t.Fatal("an added line offered nothing to anchor to")
	}
	if n != 4 {
		t.Errorf("anchor line = %d, want 4 — the added line's number in the new file", n)
	}
}

func TestFileViewShowsThreadsAgainstTheirLine(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "active", File: "/internal/ui/tail.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "this still races"}}},
		{ID: 2, Status: "active", File: "/other.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "another file"}}},
	}
	m := newFileView(t, threads)
	view := m.Body(100, 30)

	if !strings.Contains(view, "this still races") {
		t.Errorf("the thread on this file is missing:\n%s", view)
	}
	if strings.Contains(view, "another file") {
		t.Errorf("another file's thread is in this one's view:\n%s", view)
	}
}

func TestFileViewEscapePops(t *testing.T) {
	m := newFileView(t, nil)

	_, cmd := pressFile(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc did nothing")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("esc sent %T, want PopMsg", cmd())
	}
}

func TestFileViewTitleNamesTheFileAndPullRequest(t *testing.T) {
	m := newFileView(t, nil)
	title := m.Title()

	if !strings.Contains(title, "tail.go") {
		t.Errorf("title = %q, want the file in it", title)
	}
	if !strings.Contains(title, "512") {
		t.Errorf("title = %q, want the pull request in it", title)
	}
}

// TestFileViewOfAFileWithNoHunks — a file listed as changed whose text is the
// same at both commits. It is still worth opening; it just has nothing marked.
func TestFileViewOfAFileWithNoHunks(t *testing.T) {
	c, pr := prDetailFixture()
	m := NewFileView(c, pr, "/x.go", "one\ntwo\n", nil, nil, filterAll)

	view := m.Body(100, 30)
	if !strings.Contains(view, "one") || !strings.Contains(view, "two") {
		t.Errorf("the file is not shown:\n%s", view)
	}
}

// TestFileViewEnterIsWiredFromTheDiffPane — the view is unreachable
// otherwise, and a feature nobody can open is not a feature.
func TestFileViewEnterIsWiredFromTheDiffPane(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go",
		Diff: fileDiff{text: "one\ntwo\n", hunks: nil}})

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a changed file did nothing")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter sent %T, want PushMsg", cmd())
	}
	if _, ok := push.View.(*FileView); !ok {
		t.Errorf("enter pushed %T, want the file view", push.View)
	}
}

// TestFileViewIsNotOpenedBeforeItsContentArrives — enter on a row whose fetch
// has not landed would push an empty pane over the list with nothing to say
// why.
func TestFileViewIsNotOpenedBeforeItsContentArrives(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return // nothing pushed, which is the point
	}
	if _, ok := cmd().(PushMsg); ok {
		t.Error("enter opened a file whose content had not arrived")
	}
}

// commentOn walks the cursor to the file's first added line, which is the
// one a review comment in these tests is written against.
func commentOn(t *testing.T, m *FileView) *FileView {
	t.Helper()
	for range len(m.lines) {
		if n, ok := m.anchorLine(); ok && m.lines[m.cursor].Kind == udiff.Insert {
			_ = n
			return m
		}
		m, _ = pressFile(t, m, runes("j"))
	}
	t.Fatal("no added line to comment on")
	return nil
}

func TestFileCommentPromptOpensOnTheCursorLine(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))
	line, _ := m.anchorLine()

	m, cmd := pressFile(t, m, runes("c"))
	if cmd == nil {
		t.Error("opening the prompt should start the cursor blinking")
	}
	if m.commentPrompt == nil {
		t.Fatal("c did not open the comment prompt")
	}
	if !m.Prompting() {
		t.Error("Prompting() is false with the prompt open, so Root would take esc for navigation")
	}
	if status, _ := m.Status(); !strings.Contains(status, fmt.Sprint(line)) {
		t.Errorf("status = %q, want the prompt naming line %d", status, line)
	}
}

func TestFileCommentPromptEnterPosts(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))

	m, _ = pressFile(t, m, runes("c"))
	for _, r := range "this still races" {
		m, _ = pressFile(t, m, runes(string(r)))
	}
	m, cmd := pressFile(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("enter did not post the comment")
	}
	if m.commentPrompt != nil {
		t.Error("enter left the prompt open")
	}
	if status, _ := m.Status(); !strings.Contains(status, "posting") {
		t.Errorf("status = %q, want it saying the comment is on its way", status)
	}
}

// TestCommentPromptRefusesAnEmptyComment — enter on an empty prompt is a
// mistake, not a request to post nothing.
func TestFileCommentPromptRefusesAnEmptyComment(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))

	m, _ = pressFile(t, m, runes("c"))
	m, cmd := pressFile(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Error("an empty comment was sent")
	}
	if m.commentPrompt != nil {
		t.Error("the prompt stayed open")
	}
}

func TestFileCommentPromptEscapeCancels(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))

	m, _ = pressFile(t, m, runes("c"))
	m, cmd := pressFile(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Error("cancelling the prompt posted anyway")
	}
	if m.commentPrompt != nil {
		t.Error("escape did not close the prompt")
	}
}

// TestCommentPromptSwallowsActionKeys — with the prompt open "d" is a letter,
// not the diff toggle.
func TestFileCommentPromptSwallowsActionKeys(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))
	before := m.showDiff

	m, _ = pressFile(t, m, runes("c"))
	m, _ = pressFile(t, m, runes("d"))

	if m.showDiff != before {
		t.Error("a keystroke meant for the prompt toggled the diff")
	}
	if !strings.HasSuffix(m.commentPrompt.Value(), "d") {
		t.Errorf("prompt = %q, want the keystroke in it", m.commentPrompt.Value())
	}
}

// TestCommentRefusedOnARemovedLine — a line the pull request deleted is not
// in the new file, so the server has nothing to anchor a comment to. Refused
// before the request, the way armDraft refuses a merged pull request.
func TestFileCommentRefusedOnARemovedLine(t *testing.T) {
	m := newFileView(t, nil)
	for range len(m.lines) {
		if m.lines[m.cursor].Kind == udiff.Delete {
			break
		}
		m, _ = pressFile(t, m, runes("j"))
	}
	if m.lines[m.cursor].Kind != udiff.Delete {
		t.Fatal("no removed line in the fixture")
	}

	m, cmd := pressFile(t, m, runes("c"))
	if cmd != nil || m.commentPrompt != nil {
		t.Fatal("the prompt opened on a line that cannot carry a comment")
	}
	status, failed := m.Status()
	if !failed {
		t.Error("the refusal was not reported as one")
	}
	if !strings.Contains(status, "removed") {
		t.Errorf("status = %q, want it saying why the line cannot take a comment", status)
	}
}

// TestPostedCommentAppearsWithoutARefetch — the thread the server just made
// is already known, and waiting for a refetch to see your own comment reads
// as the comment having failed.
func TestPostedCommentAppearsWithoutARefetch(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))
	line, _ := m.anchorLine()

	updated, _ := m.Update(threadCreatedMsg{
		PR:   512,
		Path: "/internal/ui/tail.go",
		Thread: azdo.Thread{ID: 91, Status: "active", File: "/internal/ui/tail.go",
			Line: line, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev Example", Text: "freshly written"}}},
	})
	m = updated.(*FileView)

	if view := m.Body(100, 30); !strings.Contains(view, "freshly written") {
		t.Errorf("the comment just posted is not on screen:\n%s", view)
	}
	if status, failed := m.Status(); failed {
		t.Errorf("status = %q reported as a failure after a successful post", status)
	}
}

func TestPostFailureGoesToTheStatusLine(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))

	updated, _ := m.Update(threadCreatedMsg{PR: 512, Path: "/internal/ui/tail.go", Err: errTest})
	m = updated.(*FileView)

	if status, failed := m.Status(); !failed || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q (failed=%v), want the failure reported", status, failed)
	}
}

// TestThreadForAnotherFileIsIgnored — Root broadcasts to every view in the
// stack, and two file views can be open at once by way of a linked pull
// request.
func TestThreadForAnotherFileIsIgnored(t *testing.T) {
	m := commentOn(t, newFileView(t, nil))

	updated, _ := m.Update(threadCreatedMsg{
		PR:   512,
		Path: "/somewhere/else.go",
		Thread: azdo.Thread{ID: 92, File: "/somewhere/else.go", Line: 1, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Text: "not this file"}}},
	})
	m = updated.(*FileView)

	if view := m.Body(100, 30); strings.Contains(view, "not this file") {
		t.Errorf("another file's new thread landed in this view:\n%s", view)
	}
}

func TestFileViewFilterKeyHidesTheSettledComments(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "active", File: "/internal/ui/tail.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "OPEN QUESTION"}}},
		{ID: 2, Status: "fixed", Resolved: true, File: "/internal/ui/tail.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "CLOSED QUESTION"}}},
	}
	m := newFileView(t, threads)

	m, _ = pressFile(t, m, runes("f"))
	view := m.Body(100, 30)
	if strings.Contains(view, "CLOSED QUESTION") || !strings.Contains(view, "OPEN QUESTION") {
		t.Errorf("f did not leave the unresolved comment alone beside the code:\n%s", view)
	}

	m, _ = pressFile(t, m, runes("f"))
	view = m.Body(100, 30)
	if !strings.Contains(view, "CLOSED QUESTION") || strings.Contains(view, "OPEN QUESTION") {
		t.Errorf("the second press of f did not leave the settled comment alone:\n%s", view)
	}

	m, _ = pressFile(t, m, runes("f"))
	view = m.Body(100, 30)
	if !strings.Contains(view, "CLOSED QUESTION") || !strings.Contains(view, "OPEN QUESTION") {
		t.Errorf("the third press of f did not bring both comments back:\n%s", view)
	}
}

// TestFileViewFilterKeepsTheCursorOnItsLine — the cursor indexes the composed
// file, and the comments under a line are rows between those lines. A filter
// that changed which line the cursor names would move where a review comment
// is about to be written.
func TestFileViewFilterKeepsTheCursorOnItsLine(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "fixed", Resolved: true, File: "/internal/ui/tail.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "settled"}}},
	}
	m := newFileView(t, threads)
	m, _ = pressFile(t, m, runes("j"))
	before := m.cursor

	m, _ = pressFile(t, m, runes("f"))

	if m.cursor != before {
		t.Errorf("cursor = %d after filtering, want it left on %d", m.cursor, before)
	}
}

func TestFileViewKeepsTheFilterTheDiffPaneWasShowing(t *testing.T) {
	r := newPRDiffWithThreads(t, reviewThreads(),
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go",
		Diff: fileDiff{text: "func tail() {\n}\n", hunks: []*udiff.Hunk{logsHunk()}}})

	r.send(runes("f"))
	cmd := r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not open the file")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", cmd())
	}
	file, ok := push.View.(*FileView)
	if !ok {
		t.Fatalf("enter pushed %T, want the file view", push.View)
	}
	if file.filter != filterUnresolved {
		t.Errorf("the file opened showing %v, want the filter the diff pane had", file.filter)
	}
}
