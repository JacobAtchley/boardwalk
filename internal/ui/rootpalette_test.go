package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/history"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ctrlP = tea.KeyMsg{Type: tea.KeyCtrlP}

// send sends each message to r in turn.
func send(t *testing.T, r *Root, msgs ...tea.Msg) (*Root, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, m := range msgs {
		var updated tea.Model
		updated, cmd = r.Update(m)
		r = updated.(*Root)
	}
	return r, cmd
}

// search opens the palette, types q, and runs the top match.
func search(t *testing.T, r *Root, q string) (*Root, tea.Cmd) {
	t.Helper()
	msgs := []tea.Msg{ctrlP}
	for _, c := range q {
		msgs = append(msgs, runes(string(c)))
	}
	return send(t, r, append(msgs, tea.KeyMsg{Type: tea.KeyEnter})...)
}

func TestRootOpensThePaletteFromTheMenu(t *testing.T) {
	r, _ := send(t, newRoot(t, ""), ctrlP)

	view := r.View()
	for _, want := range []string{"commands", "go to work items", "go to pull requests", "go to builds"} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette is missing %q:\n%s", want, view)
		}
	}
}

func TestRootPaletteGoesToAMainView(t *testing.T) {
	r, cmd := search(t, newRoot(t, ""), "pull req")

	if r.palette != nil {
		t.Error("the palette stayed open after running an entry")
	}
	if _, ok := r.stack[0].(*PullRequests); !ok || len(r.stack) != 1 {
		t.Errorf("stack = %T, want only pull requests", r.stack)
	}
	if cmd == nil {
		t.Error("going to pull requests did not fetch them")
	}
}

func TestRootPaletteGoToCutsBackToAViewAlreadyOpen(t *testing.T) {
	// Going to the view the stack is rooted in keeps what it loaded and where
	// the cursor was, rather than fetching it all again.
	r := newRoot(t, "items")
	items := r.stack[0]
	r, _ = send(t, r, PushMsg{View: NewLogs(r.client, buildStub(), nil)})

	r, _ = search(t, r, "go to work")

	if len(r.stack) != 1 || r.stack[0] != items {
		t.Errorf("stack = %v, want the original work item view alone", r.stack)
	}
}

func TestRootPaletteGoToReplacesTheStack(t *testing.T) {
	r := newRoot(t, "items")
	r, _ = send(t, r, PushMsg{View: NewLogs(r.client, buildStub(), nil)})

	r, _ = search(t, r, "go to builds")

	if _, ok := r.stack[0].(*Builds); !ok || len(r.stack) != 1 {
		t.Errorf("stack = %T, want only builds", r.stack)
	}
}

func TestRootPaletteListsTheViewsActions(t *testing.T) {
	r, _ := send(t, newRoot(t, "items"), ctrlP)

	view := r.View()
	for _, want := range []string{"branch", "set active", "refresh", "copy id", "open item"} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette is missing the %q action:\n%s", want, view)
		}
	}
	// "?" opens the panel of keys; from the palette it would be a list of the
	// same entries, one level removed.
	for _, e := range r.palette.entries {
		if e.id == "act:keys" {
			t.Error("the help panel is offered as an action")
		}
	}
}

func TestRootPaletteRunsAnActionByItsKey(t *testing.T) {
	// "back" is esc: run from the palette it must do exactly what esc does.
	r, _ := search(t, newRoot(t, "items"), "back")

	if len(r.stack) != 0 {
		t.Errorf("back from the palette left %d views open", len(r.stack))
	}
}

