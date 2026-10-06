package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// needSh skips a test that runs a real shell script on a platform without sh.
func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the runner tests drive sh")
	}
}

func shortWait(t *testing.T, d time.Duration) {
	t.Helper()
	before := commandWait
	commandWait = d
	t.Cleanup(func() { commandWait = before })
}

func TestStartShellPassesTheEnvironment(t *testing.T) {
	needSh(t)
	out := filepath.Join(t.TempDir(), "env")

	got := startShell("dump", `env > "$OUT"`, []string{"OUT=" + out, "BOARDWALK_ID=4021"})

	if got.Err || got.Text != `ran "dump"` {
		t.Fatalf("status = %+v", got)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "BOARDWALK_ID=4021\n") {
		t.Errorf("the command did not see BOARDWALK_ID:\n%s", body)
	}
	if !strings.Contains(string(body), "PATH=") {
		t.Error("the command did not inherit boardwalk's environment")
	}
}

func TestStartShellReportsFailure(t *testing.T) {
	needSh(t)
	for _, tc := range []struct{ script, want string }{
		{`echo "no such tab" >&2; exit 3`, `"go" failed: no such tab`},
		{`exit 3`, `"go" failed: exit status 3`},
	} {
		got := startShell("go", tc.script, nil)
		if !got.Err || got.Text != tc.want {
			t.Errorf("%s: status = %+v, want error %q", tc.script, got, tc.want)
		}
	}
}

func TestStartShellReportsOnlyTheLastStderrLine(t *testing.T) {
	needSh(t)
	// Ten thousand lines of noise and then the reason: the status line has
	// room for one, and the last is the one a failing script ends on.
	got := startShell("noisy", `i=0; while [ $i -lt 10000 ]; do echo "noise $i" >&2; i=$((i+1)); done; echo "the reason" >&2; exit 1`, nil)
	if got.Text != `"noisy" failed: the reason` {
		t.Errorf("status = %q", got.Text)
	}
}

func TestStartShellStopsWaitingOnALongCommand(t *testing.T) {
	needSh(t)
	shortWait(t, 100*time.Millisecond)

	start := time.Now()
	got := startShell("slow", "sleep 5", nil)

	if got.Err || got.Text != `started "slow"` {
		t.Errorf("status = %+v", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the runner waited on the command instead of detaching")
	}
}

func TestStartShellDoesNotWaitOnABackgroundChild(t *testing.T) {
	needSh(t)
	shortWait(t, 5*time.Second)

	// The script exits at once but leaves a child holding stderr. With a pipe
	// for stderr, Wait would block until the child let go.
	start := time.Now()
	got := startShell("bg", "sleep 2 >&2 &", nil)

	if got.Err || got.Text != `ran "bg"` {
		t.Errorf("status = %+v", got)
	}
	if time.Since(start) > time.Second {
		t.Errorf("waited %v on a backgrounded child", time.Since(start))
	}
}

func TestStartShellNeverEvaluatesValues(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	// A title is written by somebody else. Quoted as a variable it is data.
	got := startShell("t", `printf '%s' "$BOARDWALK_TITLE" > /dev/null`,
		[]string{"BOARDWALK_TITLE=$(touch " + filepath.Join(dir, "pwned") + ")"})
	if got.Err {
		t.Fatalf("status = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("a value was run as shell")
	}
}

func TestStderrTailKeepsTheEnd(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"empty", "", ""},
		{"blank only", "\n  \n", ""},
		{"last non-blank line", "first\nlast\n\n", "last"},
		{"a long run is cut to the cap", strings.Repeat(strings.Repeat("x", 100)+"\n", 100) + "last\n\n", "last"},
		{"a single line longer than the cap", strings.Repeat("y", 3*tailLimit), strings.Repeat("y", tailLimit)},
	}
	for _, tc := range tests {
		f, err := os.CreateTemp(t.TempDir(), "tail")
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(tc.content)
		if got := stderrTail(f); got != tc.want {
			t.Errorf("%s: stderrTail = %q, want %q", tc.name, got, tc.want)
		}
		f.Close()
	}
}
