package ui

import (
	"fmt"
	"os"
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
// xclip or xsel under X11, and the two are routinely both installed, so the
// list is tried in order and the first one on PATH wins.
//
// Being on PATH is not the same as working, which is why the order is read off
// the environment rather than fixed. wl-copy on an X11 desktop exits with
// "failed to connect to a Wayland server" — a real failure the user can do
// nothing about, on a machine where copying works fine through xclip. So the
// session decides which goes first, and the other stays on the list as a
// fallback rather than being dropped.
//
// Both lists are pure functions of the operating system name and a lookup of
// the environment, so the tests can read any platform's list, in any kind of
// session, on whichever machine they happen to run.

// wsl reports whether this is a Linux session hosted by Windows, where the
// Linux tools are usually absent and the Windows ones are on PATH.
func wsl(env func(string) string) bool {
	return env("WSL_DISTRO_NAME") != "" || env("WSLENV") != ""
}

// clipboardCandidates are the commands that read what to copy from stdin.
func clipboardCandidates(goos string, env func(string) string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		// Only clip.exe: exec.LookPath on Windows resolves a bare "clip"
		// through PATHEXT, so a machine that has no clip.exe has no clip
		// either and a second candidate could never fire.
		return [][]string{{"clip.exe"}}
	default:
		x11 := [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		list := append([][]string{{"wl-copy"}}, x11...)
		if env("WAYLAND_DISPLAY") == "" {
			list = append(x11, []string{"wl-copy"})
		}
		if wsl(env) {
			// Last, not first: a WSLg session has a working wl-copy, and the
			// Windows clipboard is the fallback for the sessions that do not.
			list = append(list, []string{"clip.exe"})
		}
		return list
	}
}

// browserCandidates are the commands that hand a URL to the default browser.
func browserCandidates(goos, url string, env func(string) string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"open", url}}
	case "windows":
		// Not "cmd /c start": start reads & as a command separator, and an
		// Azure DevOps URL carries one in every query string. rundll32 takes
		// the URL as a single argument and never re-parses it.
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		if wsl(env) {
			// xdg-open here opens the Linux side, which usually has no browser
			// installed at all. wslview hands the URL to Windows.
			return [][]string{{"wslview", url}, {"xdg-open", url}}
		}
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
//
// The wait still has to happen somewhere, on its own goroutine: Go never reaps
// a process it started until something calls Wait, and boardwalk is a
// long-lived program, so every o would otherwise leave a defunct child behind
// for as long as the session lasted.
func startFirstAvailable(candidates [][]string) error {
	cmd, err := lookup(candidates)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// CopyToClipboard puts s on the system clipboard.
func CopyToClipboard(s string) error {
	return runFirstAvailable(clipboardCandidates(runtime.GOOS, os.Getenv), s)
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	return startFirstAvailable(browserCandidates(runtime.GOOS, url, os.Getenv))
}
