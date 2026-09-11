// Package ui holds boardwalk's terminal views.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/textinput"
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

// commentsErrMsg is a discussion fetch that failed. It names its work item so
// the in-flight guard can be cleared — without the id the item would stay
// marked as loading forever and never be retried.
type commentsErrMsg struct {
	ID  int
	Err error
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
	// discussionErr marks an id whose discussion fetch failed, so the pane
	// can say so honestly instead of reading "loading…" forever.
	discussionErr map[int]bool

	status string
	failed bool
	now    func() time.Time

	// branchPrompt is non-nil while the branch name is being edited.
	branchPrompt *textinput.Model
}

// NewWorkItems builds the work item browser from a fetched batch, splitting
// out the caller's own items up front so toggling scope is instant.
func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly bool) *WorkItems {
	m := &WorkItems{
		client:        c,
		browser:       NewBrowser(),
		mineOnly:      mineOnly,
		comments:      map[int][]azdo.Comment{},
		loading:       map[int]bool{},
		discussionErr: map[int]bool{},
		now:           time.Now,
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
	// A retry after a prior failure should show "loading…" again rather than
	// the stale failure message while the new attempt is in flight.
	delete(m.discussionErr, it.ID)
	client, id := m.client, it.ID
	return func() tea.Msg {
		comments, err := client.Comments(id)
		if err != nil {
			return commentsErrMsg{ID: id, Err: fmt.Errorf("could not load the discussion for #%d: %w", id, err)}
		}
		return commentsMsg{ID: id, Comments: comments}
	}
}

// Update handles input and fetch results. It satisfies View.
func (m *WorkItems) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case commentsMsg:
		delete(m.loading, msg.ID)
		delete(m.discussionErr, msg.ID)
		m.comments[msg.ID] = msg.Comments
		if it, ok := m.selected(); ok && it.ID == msg.ID {
			m.browser.RefreshDetail()
		}
		return m, nil

	case commentsErrMsg:
		delete(m.loading, msg.ID)
		m.discussionErr[msg.ID] = true
		m.status, m.failed = msg.Err.Error(), true
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

	case branchDoneMsg:
		// The state change is real on the server whenever it landed, even if
		// a later step (the link) then failed — so the row is synced off the
		// Activated flag, not off whether the whole flow succeeded.
		if msg.Activated {
			m.setRowState(msg.ID, "Active")
		}
		if msg.Err != nil {
			m.failed = true
			m.status = msg.Err.Error()
			if len(msg.Steps) > 0 {
				m.status = strings.Join(msg.Steps, ", ") + "; then " + msg.Err.Error()
			}
			return m, nil
		}
		m.status, m.failed = strings.Join(msg.Steps, " · "), false
		// The shell has to do the checkout: a child process cannot move its
		// parent's working tree.
		return m, func() tea.Msg {
			return ShellCommandMsg{Command: fmt.Sprintf("git fetch origin && git checkout %s", msg.Branch)}
		}

	case stateSetMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.setRowState(msg.ID, msg.State)
		m.status, m.failed = fmt.Sprintf("#%d is now %s", msg.ID, msg.State), false
		return m, nil

	case tea.KeyMsg:
		// The branch prompt owns every key while it is open, including esc
		// and the letters that are otherwise actions — typing "o" into it
		// must add the letter, not open a browser.
		if m.branchPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.branchPrompt = nil
				m.status, m.failed = "", false
				return m, nil
			case tea.KeyEnter:
				branch := strings.TrimSpace(m.branchPrompt.Value())
				m.branchPrompt = nil
				it, ok := m.selected()
				if !ok || branch == "" {
					return m, nil
				}
				m.status, m.failed = "creating "+branch+"…", false
				return m, branchCmd(m.client, it.ID, branch)
			}
			input, cmd := m.branchPrompt.Update(msg)
			m.branchPrompt = &input
			return m, cmd
		}

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

		case "a":
			if it, ok := m.selected(); ok {
				m.status, m.failed = fmt.Sprintf("setting #%d Active…", it.ID), false
				return m, stateCmd(m.client, it.ID, "Active")
			}
			return m, nil

		case "b":
			it, ok := m.selected()
			if !ok {
				return m, nil
			}
			input := textinput.New()
			input.Prompt = "branch: "
			input.SetValue(branchName(it.Type, it.ID, it.Title))
			input.CursorEnd()
			input.Focus()
			m.branchPrompt = &input
			return m, textinput.Blink
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
		{"iteration", orDash(it.Iteration)},
		{"tags", orDash(strings.ReplaceAll(it.Tags, "; ", ", "))},
		{"assigned", it.Assigned},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-10s", field[0]+":")),
			truncate(field[1], width-11))
	}

	section(&b, "description", it.Description, "(no description)", width)
	section(&b, "acceptance criteria", it.AcceptanceCriteria, "(none)", width)

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("discussion"))
	switch comments, loaded := m.comments[it.ID]; {
	case m.discussionErr[it.ID]:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(could not load the discussion)"))
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

// setRowState rewrites a row in place after a successful state change, so the
// list agrees with the server without refetching the project.
func (m *WorkItems) setRowState(id int, state string) {
	for _, set := range [][]Row{m.all, m.mine} {
		for i, row := range set {
			if it, ok := row.(workItemRow); ok && it.ID == id {
				it.State = state
				set[i] = it
			}
		}
	}
	m.applyScope()
	m.browser.RefreshDetail()
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
	return "^t mine/all · a active · b branch · " + SharedHints + " · esc back"
}

// Status is the transient status line, or a prompt while one is open: the
// branch prompt takes priority over the fuzzy filter since only one of the
// two can be open at a time.
func (m *WorkItems) Status() (string, bool) {
	if m.branchPrompt != nil {
		return m.branchPrompt.View(), false
	}
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}

// Prompting reports whether a text prompt is open, so Root leaves esc and q to
// the prompt rather than treating them as navigation.
func (m *WorkItems) Prompting() bool {
	return m.branchPrompt != nil || m.browser.Filtering()
}
