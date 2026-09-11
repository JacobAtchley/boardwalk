package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// buildPageSize is how many recent runs to list. Beyond this the list stops
// being something anyone scrolls.
const buildPageSize = 50

// eagerTimelines is how many rows' timelines are fetched up front, on the same
// reasoning as the pull request thread counts.
const eagerTimelines = 15

// buildRow adapts a build to the browser.
type buildRow struct {
	azdo.Build
	url      string
	progress *azdo.Progress // nil until the timeline lands
	now      time.Time
}

func (r buildRow) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s", r.Pipeline, r.Number, r.Status, shortRef(r.SourceBranch))
}

func (r buildRow) Render(width int) string {
	// The status and error cells carry styling, so they are padded by display
	// width rather than by a format verb.
	return truncate(fmt.Sprintf("%-22s %-13s %s %-26s %s %s",
		truncate(r.Pipeline, 22),
		truncate(r.Number, 13),
		padRight(statusGlyph(r.Status), 13),
		truncate(r.stepColumn(), 26),
		padRight(r.errorColumn(), 9),
		humanAge(r.Queued, r.now)), width)
}

func (r buildRow) stepColumn() string {
	if r.progress == nil {
		return "…"
	}
	if r.progress.CurrentStep == "" {
		return "—"
	}
	return r.progress.CurrentStep
}

func (r buildRow) errorColumn() string {
	if r.progress == nil || r.progress.Errors == 0 {
		return ""
	}
	return errStyle.Render(fmt.Sprintf("%d errors", r.progress.Errors))
}

func (r buildRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r buildRow) Label() string  { return fmt.Sprintf("%s #%s", r.Pipeline, r.Number) }
func (r buildRow) URL() string    { return r.url }

// buildsMsg carries the project's recent runs, fetched once at startup.
type buildsMsg struct{ Builds []azdo.Build }

// timelineMsg carries one build's fetched timeline. It names the build because
// a slow fetch can land after the cursor has moved on, and it carries Err
// rather than losing the row on a failed fetch.
type timelineMsg struct {
	Build    int
	Progress azdo.Progress
	Records  []azdo.Record
	Err      error
}

// Builds is the pipeline run browser.
type Builds struct {
	client  *azdo.Client
	browser Browser

	builds   []azdo.Build
	progress map[int]azdo.Progress
	records  map[int][]azdo.Record
	loading  map[int]bool

	loaded bool
	status string
	failed bool
	now    func() time.Time
}

// NewBuilds builds the pipeline run browser. It fetches nothing itself — Init
// does that — so it can be constructed before the client is ready to talk to
// the network in a test.
func NewBuilds(c *azdo.Client) *Builds {
	m := &Builds{
		client:   c,
		browser:  NewBrowser(),
		progress: map[int]azdo.Progress{},
		records:  map[int][]azdo.Record{},
		loading:  map[int]bool{},
		now:      time.Now,
	}
	m.browser.Detail = m.renderDetail
	return m
}

// Init fetches the project's recent runs.
func (m *Builds) Init() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		builds, err := client.Builds(buildPageSize)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch builds: %w", err)}
		}
		return buildsMsg{Builds: builds}
	}
}

// applyRows rebuilds the browser's rows, carrying forward whatever timelines
// have already loaded.
func (m *Builds) applyRows() {
	rows := make([]Row, 0, len(m.builds))
	for _, b := range m.builds {
		row := buildRow{Build: b, url: m.client.BuildURL(b.ID), now: m.now()}
		if p, ok := m.progress[b.ID]; ok {
			row.progress = &p
		}
		rows = append(rows, row)
	}
	m.browser.SetRows(rows)
}

// fetchTimelines loads the first screenful of timelines and whatever the
// cursor has reached since.
func (m *Builds) fetchTimelines() tea.Cmd {
	want := map[int]bool{}
	for i, b := range m.builds {
		if i < eagerTimelines {
			want[b.ID] = true
		}
	}
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(buildRow); ok {
			want[r.ID] = true
		}
	}

	var cmds []tea.Cmd
	for id := range want {
		if _, done := m.progress[id]; done || m.loading[id] {
			continue
		}
		m.loading[id] = true

		client, buildID := m.client, id
		cmds = append(cmds, func() tea.Msg {
			progress, records, err := client.Timeline(buildID)
			return timelineMsg{Build: buildID, Progress: progress, Records: records, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

// Update handles input and fetch results. It satisfies View.
func (m *Builds) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case buildsMsg:
		m.builds, m.loaded = msg.Builds, true
		m.applyRows()
		return m, m.fetchTimelines()

	case timelineMsg:
		// Cleared before the error check: a failed fetch must still allow a
		// later selection to retry, rather than being stuck "loading…" forever.
		delete(m.loading, msg.Build)
		if msg.Err != nil {
			m.status, m.failed = fmt.Sprintf("could not load the timeline for build %d: %v", msg.Build, msg.Err), true
			return m, nil
		}
		m.progress[msg.Build] = msg.Progress
		m.records[msg.Build] = msg.Records
		m.applyRows()
		return m, nil

	case ErrMsg:
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.browser.Filtering() {
			break
		}

		row, hasRow := m.browser.Selected()
		if hasRow {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "enter":
			r, ok := row.(buildRow)
			if !hasRow || !ok {
				return m, nil
			}
			logs := NewLogs(m.client, r.Build, m.records[r.ID])
			return m, func() tea.Msg { return PushMsg{View: logs} }

		case "r":
			m.progress = map[int]azdo.Progress{}
			m.records = map[int][]azdo.Record{}
			m.loading = map[int]bool{}
			m.status, m.failed = "refreshing…", false
			return m, m.Init()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchTimelines())
}

func (m *Builds) renderDetail(row Row, width int) string {
	r, ok := row.(buildRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(r.Label(), width)))
	for _, field := range [][2]string{
		{"status", r.Status.String()},
		{"step", r.stepColumn()},
		{"branch", shortRef(r.SourceBranch)},
		{"queued", humanAge(r.Queued, r.now) + " ago"},
		{"by", orDash(r.RequestedFor)},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-8s", field[0]+":")),
			truncate(field[1], width-9))
	}

	records := m.records[r.ID]
	if len(records) == 0 {
		fmt.Fprintf(&b, "\n%s\n", chromeStyle.Render("loading the timeline…"))
		return b.String()
	}

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("steps"))
	for _, rec := range records {
		if rec.Type != "Task" {
			continue
		}
		marker := "·"
		style := chromeStyle
		switch {
		case rec.Result == "failed":
			marker, style = "✗", errStyle
		case rec.State == "inProgress":
			marker, style = "◐", warnStyle
		case rec.Result == "succeeded":
			marker, style = "✓", statusStyle
		}
		fmt.Fprintf(&b, "%s %s\n", style.Render(marker), truncate(rec.Name, width-2))
	}
	return b.String()
}

// Body renders the browser at the size Root has left for it.
func (m *Builds) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return chromeStyle.Render("fetching builds…")
	}
	return m.browser.View()
}

// Title reports the run count and the project the builds belong to.
func (m *Builds) Title() string {
	return fmt.Sprintf("builds (%d) · %s/%s", m.browser.Len(), m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
func (m *Builds) Hints() string {
	return "enter logs · r refresh · " + SharedHints + " · esc back"
}

// Status is the transient status line, or the fuzzy filter prompt while one is
// open.
func (m *Builds) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
