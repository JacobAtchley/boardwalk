package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

var errTest = errors.New("the server said no")

// rowLine returns the rendered line for a work item id, so a test can check
// what the list shows for one row without matching on the whole body.
func rowLine(t *testing.T, m *WorkItems, id int) string {
	t.Helper()
	marker := fmt.Sprintf("%d", id)
	for _, line := range strings.Split(body(t, m), "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no row found for #%d", id)
	return ""
}

// captureClipboard swaps the clipboard shell-out for one that records what it
// was handed, so a test can read back the command the branch flow copied. The
// returned pointer is empty until something copies.
func captureClipboard(t *testing.T, err error) *string {
	t.Helper()
	var got string
	before := copyToClipboard
	copyToClipboard = func(s string) error {
		got = s
		return err
	}
	t.Cleanup(func() { copyToClipboard = before })
	return &got
}

func TestCheckoutCommandFetchesChecksOutAndPulls(t *testing.T) {
	got := CheckoutCommand("feature/4020-tidy")
	want := "git fetch origin && git checkout feature/4020-tidy && git pull"
	if got != want {
		t.Errorf("CheckoutCommand = %q, want %q", got, want)
	}
}

func TestCheckoutCommandQuotesNamesTheShellWouldActOn(t *testing.T) {
	// Git allows & in a ref name; a shell pasted this unquoted would run the
	// checkout in the background and then try to run "fixes" as a command.
	got := CheckoutCommand("bugfix/4020-a&b")
	if !strings.Contains(got, "'bugfix/4020-a&b'") {
		t.Errorf("CheckoutCommand = %q, want the branch quoted", got)
	}
}

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
	m := sized(t, NewWorkItems(c, items, false, false))

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
	m := sized(t, NewWorkItems(c, items, false, false))

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
	m := sized(t, NewWorkItems(c, items, false, false))

	m, _ = press(t, m, runes("b"))
	m, _ = press(t, m, runes("o"))
	if !strings.HasSuffix(m.branchPrompt.Value(), "o") {
		t.Errorf("prompt = %q, want the keystroke in it", m.branchPrompt.Value())
	}
}

func TestBranchDoneReportsEveryStep(t *testing.T) {
	// #4020 starts life as "Needs Refinement" in the fixture, so a row
	// picking up Active is evidence of the sync rather than a coincidence.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	copied := captureClipboard(t, nil)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch:    "feature/4020-tidy",
		ID:        4020,
		Steps:     []string{"created feature/4020-tidy", "set #4020 Active", "linked the branch"},
		Created:   true,
		Activated: true,
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
	if *copied != "git fetch origin && git checkout feature/4020-tidy && git pull" {
		t.Errorf("clipboard = %q, want the checkout command", *copied)
	}
	if !strings.Contains(status, "copied") {
		t.Errorf("status = %q, want it to say the command was copied", status)
	}
	if cmd != nil {
		t.Error("the branch flow quit or started more work instead of staying put")
	}
	// The row itself has to show the new state, not just the status line.
	if !strings.Contains(rowLine(t, m, 4020), "Active") {
		t.Errorf("row for #4020 did not pick up Active: %q", rowLine(t, m, 4020))
	}
}

func TestBranchDoneKeepsTheStepsThatSucceeded(t *testing.T) {
	// The flow got past creating the branch and setting the state before
	// failing on the link step. Both of those landed on the server — the
	// state change included — even though the flow as a whole failed.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	copied := captureClipboard(t, nil)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch:    "feature/4020-tidy",
		ID:        4020,
		Steps:     []string{"created feature/4020-tidy", "set #4020 Active"},
		Created:   true,
		Activated: true,
		Err:       errTest,
	}})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if !isErr {
		t.Error("a partial failure did not report as an error")
	}
	if !strings.Contains(status, "created feature/4020-tidy") {
		t.Errorf("status = %q, want the completed step still named", status)
	}
	if cmd != nil {
		t.Error("a failed flow started more work")
	}
	// The branch itself is on the server, so the checkout is still worth
	// having even though the link step failed.
	if !strings.Contains(*copied, "feature/4020-tidy") {
		t.Errorf("clipboard = %q, want the checkout command for the branch that was created", *copied)
	}
	// The state change really happened on the server, so the row has to
	// reflect it even though the flow overall reads as a failure.
	if !strings.Contains(rowLine(t, m, 4020), "Active") {
		t.Errorf("row for #4020 did not pick up Active despite the state change landing: %q", rowLine(t, m, 4020))
	}
}

func TestBranchDoneLeavesTheRowAloneWhenActivationNeverHappened(t *testing.T) {
	// The flow failed before the state-change step ran at all — nothing
	// landed on the server, so the row must not move.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	before := rowLine(t, m, 4020)
	copied := captureClipboard(t, nil)

	updated, _ := m.Update(branchDoneMsg{BranchResult{
		Branch: "feature/4020-tidy",
		ID:     4020,
		Err:    errTest,
	}})
	m = updated.(*WorkItems)

	if *copied != "" {
		t.Errorf("clipboard = %q, want nothing copied for a branch that was never created", *copied)
	}
	if got := rowLine(t, m, 4020); got != before {
		t.Errorf("row for #4020 changed despite the state change never landing: %q, was %q", got, before)
	}
}

func TestBranchDoneShowsTheCommandWhenTheClipboardRefuses(t *testing.T) {
	// pbcopy can be missing or refuse. Saying "copied" then would leave the
	// user pasting whatever was on the clipboard before, so the command goes
	// on the status line to be read instead.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))
	captureClipboard(t, errTest)

	updated, _ := m.Update(branchDoneMsg{BranchResult{
		Branch:  "feature/4020-tidy",
		ID:      4020,
		Steps:   []string{"created feature/4020-tidy"},
		Created: true,
	}})
	m = updated.(*WorkItems)

	status, _ := m.Status()
	if !strings.Contains(status, "git checkout feature/4020-tidy") {
		t.Errorf("status = %q, want the checkout command spelled out", status)
	}
}

func TestSetStateReportsOnTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

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
