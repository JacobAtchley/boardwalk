package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/aymanbagabas/go-udiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// diffWidth and diffHeight are the one terminal size these tests drive at. The
// pane is tall so a whole file's diff fits without the viewport clipping it,
// which would make an assertion about a missing line ambiguous.
const diffWidth, diffHeight = 140, 60

func newPRDiff(t *testing.T, changes ...azdo.Change) *runtimeView[*PullRequestDiff] {
	t.Helper()
	c, pr := prDetailFixture()
	pr.SourceCommit, pr.TargetCommit = "source-sha", "target-sha"

	r := drive(t, NewPullRequestDiff(c, pr, nil), diffWidth, diffHeight)
	r.send(prChangesMsg{PR: pr.ID, Changes: changes})
	return r
}

// hunk builds a hunk by hand rather than by diffing, so a test can state the
// exact line kinds it is about — including the ones a diff rarely produces.
func hunk(fromLine, toLine int, lines ...udiff.Line) *udiff.Hunk {
	return &udiff.Hunk{FromLine: fromLine, ToLine: toLine, Lines: lines}
}

func line(kind udiff.OpKind, content string) udiff.Line {
	return udiff.Line{Kind: kind, Content: content}
}

func TestPullRequestDiffListsTheChangedFiles(t *testing.T) {
	r := newPRDiff(t,
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"},
		azdo.Change{Path: "/internal/ui/prdiff.go", ChangeType: "add"},
		azdo.Change{Path: "/internal/ui/old.go", ChangeType: "delete"},
	)

	view := r.frame()
	for _, want := range []string{"/internal/ui/logs.go", "/internal/ui/prdiff.go", "/internal/ui/old.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("the file list is missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(r.view.Title(), "3 files") {
		t.Errorf("title = %q, want the file count", r.view.Title())
	}
}

func TestPullRequestDiffFetchesOnlyTheSelectedFile(t *testing.T) {
	// The whole point of per-file fetching: opening a pull request touching
	// three files must not ask for six file bodies.
	r := newPRDiff(t,
		azdo.Change{Path: "/a.go", ChangeType: "edit"},
		azdo.Change{Path: "/b.go", ChangeType: "edit"},
		azdo.Change{Path: "/c.go", ChangeType: "edit"},
	)

	if len(r.view.loading) != 1 {
		t.Fatalf("%d files are being fetched, want only the selected one: %v", len(r.view.loading), r.view.loading)
	}
	if !r.view.loading["/a.go"] {
		t.Errorf("the file being fetched is %v, want the one under the cursor", r.view.loading)
	}

	// Moving the cursor reaches the next one, and only the next one.
	r.send(tea.KeyMsg{Type: tea.KeyDown})
	if !r.view.loading["/b.go"] {
		t.Errorf("moving the cursor did not fetch the newly selected file: %v", r.view.loading)
	}
	if r.view.loading["/c.go"] {
		t.Errorf("a file the cursor has not reached was fetched: %v", r.view.loading)
	}
}

func TestPullRequestDiffDoesNotRefetchAFileItAlreadyHas(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"}, azdo.Change{Path: "/b.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{}})

	// Down to /b.go and back up to the one already read.
	r.send(tea.KeyMsg{Type: tea.KeyDown})
	r.send(tea.KeyMsg{Type: tea.KeyUp})

	if r.view.loading["/a.go"] {
		t.Error("returning to a file that had already been read fetched it a second time")
	}
}

// longHunk is a diff tall enough to overflow the detail pane, so the tests
// about scrolling have something to scroll.
func longHunk(tag string) []*udiff.Hunk {
	lines := make([]udiff.Line, 0, 200)
	for i := 1; i <= 200; i++ {
		lines = append(lines, line(udiff.Equal, fmt.Sprintf("%s line %d\n", tag, i)))
	}
	return []*udiff.Hunk{hunk(1, 1, lines...)}
}

