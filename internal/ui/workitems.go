// Package ui holds boardwalk's terminal views.
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

// workItemsMsg carries the project's work items, fetched when the view is
// entered rather than before the program starts.
type workItemsMsg struct{ Items []azdo.WorkItem }

// WorkItems is the work item browser.
type WorkItems struct {
	client  *azdo.Client
	browser Browser

	all      []Row
	mine     []Row
	mineOnly bool

	// includeClosed is the -all scope, kept so a refresh — and the fetch on
	// entry — asks for the same set the view was opened with.
	includeClosed bool

	// loaded is false until a batch has landed, whether handed to the
	// constructor or fetched on entry, so the body can say which it is.
	loaded bool

	status string
	failed bool
	work   work
	now    func() time.Time

	// branchPrompt is non-nil while the branch name is being edited.
	branchPrompt *textinput.Model

	// statePicker is non-nil while the full state list is open. It and
	// branchPrompt are never both set: each one's key (b, S) only reaches the
	// switch below once neither modal state has already claimed the keyboard.
	statePicker *statePicker
}

// NewWorkItems builds the work item browser. items may be empty, which means
// "fetch on entry" — the ordinary path now that the program no longer fetches
// before the TUI starts. -dump still fetches synchronously and hands the batch
// over here, and so do the tests. includeClosed mirrors the -all flag, so a
// fetch this view runs itself asks for the same scope.
func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly, includeClosed bool) *WorkItems {
	m := &WorkItems{
		client:        c,
		browser:       NewBrowser(),
		mineOnly:      mineOnly,
		includeClosed: includeClosed,
		work:          newWork(),
		now:           time.Now,
	}

	m.browser.Detail = m.renderDetail
	m.setItems(items)
	m.loaded = len(items) > 0
	return m
}

// setItems splits the caller's own items out of the batch up front, so
// toggling scope is instant.
func (m *WorkItems) setItems(items []azdo.WorkItem) {
	m.all = nil
	for _, wi := range items {
		m.all = append(m.all, workItemRow{wi, m.client.WorkItemURL(wi.ID)})
	}
	m.rebuildMine()
	m.applyScope()
}

// rebuildMine recomputes the mine subset from all. It is its own step rather
// than folded into setItems: an assignee change can move a row into or out
// of the mine scope, which a state change never does, so setRowAssignee
// needs to redo this filter while setRowState does not.
func (m *WorkItems) rebuildMine() {
	m.mine = nil
	for _, row := range m.all {
		if it, ok := row.(workItemRow); ok && m.client.Me != "" && it.AssignedKey == m.client.Me {
			m.mine = append(m.mine, row)
		}
	}
}

// Init fetches the project's work items when the view has none, and otherwise
// asks for the selected item's discussion. Root calls it when the view is
// pushed, so the menu paints without waiting on the network and a failed fetch
// lands on the status line instead of exiting the program.
func (m *WorkItems) Init() tea.Cmd {
	if !m.loaded {
		return m.fetchItems()
	}
	return nil
}

// fetchItems reads the project's work items off the UI goroutine.
func (m *WorkItems) fetchItems() tea.Cmd {
	client, includeClosed := m.client, m.includeClosed
	fetch := func() tea.Msg {
		items, err := client.WorkItems(includeClosed)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch work items: %w", err)}
		}
		return workItemsMsg{Items: items}
	}
	return tea.Batch(fetch, m.work.begin(1))
}

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

