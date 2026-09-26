package ui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// namedKeys maps a key name as bubbletea spells it — "enter", "ctrl+t" — back
// to its KeyType. bubbletea keeps its own table private, so this one is built
// by asking every KeyType for its name. It is built once, on the first palette
// open, rather than at program start, since most sessions never need it.
var (
	namedKeysOnce sync.Once
	namedKeys     map[string]tea.KeyType
)

// keyTypeRange bounds the KeyType values asked for their names: the control
// characters run 0–127 and the named keys count down from -1.
const keyTypeRange = 256

func buildNamedKeys() {
	namedKeys = make(map[string]tea.KeyType, 128)
	for t := tea.KeyType(-keyTypeRange); t < 128; t++ {
		if t == tea.KeyRunes {
			continue
		}
		if name := (tea.Key{Type: t}).String(); name != "" {
			namedKeys[name] = t
		}
	}
}

// keyMsgFor is the key message that a binding's key string describes — the
// inverse of tea.KeyMsg.String. The palette runs an action by replaying it, so
// the view acting on it cannot tell the palette from the key.
func keyMsgFor(s string) (tea.KeyMsg, bool) {
	alt := false
	if rest, ok := strings.CutPrefix(s, "alt+"); ok && rest != "" {
		alt, s = true, rest
	}

	namedKeysOnce.Do(buildNamedKeys)
	if t, ok := namedKeys[s]; ok {
		return tea.KeyMsg{Type: t, Alt: alt}, true
	}
	if r := []rune(s); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}
