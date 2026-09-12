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
)

// threadsLoadedMsg carries a fetched discussion. It names its pull request
// because Root broadcasts data to every view in the stack, and the list this
// was opened from is still underneath fetching counts of its own.
type threadsLoadedMsg struct {
	PR      int
	Threads []azdo.Thread
	Err     error
}

// PullRequestDetail is one pull request in full: the description, every
// reviewer's vote, and every discussion rather than the opening line of the
// unresolved ones.
//
// The name is long on purpose. PullRequests is the list, and a type differing
// from it by one letter would be a trap for the next reader.
type PullRequestDetail struct {
	client *azdo.Client
	pr     azdo.PullRequest

	viewport viewport.Model
	threads  []azdo.Thread

	loaded bool
	failed bool

	// renderedAt is the width the pane was last laid out for. Glamour hard
	// wraps, so a resize means rendering again rather than reflowing.
	renderedAt int

	status string
	work   work
	now    func() time.Time
}

// NewPullRequestDetail builds the view. threads is whatever the list had
// cached; Init fetches when it has none.
func NewPullRequestDetail(c *azdo.Client, pr azdo.PullRequest, threads []azdo.Thread) *PullRequestDetail {
	return &PullRequestDetail{
		client:   c,
		pr:       pr,
		viewport: viewport.New(0, 0),
		threads:  threads,
		loaded:   threads != nil,
		work:     newWork(),
		now:      time.Now,
	}
}

// Init fetches the discussion unless one was handed over.
func (m *PullRequestDetail) Init() tea.Cmd {
	if m.loaded {
		return nil
	}

	client, repo, id := m.client, m.pr.RepoID, m.pr.ID
	fetch := func() tea.Msg {
		threads, err := client.Threads(repo, id)
		if err != nil {
			return threadsLoadedMsg{PR: id, Err: fmt.Errorf("could not load the discussion for !%d: %w", id, err)}
		}
		return threadsLoadedMsg{PR: id, Threads: threads}
	}
	return tea.Batch(fetch, m.work.begin(1))
}

func (m *PullRequestDetail) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case threadsLoadedMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		m.work.done()
		m.loaded = true
		if msg.Err != nil {
			m.failed, m.status = true, msg.Err.Error()
		} else {
			m.threads, m.failed = msg.Threads, false
		}
		m.invalidate()
		return m, nil

	case StatusMsg:
		m.status = msg.Text
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keyBack):
			return m, func() tea.Msg { return PopMsg{} }
		case key.Matches(msg, keyTop):
			m.viewport.GotoTop()
			return m, nil
		case key.Matches(msg, keyBottom):
			m.viewport.GotoBottom()
			return m, nil
		case key.Matches(msg, keyRefresh):
			m.loaded, m.failed = false, false
			return m, m.Init()
		}

		if status, handled := SharedAction(prDetailRow{m.pr, m.client.PullRequestURL(m.pr.Repo, m.pr.ID)}, msg); handled {
			m.status = status.Text
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *PullRequestDetail) invalidate() { m.renderedAt = 0 }

func (m *PullRequestDetail) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height

	if m.renderedAt != width {
		offset := m.viewport.YOffset
		m.viewport.SetContent(m.render(width))
		m.renderedAt = width
		m.viewport.SetYOffset(offset)
	}
	return m.viewport.View()
}

func (m *PullRequestDetail) render(width int) string {
	var b strings.Builder

	title := fmt.Sprintf("!%d  %s", m.pr.ID, m.pr.Title)
	if m.pr.IsDraft {
		title += "  " + draftStyle.Render("◌ draft")
	}
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(title, width)))

	for _, field := range [][2]string{
		{"repo", m.pr.Repo},
		{"author", m.pr.Author},
		{"source", shortRef(m.pr.Source)},
		{"target", shortRef(m.pr.Target)},
		{"opened", humanAge(m.pr.Created, m.now()) + " ago"},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-8s", field[0]+":")),
			truncate(field[1], width-9))
	}

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("reviewers"))
	if len(m.pr.Reviewers) == 0 {
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(none)"))
	}
	for _, r := range m.pr.Reviewers {
		name := r.Name
		if r.IsGroup {
			// Worth marking: a group reviewer means the vote is owed by
			// whoever is in it, which is not obvious from a display name.
			name += chromeStyle.Render(" (group)")
		}
		fmt.Fprintf(&b, "  %s — %s\n", name, voteStyle(r.Vote).Render(r.VoteLabel()))
	}

	fmt.Fprintf(&b, "\n%s\n%s\n", labelStyle.Render("description"),
		renderMarkdown(m.pr.Description, "_no description_", width))

	m.renderThreads(&b, width)
	return b.String()
}

// renderThreads writes every discussion, unresolved first: those are the ones
// still waiting on somebody, and burying them under settled ones would defeat
// the point of opening the view.
func (m *PullRequestDetail) renderThreads(b *strings.Builder, width int) {
	counts := azdo.Summarize(m.threads)
	fmt.Fprintf(b, "\n%s\n", labelStyle.Render(
		fmt.Sprintf("discussion — %d resolved, %d unresolved", counts.Resolved, counts.Unresolved)))

	switch {
	case m.failed:
		fmt.Fprintf(b, "%s\n", errStyle.Render("could not load the discussion — press r to try again"))
		return
	case !m.loaded:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render(m.work.View()+"loading…"))
		return
	case len(m.threads) == 0:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("(no comments)"))
		return
	}

	for _, pass := range []bool{false, true} {
		for _, t := range m.threads {
			if t.Resolved != pass {
				continue
			}
			m.renderThread(b, t, width)
		}
	}
}

func (m *PullRequestDetail) renderThread(b *strings.Builder, t azdo.Thread, width int) {
	marker := warnStyle.Render("● unresolved")
	if t.Resolved {
		marker = statusStyle.Render("✓ resolved")
	}

	head := marker
	if t.File != "" {
		head += chromeStyle.Render("  " + truncate(t.File, max(10, width-20)))
	}
	fmt.Fprintf(b, "\n%s\n", head)

	for _, c := range t.Comments {
		fmt.Fprintf(b, "%s\n%s\n",
			labelStyle.Render(fmt.Sprintf("  %s · %s", c.Author, humanAge(c.Created, m.now()))),
			renderMarkdown(c.Text, "_(empty)_", max(20, width-2)))
	}
}

func (m *PullRequestDetail) Title() string {
	return fmt.Sprintf("!%d %s · %s · %s/%s",
		m.pr.ID, m.pr.Title, m.pr.Repo, m.client.Org, m.client.Project)
}

// Keys omits the filter: there is nothing here to filter.
func (m *PullRequestDetail) Keys() help.KeyMap {
	own := []key.Binding{keyTop, keyBottom, keyRefresh}
	return keyMap{
		short:  append(append([]key.Binding{}, own...), keyCopyID, keyBack, keyHelp),
		groups: [][]key.Binding{own, {keyCopyID, keySlack, keyOpen}, navBindings()},
	}
}

func (m *PullRequestDetail) Status() (string, bool) {
	return m.work.View() + m.status, m.failed
}

// prDetailRow adapts the pull request to the shared copy and open actions, so
// this view names and links it exactly as the list does.
type prDetailRow struct {
	azdo.PullRequest
	url string
}

func (r prDetailRow) FilterValue() string     { return "" }
func (r prDetailRow) Render(width int) string { return "" }
func (r prDetailRow) CopyID() string          { return fmt.Sprint(r.ID) }
func (r prDetailRow) Label() string           { return fmt.Sprintf("!%d %s", r.ID, r.Title) }
func (r prDetailRow) URL() string             { return r.url }
