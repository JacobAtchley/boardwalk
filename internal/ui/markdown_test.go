package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/styles"
)

// TestMarkdownNeverAsksGlamourToDetectTheBackground guards the fix for the
// stall that made the first detail view opened in a session take five seconds
// to paint.
//
// glamour.WithAutoStyle resolves light-versus-dark by calling
// termenv.HasDarkBackground, which writes an OSC 11 query to the terminal and
// reads the reply back off stdin. Bubble Tea has owned stdin since the program
// started, so it swallows the reply and the query waits out
// termenv.OSCTimeout — five seconds, on the render goroutine, with the whole
// interface frozen. Nothing in the rendered output says it happened, so this
// checks the source rather than behaviour: a call site is the only way the
// stall can come back.
func TestMarkdownNeverAsksGlamourToDetectTheBackground(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("could not read the package directory: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "glamour.WithAutoStyle(") {
				t.Errorf("%s:%d calls glamour.WithAutoStyle — it queries the terminal from "+
					"under Bubble Tea and blocks for termenv.OSCTimeout; pick the style from "+
					"lipgloss.HasDarkBackground instead, which Bubble Tea warms before the "+
					"program takes the terminal", name, i+1)
			}
		}
	}
}

func TestMarkdownStylePicksTheStyleForTheTerminal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tty, dark bool
		want      string
	}{
		{"dark terminal", true, true, styles.DarkStyle},
		{"light terminal", true, false, styles.LightStyle},
		// Not a terminal at all — a test run, or output redirected to a file.
		// Styling it would write escape codes nothing is going to interpret,
		// which is what "auto" avoided and what this has to keep avoiding.
		{"no terminal", false, true, styles.NoTTYStyle},
		{"no terminal, light", false, false, styles.NoTTYStyle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdownStyle(tc.tty, tc.dark); got != tc.want {
				t.Errorf("markdownStyle(%v, %v) = %q, want %q", tc.tty, tc.dark, got, tc.want)
			}
		})
	}
}

// TestRenderMarkdownStillRenders is the behavioural half: whatever style is
// chosen, the pane gets the text rather than an empty string.
//
// A test run is not a terminal, so what comes back here is the "notty" style,
// which passes the emphasis markers through untouched — the same output "auto"
// produced under test before the style was chosen explicitly.
func TestRenderMarkdownStillRenders(t *testing.T) {
	out := renderMarkdown("a **bold** word", "_none_", 60)
	if !strings.Contains(out, "bold") {
		t.Errorf("rendered description lost its text: %q", out)
	}

	empty := renderMarkdown("   ", "nothing here", 60)
	if !strings.Contains(empty, "nothing here") {
		t.Errorf("empty description did not fall back to its placeholder: %q", empty)
	}
}
