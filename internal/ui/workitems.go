// Package ui holds boardwalk's terminal views.
package ui

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// item adapts a work item to the list widget. FilterValue is what the fuzzy
// matcher sees, so it spans id, type, state, assignee and title — typing
// "25701" or "atchley defect" both land.
type item struct{ azdo.WorkItem }

func (i item) FilterValue() string {
	return fmt.Sprintf("%d %s %s %s %s", i.ID, i.Type, i.State, i.Assigned, i.Title)
}

type itemDelegate struct{ width int }

func (d itemDelegate) Height() int                         { return 1 }
func (d itemDelegate) Spacing() int                        { return 0 }
func (d itemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok {
		return
	}

	row := fmt.Sprintf("%-7d %-14s %-16s %-18s %s",
		it.ID,
		truncate("["+it.Type+"]", 14),
		truncate(it.State, 16),
		truncate(it.Assigned, 18),
		it.Title)
	row = truncate(row, d.width)

	if index == m.Index() {
		fmt.Fprint(w, selectedRow.Render("▸ "+row))
		return
	}
	fmt.Fprint(w, normalRow.Render("  "+row))
}

type keymap struct {
	toggle key.Binding
	open   key.Binding
	copyID key.Binding
	slack  key.Binding
	branch key.Binding
	quit   key.Binding
}

var keys = keymap{
	toggle: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "mine/all")),
	open:   key.NewBinding(key.WithKeys("ctrl+o", "o"), key.WithHelp("o", "open")),
	copyID: key.NewBinding(key.WithKeys("ctrl+y", "y"), key.WithHelp("y", "copy id")),
	slack:  key.NewBinding(key.WithKeys("ctrl+s", "s"), key.WithHelp("s", "slack")),
	branch: key.NewBinding(key.WithKeys("ctrl+b", "b"), key.WithHelp("b", "branch")),
	quit:   key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
}

// WorkItems is the work item browser.
type WorkItems struct {
	client *azdo.Client
	list   list.Model
	detail viewport.Model

	all      []item
	mine     []item
	mineOnly bool

	width, height int
	status        string

	// Command is set when the user picks an action that has to run in the
	// parent shell. A child process can neither change the shell's directory
	// nor drive its line editor, so the command is printed on exit instead.
	Command string
}

func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly bool) WorkItems {
	var all, mine []item
	for _, wi := range items {
		it := item{wi}
		all = append(all, it)
		if c.Me != "" && it.AssignedKey == c.Me {
			mine = append(mine, it)
		}
	}

	l := list.New(nil, itemDelegate{width: 80}, 0, 0)
	// The list keeps a title-bar row for the filter prompt even with the title
	// hidden, which would push the rows a line below the detail pane. The
	// filter input is rendered on boardwalk's own status line instead.
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.InfiniteScrolling = false

	m := WorkItems{
		client:   c,
		list:     l,
		detail:   viewport.New(0, 0),
		all:      all,
		mine:     mine,
		mineOnly: mineOnly,
	}
	m.applyScope()
	return m
}

func (m *WorkItems) applyScope() {
	src := m.all
	if m.mineOnly {
		src = m.mine
	}
	items := make([]list.Item, len(src))
	for i, it := range src {
		items[i] = it
	}
	m.list.SetItems(items)
}

func (m WorkItems) Init() tea.Cmd { return nil }

func (m *WorkItems) selected() (item, bool) {
	it, ok := m.list.SelectedItem().(item)
	return it, ok
}

func (m WorkItems) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listWidth := msg.Width * 3 / 5
		detailWidth := msg.Width - listWidth - 4
		body := msg.Height - 3 // header, hints, status

		m.list.SetSize(listWidth, body)
		m.list.SetDelegate(itemDelegate{width: listWidth - 2})
		m.detail.Width = detailWidth
		m.detail.Height = body
		m.renderDetail()
		return m, nil

	case tea.KeyMsg:
		// While the filter prompt is open every key belongs to it, or typing
		// "o" would open a browser instead of entering a letter.
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, keys.toggle):
			m.mineOnly = !m.mineOnly
			m.applyScope()
			m.renderDetail()
			return m, nil

		case key.Matches(msg, keys.open):
			if it, ok := m.selected(); ok {
				openBrowser(m.client.WorkItemURL(it.ID))
				m.status = fmt.Sprintf("opened #%d", it.ID)
			}
			return m, nil

		case key.Matches(msg, keys.copyID):
			if it, ok := m.selected(); ok {
				copyToClipboard(fmt.Sprint(it.ID))
				m.status = fmt.Sprintf("copied id %d", it.ID)
			}
			return m, nil

		case key.Matches(msg, keys.slack):
			if it, ok := m.selected(); ok {
				copyToClipboard(SlackLink(fmt.Sprintf("#%d %s", it.ID, it.Title), m.client.WorkItemURL(it.ID)))
				m.status = fmt.Sprintf("copied Slack link for #%d", it.ID)
			}
			return m, nil

		case key.Matches(msg, keys.branch):
			if it, ok := m.selected(); ok {
				m.Command = fmt.Sprintf("azdo-branch %d", it.ID)
				return m, tea.Quit
			}
			return m, nil

		case key.Matches(msg, keys.quit):
			return m, tea.Quit
		}
	}

	before := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.list.Index() != before {
		m.renderDetail()
	}
	return m, cmd
}

func (m *WorkItems) renderDetail() {
	it, ok := m.selected()
	if !ok {
		m.detail.SetContent("")
		return
	}

	width := max(20, m.detail.Width-2)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(fmt.Sprintf("#%d  %s", it.ID, it.Title), width)))
	for _, row := range [][2]string{
		{"type", it.Type},
		{"state", it.State},
		{"assigned", it.Assigned},
		{"tags", orDash(strings.ReplaceAll(it.Tags, "; ", ", "))},
		{"iteration", orDash(it.Iteration)},
	} {
		label := labelStyle.Render(fmt.Sprintf("%-10s", row[0]+":"))
		fmt.Fprintf(&b, "%s %s\n", label, truncate(row[1], width-11))
	}

	desc := it.Description
	if desc == "" {
		desc = "(no description)"
	}
	fmt.Fprintf(&b, "\n%s\n", wordwrap(desc, width))

	m.detail.SetContent(b.String())
	m.detail.GotoTop()
}

func (m WorkItems) View() string {
	if m.width == 0 {
		return "loading…"
	}

	scope := fmt.Sprintf("all %d", len(m.all))
	if m.mineOnly {
		scope = fmt.Sprintf("mine %d", len(m.mine))
	}
	header := chromeStyle.Render(fmt.Sprintf("work items (%s) · %s/%s", scope, m.client.Org, m.client.Project))
	hints := chromeStyle.Render("^t mine/all · / filter · o open · y copy id · s slack · b branch · q quit")

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.list.View(),
		detailPane.Render(m.detail.View()),
	)

	status := statusStyle.Render(m.status)
	if m.list.FilterState() == list.Filtering {
		status = m.list.FilterInput.View()
	}

	return strings.Join([]string{header, body, hints, status}, "\n")
}

// SlackLink renders a markdown link, which Slack's composer turns into a real
// link on paste. Square brackets in the title would end the link text early, so
// they become parentheses.
func SlackLink(title, url string) string {
	title = strings.NewReplacer("[", "(", "]", ")", "\n", " ", "\r", " ").Replace(title)
	return fmt.Sprintf("[%s](%s)", strings.TrimSpace(title), url)
}

func copyToClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func openBrowser(url string) error {
	return exec.Command("open", url).Start()
}
