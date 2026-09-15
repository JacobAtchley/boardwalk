package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// The candidate lists are pure functions of the operating system name so the
// suite can check what every platform would run without being on it. Actually
// running any of them is the one thing these tests do not do: a test that
// shells out only proves what the machine running it happens to have.

func TestClipboardCandidatesPerPlatform(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want [][]string
	}{
		{"darwin", [][]string{{"pbcopy"}}},
		{"windows", [][]string{{"clip.exe"}, {"clip"}}},
		{"linux", [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}},
		{"freebsd", [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}},
	} {
		got := clipboardCandidates(tc.goos)
		if !sameCommands(got, tc.want) {
			t.Errorf("clipboardCandidates(%q) = %v, want %v", tc.goos, got, tc.want)
		}
	}
}

func TestBrowserCandidatesPerPlatform(t *testing.T) {
	const url = "https://dev.azure.test/acme/_git/repo/pullrequest/512"

	for _, tc := range []struct {
		goos string
		want [][]string
	}{
		{"darwin", [][]string{{"open", url}}},
		// rundll32 rather than "cmd /c start": start treats & in a URL as a
		// command separator, and every Azure DevOps query string has one.
		{"windows", [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}},
		{"linux", [][]string{{"xdg-open", url}}},
	} {
		got := browserCandidates(tc.goos, url)
		if !sameCommands(got, tc.want) {
			t.Errorf("browserCandidates(%q) = %v, want %v", tc.goos, got, tc.want)
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
	if strings.Contains(err.Error(), "no ") {
		t.Errorf("error = %q, want the command's own failure, not a missing-command message", err)
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
