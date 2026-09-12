package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
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
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(max(20, width)),
	)
	if err != nil {
		return nil, err
	}
	renderers[width] = r
	return r, nil
}
