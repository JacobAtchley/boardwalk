package ui

import (
	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// listShare is the fraction of the width the list takes, leaving the rest for
// the detail pane.
const listShare = 3.0 / 5.0

// Row is one line in a browser. Every view's row type implements it, which is
// what lets the copy and open actions live in one place.
type Row interface {
	// FilterValue is what the fuzzy matcher sees.
	FilterValue() string

	// Render draws the row at the given width, without the selection marker.
	Render(width int) string

	// CopyID is what the copy-id action puts on the clipboard.
	CopyID() string

	// Label is the human name, leading with an identifier: "#4021 title".
	Label() string

	// URL is where the open action goes.
	URL() string
}

// Browser is the list-and-detail scaffold all three views are built on. A view
// supplies rows and a Detail renderer; everything else is here.
type Browser struct {
	// Detail renders the pane on the right for the selected row.
	Detail func(Row, int) string

	list   list.Model
	detail viewport.Model

	width, height int
}

// NewBrowser builds an empty browser with the list configured the way every
// view wants it — no title bar, no built-in status bar or help, so boardwalk's
// own chrome is the only chrome on screen.
func NewBrowser() Browser {
	l := list.New(nil, rowDelegate{width: 80}, 0, 0)
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

	return Browser{
		Detail: func(Row, int) string { return "" },
		list:   l,
		detail: viewport.New(0, 0),
	}
}

// SetRows replaces the rows the list shows, and re-renders the detail pane for
// the new selection.
func (b *Browser) SetRows(rows []Row) {
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = rowItem{r}
	}
	b.list.SetItems(items)
	b.renderDetail()
}

// SetSize splits width between the list and the detail pane, and re-renders
// the detail pane to the new width.
func (b *Browser) SetSize(width, height int) {
	b.width, b.height = width, height

	listWidth := int(float64(width) * listShare)
	detailWidth := width - listWidth - 4

	b.list.SetSize(listWidth, height)
	b.list.SetDelegate(rowDelegate{width: listWidth - 2})
	b.detail.Width = max(10, detailWidth)
	b.detail.Height = height
	b.renderDetail()
}

// Selected reports the row under the cursor, or false when the list is empty.
func (b *Browser) Selected() (Row, bool) {
	it, ok := b.list.SelectedItem().(rowItem)
	if !ok {
		return nil, false
	}
	return it.Row, true
}

// Update forwards a message to the list and re-renders the detail pane when
// the selected row has changed.
//
// ctrl+u and ctrl+d are claimed for the detail pane's own scrolling before
// anything reaches the list, so a description, acceptance criteria and
// discussion that together overflow the pane stay reachable. The list's own
// paging keys are pgup/b and pgdown/f, so these two are free — see
// bubbles/list's DefaultKeyMap.
//
// Comparing the row rather than the cursor position is deliberate: filtering
// replaces the matched set asynchronously, via a list.FilterMatchesMsg that
// arrives on a later tick, without moving the cursor. The row sitting at the
// cursor's index can therefore change while the index itself does not, and an
// index comparison alone would miss it, leaving the pane on stale data as the
// query narrows.
func (b *Browser) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+u":
			b.detail.HalfPageUp()
			return nil
		case "ctrl+d":
			b.detail.HalfPageDown()
			return nil
		}
	}

	before, hadBefore := b.Selected()

	var cmd tea.Cmd
	b.list, cmd = b.list.Update(msg)

	after, hasAfter := b.Selected()
	if hadBefore != hasAfter || (hasAfter && rowChanged(before, after)) {
		b.renderDetail()
	}
	return cmd
}

// rowChanged reports whether two rows differ by identity rather than value —
// CopyID is the one thing every Row promises is unique within a view, which
// makes it the natural key for "is this still the same row".
func rowChanged(a, b Row) bool {
	return a.CopyID() != b.CopyID()
}

// RefreshDetail re-renders the pane in place, for a view whose detail data
// arrived after the cursor landed.
func (b *Browser) RefreshDetail() { b.renderDetail() }

func (b *Browser) renderDetail() {
	row, ok := b.Selected()
	if !ok {
		b.detail.SetContent("")
		return
	}
	b.detail.SetContent(b.Detail(row, max(20, b.detail.Width-2)))
	b.detail.GotoTop()
}

// View renders the list beside the detail pane.
func (b Browser) View() string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		b.list.View(),
		detailPane.Render(b.detail.View()),
	)
}

// Filtering reports whether the fuzzy filter prompt is open, so a view knows to
// let keys fall through to it instead of its own bindings.
func (b Browser) Filtering() bool { return b.list.FilterState() == list.Filtering }

// FilterView renders the filter input, for a view's status line while
// Filtering is true.
func (b Browser) FilterView() string { return b.list.FilterInput.View() }

// Len is the number of rows currently in the list, filtered or not.
func (b Browser) Len() int { return len(b.list.Items()) }

// rowItem adapts a Row to the list widget, which wants its own interface.
type rowItem struct{ Row }

type rowDelegate struct{ width int }

func (d rowDelegate) Height() int                         { return 1 }
func (d rowDelegate) Spacing() int                        { return 0 }
func (d rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(rowItem)
	if !ok {
		return
	}

	row := it.Render(d.width)
	if index == m.Index() {
		io.WriteString(w, selectedRow.Render("▸ "+row))
		return
	}
	io.WriteString(w, normalRow.Render("  "+row))
}
