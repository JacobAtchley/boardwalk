package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestParseLogLinePeelsTheTimestampAndMarker(t *testing.T) {
	const stamp = "2026-09-18T13:49:02.1234567Z"

	for _, tc := range []struct {
		name      string
		raw       string
		wantStamp string
		wantLevel logLevel
		wantText  string
	}{
		{
			name:      "section",
			raw:       stamp + " ##[section]Starting: Build",
			wantStamp: stamp,
			wantLevel: levelSection,
			wantText:  "Starting: Build",
		},
		{
			name:      "command",
			raw:       stamp + " ##[command]/usr/bin/git fetch --tags",
			wantStamp: stamp,
			wantLevel: levelCommand,
			wantText:  "/usr/bin/git fetch --tags",
		},
		{
			name:      "warning",
			raw:       stamp + " ##[warning]this package is deprecated",
			wantStamp: stamp,
			wantLevel: levelWarning,
			wantText:  "this package is deprecated",
		},
		{
			name:      "error",
			raw:       stamp + " ##[error]Bash exited with code 1.",
			wantStamp: stamp,
			wantLevel: levelError,
			wantText:  "Bash exited with code 1.",
		},
		{
			name:      "debug",
			raw:       stamp + " ##[debug]Evaluating condition",
			wantStamp: stamp,
			wantLevel: levelDebug,
			wantText:  "Evaluating condition",
		},
		{
			name:      "group",
			raw:       stamp + " ##[group]Run actions/checkout",
			wantStamp: stamp,
			wantLevel: levelGroup,
			wantText:  "Run actions/checkout",
		},
		{
			name:      "endgroup carries no text",
			raw:       stamp + " ##[endgroup]",
			wantStamp: stamp,
			wantLevel: levelEndGroup,
			wantText:  "",
		},
		{
			name:      "ordinary output",
			raw:       stamp + " go: downloading github.com/foo/bar v1.2.3",
			wantStamp: stamp,
			wantLevel: levelPlain,
			wantText:  "go: downloading github.com/foo/bar v1.2.3",
		},
		{
			// Not every line carries one — a tool writing its own newlines
			// inside one log entry produces continuation lines with none.
			name:      "no timestamp",
			raw:       "    at Foo.Bar(String s)",
			wantStamp: "",
			wantLevel: levelPlain,
			wantText:  "    at Foo.Bar(String s)",
		},
		{
			// The underlying tool's own colour has to survive untouched:
			// boardwalk styling over the top of it would fight with it.
			name:      "line carrying its own ANSI",
			raw:       stamp + " \x1b[32mok\x1b[0m  boardwalk/internal/ui",
			wantStamp: stamp,
			wantLevel: levelPlain,
			wantText:  "\x1b[32mok\x1b[0m  boardwalk/internal/ui",
		},
		{
			// A marker Azure DevOps grows later, or one boardwalk has no
			// opinion about, must not eat the line it is on.
			name:      "unknown marker stays with its text",
			raw:       stamp + " ##[banana]still readable",
			wantStamp: stamp,
			wantLevel: levelPlain,
			wantText:  "##[banana]still readable",
		},
		{
			name:      "empty line",
			raw:       "",
			wantStamp: "",
			wantLevel: levelPlain,
			wantText:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogLine(tc.raw)
			if got.Stamp != tc.wantStamp {
				t.Errorf("Stamp = %q, want %q", got.Stamp, tc.wantStamp)
			}
			if got.Level != tc.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tc.wantLevel)
			}
			if got.Text != tc.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tc.wantText)
			}
		})
	}
}

// TestParseLogLineKeepsATimestampLikeWordInTheText guards the peel against
// being too eager: a build that echoes a date is common, and losing the first
// word of that line would be silent.
func TestParseLogLineKeepsATimestampLikeWordInTheText(t *testing.T) {
	raw := "2026-09-18 13:49:02 built at this local time"
	got := parseLogLine(raw)
	if got.Stamp != "" {
		t.Errorf("Stamp = %q, want none — that is not the ISO-8601 prefix", got.Stamp)
	}
	if got.Text != raw {
		t.Errorf("Text = %q, want the line untouched", got.Text)
	}
}

func TestWriteLogLineShowsTheStampOnlyWhenAsked(t *testing.T) {
	const stamp = "2026-09-18T13:49:02.1234567Z"
	line := parseLogLine(stamp + " compiling")

	var off strings.Builder
	writeLogLine(&off, line, false)
	if strings.Contains(off.String(), "2026-09-18") {
		t.Errorf("stamp shown while hidden: %q", off.String())
	}
	if !strings.Contains(off.String(), "compiling") {
		t.Errorf("text lost while the stamp is hidden: %q", off.String())
	}

	var on strings.Builder
	writeLogLine(&on, line, true)
	if !strings.Contains(on.String(), stamp) {
		t.Errorf("stamp missing while shown: %q", on.String())
	}
	if !strings.Contains(on.String(), "compiling") {
		t.Errorf("text lost while the stamp is shown: %q", on.String())
	}
}