func TestRootPaletteRemembersWhatItRan(t *testing.T) {
	h := history.Open("", 10)
	c, _ := fixture()
	r := NewRoot(c, false, false, "", WithHistory(h))
	r, _ = send(t, r, tea.WindowSizeMsg{Width: 140, Height: 30})

	r, _ = search(t, r, "go to builds")
	r, _ = search(t, r, "refresh")

	if got, want := h.Recent(), []string{"act:refresh", "nav:builds"}; !slices.Equal(got, want) {
		t.Errorf("history = %v, want %v", got, want)
	}

	// Back on the menu, builds is offered first; refresh is not, since the
	// menu has nothing to refresh.
	r, _ = send(t, r, tea.KeyMsg{Type: tea.KeyEsc}, ctrlP)
	first := r.palette.entries[r.palette.shown[0].Index]
	if first.id != "nav:builds" || !first.recent {
		t.Errorf("first entry = %+v, want the recent go to builds", first)
	}
}

func TestRootPaletteKeyIsConfigurable(t *testing.T) {
	c, _ := fixture()
	r := NewRoot(c, false, false, "items", WithPaletteKey("ctrl+k"))
	r, _ = send(t, r, tea.WindowSizeMsg{Width: 140, Height: 30})

	if r, _ = send(t, r, ctrlP); r.palette != nil {
		t.Error("ctrl+p opened the palette though it was rebound")
	}
	if r, _ = send(t, r, tea.KeyMsg{Type: tea.KeyCtrlK}); r.palette == nil {
		t.Error("the configured key did not open the palette")
	}
}

func TestRootLeavesThePaletteKeyToAnOpenPrompt(t *testing.T) {
	for name, open := range openPrompt {
		r, _ := send(t, newRoot(t, "items"), open, ctrlP)
		if r.palette != nil {
			t.Errorf("the palette opened over %s", name)
		}
	}
}

func TestRootCtrlCQuitsWithThePaletteOpen(t *testing.T) {
	_, cmd := send(t, newRoot(t, "items"), ctrlP, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c produced %T, want a quit", cmd())
	}
}

func TestRootPaletteOwnsEveryKeyWhileOpen(t *testing.T) {
	// q is a letter to search for, not a way out of the program.
	r, cmd := send(t, newRoot(t, "items"), ctrlP, runes("q"))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit the program with the palette open")
		}
	}
	if r.palette == nil || r.palette.input.Value() != "q" {
		t.Error("q was not typed into the palette")
	}

	r, _ = send(t, r, tea.KeyMsg{Type: tea.KeyEsc})
	if r.palette != nil || len(r.stack) != 1 {
		t.Error("esc should close the palette and nothing else")
	}
}

func TestRootHelpPanelNamesThePaletteKey(t *testing.T) {
	r, _ := send(t, newRoot(t, "items"), runes("?"))
	if !strings.Contains(r.View(), "commands") {
		t.Errorf("the help panel does not mention the palette:\n%s", r.View())
	}
}

func TestRootFrameKeepsItsHeightWithThePaletteOpen(t *testing.T) {
	r := newRoot(t, "items")
	closed := lipgloss.Height(r.View())

	r, _ = send(t, r, ctrlP)
	if opened := lipgloss.Height(r.View()); opened != closed {
		t.Errorf("the frame is %d rows with the palette open and %d closed", opened, closed)
	}
}

func TestCheckPaletteKey(t *testing.T) {
	for _, ok := range []string{"ctrl+p", "ctrl+k", ":", "alt+p", "f1"} {
		if err := CheckPaletteKey(ok); err != nil {
			t.Errorf("CheckPaletteKey(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ctrl+banana", "esc", "q", "?", "ctrl+c", "enter"} {
		if err := CheckPaletteKey(bad); err == nil {
			t.Errorf("CheckPaletteKey(%q) accepted it", bad)
		}
	}
}

func TestPaletteBindingSpellsControlKeysLikeTheFooter(t *testing.T) {
	if got := paletteBinding("ctrl+k").Help().Key; got != "^k" {
		t.Errorf("help key = %q, want ^k", got)
	}
	if got := paletteBinding(":").Help().Key; got != ":" {
		t.Errorf("help key = %q, want :", got)
	}
}
