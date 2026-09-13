package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func bugStates() []azdo.WorkItemState {
	return []azdo.WorkItemState{{Name: "New"}, {Name: "Active"}, {Name: "Resolved"}, {Name: "Closed"}}
}

// --- WorkItems: the list ---

func TestWorkItemsStatePickerOpensLoadingThenShowsOptions(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	m, cmd := press(t, m, runes("S"))
	if cmd == nil {
		t.Fatal("S did not fetch the states")
	}
	if m.statePicker == nil {
		t.Fatal("S did not open the picker")
	}
	if status, _ := m.Status(); !strings.Contains(status, "loading") {
		t.Errorf("status = %q, want it to say it is loading", status)
	}
	if !m.Prompting() {
		t.Error("Prompting is false while the picker is open")
	}

	updated, _ := m.Update(statesFetchedMsg{ID: 4021, States: bugStates()})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr {
		t.Error("a successful fetch reported as an error")
	}
	for _, want := range []string{"New", "Active", "Resolved", "Closed"} {
		if !strings.Contains(status, want) {
			t.Errorf("status = %q, missing state %q", status, want)
		}
	}
}

func TestWorkItemsStatePickerIgnoresStatesForAnotherItem(t *testing.T) {
	// Root broadcasts data to every view in the stack; a slow fetch for a
	// picker that was since cancelled, or opened on a different row, must not
	// land on this one.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m, _ = press(t, m, runes("S"))

	updated, _ := m.Update(statesFetchedMsg{ID: 9999, States: bugStates()})
	m = updated.(*WorkItems)

	status, _ := m.Status()
	if !strings.Contains(status, "loading") {
		t.Errorf("status = %q, want the picker still loading", status)
	}
}

func TestWorkItemsStatePickerCancelWithEscape(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m, _ = press(t, m, runes("S"))

	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling the picker started work anyway")
	}
	if m.statePicker != nil {
		t.Error("escape did not close the picker")
	}
	if m.Prompting() {
		t.Error("Prompting still true after the picker was cancelled")
	}
}

func TestWorkItemsStatePickerEnterPicksTheHighlightedState(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m, _ = press(t, m, runes("S"))
	updated, _ := m.Update(statesFetchedMsg{ID: 4021, States: bugStates()})
	m = updated.(*WorkItems)

	// Cursor starts on the first option; move down once to land on "Active".
	m, _ = press(t, m, runes("j"))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.statePicker != nil {
		t.Error("picking a state did not close the picker")
	}
	if cmd == nil {
		t.Fatal("enter did not start the state change")
	}
	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "Active") {
		t.Errorf("status = %q, isErr = %v, want it naming the picked state", status, isErr)
	}
}

func TestWorkItemsStatePickerSwallowsActionKeysWhileOpen(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m, _ = press(t, m, runes("S"))

	m, cmd := press(t, m, runes("a"))
	if cmd != nil {
		t.Error("a keystroke reached the view's own bindings while the picker was open")
	}
	if m.statePicker == nil {
		t.Error("the picker closed on a key that was not esc or enter")
	}
}

func TestWorkItemsRejectedTransitionReportsTheServerMessage(t *testing.T) {
	// The state picker reuses stateSetMsg, so a transition Azure DevOps
	// refuses has to surface its own message rather than a generic failure.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	before := rowLine(t, m, 4021)

	updated, _ := m.Update(stateSetMsg{ID: 4021, State: "Closed", Err: errTest})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v, want the server's own message reported", status, isErr)
	}
	if got := rowLine(t, m, 4021); got != before {
		t.Errorf("row for #4021 changed despite the rejected transition: %q, was %q", got, before)
	}
}

// --- Item: the full item ---

func TestItemStatePickerOpensAndPicks(t *testing.T) {
	m := newItem(t, nil)
	r := drive(t, m, 100, 40)

	cmd := r.send(runes("S"))
	if cmd == nil {
		t.Fatal("S did not fetch the states")
	}
	if !m.Prompting() {
		t.Error("Prompting is false while the picker is open")
	}

	r.send(statesFetchedMsg{ID: 4021, States: bugStates()})
	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "Active") {
		t.Errorf("status = %q, isErr = %v, want the fetched states listed", status, isErr)
	}

	r.send(runes("j")) // New -> Active
	cmd = r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not start the state change")
	}
	if m.statePicker != nil {
		t.Error("picking a state did not close the picker")
	}
	if m.Prompting() {
		t.Error("Prompting still true after a state was picked")
	}
}

func TestItemStatePickerEscapeCancels(t *testing.T) {
	m := newItem(t, nil)
	r := drive(t, m, 100, 40)
	r.send(runes("S"))

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("escape produced a command — it should only have closed the picker, not popped the view")
	}
	if m.statePicker != nil {
		t.Error("escape did not close the picker")
	}
}

func TestItemSyncsItsFieldOnASuccessfulStateChange(t *testing.T) {
	m := newItem(t, nil)
	r := drive(t, m, 100, 40)

	r.send(stateSetMsg{ID: 4021, State: "Resolved"})

	if m.item.State != "Resolved" {
		t.Errorf("item.State = %q, want Resolved", m.item.State)
	}
	if !strings.Contains(r.frame(), "Resolved") {
		t.Error("the rendered state field did not pick up the change")
	}
	if _, isErr := m.Status(); isErr {
		t.Error("a successful state change reported as an error")
	}
}

func TestItemRejectedTransitionReportsTheServerMessageWithoutBreakingTheDiscussion(t *testing.T) {
	m := newItem(t, []azdo.Comment{{Author: "Dev", Text: "already loaded"}})
	r := drive(t, m, 100, 40)

	r.send(stateSetMsg{ID: 4021, State: "Closed", Err: errTest})

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v, want the server's own message reported", status, isErr)
	}
	// stateErr is a field of its own precisely so a rejected transition does
	// not make the discussion section — which loaded fine — render as failed.
	if strings.Contains(r.frame(), "could not load the discussion") {
		t.Error("an unrelated state error made the already-loaded discussion look broken")
	}
	if m.item.State != "Active" {
		t.Errorf("item.State = %q, want it left alone by the rejected transition", m.item.State)
	}
}

func TestItemStatePickerIgnoresStatesForAnotherItem(t *testing.T) {
	m := newItem(t, nil)
	r := drive(t, m, 100, 40)
	r.send(runes("S"))

	r.send(statesFetchedMsg{ID: 9999, States: bugStates()})
	status, _ := m.Status()
	if !strings.Contains(status, "loading") {
		t.Errorf("status = %q, want the picker still loading", status)
	}
}
