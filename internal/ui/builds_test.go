package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func buildFixture() (*azdo.Client, []azdo.Build) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, []azdo.Build{
		{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: azdo.StatusRunning,
			Queued: now.Add(-20 * time.Minute), SourceBranch: "refs/heads/main", RequestedFor: "Dev Example"},
		{ID: 9000, Number: "20260911.2", Pipeline: "platform-web-ci", Status: azdo.StatusFailed,
			Queued: now.Add(-2 * time.Hour), SourceBranch: "refs/heads/feature/x", RequestedFor: "Other Dev"},
	}
}

func newBuilds(t *testing.T) *Builds {
	t.Helper()
	c, builds := buildFixture()
	m := NewBuilds(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	updated, _ := m.Update(buildsMsg{Builds: builds})
	m = updated.(*Builds)
	m.Body(160, 20)
	return m
}

func TestBuildsRowShowsPipelineStatusAndAge(t *testing.T) {
	m := newBuilds(t)
	view := m.Body(160, 20)

	for _, want := range []string{"platform-ci", "20260911.3", "running", "20m", "failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestBuildsShowsTheCurrentStepOnceTheTimelineLands(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{
		Build:    9001,
		Progress: azdo.Progress{CurrentStep: "Run tests"},
		Records:  []azdo.Record{{Name: "Run tests", Type: "Task", State: "inProgress", Order: 1, LogID: 7}},
	})
	m = updated.(*Builds)

	if !strings.Contains(m.Body(160, 20), "Run tests") {
		t.Errorf("the current step did not render:\n%s", m.Body(160, 20))
	}
}

func TestBuildsShowsTheErrorCount(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{
		Build:    9000,
		Progress: azdo.Progress{CurrentStep: "Test", Errors: 3},
	})
	m = updated.(*Builds)

	if !strings.Contains(m.Body(160, 20), "3 errors") {
		t.Errorf("the error count did not render:\n%s", m.Body(160, 20))
	}
}

func TestBuildsEnterOpensTheLogs(t *testing.T) {
	m := newBuilds(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg := cmd()
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", msg)
	}
	if !strings.Contains(push.View.Title(), "20260911.3") {
		t.Errorf("pushed view = %q, want the selected build's logs", push.View.Title())
	}
}

func TestBuildsFilterSpansThePipelineName(t *testing.T) {
	m := newBuilds(t)
	row, _ := m.browser.Selected()

	if !strings.Contains(row.FilterValue(), "platform-ci") {
		t.Errorf("FilterValue = %q, want the pipeline name in it", row.FilterValue())
	}
	if !strings.Contains(row.FilterValue(), "20260911.3") {
		t.Errorf("FilterValue = %q, want the build number in it", row.FilterValue())
	}
}

func TestBuildsLabelAndURL(t *testing.T) {
	_, builds := buildFixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	row := buildRow{Build: builds[0], url: c.BuildURL(9001)}

	if got := row.Label(); got != "platform-ci #20260911.3" {
		t.Errorf("Label = %q", got)
	}
	if got := row.CopyID(); got != "9001" {
		t.Errorf("CopyID = %q", got)
	}
	if !strings.HasSuffix(row.URL(), "buildId=9001") {
		t.Errorf("URL = %q", row.URL())
	}
}

func TestBuildsTimelineFailureDoesNotLoseTheRow(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{Build: 9001, Err: errTest})
	m = updated.(*Builds)

	if _, isErr := m.Status(); !isErr {
		t.Error("a timeline failure was not reported")
	}
	if !strings.Contains(m.Body(160, 20), "platform-ci") {
		t.Error("the row was lost when its timeline fetch failed")
	}
}

func TestBuildsTimelineFailureAllowsARetry(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{Build: 9001, Err: errTest})
	m = updated.(*Builds)

	// Task 12's bug: a failed lazy fetch left its in-flight guard set forever,
	// so the row was skipped on every future selection and never retried.
	// fetchTimelines must still issue a command for this build.
	if cmd := m.fetchTimelines(); cmd == nil {
		t.Error("a failed fetch left the in-flight guard set, blocking a retry")
	}
}

