package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func prDetailFixture() (*azdo.Client, azdo.PullRequest) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, azdo.PullRequest{
		ID: 512, Title: "Retry webhooks", Repo: "platform-api", RepoID: "r1",
		Author: "Other Dev", AuthorKey: "other@acme.test",
		Source: "refs/heads/feature/4021-retry", Target: "refs/heads/main",
		Created:     now.Add(-3 * time.Hour),
		Description: "Adds **backoff** to delivery.",
		Reviewers: []azdo.Reviewer{
			{Name: "Dev Example", Key: "dev@acme.test", Vote: 10},
			{Name: "platform-devs", IsGroup: true},
		},
	}
}

func newPRDetail(t *testing.T, threads []azdo.Thread) *PullRequestDetail {
	t.Helper()
	c, pr := prDetailFixture()
	m := NewPullRequestDetail(c, pr, threads)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestPullRequestDetailShowsWhatTheListSummaryCannot(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{
		{Status: "active", File: "/internal/azdo/client.go", Comments: []azdo.ThreadComment{
			{Author: "Other Dev", Text: "Why the retry cap?"},
			{Author: "Dev Example", Text: "Three felt right."},
		}},
	})
	view := drive(t, m, 100, 60).frame()

	for _, want := range []string{
		"!512", "Retry webhooks", "platform-api", "Other Dev",
		"feature/4021-retry", "main", "3h ago",
		"Dev Example", "approved", "platform-devs", "(group)",
		"backoff", "Why the retry cap?",
		// The reply is the whole reason this view exists: the list's side pane
		// only ever showed a thread's opening comment.
		"Three felt right.",
		"/internal/azdo/client.go",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the pull request view is missing %q:\n%s", want, view)
		}
	}
}

func TestPullRequestDetailPutsUnresolvedThreadsFirst(t *testing.T) {
	// Settled discussion is history; what is unresolved is why you opened it.
	m := newPRDetail(t, []azdo.Thread{
		{Status: "fixed", Resolved: true, Comments: []azdo.ThreadComment{{Author: "A", Text: "SETTLED"}}},
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "B", Text: "OUTSTANDING"}}},
	})
	view := drive(t, m, 100, 60).frame()

	settled, outstanding := strings.Index(view, "SETTLED"), strings.Index(view, "OUTSTANDING")
	if settled < 0 || outstanding < 0 {
		t.Fatalf("a thread is missing entirely:\n%s", view)
	}
	if outstanding > settled {
		t.Errorf("the resolved thread came first:\n%s", view)
	}
}

func TestPullRequestDetailCountsItsThreads(t *testing.T) {
	m := newPRDetail(t, threadsResolved(3, 2))
	view := drive(t, m, 100, 60).frame()

	if !strings.Contains(view, "3 resolved, 2 unresolved") {
		t.Errorf("the discussion heading does not carry the tally:\n%s", view)
	}
}

func TestPullRequestDetailFetchesWhenTheListHadNothingCached(t *testing.T) {
	m := newPRDetail(t, nil)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the discussion")
	}
	if !m.work.busy() {
		t.Error("the spinner is not running while the fetch is in flight")
	}
}

func TestPullRequestDetailUsesThreadsTheListAlreadyHad(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{{Status: "active", Comments: []azdo.ThreadComment{{Author: "A", Text: "hi"}}}})

	if cmd := m.Init(); cmd != nil {
		t.Error("the view refetched a discussion it was handed")
	}
}

func TestPullRequestDetailRendersThreadsThatArriveLater(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 512, Threads: []azdo.Thread{
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "Third Dev", Text: "Landed later"}}},
	}})

	if !strings.Contains(r.frame(), "Landed later") {
		t.Errorf("the fetched discussion did not render:\n%s", r.frame())
	}
	if m.work.busy() {
		t.Error("still spinning after the discussion landed")
	}
}

func TestPullRequestDetailSaysSoWhenTheFetchFails(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 512, Err: errTest})

	if strings.Contains(r.frame(), "loading…") {
		t.Errorf("the pane still claims to be loading:\n%s", r.frame())
	}
	if status, isErr := m.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
	if m.work.busy() {
		t.Error("still spinning after the fetch failed")
	}
}

func TestPullRequestDetailIgnoresAnotherPullRequestsThreads(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 511, Threads: []azdo.Thread{
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "Nobody", Text: "stale comment"}}},
	}})

	if strings.Contains(r.frame(), "stale comment") {
		t.Error("a discussion belonging to another pull request was rendered")
	}
}

func TestPullRequestDetailScrollSurvivesARepaint(t *testing.T) {
	var threads []azdo.Thread
	for i := range 30 {
		threads = append(threads, azdo.Thread{Status: "active", Comments: []azdo.ThreadComment{
			{Author: "Dev Example", Text: strings.Repeat("filler ", 8) + string(rune('a'+i%26))},
		}})
	}
	m := newPRDetail(t, threads)
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

func TestPullRequestDetailEscapeGoesBack(t *testing.T) {
	m := newPRDetail(t, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced no command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("escape produced %T, want a PopMsg", cmd())
	}
}

func TestPullRequestsEnterOpensTheDetail(t *testing.T) {
	m := newPRs(t)
	m.drafts = draftsAll
	m.applyFilters()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "!512") {
		t.Errorf("pushed view = %q, want the selected pull request", push.View.Title())
	}
}

func TestPullRequestsEnterCarriesThreadsItAlreadyFetched(t *testing.T) {
	// Opening a pull request whose counts are already on screen must not go
	// back to the server for the discussion behind them.
	m := newPRs(t)
	updated, _ := m.Update(threadsMsg{PR: 512, Threads: threadsResolved(1, 1)})
	m = updated.(*PullRequests)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push := cmd().(PushMsg)

	if init := initialise(push.View); init != nil {
		t.Error("the detail view refetched a discussion the list already had")
	}
}
