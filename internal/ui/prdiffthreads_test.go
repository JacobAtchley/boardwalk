package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/aymanbagabas/go-udiff"
)

// reviewThreads is one discussion per interesting position: a line inside the
// hunk the tests render, a line nowhere near it, and the pull request itself.
func reviewThreads() []azdo.Thread {
	return []azdo.Thread{
		{ID: 1, Status: "active", File: "/internal/ui/logs.go", Line: 11, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev Example", Text: "this leaks"}}},
		{ID: 2, Status: "fixed", Resolved: true, File: "/internal/ui/logs.go", Line: 900, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Other Dev", Text: "settled long ago"}}},
		{ID: 3, Status: "active", File: "/internal/ui/prdiff.go", Line: 4, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev Example", Text: "different file"}}},
		{ID: 4, Status: "active",
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev Example", Text: "about the whole thing"}}},
	}
}

// newPRDiffWithThreads drives the diff view with a discussion already in hand,
// which is how the detail view hands it over.
func newPRDiffWithThreads(t *testing.T, threads []azdo.Thread, changes ...azdo.Change) *runtimeView[*PullRequestDiff] {
	t.Helper()
	c, pr := prDetailFixture()
	pr.SourceCommit, pr.TargetCommit = "source-sha", "target-sha"

	r := drive(t, NewPullRequestDiff(c, pr, threads, filterAll), diffWidth, diffHeight)
	r.send(prChangesMsg{PR: pr.ID, Changes: changes})
	return r
}

// logsHunk covers new-side lines 10 to 14, with a deletion in the middle that
// advances the old side but not the new one — which is the whole reason the
// numbering has to be walked rather than counted.
func logsHunk() *udiff.Hunk {
	return hunk(10, 10,
		line(udiff.Equal, "func tail() {\n"),
		line(udiff.Delete, "\tgo poll()\n"),
		line(udiff.Insert, "\tdefer close(ch)\n"),
		line(udiff.Insert, "\tgo poll(ctx)\n"),
		line(udiff.Equal, "}\n"),
	)
}

func TestDiffLineNumbersWalkEachSideSeparately(t *testing.T) {
	got := diffLineNumbers(logsHunk())

	// New side: 10 for the context line, nothing for the deletion, 11 and 12
	// for the insertions, 13 for the trailing context.
	wantRight := []int{10, 0, 11, 12, 13}
	// Old side: 10 for the context, 11 for the deletion, nothing for the
	// insertions, 12 for the trailing context.
	wantLeft := []int{10, 11, 0, 0, 12}

	for i := range wantRight {
		if got[i].right != wantRight[i] {
			t.Errorf("line %d: new side = %d, want %d", i, got[i].right, wantRight[i])
		}
		if got[i].left != wantLeft[i] {
			t.Errorf("line %d: old side = %d, want %d", i, got[i].left, wantLeft[i])
		}
	}
}

func TestThreadRendersUnderTheLineItWasWrittenAgainst(t *testing.T) {
	r := newPRDiffWithThreads(t, reviewThreads(),
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go", Diff: fileDiff{hunks: []*udiff.Hunk{logsHunk()}}})

	view := r.frame()
	if !strings.Contains(view, "this leaks") {
		t.Fatalf("the thread is not in the pane at all:\n%s", view)
	}

	lines := strings.Split(view, "\n")
	anchor, comment := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "defer close(ch)") {
			anchor = i
		}
		if strings.Contains(l, "this leaks") {
			comment = i
		}
	}
	if anchor < 0 {
		t.Fatalf("the line the thread is anchored to is not rendered:\n%s", view)
	}
	// New-side line 11 is the first insertion. The comment belongs directly
	// under it, not collected at the foot of the file.
	if comment <= anchor || comment > anchor+4 {
		t.Errorf("the thread is at line %d and its anchor at %d — want it just below:\n%s",
			comment, anchor, view)
	}
}

// TestThreadOutsideTheHunksIsStillShown — a comment on a line the diff's
// context does not reach. Dropping it would hide review that exists.
func TestThreadOutsideTheHunksIsStillShown(t *testing.T) {
	r := newPRDiffWithThreads(t, reviewThreads(),
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go", Diff: fileDiff{hunks: []*udiff.Hunk{logsHunk()}}})

	view := r.frame()
	if !strings.Contains(view, "settled long ago") {
		t.Errorf("a thread outside the rendered hunks was dropped:\n%s", view)
	}
	if !strings.Contains(view, "900") {
		t.Errorf("the out-of-hunk thread does not say which line it is on:\n%s", view)
	}
}

// TestThreadsFromOtherFilesStayOut — the pane is one file, and a comment from
// another one appearing in it would be worse than not showing it at all.
func TestThreadsFromOtherFilesStayOut(t *testing.T) {
	r := newPRDiffWithThreads(t, reviewThreads(),
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go", Diff: fileDiff{hunks: []*udiff.Hunk{logsHunk()}}})

	view := r.frame()
	if strings.Contains(view, "different file") {
		t.Errorf("another file's thread is in this file's pane:\n%s", view)
	}
	if strings.Contains(view, "about the whole thing") {
		t.Errorf("a thread on the pull request itself is in a file's pane:\n%s", view)
	}
}

func TestChangedFileRowCountsItsDiscussion(t *testing.T) {
	r := newPRDiffWithThreads(t, reviewThreads(),
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"},
		azdo.Change{Path: "/internal/ui/prdiff.go", ChangeType: "add"},
		azdo.Change{Path: "/go.mod", ChangeType: "edit"})

	view := r.frame()
	rowOf := func(name string) string {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, name) {
				return l
			}
		}
		t.Fatalf("no row for %s:\n%s", name, view)
		return ""
	}

	// Two threads on logs.go, one on prdiff.go, none on go.mod.
	if got := rowOf("logs.go"); !strings.Contains(got, "2") {
		t.Errorf("row = %q, want it counting the two threads on the file", got)
	}
	if got := rowOf("go.mod"); strings.Contains(got, "💬") {
		t.Errorf("row = %q, want no comment marker on a file with no discussion", got)
	}
}

// TestDiffWithNoThreadsIsUnchanged — the overwhelmingly common case, and the
// one where an empty "discussion" heading or a stray marker would be noise.
func TestDiffWithNoThreadsIsUnchanged(t *testing.T) {
	r := newPRDiffWithThreads(t, nil,
		azdo.Change{Path: "/internal/ui/logs.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/internal/ui/logs.go", Diff: fileDiff{hunks: []*udiff.Hunk{logsHunk()}}})

	view := r.frame()
	if strings.Contains(view, "💬") {
		t.Errorf("a comment marker appeared with no comments:\n%s", view)
	}
	if !strings.Contains(view, "defer close(ch)") {
		t.Errorf("the diff itself stopped rendering:\n%s", view)
	}
}

// TestResolvedThreadIsMarkedAsSuch — an unresolved comment is something to
// act on and a resolved one is history. Rendering them identically in the
// place you read the code would make the pane misleading.
func TestResolvedThreadIsMarkedAsSuch(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "active", File: "/a.go", Line: 10, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "open question"}}},
		{ID: 2, Status: "fixed", Resolved: true, File: "/a.go", Line: 10, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "closed question"}}},
	}
	r := newPRDiffWithThreads(t, threads, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{
		hunks: []*udiff.Hunk{hunk(10, 10, line(udiff.Equal, "x := 1\n"))}}})

	view := r.frame()
	if !strings.Contains(view, "unresolved") {
		t.Errorf("the open thread is not marked unresolved:\n%s", view)
	}
	if !strings.Contains(view, "resolved") {
		t.Errorf("the settled thread is not marked resolved:\n%s", view)
	}
}