func TestBuildsFailedFetchReplacesTheFetchingPlaceholder(t *testing.T) {
	c, _ := buildFixture()
	m := NewBuilds(c)

	if got := m.Body(160, 20); !strings.Contains(got, "fetching builds") {
		t.Fatalf("expected the fetching placeholder before anything lands:\n%s", got)
	}

	updated, _ := m.Update(ErrMsg{Err: errTest})
	m = updated.(*Builds)

	got := m.Body(160, 20)
	if strings.Contains(got, "fetching builds") {
		t.Errorf("the body still claims to be fetching after the fetch failed:\n%s", got)
	}
	if !strings.Contains(got, errTest.Error()) {
		t.Errorf("the body does not say what went wrong:\n%s", got)
	}
}

func TestMatchBuildPullRequestByMergeRef(t *testing.T) {
	build := azdo.Build{ID: 9001, SourceBranch: "refs/pull/512/merge"}
	prs := []azdo.PullRequest{
		{ID: 400, Source: "refs/heads/spike"},
		{ID: 512, Source: "refs/heads/feature/4021-retry"},
	}

	pr, ok := matchBuildPullRequest(build, prs)
	if !ok || pr.ID != 512 {
		t.Fatalf("matchBuildPullRequest = (%+v, %v), want pull request 512", pr, ok)
	}
}

func TestMatchBuildPullRequestByBranch(t *testing.T) {
	build := azdo.Build{ID: 9000, SourceBranch: "refs/heads/feature/4021-retry"}
	prs := []azdo.PullRequest{
		{ID: 400, Source: "refs/heads/spike"},
		{ID: 512, Source: "refs/heads/feature/4021-retry"},
	}

	pr, ok := matchBuildPullRequest(build, prs)
	if !ok || pr.ID != 512 {
		t.Fatalf("matchBuildPullRequest = (%+v, %v), want pull request 512", pr, ok)
	}
}

func TestMatchBuildPullRequestMergeRefDoesNotFallBackToBranchMatching(t *testing.T) {
	// A merge ref names its pull request directly; it must not also be
	// compared against Source, which is always a refs/heads ref and could
	// never equal it anyway.
	build := azdo.Build{ID: 9002, SourceBranch: "refs/pull/999/merge"}
	prs := []azdo.PullRequest{{ID: 400, Source: "refs/pull/999/merge"}}

	if _, ok := matchBuildPullRequest(build, prs); ok {
		t.Error("a merge ref matched a pull request by comparing Source directly, it should only match by id")
	}
}

func TestMatchBuildPullRequestNoMatch(t *testing.T) {
	build := azdo.Build{ID: 9003, SourceBranch: "refs/heads/main"}
	prs := []azdo.PullRequest{{ID: 400, Source: "refs/heads/spike"}}

	if _, ok := matchBuildPullRequest(build, prs); ok {
		t.Error("expected no match for a branch with no open pull request")
	}
}

func TestBuildsPKeyPushesTheLinkedPullRequest(t *testing.T) {
	m := newBuilds(t) // selected row is build 9001, refs/heads/main

	// The command is not invoked: it fires a real fetch, which m's zero-value
	// client cannot make. p pushing nothing synchronously — the lookup has to
	// land first — is confirmed by the assertions after buildPRMsg arrives.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}); cmd == nil {
		t.Fatal("p produced no command")
	}

	updated, cmd := m.Update(buildPRMsg{
		Build: 9001,
		PR:    azdo.PullRequest{ID: 512, Title: "Retry webhooks", Source: "refs/heads/main"},
		Found: true,
	})
	m = updated.(*Builds)
	if cmd == nil {
		t.Fatal("the lookup landing produced no command")
	}

	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("the lookup landing produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "512") {
		t.Errorf("pushed view = %q, want the matched pull request", push.View.Title())
	}
}

func TestBuildsPKeyNoMatchSaysSoOnTheStatusLine(t *testing.T) {
	m := newBuilds(t)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated, cmd := m.Update(buildPRMsg{Build: 9001, Branch: "refs/heads/main", Found: false})
	m = updated.(*Builds)

	if cmd != nil {
		if _, ok := cmd().(PushMsg); ok {
			t.Fatal("a build with no matching pull request pushed a view")
		}
	}
	status, failed := m.Status()
	if failed {
		t.Error("no match is not a failure")
	}
	if !strings.Contains(status, "no open pull request") {
		t.Errorf("status = %q, want it to say no pull request was found", status)
	}
}

