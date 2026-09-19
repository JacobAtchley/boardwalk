package ui

import (
	"fmt"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Starting and stopping pipeline runs lives here rather than in builds.go,
// the way the draft toggle lives in draft.go: the three actions share one arm
// type, one set of refusals and one status vocabulary, and splitting them
// across the view's key switch is how they would drift.
//
// All three arm rather than firing outright, for the reason the draft toggle
// does: they are not private acts. A queued run takes an agent off the pool
// and tells whoever watches the pipeline that something is building; a cancel
// stops work somebody may be waiting on. Neither can be taken back by
// pressing the key again.

// buildAction is one of the two things a single keystroke can ask for. A
// fresh queue is not here: it asks for a branch first, and the prompt it
// opens is itself the deliberate act an arm would otherwise supply.
type buildAction int

const (
	// actionRerun queues the selected run's definition again, against the
	// same branch it ran on. That is what "re-run" means, and it is why it
	// does not ask: the answer is already on the row.
	actionRerun buildAction = iota
	// actionCancel stops a run that has not finished.
	actionCancel
)

// armedBuild is an action waiting on its confirming keystroke. It captures
// everything the confirm needs, because the list underneath can be rebuilt by
// a refresh landing while the arm is up — the same reason repoPicker captures
// its work item and branch.
type armedBuild struct {
	action buildAction
	// buildID is the run to cancel; definitionID and branch are what a
	// re-run or a queue is sent against. Only one pair is ever read, chosen
	// by action.
	buildID      int
	definitionID int
	branch       string
	// label is how the run is named on the status line: "platform-ci
	// #20260911.3", not a bare id.
	label string
}

// buildActionBindings pairs each key with the action it takes — the one place
// that mapping lives, so arming and confirming cannot disagree about what a
// key does. It mirrors voteBindings in pullrequestdetail.go.
var buildActionBindings = []struct {
	binding key.Binding
	action  buildAction
}{
	{keyRerun, actionRerun},
	{keyCancelBuild, actionCancel},
}

// matchBuildAction reports which action, if any, msg asks for.
func matchBuildAction(msg tea.KeyMsg) (buildAction, bool) {
	for _, ba := range buildActionBindings {
		if key.Matches(msg, ba.binding) {
			return ba.action, true
		}
	}
	return 0, false
}

// buildQueuedMsg carries the outcome of queueing a run, whether that was a
// re-run or a fresh queue. Build is what the server made, so the status line
// can name the run rather than only say that something was started.
type buildQueuedMsg struct {
	Build azdo.Build
	Err   error
}

// buildCancelledMsg carries the outcome of a cancel.
type buildCancelledMsg struct {
	Build int
	Err   error
}

// queueBuildCmd starts a run off the UI goroutine.
func queueBuildCmd(c *azdo.Client, definitionID int, branch string) tea.Cmd {
	return func() tea.Msg {
		b, err := c.QueueBuild(definitionID, branch)
		if err != nil {
			return buildQueuedMsg{Err: fmt.Errorf("could not queue the run: %w", err)}
		}
		return buildQueuedMsg{Build: b}
	}
}

// cancelBuildCmd stops a run off the UI goroutine.
func cancelBuildCmd(c *azdo.Client, id int) tea.Cmd {
	return func() tea.Msg {
		if err := c.CancelBuild(id); err != nil {
			return buildCancelledMsg{Build: id, Err: fmt.Errorf("could not cancel build %d: %w", id, err)}
		}
		return buildCancelledMsg{Build: id}
	}
}

// buildLabel names a run the way the status line should: the pipeline and its
// run number, falling back to the id for a run that has neither.
func buildLabel(b azdo.Build) string {
	if b.Pipeline == "" && b.Number == "" {
		return fmt.Sprintf("build %d", b.ID)
	}
	return fmt.Sprintf("%s #%s", b.Pipeline, b.Number)
}

// armBuildAction describes the arm a run's current state calls for, with the
// status line text saying what a second press will do — or nothing to arm and
// the reason, when the action does not apply.
//
// The refusals are the ones knowable without asking the server, which is the
// precedent armDraft and armVote set: a check that only happens at the
// network call hides the real reason behind an error code. Permissions are
// the part boardwalk cannot see coming, and those it leaves to the server.
func armBuildAction(a buildAction, b azdo.Build) (*armedBuild, string) {
	label := buildLabel(b)

	if a == actionCancel {
		if b.Status.Done() {
			return nil, fmt.Sprintf("%s has already %s — there is nothing to cancel", label, b.Status)
		}
		return &armedBuild{action: a, buildID: b.ID, label: label},
			fmt.Sprintf("press again to cancel %s — esc cancels", label)
	}

	if b.DefinitionID == 0 {
		// Every run the list shows carries one, so this is a server that
		// answered without a definition rather than anything the reader did.
		// Saying so beats a request that cannot work.
		return nil, fmt.Sprintf("%s does not name the pipeline definition it ran, so there is nothing to queue against", label)
	}

	return &armedBuild{action: a, definitionID: b.DefinitionID, branch: b.SourceBranch, label: label},
		fmt.Sprintf("press again to re-run %s on %s — esc cancels", label, shortRef(b.SourceBranch))
}

// fire is the command the confirming press runs.
func (a *armedBuild) fire(c *azdo.Client) tea.Cmd {
	if a.action == actionCancel {
		return cancelBuildCmd(c, a.buildID)
	}
	return queueBuildCmd(c, a.definitionID, a.branch)
}

// doing is what the status line says while the request is in flight.
func (a *armedBuild) doing() string {
	if a.action == actionCancel {
		return "cancelling " + a.label + "…"
	}
	return "queueing " + a.label + "…"
}
