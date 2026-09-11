package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func newLogs(t *testing.T, status azdo.BuildStatus) *Logs {
	t.Helper()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	build := azdo.Build{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: status}
	m := NewLogs(c, build, []azdo.Record{{Name: "Build", Type: "Task", Order: 1, LogID: 7}})
	m.Body(120, 20)
	return m
}

func TestLogsRendersChunksWithTaskRules(t *testing.T) {
	m := newLogs(t, azdo.StatusSucceeded)

	updated, _ := m.Update(logChunksMsg{
		Status: azdo.StatusSucceeded,
		Chunks: []azdo.LogChunk{
			{Task: "Restore", LogID: 6, Lines: []string{"restoring"}},
			{Task: "Build", LogID: 7, Lines: []string{"compiling", "done"}},
		},
	})
	m = updated.(*Logs)

	view := m.Body(120, 20)
	for _, want := range []string{"Restore", "restoring", "Build", "compiling", "done"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestLogsAppendsWithoutRepeatingTheRule(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, _ := m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"first"}}}})
	m = updated.(*Logs)
	updated, _ = m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"second"}}}})
	m = updated.(*Logs)

	view := m.Body(120, 40)
	if strings.Count(view, "── Build ──") != 1 {
		t.Errorf("the task rule was repeated on an append:\n%s", view)
	}
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Errorf("appended lines are missing:\n%s", view)
	}
}

func TestLogsKeepsTailingWhileTheBuildRuns(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	_, cmd := m.Update(logChunksMsg{Status: azdo.StatusRunning})
	if cmd == nil {
		t.Fatal("a running build did not schedule another poll")
	}
}

func TestLogsStopsTailingWhenTheBuildFinishes(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, cmd := m.Update(logChunksMsg{Status: azdo.StatusSucceeded})
	m = updated.(*Logs)

	if cmd != nil {
		t.Error("a finished build scheduled another poll")
	}
	if !strings.Contains(m.Title(), "succeeded") {
		t.Errorf("title = %q, want the final status", m.Title())
	}
	if !strings.Contains(m.Hints(), "r refresh") {
		t.Errorf("hints = %q, want refresh offered once tailing stops", m.Hints())
	}
}

func TestLogsTickFetches(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	_, cmd := m.Update(tailTickMsg{})
	if cmd == nil {
		t.Error("a tick did not fetch")
	}
}

func TestLogsEscapePops(t *testing.T) {
	m := newLogs(t, azdo.StatusSucceeded)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced no command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("escape produced %T, want a PopMsg", cmd())
	}
}

func TestLogsRDoesNotDuplicateAnOutstandingFetch(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)
	m.fetching = true // as if a tick's fetch were already in flight

	_, cmd := m.Update(runes("r"))
	if cmd != nil {
		t.Error("r started a second fetch while one was already outstanding — " +
			"two fetches running at once would race on the cursor map")
	}
}

func TestLogsFetchingClearsOnErrorSoTheNextTickFetches(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)
	m.fetching = true // as if fetch() were already outstanding

	updated, _ := m.Update(logChunksMsg{Err: errTest})
	m = updated.(*Logs)

	_, cmd := m.Update(tailTickMsg{})
	if cmd == nil {
		t.Error("the fetching guard was not cleared on a failed poll, blocking the next tick")
	}
}

func TestNeedsTimelineRefresh(t *testing.T) {
	cases := []struct {
		name    string
		status  azdo.BuildStatus
		records []azdo.Record
		want    bool
	}{
		{"running, timeline already loaded", azdo.StatusRunning, []azdo.Record{{Name: "Build"}}, true},
		{"running, no timeline yet", azdo.StatusRunning, nil, true},
		{"done, timeline already loaded", azdo.StatusSucceeded, []azdo.Record{{Name: "Build"}}, false},
		{"done, timeline never loaded", azdo.StatusSucceeded, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsTimelineRefresh(c.status, c.records); got != c.want {
				t.Errorf("needsTimelineRefresh(%v, %v) = %v, want %v", c.status, c.records, got, c.want)
			}
		})
	}
}

func TestLogsOnADoneBuildWithNoRecordsCatchesUpInsteadOfStickingOnThePlaceholder(t *testing.T) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	build := azdo.Build{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: azdo.StatusSucceeded}
	// enter can beat the build view's own lazy timeline fetch, landing here
	// with nothing to read yet.
	m := NewLogs(c, build, nil)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("a done build with no records did not fetch — it would be stuck on the placeholder forever")
	}
	if got := m.Body(120, 20); !strings.Contains(got, "fetching the build log…") {
		t.Errorf("expected the placeholder before any chunks land, got:\n%s", got)
	}

	// Simulate that fetch landing: the timeline caught up and the log has text.
	updated, _ := m.Update(logChunksMsg{
		Status:  azdo.StatusSucceeded,
		Records: []azdo.Record{{Name: "Build", Type: "Task", Order: 1, LogID: 7}},
		Chunks:  []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"compiled"}}},
	})
	m = updated.(*Logs)

	if got := m.Body(120, 20); !strings.Contains(got, "compiled") {
		t.Errorf("the pane stayed on the placeholder after the catch-up fetch landed:\n%s", got)
	}
}

func TestLogsErrorIsReportedWithoutLosingTheText(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, _ := m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"compiling"}}}})
	m = updated.(*Logs)
	updated, _ = m.Update(logChunksMsg{Err: errTest})
	m = updated.(*Logs)

	if _, isErr := m.Status(); !isErr {
		t.Error("a log fetch failure was not reported")
	}
	if !strings.Contains(m.Body(120, 20), "compiling") {
		t.Error("the log text was lost when a poll failed")
	}
}
