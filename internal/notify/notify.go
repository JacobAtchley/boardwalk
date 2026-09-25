// Package notify puts a message on the desktop.
//
// It is built the way the clipboard and browser actions in internal/ui are: a
// pure list of candidate commands per platform, tried in order, the first one
// on PATH winning. That keeps every platform's behaviour readable from a test
// on whichever machine the test runs.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Send posts a desktop notification.
func Send(title, message string) error {
	return run(candidates(runtime.GOOS, title, message), exec.LookPath)
}

// candidates are the commands that can post the notification, best first.
//
// On macOS terminal-notifier goes ahead of osascript when it is installed: a
// notification posted through osascript belongs to Script Editor, which is
// what it is listed under in System Settings and what clicking it opens.
func candidates(goos, title, message string) [][]string {
	switch goos {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s",
			appleString(message), appleString(title))
		return [][]string{
			{"terminal-notifier", "-title", title, "-message", message, "-group", "boardwalk"},
			{"osascript", "-e", script},
		}
	case "windows":
		return nil
	default:
		return [][]string{{"notify-send", "-a", "boardwalk", title, message}}
	}
}

// run runs the first candidate on PATH and waits for it. A missing command and
// a command that refuses are different errors: the first names what to
// install, the second is the command's own complaint.
func run(candidates [][]string, lookPath func(string) (string, error)) error {
	if len(candidates) == 0 {
		return fmt.Errorf("desktop notifications are not supported on %s", runtime.GOOS)
	}
	var names []string
	for _, argv := range candidates {
		names = append(names, argv[0])
		if _, err := lookPath(argv[0]); err != nil {
			continue
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if err != nil && len(out) > 0 {
			return fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
		}
		return err
	}
	return fmt.Errorf("none of these are installed: %s", strings.Join(names, ", "))
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