func TestBuildsPKeyLookupFailureIsReportedAsAFailure(t *testing.T) {
	m := newBuilds(t)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated, _ := m.Update(buildPRMsg{Build: 9001, Err: errTest})
	m = updated.(*Builds)

	if _, failed := m.Status(); !failed {
		t.Error("a failed pull request lookup was not reported as a failure")
	}
}

func TestBuildsPKeyAnswerForABuildMovedAwayFromIsDropped(t *testing.T) {
	m := newBuilds(t)

	// Ask about 9001, then supersede it by asking about 9000 before the first
	// answer lands.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Builds)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(*Builds)

	// The stale answer for 9001 must not push anything now that 9000 is what
	// was actually asked about.
	updated, cmd := m.Update(buildPRMsg{
		Build: 9001,
		PR:    azdo.PullRequest{ID: 512, Source: "refs/heads/main"},
		Found: true,
	})
	m = updated.(*Builds)
	if cmd != nil {
		if _, ok := cmd().(PushMsg); ok {
			t.Fatal("a superseded lookup pushed a view for the build that is no longer being asked about")
		}
	}

	// 9000's own answer, arriving after, is the live request and must still
	// push — being superseded once must not leave the tracking wedged so
	// nothing ever pushes again.
	updated, cmd = m.Update(buildPRMsg{
		Build: 9000,
		PR:    azdo.PullRequest{ID: 700, Title: "Fix flaky retries", Source: "refs/heads/feature/x"},
		Found: true,
	})
	m = updated.(*Builds)
	if cmd == nil {
		t.Fatal("9000's own answer produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("9000's own answer produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "700") {
		t.Errorf("pushed view = %q, want the pull request 9000 was actually matched to", push.View.Title())
	}
}

func TestBuildsPKeyAnswerDoesNotPushOverLogsTheUserHasSinceOpened(t *testing.T) {
	m := newBuilds(t) // selected row is build 9001

	// Ask about 9001's pull request, then — before it answers — open 9001's
	// logs. Root would deliver the pending buildPRMsg to this view too, even
	// though Logs is what is actually on screen now.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Builds)
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	if _, ok := cmd().(PushMsg); !ok {
		t.Fatal("enter did not push the logs")
	}

	updated, cmd = m.Update(buildPRMsg{
		Build: 9001,
		PR:    azdo.PullRequest{ID: 512, Source: "refs/heads/main"},
		Found: true,
	})
	m = updated.(*Builds)
	if cmd != nil {
		if _, ok := cmd().(PushMsg); ok {
			t.Fatal("the pull request lookup pushed a view on top of the logs the user had since opened")
		}
	}

	// Once a key reaches this view again — proof Logs was popped and it is
	// back on top — it must resume acting on new lookups normally.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Builds)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(*Builds)
	updated, cmd = m.Update(buildPRMsg{
		Build: 9000,
		PR:    azdo.PullRequest{ID: 700, Title: "Fix flaky retries", Source: "refs/heads/feature/x"},
		Found: true,
	})
	m = updated.(*Builds)
	if cmd == nil {
		t.Fatal("a lookup made after returning to this view produced no command")
	}
	if _, ok := cmd().(PushMsg); !ok {
		t.Fatal("a lookup made after returning to this view should push normally")
	}
}

func TestBuildsEmptyStateSaysThereAreNoRuns(t *testing.T) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	m := NewBuilds(c)
	updated, _ := m.Update(buildsMsg{})
	m = updated.(*Builds)

	out := m.Body(160, 20)
	if !strings.Contains(out, "no pipeline runs") {
		t.Errorf("empty view is missing the message:\n%s", out)
	}
	if !strings.Contains(out, "⌒v⌒") {
		t.Errorf("empty view did not draw the gulls:\n%s", out)
	}
}

