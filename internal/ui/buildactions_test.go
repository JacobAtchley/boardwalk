package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// actionableBuilds is the builds view with definition ids on its rows, which
// is what the queue and re-run keys need. buildFixture leaves them off, since
// nothing else reads them.
func actionableBuilds(t *testing.T) *Builds {
	t.Helper()
	c, builds := buildFixture()
	for i := range builds {
		builds[i].DefinitionID = 40 + i
	}
	m := NewBuilds(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	updated, _ := m.Update(buildsMsg{Builds: builds})
	m = updated.(*Builds)
	m.Body(160, 20)
	return m
}

// pressBuilds sends one key to the builds view.
func pressBuilds(t *testing.T, m *Builds, msg tea.KeyMsg) (*Builds, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(*Builds)
	if !ok {
		t.Fatalf("Update returned %T, want *Builds", updated)
	}
	return next, cmd
}

// TestBuildActionsArmRatherThanFiring — each of these starts or stops real CI
// on a shared server. They follow the draft toggle: the first press arms and
// says what a second will do, and nothing leaves the machine until it comes.
func TestBuildActionsArmRatherThanFiring(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		want string
	}{
		{"re-run", "Q", "re-run"},
		{"cancel", "C", "cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := actionableBuilds(t)

			m, cmd := pressBuilds(t, m, runes(tc.key))
			if cmd != nil {
				t.Error("the first press sent something to the server")
			}
			if m.armedBuild == nil {
				t.Fatal("the first press did not arm")
			}
			status, _ := m.Status()
			if !strings.Contains(status, tc.want) {
				t.Errorf("status = %q, want it naming the %s a second press would do", status, tc.want)
			}
			if !strings.Contains(status, "press again") {
				t.Errorf("status = %q, want it saying a second press is needed", status)
			}
		})
	}
}

func TestBuildActionConfirmsOnASecondPress(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("Q"))
	m, cmd := pressBuilds(t, m, runes("Q"))

	if cmd == nil {
		t.Fatal("the second press did not start the re-run")
	}
	if m.armedBuild != nil {
		t.Error("the arm outlived the press that fired it")
	}
}

func TestBuildActionEscapeCancelsTheArm(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("Q"))
	m, cmd := pressBuilds(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Error("escaping the arm started work anyway")
	}
	if m.armedBuild != nil {
		t.Error("escape did not disarm")
	}
	// Not asserted as empty: the timeline fetches the list kicks off leave
	// the work spinner on the front of every status line.
	if status, _ := m.Status(); strings.Contains(status, "press again") {
		t.Errorf("status = %q, want the arm's prompt gone", status)
	}
}

// TestBuildActionRearmsToADifferentAction — pressing cancel while a re-run is
// armed means cancel, not a confirmation of the re-run. Anything else would
// let one key press fire an action the reader was not looking at.
func TestBuildActionRearmsToADifferentAction(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("Q"))
	m, cmd := pressBuilds(t, m, runes("C"))

	if cmd != nil {
		t.Fatal("a different action's key fired the armed one")
	}
	if m.armedBuild == nil {
		t.Fatal("the arm was dropped rather than moved")
	}
	if status, _ := m.Status(); !strings.Contains(status, "cancel") {
		t.Errorf("status = %q, want it armed to cancel now", status)
	}
}

// TestBuildActionSwallowsOtherKeys — the arm is modal, the same as the vote
// and the draft toggle: a key that is neither confirm nor cancel must not
// reach the list underneath and push a view over the top of it.
func TestBuildActionSwallowsOtherKeys(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("Q"))
	m, cmd := pressBuilds(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Error("enter reached the list and opened the logs under an armed action")
	}
	if m.armedBuild == nil {
		t.Error("enter dropped the arm")
	}
}

// TestCancelRefusesAFinishedRun — knowable before the request, so it is
// refused before the request, the way armDraft refuses a merged pull request.
func TestCancelRefusesAFinishedRun(t *testing.T) {
	m := actionableBuilds(t)
	m, _ = pressBuilds(t, m, runes("j")) // the failed run

	m, cmd := pressBuilds(t, m, runes("C"))

	if cmd != nil || m.armedBuild != nil {
		t.Fatal("cancel armed on a run that has already finished")
	}
	status, failed := m.Status()
	if !failed {
		t.Error("the refusal was not reported as one")
	}
	if !strings.Contains(status, "failed") {
		t.Errorf("status = %q, want it naming the state that makes cancelling pointless", status)
	}
}

