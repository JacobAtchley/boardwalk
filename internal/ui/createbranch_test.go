package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// createCall records what the screen asked the branch flow to do.
type createCall struct {
	id     int
	branch string
	repo   azdo.Repo
	from   azdo.Ref
}

func testRepos() []azdo.Repo {
	return []azdo.Repo{
		{ID: "r1", Name: "platform-api", ProjectID: "p", DefaultBranch: "refs/heads/main"},
		{ID: "r2", Name: "platform-web", ProjectID: "p", DefaultBranch: "refs/heads/main"},
		{ID: "r3", Name: "tooling", ProjectID: "p", DefaultBranch: "refs/heads/trunk"},
	}
}

func testRefs() []azdo.Ref {
	return []azdo.Ref{
		{Name: "refs/heads/feature/old", ObjectID: "f1"},
		{Name: "refs/heads/main", ObjectID: "m1"},
		{Name: "refs/heads/hotfix/1.2", ObjectID: "h1"},
	}
}

// newCreateBranch opens the screen for #4021 with the repositories fetched,
// standing where current says. The create step is swapped for one that
// records its arguments, so enter can be checked without a server.
func newCreateBranch(t *testing.T, current string) (*CreateBranch, *[]createCall) {
	t.Helper()
	c, items := fixture()
	m := NewCreateBranch(c, items[0], func() string { return current }, nil)
	var calls []createCall
	m.create = func(id int, branch string, repo azdo.Repo, from azdo.Ref) tea.Cmd {
		calls = append(calls, createCall{id, branch, repo, from})
		return func() tea.Msg { return nil }
	}
	m = updateCB(t, m, tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	m = updateCB(t, m, reposFetchedMsg{ID: 4021, Repos: testRepos()})
	return m, &calls
}

// loaded is newCreateBranch with the selected repository's branches in.
func loaded(t *testing.T, current string) (*CreateBranch, *[]createCall) {
	t.Helper()
	m, calls := newCreateBranch(t, current)
	repo, _ := m.repo()
	return updateCB(t, m, branchesFetchedMsg{ID: 4021, RepoID: repo.ID, Refs: testRefs()}), calls
}

func updateCB(t *testing.T, m *CreateBranch, msg tea.Msg) *CreateBranch {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(*CreateBranch)
}

func pressCB(t *testing.T, m *CreateBranch, msg tea.KeyMsg) (*CreateBranch, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(*CreateBranch), cmd
}

func typeCB(t *testing.T, m *CreateBranch, s string) *CreateBranch {
	t.Helper()
	for _, r := range s {
		m, _ = pressCB(t, m, runes(string(r)))
	}
	return m
}

// batched flattens a command into the messages it produces, one level of
// tea.Batch deep — enough for what enter returns.
func batched(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		if c != nil {
			out = append(out, c())
		}
	}
	return out
}

func TestWorkItemsBranchKeyOpensTheCreateBranchScreen(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	_, cmd := press(t, m, runes("b"))
	if cmd == nil {
		t.Fatal("b did nothing")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("b produced %T, want a PushMsg", cmd())
	}
	if _, ok := push.View.(*CreateBranch); !ok {
		t.Errorf("b pushed %T, want the create branch screen", push.View)
	}
}

func TestCreateBranchFetchesRepositoriesOnEntry(t *testing.T) {
	c, items := fixture()
	m := NewCreateBranch(c, items[0], func() string { return "" }, nil)
	if m.Init() == nil {
		t.Error("the screen did not fetch the project's repositories on entry")
	}
}

func TestCreateBranchPrefillsTheName(t *testing.T) {
	m, _ := newCreateBranch(t, "")
	if got := m.name.Value(); got != "feature/4021-retry-webhook-delivery-on-5xx" {
		t.Errorf("name = %q, want it prefilled with the generated name", got)
	}
}

func TestCreateBranchSelectsTheWorkingDirectoryRepository(t *testing.T) {
	m, _ := newCreateBranch(t, "platform-web")
	if repo, _ := m.repo(); repo.Name != "platform-web" {
		t.Errorf("repository = %q, want the one the working directory is in", repo.Name)
	}
}

