// Package ui holds boardwalk's terminal views.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// workItemRow adapts a work item to the browser. FilterValue spans id, type,
// state, assignee and title, so typing "25701" or "atchley defect" both land.
type workItemRow struct {
	azdo.WorkItem
	url string
}

func (r workItemRow) FilterValue() string {
	return fmt.Sprintf("%d %s %s %s %s", r.ID, r.Type, r.State, r.Assigned, r.Title)
}

func (r workItemRow) Render(width int) string {
	return truncate(fmt.Sprintf("%-7d %-14s %-16s %-18s %s",
		r.ID,
		truncate("["+r.Type+"]", 14),
		truncate(r.State, 16),
		truncate(r.Assigned, 18),
		r.Title), width)
}

func (r workItemRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r workItemRow) Label() string  { return fmt.Sprintf("#%d %s", r.ID, r.Title) }
func (r workItemRow) URL() string    { return r.url }

// commentsMsg carries a fetched discussion. It names the work item it belongs
// to because a slow fetch can land after the cursor has moved on.
type commentsMsg struct {
	ID       int
	Comments []azdo.Comment
}

// WorkItems is the work item browser.
type WorkItems struct {
	client  *azdo.Client
	browser Browser

	all      []Row
	mine     []Row
	mineOnly bool

	// comments is keyed by work item id and filled lazily as the cursor moves.
	comments map[int][]azdo.Comment
	loading  map[int]bool

	status string
	failed bool
	now    func() time.Time
}

// NewWorkItems builds the work item browser from a fetched batch, splitting
// out the caller's own items up front so toggling scope is instant.
func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly bool) *WorkItems {
	m := &WorkItems{
		client:   c,
		browser:  NewBrowser(),
		mineOnly: mineOnly,
		comments: map[int][]azdo.Comment{},
		loading:  map[int]bool{},
		now:      time.Now,
	}

	for _, wi := range items {
		row := workItemRow{wi, c.WorkItemURL(wi.ID)}
		m.all = append(m.all, row)
		if c.Me != "" && wi.AssignedKey == c.Me {
			m.mine = append(m.mine, row)
		}
	}

	m.browser.Detail = m.renderDetail
	m.applyScope()
	return m
}

// Init asks for the first selected item's discussion.
func (m *WorkItems) Init() tea.Cmd { return m.fetchComments() }

func (m *WorkItems) applyScope() {
	rows := m.all
	if m.mineOnly {
		rows = m.mine
	}
	m.browser.SetRows(rows)
}

func (m *WorkItems) selected() (workItemRow, bool) {
	row, ok := m.browser.Selected()
	if !ok {
		return workItemRow{}, false
	}
	it, ok := row.(workItemRow)
	return it, ok
}

// fetchComments loads the selected item's discussion unless it is already
// loaded or in flight.
func (m *WorkItems) fetchComments() tea.Cmd {
	it, ok := m.selected()
	if !ok {
		return nil
	}
	if _, done := m.comments[it.ID]; done || m.loading[it.ID] {
		return nil
	}

	m.loading[it.ID] = true
	client, id := m.client, it.ID
	return func() tea.Msg {
		comments, err := client.Comments(id)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not load the discussion for #%d: %w", id, err)}
		}
		return commentsMsg{ID: id, Comments: comments}
	}
}

// Update handles input and fetch results. It satisfies View.
func (m *WorkItems) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case commentsMsg:
		delete(m.loading, msg.ID)
		m.comments[msg.ID] = msg.Comments
		if it, ok := m.selected(); ok && it.ID == msg.ID {
			m.browser.RefreshDetail()
		}
		return m, nil

	case ErrMsg:
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		// While the filter prompt is open every key belongs to it, or typing
		// "o" would open a browser instead of entering a letter.
		if m.browser.Filtering() {
			break
		}

		if it, ok := m.selected(); ok {
			if status, handled := SharedAction(it, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+t":
			m.mineOnly = !m.mineOnly
			m.applyScope()
			return m, m.fetchComments()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchComments())
}

func (m *WorkItems) renderDetail(row Row, width int) string {
	it, ok := row.(workItemRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(it.Label(), width)))
	for _, field := range [][2]string{
		{"type", it.Type},
		{"state", it.State},
		{"assigned", it.Assigned},
		{"tags", orDash(strings.ReplaceAll(it.Tags, "; ", ", "))},
		{"iteration", orDash(it.Iteration)},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-10s", field[0]+":")),
			truncate(field[1], width-11))
	}

	section(&b, "description", it.Description, "(no description)", width)
	section(&b, "acceptance criteria", it.AcceptanceCriteria, "(none)", width)

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("discussion"))
	switch comments, loaded := m.comments[it.ID]; {
	case !loaded:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("loading…"))
	case len(comments) == 0:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(no comments)"))
	default:
		for _, c := range comments {
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(fmt.Sprintf("%s · %s", c.Author, humanAge(c.Created, m.now()))),
				wordwrap(c.Text, width))
		}
	}
	return b.String()
}

// section writes a titled block, or a placeholder when the field is empty.
func section(b *strings.Builder, title, text, empty string, width int) {
	if strings.TrimSpace(text) == "" {
		text = empty
	}
	fmt.Fprintf(b, "\n%s\n%s\n", labelStyle.Render(title), wordwrap(text, width))
}

// Body renders the browser at the size Root has left for it.
func (m *WorkItems) Body(width, height int) string {
	m.browser.SetSize(width, height)
	return m.browser.View()
}

// Title reports the current scope and the project the items belong to.
func (m *WorkItems) Title() string {
	scope := fmt.Sprintf("all %d", len(m.all))
	if m.mineOnly {
		scope = fmt.Sprintf("mine %d", len(m.mine))
	}
	return fmt.Sprintf("work items (%s) · %s/%s", scope, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
func (m *WorkItems) Hints() string {
	return "^t mine/all · " + SharedHints + " · esc back"
}

// Status is the transient status line, or the filter prompt while it is open.
func (m *WorkItems) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
