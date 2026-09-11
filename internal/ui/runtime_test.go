package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// runtimeView drives a view the way bubbletea drives it: every Update is
// followed by a repaint, because the runtime calls View — and therefore Body —
// after every message it delivers. A test that calls Update and then inspects
// state, or renders only once at the end, cannot see anything a repaint undoes.
// That gap is exactly how a detail pane which reset its scroll offset on every
// frame survived a whole branch of testing, so the loop belongs in a helper
// every view's tests can reach for.
type runtimeView[M View] struct {
	t             *testing.T
	view          M
	width, height int
}

// drive constructs the loop around a view at a fixed terminal size and paints
// the first frame, the way the runtime does once the window size lands.
func drive[M View](t *testing.T, v M, width, height int) *runtimeView[M] {
	t.Helper()
	r := &runtimeView[M]{t: t, view: v, width: width, height: height}
	r.frame()
	return r
}

// send delivers a message and repaints, returning whatever command the view
// asked for. The command is deliberately not run: a view's commands talk to
// Azure DevOps, and a test never does.
func (r *runtimeView[M]) send(msg tea.Msg) tea.Cmd {
	r.t.Helper()
	updated, cmd := r.view.Update(msg)
	next, ok := updated.(M)
	if !ok {
		r.t.Fatalf("Update returned %T, want the same view type back", updated)
	}
	r.view = next
	r.frame()
	return cmd
}

// frame repaints and returns the bytes a user would be looking at.
func (r *runtimeView[M]) frame() string {
	r.t.Helper()
	return r.view.Body(r.width, r.height)
}

// assertDetailScrollSticks proves ctrl+d moves the detail pane and — the part
// that matters — that the movement survives the repaints the runtime does
// afterwards, rather than being undone by the next frame.
func assertDetailScrollSticks[M View](t *testing.T, r *runtimeView[M], marker string) {
	t.Helper()

	if !strings.Contains(r.frame(), marker) {
		t.Fatalf("expected %q at the top of the detail pane before scrolling:\n%s", marker, r.frame())
	}

	r.send(tea.KeyMsg{Type: tea.KeyCtrlD})
	if strings.Contains(r.frame(), marker) {
		t.Fatalf("ctrl+d did not scroll the detail pane — %q is still visible:\n%s", marker, r.frame())
	}

	// The runtime repaints after every message, not just the one that scrolled.
	// A view that re-renders its detail pane on every frame scrolls back to the
	// top here, which is what the user actually experiences.
	r.frame()
	r.frame()
	if strings.Contains(r.frame(), marker) {
		t.Errorf("the detail pane scrolled back to the top on a repaint — %q is visible again:\n%s", marker, r.frame())
	}
}

func TestWorkItemsDetailScrollSurvivesARepaint(t *testing.T) {
	c, items := fixture()
	r := drive[*WorkItems](t, NewWorkItems(c, items, false), 120, 8)

	assertDetailScrollSticks(t, r, "#4021")
}

func TestPullRequestsDetailScrollSurvivesARepaint(t *testing.T) {
	r := drive[*PullRequests](t, newPRs(t), 120, 8)

	assertDetailScrollSticks(t, r, "author:")
}

func TestBuildsDetailScrollSurvivesARepaint(t *testing.T) {
	m := newBuilds(t)
	updated, _ := m.Update(timelineMsg{
		Build:    9001,
		Progress: azdo.Progress{CurrentStep: "Run tests"},
		Records: []azdo.Record{
			{Name: "Checkout", Type: "Task", Result: "succeeded", Order: 1},
			{Name: "Restore", Type: "Task", Result: "succeeded", Order: 2},
			{Name: "Build", Type: "Task", Result: "succeeded", Order: 3},
			{Name: "Run tests", Type: "Task", State: "inProgress", Order: 4},
			{Name: "Package", Type: "Task", Order: 5},
			{Name: "Publish", Type: "Task", Order: 6},
		},
	})
	r := drive[*Builds](t, updated.(*Builds), 120, 8)

	assertDetailScrollSticks(t, r, "status:")
}
