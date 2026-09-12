package ui

import "strings"

// Banner is the landing screen's wordmark, drawn at half height so the pier
// below it has room without the two together pushing the menu off a short
// terminal. It is a literal rather than a generated figlet so the binary needs
// no font data and the shape cannot drift.
const Banner = `
  ╔╗ ╔═╗╔═╗╦═╗╔╦╗╦ ╦╔═╗╦  ╦╔═
  ╠╩╗║ ║╠═╣╠╦╝ ║║║║║╠═╣║  ╠╩╗
  ╚═╝╚═╝╩ ╩╩╚══╩╝╚╩╝╩ ╩╩═╝╩ ╩`

// Pier is the boardwalk itself: pilings, decking, and the water underneath.
// It sits directly under the wordmark, drawn in the same box characters so the
// two read as one object rather than a logo with a picture stuck to it.
const Pier = ` ═══╬══════╬══════╬══════╬════
    ║      ║      ║      ║
 ≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈`

// bannerWidth is the widest row of the wordmark and pier together. Below it
// the art is dropped rather than wrapped, since a wrapped pier reads as
// corruption.
const bannerWidth = 30

// renderBanner colours the wordmark, fading it row by row so it reads as one
// object rather than a wall of the same colour, and hangs the pier beneath it.
// A terminal too narrow for the art gets the plain name instead of a mangled
// one.
func renderBanner(width int) string {
	if width < bannerWidth {
		return bannerRow.Render("boardwalk")
	}

	// Coral, deepening row by row, carrying on into the pier's decking so the
	// wordmark and the boards it stands on share one light. These are hex
	// rather than palette indexes so the ramp stays coral on a truecolour
	// terminal; lipgloss downsamples them on anything narrower.
	shades := []string{"#FF9A80", "#FF8A6B", "#FF7F50", "#EE6B3F", "#D2553C", "#B54530"}

	var out []string
	for i, line := range strings.Split(strings.TrimLeft(Banner, "\n"), "\n") {
		out = append(out, bannerRow.Foreground(shade(shades, i)).Render(line))
	}

	// The last pier row is the water, which is the one thing here that is not
	// made of wood — colouring it with the deck would flatten the horizon the
	// whole drawing depends on.
	pier := strings.Split(Pier, "\n")
	for i, line := range pier {
		if i == len(pier)-1 {
			out = append(out, waterStyle.Render(line))
			continue
		}
		out = append(out, bannerRow.Foreground(shade(shades, len(out))).Render(line))
	}
	return strings.Join(out, "\n")
}

// emptyArt is what a view shows when it has nothing to list: gulls over the
// water off the end of the boardwalk.
const emptyArt = `     ⌒v⌒            ⌒v⌒
≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈`

// emptyWidth is the width of the widest emptyArt row.
const emptyWidth = 30

// emptyPadBelow is the blank rows kept under the message. Root sizes the body
// to exactly what is left between the title and the help line, so without them
// the message lands directly on top of the key hints and reads as part of the
// chrome rather than as the answer to what the user just asked for.
const emptyPadBelow = 2

// emptyState renders the gulls above a line saying what is not there, centred
// in the pane.
//
// An empty list otherwise draws as an empty box beside an empty detail pane,
// which reads as a fetch that broke rather than as a board with nothing on it —
// and the two cases want opposite reactions from the user.
func emptyState(message string, width, height int) string {
	lines := strings.Split(emptyArt, "\n")

	// The art, the gap under it, the message, and the padding below: what the
	// drawing costs. Root sizes the body to the rows it has left, so a pane
	// shorter than this would have the overflow push the help line and the
	// status line off the bottom of the screen — the message alone is worth
	// more there than the gulls are.
	needed := len(lines) + 1 + 1 + emptyPadBelow
	if width < emptyWidth || height < needed {
		return sandStyle.Render(centre(message, width))
	}

	// One indent for the whole drawing, not one per row: the gulls are placed
	// against the waterline they sit over, and centring each row on its own
	// width slides them apart.
	indent := strings.Repeat(" ", max(0, (width-emptyWidth)/2))

	rows := make([]string, 0, needed)
	for i, line := range lines {
		style := chromeStyle
		if i == len(lines)-1 {
			style = waterStyle
		}
		rows = append(rows, style.Render(indent+line))
	}
	rows = append(rows, "", sandStyle.Render(centre(message, width)))
	rows = append(rows, make([]string, emptyPadBelow)...)

	// Sitting the block a little above the middle reads as deliberate; dead
	// centre in a tall pane leaves it stranded. The padding below is part of
	// the block by the time this runs, so centring accounts for it rather than
	// pushing the drawing up by two rows.
	if pad := (height-len(rows))/2 - 1; pad > 0 {
		rows = append(make([]string, pad), rows...)
	}
	return strings.Join(rows, "\n")
}

// centre pads a line so it sits in the middle of width. It pads the left only:
// trailing spaces would carry the background colour past the text on a
// terminal that paints one.
func centre(s string, width int) string {
	pad := (width - len([]rune(s))) / 2
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}
