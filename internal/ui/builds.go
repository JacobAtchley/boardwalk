package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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

// buildPRMsg carries the outcome of looking up the pull request a build ran
// for. It names the build for the same reason timelineMsg does: pressing p
// again for a different build before the first lookup answers must not let
// the stale answer act on a build the user is no longer asking about. Branch
// rides along too, so the no-match status line can name it without looking
// the build back up by id — a refresh could have dropped it from m.builds
// entirely by the time the answer lands.
type buildPRMsg struct {
	Build  int
	Branch string
	PR     azdo.PullRequest
	Found  bool
	Err    error
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
	work   work
	now    func() time.Time

	// queueDefinition and queueLabel are the pipeline the open branch prompt
	// will queue, captured when it opened. They cannot be read back off the
	// list when it closes: a refresh landing in between rebuilds the rows.
	queueDefinition int
	queueLabel      string

	// linkBuild is the id of the build a pull request lookup is in flight
	// for, or 0 when none is. It is how a late answer knows whether it is
	// still the one asked about — see buildPRMsg.
	linkBuild int

	// armedBuild is non-nil while a re-run or a cancel is waiting on its
	// confirming keystroke, and branchPrompt is non-nil while a fresh queue
	// is asking which branch. They are never both set: each swallows every
	// key that is not its own, so neither can be reached while the other is
	// up. Same arrangement as the pull request detail view's three modals.
	armedBuild   *armedBuild
	branchPrompt *textinput.Model

	// hidden is true from the moment this view pushes Logs on top of itself
	// until it is back on top. Root broadcasts buildPRMsg to the whole
	// stack, not just the top view, so a lookup that resolves while Logs is
	// showing must not surface its own push over it. A KeyMsg is proof of
	// being back on top — Root's topOnly routes every key to the top view
	// alone — so the flag clears the moment one arrives, with no need for
	// Root or the View interface to say so explicitly.
	hidden bool
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
		work:     newWork(),
		now:      time.Now,
	}
	m.browser.Detail = m.renderDetail
	return m
}

// Init fetches the project's recent runs.
func (m *Builds) Init() tea.Cmd {
	client := m.client
	fetch := func() tea.Msg {
		builds, err := client.Builds(buildPageSize)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch builds: %w", err)}
		}
		return buildsMsg{Builds: builds}
	}
	return tea.Batch(fetch, m.work.begin(1))
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
	return tea.Batch(tea.Batch(cmds...), m.work.begin(len(cmds)))
}

// fetchPullRequestLink looks up the pull request b ran for.
//
// The builds view holds no pull request list of its own — unlike a build's
// timeline, which every row eventually needs and so is worth fetching eagerly,
// a linked pull request is asked for on the rare row someone actually presses
// p on. Fetching the active list on demand, once per press, avoids paying for
// a second full pull request listing (and its own thread and reviewer fetches
// were it the pull request view instead) on every build the project has run.
func (m *Builds) fetchPullRequestLink(b azdo.Build) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		prs, err := client.PullRequests()
		if err != nil {
			return buildPRMsg{Build: b.ID, Branch: b.SourceBranch, Err: fmt.Errorf("could not fetch pull requests: %w", err)}
		}
		pr, found := matchBuildPullRequest(b, prs)
		return buildPRMsg{Build: b.ID, Branch: b.SourceBranch, PR: pr, Found: found}
	}
}

// matchBuildPullRequest finds the pull request a build ran for. Azure DevOps
// builds a pull request against the merge ref it maintains for it,
// refs/pull/{id}/merge, rather than the source branch — that form is checked
// first because it names the pull request directly, and is checked exactly
// once the id is on hand rather than falling through to a branch comparison
// that a merge ref could never satisfy anyway (its Source is a refs/heads
// ref). Everything else is a build triggered off a push, matched by comparing
// full refs — the build list carries no repository, so nothing here
// disambiguates same-named branches across repositories, but neither does the
// build itself.
func matchBuildPullRequest(b azdo.Build, prs []azdo.PullRequest) (azdo.PullRequest, bool) {
	if id, ok := azdo.MergeRefPullRequestID(b.SourceBranch); ok {
		for _, pr := range prs {
			if pr.ID == id {
				return pr, true
			}
		}
		return azdo.PullRequest{}, false
	}
	for _, pr := range prs {
		if pr.Source == b.SourceBranch {
			return pr, true
		}
	}
	return azdo.PullRequest{}, false
}

