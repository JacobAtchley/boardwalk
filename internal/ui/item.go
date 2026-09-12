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

// commentsMsg carries a fetched discussion. It names the work item it belongs
// to because Root broadcasts data to every view in the stack, and a slow fetch
// can land after the user has moved on.
type commentsMsg struct {
	ID       int
	Comments []azdo.Comment
}

// commentsErrMsg is a discussion fetch that failed. It names its work item for
// the same reason, and so the pane can say so honestly rather than reading
// "loading…" forever.
type commentsErrMsg struct {
	ID  int
	Err error
}

// Item is one work item in full: every field, and the whole discussion.
//
// The list's side pane is a summary — enough to recognise a row while moving
// through it — and this is what enter opens. Splitting them is what lets the
// summary stay glanceable while the long text gets the room it needs.
type Item struct {
	client *azdo.Client
	item   azdo.WorkItem

	viewport viewport.Model
	comments []azdo.Comment

	// loaded separates "no comments" from "not fetched yet", which read the
	// same in an empty slice.
	loaded bool
	failed bool

	// renderedAt is the width the pane was last laid out for. Glamour hard
	// wraps, so a resize means rendering again rather than reflowing.
	renderedAt int

	status string
	work   work
	now    func() time.Time
}

// NewItem builds the view. comments is whatever the list already had cached,
// which is usually nothing; Init fetches when it is empty.
func NewItem(c *azdo.Client, wi azdo.WorkItem, comments []azdo.Comment) *Item {
	return &Item{
		client:   c,
		item:     wi,
		viewport: viewport.New(0, 0),
		comments: comments,
		loaded:   comments != nil,
		work:     newWork(),
		now:      time.Now,
	}
}

// Init fetches the discussion unless one was handed over.
func (m *Item) Init() tea.Cmd {
	if m.loaded {
		return nil
	}

	client, id := m.client, m.item.ID
	fetch := func() tea.Msg {
		comments, err := client.Comments(id)
		if err != nil {
			return commentsErrMsg{ID: id, Err: fmt.Errorf("could not load the discussion for #%d: %w", id, err)}
		}
		return commentsMsg{ID: id, Comments: comments}
	}
	return tea.Batch(fetch, m.work.begin(1))
}

func (m *Item) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case commentsMsg:
		// Named by id: Root broadcasts data to every view in the stack, and the
		// list this was opened from is still underneath asking for its own.
		if msg.ID != m.item.ID {
			return m, nil
		}
		m.work.done()
		m.comments, m.loaded = msg.Comments, true
		m.invalidate()
		return m, nil

	case commentsErrMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		m.work.done()
		m.failed, m.loaded = true, true
		m.status = msg.Err.Error()
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

		if status, handled := SharedAction(itemRow{m.item, m.client.WorkItemURL(m.item.ID)}, msg); handled {
			m.status = status.Text
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// invalidate forces the next frame to lay the content out again.
func (m *Item) invalidate() { m.renderedAt = 0 }

func (m *Item) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height

	if m.renderedAt != width {
		// Hold the reader's place across a relayout, but open at the top. The
		// log pane follows its tail because it is tailing; this is not, and an
		// empty viewport reports itself at the bottom, so following would land
		// the first frame at the end of the discussion instead of the title.
		offset := m.viewport.YOffset
		m.viewport.SetContent(m.render(width))
		m.renderedAt = width
		m.viewport.SetYOffset(offset)
	}
	return m.viewport.View()
}

// render lays the whole item out. The field list is rendered plainly — five
// short values gain nothing from markdown — while the long text goes through
// glamour, which is the reason it was converted from HTML in the first place.
func (m *Item) render(width int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(
		fmt.Sprintf("#%d  %s", m.item.ID, m.item.Title), width)))

	for _, field := range [][2]string{
		{"type", m.item.Type},
		{"state", m.item.State},
		{"iteration", orDash(m.item.Iteration)},
		{"tags", orDash(strings.ReplaceAll(m.item.Tags, "; ", ", "))},
		{"assigned", m.item.Assigned},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-10s", field[0]+":")),
			truncate(field[1], width-11))
	}

	fmt.Fprintf(&b, "\n%s\n%s\n", labelStyle.Render("description"),
		renderMarkdown(m.item.Description, "_no description_", width))
	fmt.Fprintf(&b, "\n%s\n%s\n", labelStyle.Render("acceptance criteria"),
		renderMarkdown(m.item.AcceptanceCriteria, "_none_", width))

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("discussion"))
	switch {
	case m.failed:
		fmt.Fprintf(&b, "%s\n", errStyle.Render("could not load the discussion — press r to try again"))
	case !m.loaded:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render(m.work.View()+"loading…"))
	case len(m.comments) == 0:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(no comments)"))
	default:
		for _, c := range m.comments {
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(fmt.Sprintf("%s · %s", c.Author, humanAge(c.Created, m.now()))),
				renderMarkdown(c.Text, "_(empty)_", width))
		}
	}
	return b.String()
}

func (m *Item) Title() string {
	return fmt.Sprintf("#%d %s · %s/%s",
		m.item.ID, m.item.Title, m.client.Org, m.client.Project)
}

// Keys omits the filter: there is nothing here to filter.
func (m *Item) Keys() help.KeyMap {
	own := []key.Binding{keyTop, keyBottom, keyRefresh}
	return keyMap{
		short:  append(append([]key.Binding{}, own...), keyCopyID, keyBack, keyHelp),
		groups: [][]key.Binding{own, {keyCopyID, keySlack, keyOpen}, navBindings()},
	}
}

func (m *Item) Status() (string, bool) {
	return m.work.View() + m.status, m.failed
}

// itemRow adapts the work item to the shared copy and open actions, which take
// a Row so that every view names and links things the same way.
type itemRow struct {
	azdo.WorkItem
	url string
}

func (r itemRow) FilterValue() string     { return "" }
func (r itemRow) Render(width int) string { return "" }
func (r itemRow) CopyID() string          { return fmt.Sprint(r.ID) }
func (r itemRow) Label() string           { return fmt.Sprintf("#%d %s", r.ID, r.Title) }
func (r itemRow) URL() string             { return r.url }
