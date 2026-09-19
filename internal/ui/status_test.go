package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// goodSession is a session with nothing wrong with it, for a test to spoil
// one field of.
func goodSession() azdo.Session {
	return azdo.Session{
		Org:     "acme",
		Project: "Platform",
		Me:      "dev@acme.test",
		MyID:    "me-guid",
		Groups: azdo.Groups{
			IDs:   map[string]bool{"guid-devs": true},
			Names: map[string]bool{"platform-devs": true, "engineering": true},
		},
		GroupsResolved: true,
	}
}

// newStatus drives the session view with the surroundings a test wants to
// describe, rather than the machine's own.
func newStatus(t *testing.T, s azdo.Session) *Status {
	t.Helper()
	m := NewStatus(&azdo.Client{Org: s.Org, Project: s.Project}, s)
	m.configPath = func() (string, error) { return "/home/dev/.config/boardwalk.json", nil }
	m.workingDir = func() (string, error) { return "/home/dev/src/boardwalk", nil }
	m.currentRepo = func() string { return "boardwalk" }
	m.Body(120, 40)
	return m
}

func TestStatusShowsWhereItIsPointed(t *testing.T) {
	m := newStatus(t, goodSession())

	view := m.Body(120, 40)
	for _, want := range []string{"acme", "Platform", "/home/dev/.config/boardwalk.json",
		"/home/dev/src/boardwalk", "dev@acme.test"} {
		if !strings.Contains(view, want) {
			t.Errorf("the session view is missing %q:\n%s", want, view)
		}
	}
	if _, failed := m.Status(); failed {
		t.Error("a healthy session was reported as a problem")
	}
}

// TestStatusNamesTheConsequenceOfAnUnknownUser — the whole reason this view
// exists. "(unknown)" is a fact; the sentence after it is what tells somebody
// why their pull request list is empty.
func TestStatusNamesTheConsequenceOfAnUnknownUser(t *testing.T) {
	s := goodSession()
	s.Me = ""
	m := newStatus(t, s)

	view := m.Body(120, 40)
	if !strings.Contains(view, "az account show") {
		t.Errorf("the view does not say where the user name comes from:\n%s", view)
	}
	if !strings.Contains(view, "nothing reads as yours") {
		t.Errorf("the view does not say what an unknown user costs:\n%s", view)
	}
	if _, failed := m.Status(); !failed {
		t.Error("a session that cannot identify its user was not reported as a problem")
	}
}

func TestStatusListsTheResolvedGroups(t *testing.T) {
	m := newStatus(t, goodSession())

	view := m.Body(120, 40)
	for _, want := range []string{"platform-devs", "engineering"} {
		if !strings.Contains(view, want) {
			t.Errorf("a resolved group is missing from the view:\n%s", view)
		}
	}
}

// TestStatusExplainsAFailedGraphWalk — the case actually hit: an empty
// reviewGroups and a Graph that answered with something other than groups.
func TestStatusExplainsAFailedGraphWalk(t *testing.T) {
	s := goodSession()
	s.Groups, s.GroupsResolved = azdo.Groups{}, false
	s.GroupsErr = errors.New("403 Forbidden: the token is not scoped for Graph")
	m := newStatus(t, s)

	view := m.Body(120, 40)
	if !strings.Contains(view, "not scoped for Graph") {
		t.Errorf("the reason the walk failed is not on screen:\n%s", view)
	}
	if !strings.Contains(view, "reviewGroups") {
		t.Errorf("the view does not say what still works without Graph:\n%s", view)
	}
	if _, failed := m.Status(); !failed {
		t.Error("a failed Graph walk was not reported as a problem")
	}
}

// TestStatusDistinguishesNoGroupsFromNoAnswer — the two states this view was
// built to separate. Rendering them the same way is the bug it fixes.
func TestStatusDistinguishesNoGroupsFromNoAnswer(t *testing.T) {
	empty := goodSession()
	empty.Groups = azdo.Groups{IDs: map[string]bool{}, Names: map[string]bool{}}

	refused := goodSession()
	refused.Groups, refused.GroupsResolved = azdo.Groups{}, false
	refused.GroupsErr = errors.New("no route to host")

	a := newStatus(t, empty).Body(120, 40)
	b := newStatus(t, refused).Body(120, 40)

	if a == b {
		t.Errorf("belonging to no groups and failing to ask render identically:\n%s", a)
	}
	if !strings.Contains(a, "no groups") {
		t.Errorf("a genuine empty answer does not say so:\n%s", a)
	}
}