// Update handles input and fetch results. It satisfies View.
func (m *Builds) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case buildsMsg:
		m.work.done()
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

	case buildPRMsg:
		m.work.done()
		if m.linkBuild != msg.Build {
			// Superseded: p was pressed again for a different build before this
			// one answered. Acting on it now would jump to a pull request for a
			// build the user is no longer asking about.
			return m, nil
		}
		m.linkBuild = 0
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		if !msg.Found {
			m.status, m.failed = fmt.Sprintf("no open pull request builds %s", shortRef(msg.Branch)), false
			return m, nil
		}
		if m.hidden {
			// The lookup was still in flight when the user opened this build's
			// logs. Pushing now would yank them off that screen onto a pull
			// request they did not just ask to see — see the hidden field's doc.
			return m, nil
		}
		detail := NewPullRequestDetail(m.client, msg.PR, nil)
		return m, func() tea.Msg { return PushMsg{View: detail} }

	case ErrMsg:
		m.work.done()
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case buildQueuedMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.status, m.failed = "queued "+buildLabel(msg.Build), false
		// The run belongs on the list, and only the server knows the number
		// it was given. Refetching is the honest way to show it; synthesising
		// a row from the queue response would put a row on screen that the
		// next refresh could contradict.
		return m, m.refresh()

	case buildCancelledMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.status, m.failed = fmt.Sprintf("cancelling build %d", msg.Build), false
		// Cancelling is a request the agent has to notice, so the row does
		// not change here — the refresh is what eventually shows it stopped.
		return m, m.refresh()

	case tea.KeyMsg:
		// Receiving a key at all proves this view is back on top: Root's
		// topOnly sends a KeyMsg to the top of the stack alone, never to a
		// view sitting underneath a pushed Logs.
		m.hidden = false

		// The branch prompt owns every key while it is open, including the
		// letters that are otherwise actions — typing "Q" into it must add
		// the letter, not arm a re-run. Mirrors the branch prompt in
		// workitems.go.
		if m.branchPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.branchPrompt = nil
				m.status, m.failed = "", false
				return m, nil
			case tea.KeyEnter:
				branch := strings.TrimSpace(m.branchPrompt.Value())
				definition, label := m.queueDefinition, m.queueLabel
				m.branchPrompt = nil
				if definition == 0 {
					return m, nil
				}
				m.status, m.failed = "queueing "+label+"…", false
				return m, queueBuildCmd(m.client, definition, branch)
			}
			input, cmd := m.branchPrompt.Update(msg)
			m.branchPrompt = &input
			return m, cmd
		}

		// An armed action is the other modal state, and takes every key too,
		// so a key meant to confirm or cancel it cannot be read as something
		// else instead.
		if m.armedBuild != nil {
			return m, m.handleArmedBuild(msg)
		}

		if m.browser.Filtering() {
			break
		}

		row, hasRow := m.browser.Selected()
		if hasRow {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status.Text, status.Err
				return m, nil
			}
		}

		switch msg.String() {
		case "enter":
			r, ok := row.(buildRow)
			if !hasRow || !ok {
				return m, nil
			}
			m.hidden = true
			logs := NewLogs(m.client, r.Build, m.records[r.ID])
			return m, func() tea.Msg { return PushMsg{View: logs} }

		case "p":
			r, ok := row.(buildRow)
			if !hasRow || !ok {
				return m, nil
			}
			if m.linkBuild == r.ID {
				return m, nil // already looking this one up
			}
			m.linkBuild = r.ID
			m.status, m.failed = "looking for the pull request…", false
			return m, tea.Batch(m.fetchPullRequestLink(r.Build), m.work.begin(1))

		case "r":
			m.status, m.failed = "refreshing…", false
			return m, m.refresh()

		case keyQueue.Help().Key:
			r, ok := row.(buildRow)
			if !hasRow || !ok {
				return m, nil
			}
			if r.DefinitionID == 0 {
				m.status, m.failed = fmt.Sprintf(
					"%s does not name the pipeline definition it ran, so there is nothing to queue against",
					buildLabel(r.Build)), true
				return m, nil
			}
			m.queueDefinition, m.queueLabel = r.DefinitionID, buildLabel(r.Build)
			input := textinput.New()
			input.Prompt = "branch: "
			input.SetValue(r.SourceBranch)
			input.CursorEnd()
			input.Focus()
			m.branchPrompt = &input
			return m, textinput.Blink
		}

		if action, ok := matchBuildAction(msg); ok {
			r, isBuild := row.(buildRow)
			if !hasRow || !isBuild {
				return m, nil
			}
			m.armBuild(action, r.Build)
			return m, nil
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
		return placeholder("builds", m.status, m.failed, m.work.View())
	}
	if m.browser.Len() == 0 {
		return emptyState("no pipeline runs in this project", width, height)
	}
	return m.browser.View()
}

