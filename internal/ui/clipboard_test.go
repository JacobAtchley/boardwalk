package ui

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// The candidate lists are pure functions of the operating system name and the
// environment, so the suite can check what every platform would run without
// being on it. Actually running any of them is the one thing these tests do
// not do: a test that shells out only proves what the machine running it
// happens to have.

// noEnv is a session with none of the variables the lists read.
func noEnv(string) string { return "" }

// envWith answers only the variables it was given, as a session with those set
// and nothing else would.
func envWith(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestClipboardCandidatesPerPlatform(t *testing.T) {
	x11 := [][]string{
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"wl-copy"},
	}
	wayland := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}

	for _, tc := range []struct {
		name string
		goos string
		env  func(string) string
		want [][]string
	}{
		{"macOS", "darwin", noEnv, [][]string{{"pbcopy"}}},
		// One candidate, not two: exec.LookPath resolves "clip" through
		// PATHEXT, so a machine without clip.exe has no clip either.
		{"Windows", "windows", noEnv, [][]string{{"clip.exe"}}},
		// wl-copy is on PATH on an X11 desktop with wl-clipboard installed,
		// and exits non-zero there because there is no Wayland server to
		// connect to. Whether the session is Wayland is what decides the
		// order, not what happens to be installed.
		{"X11", "linux", noEnv, x11},
		{"Wayland", "linux", envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}), wayland},
		{"other Unix", "freebsd", noEnv, x11},
		// Under WSL the Linux clipboard tools are usually absent and the
		// Windows one is on PATH, so it goes last rather than first: WSLg
		// sessions do have a working wl-copy.
		{"WSL", "linux", envWith(map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}),
			append(append([][]string{}, x11...), []string{"clip.exe"})},
	} {
		got := clipboardCandidates(tc.goos, tc.env)
		if !sameCommands(got, tc.want) {
			t.Errorf("%s: clipboardCandidates(%q) = %v, want %v", tc.name, tc.goos, got, tc.want)
		}
	}
}

func TestBrowserCandidatesPerPlatform(t *testing.T) {
	const url = "https://dev.azure.test/acme/_git/repo/pullrequest/512"

	for _, tc := range []struct {
		name string
		goos string
		env  func(string) string
		want [][]string
	}{
		{"macOS", "darwin", noEnv, [][]string{{"open", url}}},
		// rundll32 rather than "cmd /c start": start treats & in a URL as a
		// command separator, and every Azure DevOps query string has one.
		{"Windows", "windows", noEnv, [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}},
		{"Linux", "linux", noEnv, [][]string{{"xdg-open", url}}},
		// xdg-open under WSL opens the Linux side, where there is usually no
		// browser at all; wslview hands the URL to Windows.
		{"WSL", "linux", envWith(map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}),
			[][]string{{"wslview", url}, {"xdg-open", url}}},
	} {
		got := browserCandidates(tc.goos, url, tc.env)
		if !sameCommands(got, tc.want) {
			t.Errorf("%s: browserCandidates(%q) = %v, want %v", tc.name, tc.goos, got, tc.want)
		}
	}
}

func TestRunFirstAvailableReportsWhatIsMissing(t *testing.T) {
	// Nothing on the list exists, so the error has to name the commands that
	// would have worked — otherwise the status line says "could not copy to
	// the clipboard" and leaves the user with nothing to install.
	err := runFirstAvailable([][]string{
		{"boardwalk-no-such-command"},
		{"boardwalk-also-missing", "--flag"},
	}, "")
	if err == nil {
		t.Fatal("running a list of missing commands returned no error")
	}
	for _, want := range []string{"boardwalk-no-such-command", "boardwalk-also-missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q named in it", err, want)
		}
	}
	if strings.Contains(err.Error(), "--flag") {
		t.Errorf("error = %q, want the command names only, not their arguments", err)
	}
}

func TestRunFirstAvailableRunsTheFirstCommandOnPath(t *testing.T) {
	if err := runFirstAvailable([][]string{
		{"boardwalk-no-such-command"},
		exitCommand(0),
	}, "input"); err != nil {
		t.Errorf("skipping the missing command and running the next one failed: %v", err)
	}
}

func TestRunFirstAvailableReturnsTheErrorFromTheCommandItRan(t *testing.T) {
	// A command that exists but refuses is not the same as no command at all,
	// and the status line has to say so rather than telling the user to
	// install something they already have.
	err := runFirstAvailable([][]string{exitCommand(3)}, "")
	if err == nil {
		t.Fatal("a command that exited non-zero reported success")
	}
	if !errors.As(err, new(*exec.ExitError)) {
		t.Errorf("error = %q, want the command's own exit failure, not a missing-command message", err)
	}
}

// exitCommand is a command the machine running the suite really has, which
// exits with the code it is given. There is no one shell on every platform, so
// the two the runners do have stand in for each other.
func exitCommand(code int) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", fmt.Sprintf("exit %d", code)}
	}
	return []string{"sh", "-c", fmt.Sprintf("exit %d", code)}
}

func sameCommands(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if strings.Join(got[i], "\x00") != strings.Join(want[i], "\x00") {
			return false
		}
	}
	return true
}
