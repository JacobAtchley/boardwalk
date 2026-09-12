package ui

import "github.com/charmbracelet/bubbles/key"

// The key bindings live here rather than beside the code that acts on them so
// that a binding and the help text describing it cannot drift apart. They did
// once already: the detail-pane scroll keys worked for a whole branch without
// appearing in the README's key table, because the table was prose maintained
// by hand.
var (
	keyFilter     = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	keyCopyID     = key.NewBinding(key.WithKeys("y", "ctrl+y"), key.WithHelp("y", "copy id"))
	keySlack      = key.NewBinding(key.WithKeys("s", "ctrl+s"), key.WithHelp("s", "copy slack link"))
	keyOpen       = key.NewBinding(key.WithKeys("o", "ctrl+o"), key.WithHelp("o", "open in browser"))
	keyDetailUp   = key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "detail up"))
	keyDetailDown = key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^d", "detail down"))
	keyRefresh    = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	keyBack       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keyQuit       = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	keyHelp       = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys"))

	keyScope  = key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "scope"))
	keyActive = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "set active"))
	keyBranch = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch"))
	keyDrafts = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "drafts"))
	keyLogs   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "logs"))
	keyItem   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open item"))
	keyTop    = key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top"))
	keyBottom = key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom"))
)

// sharedBindings are the keys every list view carries, in the order the short
// help line reads them.
func sharedBindings() []key.Binding {
	return []key.Binding{keyFilter, keyCopyID, keySlack, keyOpen}
}

// navBindings are the keys Root owns on a view's behalf.
func navBindings() []key.Binding {
	return []key.Binding{keyDetailUp, keyDetailDown, keyBack, keyQuit, keyHelp}
}

// keyMap adapts a view's bindings to what the help component wants: a short
// line for the footer and grouped columns for the panel behind "?".
type keyMap struct {
	short  []key.Binding
	groups [][]key.Binding
}

func (k keyMap) ShortHelp() []key.Binding  { return k.short }
func (k keyMap) FullHelp() [][]key.Binding { return k.groups }

// listKeys builds the key map for a list view from the bindings it adds on top
// of the shared set. own is shown first, because it is what distinguishes this
// view from its siblings.
func listKeys(own ...key.Binding) keyMap {
	// back stays on the short line rather than moving to the panel: it is the
	// only way out of a view, and a user who cannot see it has to guess.
	short := append(append([]key.Binding{}, own...), keyFilter, keyRefresh, keyBack, keyHelp)
	return keyMap{
		short: short,
		groups: [][]key.Binding{
			append(append([]key.Binding{}, own...), keyRefresh),
			sharedBindings(),
			navBindings(),
		},
	}
}
