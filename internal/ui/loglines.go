package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// logLevel is what a build log line turned out to be. Azure Pipelines says so
// itself, in a "##[…]" marker at the head of the line, so this is read off the
// log rather than guessed at from its wording.
type logLevel int

const (
	// levelPlain is ordinary tool output — the bulk of any log, and the one
	// level boardwalk leaves entirely alone.
	levelPlain logLevel = iota
	// levelSection is "##[section]", the boundary between a task's phases.
	levelSection
	// levelGroup is "##[group]", a collapsible heading. Boardwalk's pane does
	// not collapse, so it reads as a quieter section.
	levelGroup
	// levelEndGroup is "##[endgroup]", which closes a group and carries no
	// text of its own.
	levelEndGroup
	// levelCommand is "##[command]", the command line a task is about to run.
	levelCommand
	// levelWarning is "##[warning]".
	levelWarning
	// levelError is "##[error]".
	levelError
	// levelDebug is "##[debug]", which a pipeline only emits with
	// system.debug set and which is noise next to everything else.
	levelDebug
	// levelTaskRule is not a log line at all: it is the "── task ──" heading
	// the pane writes between one task's output and the next. It lives here
	// so that a rebuild of the buffer — what the timestamp toggle does — can
	// replay the rules in place along with the lines they separate.
	levelTaskRule
)

// markers maps each "##[…]" tag to its level. A tag that is not here is not
// treated as a marker at all: the line keeps it and reads as ordinary output,
// which is the safe way to meet a tag Azure DevOps adds after this was
// written.
var markers = map[string]logLevel{
	"section":  levelSection,
	"group":    levelGroup,
	"endgroup": levelEndGroup,
	"command":  levelCommand,
	"warning":  levelWarning,
	"error":    levelError,
	"debug":    levelDebug,
}

// logLine is one line of a build log, taken apart into the three things the
// pane wants to treat separately. Text keeps whatever ANSI the underlying tool
// emitted: go test's greens and docker's progress colours are already correct,
// and boardwalk has nothing to add to them.
type logLine struct {
	Stamp string // the ISO-8601 prefix, or empty when the line carried none
	Level logLevel
	Text  string
}

// stampLen is the width of the timestamp Azure Pipelines puts at the head of
// every line: "2026-09-18T13:49:02.1234567Z" — a date, a T, a time with seven
// fractional digits, and a Z.
const stampLen = len("2026-09-18T13:49:02.1234567Z")

// parseLogLine splits raw into its timestamp, its level and the rest.
func parseLogLine(raw string) logLine {
	var l logLine

	l.Text = raw
	if stamp, rest, ok := peelStamp(raw); ok {
		l.Stamp, l.Text = stamp, rest
	}

	if tag, rest, ok := peelMarker(l.Text); ok {
		l.Level, l.Text = tag, rest
	}
	return l
}

// peelStamp takes the timestamp off the front of a line. It insists on the
// exact shape Azure Pipelines writes rather than on anything date-like: a
// build that echoes a date of its own — "2026-09-18 13:49:02 built at …" — must
// keep it, and a loose match would swallow the first word of that line with
// nothing to show it had.
func peelStamp(raw string) (stamp, rest string, ok bool) {
	if len(raw) < stampLen+1 || raw[stampLen] != ' ' {
		return "", raw, false
	}

	stamp = raw[:stampLen]
	if stamp[4] != '-' || stamp[7] != '-' || stamp[10] != 'T' ||
		stamp[13] != ':' || stamp[16] != ':' || stamp[19] != '.' ||
		stamp[stampLen-1] != 'Z' {
		return "", raw, false
	}
	for i, c := range stamp {
		switch i {
		case 4, 7, 10, 13, 16, 19, stampLen - 1:
			continue
		}
		if c < '0' || c > '9' {
			return "", raw, false
		}
	}
	return stamp, raw[stampLen+1:], true
}

// peelMarker takes a known "##[…]" tag off the front of a line.
func peelMarker(text string) (level logLevel, rest string, ok bool) {
	if !strings.HasPrefix(text, "##[") {
		return levelPlain, text, false
	}
	end := strings.IndexByte(text, ']')
	if end < 0 {
		return levelPlain, text, false
	}
	level, known := markers[text[len("##["):end]]
	if !known {
		return levelPlain, text, false
	}
	return level, text[end+1:], true
}

// logStyle is the colour each level is rendered in, drawn from the palette the
// rest of the interface uses so the log pane reads as part of the same
// program. Ordinary output is deliberately unstyled: colouring it would leave
// the levels that mean something nothing to stand out against.
func logStyle(level logLevel) lipgloss.Style {
	switch level {
	case levelSection:
		return lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	case levelGroup:
		return lipgloss.NewStyle().Foreground(colAccent)
	case levelCommand:
		return labelStyle
	case levelWarning:
		return warnStyle
	case levelError:
		return errStyle
	case levelDebug, levelTaskRule:
		return chromeStyle
	default:
		return lipgloss.NewStyle()
	}
}

// logRows is how many rows a line takes in the buffer, which is not always
// one: an endgroup marker writes nothing at all, and a task rule writes the
// blank line above itself. The pane indexes its errors by row, so this and
// writeLogLine have to agree — TestLogRowsAgreesWithWhatIsWritten is what
// keeps them honest.
func logRows(l logLine) int {
	switch l.Level {
	case levelEndGroup:
		return 0
	case levelTaskRule:
		return 2
	default:
		return 1
	}
}

// writeLogLine renders one line into the pane's buffer, newline included.
//
// showStamps is off by default in the pane: the prefix is the same 28 columns
// on every row of a log that is already fighting for width, and it matters
// only when you are timing something.
func writeLogLine(b *strings.Builder, l logLine, showStamps bool) {
	// An endgroup marker is a marker and nothing else. Writing the line it
	// arrived on would put a blank-looking row in the log — or, with stamps
	// shown, a timestamp attached to no output at all.
	if l.Level == levelEndGroup {
		return
	}

	// A task rule is preceded by a blank line, written before the style
	// rather than inside it: lipgloss styles a multi-line string one line at
	// a time and pads each to the width of the longest, so a leading newline
	// inside the rule came out as a row of spaces wearing the rule's colour.
	if l.Level == levelTaskRule {
		b.WriteByte('\n')
	}

	if showStamps && l.Stamp != "" {
		b.WriteString(chromeStyle.Render(l.Stamp))
		b.WriteByte(' ')
	}
	b.WriteString(logStyle(l.Level).Render(l.Text))
	b.WriteByte('\n')
}
