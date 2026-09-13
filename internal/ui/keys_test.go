package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// helpLine is what a view's footer advertises, flattened so a test can assert a
// key is offered without depending on how the help component spaces it.
func helpLine(k help.KeyMap) string {
	var b strings.Builder
	for _, bind := range k.ShortHelp() {
		h := bind.Help()
		fmt.Fprintf(&b, "%s %s · ", h.Key, h.Desc)
	}
	return b.String()
}

// helpPanel is the same for the full panel behind "?".
func helpPanel(k help.KeyMap) string {
	var b strings.Builder
	for _, group := range k.FullHelp() {
		for _, bind := range group {
			h := bind.Help()
			fmt.Fprintf(&b, "%s %s · ", h.Key, h.Desc)
		}
	}
	return b.String()
}

func TestEveryBindingCarriesHelpText(t *testing.T) {
	// A binding with no help text renders as a gap in the footer, which is how
	// a key ends up working without ever being advertised.
	for name, b := range map[string]key.Binding{
		"filter": keyFilter, "copy id": keyCopyID, "slack": keySlack,
		"open": keyOpen, "detail up": keyDetailUp, "detail down": keyDetailDown,
		"refresh": keyRefresh, "back": keyBack, "quit": keyQuit, "help": keyHelp,
		"scope": keyScope, "active": keyActive, "assign": keyAssign, "set state": keyState, "branch": keyBranch,
		"drafts": keyDrafts, "logs": keyLogs, "top": keyTop, "bottom": keyBottom,
	} {
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("%s: help = %+v, want both a key and a description", name, h)
		}
		if len(b.Keys()) == 0 {
			t.Errorf("%s: binding matches no keys", name)
		}
	}
}

func TestListKeysPutsTheViewsOwnKeysFirst(t *testing.T) {
	k := listKeys(keyDrafts, keyScope)

	line := helpLine(k)
	if !strings.HasPrefix(line, "d drafts") {
		t.Errorf("short help = %q, want the view's own keys leading", line)
	}
	for _, want := range []string{"d drafts", "^t scope", "/ filter", "r refresh", "esc back", "? keys"} {
		if !strings.Contains(line, want) {
			t.Errorf("short help = %q, missing %q", line, want)
		}
	}
}

func TestTheFullPanelCarriesKeysTheShortLineOmits(t *testing.T) {
	// The point of the panel is the keys that do not fit on one line.
	k := listKeys(keyLogs)

	line, panel := helpLine(k), helpPanel(k)
	for _, want := range []string{"y copy id", "s copy slack link", "o open in browser", "^u detail up", "^d detail down", "q quit"} {
		if !strings.Contains(panel, want) {
			t.Errorf("full panel = %q, missing %q", panel, want)
		}
	}
	if strings.Contains(line, "detail up") {
		t.Error("the short line carries a key meant for the panel; it will not fit")
	}
}
