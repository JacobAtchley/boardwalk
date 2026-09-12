package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestWorkIsIdleUntilSomethingIsIssued(t *testing.T) {
	w := newWork()

	if w.busy() {
		t.Error("a fresh work tracker reported itself busy")
	}
	if w.View() != "" {
		t.Errorf("View = %q, want nothing while idle", w.View())
	}
}

func TestWorkStaysBusyUntilEveryBatchedCallLands(t *testing.T) {
	// A view fans out one fetch per visible row. The spinner has to survive
	// until the last of them answers, not stop at the first.
	w := newWork()

	if cmd := w.begin(3); cmd == nil {
		t.Fatal("going from idle to busy did not start the spinner ticking")
	}
	for i := range 2 {
		w.done()
		if !w.busy() {
			t.Fatalf("stopped being busy after %d of 3 calls landed", i+1)
		}
	}

	w.done()
	if w.busy() {
		t.Error("still busy after every call landed")
	}
	if w.View() != "" {
		t.Errorf("View = %q, want nothing once idle", w.View())
	}
}

func TestWorkDoesNotStartASecondTickChain(t *testing.T) {
	// Two chains would advance the frame twice per interval and spin visibly
	// faster, the same way two poll chains doubled the log pane's fetch rate.
	w := newWork()
	w.begin(1)

	if cmd := w.begin(1); cmd != nil {
		t.Error("a second batch started another tick chain while one was running")
	}
}

func TestWorkIgnoresBeginWithNothingToDo(t *testing.T) {
	w := newWork()

	if cmd := w.begin(0); cmd != nil {
		t.Error("begin(0) started the spinner")
	}
	if w.busy() {
		t.Error("begin(0) made the tracker busy")
	}
}

func TestWorkSurvivesMoreCompletionsThanCalls(t *testing.T) {
	// Every view decrements on its own messages and on the generic ErrMsg,
	// which it may not have caused. Going negative would leave the spinner
	// stuck on forever once the counts drifted.
	w := newWork()
	w.begin(1)
	w.done()
	w.done()

	if w.busy() {
		t.Error("an extra completion pushed the counter below zero")
	}
	if cmd := w.begin(1); cmd == nil {
		t.Error("the tracker did not restart cleanly after over-completing")
	}
}

func TestWorkTicksOnlyWhileBusy(t *testing.T) {
	w := newWork()
	w.begin(1)

	if cmd := w.tick(spinner.TickMsg{ID: w.spinner.ID()}); cmd == nil {
		t.Error("a tick while busy did not schedule the next frame")
	}
	if w.View() == "" {
		t.Error("View is empty while busy")
	}

	w.done()
	if cmd := w.tick(spinner.TickMsg{ID: w.spinner.ID()}); cmd != nil {
		t.Error("a tick after the last call landed kept the chain alive")
	}
}

func TestWorkIgnoresAnotherSpinnersTick(t *testing.T) {
	// Root broadcasts data messages to every view in the stack, so a tick
	// belonging to one view's spinner reaches all of them.
	w := newWork()
	w.begin(1)
	before := w.View()

	w.tick(spinner.TickMsg{ID: w.spinner.ID() + 1})

	if w.View() != before {
		t.Error("a tick from another view's spinner advanced this one")
	}
}
