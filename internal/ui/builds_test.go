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
