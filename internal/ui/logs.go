package ui

import (
	"fmt"
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
		client:   c,
		build:    b,
		viewport: viewport.New(0, 0),
		records:  records,
		cursor:   azdo.LogCursor{},
		work:     newWork(),
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
	for _, c := range chunks {
		if c.Task != m.lastTask {
			m.add(taskRule(c.Task))
			m.lastTask = c.Task
		}
		for _, line := range c.Lines {
			m.add(parseLogLine(line))
		}
	}

	m.viewport.SetContent(m.text.String())
	// Following the tail is only useful if the reader has not scrolled away.
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// add records a line and renders it onto the end of the buffer.
func (m *Logs) add(l logLine) {
	m.lines = append(m.lines, l)
	writeLogLine(&m.text, l, m.showStamps)
}

// taskRule is the heading written between one task's output and the next. It
// is a logLine rather than text written straight to the buffer so that a
// rebuild replays it in place along with the lines it separates. The blank
// line above it is writeLogLine's job, not part of the text — see there.
func taskRule(task string) logLine {
	return logLine{Level: levelTaskRule, Text: "── " + task + " ──"}
}

// rebuild renders every line again, which is what the timestamp toggle needs:
// the prefix is baked into the buffer at append time, so showing or hiding it
// means rewriting what is already there. The scroll position is left exactly
// where it was — the toggle is for reading the part of the log already on
// screen, and throwing the reader back to the top would defeat it. Line count
// does not change, so the offset still points at the same line.
func (m *Logs) rebuild() {
	offset := m.viewport.YOffset
	atBottom := m.viewport.AtBottom()

	m.text.Reset()
	for _, l := range m.lines {
		writeLogLine(&m.text, l, m.showStamps)
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
	body := m.viewport.View()
	if m.text.Len() == 0 {
		body = chromeStyle.Render(m.work.View() + "fetching the build log…")
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

	rows := digestLines(failures, width)
	// A third of the pane, so the digest stays a heading over the log rather
	// than a second pane competing with it.
	if room := min(digestRows, height/3); len(rows) > room {
		hidden := len(rows) - room + 1
		rows = append(rows[:max(0, room-1)], chromeStyle.Render(fmt.Sprintf("  +%d more — the log is below", hidden)))
	}
	return strings.Join(rows, "\n")
}

// digestLines renders one row per failed task and one per error under it. A
// message is taken to its first line and cut to the width: Azure DevOps error
// messages run to stack traces, and the digest is the summary that sends you
// to the log, not a replacement for reading it.
func digestLines(failures []azdo.Failure, width int) []string {
	var rows []string
	for _, f := range failures {
		rows = append(rows, errStyle.Render("✗ "+f.Task))
		if len(f.Errors) == 0 {
			rows = append(rows, chromeStyle.Render("    no error message published — see its log below"))
			continue
		}
		for _, e := range f.Errors {
			rows = append(rows, "    "+truncate(firstLine(e), max(1, width-4)))
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
	own := []key.Binding{keyTop, keyBottom, keyStamps}
	if m.build.Status.Done() {
		own = append(own, keyRefresh)
	}
	return keyMap{
		short:  append(append([]key.Binding{}, own...), keyBack, keyHelp),
		groups: [][]key.Binding{own, {keyCopyID, keyOpen}, navBindings()},
	}
}

// Status is the transient status line.
func (m *Logs) Status() (string, bool) { return m.work.View() + m.status, m.failed }
