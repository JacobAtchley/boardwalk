package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tailInterval is how often a running build's logs are re-read. Three seconds
// keeps the pane feeling live without hammering the API for a build whose steps
// take minutes.
const tailInterval = 3 * time.Second

// logChunksMsg carries whatever a poll gained: new log text, the build's
// current status (so tailing knows when to stop), and a refreshed timeline (so
// a task that grew a log id since the pane opened is picked up).
type logChunksMsg struct {
	Chunks  []azdo.LogChunk
	Status  azdo.BuildStatus
	Records []azdo.Record
	Err     error
}

// tailTickMsg fires every tailInterval while the build is still running.
type tailTickMsg struct{}

// Logs pages through one build's logs, appending while the build runs.
type Logs struct {
	client *azdo.Client
	build  azdo.Build

	viewport viewport.Model
	records  []azdo.Record
	cursor   azdo.LogCursor

	// lastTask is the task whose rule was written most recently, so an append
	// to the same log does not repeat the heading.
	lastTask string

	// lines is every line the pane has been given, parsed but not rendered,
	// and text is the rendered buffer the viewport reads. Both are kept: an
	// append writes only the new lines into text, which is the every-three-
	// seconds path, while a timestamp toggle rewrites text from lines, which
	// happens only when a key is pressed. Both go through writeLogLine, so
	// the two paths cannot disagree about how a line looks.
	lines []logLine
	text  strings.Builder

	// showStamps is whether the timestamp prefix is on screen. See keyStamps
	// for why it starts off.
	showStamps bool

	// showErrors is whether the pane is cut down to its error lines. See
	// keyErrorsOnly.
	showErrors bool

	// rowCount is how many rows the buffer holds, which is what makes the next
	// line's row known without counting the buffer again.
	rowCount int

	// jumpRow is the row the last jump asked for and jumpOffset is where the
	// viewport actually landed, which are not the same number: an error near
	// the end of a log cannot be scrolled to the top, so SetYOffset clamps.
	// Deciding the next jump from the offset alone therefore picked the same
	// error forever and never came back round. Both start at -1, which no row
	// can be, so the first press reads the viewport rather than a jump that
	// never happened.
	jumpRow, jumpOffset int

	// errRows is the buffer row each error line landed on, in order, which is
	// what keyNextError jumps between. Rows rather than indexes into lines:
	// see logRows for why the two are not the same number. It is rebuilt
	// wherever the buffer is.
	errRows []int

	// rowOf and lineOf are the two directions of the same map, one entry per
	// line the buffer actually holds: which row a line starts on, and which
	// line that was. Both are ascending, so both are searchable. They are
	// what lets the error filter keep the reader's place — the row numbers
	// mean nothing across a filter, but the line they were looking at is the
	// same line either way.
	rowOf, lineOf []int

	// fetching is a single-flight guard. fetch's closure mutates m.cursor in
	// place, and bubbletea runs each returned Cmd in its own goroutine — two
	// fetches in flight at once would be a concurrent map write, which is a
	// fatal, unrecoverable crash. Both "r" and a tail tick go through
	// startFetch, which this guards.
	fetching bool

	status string
	failed bool
	work   work
}

// NewLogs opens the log pane for one build. records is the timeline the build
// view already fetched, so the pane has something to read from before its own
// first poll lands.
func NewLogs(c *azdo.Client, b azdo.Build, records []azdo.Record) *Logs {
	return &Logs{
		client:     c,
		build:      b,
		viewport:   viewport.New(0, 0),
		records:    records,
		cursor:     azdo.LogCursor{},
		jumpRow:    -1,
		jumpOffset: -1,
		work:       newWork(),
	}
}

// Init reads the log from the beginning.
func (m *Logs) Init() tea.Cmd { return m.startFetch() }

// startFetch issues a fetch unless one is already outstanding. Without this
// guard, "r" pressed while a tail tick's fetch is still in flight — or two
// ticks racing — could run two fetches concurrently; see the fetching field's
// comment for why that is a crash rather than just wasted work.
func (m *Logs) startFetch() tea.Cmd {
	if m.fetching {
		return nil
	}
	m.fetching = true
	return tea.Batch(m.fetch(), m.work.begin(1))
}

