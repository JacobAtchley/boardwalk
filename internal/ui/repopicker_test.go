package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func testRepos() []azdo.Repo {
	return []azdo.Repo{
		{ID: "r1", Name: "platform-api", ProjectID: "p", DefaultBranch: "refs/heads/main"},
		{ID: "r2", Name: "platform-web", ProjectID: "p", DefaultBranch: "refs/heads/main"},
		{ID: "r3", Name: "tooling", ProjectID: "p", DefaultBranch: "refs/heads/trunk"},
	}
}

// branchAsked walks a work item list up to the point where the branch name has
// been typed and the repositories have come back, which is where the picker
// either opens or does not. current is what the working directory resolves to.
func branchAsked(t *testing.T, current string) *WorkItems {
	t.Helper()
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m.currentRepo = func() string { return current }

	m, _ = press(t, m, runes("b"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	updated, _ := m.Update(reposFetchedMsg{ID: 4021, Branch: "feature/x", Repos: testRepos()})
	return updated.(*WorkItems)
}

// TestBranchInsideARepositoryNeverAsks — the picker exists for the case where
// the working directory does not resolve. Inside a repository that does, the
// flow must run exactly as it did before, with no keystroke added.
func TestBranchInsideARepositoryNeverAsks(t *testing.T) {
	m := branchAsked(t, "platform-web")

	if m.repoPicker != nil {
		t.Fatal("the picker opened for a working directory that resolves on its own")
	}
	if status, _ := m.Status(); !strings.Contains(status, "platform-web") {
		t.Errorf("status = %q, want it naming the repository the flow is running against", status)
	}
}

func TestBranchOutsideARepositoryOpensThePicker(t *testing.T) {
	m := branchAsked(t, "")

	if m.repoPicker == nil {
		t.Fatal("the picker did not open for a working directory that is not a repository")
	}
	status, _ := m.Status()
	if !strings.Contains(status, "platform-api") {
		t.Errorf("status = %q, want the picker showing the repository under the cursor", status)
	}
	if !strings.Contains(status, "1 of 3") {
		t.Errorf("status = %q, want it saying where in the list the cursor is", status)
	}
}

// TestBranchInAForeignRepositoryOpensThePicker — a GitHub checkout, or an
// Azure repository belonging to another project. CurrentRepo answers something
// that is simply not in this project's list.
func TestBranchInAForeignRepositoryOpensThePicker(t *testing.T) {
	if m := branchAsked(t, "some-other-repo"); m.repoPicker == nil {
		t.Fatal("the picker did not open for a repository that is not in the project")
	}
}

func TestRepoPickerMovesAndWraps(t *testing.T) {
	m := branchAsked(t, "")

	m, _ = press(t, m, runes("j"))
	if status, _ := m.Status(); !strings.Contains(status, "platform-web") {
		t.Errorf("status = %q, want the second repository after one press of j", status)
	}

	// Up from the top wraps to the bottom, the way the state picker and the
	// landing menu both do.
	m, _ = press(t, m, runes("k"))
	m, _ = press(t, m, runes("k"))
	if status, _ := m.Status(); !strings.Contains(status, "tooling") {
		t.Errorf("status = %q, want the last repository after wrapping past the top", status)
	}
}

func TestRepoPickerEnterRunsTheFlowAgainstTheChosenRepository(t *testing.T) {
	m := branchAsked(t, "")

	m, _ = press(t, m, runes("j"))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.repoPicker != nil {
		t.Error("enter left the picker open")
	}
	if cmd == nil {
		t.Fatal("enter did not start the branch flow")
	}
	if status, _ := m.Status(); !strings.Contains(status, "platform-web") {
		t.Errorf("status = %q, want it naming the repository that was picked", status)
	}
}

func TestRepoPickerEscapeCancels(t *testing.T) {
	m := branchAsked(t, "")

	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling the picker started work anyway")
	}
	if m.repoPicker != nil {
		t.Error("escape did not close the picker")
	}
}

// TestRepoPickerSwallowsActionKeys — the picker is modal, the same as the
// branch prompt and the state picker: a key it does not recognise must do
// nothing rather than reach the list underneath.
func TestRepoPickerSwallowsActionKeys(t *testing.T) {
	m := branchAsked(t, "")

	m, cmd := press(t, m, runes("o"))
	if cmd != nil {
		t.Error("a key the picker does not use reached the view underneath")
	}
	if m.repoPicker == nil {
		t.Error("a key the picker does not use closed it")
	}
}

func TestRepoPickerIsAPrompt(t *testing.T) {
	m := branchAsked(t, "")
	if !m.Prompting() {
		t.Error("Prompting() is false with the picker open, so Root would take esc for navigation")
	}
}

// TestBranchWithNoRepositoriesSaysSoRatherThanOpeningAnEmptyPicker — a project
// with no Git repositories at all. An empty picker offers a choice that does
// not exist.
func TestBranchWithNoRepositoriesSaysSoRatherThanOpeningAnEmptyPicker(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	m.currentRepo = func() string { return "" }

	updated, _ := m.Update(reposFetchedMsg{ID: 4021, Branch: "feature/x"})
	m = updated.(*WorkItems)

	if m.repoPicker != nil {
		t.Fatal("the picker opened with nothing in it")
	}
	status, failed := m.Status()
	if !failed {
		t.Error("a project with no repositories was not reported as a failure")
	}
	if !strings.Contains(status, "no Git repositories") {
		t.Errorf("status = %q, want it saying the project has no repositories", status)
	}
}

func TestReposFetchFailureGoesToTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(reposFetchedMsg{ID: 4021, Branch: "feature/x", Err: errTest})
	m = updated.(*WorkItems)

	if m.repoPicker != nil {
		t.Error("the picker opened on a failed fetch")
	}
	status, failed := m.Status()
	if !failed || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q (failed=%v), want the fetch error reported", status, failed)
	}
}
