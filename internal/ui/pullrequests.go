package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// eagerThreads is how many rows' comment threads are fetched up front. Beyond
// that they load as the cursor reaches them, so a project with two hundred open
// pull requests still opens instantly.
const eagerThreads = 20

// draftFilter is the three-state draft toggle.
type draftFilter int

const (
	draftsHidden draftFilter = iota
	draftsOnly
	draftsAll
)

func (f draftFilter) String() string {
	switch f {
	case draftsOnly:
		return "drafts only"
	case draftsAll:
		return "drafts shown"
	default:
		return "drafts hidden"
	}
}

func (f draftFilter) keeps(isDraft bool) bool {
	switch f {
	case draftsOnly:
		return isDraft
	case draftsAll:
		return true
	default:
		return !isDraft
	}
}

// prRow adapts a pull request to the browser.
type prRow struct {
	azdo.PullRequest
	url    string
	counts *azdo.ThreadCounts // nil until the threads fetch lands
	now    time.Time
}

func (r prRow) FilterValue() string {
	return fmt.Sprintf("%d %s %s %s %s", r.ID, r.Repo, r.Author, shortRef(r.Source), r.Title)
}

func (r prRow) Render(width int) string {
	draft := " "
	if r.IsDraft {
		draft = draftStyle.Render("◌")
	}

	return truncate(fmt.Sprintf("%-16s %-6s %s %-34s %-28s %-7s %s", // draft is one display column, styled or not
		truncate(r.Repo, 16),
		fmt.Sprintf("!%d", r.ID),
		draft,
		truncate(r.Title, 34),
		truncate(shortRef(r.Source)+" → "+shortRef(r.Target), 28),
		r.countColumn(),
		humanAge(r.Created, r.now)), width)
}

// countColumn reads resolved over unresolved, and stays an ellipsis until the
// threads for this row have been fetched.
func (r prRow) countColumn() string {
	if r.counts == nil {
		return "…"
	}
	return fmt.Sprintf("%d/%d", r.counts.Resolved, r.counts.Unresolved)
}

func (r prRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r prRow) Label() string  { return fmt.Sprintf("!%d %s", r.ID, r.Title) }
func (r prRow) URL() string    { return r.url }

// prsMsg carries the project's active pull requests, fetched once at startup.
type prsMsg struct{ PRs []azdo.PullRequest }

// threadsMsg carries one pull request's fetched comment counts. It names the
// pull request because a slow fetch can land after the cursor has moved on,
// and it carries Err rather than losing the row on a failed fetch.
type threadsMsg struct {
	PR     int
	Counts azdo.ThreadCounts
	Err    error
}

// PullRequests is the pull request browser.
type PullRequests struct {
	client  *azdo.Client
	browser Browser

	prs     []azdo.PullRequest
	counts  map[int]azdo.ThreadCounts
	loading map[int]bool

	drafts   draftFilter
	repo     string // the working directory's repository, if it is one
	repoOnly bool

	loaded bool
	status string
	failed bool
	now    func() time.Time
}

// NewPullRequests builds the pull request browser. It fetches nothing itself —
// Init does that — so it can be constructed before the client is ready to talk
// to the network in a test.
func NewPullRequests(c *azdo.Client) *PullRequests {
	m := &PullRequests{
		client:  c,
		browser: NewBrowser(),
		counts:  map[int]azdo.ThreadCounts{},
		loading: map[int]bool{},
		repo:    azdo.CurrentRepo(),
		now:     time.Now,
	}
	// Starting narrow when boardwalk is run inside a repository matches what
	// the user is looking at; ^t widens.
	m.repoOnly = m.repo != ""
	m.browser.Detail = m.renderDetail
	return m
}

// Init fetches the project's active pull requests.
func (m *PullRequests) Init() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		prs, err := client.PullRequests()
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch pull requests: %w", err)}
		}
		return prsMsg{PRs: prs}
	}
}

// visible applies the draft filter and repository scope to the fetched batch.
func (m *PullRequests) visible() []azdo.PullRequest {
	var out []azdo.PullRequest
	for _, pr := range m.prs {
		if !m.drafts.keeps(pr.IsDraft) {
			continue
		}
		if m.repoOnly && m.repo != "" && pr.Repo != m.repo {
			continue
		}
		out = append(out, pr)
	}
	return out
}