// fetch reads whatever the logs have gained, and refreshes the timeline so a
// build that has started new tasks picks up their logs too.
func (m *Logs) fetch() tea.Cmd {
	client, build, cursor := m.client, m.build, m.cursor
	records := m.records

	return func() tea.Msg {
		// A running build grows new records, and a record that had no log id
		// when the pane opened may have one now. A finished build can need the
		// same catch-up once: enter can beat the build view's own lazy
		// timeline fetch, landing here with no records at all.
		if needsTimelineRefresh(build.Status, records) {
			if _, fresh, err := client.Timeline(build.ID); err == nil {
				records = fresh
			}
		}

		chunks, err := client.NewLogChunks(build.ID, records, cursor)
		if err != nil {
			return logChunksMsg{Err: fmt.Errorf("could not read the build log: %w", err)}
		}

		current, err := client.BuildByID(build.ID)
		if err != nil {
			return logChunksMsg{Chunks: chunks, Records: records, Status: build.Status}
		}
		return logChunksMsg{Chunks: chunks, Records: records, Status: current.Status}
	}
}

// needsTimelineRefresh reports whether fetch should re-read the timeline
// before reading logs: always while the build is still running, since it can
// grow new records; once more for a finished build whose records never
// loaded, since NewLogChunks can read nothing from an empty slice and the pane
// would otherwise be stuck on its placeholder forever.
func needsTimelineRefresh(status azdo.BuildStatus, records []azdo.Record) bool {
	return !status.Done() || len(records) == 0
}

func tailTick() tea.Cmd {
	return tea.Tick(tailInterval, func(time.Time) tea.Msg { return tailTickMsg{} })
}

