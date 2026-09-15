package ui

import (
	"fmt"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Toggling a pull request between draft and published is bound in two views —
// the list and the detail pane — and both do it the same way: arm on the first
// press, fire on the second. The wording, the arming state and the command all
// live here so the two cannot drift apart, the way the key bindings live in
// keys.go for the same reason.
//
// It arms rather than firing outright because publishing is not a private act:
// Azure DevOps notifies every reviewer on the pull request, and a key pressed
// by accident cannot be taken back by pressing it again — that would only put
// the pull request back into draft after everyone had already been told.

// draftVerb names what pressing the key on this pull request will do.
func draftVerb(isDraft bool) string {
	if isDraft {
		return "publish"
	}
	return "mark draft"
}

// draftDone is the same action once it has happened, for the status line.
func draftDone(nowDraft bool) string {
	if nowDraft {
		return "marked as a draft"
	}
	return "published"
}

// draftBinding is the key, labelled for the pull request it would act on. The
// help panel is built fresh on every keystroke, so a binding that reads
// "publish" on a draft and "mark draft" on a published pull request costs
// nothing and saves the reader guessing which direction the key goes.
func draftBinding(isDraft bool) key.Binding {
	b := keyDraft
	b.SetHelp("P", draftVerb(isDraft))
	return b
}

// draftSetMsg is the result of a toggle. It names the pull request because a
// toggle fired from the list can land after the detail view for another one
// has been pushed.
type draftSetMsg struct {
	PR    int
	Draft bool
	Err   error
}

// setDraftCmd toggles pr to draft, reporting through draftSetMsg.
func setDraftCmd(client *azdo.Client, repoID string, prID int, draft bool) tea.Cmd {
	return func() tea.Msg {
		if err := client.SetDraft(repoID, prID, draft); err != nil {
			return draftSetMsg{PR: prID, Draft: draft, Err: fmt.Errorf("could not %s !%d: %w", draftVerb(!draft), prID, err)}
		}
		return draftSetMsg{PR: prID, Draft: draft}
	}
}

// armedDraft is a toggle waiting on its confirming keystroke.
type armedDraft struct {
	repoID string
	prID   int
	// draft is what the confirming press will set, which is the opposite of
	// where the pull request was when the key was first pressed.
	draft bool
}

// armDraft describes the toggle pr's current state calls for, with the status
// line text that says what a second press will do.
func armDraft(pr azdo.PullRequest) (*armedDraft, string) {
	return &armedDraft{repoID: pr.RepoID, prID: pr.ID, draft: !pr.IsDraft},
		fmt.Sprintf("press again to %s !%d — esc cancels", draftVerb(pr.IsDraft), pr.ID)
}

// resolveDraftKey decides what a keypress means while a toggle is armed. The
// arm is modal: esc cancels, the same key confirms, and every other key is
// swallowed rather than acted on, so a key meant for the confirm cannot cycle
// a filter or push a view underneath it.
func resolveDraftKey(msg tea.KeyMsg) (confirm, cancel bool) {
	switch {
	case key.Matches(msg, keyBack):
		return false, true
	case key.Matches(msg, keyDraft):
		return true, false
	default:
		return false, false
	}
}
