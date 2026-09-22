package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func buildStub() azdo.Build {
	return azdo.Build{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: azdo.StatusSucceeded}
}

func newRoot(t *testing.T, start string) *Root {
	t.Helper()
	c, items := fixture()
	r := NewRoot(c, false, false, start)
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	r = updated.(*Root)
	// The work item view fetches on entry like every other view, so the batch
	// its command would have returned is delivered here instead. Nothing in
	// these tests talks to the network.
	if start == "items" {
		r = loadItems(t, r, items)
	}
	return r
}

// loadItems delivers the work item batch a view's fetch would have produced.
func loadItems(t *testing.T, r *Root, items []azdo.WorkItem) *Root {
	t.Helper()
	updated, _ := r.Update(workItemsMsg{Items: items})
	return updated.(*Root)
}

func TestRootShowsTheBannerAndMenu(t *testing.T) {
	r := newRoot(t, "")
	view := r.View()

	for _, want := range []string{"work items", "pull requests", "builds"} {
		if !strings.Contains(view, want) {
			t.Errorf("the menu is missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, strings.Split(Banner, "\n")[1]) {
		t.Error("the banner did not render")
	}
}

func TestRootMenuOpensTheChosenView(t *testing.T) {
	r := newRoot(t, "")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r = loadItems(t, updated.(*Root), mustItems(t))

	if !strings.Contains(r.View(), "work items (all 3)") {
		t.Errorf("enter on the first entry did not open work items:\n%s", r.View())
	}
}

func TestRootMenuMovesBetweenEntries(t *testing.T) {
	r := newRoot(t, "")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyDown})
	r = updated.(*Root)
	updated, _ = r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r = updated.(*Root)

	if !strings.Contains(r.View(), "pull requests") {
		t.Errorf("the second entry did not open pull requests:\n%s", r.View())
	}
}

func TestRootStartJumpsStraightToAView(t *testing.T) {
	r := newRoot(t, "prs")

	if !strings.Contains(r.View(), "pull requests") {
		t.Errorf("start=prs did not open the pull request view:\n%s", r.View())
	}
}

func TestRootEscapeReturnsToTheMenu(t *testing.T) {
	r := newRoot(t, "items")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	r = updated.(*Root)

	// "open item" is on the work item view's own footer, not the menu's blurb
	// for the same entry — unlike "^t scope", which moved into the panel
	// behind "?" once the footer was trimmed to fit 80 columns (see
	// WorkItems.Keys), this stays a marker only the open view renders.
	if !strings.Contains(r.View(), "pull requests") || strings.Contains(r.View(), "open item") {
		t.Errorf("escape did not return to the menu:\n%s", r.View())
	}
}

func TestRootEscapeOnTheMenuQuits(t *testing.T) {
	r := newRoot(t, "")

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape on the menu produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("escape on the menu produced %T, want a quit", cmd())
	}
}

func TestRootPushAndPop(t *testing.T) {
	r := newRoot(t, "items")

	pushed := NewLogs(r.client, buildStub(), nil)
	updated, _ := r.Update(PushMsg{View: pushed})
	r = updated.(*Root)
	if !strings.Contains(r.View(), "platform-ci") {
		t.Errorf("the pushed view did not render:\n%s", r.View())
	}

	updated, _ = r.Update(PopMsg{})
	r = updated.(*Root)
	if !strings.Contains(r.View(), "work items") {
		t.Errorf("pop did not return to the view underneath:\n%s", r.View())
	}
}

func TestRootRendersChromeAroundTheView(t *testing.T) {
	r := newRoot(t, "items")
	view := r.View()

	if !strings.Contains(view, "acme/Platform") {
		t.Error("the header is missing")
	}
	if !strings.Contains(view, "esc back") {
		t.Error("the help line is missing")
	}
}

func TestRootQuitsOnQFromTheMenu(t *testing.T) {
	r := newRoot(t, "")

	_, cmd := r.Update(runes("q"))
	if cmd == nil {
		t.Fatal("q on the menu produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want a quit", cmd())
	}
}

