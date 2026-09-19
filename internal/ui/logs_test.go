package ui

import (
	"fmt"
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
	if !strings.Contains(helpLine(m.Keys()), "r refresh") {
		t.Errorf("help = %q, want refresh offered once tailing stops", helpLine(m.Keys()))
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

// stampedLogs is a pane holding one task's worth of log with the timestamp
// prefix Azure Pipelines writes on every line.
func stampedLogs(t *testing.T, lines ...string) *Logs {
	t.Helper()
	m := newLogs(t, azdo.StatusSucceeded)
	updated, _ := m.Update(logChunksMsg{
		Status: azdo.StatusSucceeded,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: lines}},
	})
	return updated.(*Logs)
}

const logStamp = "2026-09-18T13:49:02.1234567Z"

func TestLogsHidesTheTimestampPrefixUntilItIsAskedFor(t *testing.T) {
	m := stampedLogs(t, logStamp+" compiling")

	if view := m.Body(120, 20); strings.Contains(view, logStamp) {
		t.Errorf("the timestamp prefix is shown by default:\n%s", view)
	} else if !strings.Contains(view, "compiling") {
		t.Errorf("the line's text went with its timestamp:\n%s", view)
	}
}

func TestLogsTogglesTheTimestampPrefix(t *testing.T) {
	m := stampedLogs(t, logStamp+" compiling")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(*Logs)
	if view := m.Body(120, 20); !strings.Contains(view, logStamp) {
		t.Errorf("t did not show the timestamp prefix:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(*Logs)
	if view := m.Body(120, 20); strings.Contains(view, logStamp) {
		t.Errorf("a second t did not hide the timestamp prefix again:\n%s", view)
	}
}

// TestLogsKeepsTheTaskRulesAcrossAToggle — the toggle rewrites the whole
// buffer, so the rules between one task's output and the next have to be
// replayed rather than lost.
func TestLogsKeepsTheTaskRulesAcrossAToggle(t *testing.T) {
	m := newLogs(t, azdo.StatusSucceeded)
	updated, _ := m.Update(logChunksMsg{
		Status: azdo.StatusSucceeded,
		Chunks: []azdo.LogChunk{
			{Task: "Restore", LogID: 6, Lines: []string{logStamp + " restoring"}},
			{Task: "Build", LogID: 7, Lines: []string{logStamp + " compiling"}},
		},
	})
	m = updated.(*Logs)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(*Logs)

	view := m.Body(120, 40)
	for _, want := range []string{"── Restore ──", "── Build ──", "restoring", "compiling"} {
		if !strings.Contains(view, want) {
			t.Errorf("the toggle lost %q:\n%s", want, view)
		}
	}
}

// TestLogsKeepsItsPlaceAcrossAToggle — the toggle is for reading the log you
// are already looking at, so it must not throw you back to the top.
func TestLogsKeepsItsPlaceAcrossAToggle(t *testing.T) {
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s line %d", logStamp, i)
	}
	m := stampedLogs(t, lines...)
	m.Body(120, 10)

	m.viewport.SetYOffset(120)
	before := m.viewport.YOffset

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(*Logs)
	m.Body(120, 10)

	if m.viewport.YOffset != before {
		t.Errorf("the toggle moved the pane from line %d to line %d", before, m.viewport.YOffset)
	}
}

// TestLogsStripsTheAzureMarkers — "##[error]" is Azure Pipelines telling
// boardwalk what the line is, not text the reader needs to see.
func TestLogsStripsTheAzureMarkers(t *testing.T) {
	m := stampedLogs(t,
		logStamp+" ##[section]Starting: Build",
		logStamp+" ##[error]Bash exited with code 1.",
		logStamp+" ##[endgroup]",
	)

	view := m.Body(120, 20)
	if strings.Contains(view, "##[") {
		t.Errorf("a marker was left in the rendered log:\n%s", view)
	}
	for _, want := range []string{"Starting: Build", "Bash exited with code 1."} {
		if !strings.Contains(view, want) {
			t.Errorf("the marker took %q with it:\n%s", want, view)
		}
	}
}
