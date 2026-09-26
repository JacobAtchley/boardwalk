package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func testEntries() []paletteEntry {
	return []paletteEntry{
		{id: "nav:items", title: "go to work items", hint: "go to"},
		{id: "nav:prs", title: "go to pull requests", hint: "go to"},
		{id: "act:refresh", title: "refresh", hint: "r"},
		{id: "act:branch", title: "branch", hint: "b"},
	}
}

func testPalette(recent ...string) *Palette {
	toggle := key.NewBinding(key.WithKeys("ctrl+p"))
	return newPalette(toggle, testEntries(), recent)
}

func typeInto(p *Palette, s string) {
	for _, r := range s {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func shownIDs(p *Palette) []string {
	ids := make([]string, len(p.shown))
	for i, r := range p.shown {
		ids[i] = p.entries[r.Index].id
	}
	return ids
}

func TestPaletteListsRecentEntriesFirst(t *testing.T) {
	// A history id the screen does not offer is dropped, not shown dead.
	p := testPalette("act:branch", "act:gone", "nav:prs")

	got := strings.Join(shownIDs(p), ",")
	if want := "act:branch,nav:prs,nav:items,act:refresh"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	if !p.entries[p.shown[0].Index].recent || p.entries[p.shown[2].Index].recent {
		t.Error("recent flags are wrong")
	}
}

func TestPaletteFuzzyFiltersAsYouType(t *testing.T) {
	p := testPalette()
	typeInto(p, "gpr")

	if got := shownIDs(p); len(got) != 1 || got[0] != "nav:prs" {
		t.Errorf("filtered = %v, want only nav:prs", got)
	}

	p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(p.shown) != 4 {
		t.Errorf("clearing the query shows %d entries, want 4", len(p.shown))
	}
}

func TestPaletteEnterChoosesTheHighlightedEntry(t *testing.T) {
	p := testPalette()
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p.Update(tea.KeyMsg{Type: tea.KeyDown})

	res, e := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if res != paletteChosen || e.id != "act:refresh" {
		t.Errorf("enter = %v %q, want chosen act:refresh", res, e.id)
	}
}

func TestPaletteCursorStaysInsideTheList(t *testing.T) {
	p := testPalette()
	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 0 {
		t.Errorf("up from the top: cursor = %d", p.cursor)
	}
	for range 10 {
		p.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if p.cursor != 3 {
		t.Errorf("down past the end: cursor = %d", p.cursor)
	}

	// Narrowing the list pulls the cursor back onto it.
	typeInto(p, "branch")
	if p.cursor != 0 {
		t.Errorf("after filtering cursor = %d, want 0", p.cursor)
	}
}

func TestPaletteEnterWithNothingMatchingChoosesNothing(t *testing.T) {
	p := testPalette()
	typeInto(p, "zzzz")
	if res, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); res != paletteOpen {
		t.Errorf("enter on an empty list = %v, want the palette to stay open", res)
	}
}

func TestPaletteClosesOnEscAndOnItsOwnKey(t *testing.T) {
	if res, _ := testPalette().Update(tea.KeyMsg{Type: tea.KeyEsc}); res != paletteClosed {
		t.Errorf("esc = %v, want closed", res)
	}
	if res, _ := testPalette().Update(tea.KeyMsg{Type: tea.KeyCtrlP}); res != paletteClosed {
		t.Errorf("ctrl+p = %v, want closed", res)
	}
}

func TestPaletteTypesAPrintableToggleKeyInsteadOfClosing(t *testing.T) {
	p := newPalette(key.NewBinding(key.WithKeys(":")), testEntries(), nil)
	if res, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}}); res != paletteOpen {
		t.Errorf(": = %v, want it typed", res)
	}
	if p.input.Value() != ":" {
		t.Errorf("query = %q, want \":\"", p.input.Value())
	}
}

func TestPaletteViewShowsEntriesAndTheirKeys(t *testing.T) {
	p := testPalette("act:branch")
	view := ansi.Strip(p.View(100, 20, "work items"))

	for _, want := range []string{"commands", "work items", "branch", "recent", "go to pull requests", "refresh"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 100 {
			t.Errorf("line is %d wide, want at most 100: %q", w, line)
		}
	}
}

func TestPaletteViewScrollsToKeepTheCursorVisible(t *testing.T) {
	entries := make([]paletteEntry, 30)
	for i := range entries {
		entries[i] = paletteEntry{id: string(rune('a' + i)), title: "entry " + string(rune('A'+i)), hint: "x"}
	}
	p := newPalette(key.NewBinding(key.WithKeys("ctrl+p")), entries, nil)
	for range 29 {
		p.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := ansi.Strip(p.View(80, 12, ""))
	if !strings.Contains(view, "entry "+string(rune('A'+29))) {
		t.Errorf("the selected last entry is off screen:\n%s", view)
	}
	if h := strings.Count(view, "\n") + 1; h > 12 {
		t.Errorf("view is %d rows, want at most 12", h)
	}
}
