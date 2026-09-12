package ui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

// View is one screen inside boardwalk. Root owns the header, the help line and
// the status line, so a view only renders its own body — which keeps the chrome
// identical everywhere and in one place.
type View interface {
	// Update handles a message and returns the view to carry on with. It
	// returns View rather than tea.Model so Root can hold the stack without
	// type assertions.
	Update(tea.Msg) (View, tea.Cmd)

	// Body renders the view at the size Root has left for it.
	Body(width, height int) string

	// Title is the header line, for example "pull requests (42) · acme/Platform".
	Title() string

	// Keys are the view's bindings. Root renders them as the footer's short
	// help line, and as the panel behind "?". Returning bindings rather than a
	// prose hint line keeps what the footer claims and what the keys do from
	// drifting apart.
	Keys() help.KeyMap

	// Status is the transient message under the hints, and whether it is an
	// error, which decides its colour.
	Status() (string, bool)
}

// PushMsg asks Root to open a view on top of the current one, for example a
// build's logs.
type PushMsg struct{ View View }

// PopMsg asks Root to return to the view underneath.
type PopMsg struct{}

// StatusMsg sets the status line from inside a command.
type StatusMsg struct {
	Text string
	Err  bool
}

// ErrMsg is a failed fetch. Root renders it on the status line and the view
// keeps whatever data it already had.
type ErrMsg struct{ Err error }
