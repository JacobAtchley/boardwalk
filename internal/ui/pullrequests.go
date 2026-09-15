package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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
	PR      int
	Threads []azdo.Thread
	Err     error
}

// PullRequests is the pull request browser.
type PullRequests struct {
	client  *azdo.Client
	browser Browser

	prs    []azdo.PullRequest
	counts map[int]azdo.ThreadCounts
	// threads keeps what the counts were derived from, so opening a pull
	// request does not refetch a discussion the list already has.
	threads map[int][]azdo.Thread
	loading map[int]bool

	drafts   draftFilter
	repo     string // the working directory's repository, if it is one
	repoOnly bool

	// armedDraft is non-nil while a draft toggle is waiting on its confirming
	// keystroke. While it is set this view's other keys are swallowed: see
	// draft.go for why the toggle confirms at all, and the filter test in
	// draft_test.go for why d in particular must not get through — cycling the
	// filter would re-sort the list under a toggle armed on one of its rows.
	armedDraft *armedDraft

	// reviewGroups are the teams and security groups the user belongs to, from
	// the config file. A pull request can name a group as its reviewer instead
	// of a person, and the payload does not say who is in it.
	reviewGroups []string
	// mineToReview narrows the list to pull requests waiting on this user.
	mineToReview bool

	loaded bool
	status string
	failed bool
	work   work
	now    func() time.Time
}

// NewPullRequests builds the pull request browser. It fetches nothing itself —
// Init does that — so it can be constructed before the client is ready to talk
// to the network in a test.
func NewPullRequests(c *azdo.Client, reviewGroups []string) *PullRequests {
	m := &PullRequests{
		client:       c,
		reviewGroups: reviewGroups,
		browser:      NewBrowser(),
		counts:       map[int]azdo.ThreadCounts{},
		threads:      map[int][]azdo.Thread{},
		work:         newWork(),
		loading:      map[int]bool{},
		repo:         azdo.CurrentRepo(),
		now:          time.Now,
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
	fetch := func() tea.Msg {
		prs, err := client.PullRequests()
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch pull requests: %w", err)}
		}
		return prsMsg{PRs: prs}
	}
	return tea.Batch(fetch, m.work.begin(1))
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
		if m.mineToReview && !azdo.NeedsReviewFrom(pr, m.client.Me, m.reviewGroups) {
			continue
		}
		out = append(out, pr)
	}
	return out
}

// setDraft rewrites the fetched pull request in place, so the row badge and
// the draft filter both read the new state without a refetch — the answer is
// already known: it is what was just sent and accepted.
func (m *PullRequests) setDraft(id int, draft bool) {
	for i, pr := range m.prs {
		if pr.ID == id {
			m.prs[i].IsDraft = draft
			return
		}
	}
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
			threads, err := client.Threads(repoID, prID)
			return threadsMsg{PR: prID, Threads: threads, Err: err}
		})
	}
	return tea.Batch(tea.Batch(cmds...), m.work.begin(len(cmds)))
}