func TestStatusShowsTheConfiguredGroupsSeparately(t *testing.T) {
	s := goodSession()
	s.ReviewGroups = []string{"release-managers"}
	m := newStatus(t, s)

	if view := m.Body(120, 40); !strings.Contains(view, "release-managers") {
		t.Errorf("the configured groups are not shown:\n%s", view)
	}
}

// TestStatusNamesTheConsequenceOfNoIdentity — without MyID a group's vote
// cannot be cast, which is a different failure from not being in the group.
func TestStatusNamesTheConsequenceOfNoIdentity(t *testing.T) {
	s := goodSession()
	s.MyID = ""
	s.IdentityErr = errors.New("TF400813: the user is not authorized")
	m := newStatus(t, s)

	view := m.Body(120, 40)
	if !strings.Contains(view, "TF400813") {
		t.Errorf("the reason the identity is missing is not on screen:\n%s", view)
	}
	if !strings.Contains(view, "vote") {
		t.Errorf("the view does not say what a missing identity costs:\n%s", view)
	}
}

func TestStatusResolvesTheWorkingDirectoryAgainstTheProject(t *testing.T) {
	m := newStatus(t, goodSession())

	updated, _ := m.Update(reposFetchedMsg{Repos: []azdo.Repo{
		{ID: "r1", Name: "boardwalk"},
		{ID: "r2", Name: "other"},
	}})
	m = updated.(*Status)

	view := m.Body(120, 40)
	if !strings.Contains(view, "boardwalk") {
		t.Errorf("the working directory's repository is not named:\n%s", view)
	}
	if !strings.Contains(view, "in this project") {
		t.Errorf("the view does not say the repository is one of the project's:\n%s", view)
	}
}

func TestStatusSaysWhenTheWorkingDirectoryIsNotAProjectRepository(t *testing.T) {
	m := newStatus(t, goodSession())
	m.currentRepo = func() string { return "" }

	updated, _ := m.Update(reposFetchedMsg{Repos: []azdo.Repo{{ID: "r1", Name: "boardwalk"}}})
	m = updated.(*Status)

	view := m.Body(120, 40)
	if !strings.Contains(view, "not an Azure DevOps repository") {
		t.Errorf("the view does not say the working directory is not a repository:\n%s", view)
	}
	// Not a problem: running boardwalk outside a checkout is ordinary, and
	// the repository picker handles it.
	if _, failed := m.Status(); failed {
		t.Error("running outside a repository was reported as a problem")
	}
}

func TestStatusRefreshesOnR(t *testing.T) {
	m := newStatus(t, goodSession())

	updated, cmd := m.Update(runes("r"))
	m = updated.(*Status)
	if cmd == nil {
		t.Fatal("r did not re-resolve anything")
	}
	if status, _ := m.Status(); !strings.Contains(status, "resolving") {
		t.Errorf("status = %q, want it saying the walk is running again", status)
	}
}

// TestStatusTakesAFreshSession — what the refresh comes back with has to
// replace what was on screen, or r would look like it did nothing.
func TestStatusTakesAFreshSession(t *testing.T) {
	s := goodSession()
	s.Groups, s.GroupsResolved = azdo.Groups{}, false
	s.GroupsErr = errors.New("no route to host")
	m := newStatus(t, s)

	updated, _ := m.Update(sessionMsg{Session: goodSession()})
	m = updated.(*Status)

	view := m.Body(120, 40)
	if strings.Contains(view, "no route to host") {
		t.Errorf("the stale failure survived a successful re-resolve:\n%s", view)
	}
	if !strings.Contains(view, "platform-devs") {
		t.Errorf("the freshly resolved groups are not shown:\n%s", view)
	}
}

func TestStatusEscapePops(t *testing.T) {
	m := newStatus(t, goodSession())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc did nothing")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("esc sent %T, want PopMsg", cmd())
	}
}

// TestSessionIsOnTheMenu — a diagnostic nobody can find is not a diagnostic.
func TestSessionIsOnTheMenu(t *testing.T) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	r := NewRoot(c, false, false, "")
	r.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(r.View(), "session") {
		t.Errorf("the menu does not offer the session view:\n%s", r.View())
	}
	if v := r.build("status"); v == nil {
		t.Error(`Root.build("status") returned nothing, so the menu entry opens nothing`)
	}
}
