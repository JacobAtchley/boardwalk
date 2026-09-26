package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DefaultPaletteKey opens the command palette when the config names no other.
// ctrl+p is what most editors use for "find anything", and no view binds it.
const DefaultPaletteKey = "ctrl+p"

// HistoryLimit is how many palette entries are remembered. It is more than
// fit on screen, so a view's recent actions survive a detour through others.
const HistoryLimit = 20

// History is what the palette remembers having run, most recent first.
// Root depends on this rather than on a file so a test can hand it one in
// memory, and so where history lives is main's decision.
type History interface {
	Recent() []string
	// Record remembers id. A failure is ignored by the palette: history is a
	// convenience, and must never cost the user the action they asked for.
	Record(id string) error
}

// RootOption configures NewRoot.
type RootOption func(*Root)

// WithPaletteKey sets the key that opens the command palette. Check it with
// CheckPaletteKey first.
func WithPaletteKey(k string) RootOption {
	return func(r *Root) { r.paletteKey = paletteBinding(k) }
}

// WithHistory sets where the palette remembers what it ran. Without it,
// history lasts as long as the session.
func WithHistory(h History) RootOption {
	return func(r *Root) { r.history = h }
}

// reservedKeys are the keys Root cannot give to the palette: the ways out of
// a view, the help panel, and the menu's own movement.
var reservedKeys = []string{"esc", "q", "?", "ctrl+c", "enter", "up", "down", "j", "k"}

// CheckPaletteKey reports whether k can open the palette: whether it names a
// key at all, and whether it is one Root already needs for itself.
func CheckPaletteKey(k string) error {
	if _, ok := keyMsgFor(k); !ok {
		return fmt.Errorf("paletteKey %q is not a key boardwalk recognises — try %q or \"ctrl+k\"", k, DefaultPaletteKey)
	}
	if slices.Contains(reservedKeys, k) {
		return fmt.Errorf("paletteKey %q is one boardwalk needs for itself", k)
	}
	return nil
}

// paletteBinding is the binding for the palette key, with its help spelled
// the way the rest of the footer spells control keys: ^p, not ctrl+p.
func paletteBinding(k string) key.Binding {
	shown := k
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok && len(rest) == 1 {
		shown = "^" + rest
	}
	return key.NewBinding(key.WithKeys(k), key.WithHelp(shown, "commands"))
}

// goToMsg asks Root to show one of the main views, from however deep the
// stack is.
type goToMsg struct{ name string }

// goToTargets are the menu entries the palette offers to go to.
var goToTargets = []string{"items", "prs", "builds"}

// goTo shows the named view. If the stack is already rooted in it, the stack
// is cut back to it, keeping what it loaded and where its cursor was;
// otherwise the stack is replaced with a freshly built one.
func (r *Root) goTo(name string) tea.Cmd {
	if len(r.stack) > 0 && r.base == name {
		r.truncate(1)
		return nil
	}
	v := r.build(name)
	if v == nil {
		return nil
	}
	r.truncate(0)
	r.stack = append(r.stack, v)
	r.base = name
	return initialise(v)
}

// truncate cuts the stack to n views, clearing the dropped slots so the views
// in them can be collected rather than pinned by the backing array.
func (r *Root) truncate(n int) {
	clear(r.stack[n:])
	r.stack = r.stack[:n]
}

// openPalette opens the palette over whatever is on screen.
func (r *Root) openPalette() {
	r.palette = newPalette(r.paletteKey, r.paletteEntries(), r.history.Recent())
}

// paletteEntries is everything the palette offers on this screen: the main
// views to go to, then every binding the view on top shows in its "?" panel.
// Taking them from the panel means the palette and the panel cannot disagree
// about what the view can do.
func (r *Root) paletteEntries() []paletteEntry {
	entries := make([]paletteEntry, 0, 32)
	for _, e := range menuEntries {
		if slices.Contains(goToTargets, e.key) {
			entries = append(entries, paletteEntry{
				id: "nav:" + e.key, title: "go to " + e.label, hint: "go to", msg: goToMsg{e.key},
			})
		}
	}

	top, ok := r.top()
	if !ok {
		return entries
	}

	seen := make(map[string]struct{}, 32)
	for _, group := range top.Keys().FullHelp() {
		for _, b := range group {
			h := b.Help()
			if !b.Enabled() || h.Desc == "" || len(b.Keys()) == 0 {
				continue
			}
			// "?" would open a panel listing these same entries.
			first := b.Keys()[0]
			if slices.Contains(keyHelp.Keys(), first) {
				continue
			}
			id := "act:" + h.Desc
			if _, dup := seen[id]; dup {
				continue
			}
			msg, ok := keyMsgFor(first)
			if !ok {
				continue
			}
			seen[id] = struct{}{}
			entries = append(entries, paletteEntry{id: id, title: h.Desc, hint: h.Key, msg: msg})
		}
	}
	return entries
}

// paletteKeyPress hands a key to the open palette, and runs what it chose.
// Running an entry sends its message back through Update, so an action goes
// through exactly the path its key would have.
func (r *Root) paletteKeyPress(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}

	res, e := r.palette.Update(msg)
	switch res {
	case paletteClosed:
		r.palette = nil
	case paletteChosen:
		r.palette = nil
		// Written synchronously: the file is a few hundred bytes, and a
		// write racing the next palette open's read would be worse.
		_ = r.history.Record(e.id)
		_, cmd := r.Update(e.msg)
		return cmd
	}
	return nil
}

// paletteFooter replaces the view's key hints while the palette has the keys.
const paletteFooter = "↑↓ move · enter run · esc close"

// paletteFrame is the usual chrome with the palette where the body goes. The
// view's own body is not drawn under it: lipgloss has no compositing, and a
// palette over a half-hidden list reads worse than a palette on its own.
func (r *Root) paletteFrame(top View) string {
	height := max(1, r.height-3)
	body := lipgloss.Place(r.width, height, lipgloss.Center, lipgloss.Top,
		r.palette.View(r.width, height, top.Title()))

	status, isErr := top.Status()
	style := statusStyle
	if isErr {
		style = errStyle
	}
	return strings.Join([]string{
		chromeStyle.Render(truncate(top.Title(), r.width)),
		body,
		chromeStyle.Render(truncate(paletteFooter, r.width)),
		style.Render(truncate(status, r.width)),
	}, "\n")
}

// paletteKeys adds the palette key to a view's own, so the "?" panel lists
// it without every view having to know it exists or what it is bound to.
type paletteKeys struct {
	help.KeyMap
	palette key.Binding
}

func withPalette(k help.KeyMap, palette key.Binding) help.KeyMap {
	return paletteKeys{KeyMap: k, palette: palette}
}

func (k paletteKeys) FullHelp() [][]key.Binding {
	groups := k.KeyMap.FullHelp()
	out := make([][]key.Binding, len(groups), len(groups)+1)
	copy(out, groups)
	return append(out, []key.Binding{k.palette})
}
