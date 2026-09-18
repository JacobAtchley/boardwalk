package ui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// renderers are cached per width. Building one compiles a style, and the item
// view lays out a description, an acceptance criteria list and every comment on
// each relayout.
var (
	renderMu  sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}
)

// renderMarkdown styles a markdown field for a pane of the given width, falling
// back to the text itself if glamour cannot render it — a pane showing the raw
// source is worse than one showing nothing, but not as bad as an empty pane
// with no explanation.
func renderMarkdown(text, empty string, width int) string {
	if strings.TrimSpace(text) == "" {
		text = empty
	}

	r, err := rendererFor(width)
	if err != nil {
		return wordwrap(text, width)
	}

	out, err := r.Render(text)
	if err != nil {
		return wordwrap(text, width)
	}
	// Glamour pads a block with blank lines above and below. Inside a pane that
	// already spaces its sections, that reads as a gap rather than as breathing
	// room.
	return strings.Trim(out, "\n")
}

func rendererFor(width int) (*glamour.TermRenderer, error) {
	renderMu.Lock()
	defer renderMu.Unlock()

	if r, ok := renderers[width]; ok {
		return r, nil
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(markdownStyle(
			term.IsTerminal(int(os.Stdout.Fd())), lipgloss.HasDarkBackground())),
		glamour.WithWordWrap(max(20, width)),
	)
	if err != nil {
		return nil, err
	}
	renderers[width] = r
	return r, nil
}

// markdownStyle names the glamour style to render with: the same three-way
// choice glamour.WithAutoStyle makes, but made from answers this process
// already has rather than from a fresh question to the terminal.
//
// "auto" is what this used to pass, and it cost five seconds. It resolves
// light-versus-dark by calling termenv.HasDarkBackground, which writes an
// OSC 11 query to the terminal and then reads the reply back off stdin — but
// the first markdown a session renders is in a detail pane, long after Bubble
// Tea took the terminal, so Bubble Tea's input reader gets the reply and the
// query sits until termenv.OSCTimeout gives up. It happens on the render
// goroutine, so the whole interface is frozen for it, and it happens again on
// the first render at every new width, because renderers are cached per width.
//
// Bubble Tea already warms the same answer in lipgloss from an init function,
// before any program can take the terminal, precisely so that this cannot
// happen (see bubbletea/tea_init.go); lipgloss caches it. Asking lipgloss is
// therefore free and gives the same verdict the query would have.
func markdownStyle(tty, dark bool) string {
	switch {
	case !tty:
		return styles.NoTTYStyle
	case dark:
		return styles.DarkStyle
	default:
		return styles.LightStyle
	}
}