func TestPullRequestDiffALateDiffDoesNotDisturbThePaneBeingRead(t *testing.T) {
	// The failure this guards: the cursor moves to /b.go, whose diff lands
	// first; the reader scrolls into it; then /a.go's slower fetch lands and
	// rebuilds the list, which re-renders the detail pane and returns it to the
	// top — pulling the page out from under someone mid-read.
	r := newPRDiff(t,
		azdo.Change{Path: "/a.go", ChangeType: "edit"},
		azdo.Change{Path: "/b.go", ChangeType: "edit"},
	)

	r.send(tea.KeyMsg{Type: tea.KeyDown})
	r.send(fileDiffMsg{PR: 512, Path: "/b.go", Diff: fileDiff{hunks: longHunk("b")}})
	if !strings.Contains(r.frame(), "b line 1") {
		t.Fatalf("the selected file's diff is not at the top of the pane:\n%s", r.frame())
	}

	r.send(tea.KeyMsg{Type: tea.KeyCtrlD})
	if strings.Contains(r.frame(), "b line 1") {
		t.Fatalf("ctrl+d did not scroll the pane, so this test proves nothing:\n%s", r.frame())
	}

	// The slower fetch for the row the cursor has left now lands.
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: longHunk("a")}})

	if strings.Contains(r.frame(), "b line 1") {
		t.Errorf("a diff landing for a file the cursor had left returned the pane to the top:\n%s", r.frame())
	}
	if strings.Contains(r.frame(), "a line 1") {
		t.Errorf("a diff landing for a file the cursor had left replaced the pane's contents:\n%s", r.frame())
	}
	// The row still has to notice it has been read, or the pending marker lies.
	if _, done := r.view.diffs["/a.go"]; !done {
		t.Error("the late diff was not recorded")
	}
	if strings.Count(r.frame(), "·") != 0 {
		t.Errorf("a file whose diff has landed is still marked pending:\n%s", r.frame())
	}
}

func TestPullRequestDiffTheSelectedFilesDiffStartsAtTheTopWhenItLands(t *testing.T) {
	// The other side of the guard above: a diff arriving for the file actually
	// on screen must render, and render from line one rather than at whatever
	// offset the previous file was left at.
	r := newPRDiff(t,
		azdo.Change{Path: "/a.go", ChangeType: "edit"},
		azdo.Change{Path: "/b.go", ChangeType: "edit"},
	)
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: longHunk("a")}})
	r.send(tea.KeyMsg{Type: tea.KeyCtrlD})

	r.send(tea.KeyMsg{Type: tea.KeyDown})
	r.send(fileDiffMsg{PR: 512, Path: "/b.go", Diff: fileDiff{hunks: longHunk("b")}})

	if !strings.Contains(r.frame(), "b line 1") {
		t.Errorf("the newly selected file's diff did not render from the top:\n%s", r.frame())
	}
}

func TestPullRequestDiffRefreshEmptiesTheRowsSharedView(t *testing.T) {
	// The rows hold the diffs map by reference, so refresh has to empty it
	// rather than hand the view a new one — otherwise every row goes on
	// claiming to have been read until the fresh file list lands.
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: longHunk("a")}})
	if strings.Contains(r.frame(), "·") {
		t.Fatalf("a file that has been read is still marked pending:\n%s", r.frame())
	}

	r.send(runes("r"))

	if !strings.Contains(r.frame(), "·") {
		t.Errorf("after a refresh the rows still report the discarded diffs as read:\n%s", r.frame())
	}
}

func TestPullRequestDiffIgnoresAnotherPullRequestsMessages(t *testing.T) {
	// Root broadcasts data to every view in the stack, and a pull request whose
	// linked work item links back to another can have two of these open.
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 999, Path: "/a.go", Diff: fileDiff{note: "binary file — not shown"}})

	if _, ok := r.view.diffs["/a.go"]; ok {
		t.Error("a diff belonging to a different pull request was stored")
	}
}

func TestPullRequestDiffRendersTheDiffForTheSelectedFile(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: []*udiff.Hunk{
		hunk(10, 10,
			line(udiff.Equal, "func main() {\n"),
			line(udiff.Delete, "\tprintln(\"old\")\n"),
			line(udiff.Insert, "\tprintln(\"new\")\n"),
			line(udiff.Equal, "}\n"),
		),
	}}})

	view := r.frame()
	for _, want := range []string{"@@ -10,3 +10,3 @@", "func main() {", `-	println("old")`, `+	println("new")`} {
		// The tab in the source lines is expanded before rendering, so the
		// assertion is on the expanded form.
		want = expandTabs(want)
		if !strings.Contains(view, want) {
			t.Errorf("the diff pane is missing %q:\n%s", want, view)
		}
	}
}

