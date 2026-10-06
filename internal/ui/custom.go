package ui

import (
	"fmt"
	"io"
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
//
// Stderr goes to a temp file, not a pipe. A pipe's read end is held by
// boardwalk: when boardwalk quits it closes, and a command still running dies
// of SIGPIPE on its next write, so it would not outlive boardwalk. A script
// that backgrounds a child also leaves that child holding the pipe open, and
// Wait blocks until the child exits. A file has neither problem. It is
// unlinked at once where the platform allows, so it can't be left behind.
func startShell(name, script string, env []string) StatusMsg {
	cmd := shellCommand(script)
	cmd.Env = append(os.Environ(), env...)

	f, err := os.CreateTemp("", "boardwalk-cmd-*")
	if err != nil {
		return StatusMsg{Text: fmt.Sprintf("could not run %q: %v", name, err), Err: true}
	}
	// Windows refuses to remove an open file, so that is retried on close.
	_ = os.Remove(f.Name())
	defer func() {
		f.Close()
		_ = os.Remove(f.Name())
	}()
	cmd.Stderr = f

	if err := cmd.Start(); err != nil {
		return StatusMsg{Text: fmt.Sprintf("could not run %q: %v", name, err), Err: true}
	}

	// Wait runs on its own goroutine so a command still going after
	// commandWait is reaped when it does finish, rather than left a zombie.
	// The child has its own descriptor, so closing ours on the timeout path
	// does not disturb it.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return StatusMsg{Text: fmt.Sprintf("ran %q", name)}
		}
		if line := stderrTail(f); line != "" {
			return StatusMsg{Text: fmt.Sprintf("%q failed: %s", name, line), Err: true}
		}
		return StatusMsg{Text: fmt.Sprintf("%q failed: %v", name, err), Err: true}
	case <-time.After(commandWait):
		return StatusMsg{Text: fmt.Sprintf("started %q", name)}
	}
}

// tailLimit bounds how much of a command's stderr is read back.
const tailLimit = 4096

// stderrTail is the last non-blank line among the last tailLimit bytes of f.
// Only that line is ever shown, and a chatty command must not cost boardwalk
// more than a bounded read.
func stderrTail(f *os.File) string {
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return ""
	}
	buf := make([]byte, min(end, tailLimit))
	n, _ := f.ReadAt(buf, end-int64(len(buf)))
	lines := strings.Split(strings.TrimSpace(string(buf[:n])), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
