package ui

import (
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/lipgloss"
)

var (
	colDim      = lipgloss.AdaptiveColor{Light: "245", Dark: "241"}
	colAccent   = lipgloss.AdaptiveColor{Light: "27", Dark: "39"}
	colSelected = lipgloss.AdaptiveColor{Light: "27", Dark: "86"}
	colLabel    = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	colOK       = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colErr      = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colWarn     = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	colDraft    = lipgloss.AdaptiveColor{Light: "97", Dark: "141"}

	chromeStyle = lipgloss.NewStyle().Foreground(colDim)
	selectedRow = lipgloss.NewStyle().Foreground(colSelected).Bold(true)
	normalRow   = lipgloss.NewStyle()
	detailTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	labelStyle  = lipgloss.NewStyle().Foreground(colLabel)
	statusStyle = lipgloss.NewStyle().Foreground(colOK)
	errStyle    = lipgloss.NewStyle().Foreground(colErr)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	draftStyle  = lipgloss.NewStyle().Foreground(colDraft)
	bannerRow   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	menuItem    = lipgloss.NewStyle().PaddingLeft(2)
	menuPicked  = lipgloss.NewStyle().PaddingLeft(0).Foreground(colSelected).Bold(true)

	detailPane = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(colDim).
			PaddingLeft(2)
)

// truncate cuts a string to width display columns, marking the cut with an
// ellipsis. Width is measured with lipgloss so ANSI styling does not count.
func truncate(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
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

// padRight pads to a display width, measuring with lipgloss so ANSI styling does
// not count. A styled cell cannot go through %-Ns: the escape bytes would eat
// the padding.
func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
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
