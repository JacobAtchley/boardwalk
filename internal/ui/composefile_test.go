package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aymanbagabas/go-udiff"
)

// show renders a composed file the way an assertion reads best: one line per
// row, gutter and both numbers, so a wrong number is visible in the failure
// rather than having to be worked out from it.
func show(lines []fileLine) string {
	var b strings.Builder
	for _, l := range lines {
		mark := " "
		switch l.Kind {
		case udiff.Insert:
			mark = "+"
		case udiff.Delete:
			mark = "-"
		}
		old, new := "  ", "  "
		if l.Old > 0 {
			old = fmt.Sprintf("%2d", l.Old)
		}
		if l.New > 0 {
			new = fmt.Sprintf("%2d", l.New)
		}
		fmt.Fprintf(&b, "%s %s %s %s\n", mark, old, new, l.Text)
	}
	return b.String()
}

// TestComposeFileWithNoHunks — a file the pull request lists as changed but
// whose text is identical at both commits, and the plain case underneath
// every other: every line is context, numbered from one.
func TestComposeFileWithNoHunks(t *testing.T) {
	got := composeFile("alpha\nbeta\ngamma\n", nil)

	want := `   1  1 alpha
   2  2 beta
   3  3 gamma
`
	if show(got) != want {
		t.Errorf("composed:\n%s\nwant:\n%s", show(got), want)
	}
}

// TestComposeFilePlacesInsertionsAndDeletions is the one that matters. The
// hunk covers new lines 2 to 4 and removes an old line 2 while adding two: the
// two sides advance at different rates, and a row numbered from the wrong one
// puts a + against code nobody touched.
func TestComposeFilePlacesInsertionsAndDeletions(t *testing.T) {
	text := "func tail() {\n\tdefer close(ch)\n\tgo poll(ctx)\n}\n"
	hunks := []*udiff.Hunk{hunk(1, 1,
		line(udiff.Equal, "func tail() {\n"),
		line(udiff.Delete, "\tgo poll()\n"),
		line(udiff.Insert, "\tdefer close(ch)\n"),
		line(udiff.Insert, "\tgo poll(ctx)\n"),
		line(udiff.Equal, "}\n"),
	)}

	want := `   1  1 func tail() {
-  2    	go poll()
+     2 	defer close(ch)
+     3 	go poll(ctx)
   3  4 }
`
	if got := show(composeFile(text, hunks)); got != want {
		t.Errorf("composed:\n%s\nwant:\n%s", got, want)
	}
}

// TestComposeFileKeepsUntouchedLinesOutsideTheHunks — the whole point of a
// full-file view. The hunk covers three lines in the middle; everything
// before and after it is still the file and still numbered correctly.
func TestComposeFileKeepsUntouchedLinesOutsideTheHunks(t *testing.T) {
	text := "one\ntwo\nCHANGED\nfour\nfive\n"
	hunks := []*udiff.Hunk{hunk(3, 3,
		line(udiff.Delete, "three\n"),
		line(udiff.Insert, "CHANGED\n"),
	)}

	want := `   1  1 one
   2  2 two
-  3    three
+     3 CHANGED
   4  4 four
   5  5 five
`
	if got := show(composeFile(text, hunks)); got != want {
		t.Errorf("composed:\n%s\nwant:\n%s", got, want)
	}
}

// TestComposeFileWithTwoHunks — the numbering has to survive a gap. The
// second hunk's new-side start is what says where it lands, and drifting by
// the first hunk's net change is the classic way to get this wrong.
func TestComposeFileWithTwoHunks(t *testing.T) {
	text := "A+\nb\nc\nd\ne\nF+\n"
	hunks := []*udiff.Hunk{
		hunk(1, 1, line(udiff.Insert, "A+\n")),
		hunk(5, 6, line(udiff.Insert, "F+\n")),
	}

	want := `+     1 A+
   1  2 b
   2  3 c
   3  4 d
   4  5 e
+     6 F+
`
	if got := show(composeFile(text, hunks)); got != want {
		t.Errorf("composed:\n%s\nwant:\n%s", got, want)
	}
}

// TestComposeFileWithADeletionAtTheEnd — a deletion after the last surviving
// line has no following line to splice in front of, and must not be dropped.
func TestComposeFileWithADeletionAtTheEnd(t *testing.T) {
	text := "kept\n"
	hunks := []*udiff.Hunk{hunk(1, 1,
		line(udiff.Equal, "kept\n"),
		line(udiff.Delete, "gone\n"),
	)}

	want := `   1  1 kept
-  2    gone
`
	if got := show(composeFile(text, hunks)); got != want {
		t.Errorf("composed:\n%s\nwant:\n%s", got, want)
	}
}

// TestComposeFileOfADeletedFile — no new side at all. Every line is a
// removal, numbered on the old side only.
func TestComposeFileOfADeletedFile(t *testing.T) {
	hunks := []*udiff.Hunk{hunk(1, 1,
		line(udiff.Delete, "one\n"),
		line(udiff.Delete, "two\n"),
	)}

	want := `-  1    one
-  2    two
`
	if got := show(composeFile("", hunks)); got != want {
		t.Errorf("composed:\n%s\nwant:\n%s", got, want)
	}
}

// TestComposeFileWithNoTrailingNewline — a last line without one must not
// become a phantom empty row at the bottom of every file.
func TestComposeFileWithNoTrailingNewline(t *testing.T) {
	got := composeFile("alpha\nbeta", nil)
	if len(got) != 2 {
		t.Fatalf("composed %d lines, want 2:\n%s", len(got), show(got))
	}
	if got[1].Text != "beta" {
		t.Errorf("last line = %q, want %q", got[1].Text, "beta")
	}
}

func TestComposeFileOfAnEmptyFile(t *testing.T) {
	if got := composeFile("", nil); len(got) != 0 {
		t.Errorf("composed %d lines for an empty file, want none:\n%s", len(got), show(got))
	}
}

// TestComposeFileNumbersEveryNewLine — the line cursor reports the number
// inline feedback anchors to, so every line of the new file must carry one.
func TestComposeFileNumbersEveryNewLine(t *testing.T) {
	text := "a\nb\nc\nd\n"
	hunks := []*udiff.Hunk{hunk(2, 2, line(udiff.Insert, "b\n"))}

	n := 0
	for _, l := range composeFile(text, hunks) {
		if l.Kind == udiff.Delete {
			continue
		}
		n++
		if l.New != n {
			t.Errorf("row %d carries new-side number %d, want %d", n, l.New, n)
		}
	}
	if n != 4 {
		t.Errorf("%d rows belong to the new file, want 4", n)
	}
}
