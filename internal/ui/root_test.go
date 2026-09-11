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
	r := NewRoot(c, items, false, start)
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
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
	r = updated.(*Root)

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