// applyFilters rebuilds the browser's rows from the current filters, carrying
// forward whatever thread counts have already loaded.
func (m *PullRequests) applyFilters() {
	prs := m.visible()
	rows := make([]Row, 0, len(prs))
	for _, pr := range prs {
		row := prRow{
			PullRequest: pr,
			url:         m.client.PullRequestURL(pr.Repo, pr.ID),
			now:         m.now(),
		}
		if counts, ok := m.counts[pr.ID]; ok {
			row.counts = &counts
		}
		rows = append(rows, row)
	}
	m.browser.SetRows(rows)
}

// fetchThreads loads the first screenful of rows up front and whatever the
// cursor has reached since.
func (m *PullRequests) fetchThreads() tea.Cmd {
	prs := m.visible()

	want := map[int]azdo.PullRequest{}
	for i, pr := range prs {
		if i < eagerThreads {
			want[pr.ID] = pr
		}
	}
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(prRow); ok {
			want[r.ID] = r.PullRequest
		}
	}

	var cmds []tea.Cmd
	for id, pr := range want {
		if _, done := m.counts[id]; done || m.loading[id] {
			continue
		}
		m.loading[id] = true

		client, repoID, prID := m.client, pr.RepoID, pr.ID
		cmds = append(cmds, func() tea.Msg {
			counts, err := client.Threads(repoID, prID)
			return threadsMsg{PR: prID, Counts: counts, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

// Update handles input and fetch results. It satisfies View.
func (m *PullRequests) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case prsMsg:
		m.prs, m.loaded = msg.PRs, true
		m.applyFilters()
		return m, m.fetchThreads()

	case threadsMsg:
		// Cleared before the error check: a failed fetch must still allow a
		// later selection to retry, rather than being stuck "loading…" forever.
		delete(m.loading, msg.PR)
		if msg.Err != nil {
			m.status, m.failed = fmt.Sprintf("could not load threads for !%d: %v", msg.PR, msg.Err), true
			return m, nil
		}
		m.counts[msg.PR] = msg.Counts
		m.applyFilters()
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

		if row, ok := m.browser.Selected(); ok {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "d":
			m.drafts = (m.drafts + 1) % 3
			m.applyFilters()
			return m, m.fetchThreads()
		case "ctrl+t":
			m.repoOnly = !m.repoOnly
			m.applyFilters()
			return m, m.fetchThreads()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchThreads())
}

func (m *PullRequests) renderDetail(row Row, width int) string {
	r, ok := row.(prRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(r.Label(), width)))
	for _, field := range [][2]string{
		{"repo", r.Repo},
		{"author", r.Author},
		{"source", shortRef(r.Source)},
		{"target", shortRef(r.Target)},
		{"opened", humanAge(r.Created, r.now) + " ago"},
		{"threads", r.countColumn() + " resolved/unresolved"},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-9s", field[0]+":")),
			truncate(field[1], width-10))
	}

	if len(r.Reviewers) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("reviewers"))
		for _, rev := range r.Reviewers {
			fmt.Fprintf(&b, "  %s — %s\n", truncate(rev.Name, width-24), rev.VoteLabel())
		}
	}

	section(&b, "description", r.Description, "(no description)", width)

	if r.counts != nil && len(r.counts.Open) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("unresolved"))
		for _, t := range r.counts.Open {
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(t.Author), wordwrap(t.Text, width))
		}
	}
	return b.String()
}

// Body renders the browser at the size Root has left for it.
func (m *PullRequests) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return chromeStyle.Render("fetching pull requests…")
	}
	return m.browser.View()
}

// Title reports the current filters and the project the pull requests belong
// to.
func (m *PullRequests) Title() string {
	scope := "all repos"
	if m.repoOnly && m.repo != "" {
		scope = m.repo
	}
	return fmt.Sprintf("pull requests (%d · %s · %s) · %s/%s",
		m.browser.Len(), scope, m.drafts, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
func (m *PullRequests) Hints() string {
	return "d drafts · ^t repo/all · " + SharedHints + " · esc back"
}

// Status is the transient status line, or the fuzzy filter prompt while one is
// open.
func (m *PullRequests) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
