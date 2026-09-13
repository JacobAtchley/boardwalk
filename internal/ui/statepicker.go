package ui

import (
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// statesFetchedMsg carries the states available for a work item type, for the
// picker S opens. It names the work item id the picker was opened for, not
// just the type: Root broadcasts data to every view in the stack, and the
// list and an item pushed from it can each have a picker open on a different
// item of the same type at once, so matching on the type alone would let one
// picker's fetch resolve the other's.
type statesFetchedMsg struct {
	ID     int
	States []azdo.WorkItemState
	Err    error
}

// statesCmd fetches the states for typ, off the UI goroutine. Client.States
// caches per type for the session, so every picker after the first one for a
// given type resolves without a network round trip.
func statesCmd(c *azdo.Client, id int, typ string) tea.Cmd {
	return func() tea.Msg {
		states, err := c.States(typ)
		return statesFetchedMsg{ID: id, States: states, Err: err}
	}
}

// statePicker is the small modal list S opens: the states a work item's type
// can move to. It is not a pushed view — Root's stack is for whole panes, and
// this is a handful of names on the status line — so it lives as a field on
// whichever view opened it, the same way the branch prompt in workitems.go
// and the reply prompt in pullrequestdetail.go hold their own modal state. It
// owns every key while open; esc cancels and enter picks.
type statePicker struct {
	itemID  int
	options []azdo.WorkItemState
	cursor  int

	// loading is true until the states fetch lands; err is set instead when
	// it fails, and the picker has nothing to offer.
	loading bool
	err     error
}

// newStatePicker opens the picker for a work item, loading until the states
// fetch for its type returns.
func newStatePicker(id int) *statePicker {
	return &statePicker{itemID: id, loading: true}
}

// resolve records the outcome of the states fetch this picker is waiting on.
func (p *statePicker) resolve(msg statesFetchedMsg) {
	p.loading = false
	p.err = msg.Err
	p.options = msg.States
}

// up and down move the cursor, wrapping the same way the landing menu's does.
func (p *statePicker) up() {
	if len(p.options) == 0 {
		return
	}
	p.cursor = (p.cursor - 1 + len(p.options)) % len(p.options)
}

func (p *statePicker) down() {
	if len(p.options) == 0 {
		return
	}
	p.cursor = (p.cursor + 1) % len(p.options)
}

// selected is the state under the cursor, or false when there is nothing
// loaded to pick from yet.
func (p *statePicker) selected() (string, bool) {
	if p.cursor < 0 || p.cursor >= len(p.options) {
		return "", false
	}
	return p.options[p.cursor].Name, true
}

// View renders the picker in the status line's slot, the same slot the branch
// and reply prompts render into while they are open.
func (p *statePicker) View() string {
	switch {
	case p.err != nil:
		return errStyle.Render("could not load the states: " + p.err.Error())
	case p.loading:
		return chromeStyle.Render("loading states…")
	case len(p.options) == 0:
		return chromeStyle.Render("no states offered for this work item type")
	}

	names := make([]string, len(p.options))
	for i, s := range p.options {
		if i == p.cursor {
			names[i] = selectedRow.Render("▸ " + s.Name)
			continue
		}
		names[i] = normalRow.Render(s.Name)
	}
	return "set state:  " + strings.Join(names, "   ") + chromeStyle.Render("   (enter picks · esc cancels)")
}