// Update handles input and fetch results. It satisfies View.
func (m *Logs) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case logChunksMsg:
		// Cleared before anything else: every path that sets fetching must
		// clear it, or the tail deadlocks the first time a poll fails.
		m.fetching = false
		m.work.done()

		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			// A failed poll is not a reason to stop watching a running build.
			if !m.build.Status.Done() {
				return m, tailTick()
			}
			return m, nil
		}

		if msg.Records != nil {
			m.records = msg.Records
		}
		m.append(msg.Chunks)
		m.build.Status = msg.Status
		m.status, m.failed = "", false

		if m.build.Status.Done() {
			return m, nil
		}
		return m, tailTick()

	case tailTickMsg:
		return m, m.startFetch()

	case tea.KeyMsg:
		// Matched against the bindings in keys.go rather than against raw
		// strings: this view's footer is built from those bindings, and a
		// handler keyed off the letters instead was a second copy of the same
		// mapping, free to drift from the one the reader is shown.
		switch {
		case key.Matches(msg, keyBack), key.Matches(msg, keyQuit):
			return m, func() tea.Msg { return PopMsg{} }
		case key.Matches(msg, keyTop):
			m.viewport.GotoTop()
			return m, nil
		case key.Matches(msg, keyBottom):
			m.viewport.GotoBottom()
			return m, nil
		case key.Matches(msg, keyErrorsOnly):
			m.toggleErrors()
			return m, nil
		case key.Matches(msg, keyNextError):
			m.jumpToNextError()
			return m, nil
		case key.Matches(msg, keyWatch):
			b := m.build
			return m, func() tea.Msg { return WatchBuildMsg{Build: b} }
		case key.Matches(msg, keyStamps):
			m.showStamps = !m.showStamps
			m.rebuild()
			return m, nil
		case key.Matches(msg, keyRefresh):
			cmd := m.startFetch()
			if cmd == nil {
				// A fetch is already outstanding; let it land rather than
				// starting a second one.
				return m, nil
			}
			m.status, m.failed = "refreshing…", false
			return m, cmd
		case key.Matches(msg, keyOpen):
			status := report("opened the build in a browser",
				"could not open a browser", openBrowser(m.client.BuildURL(m.build.ID)))
			m.status, m.failed = status.Text, status.Err
			return m, nil
		case key.Matches(msg, keyCopyID):
			status := report(fmt.Sprintf("copied id %d", m.build.ID),
				"could not copy to the clipboard", copyToClipboard(fmt.Sprint(m.build.ID)))
			m.status, m.failed = status.Text, status.Err
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// append writes new chunks, heading each task's output with a rule the first
// time that task is seen.
func (m *Logs) append(chunks []azdo.LogChunk) {
	if len(chunks) == 0 {
		return
	}

	atBottom := m.viewport.AtBottom()
	// Where this append's own lines start. A local rather than a field:
	// nothing outside this call has any use for it, and the one time it lived
	// on the struct it survived a rebuild and had the next append write the
	// tail of the buffer twice.
	start := len(m.lines)
	for _, c := range chunks {
		if c.Task != m.lastTask {
			m.lines = append(m.lines, taskRule(c.Task))
			m.lastTask = c.Task
		}
		for _, line := range c.Lines {
			m.lines = append(m.lines, parseLogLine(line))
		}
	}

	// Filtered, an appended line cannot be rendered as it arrives: whether a
	// task heading belongs on screen depends on the lines that come after it,
	// which have not arrived yet. The whole buffer is written again instead —
	// only while the filter is on, and only for as long as the build tails.
	if m.showErrors {
		m.rebuild()
		return
	}
	for i := start; i < len(m.lines); i++ {
		m.write(i, m.lines[i])
	}
	m.viewport.SetContent(m.text.String())
	// Following the tail is only useful if the reader has not scrolled away.
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// write renders one line onto the end of the buffer and notes where it landed,
// so that a jump has somewhere to jump to. Both the append path and the
// rebuild path go through here: an index maintained in only one of them would
// be right until the first timestamp toggle.
func (m *Logs) write(index int, l logLine) {
	if l.Level == levelError {
		m.errRows = append(m.errRows, m.rowCount)
	}
	m.rowOf = append(m.rowOf, m.rowCount)
	m.lineOf = append(m.lineOf, index)
	m.rowCount += logRows(l)
	writeLogLine(&m.text, l, m.showStamps)
}

// lineAtTop is the line the pane is currently showing first, as an index into
// lines rather than a row.
func (m *Logs) lineAtTop() int {
	if len(m.rowOf) == 0 {
		return 0
	}
	after := sort.Search(len(m.rowOf), func(i int) bool { return m.rowOf[i] > m.viewport.YOffset })
	return m.lineOf[max(0, after-1)]
}

// rowOfLine is the row to scroll to in order to show a line again: its own
// row, or the first row after it when the line itself is not in the buffer —
// which is the ordinary case on the way into the filter, where the line the
// reader was on is most of what the filter throws away.
func (m *Logs) rowOfLine(index int) int {
	at := sort.Search(len(m.lineOf), func(i int) bool { return m.lineOf[i] >= index })
	if at == len(m.lineOf) {
		return m.rowCount
	}
	return m.rowOf[at]
}

// toggleErrors cuts the pane down to its errors, or gives the log back.
//
// A log with no errors is left alone rather than emptied: a blank pane is not
// an answer, and the same sentence the jump key uses says why there was
// nothing to show.
func (m *Logs) toggleErrors() {
	if !m.showErrors && !m.hasErrors() {
		m.status, m.failed = "no error lines in this log", false
		return
	}

	// The offset belongs to the buffer being replaced — a row number means
	// nothing once most of the rows are gone, or once they are all back — but
	// the line under it is the same line either way, so that is what the pane
	// is put back on.
	anchor := m.lineAtTop()
	m.showErrors = !m.showErrors
	m.rebuild()
	m.viewport.SetYOffset(m.rowOfLine(anchor))
	// A failed poll's message is left alone: it is the pane's most recent
	// news, and this key has nothing to say over it.
	if !m.failed {
		m.status = ""
	}
}

func (m *Logs) hasErrors() bool {
	for _, l := range m.lines {
		if l.Level == levelError {
			return true
		}
	}
	return false
}

// errorIndexes is what the filtered buffer is rendered from: the errors and
// the task headings that own them. A heading whose task reported nothing is
// left out too; keeping it would say a task failed when all it did was run.
func (m *Logs) errorIndexes() []int {
	out := make([]int, 0, 16)
	for i, l := range m.lines {
		switch {
		case l.Level == levelError:
			out = append(out, i)
		case l.Level == levelTaskRule && taskHasError(m.lines[i+1:]):
			out = append(out, i)
		}
	}
	return out
}

// taskHasError reports whether the lines after a task heading, up to the next
// heading, include an error.
func taskHasError(rest []logLine) bool {
	for _, l := range rest {
		if l.Level == levelTaskRule {
			return false
		}
		if l.Level == levelError {
			return true
		}
	}
	return false
}

// jumpToNextError scrolls to the first error below the top of the pane,
// wrapping round to the first once past the last one. The error is put at the
// top of the pane rather than in the middle: what explains a failure is the
// output under it.
func (m *Logs) jumpToNextError() {
	if len(m.errRows) == 0 {
		m.status, m.failed = "no error lines in this log", false
		return
	}

	// Where the next jump counts from: the row the last one asked for while
	// the reader has left the pane alone, and wherever they scrolled to once
	// they have moved it themselves.
	floor := m.viewport.YOffset
	if m.viewport.YOffset == m.jumpOffset {
		floor = m.jumpRow
	}

	next, wrapped := m.errRows[0], true
	for _, row := range m.errRows {
		if row > floor {
			next, wrapped = row, false
			break
		}
	}

	m.viewport.SetYOffset(next)
	m.jumpRow, m.jumpOffset = next, m.viewport.YOffset
	if wrapped {
		// A jump that silently travels backwards reads as a jump that did
		// nothing, since the error it lands on may already be on screen.
		m.status, m.failed = "wrapped to the first error", false
	}
}

// taskRule is the heading written between one task's output and the next. It
// is a logLine rather than text written straight to the buffer so that a
// rebuild replays it in place along with the lines it separates. The blank
// line above it is writeLogLine's job, not part of the text — see there.
func taskRule(task string) logLine {
	return logLine{Level: levelTaskRule, Text: "── " + task + " ──"}
}

// rebuild renders the buffer again from the lines the pane has been given:
// what the timestamp toggle needs, since the prefix is baked in at append
// time, and what the error filter needs, since which lines belong on screen
// changes with it.
//
// The offset is left where it was, which is right for the timestamp toggle —
// that rewrite changes no line's row, and throwing the reader back to the top
// would defeat a toggle meant for the part of the log already on screen. It is
// not right for the filter, where a row means something else on the other side
// of the rebuild; toggleErrors sets the offset itself afterwards, by line
// rather than by row.
func (m *Logs) rebuild() {
	offset := m.viewport.YOffset
	atBottom := m.viewport.AtBottom()

	m.text.Reset()
	m.errRows, m.rowCount = nil, 0
	m.rowOf, m.lineOf = nil, nil
	// The rows the last jump remembered belonged to the buffer being replaced.
	m.jumpRow, m.jumpOffset = -1, -1

	if m.showErrors {
		for _, i := range m.errorIndexes() {
			m.write(i, m.lines[i])
		}
	} else {
		for i, l := range m.lines {
			m.write(i, l)
		}
	}
	m.viewport.SetContent(m.text.String())

	if atBottom {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(offset)
}

// digestRows is the most rows the failure digest may take: enough for a task
// and a few of its errors, and never more than a third of the pane. The log is
// what the reader came for — a digest that pushed it off the screen would have
// answered "why did it fail" by hiding the evidence.
const digestRows = 6

// Body renders the digest and the viewport at the size Root has left for it.
// The digest is pinned above rather than written into the log buffer: the
// buffer is appended to every three seconds while a build tails, and a heading
// inside it would scroll away from the reader the moment it mattered.
func (m *Logs) Body(width, height int) string {
	digest := m.digestView(width, height)
	if digest != "" {
		height = max(1, height-lipgloss.Height(digest))
	}

	m.viewport.Width, m.viewport.Height = width, height
	body := chromeStyle.Render(m.work.View() + "fetching the build log…")
	if m.text.Len() > 0 {
		body = m.viewport.View()
	}
	if digest == "" {
		return body
	}
	return digest + "\n" + body
}

// digestView is the account of what broke, read off the timeline rather than
// out of the log: the failed tasks in execution order, each with the error
// messages Azure DevOps itself puts on the run's summary page.
//
// It appears only once a build has actually failed. A running build has
// nothing settled to report, and a succeeded one has nothing to report at all.
func (m *Logs) digestView(width, height int) string {
	if m.build.Status != azdo.StatusFailed && m.build.Status != azdo.StatusPartial {
		return ""
	}
	failures := azdo.Failures(m.records)
	if len(failures) == 0 {
		return ""
	}

	// A third of the pane, so the digest stays a heading over the log rather
	// than a second pane competing with it. A pane too short for even one row
	// gets no digest at all: a summary that left room for none of the log
	// would be answering the question by hiding the evidence.
	room := min(digestRows, height/3)
	if room < 1 {
		return ""
	}

	rows := digestLines(failures, width)
	if len(rows) > room {
		kept := rows[:room-1]
		hidden := len(rows) - len(kept)
		rows = append(kept, truncate(chromeStyle.Render(
			fmt.Sprintf("  +%d more — the log is below", hidden)), width))
	}
	return strings.Join(rows, "\n")
}

// digestLines renders one row per failed task and one per error under it.
//
// Every row is cut to the width, task names included: Azure DevOps error
// messages run to stack traces and a task's display name is whatever its YAML
// called it, while Root sizes the pane by counting newlines — a row wider than
// the terminal wraps into rows nobody budgeted for, and pushes the help and
// status lines off the bottom. A message is taken to its first line for the
// same reason; the digest is the summary that sends you to the log, not a
// replacement for reading it.
func digestLines(failures []azdo.Failure, width int) []string {
	var rows []string
	for _, f := range failures {
		rows = append(rows, errStyle.Render(truncate("✗ "+f.Task, width)))
		if len(f.Errors) == 0 {
			rows = append(rows, chromeStyle.Render(
				truncate("    no error message published — see its log below", width)))
			continue
		}
		for _, e := range f.Errors {
			rows = append(rows, truncate("    "+firstLine(e), width))
		}
	}
	return rows
}

// firstLine is the head of a message that arrived with newlines in it, which a
// pinned digest cannot show without pushing the log off the screen.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], "\r")
	}
	return s
}

// Title is the header line: pipeline, run number, live status and project.
func (m *Logs) Title() string {
	return fmt.Sprintf("%s #%s · %s · %s/%s",
		m.build.Pipeline, m.build.Number, m.build.Status, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom. Refresh only makes sense once tailing
// has stopped — otherwise a poll is already on its way.
// Keys omits the filter and the Slack link: the log pane is a pager over one
// build, not a list of things to act on. Refresh only appears once the build
// has finished, since a running one is already tailing itself.
//
// The footer used to also carry copy-id and open-in-browser and never carried
// back at all — the one view in the branch that left esc off the footer
// entirely, the same rule violation the other list views had, just never
// caught here because this one never overflowed. Copy-id and open are still
// one press of "?" away in the panel's second column.
func (m *Logs) Keys() help.KeyMap {
	own := []key.Binding{keyTop, keyBottom, keyStamps, keyWatch}
	if m.build.Status.Done() {
		own = append(own, keyRefresh)
	}
	// The footer carries the two keys that answer "why did it fail", and
	// refresh once there is any point in it. Naming the scroll and timestamp
	// keys there too pushed esc and ? off the end at eighty columns, which is
	// the defect TestEveryViewsShortHelpSurvivesOrdinaryWidths exists to
	// catch; they are one press of "?" away.
	short := []key.Binding{keyNextError, keyErrorsOnly}
	if m.build.Status.Done() {
		short = append(short, keyRefresh)
	}
	return keyMap{
		short:  append(short, keyBack, keyHelp),
		groups: [][]key.Binding{append([]key.Binding{keyNextError, keyErrorsOnly}, own...), {keyCopyID, keyOpen}, navBindings()},
	}
}

// Status is the transient status line.
func (m *Logs) Status() (string, bool) { return m.work.View() + m.status, m.failed }
