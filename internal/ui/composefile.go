package ui

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// fileLine is one row of the file view: a line of the file, or a line the
// pull request removed, shown where it used to be.
//
// Both numbers are kept because the two sides of a diff advance at different
// rates. New is what a review comment anchors to and what the cursor reports;
// Old is what a removed line is known by, since it no longer exists in the
// new file to have a number there. Whichever side a row does not belong to
// is zero, which no real line number ever is.
type fileLine struct {
	Text string
	New  int
	Old  int
	Kind udiff.OpKind
}

// composeFile lays the pull request's changes over the whole file.
//
// The diff pane shows hunks: the changed lines and three lines either side.
// This shows the file, with the changes marked in place — which is the point
// of the view, since the reason a change is wrong is usually in the part of
// the file the hunk does not reach.
//
// text is the new side of the file, or the old side for a file the pull
// request deleted, in which case it is empty and every row comes from the
// hunks. Removed lines are spliced in at the new-side position they were
// removed from, so a rewrite reads as a rewrite rather than as an unexplained
// pair of additions.
//
// It is a pure function over the text and the hunks, and deliberately knows
// nothing about rendering: a row numbered from the wrong side puts a "+"
// against code nobody touched, which looks entirely plausible on screen and
// can only be caught by asserting the numbers directly.
func composeFile(text string, hunks []*udiff.Hunk) []fileLine {
	lines := splitLines(text)

	// Both maps are keyed by new-side line number. inserted marks the lines
	// the pull request added; removed holds what it took out, keyed by the
	// line it sat in front of.
	inserted := map[int]bool{}
	removed := map[int][]fileLine{}
	// oldOf carries the old-side number of the unchanged lines inside a
	// hunk, where the diff states it outright. Outside the hunks it has to
	// be derived — see shifts below.
	oldOf := map[int]int{}
	// shifts is how far the old side trails the new one after each hunk:
	// old = new + delta. A hunk that adds two lines and removes one leaves
	// every later line of the new file one number ahead of its old self, and
	// a view that assumed the two stayed level would number most of the file
	// wrongly the moment anything changed.
	type shift struct{ afterNew, delta int }
	var shifts []shift
	delta := 0
	// after the last line of the new file there is nothing to splice in
	// front of, so trailing removals are collected separately.
	var trailing []fileLine

	for _, h := range hunks {
		positions := diffLineNumbers(h)
		lastNew := 0
		for i, l := range h.Lines {
			pos := positions[i]
			if pos.right > lastNew {
				lastNew = pos.right
			}
			switch l.Kind {
			case udiff.Equal:
				oldOf[pos.right] = pos.left
			case udiff.Insert:
				delta--
				inserted[pos.right] = true
			case udiff.Delete:
				delta++
				row := fileLine{Text: trimNewline(l.Content), Old: pos.left, Kind: udiff.Delete}
				// A deletion's own right-hand position is zero — it is not in
				// the new file — so it belongs in front of whichever new line
				// follows it, which is the one the sides were level at.
				switch next := nextRight(h, positions, i); {
				case next == 0 || next > len(lines):
					trailing = append(trailing, row)
				default:
					removed[next] = append(removed[next], row)
				}
			}
		}
		shifts = append(shifts, shift{afterNew: lastNew, delta: delta})
	}

	out := make([]fileLine, 0, len(lines)+len(trailing))
	next, running := 0, 0
	for i, text := range lines {
		n := i + 1
		out = append(out, removed[n]...)

		// Every hunk the walk has now passed contributes its shift to the
		// lines beyond it.
		for next < len(shifts) && shifts[next].afterNew < n {
			running = shifts[next].delta
			next++
		}

		kind := udiff.Equal
		if inserted[n] {
			kind = udiff.Insert
		}
		row := fileLine{Text: text, New: n, Kind: kind}
		if kind == udiff.Equal {
			if old, ok := oldOf[n]; ok {
				row.Old = old
			} else {
				row.Old = n + running
			}
		}
		out = append(out, row)
	}
	return append(out, trailing...)
}

// nextRight is the new-side line number of the first row after i that has
// one, which is where a deletion at i belongs. Zero when the hunk ends
// without another.
func nextRight(h *udiff.Hunk, positions []diffLine, i int) int {
	for j := i + 1; j < len(h.Lines); j++ {
		if positions[j].right != 0 {
			return positions[j].right
		}
	}
	return 0
}

// splitLines breaks a file into its lines without inventing a last empty one
// for the trailing newline nearly every file ends with, and without the
// carriage return a CRLF file leaves on the end of each.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// trimNewline drops the line ending a hunk line was split on. A line without
// one is the last line of a file that does not end with one.
//
// The carriage return goes with it. It is not part of the line: a terminal
// reading one returns the cursor to column zero, so the padding written after
// the text would overwrite the text. Bubbles strips control characters before
// it paints, which is why leaving it on was invisible rather than broken —
// but a pane's correctness should not rest on that.
func trimNewline(s string) string {
	return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r")
}
