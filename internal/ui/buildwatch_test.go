package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

type sent struct{ title, body string }

// fakeWatch is a watch whose server answers from builds and whose desktop
// records what it was told.
func fakeWatch(builds map[int]azdo.Build, records []azdo.Record) (*buildWatch, *[]sent) {
	var got []sent
	w := newBuildWatch(nil)
	w.fetch = func(id int) (azdo.Build, error) {
		b, ok := builds[id]
		if !ok {
			return azdo.Build{}, errors.New("boom")
		}
		return b, nil
	}
	w.timeline = func(int) (azdo.Progress, []azdo.Record, error) { return azdo.Progress{}, records, nil }
	w.send = func(title, body string) error {
		got = append(got, sent{title, body})
		return nil
	}
	return w, &got
}

func running(id int) azdo.Build {
	return azdo.Build{ID: id, Pipeline: "api-ci", Number: "20260924.1", SourceBranch: "refs/heads/main", Status: azdo.StatusRunning}
}

// pass runs one poll to completion and applies it, the way Root would.
func pass(t *testing.T, w *buildWatch) (StatusMsg, tea.Cmd) {
	t.Helper()
	cmd := w.poll()
	if cmd == nil {
		t.Fatal("poll returned no command with builds watched")
	}
	return w.apply(cmd().(buildWatchResultMsg))
}

func TestWatchedRunningBuildStaysWatchedAndSendsNothing(t *testing.T) {
	b := running(1)
	w, got := fakeWatch(map[int]azdo.Build{1: b}, nil)
	if _, cmd := w.toggle(b); cmd == nil {
		t.Fatal("first watch did not start the tick")
	}
	_, next := pass(t, w)
	if len(*got) != 0 {
		t.Errorf("sent %v for a running build", *got)
	}
	if next == nil || len(w.watched) != 1 {
		t.Errorf("running build dropped or tick stopped: watched=%d next=%v", len(w.watched), next != nil)
	}
}

func TestFinishedBuildNotifiesOnceAndIsDropped(t *testing.T) {
	b := running(1)
	builds := map[int]azdo.Build{1: b}
	w, got := fakeWatch(builds, nil)
	w.toggle(b)

	done := b
	done.Status = azdo.StatusSucceeded
	builds[1] = done
	status, next := pass(t, w)

	if len(*got) != 1 || (*got)[0].title != "boardwalk · build succeeded" {
		t.Fatalf("sent %v, want one success", *got)
	}
	if !strings.Contains((*got)[0].body, "api-ci #20260924.1 · main") {
		t.Errorf("body = %q", (*got)[0].body)
	}
	if status.Text != "✓ api-ci #20260924.1 succeeded" || status.Err {
		t.Errorf("status = %+v", status)
	}
	if len(w.watched) != 0 || next != nil || w.ticking {
		t.Errorf("finished build still watched: watched=%d next=%v ticking=%v", len(w.watched), next != nil, w.ticking)
	}
}

func TestFailureNamesTheFailedTask(t *testing.T) {
	b := running(1)
	failed := b
	failed.Status = azdo.StatusFailed
	records := []azdo.Record{{Name: "Run tests", Type: "Task", Result: "failed", Issues: []azdo.Issue{{Type: "error", Message: "3 tests failed"}}}}
	w, got := fakeWatch(map[int]azdo.Build{1: failed}, records)
	w.watched[1] = b

	status, _ := pass(t, w)
	if len(*got) != 1 || !strings.Contains((*got)[0].body, "failed at Run tests: 3 tests failed") {
		t.Fatalf("sent %v, want the failed task named", *got)
	}
	if status.Text != "✗ api-ci #20260924.1 failed at Run tests" {
		t.Errorf("status = %q", status.Text)
	}
}

func TestWatchingTwiceStops(t *testing.T) {
	b := running(1)
	w, _ := fakeWatch(nil, nil)
	w.toggle(b)
	status, _ := w.toggle(b)
	if len(w.watched) != 0 || !strings.HasPrefix(status.Text, "stopped watching") {
		t.Errorf("second press: watched=%d status=%q", len(w.watched), status.Text)
	}
}

func TestAFinishedBuildIsNotWatched(t *testing.T) {
	b := running(1)
	b.Status = azdo.StatusSucceeded
	w, _ := fakeWatch(nil, nil)
	status, cmd := w.toggle(b)
	if len(w.watched) != 0 || cmd != nil || !strings.Contains(status.Text, "already succeeded") {
		t.Errorf("watched=%d cmd=%v status=%q", len(w.watched), cmd != nil, status.Text)
	}
}

func TestAFailedCheckKeepsTheWatch(t *testing.T) {
	w, got := fakeWatch(map[int]azdo.Build{}, nil)
	w.watched[1] = running(1)
	w.ticking = true
	status, next := pass(t, w)
	if len(w.watched) != 1 || next == nil || !status.Err || len(*got) != 0 {
		t.Errorf("watched=%d next=%v status=%+v sent=%v", len(w.watched), next != nil, status, *got)
	}
}

func TestANotifierFailureIsReported(t *testing.T) {
	b := running(1)
	b.Status = azdo.StatusCanceled
	w, _ := fakeWatch(map[int]azdo.Build{1: b}, nil)
	w.send = func(string, string) error { return errors.New("none of these are installed: notify-send") }
	w.watched[1] = running(1)
	status, _ := pass(t, w)
	if !status.Err || !strings.Contains(status.Text, "could not notify: none of these are installed") {
		t.Errorf("status = %+v", status)
	}
}

// The key on the build list and on the log pane both reach Root, and the
// header says a watch is running wherever the user has gone since.
func TestWatchKeyReachesRootFromBuildsAndLogs(t *testing.T) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	b := running(7)

	logs := NewLogs(c, b, nil)
	_, cmd := logs.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("log pane sent nothing for a")
	}
	if msg, ok := cmd().(WatchBuildMsg); !ok || msg.Build.ID != 7 {
		t.Fatalf("log pane sent %T, want WatchBuildMsg for 7", cmd())
	}

	builds := NewBuilds(c)
	builds.Update(buildsMsg{Builds: []azdo.Build{b}})
	_, cmd = builds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	var got tea.Msg
	if cmd != nil {
		got = cmd()
	}
	if msg, ok := got.(WatchBuildMsg); !ok || msg.Build.ID != 7 {
		t.Fatalf("build list sent %T, want WatchBuildMsg for 7", got)
	}

	root := NewRoot(c, false, false, "")
	root.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	root.Update(PushMsg{View: builds})
	root.Update(WatchBuildMsg{Build: b})
	if title := strings.SplitN(root.View(), "\n", 2)[0]; !strings.Contains(title, "watching 1") {
		t.Errorf("header = %q, want it to count the watch", title)
	}
}
