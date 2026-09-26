package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// paletteEntry is one thing the palette can run.
type paletteEntry struct {
	// id is what history remembers: "nav:prs", "act:refresh".
	id string
	// title is what the fuzzy filter matches and the row shows.
	title string
	// hint is the key the entry stands for, so running it from the palette
	// teaches the key.
	hint string
	// recent marks an entry that history put at the top.
	recent bool
	// msg is what running the entry sends back through Root.Update: the
	// binding's own key for an action, a goToMsg for navigation.
	msg tea.Msg
}

// paletteResult is what a key did to the palette.
type paletteResult int

const (
	paletteOpen paletteResult = iota
	paletteClosed
	paletteChosen
)

// paletteMaxWidth keeps the box readable on a wide terminal: the entries are
// a few words each, and a box the width of the screen leaves the keys a long
// way from the names they belong to.
const paletteMaxWidth = 72

// paletteChrome is the rows the box spends on anything but entries: two
// borders, the heading, the query, and the gap under it.
const paletteChrome = 5

// Palette is the command palette: a query over the entries the screen offers,
// ranked by the same fuzzy matcher the list views' "/" filter uses.
type Palette struct {
	toggle  key.Binding
	input   textinput.Model
	entries []paletteEntry
	// targets are the entries' titles, kept beside them so filtering does not
	// rebuild the slice on every keystroke.
	targets []string
	// shown is the entries on screen, in order. An empty query shows them all
	// with no matches; a query shows the fuzzy ranks.
	shown  []list.Rank
	cursor int
}

// newPalette orders the entries recent first, in the order history has them,
// then everything else in the order given. A history id the screen does not
// offer is skipped: an action remembered from another view would do nothing
// here.
func newPalette(toggle key.Binding, entries []paletteEntry, recent []string) *Palette {
	byID := make(map[string]int, len(entries))
	for i, e := range entries {
		byID[e.id] = i
	}

	ordered := make([]paletteEntry, 0, len(entries))
	taken := make([]bool, len(entries))
	for _, id := range recent {
		if i, ok := byID[id]; ok && !taken[i] {
			taken[i] = true
			e := entries[i]
			e.recent = true
			ordered = append(ordered, e)
		}
	}
	for i, e := range entries {
		if !taken[i] {
			ordered = append(ordered, e)
		}
	}

	targets := make([]string, len(ordered))
	for i, e := range ordered {
		targets[i] = e.title
	}

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "type to filter"
	// A static cursor needs no blink messages, which would otherwise be
	// broadcast to every view on the stack twice a second.
	input.Cursor.SetMode(cursor.CursorStatic)
	input.Focus()

	p := &Palette{toggle: toggle, input: input, entries: ordered, targets: targets}
	p.filter()
	return p
}

// filter recomputes what is shown from the query.
func (p *Palette) filter() {
	q := p.input.Value()
	if q == "" {
		p.shown = p.shown[:0]
		for i := range p.entries {
			p.shown = append(p.shown, list.Rank{Index: i})
		}
	} else {
		// DefaultFilter sorts stably by score, so recent entries — which come
		// first in targets — win a tie.
		p.shown = list.DefaultFilter(q, p.targets)
	}
	p.cursor = min(p.cursor, max(0, len(p.shown)-1))
}

// Update handles a key, reporting whether the palette is still open, was
// closed, or chose an entry.
func (p *Palette) Update(msg tea.KeyMsg) (paletteResult, paletteEntry) {
	// A printable toggle key is typed into the query instead: someone who
	// bound the palette to ":" still needs to be able to search for one.
	if msg.Type != tea.KeyRunes && key.Matches(msg, p.toggle) {
		return paletteClosed, paletteEntry{}
	}

	switch msg.String() {
	case "esc":
		return paletteClosed, paletteEntry{}
	case "enter":
		if len(p.shown) == 0 {
			return paletteOpen, paletteEntry{}
		}
		return paletteChosen, p.entries[p.shown[p.cursor].Index]
	case "up", "ctrl+k", "ctrl+p":
		p.cursor = max(0, p.cursor-1)
		return paletteOpen, paletteEntry{}
	case "down", "ctrl+j", "ctrl+n":
		p.cursor = min(max(0, len(p.shown)-1), p.cursor+1)
		return paletteOpen, paletteEntry{}
	}

	before := p.input.Value()
	p.input, _ = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor = 0
		p.filter()
	}
	return paletteOpen, paletteEntry{}
}

var (
	paletteBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colCoral).
			Padding(0, 1)
	paletteMatch = lipgloss.NewStyle().Foreground(colCoral).Underline(true)
)

// View renders the box at most width by height. subtitle names the screen
// the entries belong to.
func (p *Palette) View(width, height int, subtitle string) string {
	boxWidth := max(20, min(paletteMaxWidth, width-2))
	inner := boxWidth - 4 // borders and padding
	p.input.Width = max(1, inner-lipgloss.Width(p.input.Prompt)-1)

	heading := "commands"
	if subtitle != "" {
		heading += " · " + subtitle
	}

	var b strings.Builder
	b.WriteString(chromeStyle.Render(ansi.Truncate(heading, inner, "…")))
	b.WriteByte('\n')
	b.WriteString(p.input.View())
	b.WriteString("\n\n")

	rows := max(1, height-paletteChrome)
	if len(p.shown) == 0 {
		b.WriteString(chromeStyle.Render("nothing matches"))
	}
	offset := max(0, p.cursor-rows+1)
	end := min(len(p.shown), offset+rows)
	for i := offset; i < end; i++ {
		if i > offset {
			b.WriteByte('\n')
		}
		b.WriteString(p.row(p.shown[i], i == p.cursor, inner))
	}

	return paletteBox.Width(boxWidth - 2).Render(b.String())
}

// row renders one entry: its title with the matched characters marked, and
// its key on the right.
func (p *Palette) row(r list.Rank, selected bool, width int) string {
	e := p.entries[r.Index]

	hint := e.hint
	if e.recent {
		hint = "recent · " + hint
	}
	hint = chromeStyle.Render(hint)

	marker, style := "  ", normalRow
	if selected {
		marker, style = "▸ ", selectedRow
	}

	room := max(1, width-lipgloss.Width(marker)-lipgloss.Width(hint)-1)
	title := highlight(ansi.Truncate(e.title, room, "…"), r.MatchedIndexes, style)
	gap := max(1, width-lipgloss.Width(marker)-lipgloss.Width(title)-lipgloss.Width(hint))

	return fmt.Sprintf("%s%s%s%s", style.Render(marker), title, strings.Repeat(" ", gap), hint)
}

// highlight renders s in style, with the bytes at matched marked. The fuzzy
// matcher reports byte offsets, which is what ranging over a string yields.
func highlight(s string, matched []int, style lipgloss.Style) string {
	if len(matched) == 0 {
		return style.Render(s)
	}
	var b strings.Builder
	next := 0
	start := 0
	for i, r := range s {
		for next < len(matched) && matched[next] < i {
			next++
		}
		if next < len(matched) && matched[next] == i {
			if start < i {
				b.WriteString(style.Render(s[start:i]))
			}
			end := i + utf8.RuneLen(r)
			b.WriteString(paletteMatch.Inherit(style).Render(s[i:end]))
			start = end
		}
	}
	if start < len(s) {
		b.WriteString(style.Render(s[start:]))
	}
	return b.String()
}
