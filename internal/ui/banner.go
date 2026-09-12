package ui

import "strings"

// Banner is the landing screen's wordmark. It is a literal rather than a
// generated figlet so the binary needs no font data and the shape cannot drift.
const Banner = `
 ██████╗  ██████╗  █████╗ ██████╗ ██████╗ ██╗    ██╗ █████╗ ██╗     ██╗  ██╗
 ██╔══██╗██╔═══██╗██╔══██╗██╔══██╗██╔══██╗██║    ██║██╔══██╗██║     ██║ ██╔╝
 ██████╔╝██║   ██║███████║██████╔╝██║  ██║██║ █╗ ██║███████║██║     █████╔╝
 ██╔══██╗██║   ██║██╔══██║██╔══██╗██║  ██║██║███╗██║██╔══██║██║     ██╔═██╗
 ██████╔╝╚██████╔╝██║  ██║██║  ██║██████╔╝╚███╔███╔╝██║  ██║███████╗██║  ██╗
 ╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝`

// renderBanner colours the wordmark, fading the lower rows so it reads as one
// object rather than a wall of the same colour. A terminal too narrow for the
// full width gets the plain name instead of a mangled one.
func renderBanner(width int) string {
	lines := strings.Split(strings.TrimLeft(Banner, "\n"), "\n")
	if width < 80 {
		return bannerRow.Render("boardwalk")
	}

	// Coral, deepening row by row. These are hex rather than palette indexes so
	// the ramp stays coral on a truecolour terminal; lipgloss downsamples them
	// on anything narrower.
	shades := []string{"#FF9A80", "#FF8A6B", "#FF7F50", "#EE6B3F", "#D2553C", "#B54530"}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = bannerRow.Foreground(shade(shades, i)).Render(line)
	}
	return strings.Join(out, "\n")
}
