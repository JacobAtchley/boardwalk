package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// csi matches a complete SGR escape sequence. Anything starting an escape that
// this does not match is a sequence the terminal would be left holding.
var csi = regexp.MustCompile(`^\x1b\[[0-9;]*[A-Za-z]`)

// styled is a row cell with colour in it, written as raw escapes rather than
// through a lipgloss style: lipgloss strips colour when the test binary's
// output is not a terminal, so a style built here would carry no escapes at all
// and prove nothing.
const styled = "\x1b[91m12 errors\x1b[0m and a plain tail long enough to be cut"

func TestTruncateIsAnsiAware(t *testing.T) {
	// buildRow.Render pushes a composite holding two styled cells through
	// truncate. Cutting by runes counted the escape bytes as display columns,
	// which lost content at ordinary widths, and at some widths cut inside an
	// escape sequence or between a colour and its reset.
	for _, width := range []int{40, 30, 12, 5, 3} {
		got := truncate(styled, width)

		if w := lipgloss.Width(got); w != width {
			t.Errorf("truncate(styled, %d) is %d display columns wide, want %d: %q", width, w, width, got)
		}
		for i := 0; i < len(got); i++ {
			if got[i] == 0x1b && !csi.MatchString(got[i:]) {
				t.Errorf("truncate(styled, %d) emitted a partial escape sequence: %q", width, got)
				break
			}
		}
		if strings.Contains(got, "\x1b[91m") && !strings.Contains(got, "\x1b[0m") {
			t.Errorf("truncate(styled, %d) left the colour unreset, so it bleeds onto the rest of the line: %q", width, got)
		}
	}
}

func TestTruncateLeavesAFittingStringAlone(t *testing.T) {
	if got := truncate(styled, 80); got != styled {
		t.Errorf("truncate rewrote a string that already fits: %q", got)
	}
	if got := truncate(styled, 1); got != styled {
		t.Errorf("truncate at a width too small to mark a cut should pass the string through, got %q", got)
	}
}

func TestPadRightClampsAsWellAsPads(t *testing.T) {
	// A cell wider than its column pushes every column after it out of
	// alignment, which is the whole reason padRight exists.
	if got := padRight("abc", 6); got != "abc   " {
		t.Errorf("padRight = %q, want it padded to six columns", got)
	}
	if got := padRight(styled, 9); lipgloss.Width(got) != 9 {
		t.Errorf("padRight left a cell %d columns wide in a 9 column slot: %q", lipgloss.Width(got), got)
	}
}