// openPrompt is a key that leaves the work item view taking typed input: the
// fuzzy filter, and the branch name editor.
var openPrompt = map[string]tea.KeyMsg{
	"the fuzzy filter":  runes("/"),
	"the branch prompt": runes("b"),
}

func TestRootCtrlCQuitsEvenWithAPromptOpen(t *testing.T) {
	// bubbletea does not quit on ctrl+c by itself — the model has to. Yielding
	// every key to a view that is taking typed input left the universal
	// terminal interrupt doing nothing at all: the branch prompt is a bare
	// textinput and does not bind it. (The fuzzy filter survived on an
	// accident: bubbles/list binds ctrl+c as ForceQuit itself. It is covered
	// here anyway, so the guarantee does not rest on that.)
	for name, open := range openPrompt {
		r := newRoot(t, "items")

		updated, _ := r.Update(open)
		r = updated.(*Root)
		top, _ := r.top()
		if !typing(top) {
			t.Fatalf("%s did not open", name)
		}

		_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatalf("ctrl+c with %s open produced no command", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("ctrl+c with %s open produced %T, want a quit", name, cmd())
		}
	}
}

func TestRootLeavesEscAndQToAnOpenPrompt(t *testing.T) {
	// esc closes the prompt and q is a letter to type into it; neither may be
	// read as navigation while the view is taking typed input.
	for name, open := range openPrompt {
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, runes("q")} {
			r := newRoot(t, "items")
			updated, _ := r.Update(open)
			r = updated.(*Root)

			updated, cmd := r.Update(key)
			r = updated.(*Root)

			if cmd != nil {
				if _, quit := cmd().(tea.QuitMsg); quit {
					t.Errorf("%v quit the program with %s open", key, name)
				}
			}
			// "open item" over "^t scope": the latter moved into the panel
			// behind "?" once the work item view's footer was trimmed to fit
			// 80 columns (see WorkItems.Keys), so it is no longer on r.View()
			// even while this view is legitimately on top.
			if !strings.Contains(r.View(), "open item") {
				t.Errorf("%v popped the view instead of going to %s:\n%s", key, name, r.View())
			}
		}
	}
}

func TestRootOpensWorkItemsFromTheMenuAfterStartingElsewhere(t *testing.T) {
	// boardwalk prs left the work item batch nil, and the menu handed that nil
	// slice to the view: the user was told their project had no work items, with
	// no error and no way to load them.
	c, _ := fixture()
	r := NewRoot(c, false, false, "prs")
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	r = updated.(*Root)

	updated, _ = r.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to the menu
	r = updated.(*Root)
	updated, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEnter}) // work items
	r = updated.(*Root)

	if cmd == nil {
		t.Fatal("opening work items from the menu did not fetch them")
	}
	view := r.View()
	if strings.Contains(view, "all 0") {
		t.Errorf("the view claimed the project has no work items:\n%s", view)
	}
	if !strings.Contains(view, "fetching work items") {
		t.Errorf("expected the fetching placeholder while the fetch is in flight:\n%s", view)
	}
}

// mustItems is the fixture's batch, for a test that opens work items partway
// through rather than through newRoot.
func mustItems(t *testing.T) []azdo.WorkItem {
	t.Helper()
	_, items := fixture()
	return items
}

