package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

var errTest = errors.New("the server said no")

func TestPickRepo(t *testing.T) {
	repos := []azdo.Repo{
		{ID: "r1", Name: "platform-api"},
		{ID: "r2", Name: "platform-web"},
	}

	if got, ok := pickRepo(repos, "platform-web"); !ok || got.ID != "r2" {
		t.Errorf("pickRepo by name = %+v, %v", got, ok)
	}
	if _, ok := pickRepo(repos, "not-here"); ok {
		t.Error("pickRepo matched a repository that is not in the project")
	}
	if _, ok := pickRepo(repos, ""); ok {
		t.Error("pickRepo with no name reported a match")
	}
	if _, ok := pickRepo(nil, "platform-api"); ok {
		t.Error("pickRepo against no repositories reported a match")
	}
}

func TestBranchPromptOpensPrefilledAndIsEditable(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	if m.branchPrompt == nil {
		t.Fatal("b did not open the branch prompt")
	}
	if got := m.branchPrompt.Value(); got != "feature/4021-retry-webhook-delivery-on-5xx" {
		t.Fatalf("prompt = %q, want it prefilled with the generated name", got)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if strings.HasSuffix(m.branchPrompt.Value(), "5xx") {
		t.Error("the prompt did not take the edit")
	}
}

func TestBranchPromptEscapeCancels(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling the prompt started work anyway")
	}
	if m.branchPrompt != nil {
		t.Error("escape did not close the prompt")
	}
}

func TestBranchPromptSwallowsActionKeys(t *testing.T) {
	// With the prompt open, "o" is a letter rather than the open action.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	m, _ = press(t, m, runes("o"))
	if !strings.HasSuffix(m.branchPrompt.Value(), "o") {
		t.Errorf("prompt = %q, want the keystroke in it", m.branchPrompt.Value())
	}
}

func TestBranchDoneReportsEveryStep(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch: "feature/4021-retry",
		Steps:  []string{"created feature/4021-retry", "set #4021 Active", "linked the branch"},
	}})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr {
		t.Error("a successful flow reported as an error")
	}
	for _, want := range []string{"created", "Active", "linked"} {
		if !strings.Contains(status, want) {
			t.Errorf("status = %q, want it to mention %q", status, want)
		}
	}
	if cmd == nil {
		t.Error("a successful flow did not hand a checkout command to the shell")
	}
}

func TestBranchDoneKeepsTheStepsThatSucceeded(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch: "feature/4021-retry",
		Steps:  []string{"created feature/4021-retry"},
		Err:    errTest,
	}})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if !isErr {
		t.Error("a partial failure did not report as an error")
	}
	if !strings.Contains(status, "created feature/4021-retry") {
		t.Errorf("status = %q, want the completed step still named", status)
	}
	if cmd != nil {
		t.Error("a failed flow still handed a command to the shell")
	}
}

func TestSetStateReportsOnTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, _ := m.Update(stateSetMsg{ID: 4021, State: "Active"})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "Active") {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
	// The row itself has to show the new state, not just the status line.
	if !strings.Contains(body(t, m), "Active") {
		t.Error("the row did not pick up the new state")
	}
}
