package ui

import (
	"strings"
	"testing"
)

func TestRenderBannerStandsTheWordmarkOnThePier(t *testing.T) {
	out := renderBanner(100)
	t.Logf("\n%s", out)

	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("banner is %d rows, want the 3-row wordmark over the 3-row pier:\n%s", len(lines), out)
	}
	for _, want := range []string{"╔╗ ╔═╗╔═╗", "╬", "║", "≈"} {
		if !strings.Contains(out, want) {
			t.Errorf("banner is missing %q:\n%s", want, out)
		}
	}
	// The water is the last thing drawn: anything under it would be floating.
	if !strings.Contains(lines[len(lines)-1], "≈") {
		t.Errorf("the waterline is not the bottom row:\n%s", out)
	}
}

func TestRenderBannerDropsTheArtWhenItWouldNotFit(t *testing.T) {
	// A wrapped pier reads as corruption rather than as a drawing, so a narrow
	// terminal gets the name and nothing else.
	out := renderBanner(bannerWidth - 1)
	if !strings.Contains(out, "boardwalk") {
		t.Errorf("the narrow fallback lost the name: %q", out)
	}
	if strings.ContainsAny(out, "╬≈") {
		t.Errorf("the pier was drawn at a width it does not fit: %q", out)
	}
}

func TestRenderBannerFitsExactlyAtItsOwnWidth(t *testing.T) {
	// bannerWidth is the guard and the art's own width, so the widest row has
	// to actually fit inside it — otherwise the art draws at a width where it
	// wraps.
	for _, line := range strings.Split(renderBanner(bannerWidth), "\n") {
		if got := len([]rune(line)); got > bannerWidth {
			t.Errorf("row %q is %d columns, over the %d guard", line, got, bannerWidth)
		}
	}
}

func TestEmptyStateFloatsTheGullsOnTheWaterline(t *testing.T) {
	out := emptyState("nothing waiting on your review", 70, 12)
	t.Logf("\n%s", out)

	if !strings.Contains(out, "nothing waiting on your review") {
		t.Errorf("the empty state did not say what is missing:\n%s", out)
	}

	// The gulls are positioned against the water they sit over. Indenting each
	// row on its own width slides the two apart, so the gap in front of each
	// has to match.
	var gulls, water string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "⌒"):
			gulls = line
		case strings.Contains(line, "≈"):
			water = line
		}
	}
	if gulls == "" || water == "" {
		t.Fatalf("the empty state is missing the gulls or the water:\n%s", out)
	}
	if indentOf(gulls) <= indentOf(water) {
		t.Errorf("the gulls are not sitting over the water:\n%s", out)
	}
}

func TestEmptyStateKeepsTheMessageWhenTheArtWillNotFit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"too narrow", emptyWidth - 1, 20},
		{"too short", 80, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := emptyState("no builds", tc.width, tc.height)
			if !strings.Contains(out, "no builds") {
				t.Errorf("the empty state lost the message: %q", out)
			}
			if strings.Contains(out, "≈") {
				t.Errorf("the art was drawn at a size it does not fit: %q", out)
			}
		})
	}
}

func TestEmptyStateKeepsBreathingRoomUnderTheMessage(t *testing.T) {
	// Root hands the body exactly the rows between the title and the help
	// line, so a message on the last row sits against the key hints and reads
	// as chrome rather than as the answer.
	rows := strings.Split(emptyState("no builds", 80, 20), "\n")

	last := -1
	for i, row := range rows {
		if strings.Contains(row, "no builds") {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("the message did not render:\n%s", strings.Join(rows, "\n"))
	}
	if got := len(rows) - 1 - last; got < emptyPadBelow {
		t.Errorf("%d blank rows under the message, want at least %d", got, emptyPadBelow)
	}
	for _, row := range rows[last+1:] {
		if strings.TrimSpace(row) != "" {
			t.Errorf("the padding under the message is not blank: %q", row)
		}
	}
}

func TestEmptyStateFitsThePaneItWasGiven(t *testing.T) {
	// Overflow here costs the help line and the status line, which are the two
	// things telling the user what to do about the empty list.
	for _, height := range []int{1, 3, 6, 8, 12, 20, 60} {
		rows := strings.Split(emptyState("no builds", 80, height), "\n")
		if len(rows) > height {
			t.Errorf("at height %d the empty state drew %d rows", height, len(rows))
		}
	}
}

// indentOf counts the leading spaces of a rendered row, ignoring the colour
// escape the style put in front of them.
func indentOf(line string) int {
	if i := strings.IndexAny(line, " ⌒≈"); i > 0 {
		line = line[i:]
	}
	return len(line) - len(strings.TrimLeft(line, " "))
}