// TestWriteLogLineEndsEveryLine keeps the buffer's lines separate: the caller
// appends these back to back.
func TestWriteLogLineEndsEveryLine(t *testing.T) {
	var b strings.Builder
	writeLogLine(&b, parseLogLine("first"), false)
	writeLogLine(&b, parseLogLine("second"), false)
	if got := b.String(); got != "first\nsecond\n" {
		t.Errorf("wrote %q, want %q", got, "first\nsecond\n")
	}
}

// TestWriteLogLineDropsEndGroup — "##[endgroup]" is a marker with no content.
// Printing the bare timestamp it arrives on would be a blank-looking row the
// reader cannot account for.
func TestWriteLogLineDropsEndGroup(t *testing.T) {
	var b strings.Builder
	writeLogLine(&b, parseLogLine("2026-09-18T13:49:02.1234567Z ##[endgroup]"), true)
	if b.Len() != 0 {
		t.Errorf("wrote %q for an endgroup marker, want nothing", b.String())
	}
}

// TestLogStyleMapsEachLevelToThePalette checks the mapping rather than the
// escape bytes: a test run is not a terminal, so lipgloss resolves every
// colour to nothing and two differently-styled lines render identically here.
func TestLogStyleMapsEachLevelToThePalette(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level logLevel
		want  lipgloss.TerminalColor
	}{
		{"error", levelError, colErr},
		{"warning", levelWarning, colWarn},
		{"section", levelSection, colAccent},
		{"group", levelGroup, colAccent},
		{"command", levelCommand, colLabel},
		{"debug", levelDebug, colDim},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := logStyle(tc.level).GetForeground(); got != tc.want {
				t.Errorf("logStyle(%v) foreground = %v, want %v", tc.level, got, tc.want)
			}
		})
	}

	// Ordinary output is left alone: colouring it would leave nothing for the
	// levels that mean something to stand out against.
	if got := logStyle(levelPlain).GetForeground(); got != (lipgloss.NoColor{}) {
		t.Errorf("ordinary output is coloured %v, want no colour", got)
	}

	// A section is the loudest thing in the log — the boundary between one
	// task's phases — so it carries weight as well as colour.
	if !logStyle(levelSection).GetBold() {
		t.Error("a section heading is not bold")
	}
	if logStyle(levelGroup).GetBold() {
		t.Error("a group heading is bold, which leaves it indistinguishable from a section")
	}
}

// TestTaskRuleKeepsItsBlankLineOutsideTheStyle — the rule is preceded by a
// blank line to separate one task's output from the next, but that blank line
// must not be part of the styled string. lipgloss styles a multi-line string
// line by line, so a leading newline inside it means colour codes wrapped
// around an empty line. Nothing in a test run can see that — a test is not a
// terminal, so every style resolves to nothing — which is exactly why the
// shape is asserted here instead.
func TestTaskRuleKeepsItsBlankLineOutsideTheStyle(t *testing.T) {
	rule := taskRule("Build")
	if strings.Contains(rule.Text, "\n") {
		t.Errorf("the styled text carries a newline: %q", rule.Text)
	}

	var b strings.Builder
	writeLogLine(&b, rule, false)
	if got := b.String(); got != "\n── Build ──\n" {
		t.Errorf("wrote %q, want %q", got, "\n── Build ──\n")
	}
}

func TestLogRowsAgreesWithWhatIsWritten(t *testing.T) {
	// The jump keys index the buffer by row, and the writer is what puts rows
	// in it. Two counts of the same thing drift silently, so they are checked
	// against each other here.
	for _, l := range []logLine{
		{Level: levelPlain, Text: "plain"},
		{Level: levelError, Text: "boom"},
		{Level: levelWarning, Text: "careful"},
		{Level: levelSection, Text: "section"},
		{Level: levelGroup, Text: "group"},
		{Level: levelEndGroup},
		{Level: levelCommand, Text: "go build"},
		{Level: levelDebug, Text: "debug"},
		{Level: levelTaskRule, Text: "── Build ──"},
	} {
		var b strings.Builder
		writeLogLine(&b, l, false)
		if got := strings.Count(b.String(), "\n"); got != logRows(l) {
			t.Errorf("level %v: writer put %d rows in the buffer, logRows says %d", l.Level, got, logRows(l))
		}
	}
}
