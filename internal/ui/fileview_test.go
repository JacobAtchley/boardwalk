package ui

import (
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
	m := NewFileView(c, pr, "/internal/ui/tail.go", text, hunks, threads)
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
	m := NewFileView(c, pr, "/x.go", "one\ntwo\n", nil, nil)

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
