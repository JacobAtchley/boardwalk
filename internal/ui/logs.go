package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
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

	status string
	failed bool
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
	}
}

// Init reads the log from the beginning.
func (m *Logs) Init() tea.Cmd { return m.fetch() }

// fetch reads whatever the logs have gained, and refreshes the timeline so a
// build that has started new tasks picks up their logs too.
func (m *Logs) fetch() tea.Cmd {
	client, build, cursor := m.client, m.build, m.cursor
	records := m.records

	return func() tea.Msg {
		// A running build grows new records, and a record that had no log id
		// when the pane opened may have one now.
		if !build.Status.Done() {
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

func tailTick() tea.Cmd {
	return tea.Tick(tailInterval, func(time.Time) tea.Msg { return tailTickMsg{} })
}

// Update handles input and fetch results. It satisfies View.
func (m *Logs) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case logChunksMsg:
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
		return m, m.fetch()

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
			m.status, m.failed = "refreshing…", false
			return m, m.fetch()
		case "o":
			OpenBrowser(m.client.BuildURL(m.build.ID))
			m.status, m.failed = "opened the build in a browser", false
			return m, nil
		case "y":
			CopyToClipboard(fmt.Sprint(m.build.ID))
			m.status, m.failed = fmt.Sprintf("copied id %d", m.build.ID), false
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
		return chromeStyle.Render("fetching the build log…")
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
func (m *Logs) Status() (string, bool) { return m.status, m.failed }