func TestCreateBranchFallsBackToTheFirstRepository(t *testing.T) {
	m, _ := newCreateBranch(t, "")
	if repo, _ := m.repo(); repo.Name != "platform-api" {
		t.Errorf("repository = %q, want the first in the list", repo.Name)
	}
}

func TestCreateBranchFetchesBranchesForTheSelectedRepository(t *testing.T) {
	c, items := fixture()
	m := NewCreateBranch(c, items[0], func() string { return "" }, nil)
	_, cmd := m.Update(reposFetchedMsg{ID: 4021, Repos: testRepos()})
	if cmd == nil {
		t.Error("repositories arriving did not start the branch fetch")
	}
}

func TestCreateBranchDefaultsTheSourceToTheDefaultBranch(t *testing.T) {
	m, _ := loaded(t, "")
	if src, _ := m.source(); src.Name != "refs/heads/main" {
		t.Errorf("source = %q, want the repository's default branch", src.Name)
	}
}

func TestCreateBranchEnterCreatesFromTheDefaultBranch(t *testing.T) {
	m, calls := loaded(t, "platform-web")

	_, cmd := pressCB(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msgs := batched(cmd)

	if len(*calls) != 1 {
		t.Fatalf("create ran %d times, want once", len(*calls))
	}
	got := (*calls)[0]
	if got.id != 4021 || got.branch != "feature/4021-retry-webhook-delivery-on-5xx" ||
		got.repo.Name != "platform-web" || got.from.ObjectID != "m1" {
		t.Errorf("create = %+v", got)
	}

	var popped, started bool
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case PopMsg:
			popped = true
		case branchStartedMsg:
			started = msg.ID == 4021
		}
	}
	if !popped {
		t.Error("enter did not return to the work item list")
	}
	if !started {
		t.Error("enter did not tell the work item list the flow started")
	}
}

