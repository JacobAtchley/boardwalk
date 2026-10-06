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

func TestTailBufferKeepsTheEnd(t *testing.T) {
	var b tailBuffer
	for range 100 {
		b.Write([]byte(strings.Repeat("x", 100) + "\n"))
	}
	b.Write([]byte("last\n\n"))
	if len(b.b) > tailLimit {
		t.Errorf("buffer grew to %d bytes", len(b.b))
	}
	if b.lastLine() != "last" {
		t.Errorf("lastLine = %q", b.lastLine())
	}
}
