package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// The copy and open actions are the only part of boardwalk that has to know
// what it is running on: everything else is Go, the Azure DevOps REST API and
// the az CLI, all three of which are the same everywhere.
//
// Both actions are expressed as a list of candidate commands rather than one
// command per platform. Linux is why: a desktop has wl-copy under Wayland,
// xclip or xsel under X11, and no way to know which from inside a terminal, so
// the list is tried in order and the first one on PATH wins. The lists are
// pure functions of the operating system name so the tests can read every
// platform's list on whichever platform they happen to run.

// clipboardCandidates are the commands that read what to copy from stdin.
func clipboardCandidates(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		// clip.exe is the real name; plain clip is what a shell on PATHEXT
		// resolves, and both are here because a Go exec.LookPath under WSL or
		// a stripped-down image can find one without the other.
		return [][]string{{"clip.exe"}, {"clip"}}
	default:
		return [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}
}

// browserCandidates are the commands that hand a URL to the default browser.
func browserCandidates(goos, url string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"open", url}}
	case "windows":
		// Not "cmd /c start": start reads & as a command separator, and an
		// Azure DevOps URL carries one in every query string. rundll32 takes
		// the URL as a single argument and never re-parses it.
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		return [][]string{{"xdg-open", url}}
	}
}

// lookup finds the first candidate that is on PATH. It returns the candidates'
// command names alongside the miss so the caller can say what to install: a
// status line reading "could not copy to the clipboard" and nothing else
// leaves the user with no next step.
func lookup(candidates [][]string) (*exec.Cmd, error) {
	var names []string
	for _, argv := range candidates {
		names = append(names, argv[0])
		if _, err := exec.LookPath(argv[0]); err == nil {
			return exec.Command(argv[0], argv[1:]...), nil
		}
	}
	return nil, fmt.Errorf("none of these are installed: %s", strings.Join(names, ", "))
}

// runFirstAvailable runs the first candidate that is on PATH, feeding it stdin,
// and waits for it to finish.
//
// A missing command and a command that refuses are deliberately different
// errors. The first is something the user can fix by installing a package, and
// the message says which; the second is the command's own complaint, and
// dressing it up as a missing tool would send them to install what they have.
func runFirstAvailable(candidates [][]string, stdin string) error {
	cmd, err := lookup(candidates)
	if err != nil {
		return err
	}
	// Always a reader, even an empty one: pbcopy with no stdin attached reads
	// the terminal boardwalk is drawing on, and hangs.
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// startFirstAvailable starts the first candidate on PATH without waiting for
// it. Opening a browser is the caller: xdg-open can stay in the foreground for
// as long as the browser it launched lives, and waiting for that would freeze
// the interface until the user closed their browser.
func startFirstAvailable(candidates [][]string) error {
	cmd, err := lookup(candidates)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// CopyToClipboard puts s on the system clipboard.
func CopyToClipboard(s string) error {
	return runFirstAvailable(clipboardCandidates(runtime.GOOS), s)
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	return startFirstAvailable(browserCandidates(runtime.GOOS, url))
}