// Update handles input and fetch results. It satisfies View.
func (m *PullRequests) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case prsMsg:
		m.work.done()
		m.prs, m.loaded = msg.PRs, true
		m.applyFilters()
		return m, m.fetchThreads()

	case threadsMsg:
		// Cleared before the error check: a failed fetch must still allow a
		// later selection to retry, rather than being stuck "loading…" forever.
		m.work.done()
		delete(m.loading, msg.PR)
		if msg.Err != nil {
			m.status, m.failed = fmt.Sprintf("could not load threads for !%d: %v", msg.PR, msg.Err), true
			return m, nil
		}
		m.threads[msg.PR] = msg.Threads
		m.counts[msg.PR] = azdo.Summarize(msg.Threads)
		m.applyFilters()
		return m, nil

	case ErrMsg:
		m.work.done()
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case draftSetMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.setDraft(msg.PR, msg.Draft)
		m.status, m.failed = fmt.Sprintf("!%d %s", msg.PR, draftDone(msg.Draft)), false
		m.applyFilters()
		return m, nil

	case tea.KeyMsg:
		if m.browser.Filtering() {
			break
		}

		// An armed toggle takes every key: the confirming press, esc, and
		// nothing else. It is checked before the shared copy and open actions
		// below for the same reason — y, s and o must not act while the
		// status line is asking a question.
		if m.armedDraft != nil {
			confirm, cancel := resolveDraftKey(msg)
			switch {
			case cancel:
				m.armedDraft = nil
				m.status, m.failed = "", false
			case confirm:
				armed := m.armedDraft
				m.armedDraft = nil
				m.status, m.failed = draftDoing(armed.draft), false
				return m, setDraftCmd(m.client, armed.repoID, armed.prID, armed.draft)
			}
			return m, nil
		}

		if row, ok := m.browser.Selected(); ok {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status.Text, status.Err
				return m, nil
			}
		}

		// Matched against the binding rather than by letter, unlike the cases
		// below: this is the one key here that is also matched elsewhere —
		// resolveDraftKey reads it to confirm — and the two must not be able
		// to disagree about which key that is.
		if key.Matches(msg, keyDraftToggle) {
			row, ok := m.browser.Selected()
			if !ok {
				return m, nil
			}
			r, ok := row.(prRow)
			if !ok {
				return m, nil
			}
			m.armedDraft, m.status = armDraft(r.PullRequest)
			// A refusal is reported as one: the key did nothing, and an
			// ordinary-looking status line reads as though it had.
			m.failed = m.armedDraft == nil
			return m, nil
		}

		switch msg.String() {
		case "d":
			m.drafts = (m.drafts + 1) % 3
			m.applyFilters()
			return m, m.fetchThreads()
		case "enter":
			if row, ok := m.browser.Selected(); ok {
				if r, ok := row.(prRow); ok {
					detail := NewPullRequestDetail(m.client, r.PullRequest, m.threads[r.ID])
					return m, func() tea.Msg { return PushMsg{View: detail} }
				}
			}
			return m, nil

		case "v":
			m.mineToReview = !m.mineToReview
			m.applyFilters()
			return m, m.fetchThreads()

		case "ctrl+t":
			m.repoOnly = !m.repoOnly
			m.applyFilters()
			return m, m.fetchThreads()
		case "r":
			m.counts = map[int]azdo.ThreadCounts{}
			m.loading = map[int]bool{}
			m.status, m.failed = "refreshing…", false
			return m, m.Init()
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
			opener := t.Opener()
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(opener.Author), wordwrap(opener.Text, width))
		}
	}
	return b.String()
}

// Body renders the browser at the size Root has left for it.
func (m *PullRequests) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return placeholder("pull requests", m.status, m.failed, m.work.View())
	}
	if m.browser.Len() == 0 {
		return emptyState(m.emptyMessage(), width, height)
	}
	return m.browser.View()
}

// emptyMessage names what is missing, in the terms of whichever filter emptied
// the list. An unconfigured review group empties it for a reason that is not
// about the project at all, so that case says so rather than reporting a quiet
// queue the user has no way to see into.
func (m *PullRequests) emptyMessage() string {
	switch {
	case m.mineToReview && len(m.reviewGroups) == 0:
		return "no review groups are configured, so nothing can match"
	case m.mineToReview:
		return "nothing waiting on your review"
	case m.repoOnly && m.repo != "":
		return "no pull requests open in " + m.repo
	default:
		return "no pull requests open in this project"
	}
}

// Title reports the current filters and the project the pull requests belong
// to.
func (m *PullRequests) Title() string {
	scope := "all repos"
	if m.repoOnly && m.repo != "" {
		scope = m.repo
	}
	// The review filter is named in the title because it is the one that can
	// empty the list, and a list that is empty for a reason the user cannot see
	// reads as a broken fetch.
	review := ""
	if m.mineToReview {
		review = " · needs my review"
		if len(m.reviewGroups) == 0 {
			review += " (no groups configured)"
		}
	}
	return fmt.Sprintf("pull requests (%d · %s · %s%s) · %s/%s",
		m.browser.Len(), scope, m.drafts, review, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
//
// The footer carries opening a pull request and the two scope toggles;
// needs-my-review is one press of "?" away instead. All four together, plus
// filter and refresh, is what pushed esc and ? off the footer at 80 columns.
// See listKeys's own doc.
func (m *PullRequests) Keys() help.KeyMap {
	short := []key.Binding{keyPullRequest, keyDrafts, keyScope}
	// The draft toggle is labelled for the row under the cursor, so the panel
	// reads "publish" on a draft and "mark draft" on a published one rather
	// than making the reader work out which way the key goes. No draftable
	// check here, unlike the detail view: this list asks for active pull
	// requests and holds nothing else.
	toggle := keyDraftToggle
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(prRow); ok {
			toggle = draftBinding(r.IsDraft)
		}
	}
	full := []key.Binding{keyPullRequest, keyReview, keyDrafts, keyScope, toggle}
	return listKeys(short, full...)
}

// Status is the transient status line, or the fuzzy filter prompt while one is
// open.
func (m *PullRequests) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.work.View() + m.status, m.failed
}

// Prompting reports whether a text prompt or an armed draft toggle is open, so
// Root leaves esc and q to this view rather than treating them as navigation —
// esc has to cancel the arm, not pop the pane out from under it.
func (m *PullRequests) Prompting() bool { return m.browser.Filtering() || m.armedDraft != nil }
