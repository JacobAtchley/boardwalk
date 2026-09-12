package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
	text     strings.Builder

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
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return PopMsg{} }
		case "g":
			m.viewport.GotoTop()
			return m, nil
		case "G":
			m.viewport.GotoBottom()
			return m, nil
		case "r":
			cmd := m.startFetch()
			if cmd == nil {
				// A fetch is already outstanding; let it land rather than
				// starting a second one.
				return m, nil
			}
			m.status, m.failed = "refreshing…", false
			return m, cmd
		case "o":
			status := report("opened the build in a browser",
				"could not open a browser", openBrowser(m.client.BuildURL(m.build.ID)))
			m.status, m.failed = status.Text, status.Err
			return m, nil
		case "y":
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
			fmt.Fprintf(&m.text, "\n%s\n", chromeStyle.Render("── "+c.Task+" ──"))
			m.lastTask = c.Task
		}
		for _, line := range c.Lines {
			m.text.WriteString(line)
			m.text.WriteByte('\n')
		}
	}

	m.viewport.SetContent(m.text.String())
	// Following the tail is only useful if the reader has not scrolled away.
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// Body renders the viewport at the size Root has left for it.
func (m *Logs) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height
	if m.text.Len() == 0 {
		return chromeStyle.Render(m.work.View() + "fetching the build log…")
	}
	return m.viewport.View()
}

// Title is the header line: pipeline, run number, live status and project.
func (m *Logs) Title() string {
	return fmt.Sprintf("%s #%s · %s · %s/%s",
		m.build.Pipeline, m.build.Number, m.build.Status, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom. Refresh only makes sense once tailing
// has stopped — otherwise a poll is already on its way.
func (m *Logs) Hints() string {
	if m.build.Status.Done() {
		return "g/G top/bottom · r refresh · y copy id · o open · esc back"
	}
	return "g/G top/bottom · tailing · y copy id · o open · esc back"
}

// Status is the transient status line.
func (m *Logs) Status() (string, bool) { return m.work.View() + m.status, m.failed }
