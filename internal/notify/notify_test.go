package notify

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCandidatesOnMacPreferTerminalNotifier(t *testing.T) {
	got := candidates("darwin", "boardwalk", "api #12")
	if len(got) != 2 || got[0][0] != "terminal-notifier" || got[1][0] != "osascript" {
		t.Fatalf("candidates = %v, want terminal-notifier then osascript", got)
	}
	want := []string{"terminal-notifier", "-title", "boardwalk", "-message", "api #12", "-group", "boardwalk"}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("terminal-notifier argv = %v, want %v", got[0], want)
	}
}

func TestCandidatesOnMacEscapeTheScript(t *testing.T) {
	script := candidates("darwin", `say "hi"`, `a\b "c"`)[1][2]
	want := `display notification "a\\b \"c\"" with title "say \"hi\""`
	if script != want {
		t.Errorf("script = %s, want %s", script, want)
	}
}

func TestCandidatesOnLinuxUseNotifySend(t *testing.T) {
	got := candidates("linux", "t", "m")
	want := [][]string{{"notify-send", "-a", "boardwalk", "t", "m"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestCandidatesOnWindowsAreNone(t *testing.T) {
	if got := candidates("windows", "t", "m"); len(got) != 0 {
		t.Errorf("candidates = %v, want none", got)
	}
}

func TestRunNamesWhatToInstall(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	err := run(candidates("darwin", "t", "m"), missing)
	if err == nil || !strings.Contains(err.Error(), "terminal-notifier, osascript") {
		t.Errorf("err = %v, want it to name both commands", err)
	}
}

func TestRunWithNoCandidatesSaysUnsupported(t *testing.T) {
	err := run(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("err = %v, want not supported", err)
	}
}
