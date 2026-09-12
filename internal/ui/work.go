package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// work tracks the background fetches a view has outstanding and spins while any
// of them are in flight.
//
// The count is a tally rather than a flag because the views fan out: the pull
// request view asks for a screenful of comment threads at once, and the build
// view for a screenful of timelines. A flag would clear on the first answer and
// leave the rest arriving under a still screen.
type work struct {
	spinner spinner.Model
	pending int
}

func newWork() work {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = chromeStyle
	return work{spinner: s}
}

// begin records n newly issued fetches, returning a command only when the
// tracker was idle — a second chain would advance the frame twice per interval
// and visibly spin faster.
func (w *work) begin(n int) tea.Cmd {
	if n <= 0 {
		return nil
	}

	idle := w.pending == 0
	w.pending += n
	if idle {
		return w.spinner.Tick
	}
	return nil
}

// done records one fetch answering, whether it succeeded or failed. It floors
// at zero: a view decrements on its own messages and on the generic ErrMsg,
// which it may not have caused, and a negative tally would leave the spinner
// running forever once the counts drifted.
func (w *work) done() {
	if w.pending > 0 {
		w.pending--
	}
}

// tick advances the spinner, and schedules the next frame only while something
// is still outstanding. Returning nil is what stops the chain.
func (w *work) tick(msg spinner.TickMsg) tea.Cmd {
	if w.pending == 0 {
		return nil
	}

	var cmd tea.Cmd
	w.spinner, cmd = w.spinner.Update(msg)
	return cmd
}

// busy reports whether anything is still in flight.
func (w work) busy() bool { return w.pending > 0 }

// View is the spinner frame, or nothing at all when idle, so a caller can
// concatenate it unconditionally.
func (w work) View() string {
	if w.pending == 0 {
		return ""
	}
	return w.spinner.View()
}