func TestRootDeliversDataToAViewThatIsNotOnTop(t *testing.T) {
	// Builds fetches its timelines the instant the list lands, and enter is the
	// next thing a user does. Routing every message to the top of the stack
	// alone dropped the timeline on the log pane, leaving the build's in-flight
	// guard set forever and its step and error columns reading "…" for good.
	c, builds := buildFixture()
	r := NewRoot(c, false, false, "builds")
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	r = updated.(*Root)

	// The batch landing sets the in-flight guard for every eager row.
	updated, _ = r.Update(buildsMsg{Builds: builds})
	r = updated.(*Root)

	updated, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r = updated.(*Root)
	if cmd == nil {
		t.Fatal("enter on a build produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", cmd())
	}
	updated, _ = r.Update(push)
	r = updated.(*Root)
	if !strings.Contains(r.View(), "platform-ci #20260911.3") {
		t.Fatalf("the log pane is not on top:\n%s", r.View())
	}

	updated, _ = r.Update(timelineMsg{
		Build:    9001,
		Progress: azdo.Progress{CurrentStep: "Run tests"},
		Records:  []azdo.Record{{Name: "Run tests", Type: "Task", State: "inProgress", Order: 1, LogID: 7}},
	})
	r = updated.(*Root)

	// q from a drill-down goes back rather than quitting.
	updated, cmd = r.Update(runes("q"))
	r = updated.(*Root)
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("q from a drill-down quit the program instead of going back")
		}
	}

	view := r.View()
	if !strings.Contains(view, "builds (2)") {
		t.Fatalf("q did not return to the build list:\n%s", view)
	}
	if !strings.Contains(view, "Run tests") {
		t.Errorf("the timeline never reached the build list, which was not on top when it landed:\n%s", view)
	}
}

func TestRootBroadcastsErrMsgToTheViewItBelongsToEvenWhenNotOnTop(t *testing.T) {
	// Finding 2 from the branch review: refresh the pull request list, drill
	// into detail, press D to open the diff, and let the list's own fetch
	// fail. ErrMsg used to reach the top view alone, so the diff view — which
	// never emits one — printed the list's error over its own status line and
	// decremented its own work counter for a fetch that was never its, while
	// PullRequests, the view that actually asked, never heard back and spun
	// forever.
	c, prs := prFixture()
	r := NewRoot(c, false, false, "prs")
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	r = updated.(*Root)
	updated, _ = r.Update(prsMsg{PRs: prs})
	r = updated.(*Root)

	updated, _ = r.Update(PushMsg{View: NewPullRequestDetail(c, prs[0], []azdo.Thread{})})
	r = updated.(*Root)
	diff := NewPullRequestDiff(c, prs[0], nil, filterAll)
	updated, _ = r.Update(PushMsg{View: diff})
	r = updated.(*Root)

	updated, _ = r.Update(ErrMsg{Err: errTest})
	r = updated.(*Root)

	if status, isErr := diff.Status(); isErr || strings.Contains(status, errTest.Error()) {
		t.Errorf("the diff view reacted to another view's error: status = %q, isErr = %v", status, isErr)
	}
	prList, ok := r.stack[0].(*PullRequests)
	if !ok {
		t.Fatalf("stack[0] = %T, want *PullRequests", r.stack[0])
	}
	if status, isErr := prList.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("the pull request list never heard about its own fetch failing: status = %q, isErr = %v", status, isErr)
	}
	if prList.work.busy() {
		t.Error("the pull request list is still spinning after its failure landed")
	}
}

// TestListViewsAreConstructedOnlyByRootBuild guards the structural invariant
// the ErrMsg broadcast fix depends on: WorkItems, PullRequests and Builds each
// emit ErrMsg with no id naming which instance it belongs to, which is only
// safe to broadcast because exactly one instance of each is ever alive in a
// stack — every one is built once, by Root.build, from an empty stack (see
// NewRoot and the menu's enter handler in Root.key). A feature that pushed a
// second instance of one of these three views — a work item view reachable
// from inside another view's drill-down, say — would make a broadcast ErrMsg
// ambiguous between two live views and resurrect the very bug the broadcast
// was written to fix, silently.
//
// NewBuildsForPullRequest is the one exception, and it earns it by not
// relying on the invariant at all: a build list opened on a pull request's
// gates fetches through gateRunsMsg, which names its pull request, ignores
// the project listing, and reports its own failures rather than emitting an
// unowned ErrMsg — see the gate tests in builds_test.go. Its call to
// NewBuilds is skipped below for that reason, and nothing else may be.
//
// There is no runtime state that exercises this today — the risk is a future
// call site, not a reachable program state — so this checks the source itself
// rather than behaviour: every reference to the three constructors outside
// their own definitions must live in root.go.
func TestListViewsAreConstructedOnlyByRootBuild(t *testing.T) {
	constructors := []string{"NewWorkItems(", "NewPullRequests(", "NewBuilds("}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("could not read the package directory: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue // test fixtures build these views directly; that's expected.
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, ctor := range constructors {
				if !strings.Contains(line, ctor) {
					continue
				}
				if strings.Contains(line, "func "+ctor) {
					continue // the constructor's own definition, not a call site.
				}
				if name == "builds.go" && ctor == "NewBuilds(" && strings.Contains(line, "m := NewBuilds(c)") {
					continue // NewBuildsForPullRequest; see this test's doc.
				}
				if name != "root.go" {
					t.Errorf("%s:%d calls %s outside root.go — a second live instance of this view "+
						"would make the ErrMsg broadcast ambiguous between it and the original",
						name, i+1, strings.TrimSuffix(ctor, "("))
				}
			}
		}
	}
}

