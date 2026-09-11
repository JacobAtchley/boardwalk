package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
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

	if !strings.Contains(r.View(), "pull requests") || strings.Contains(r.View(), "^t mine/all") {
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
		t.Error("the hint line is missing")
	}
}

func TestRootShellCommandQuits(t *testing.T) {
	r := newRoot(t, "items")

	updated, cmd := r.Update(ShellCommandMsg{Command: "git checkout feature/x"})
	r = updated.(*Root)

	if r.ShellCommand() != "git checkout feature/x" {
		t.Errorf("ShellCommand = %q", r.ShellCommand())
	}
	if cmd == nil {
		t.Fatal("a shell command did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("a shell command produced %T, want a quit", cmd())
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
			if !strings.Contains(r.View(), "^t mine/all") {
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