func TestCreateBranchFilterPicksANonDefaultSource(t *testing.T) {
	m, calls := loaded(t, "")

	m, _ = pressCB(t, m, tea.KeyMsg{Type: tea.KeyShiftTab}) // repository → source
	m = typeCB(t, m, "hot")
	if src, _ := m.source(); src.Name != "refs/heads/hotfix/1.2" {
		t.Fatalf("source = %q after filtering, want the hotfix branch", src.Name)
	}

	pressCB(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(*calls) != 1 || (*calls)[0].from.ObjectID != "h1" {
		t.Errorf("create = %+v, want it created from the hotfix commit", *calls)
	}
}

func TestCreateBranchSourceMovesWithTheArrows(t *testing.T) {
	m, _ := loaded(t, "")
	m, _ = pressCB(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	m, _ = pressCB(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if src, _ := m.source(); src.Name != "refs/heads/hotfix/1.2" {
		t.Errorf("source = %q after down, want the branch after main", src.Name)
	}
}

func TestCreateBranchChangingRepositoryReloadsBranches(t *testing.T) {
	m, _ := loaded(t, "")

	m, cmd := pressCB(t, m, runes("j"))
	if repo, _ := m.repo(); repo.Name != "platform-web" {
		t.Fatalf("repository = %q after j, want the next one", repo.Name)
	}
	if cmd == nil {
		t.Error("changing repository did not fetch its branches")
	}
	if _, ok := m.source(); ok {
		t.Error("the previous repository's branches are still offered")
	}

	// An answer for the repository just left is stale and must not land.
	m = updateCB(t, m, branchesFetchedMsg{ID: 4021, RepoID: "r1", Refs: testRefs()})
	if _, ok := m.source(); ok {
		t.Error("a stale branch listing replaced the loading state")
	}

	m = updateCB(t, m, branchesFetchedMsg{ID: 4021, RepoID: "r2", Refs: testRefs()})
	if src, _ := m.source(); src.Name != "refs/heads/main" {
		t.Errorf("source = %q, want the new repository's default branch", src.Name)
	}
}

func TestCreateBranchIgnoresAnotherItemsMessages(t *testing.T) {
	m, _ := newCreateBranch(t, "")
	m = updateCB(t, m, branchesFetchedMsg{ID: 9999, RepoID: "r1", Refs: testRefs()})
	if _, ok := m.source(); ok {
		t.Error("a listing fetched for another work item landed here")
	}
}

func TestCreateBranchEnterWaitsForBranches(t *testing.T) {
	m, calls := newCreateBranch(t, "")

	m, cmd := pressCB(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(*calls) != 0 || cmd != nil {
		t.Error("enter created a branch with no source chosen")
	}
	if status, _ := m.Status(); status == "" {
		t.Error("enter with no source said nothing")
	}
}

func TestCreateBranchEnterRefusesAnEmptyName(t *testing.T) {
	m, calls := loaded(t, "")
	m.name.SetValue("   ")

	pressCB(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(*calls) != 0 {
		t.Error("enter created a branch with no name")
	}
}

func TestCreateBranchEscapeCancels(t *testing.T) {
	m, calls := loaded(t, "")

	_, cmd := pressCB(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape did nothing")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Error("escape did not go back")
	}
	if len(*calls) != 0 {
		t.Error("escape created a branch")
	}
}

func TestCreateBranchNameTakesActionLetters(t *testing.T) {
	// The name field owns letters that are actions elsewhere — q and o
	// included — and Root must leave them to it.
	m, _ := loaded(t, "")
	m, _ = pressCB(t, m, tea.KeyMsg{Type: tea.KeyTab}) // repository → name
	m = typeCB(t, m, "qo")

	if !strings.HasSuffix(m.name.Value(), "qo") {
		t.Errorf("name = %q, want the keystrokes in it", m.name.Value())
	}
	if !m.Prompting() {
		t.Error("the screen does not report itself as taking typed input")
	}
}

func TestCreateBranchRepositoryFieldLeavesRootItsKeys(t *testing.T) {
	// The repository field takes no text, so esc, q and ? keep meaning what
	// they mean on every other view.
	m, _ := loaded(t, "")
	if m.Prompting() {
		t.Error("the repository field claims typed input it does not take")
	}
}

func TestCreateBranchRepositoryFetchFailureIsShown(t *testing.T) {
	c, items := fixture()
	m := NewCreateBranch(c, items[0], func() string { return "" }, nil)
	m = updateCB(t, m, reposFetchedMsg{ID: 4021, Err: errTest})

	if status, isErr := m.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestCreateBranchWithNoRepositoriesSaysSo(t *testing.T) {
	c, items := fixture()
	m := NewCreateBranch(c, items[0], func() string { return "" }, nil)
	m = updateCB(t, m, reposFetchedMsg{ID: 4021})

	if status, isErr := m.Status(); !isErr || !strings.Contains(status, "no Git repositories") {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestCreateBranchBranchFetchFailureIsShown(t *testing.T) {
	m, _ := newCreateBranch(t, "")
	m = updateCB(t, m, branchesFetchedMsg{ID: 4021, RepoID: "r1", Err: errTest})

	if status, isErr := m.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestCreateBranchBodyShowsEachField(t *testing.T) {
	m, _ := loaded(t, "")
	out := m.Body(testWidth, testHeight)
	for _, want := range []string{"platform-api", "main", "hotfix/1.2", "feature/4021"} {
		if !strings.Contains(out, want) {
			t.Errorf("body is missing %q:\n%s", want, out)
		}
	}
}

func TestWorkItemsReportsTheStartedFlow(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false, false))

	updated, _ := m.Update(branchStartedMsg{Owner: m, ID: 4021, Branch: "hotfix/4021-x", Repo: "platform-api", From: "hotfix/1.2"})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "hotfix/4021-x") || !strings.Contains(status, "hotfix/1.2") {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestItemBranchKeyOpensTheCreateBranchScreen(t *testing.T) {
	m := newItem(t, nil)

	updated, cmd := m.Update(runes("b"))
	if cmd == nil {
		t.Fatal("b did nothing on the item")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("b produced %T, want a PushMsg", cmd())
	}
	screen, ok := push.View.(*CreateBranch)
	if !ok {
		t.Fatalf("b pushed %T, want the create branch screen", push.View)
	}
	if screen.item.ID != 4021 || screen.owner != updated {
		t.Errorf("screen is for #%d owned by %T, want #4021 owned by the item", screen.item.ID, screen.owner)
	}
}

// doneFrom is a flow that created the branch and set the item Active,
// started by owner.
func doneFrom(owner View) branchDoneMsg {
	return branchDoneMsg{Owner: owner, BranchResult: BranchResult{
		Branch:    "feature/4020-tidy",
		ID:        4020,
		Steps:     []string{"created feature/4020-tidy", "set #4020 Active", "linked the branch"},
		Created:   true,
		Activated: true,
	}}
}

func TestItemReportsTheBranchItStarted(t *testing.T) {
	c, items := fixture()
	m := NewItem(c, items[1], nil) // #4020, Needs Refinement
	copied := captureClipboard(t, nil)

	updated, _ := m.Update(doneFrom(m))
	m = updated.(*Item)

	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "created") || !strings.Contains(status, "copied") {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
	if !strings.Contains(*copied, "feature/4020-tidy") {
		t.Errorf("clipboard = %q, want the checkout command", *copied)
	}
	if m.item.State != "Active" {
		t.Errorf("state = %q, want the item showing Active", m.item.State)
	}
}

func TestWorkItemsSyncsTheRowButLeavesTheReportToTheItem(t *testing.T) {
	// Branching from the item that sits over the list: the list's row has to
	// pick up Active, but the item on top reports it and copies the command,
	// once.
	c, items := fixture()
	list := sized(t, NewWorkItems(c, items, false, false))
	item := NewItem(c, items[1], nil)
	copies := 0
	before := copyToClipboard
	copyToClipboard = func(string) error { copies++; return nil }
	t.Cleanup(func() { copyToClipboard = before })

	updated, _ := list.Update(doneFrom(item))
	list = updated.(*WorkItems)
	item.Update(doneFrom(item))

	if !strings.Contains(rowLine(t, list, 4020), "Active") {
		t.Errorf("row for #4020 did not pick up Active: %q", rowLine(t, list, 4020))
	}
	if status, _ := list.Status(); strings.Contains(status, "created") {
		t.Errorf("list status = %q, want the report left to the item", status)
	}
	if copies != 1 {
		t.Errorf("checkout copied %d times, want once", copies)
	}
}

func TestItemIgnoresABranchItDidNotStart(t *testing.T) {
	c, items := fixture()
	m := NewItem(c, items[1], nil)
	copied := captureClipboard(t, nil)
	list := NewWorkItems(c, items, false, false)

	updated, _ := m.Update(doneFrom(list))
	m = updated.(*Item)

	if status, _ := m.Status(); strings.Contains(status, "created") {
		t.Errorf("status = %q, want nothing reported for someone else's flow", status)
	}
	if *copied != "" {
		t.Errorf("clipboard = %q, want nothing copied", *copied)
	}
}

func TestItemReportsTheStartedFlowItOwns(t *testing.T) {
	c, items := fixture()
	m := NewItem(c, items[1], nil)
	updated, _ := m.Update(branchStartedMsg{Owner: m, ID: 4020, Branch: "feature/4020-tidy", Repo: "platform-api", From: "main"})
	m = updated.(*Item)

	if status, _ := m.Status(); !strings.Contains(status, "creating feature/4020-tidy") {
		t.Errorf("status = %q", status)
	}
}

func TestCreateBranchTagsTheFlowWithItsOwner(t *testing.T) {
	c, items := fixture()
	list := NewWorkItems(c, items, false, false)
	m := NewCreateBranch(c, items[0], func() string { return "" }, list)
	m = updateCB(t, m, reposFetchedMsg{ID: 4021, Repos: testRepos()})
	m = updateCB(t, m, branchesFetchedMsg{ID: 4021, RepoID: "r1", Refs: testRefs()})
	m.create = func(int, string, azdo.Repo, azdo.Ref) tea.Cmd { return nil }

	_, cmd := pressCB(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, msg := range batched(cmd) {
		if started, ok := msg.(branchStartedMsg); ok && started.Owner != list {
			t.Errorf("started owner = %T, want the view that opened the screen", started.Owner)
		}
	}
}