func TestDiffPaneFilterKeyHidesTheSettledComments(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "active", File: "/a.go", Line: 10, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "OPEN QUESTION"}}},
		{ID: 2, Status: "fixed", Resolved: true, File: "/a.go", Line: 10, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "CLOSED QUESTION"}}},
	}
	r := newPRDiffWithThreads(t, threads, azdo.Change{Path: "/a.go", ChangeType: "edit"})
	r.send(fileDiffMsg{PR: 512, Path: "/a.go", Diff: fileDiff{
		hunks: []*udiff.Hunk{hunk(10, 10, line(udiff.Equal, "x := 1\n"))}}})

	r.send(runes("f"))
	view := r.frame()
	if strings.Contains(view, "CLOSED QUESTION") || !strings.Contains(view, "OPEN QUESTION") {
		t.Errorf("f did not leave the unresolved comment alone under the code:\n%s", view)
	}

	r.send(runes("f"))
	view = r.frame()
	if !strings.Contains(view, "CLOSED QUESTION") || strings.Contains(view, "OPEN QUESTION") {
		t.Errorf("the second press of f did not leave the settled comment alone:\n%s", view)
	}
}

func TestChangedFileRowCountsOnlyWhatTheFilterShows(t *testing.T) {
	// The badge and the comments under the code have to agree: a row saying
	// two while the pane shows one is the row lying about which files still
	// need reading.
	threads := []azdo.Thread{
		{ID: 1, Status: "active", File: "/a.go", Line: 10, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "open"}}},
		{ID: 2, Status: "fixed", Resolved: true, File: "/a.go", Line: 11, RightSide: true,
			Comments: []azdo.ThreadComment{{ID: 1, Author: "Dev", Text: "closed"}}},
	}
	r := newPRDiffWithThreads(t, threads, azdo.Change{Path: "/a.go", ChangeType: "edit"})

	rowOf := func() string {
		for _, l := range strings.Split(r.frame(), "\n") {
			if strings.Contains(l, "a.go") {
				return l
			}
		}
		t.Fatalf("no row for a.go:\n%s", r.frame())
		return ""
	}

	if got := rowOf(); !strings.Contains(got, "💬2") {
		t.Errorf("row = %q, want it counting both threads unfiltered", got)
	}

	r.send(runes("f"))
	if got := rowOf(); !strings.Contains(got, "💬1") {
		t.Errorf("row = %q, want it counting only the unresolved thread", got)
	}
}

func TestDiffPaneKeepsTheFilterTheDetailViewWasShowing(t *testing.T) {
	// Drilling into the code from a filtered discussion and finding every
	// settled comment back under it would undo the filtering by hand.
	m := newPRDetail(t, threadsResolved(1, 1))
	m.pr.SourceCommit, m.pr.TargetCommit = "source-sha", "target-sha"
	r := drive(t, m, 100, 60)

	r.send(runes("f"))
	cmd := r.send(runes("D"))
	if cmd == nil {
		t.Fatal("D did not open the diff pane")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("D produced %T, want a PushMsg", cmd())
	}
	diff, ok := push.View.(*PullRequestDiff)
	if !ok {
		t.Fatalf("D pushed %T, want the diff pane", push.View)
	}
	if diff.filter != filterUnresolved {
		t.Errorf("the diff pane opened showing %v, want the filter the detail view had", diff.filter)
	}
}