// Update handles input and fetch results. It satisfies View.
func (m *WorkItems) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case workItemsMsg:
		m.work.done()
		m.setItems(msg.Items)
		m.loaded = true
		m.status, m.failed = "", false
		return m, nil

	case ErrMsg:
		m.work.done()
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
		// The checkout command is only worth handing over once the ref is
		// really on the server, but it is worth handing over even when a
		// later step failed — the branch is there either way, and retyping it
		// by hand is exactly what this saves.
		copied := false
		if msg.Created {
			copied = copyToClipboard(CheckoutCommand(msg.Branch)) == nil
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
		// boardwalk cannot move another shell's working tree, so the checkout
		// goes on the clipboard: open a shell in the repository and paste.
		if copied {
			m.status += " · checkout command copied — paste it in the repository"
		} else {
			m.status += " · could not copy the checkout command: " + CheckoutCommand(msg.Branch)
		}
		return m, nil

	case stateSetMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.setRowState(msg.ID, msg.State)
		m.status, m.failed = fmt.Sprintf("#%d is now %s", msg.ID, msg.State), false
		return m, nil

	case assigneeSetMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.setRowAssignee(msg.ID, msg.Assigned, msg.AssignedKey)
		m.status, m.failed = fmt.Sprintf("#%d is now assigned to you", msg.ID), false
		return m, nil

	case statesFetchedMsg:
		// Named by id, like every other broadcast message in this file: the
		// picker this landed for may have already been cancelled, or reopened
		// on a different row.
		if m.statePicker == nil || m.statePicker.itemID != msg.ID {
			return m, nil
		}
		m.statePicker.resolve(msg)
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

		// The state picker owns every key while it is open, the same
		// discipline the branch prompt above uses for its own modal state.
		if m.statePicker != nil {
			switch msg.String() {
			case "esc":
				m.statePicker = nil
				m.status, m.failed = "", false
			case "enter":
				state, ok := m.statePicker.selected()
				id := m.statePicker.itemID
				m.statePicker = nil
				if !ok {
					return m, nil
				}
				m.status, m.failed = fmt.Sprintf("setting #%d %s…", id, state), false
				return m, stateCmd(m.client, id, state)
			case "up", "k":
				m.statePicker.up()
			case "down", "j":
				m.statePicker.down()
			}
			return m, nil
		}

		// While the filter prompt is open every key belongs to it, or typing
		// "o" would open a browser instead of entering a letter.
		if m.browser.Filtering() {
			break
		}

		if it, ok := m.selected(); ok {
			if status, handled := SharedAction(it, msg); handled {
				m.status, m.failed = status.Text, status.Err
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+t":
			m.mineOnly = !m.mineOnly
			m.applyScope()
			return m, nil

		case "enter":
			if it, ok := m.selected(); ok {
				item := NewItem(m.client, it.WorkItem, nil)
				return m, func() tea.Msg { return PushMsg{View: item} }
			}
			return m, nil

		case "a":
			if it, ok := m.selected(); ok {
				m.status, m.failed = fmt.Sprintf("setting #%d Active…", it.ID), false
				return m, stateCmd(m.client, it.ID, "Active")
			}
			return m, nil

		case "m":
			it, ok := m.selected()
			if !ok {
				return m, nil
			}
			// An empty Me means az account show failed at startup — sending it
			// as the assignee would unassign the item instead of claiming it,
			// which is the opposite of what the key means and destructive.
			if m.client.Me == "" {
				m.status, m.failed = "cannot assign: the signed-in user is unknown", true
				return m, nil
			}
			m.status, m.failed = fmt.Sprintf("assigning #%d to you…", it.ID), false
			return m, assignCmd(m.client, it.ID)

		case "S":
			if it, ok := m.selected(); ok {
				m.statePicker = newStatePicker(it.ID)
				return m, statesCmd(m.client, it.ID, it.Type)
			}
			return m, nil

		case "r":
			m.status, m.failed = "refreshing…", false
			return m, m.fetchItems()

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
	return m, cmd
}

// renderDetail is a summary, not the item. It carries what identifies a row
// while the cursor moves over it; the description, the acceptance criteria and
// the discussion need room to be readable and live behind enter, in Item.
func (m *WorkItems) renderDetail(row Row, width int) string {
	it, ok := row.(workItemRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(it.Label(), width)))
	for _, field := range [][2]string{
		{"assigned", it.Assigned},
		{"tags", orDash(strings.ReplaceAll(it.Tags, "; ", ", "))},
		{"iteration", orDash(it.Iteration)},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-10s", field[0]+":")),
			truncate(field[1], width-11))
	}

	fmt.Fprintf(&b, "\n%s\n", chromeStyle.Render("enter for the full item"))
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

// setRowAssignee rewrites a row after a successful assign, updating Assigned
// and AssignedKey together — a refetch-free update that only touched the
// display name would leave the mine scope filter, which reads AssignedKey,
// disagreeing with what the row now shows. Unlike setRowState, mine is
// rebuilt rather than patched in place: the row is newly appearing in it,
// not already there under a stale value.
func (m *WorkItems) setRowAssignee(id int, assigned, assignedKey string) {
	for i, row := range m.all {
		if it, ok := row.(workItemRow); ok && it.ID == id {
			it.Assigned, it.AssignedKey = assigned, assignedKey
			m.all[i] = it
		}
	}
	m.rebuildMine()
	m.applyScope()
	m.browser.RefreshDetail()
}

// Body renders the browser at the size Root has left for it.
func (m *WorkItems) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return placeholder("work items", m.status, m.failed, m.work.View())
	}
	if m.browser.Len() == 0 {
		return emptyState(m.emptyMessage(), width, height)
	}
	return m.browser.View()
}

// emptyMessage names what is missing. The scope matters: "nothing assigned to
// you" and "no work items in this project" are very different pieces of news,
// and the second one arriving when the first is true reads as a broken fetch.
func (m *WorkItems) emptyMessage() string {
	if m.mineOnly {
		return "nothing assigned to you"
	}
	if !m.includeClosed {
		return "no open work items in this project"
	}
	return "no work items in this project"
}

// Title reports the current scope and the project the items belong to.
func (m *WorkItems) Title() string {
	if !m.loaded {
		return fmt.Sprintf("work items (fetching) · %s/%s", m.client.Org, m.client.Project)
	}
	scope := fmt.Sprintf("all %d", len(m.all))
	if m.mineOnly {
		scope = fmt.Sprintf("mine %d", len(m.mine))
	}
	return fmt.Sprintf("work items (%s) · %s/%s", scope, m.client.Org, m.client.Project)
}

// Hints is the key line at the bottom.
//
// The footer carries only opening an item, the fast path to Active, and
// assigning to yourself — the three reached for constantly while triaging a
// list. Scope, set state and starting a branch are one press of "?" away
// instead: all six together, plus filter and refresh, is what pushed esc and
// ? off the footer at 80 columns. See listKeys's own doc.
func (m *WorkItems) Keys() help.KeyMap {
	short := []key.Binding{keyItem, keyActive, keyAssign}
	full := []key.Binding{keyItem, keyScope, keyActive, keyAssign, keyState, keyBranch}
	return listKeys(short, full...)
}

// Status is the transient status line, or a prompt while one is open: the
// branch prompt and the state picker take priority over the fuzzy filter
// since only one of the three can be open at a time.
func (m *WorkItems) Status() (string, bool) {
	if m.branchPrompt != nil {
		return m.branchPrompt.View(), false
	}
	if m.statePicker != nil {
		return m.statePicker.View(), false
	}
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.work.View() + m.status, m.failed
}

// Prompting reports whether a text prompt or the state picker is open, so
// Root leaves esc and q to it rather than treating them as navigation.
func (m *WorkItems) Prompting() bool {
	return m.branchPrompt != nil || m.statePicker != nil || m.browser.Filtering()
}