func TestDiffLineStyle(t *testing.T) {
	// The colours are the only thing separating an added line from a removed
	// one at a glance, so a renderer that drew both the same would be useless
	// while still passing every text assertion above. This asserts on the
	// styles rather than on rendered escapes because lipgloss drops colour
	// entirely when the test binary's output is not a terminal.
	cases := []struct {
		name       string
		kind       udiff.OpKind
		wantMarker string
		wantColour lipgloss.TerminalColor
	}{
		{"an insertion is green", udiff.Insert, "+", colOK},
		{"a deletion is red", udiff.Delete, "-", colErr},
		{"context is left alone", udiff.Equal, " ", lipgloss.NoColor{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			marker, style := diffLineStyle(c.kind)
			if marker != c.wantMarker {
				t.Errorf("marker = %q, want %q", marker, c.wantMarker)
			}
			if got := style.GetForeground(); got != c.wantColour {
				t.Errorf("colour = %v, want %v", got, c.wantColour)
			}
		})
	}

	if addedLine.GetForeground() == removedLine.GetForeground() {
		t.Error("additions and deletions are the same colour")
	}
}

func TestRenderHunksMarksALineWithNoTrailingNewline(t *testing.T) {
	// go-udiff leaves the newline on a line's content, so the last line of a
	// file that does not end with one arrives without it. Saying so is what
	// stops "the file grew a newline" from looking like no change at all.
	var b strings.Builder
	renderHunks(&b, []*udiff.Hunk{hunk(1, 1,
		line(udiff.Delete, "last"),
		line(udiff.Insert, "last\n"),
	)}, nil, 40)

	if !strings.Contains(b.String(), `\ no newline at end of file`) {
		t.Errorf("a line with no trailing newline was not marked:\n%s", b.String())
	}
	if strings.Count(b.String(), `\ no newline at end of file`) != 1 {
		t.Errorf("the marker was written for a line that does end with a newline:\n%s", b.String())
	}
}

func TestRenderHunksExpandsTabsSoALineCannotOverflowThePane(t *testing.T) {
	// A real tab is expanded by the terminal, not by lipgloss, so truncate
	// measures a tab-indented line as shorter than it draws and the overflow
	// lands in the list beside it.
	var b strings.Builder
	renderHunks(&b, []*udiff.Hunk{hunk(1, 1, line(udiff.Equal, "\t\treturn nil\n"))}, nil, 40)

	if strings.Contains(b.String(), "\t") {
		t.Errorf("a tab survived into the rendered diff:\n%q", b.String())
	}
	if !strings.Contains(b.String(), strings.Repeat(" ", 2*tabWidth)+"return nil") {
		t.Errorf("the indentation was not preserved as spaces:\n%q", b.String())
	}
}

func TestHunkHeaderCountsEachSide(t *testing.T) {
	cases := []struct {
		name string
		h    *udiff.Hunk
		want string
	}{
		{
			"context counts on both sides",
			hunk(4, 4, line(udiff.Equal, "a\n"), line(udiff.Equal, "b\n")),
			"@@ -4,2 +4,2 @@",
		},
		{
			"a deletion belongs to the old file only",
			hunk(4, 4, line(udiff.Equal, "a\n"), line(udiff.Delete, "b\n")),
			"@@ -4,2 +4,1 @@",
		},
		{
			"an insertion belongs to the new file only",
			hunk(4, 4, line(udiff.Equal, "a\n"), line(udiff.Insert, "b\n")),
			"@@ -4,1 +4,2 @@",
		},
		{
			"a pure addition has nothing on the old side",
			hunk(1, 1, line(udiff.Insert, "a\n"), line(udiff.Insert, "b\n")),
			"@@ -1,0 +1,2 @@",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hunkHeader(c.h); got != c.want {
				t.Errorf("hunkHeader = %q, want %q", got, c.want)
			}
		})
	}
}

