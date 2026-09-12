package ui

import (
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colDim    = lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	colAccent = lipgloss.AdaptiveColor{Light: "27", Dark: "39"}
	colLabel  = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	colOK     = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colErr    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colWarn   = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	colDraft  = lipgloss.AdaptiveColor{Light: "97", Dark: "141"}

	// colCoral is boardwalk's accent: the wordmark, the selected row, the
	// spinner, and the keys in the help line. It is kept separate from
	// colAccent, which colours detail titles, so the things the eye should be
	// drawn to share one colour and the things it should read share another.
	colCoral = lipgloss.AdaptiveColor{Light: "#D2553C", Dark: "#FF7F50"}

	chromeStyle = lipgloss.NewStyle().Foreground(colDim)
	selectedRow = lipgloss.NewStyle().Foreground(colCoral).Bold(true)
	normalRow   = lipgloss.NewStyle()
	detailTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	labelStyle  = lipgloss.NewStyle().Foreground(colLabel)
	statusStyle = lipgloss.NewStyle().Foreground(colOK)
	errStyle    = lipgloss.NewStyle().Foreground(colErr)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	draftStyle  = lipgloss.NewStyle().Foreground(colDraft)
	coralStyle  = lipgloss.NewStyle().Foreground(colCoral)
	bannerRow   = lipgloss.NewStyle().Foreground(colCoral).Bold(true)
	menuItem    = lipgloss.NewStyle().PaddingLeft(2)
	menuPicked  = lipgloss.NewStyle().PaddingLeft(0).Foreground(colCoral).Bold(true)

	detailPane = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(colDim).
			PaddingLeft(2)
)

// truncate cuts a string to width display columns, marking the cut with an
// ellipsis. Width is measured with lipgloss so ANSI styling does not count.
//
// The cut goes through ansi rather than slicing runes for the same reason the
// measurement does: a row cell can be styled — buildRow.Render pushes a
// composite holding a coloured status glyph and a red error count through
// here. Slicing runes counted the escape bytes as columns, so a styled row lost
// content at ordinary widths, and at some widths the cut landed between a
// colour and its reset (bleeding the colour onto the rest of the line) or
// inside the escape sequence itself.
func truncate(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// placeholder is what a list view's body reads before its first batch lands. A
// fetch that has already failed says so here: leaving the body on "fetching…"
// while the status line under it reports the failure puts two contradictory
// statements on one screen, with nothing to say which is current.
func placeholder(what, status string, failed bool, spin string) string {
	if failed && status != "" {
		return errStyle.Render(status) + "\n" + chromeStyle.Render("press r to try again")
	}
	return chromeStyle.Render(spin + "fetching " + what + "…")
}

// wordwrap breaks text on word boundaries at the given width.
func wordwrap(s string, width int) string {
	var out strings.Builder
	col := 0
	for _, word := range strings.Fields(s) {
		length := len([]rune(word))
		switch {
		case col == 0:
		case col+1+length > width:
			out.WriteString("\n")
			col = 0
		default:
			out.WriteString(" ")
			col++
		}
		out.WriteString(word)
		col += length
	}
	return out.String()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// padRight fits a cell to a display width, measuring with lipgloss so ANSI
// styling does not count. A styled cell cannot go through %-Ns: the escape
// bytes would eat the padding. An oversize cell is cut rather than allowed
// through, since one long cell pushing every column after it out of alignment
// defeats the point of padding the short ones.
func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return truncate(s, width)
}

// shade picks the nth colour of a gradient, holding the last one for any row
// beyond the list.
func shade(shades []string, i int) lipgloss.Color {
	if i >= len(shades) {
		i = len(shades) - 1
	}
	return lipgloss.Color(shades[i])
}

// statusGlyph pairs a build status with a coloured marker, so the state reads
// at a glance without the word taking a column of its own.
func statusGlyph(s azdo.BuildStatus) string {
	switch s {
	case azdo.StatusRunning:
		return warnStyle.Render("◐ running")
	case azdo.StatusSucceeded:
		return statusStyle.Render("✓ succeeded")
	case azdo.StatusFailed:
		return errStyle.Render("✗ failed")
	case azdo.StatusPartial:
		return warnStyle.Render("~ partial")
	case azdo.StatusCanceled:
		return chromeStyle.Render("⊘ canceled")
	default:
		return chromeStyle.Render("· queued")
	}
}

// newHelp builds the help component in boardwalk's chrome colours, so the key
// line reads as part of the frame rather than as content.
func newHelp() help.Model {
	h := help.New()
	h.Styles.ShortKey = coralStyle
	h.Styles.ShortDesc = chromeStyle
	h.Styles.ShortSeparator = chromeStyle
	h.Styles.FullKey = coralStyle
	h.Styles.FullDesc = chromeStyle
	h.Styles.FullSeparator = chromeStyle
	return h
}

// voteStyle colours a reviewer's vote: approvals read as done, a rejection or a
// wait as something still owed.
func voteStyle(vote int) lipgloss.Style {
	switch {
	case vote > 0:
		return statusStyle
	case vote == 0:
		return chromeStyle
	case vote > -10:
		return warnStyle
	default:
		return errStyle
	}
}

// prStatusLabel reads a pull request's state the way a person would say it. A
// draft is called out before its status because it is the more useful fact:
// a draft is not waiting on anybody.
func prStatusLabel(pr azdo.PullRequest) string {
	if pr.IsDraft {
		return "draft"
	}
	switch pr.Status {
	case "completed":
		return "merged"
	case "abandoned":
		return "abandoned"
	case "", "active":
		return "active"
	default:
		return pr.Status
	}
}
