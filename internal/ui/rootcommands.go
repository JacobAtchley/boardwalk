package ui

import (
	"fmt"
	"maps"
	"slices"

	"github.com/JacobAtchley/boardwalk/internal/config"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// WithCommands binds the user's custom commands. Check them with
// CheckCommands first.
func WithCommands(cmds []config.Command) RootOption {
	return func(r *Root) { r.commands = cmds }
}

// listMovementKeys are the keys a list moves its cursor with. No view lists
// them in its help — they belong to bubbles/list — so the help-based
// collision check cannot see them, and Root sees a key before the view does.
func listMovementKeys() []string {
	km := list.DefaultKeyMap()
	var keys []string
	for _, b := range []key.Binding{km.CursorUp, km.CursorDown, km.PrevPage, km.NextPage, km.GoToStart, km.GoToEnd, km.Filter} {
		keys = append(keys, b.Keys()...)
	}
	return keys
}

// CheckCommands reports a command whose key cannot work: one that is not a
// key, one Root needs, one a list moves with, or the palette's.
func CheckCommands(cmds []config.Command, paletteKey string) error {
	if paletteKey == "" {
		paletteKey = DefaultPaletteKey
	}
	moves := listMovementKeys()
	for i, c := range cmds {
		switch {
		case !validKey(c.Key):
			return fmt.Errorf("commands[%d] key %q is not a key boardwalk recognises — try \"ctrl+r\"", i, c.Key)
		case slices.Contains(reservedKeys, c.Key):
			return fmt.Errorf("commands[%d] key %q is one boardwalk needs for itself", i, c.Key)
		case slices.Contains(moves, c.Key):
			return fmt.Errorf("commands[%d] key %q moves the cursor in lists", i, c.Key)
		case c.Key == paletteKey:
			return fmt.Errorf("commands[%d] key %q opens the command palette", i, c.Key)
		}
	}
	return nil
}

func validKey(k string) bool { _, ok := keyMsgFor(k); return ok }

// builtinKeys is every key the view on top answers to on its own: what its
// help lists, plus what Root and the shared actions claim on every view.
func builtinKeys(top View) map[string]bool {
	keys := map[string]bool{}
	add := func(bs ...key.Binding) {
		for _, b := range bs {
			for _, k := range b.Keys() {
				keys[k] = true
			}
		}
	}
	for _, g := range top.Keys().FullHelp() {
		add(g...)
	}
	add(sharedBindings()...)
	add(navBindings()...)
	add(keyRefresh)
	return keys
}

// activeCommands are the custom commands that run on the view on top: bound
// for its kind, on a key it does not use itself.
func (r *Root) activeCommands(top View) []config.Command {
	s, ok := top.(subjecter)
	if !ok || len(r.commands) == 0 {
		return nil
	}
	subj, _ := s.Subject()
	taken := builtinKeys(top)
	var out []config.Command
	for _, c := range r.commands {
		if c.On == subj.kind && !taken[c.Key] {
			out = append(out, c)
		}
	}
	return out
}

// commandKey runs the custom command bound to msg on the view on top, if
// there is one.
func (r *Root) commandKey(top View, msg tea.KeyMsg) (tea.Cmd, bool) {
	i := slices.IndexFunc(r.activeCommands(top), func(c config.Command) bool { return c.Key == msg.String() })
	if i < 0 {
		return nil, false
	}
	c := r.activeCommands(top)[i]

	subj, ok := top.(subjecter).Subject()
	if !ok {
		r.tell(top, StatusMsg{Text: "nothing selected"})
		return nil, true
	}

	env := r.commandEnv(subj)
	r.tell(top, StatusMsg{Text: fmt.Sprintf("running %q…", c.Name)})
	return func() tea.Msg {
		return commandDoneMsg{view: top, status: runShell(c.Name, c.Run, env)}
	}, true
}

// tell delivers a status to v wherever it sits in the stack, and does
// nothing if it is no longer there. It is called synchronously for "running…"
// rather than returned as a command: a command finishing faster than that
// message was delivered would have its result overwritten by it.
func (r *Root) tell(v View, s StatusMsg) {
	if i := slices.Index(r.stack, v); i >= 0 {
		r.stack[i], _ = v.Update(s)
	}
}

// commandEnv is the BOARDWALK_ variables for a subject, in a fixed order so
// a test — or a user reading `env` — sees the same thing every time.
func (r *Root) commandEnv(s subject) []string {
	env := []string{
		"BOARDWALK_KIND=" + s.kind,
		"BOARDWALK_ORG=" + r.client.Org,
		"BOARDWALK_PROJECT=" + r.client.Project,
	}
	for _, k := range slices.Sorted(maps.Keys(s.env)) {
		env = append(env, "BOARDWALK_"+k+"="+s.env[k])
	}
	return env
}

// commandDoneMsg is how a custom command turned out, addressed to the view
// it ran on. A plain StatusMsg would go to whatever is on top when it lands,
// which after two seconds may be somewhere else entirely.
type commandDoneMsg struct {
	view   View
	status StatusMsg
}

// keysFor is the view's bindings with the custom commands active on it
// added, which is what the help line, the "?" panel and the palette read.
func (r *Root) keysFor(top View) help.KeyMap {
	active := r.activeCommands(top)
	if len(active) == 0 {
		return top.Keys()
	}
	bindings := make([]key.Binding, len(active))
	for i, c := range active {
		bindings[i] = key.NewBinding(key.WithKeys(c.Key), key.WithHelp(shownKey(c.Key), c.Name))
	}
	return commandKeys{KeyMap: top.Keys(), commands: bindings}
}

// commandKeys adds a group of custom commands to a view's own bindings.
type commandKeys struct {
	help.KeyMap
	commands []key.Binding
}

func (k commandKeys) FullHelp() [][]key.Binding {
	groups := k.KeyMap.FullHelp()
	out := make([][]key.Binding, len(groups), len(groups)+1)
	copy(out, groups)
	return append(out, k.commands)
}