func TestUnrenderable(t *testing.T) {
	big := strings.Repeat("x", maxDiffBytes+1)
	cases := []struct {
		name          string
		before, after string
		want          string
	}{
		{"two ordinary files", "package ui\n", "package ui\n\n", ""},
		{"both sides empty", "", "", ""},
		{"a NUL byte on the new side", "text\n", "\x00\x01ELF", "binary file — not shown"},
		{"a NUL byte on the old side", "\x00\x01ELF", "text\n", "binary file — not shown"},
		{
			"a NUL past the sniff window is not looked for",
			"", strings.Repeat("a", binarySniff+1) + "\x00",
			"",
		},
		{"the new side is over the cap", "small\n", big, "file is larger than 256 KiB — not shown"},
		{"the old side is over the cap", big, "small\n", "file is larger than 256 KiB — not shown"},
		{
			"binary is reported before size, because it is the more useful reason",
			"", "\x00" + big,
			"binary file — not shown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unrenderable(c.before, c.after); got != c.want {
				t.Errorf("unrenderable = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPullRequestDiffSaysWhyAFileIsNotShown(t *testing.T) {
	for _, note := range []string{"binary file — not shown", "file is larger than 256 KiB — not shown"} {
		r := newPRDiff(t, azdo.Change{Path: "/logo.png", ChangeType: "edit"})
		r.send(fileDiffMsg{PR: 512, Path: "/logo.png", Diff: fileDiff{note: note}})

		if !strings.Contains(r.frame(), note) {
			t.Errorf("the pane does not say %q:\n%s", note, r.frame())
		}
	}
}

func TestPullRequestDiffReportsAFileItCouldNotRead(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{err: "could not read the previous version: 404"}})

	if status, failed := r.view.Status(); !failed || !strings.Contains(status, "could not read") {
		t.Errorf("status = %q (error: %v), want the failure reported", status, failed)
	}
	if !strings.Contains(r.frame(), "could not read") {
		t.Errorf("the pane does not carry the failure:\n%s", r.frame())
	}
	// A failure must not wedge the path: it is recorded like any other answer,
	// so the row stops claiming to be loading.
	if r.view.loading["/a.go"] {
		t.Error("a failed read left the file marked as still loading")
	}
}

func TestPullRequestDiffSaysWhenAFilesTextIsUnchanged(t *testing.T) {
	// Azure DevOps lists a file whose mode changed, or one a merge reverted, as
	// a change with nothing to show.
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{}})

	if !strings.Contains(r.frame(), "(no textual changes)") {
		t.Errorf("a change with no hunks did not say so:\n%s", r.frame())
	}
}

func TestPullRequestDiffOnAPullRequestWithNoComputedMerge(t *testing.T) {
	c, pr := prDetailFixture() // no SourceCommit or TargetCommit
	r := drive(t, NewPullRequestDiff(c, pr, nil), diffWidth, diffHeight)
	r.send(prChangesMsg{PR: pr.ID})

	if !strings.Contains(r.frame(), "has not computed") {
		t.Errorf("an uncomputed merge was reported as a pull request changing no files:\n%s", r.frame())
	}
}

func TestPullRequestDiffOnAPullRequestChangingNothing(t *testing.T) {
	r := newPRDiff(t)

	if !strings.Contains(r.frame(), "changes no files") {
		t.Errorf("an empty change list did not say so:\n%s", r.frame())
	}
}

func TestPullRequestDiffReportsAFailedListing(t *testing.T) {
	c, pr := prDetailFixture()
	pr.SourceCommit, pr.TargetCommit = "source-sha", "target-sha"
	r := drive(t, NewPullRequestDiff(c, pr, nil), diffWidth, diffHeight)
	r.send(prChangesMsg{PR: pr.ID, Err: errTest})

	if status, failed := r.view.Status(); !failed || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q (error: %v), want the listing failure", status, failed)
	}
	// The body must not fall through to the empty state, which would claim the
	// pull request changes no files over a status line saying otherwise.
	frame := r.frame()
	if strings.Contains(frame, "changes no files") {
		t.Errorf("a failed listing rendered as an empty pull request:\n%s", frame)
	}
	if !strings.Contains(frame, "press r to try again") {
		t.Errorf("a failed listing does not offer the retry:\n%s", frame)
	}
}

func TestShortPathKeepsTheFileName(t *testing.T) {
	const path = "/internal/ui/pullrequestdetail.go"
	cases := []struct {
		name  string
		width int
		want  string
	}{
		{"it fits whole", 40, path},
		{"one directory has to go", 30, "…/ui/pullrequestdetail.go"},
		{"both directories have to go", 24, "…/pullrequestdetail.go"},
		// Nothing left to drop: the name itself is cut, which is the one case
		// where the tail is lost.
		{"not even the name fits", 10, "pullreque…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shortPath(path, c.width)
			if got != c.want {
				t.Errorf("shortPath(%d) = %q, want %q", c.width, got, c.want)
			}
			if lipgloss.Width(got) > c.width {
				t.Errorf("shortPath(%d) = %q, which is %d columns wide", c.width, got, lipgloss.Width(got))
			}
		})
	}
}