func TestBuildsForAPullRequestNamesItInTheTitle(t *testing.T) {
	c, builds := buildFixture()
	m := NewBuildsForPullRequest(c, 512, []int{9001, 9000})
	updated, _ := m.Update(gateRunsMsg{PR: 512, Builds: builds})
	m = updated.(*Builds)

	if !strings.Contains(m.Title(), "!512") {
		t.Errorf("title = %q, want it to name the pull request", m.Title())
	}
	if !strings.Contains(m.Title(), "(2)") {
		t.Errorf("title = %q, want the run count", m.Title())
	}
}

func TestBuildsForAPullRequestOpensOnTheGateRunsItWasGiven(t *testing.T) {
	c, _ := buildFixture()
	m := NewBuildsForPullRequest(c, 512, []int{9001})

	if got := m.gateBuilds; len(got) != 1 || got[0] != 9001 {
		t.Errorf("gate builds = %v, want the ids it was opened with", got)
	}
}

func TestBuildsForAPullRequestSaysSoWhenEmpty(t *testing.T) {
	c, _ := buildFixture()
	m := NewBuildsForPullRequest(c, 512, []int{9001})
	updated, _ := m.Update(gateRunsMsg{PR: 512})
	m = updated.(*Builds)

	if body := m.Body(120, 20); !strings.Contains(body, "!512") {
		t.Errorf("empty state = %q, want it to name the pull request", body)
	}
}

func TestBuildsForAPullRequestReportsItsOwnFetchFailing(t *testing.T) {
	c, _ := buildFixture()
	m := NewBuildsForPullRequest(c, 512, []int{9001})
	updated, _ := m.Update(gateRunsMsg{PR: 512, Err: errTest})
	m = updated.(*Builds)

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v, want the failure reported", status, isErr)
	}
}

// The gate list and the project list can both be alive in one stack — menu,
// builds, p, then B from the pull request it opened — and Root broadcasts
// data to every view in it. Each listing must land in exactly one of them.
func TestBuildsForAPullRequestIgnoresTheProjectListing(t *testing.T) {
	c, builds := buildFixture()
	m := NewBuildsForPullRequest(c, 512, []int{9001})
	updated, _ := m.Update(buildsMsg{Builds: builds})
	m = updated.(*Builds)

	if m.loaded {
		t.Errorf("the gate list took the project's recent runs as its own:\n%s", m.Body(120, 20))
	}
}

func TestBuildsIgnoresGateRunsThatAreNotItsOwn(t *testing.T) {
	c, builds := buildFixture()

	project := newBuilds(t)
	updated, _ := project.Update(gateRunsMsg{PR: 512, Builds: builds[:1]})
	project = updated.(*Builds)
	if project.browser.Len() != 2 {
		t.Errorf("the project list took a gate listing: %d rows, want its own 2", project.browser.Len())
	}

	gates := NewBuildsForPullRequest(c, 512, []int{9001})
	updated, _ = gates.Update(gateRunsMsg{PR: 999, Builds: builds})
	gates = updated.(*Builds)
	if gates.loaded {
		t.Error("the gate list took another pull request's gate runs")
	}
}

// The gate list is the one Builds instance Root does not build, so the
// property that makes a second instance safe is asserted here rather than
// assumed: every answer it reads back names the pull request it belongs to,
// including a failed one. An ErrMsg would name nobody and be read by the
// project list as its own fetch failing.
func TestGateRunsAnswerNamesThePullRequestEvenWhenTheFetchFails(t *testing.T) {
	_, builds := buildFixture()

	got := gateRuns(512, []int{9001, 9000}, func(id int) (azdo.Build, error) {
		for _, b := range builds {
			if b.ID == id {
				return b, nil
			}
		}
		return azdo.Build{}, errTest
	})
	if got.PR != 512 || got.Err != nil || len(got.Builds) != 2 {
		t.Fatalf("gateRuns = %+v, want !512's two runs and no error", got)
	}

	got = gateRuns(512, []int{7}, func(int) (azdo.Build, error) { return azdo.Build{}, errTest })
	if got.PR != 512 {
		t.Errorf("a failed answer names !%d, want !512", got.PR)
	}
	if got.Err == nil {
		t.Error("a failed fetch reported no error")
	}
	if got.Builds != nil {
		t.Errorf("a failed fetch returned %d runs, want none", len(got.Builds))
	}
}
