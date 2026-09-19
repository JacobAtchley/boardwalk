package ui

import (
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Syntax highlighting for the file view.
//
// The diff pane colours whole lines by what the change did to them, which is
// the right answer there — three lines of context either side is not enough
// code to be worth reading as code. A whole file is, so it is coloured as
// source and the change is marked in the gutter instead.

// highlightLines styles a file and returns it one line per entry.
//
// Per line rather than as one string because the file view interleaves: a
// removed line goes between two of these, and a review comment goes under
// one. Chroma re-emits its style at the start of every line and closes it
// with a reset, even in the middle of a token spanning several — so splitting
// its output is safe, and a block comment's third line keeps its colour.
//
// A file chroma has no lexer for comes back as itself. Guessing at a language
// makes a mess of one that is not it, and plain text is what a reviewer had
// before this existed.
func highlightLines(text, path string) []string {
	lines := splitLines(text)
	if len(lines) == 0 {
		return nil
	}

	style := highlightStyle(term.IsTerminal(int(os.Stdout.Fd())), lipgloss.HasDarkBackground())
	if style == "" {
		return lines
	}

	lexer := lexers.Match(path)
	if lexer == nil {
		return lines
	}

	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return lines
	}

	var out strings.Builder
	if err := formatters.Get("terminal256").Format(&out, styles.Get(style), it); err != nil {
		return lines
	}

	got := splitLines(out.String())
	// A lexer that somehow changed the line count would put every marker and
	// every comment against the wrong line, which reads as a confidently
	// wrong pane rather than as a broken one. The unstyled text is the
	// better failure.
	if len(got) != len(lines) {
		return lines
	}
	return got
}

// highlightStyle names the chroma style to use, or an empty string for no
// highlighting at all.
//
// The light-versus-dark answer comes from lipgloss, for the reason
// markdownStyle's does: asking the terminal directly means an OSC query from
// under Bubble Tea, which blocks until termenv's timeout. Bubble Tea warms
// lipgloss's copy before any program can take the terminal, and lipgloss
// caches it.
//
// Not a terminal means no styling, which matches what glamour does with the
// same question and keeps a redirected file, and a test run, as plain text.
func highlightStyle(tty, dark bool) string {
	switch {
	case !tty:
		return ""
	case dark:
		return "monokai"
	default:
		return "github"
	}
}