func TestRerunRefusesARunWithNoDefinition(t *testing.T) {
	c, builds := buildFixture() // no definition ids
	m := NewBuilds(c)
	updated, _ := m.Update(buildsMsg{Builds: builds})
	m = updated.(*Builds)
	m.Body(160, 20)

	m, cmd := pressBuilds(t, m, runes("Q"))

	if cmd != nil || m.armedBuild != nil {
		t.Fatal("a re-run armed with no definition to queue against")
	}
	if status, failed := m.Status(); !failed || !strings.Contains(status, "definition") {
		t.Errorf("status = %q (failed=%v), want it saying the definition is unknown", status, failed)
	}
}

func TestNewRunPromptsPrefilledWithTheRunsBranch(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("N"))

	if m.branchPrompt == nil {
		t.Fatal("N did not open the branch prompt")
	}
	if got := m.branchPrompt.Value(); got != "refs/heads/main" {
		t.Errorf("prompt = %q, want it prefilled with the selected run's branch", got)
	}
	if !m.Prompting() {
		t.Error("Prompting() is false with the prompt open")
	}
}

func TestNewRunPromptEnterQueues(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("N"))
	m, cmd := pressBuilds(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("enter did not queue the run")
	}
	if m.branchPrompt != nil {
		t.Error("enter left the prompt open")
	}
}

func TestNewRunPromptEscapeCancels(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("N"))
	m, cmd := pressBuilds(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Error("escaping the prompt queued a run anyway")
	}
	if m.branchPrompt != nil {
		t.Error("escape did not close the prompt")
	}
}

// TestNewRunPromptSwallowsActionKeys — with the prompt open "Q" is a letter,
// not the re-run key.
func TestNewRunPromptSwallowsActionKeys(t *testing.T) {
	m := actionableBuilds(t)

	m, _ = pressBuilds(t, m, runes("N"))
	m, _ = pressBuilds(t, m, runes("Q"))

	if m.armedBuild != nil {
		t.Fatal("a keystroke meant for the prompt armed a re-run")
	}
	if !strings.HasSuffix(m.branchPrompt.Value(), "Q") {
		t.Errorf("prompt = %q, want the keystroke in it", m.branchPrompt.Value())
	}
}

func TestQueuedRunIsReportedAndTheListRefreshes(t *testing.T) {
	m := actionableBuilds(t)

	updated, cmd := m.Update(buildQueuedMsg{
		Build: azdo.Build{ID: 9100, Number: "20260911.4", Pipeline: "platform-ci"},
	})
	m = updated.(*Builds)

	status, failed := m.Status()
	if failed {
		t.Error("a successful queue was reported as a failure")
	}
	if !strings.Contains(status, "20260911.4") {
		t.Errorf("status = %q, want it naming the run that was queued", status)
	}
	// The new run belongs on the list, and only the server knows its number.
	if cmd == nil {
		t.Error("the list was not refreshed, so the queued run never appears")
	}
}

func TestQueueFailureGoesToTheStatusLine(t *testing.T) {
	m := actionableBuilds(t)

	updated, _ := m.Update(buildQueuedMsg{Err: errTest})
	m = updated.(*Builds)

	if status, failed := m.Status(); !failed || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q (failed=%v), want the failure reported", status, failed)
	}
}

func TestCancelledRunIsReportedAndTheListRefreshes(t *testing.T) {
	m := actionableBuilds(t)

	updated, cmd := m.Update(buildCancelledMsg{Build: 9001})
	m = updated.(*Builds)

	if status, failed := m.Status(); failed || !strings.Contains(status, "9001") {
		t.Errorf("status = %q (failed=%v), want it naming the cancelled run", status, failed)
	}
	if cmd == nil {
		t.Error("the list was not refreshed, so the row still reads as running")
	}
}