func TestPullRequestDiffRefreshIsNotPinnedByAFetchThatWasAlreadyInFlight(t *testing.T) {
	// Finding 3 from the branch review: the cursor sits on a file whose fetch
	// is already in flight when refresh is pressed. clear(m.diffs) empties the
	// map, but the in-flight fetch answers a moment later — before the
	// refreshed file list has come back to trigger a fresh one — and lands
	// carrying the same path. Storing it would win the race against the
	// refresh: fetchSelected would then find the path already present once the
	// new list landed and never ask again, silently keeping the reader on the
	// diff refresh was trying to replace.
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	if !r.view.loading["/a.go"] {
		t.Fatal("the fixture did not leave the selected file's fetch in flight")
	}

	r.send(runes("r"))
	if r.view.generation == 0 {
		t.Fatal("refresh did not advance the generation")
	}

	// The fetch that started before refresh was pressed lands now, still
	// carrying the generation it was issued under.
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: longHunk("stale")}, generation: 0})

	if _, ok := r.view.diffs["/a.go"]; ok {
		t.Fatal("a diff issued before the refresh was stored after it")
	}
	if strings.Count(r.frame(), "·") == 0 {
		t.Errorf("the row lost its pending marker to a diff refresh discarded:\n%s", r.frame())
	}

	// The refreshed file list now lands and must still ask for the file again
	// — the stale answer must not have left it looking already read.
	r.send(prChangesMsg{PR: 512, Changes: []azdo.Change{{Path: "/a.go", ChangeType: "edit"}}})
	if !r.view.loading["/a.go"] {
		t.Fatal("the refreshed list did not refetch a file a stale diff had wrongly marked as read")
	}

	// And the answer to that real fetch, carrying the current generation, is
	// the one that is allowed to land.
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{hunks: longHunk("fresh")}, generation: r.view.generation})
	if _, ok := r.view.diffs["/a.go"]; !ok {
		t.Error("a diff carrying the current generation was not stored")
	}
	if !strings.Contains(r.frame(), "fresh line 1") {
		t.Errorf("the current generation's diff did not render:\n%s", r.frame())
	}
}

func TestPullRequestDiffRefreshDiscardsWhatItRead(t *testing.T) {
	r := newPRDiff(t, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{note: "binary file — not shown"}})

	if cmd := r.send(runes("r")); cmd == nil {
		t.Fatal("r did not re-fetch")
	}
	if len(r.view.diffs) != 0 {
		t.Errorf("refresh kept %d cached diffs, want them discarded", len(r.view.diffs))
	}
}

func TestChangeSides(t *testing.T) {
	cases := []struct {
		changeType   string
		wantOld      bool
		wantNew      bool
		wantGlyphFor string // the palette style the row's glyph should use
	}{
		{"edit", true, true, "~"},
		{"add", false, true, "+"},
		{"delete", true, false, "−"},
		// A rename's previous content is under a path the change entry does not
		// name, so there is no old side to ask for.
		{"rename", false, true, "→"},
		{"edit, rename", false, true, "→"},
		{"sourceRename", false, true, "→"},
	}
	for _, c := range cases {
		t.Run(c.changeType, func(t *testing.T) {
			if got := hasOldSide(c.changeType); got != c.wantOld {
				t.Errorf("hasOldSide(%q) = %v, want %v", c.changeType, got, c.wantOld)
			}
			if got := hasNewSide(c.changeType); got != c.wantNew {
				t.Errorf("hasNewSide(%q) = %v, want %v", c.changeType, got, c.wantNew)
			}
			if got := changeGlyph(c.changeType); !strings.Contains(got, c.wantGlyphFor) {
				t.Errorf("changeGlyph(%q) = %q, want %q in it", c.changeType, got, c.wantGlyphFor)
			}
		})
	}
}

// TestDiffedLinesComeThroughAsHunks is the one test that runs a real diff. It
// is not here to re-check go-udiff's correctness — that is its maintainers'
// job — but to prove the two calls boardwalk makes are wired together the right
// way round, so an edit does not render as its own inverse.
func TestDiffedLinesComeThroughAsHunks(t *testing.T) {
	before := "one\ntwo\nthree\n"
	after := "one\nTWO\nthree\n"

	unified, err := udiff.ToUnifiedDiff("f", "f", before, udiff.Lines(before, after), udiff.DefaultContextLines)
	if err != nil {
		t.Fatalf("ToUnifiedDiff returned %v", err)
	}

	var b strings.Builder
	renderHunks(&b, unified.Hunks, nil, 40)
	got := b.String()

	if !strings.Contains(got, "-two") {
		t.Errorf("the line that went is not marked as a deletion:\n%s", got)
	}
	if !strings.Contains(got, "+TWO") {
		t.Errorf("the line that arrived is not marked as an addition:\n%s", got)
	}
	if !strings.Contains(got, " one") {
		t.Errorf("the surrounding context is missing:\n%s", got)
	}
}