// refresh re-reads the list from scratch, dropping every timeline with it so
// the rows rebuild against what the server says now. It is its own method
// because three things ask for it — the refresh key and the two actions that
// change what the list should show — and a queued run that did not appear
// would read as a queue that silently failed.
func (m *Builds) refresh() tea.Cmd {
	m.progress = map[int]azdo.Progress{}
	m.records = map[int][]azdo.Record{}
	m.loading = map[int]bool{}
	m.linkBuild = 0
	return m.Init()
}

// armBuild arms an action, or reports why it does not apply.
func (m *Builds) armBuild(a buildAction, b azdo.Build) {
	armed, status := armBuildAction(a, b)
	m.armedBuild = armed
	// A refusal is reported as one: an ordinary-looking status line reads as
	// though the key had done something.
	m.status, m.failed = status, armed == nil
}

// handleArmedBuild resolves a keypress while an action is armed. esc cancels,
// the same action's key confirms and fires it, and the other action's key
// re-arms to that instead of forcing an esc round trip. Anything else is
// swallowed — the arm is modal, the same way the vote is in the pull request
// detail view.
func (m *Builds) handleArmedBuild(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, keyBack) {
		m.armedBuild = nil
		m.status, m.failed = "", false
		return nil
	}

	action, ok := matchBuildAction(msg)
	if !ok {
		return nil
	}
	if action != m.armedBuild.action {
		row, hasRow := m.browser.Selected()
		r, isBuild := row.(buildRow)
		if !hasRow || !isBuild {
			return nil
		}
		m.armBuild(action, r.Build)
		return nil
	}

	armed := m.armedBuild
	m.armedBuild = nil
	m.status, m.failed = armed.doing(), false
	return armed.fire(m.client)
}

// Title reports the run count and the project the builds belong to.
func (m *Builds) Title() string {
	return fmt.Sprintf("builds (%d) · %s/%s", m.browser.Len(), m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
func (m *Builds) Keys() help.KeyMap {
	// The footer carries the two keys that only read; the three that change
	// something are behind "?". A footer naming five actions would push esc
	// and ? off the end of it at ordinary widths, which is the defect
	// TestEveryViewsShortHelpSurvivesOrdinaryWidths exists to catch.
	short := []key.Binding{keyLogs, keyLinkedPR}
	full := []key.Binding{keyLogs, keyLinkedPR, keyRerun, keyQueue, keyCancelBuild}
	return listKeys(short, full...)
}

// Status is the transient status line, or the fuzzy filter prompt while one is
// open.
func (m *Builds) Status() (string, bool) {
	if m.branchPrompt != nil {
		return m.branchPrompt.View(), false
	}
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.work.View() + m.status, m.failed
}

// Prompting reports whether a text prompt is open, so Root leaves esc and q to
// the prompt rather than treating them as navigation.
func (m *Builds) Prompting() bool {
	return m.branchPrompt != nil || m.armedBuild != nil || m.browser.Filtering()
}
