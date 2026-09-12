package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func prFixture() (*azdo.Client, []azdo.PullRequest) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, []azdo.PullRequest{
		{ID: 512, Title: "Retry webhooks", Repo: "platform-api", RepoID: "r1",
			Author: "Dev Example", AuthorKey: "dev@acme.test",
			Source: "refs/heads/feature/4021-retry", Target: "refs/heads/main",
			Created: now.Add(-3 * time.Hour), Description: "Adds backoff",
			Reviewers: []azdo.Reviewer{{Name: "Other Dev", Vote: 10}}},
		{ID: 511, Title: "WIP cache", Repo: "platform-web", RepoID: "r2",
			Author: "Other Dev", IsDraft: true,
			Source: "refs/heads/spike", Target: "refs/heads/main",
			Created: now.Add(-48 * time.Hour)},
	}
}

func newPRs(t *testing.T) *PullRequests {
	t.Helper()
	c, prs := prFixture()
	m := NewPullRequests(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	// NewPullRequests scopes to the working directory's repository when
	// azdo.CurrentRepo() finds one, which depends on this checkout's git
	// remote. Pinning it here keeps the tests' outcome independent of where
	// boardwalk itself was cloned from.
	m.repoOnly = false
	updated, _ := m.Update(prsMsg{PRs: prs})
	m = updated.(*PullRequests)
	m.Body(160, 20)
	return m
}

func TestPullRequestsRowShowsBranchesAndAge(t *testing.T) {
	m := newPRs(t)
	view := m.Body(160, 20)

	for _, want := range []string{"platform-api", "!512", "feature/4021-retry", "main", "3h"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "refs/heads/") {
		t.Error("the refs/heads prefix was not trimmed")
	}
}

func TestPullRequestsHidesDraftsByDefault(t *testing.T) {
	m := newPRs(t)

	if strings.Contains(m.Body(160, 20), "WIP cache") {
		t.Error("a draft showed up in the default view")
	}
	if !strings.Contains(m.Title(), "drafts hidden") {
		t.Errorf("title = %q, want the filter named", m.Title())
	}
}

func TestPullRequestsDraftFilterCycles(t *testing.T) {
	m := newPRs(t)

	// hidden -> only
	updated, _ := m.Update(runes("d"))
	m = updated.(*PullRequests)
	view := m.Body(160, 20)
	if !strings.Contains(view, "WIP cache") || strings.Contains(view, "Retry webhooks") {
		t.Errorf("drafts-only did not filter:\n%s", view)
	}

	// only -> all
	updated, _ = m.Update(runes("d"))
	m = updated.(*PullRequests)
	view = m.Body(160, 20)
	if !strings.Contains(view, "WIP cache") || !strings.Contains(view, "Retry webhooks") {
		t.Errorf("all did not show both:\n%s", view)
	}

	// all -> hidden
	updated, _ = m.Update(runes("d"))
	m = updated.(*PullRequests)
	if strings.Contains(m.Body(160, 20), "WIP cache") {
		t.Error("the filter did not cycle back to hidden")
	}
}

func TestPullRequestsThreadCountsRenderOnceFetched(t *testing.T) {
	m := newPRs(t)

	if !strings.Contains(m.Body(160, 20), "…") {
		t.Error("an unfetched thread count did not render as an ellipsis")
	}

	updated, _ := m.Update(threadsMsg{PR: 512, Counts: azdo.ThreadCounts{
		Resolved: 4, Unresolved: 2,
		Open: []azdo.OpenThread{{Author: "Other Dev", Text: "Why the retry cap?"}},
	}})
	m = updated.(*PullRequests)

	view := m.Body(160, 20)
	if !strings.Contains(view, "4/2") {
		t.Errorf("the counts did not render as resolved/unresolved:\n%s", view)
	}
	if !strings.Contains(view, "Why the retry cap?") {
		t.Errorf("the open thread did not reach the detail pane:\n%s", view)
	}
}

func TestPullRequestsThreadFetchFailureDoesNotLoseTheRow(t *testing.T) {
	m := newPRs(t)

	updated, _ := m.Update(threadsMsg{PR: 512, Err: errTest})
	m = updated.(*PullRequests)

	if _, isErr := m.Status(); !isErr {
		t.Error("a thread fetch failure was not reported")
	}
	view := m.Body(160, 20)
	if !strings.Contains(view, "Retry webhooks") {
		t.Error("the row was lost when its thread fetch failed")
	}
	if strings.Contains(view, "0/0") {
		t.Error("a failed fetch rendered as a pull request with no discussion")
	}
	if !strings.Contains(view, "…") {
		t.Error("the count column did not stay unfetched after the failure")
	}
	// Task 10's bug: a failed lazy fetch left its in-flight guard set forever,
	// so the row was skipped on every future selection and never retried.
	// fetchThreads must still issue a command for this row.
	if cmd := m.fetchThreads(); cmd == nil {
		t.Error("a failed fetch left the in-flight guard set, blocking a retry")
	}
}

func TestPullRequestsRepoScopeToggle(t *testing.T) {
	m := newPRs(t)
	m.repo = "platform-api"
	m.repoOnly = true

	if strings.Contains(m.Body(160, 20), "platform-web") {
		t.Error("the repository scope did not filter")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(*PullRequests)

	// Widening shows the other repository's pull request, drafts aside.
	if !strings.Contains(m.Title(), "all repos") {
		t.Errorf("title = %q, want the widened scope named", m.Title())
	}
}

func TestPullRequestsLabelAndURL(t *testing.T) {
	_, prs := prFixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	row := prRow{PullRequest: prs[0], url: c.PullRequestURL(prs[0].Repo, prs[0].ID)}

	if got := row.Label(); got != "!512 Retry webhooks" {
		t.Errorf("Label = %q", got)
	}
	if got := row.CopyID(); got != "512" {
		t.Errorf("CopyID = %q", got)
	}
	if !strings.HasSuffix(row.URL(), "/_git/platform-api/pullrequest/512") {
		t.Errorf("URL = %q", row.URL())
	}
}

func TestPullRequestsFailedFetchReplacesTheFetchingPlaceholder(t *testing.T) {
	// loaded is only set by a successful fetch, so a failed one left the body
	// reading "fetching pull requests…" while the status line under it said the
	// fetch had failed — two contradictory statements on one screen.
	c, _ := prFixture()
	m := NewPullRequests(c)

	if got := m.Body(160, 20); !strings.Contains(got, "fetching pull requests") {
		t.Fatalf("expected the fetching placeholder before anything lands:\n%s", got)
	}

	updated, _ := m.Update(ErrMsg{Err: errTest})
	m = updated.(*PullRequests)

	got := m.Body(160, 20)
	if strings.Contains(got, "fetching pull requests") {
		t.Errorf("the body still claims to be fetching after the fetch failed:\n%s", got)
	}
	if !strings.Contains(got, errTest.Error()) {
		t.Errorf("the body does not say what went wrong:\n%s", got)
	}
}

func TestPullRequestsRefetchesOnR(t *testing.T) {
	// Without a refresh key a failed initial fetch is unrecoverable short of
	// restarting boardwalk.
	m := newPRs(t)

	updated, cmd := m.Update(runes("r"))
	m = updated.(*PullRequests)

	if cmd == nil {
		t.Fatal("r did not refetch the pull requests")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "refresh") {
		t.Errorf("status = %q, isErr = %v; want the refresh reported", status, isErr)
	}
	if !strings.Contains(m.Hints(), "r refresh") {
		t.Errorf("hints = %q, want the refresh key offered", m.Hints())
	}
}

func TestPullRequestsSpinsWhileLoadingAndStopsWhenItLands(t *testing.T) {
	c, prs := prFixture()
	m := NewPullRequests(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	m.repoOnly = false
	m.drafts = draftsAll // both fixture rows visible, so the fan-out is two deep

	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init produced no command")
	}
	r := drive(t, m, 160, 20)

	if !strings.Contains(r.frame(), "fetching pull requests") {
		t.Fatalf("body is not the loading placeholder:\n%s", r.frame())
	}
	if !m.work.busy() {
		t.Error("the view is not busy while its first fetch is outstanding")
	}

	// The listing lands and immediately fans out one thread fetch per row.
	r.send(prsMsg{PRs: prs})
	if !m.work.busy() {
		t.Fatal("stopped spinning between the listing landing and its thread fetches")
	}

	// One of the two answers. The spinner has to outlast it.
	r.send(threadsMsg{PR: 512, Counts: azdo.ThreadCounts{Resolved: 4, Unresolved: 2}})
	if !m.work.busy() {
		t.Error("stopped spinning after the first of two batched calls landed")
	}
	if status, _ := m.Status(); status == "" {
		t.Error("the status line carries no spinner while work is outstanding")
	}

	// The last one. Now it stops.
	r.send(threadsMsg{PR: 511, Counts: azdo.ThreadCounts{}})
	if m.work.busy() {
		t.Error("still spinning after every batched call landed")
	}
	if status, _ := m.Status(); status != "" {
		t.Errorf("status = %q, want it empty once the spinner stops", status)
	}
}

func TestPullRequestsStopsSpinningWhenAFetchFails(t *testing.T) {
	// A failure that left the counter up would spin forever with nothing
	// running — the same shape as the in-flight guards that stranded rows.
	c, _ := prFixture()
	m := NewPullRequests(c)
	m.repoOnly = false
	m.Init()
	r := drive(t, m, 160, 20)

	r.send(ErrMsg{Err: errTest})

	if m.work.busy() {
		t.Error("still spinning after the fetch failed")
	}
}