// TestALineWhoseNeighbourChangedStaysContext is why Lines is called rather than
// Strings. Strings diffs runes and widens the result to line boundaries, which
// reports "three" here as removed and immediately re-added — noise the reader
// has to compare character by character to dismiss.
func TestALineWhoseNeighbourChangedStaysContext(t *testing.T) {
	before := "one\ntwo\nthree\n"
	after := "one\nTWO\nthree\nfour\n"

	unified, err := udiff.ToUnifiedDiff("f", "f", before, udiff.Lines(before, after), udiff.DefaultContextLines)
	if err != nil {
		t.Fatalf("ToUnifiedDiff returned %v", err)
	}

	var b strings.Builder
	renderHunks(&b, unified.Hunks, nil, 40)
	got := b.String()

	if strings.Contains(got, "-three") {
		t.Errorf("an unchanged line was reported as removed:\n%s", got)
	}
	if !strings.Contains(got, " three") {
		t.Errorf("an unchanged line is not shown as context:\n%s", got)
	}
	if !strings.Contains(got, "+four") {
		t.Errorf("the genuinely new line is missing:\n%s", got)
	}
}

func TestPullRequestDetailOpensTheDiff(t *testing.T) {
	m := newPRDetail(t, nil)
	m.pr.SourceCommit, m.pr.TargetCommit = "source-sha", "target-sha"
	r := drive(t, m, 100, 40)

	cmd := r.send(runes("D"))
	if cmd == nil {
		t.Fatal("D produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("D produced %T, want a PushMsg", cmd())
	}
	if _, ok := push.View.(*PullRequestDiff); !ok {
		t.Errorf("D pushed %T, want the diff view", push.View)
	}
	if !strings.Contains(helpLine(m.Keys()), "D diff") {
		t.Errorf("help = %q, want the diff key advertised", helpLine(m.Keys()))
	}
}

func TestPullRequestDetailSaysWhenThereIsNothingToDiff(t *testing.T) {
	m := newPRDetail(t, nil) // the fixture has no merge commits
	r := drive(t, m, 100, 40)

	if cmd := r.send(runes("D")); cmd != nil {
		t.Fatalf("D pushed a view with no commits to diff: %T", cmd())
	}
	if status, _ := m.Status(); !strings.Contains(status, "has not computed") {
		t.Errorf("status = %q, want the reason there is nothing to diff", status)
	}
}

func TestPullRequestDetailDiffKeyDoesNotAbandonAnArmedVote(t *testing.T) {
	// A vote armed by A is confirmed by a second A. Letting D push a whole view
	// over the top would leave that arm alive under a screen with no room to
	// say so, so the arm keeps every key it does not recognise — and keeps
	// saying on the status line what is still pending.
	m := newPRDetail(t, nil)
	m.pr.SourceCommit, m.pr.TargetCommit = "source-sha", "target-sha"
	r := drive(t, m, 100, 40)
	r.send(runes("A"))

	if cmd := r.send(runes("D")); cmd != nil {
		t.Fatalf("D was acted on while a vote was armed: %T", cmd())
	}
	if m.armedVote == nil {
		t.Error("D cancelled the armed vote instead of leaving it alone")
	}
	if status, _ := m.Status(); !strings.Contains(status, "press again to approve") {
		t.Errorf("status = %q, want the arm still explaining itself", status)
	}
}

func TestPullRequestDetailReplyPromptSwallowsTheDiffKey(t *testing.T) {
	// Typing "D" into a reply has to add the letter, not open a view.
	m := newPRDetail(t, []azdo.Thread{{ID: 1, Status: "active",
		Comments: []azdo.ThreadComment{{ID: 9, Author: "Other Dev", Text: "why?"}}}})
	r := drive(t, m, 100, 40)
	r.send(runes("c"))

	if cmd := r.send(runes("D")); cmd != nil {
		if _, ok := cmd().(PushMsg); ok {
			t.Fatal("D opened the diff view from inside the reply prompt")
		}
	}
	if m.replyPrompt == nil {
		t.Fatal("the reply prompt closed")
	}
	if got := m.replyPrompt.Value(); got != "D" {
		t.Errorf("the prompt holds %q, want the typed letter", got)
	}
}
