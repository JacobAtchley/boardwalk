package ui

import (
	"os"
	"strings"
	"testing"
)

func TestHighlightKeepsOneEntryPerLine(t *testing.T) {
	// Interleaving diff markers and review comments means indexing the
	// highlighted output by line, so it has to come back with exactly as many
	// lines as it went in with — no more, no fewer, whatever the lexer made
	// of the source.
	src := "package x\n\n/* a comment\n   over three\n   lines */\nfunc f() {}\n"
	got := highlightLines(src, "x.go")

	if want := len(splitLines(src)); len(got) != want {
		t.Fatalf("highlighted %d lines, want %d:\n%q", len(got), want, got)
	}
	if !strings.Contains(got[5], "func f() {}") {
		t.Errorf("line 6 = %q, want the source text in it", got[5])
	}
}

func TestHighlightLeavesTheTextIntact(t *testing.T) {
	// Whatever styling is or is not applied, the code itself has to survive:
	// a pane that silently drops a character is worse than one with no
	// colour at all.
	src := "func f(a int) (int, error) {\n\treturn a * 2, nil\n}\n"
	for i, l := range highlightLines(src, "x.go") {
		want := splitLines(src)[i]
		if stripANSI(l) != want {
			t.Errorf("line %d = %q, want %q once the styling is removed", i, stripANSI(l), want)
		}
	}
}

// TestHighlightOfAnUnknownExtension — a file chroma has no lexer for must
// come back as itself rather than being guessed at or dropped.
func TestHighlightOfAnUnknownExtension(t *testing.T) {
	src := "some notes\nand more\n"
	got := highlightLines(src, "notes.unknownext")

	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(got), got)
	}
	for i, l := range got {
		if l != splitLines(src)[i] {
			t.Errorf("line %d = %q, want it untouched", i, l)
		}
	}
}

func TestHighlightOfAnEmptyFile(t *testing.T) {
	if got := highlightLines("", "x.go"); len(got) != 0 {
		t.Errorf("got %d lines for an empty file, want none: %q", len(got), got)
	}
}

// TestHighlightStylePicksForTheTerminal mirrors markdownStyle: the light and
// dark answers come from the value Bubble Tea warms before the program takes
// the terminal, and not being a terminal means no styling at all — which is
// also why the tests above can compare against the raw source.
func TestHighlightStylePicksForTheTerminal(t *testing.T) {
	if got := highlightStyle(false, true); got != "" {
		t.Errorf("highlightStyle(notty) = %q, want no style at all", got)
	}
	dark, light := highlightStyle(true, true), highlightStyle(true, false)
	if dark == "" || light == "" {
		t.Fatalf("a terminal got no style: dark=%q light=%q", dark, light)
	}
	if dark == light {
		t.Errorf("light and dark terminals share the style %q", dark)
	}
}

// TestHighlightNeverQueriesTheTerminal guards the same trap the glamour fix
// closed: a styling library that asks the terminal what colour it is, from
// under Bubble Tea, blocks until termenv gives up. chroma is handed the
// answer instead of being left to find it.
func TestHighlightNeverQueriesTheTerminal(t *testing.T) {
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
			// Comments are where the trap is explained, and explaining it is
			// the opposite of falling into it.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "termenv.") || strings.Contains(line, "WithAutoStyle") {
				t.Errorf("%s:%d asks the terminal about itself; take the answer from "+
					"lipgloss instead, which Bubble Tea warms before the program starts",
					name, i+1)
			}
		}
	}
}

// stripANSI removes the escape sequences a styled line carries, so a test can
// compare the text underneath.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // the m itself
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