func TestRootKeepsTheStatusLineForTheViewOnTop(t *testing.T) {
	// A hidden view must not write the status line: the user would read it as
	// describing whatever is actually on screen.
	c, builds := buildFixture()
	r := NewRoot(c, false, false, "builds")
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	r = updated.(*Root)
	updated, _ = r.Update(buildsMsg{Builds: builds})
	r = updated.(*Root)

	logs := NewLogs(c, buildStub(), nil)
	updated, _ = r.Update(PushMsg{View: logs})
	r = updated.(*Root)

	updated, _ = r.Update(StatusMsg{Text: "a status for the top view"})
	r = updated.(*Root)
	updated, _ = r.Update(PopMsg{})
	r = updated.(*Root)

	// Asserted on the text rather than on the line being empty: a view that is
	// still fetching carries a spinner in its status line, which is its own
	// state and not something a hidden view was told.
	if status, _ := r.stack[0].Status(); strings.Contains(status, "a status for the top view") {
		t.Errorf("a hidden view took the status line: %q", status)
	}
}

func TestRootTogglesTheFullHelpPanel(t *testing.T) {
	r := newRoot(t, "items")

	// Asserted on the descriptions: the full panel pads keys into columns, so
	// the key and its description are not adjacent in the rendered text.
	line := r.View()
	if strings.Contains(line, "detail up") {
		t.Fatalf("the panel's keys are on the short line already:\n%s", line)
	}

	updated, _ := r.Update(runes("?"))
	r = updated.(*Root)

	open := r.View()
	for _, want := range []string{"detail up", "detail down", "quit", "branch"} {
		if !strings.Contains(open, want) {
			t.Errorf("the full panel is missing %q:\n%s", want, open)
		}
	}

	updated, _ = r.Update(runes("?"))
	r = updated.(*Root)
	if strings.Contains(r.View(), "detail up") {
		t.Error("? did not close the panel again")
	}
}

func TestRootGivesTheBodyLessRoomWhenTheHelpPanelOpens(t *testing.T) {
	// The panel is several rows tall. Sizing the body from a constant would
	// push the status line off the bottom of the terminal when it opened.
	r := newRoot(t, "items")
	closed := lipgloss.Height(r.View())

	updated, _ := r.Update(runes("?"))
	r = updated.(*Root)
	opened := lipgloss.Height(r.View())

	if opened != closed {
		t.Errorf("the frame is %d rows with the panel open and %d closed; it must stay the terminal's height", opened, closed)
	}
}

func TestRootLeavesTheHelpKeyToAnOpenPrompt(t *testing.T) {
	// "?" is a character someone can type into a branch name or a filter.
	for name, open := range openPrompt {
		r := newRoot(t, "items")
		updated, _ := r.Update(open)
		r = updated.(*Root)

		updated, _ = r.Update(runes("?"))
		r = updated.(*Root)

		if r.help.ShowAll {
			t.Errorf("? opened the help panel instead of reaching %s", name)
		}
	}
}
