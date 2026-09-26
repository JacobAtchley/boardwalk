package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyMsgForRoundTripsEveryKeyTheViewsBind(t *testing.T) {
	// The palette runs an action by replaying its binding's first key, so a
	// key that does not come back as the same string would be an entry that
	// silently does nothing.
	for _, s := range []string{
		"enter", "esc", "up", "down", "tab", " ", "ctrl+t", "ctrl+u", "ctrl+d",
		"ctrl+y", "ctrl+p", "alt+x", "k", "S", "?", "/", ":",
	} {
		msg, ok := keyMsgFor(s)
		if !ok {
			t.Errorf("keyMsgFor(%q) failed", s)
			continue
		}
		if got := msg.String(); got != s {
			t.Errorf("keyMsgFor(%q).String() = %q", s, got)
		}
	}
}

func TestKeyMsgForMakesPrintableKeysRunes(t *testing.T) {
	msg, _ := keyMsgFor("b")
	if msg.Type != tea.KeyRunes {
		t.Errorf("keyMsgFor(b).Type = %v, want runes", msg.Type)
	}
}

func TestKeyMsgForRejectsUnknownNames(t *testing.T) {
	for _, s := range []string{"", "ctrl+banana", "alt+", "hello"} {
		if _, ok := keyMsgFor(s); ok {
			t.Errorf("keyMsgFor(%q) succeeded", s)
		}
	}
}
