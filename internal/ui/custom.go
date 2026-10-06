package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// commandWait is how long a custom command is watched before boardwalk stops
// waiting and reports it as started. A command like "open a terminal tab"
// finishes well inside it, so its real outcome is what the status line says;
// one that keeps running is left to run. A variable so tests can shorten it.
var commandWait = 2 * time.Second

// runShell runs a custom command, indirected so Root's tests can see what
// would have run without running it.
var runShell = startShell

// startShell runs script detached from the terminal and reports what became
// of it within commandWait.
//
// Nothing the command prints may reach the terminal: bubbletea is drawing on
// it, and a stray line would tear the screen until the next repaint. So
// stdin is /dev/null, stdout is dropped, and stderr is kept — its last line
// is the reason a failing script gives.
func startShell(name, script string, env []string) StatusMsg {
	cmd := shellCommand(script)
	cmd.Env = append(os.Environ(), env...)
	var stderr tailBuffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return StatusMsg{Text: fmt.Sprintf("could not run %q: %v", name, err), Err: true}
	}

	// Wait runs on its own goroutine so a command still going after
	// commandWait is reaped when it does finish, rather than left a zombie.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return StatusMsg{Text: fmt.Sprintf("ran %q", name)}
		}
		if line := stderr.lastLine(); line != "" {
			return StatusMsg{Text: fmt.Sprintf("%q failed: %s", name, line), Err: true}
		}
		return StatusMsg{Text: fmt.Sprintf("%q failed: %v", name, err), Err: true}
	case <-time.After(commandWait):
		return StatusMsg{Text: fmt.Sprintf("started %q", name)}
	}
}

// tailLimit bounds what tailBuffer keeps of a command's stderr.
const tailLimit = 4096

// tailBuffer keeps the end of what is written to it. Only the last line is
// ever shown, and a chatty command must not grow boardwalk without bound.
//
// It is written by exec's copying goroutine and read only after Wait has
// returned, which orders the two, so it needs no lock.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > tailLimit {
		t.b = append(t.b[:0], t.b[len(t.b)-tailLimit:]...)
	}
	return len(p), nil
}

// lastLine is the last non-blank line written.
func (t *tailBuffer) lastLine() string {
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
