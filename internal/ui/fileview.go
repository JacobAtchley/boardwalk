package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// FileView is one changed file, whole, with the pull request's changes marked
// in place.
//
// The diff pane beside the file list shows hunks — the changed lines and
// three either side — which is the right amount of code to skim a change by
// and the wrong amount to judge one by. The reason a change is wrong is
// usually in the part of the file the hunk does not reach: the caller two
// hundred lines up, the early return it now skips. This is that file.
//
// It is syntax highlighted rather than coloured by change, and the change
// goes in the gutter. Three lines of context are not worth reading as code;
// a whole file is.
type FileView struct {
	client *azdo.Client
	pr     azdo.PullRequest
	path   string

	// lines is the composed file — every line of it, with the removed ones
	// spliced back in where they were. It does not change; showDiff decides
	// which of them are rendered.
	lines []fileLine
	// highlighted is the new side's lines after chroma, indexed by new-side
	// line number minus one. Removed lines are not in it: they are old
	// content, dimmed rather than coloured, so the eye skips them.
	highlighted []string

	threads []azdo.Thread

	// filter is which half of the discussion is drawn beside the code. It
	// arrives from the diff pane rather than starting fresh, for the reason
	// that field's own comment gives.
	filter threadFilter

	// showDiff is whether the change is marked at all. With it off this is
	// simply the file, which is what reading the code rather than the change
	// wants.
	showDiff bool

	// cursor indexes lines, and is what a review comment will anchor to. It
	// is a line cursor rather than a scroll position because "the line I am
	// looking at" is a thing the next feature has to name exactly.
	cursor   int
	viewport viewport.Model

	// renderedAt and renderedDiff are what the pane was last laid out for,
	// so it is rebuilt when the width or the toggle changes and not on every
	// keystroke — a thousand-line file is re-rendered from scratch each
	// time, and the cursor moves a line at a time.
	renderedAt   int
	renderedDiff bool
	rendered     bool

	// commentPrompt is non-nil while a review comment is being typed. It is
	// the view's only modal state, and it owns every key while it is open —
	// "d" typed into it is a letter, not the diff toggle.
	commentPrompt *textinput.Model
	// commentLine is the line the open prompt will post against, captured
	// when it opened rather than read back from the cursor afterwards: the
	// cursor cannot be asked again once the post's round trip returns.
	commentLine int

	status string
	failed bool
}

// threadCreatedMsg carries the outcome of writing a review comment. It names
// the pull request and the file because Root broadcasts to every view in the
// stack, and two file views can be open at once by way of a linked pull
// request.
type threadCreatedMsg struct {
	PR     int
	Path   string
	Thread azdo.Thread
	Err    error
}

// createThreadCmd posts a review comment off the UI goroutine.
func createThreadCmd(c *azdo.Client, pr azdo.PullRequest, path string, line int, text string) tea.Cmd {
	return func() tea.Msg {
		thread, err := c.CreateThread(pr.RepoID, pr.ID, path, line, text)
		if err != nil {
			return threadCreatedMsg{PR: pr.ID, Path: path,
				Err: fmt.Errorf("could not post the comment: %w", err)}
		}
		return threadCreatedMsg{PR: pr.ID, Path: path, Thread: thread}
	}
}

// NewFileView builds the view over content the diff pane has already
// fetched. It makes no request of its own: buildFileDiff reads both sides of
// the file to diff them, so by the time a row can be opened the text is
// already in hand.
func NewFileView(c *azdo.Client, pr azdo.PullRequest, path, text string,
	hunks []*udiff.Hunk, threads []azdo.Thread, filter threadFilter) *FileView {
	return &FileView{
		client:      c,
		pr:          pr,
		path:        path,
		lines:       composeFile(text, hunks),
		highlighted: highlightLines(text, path),
		threads:     azdo.ThreadsForFile(threads, path),
		filter:      filter,
		showDiff:    true,
		viewport:    viewport.New(0, 0),
	}
}

func (m *FileView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case threadCreatedMsg:
		if msg.PR != m.pr.ID || msg.Path != m.path {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		// Added to what is already held rather than refetched: the thread
		// the server just made is known, and waiting for a round trip to see
		// your own comment reads as the comment having failed.
		m.threads = append(m.threads, msg.Thread)
		m.status, m.failed = "comment posted", false
		m.invalidate()
		return m, nil

	case tea.KeyMsg:
		// The prompt owns every key while it is open, including the letters
		// that are otherwise actions. Mirrors the branch prompt in
		// workitems.go and the reply prompt in pullrequestdetail.go.
		if m.commentPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.commentPrompt = nil
				m.status, m.failed = "", false
				return m, nil
			case tea.KeyEnter:
				text := strings.TrimSpace(m.commentPrompt.Value())
				line := m.commentLine
				m.commentPrompt = nil
				if text == "" {
					// An empty prompt closed with enter is a mistake, not a
					// request to post nothing.
					return m, nil
				}
				m.status, m.failed = fmt.Sprintf("posting the comment on line %d…", line), false
				return m, createThreadCmd(m.client, m.pr, m.path, line, text)
			}
			input, cmd := m.commentPrompt.Update(msg)
			m.commentPrompt = &input
			return m, cmd
		}

		switch {
		case key.Matches(msg, keyBack), key.Matches(msg, keyQuit):
			return m, func() tea.Msg { return PopMsg{} }
		case key.Matches(msg, keyFileDiff):
			m.showDiff = !m.showDiff
			// The cursor indexes the composed file, which does not change —
			// only which of its lines are drawn. Landing on a hidden row is
			// handled by the render, which skips it.
			m.invalidate()
			return m, nil
		case key.Matches(msg, keyThreadFilter):
			// The cursor indexes the composed file, which the filter does not
			// touch — only the comment rows drawn between its lines — so it
			// stays on the line it was naming.
			m.filter = m.filter.next()
			m.status, m.failed = "comments: "+m.filter.label(), false
			m.invalidate()
			return m, nil
		case key.Matches(msg, keyLineDown):
			m.moveCursor(1)
			return m, nil
		case key.Matches(msg, keyLineUp):
			m.moveCursor(-1)
			return m, nil
		case key.Matches(msg, keyComment):
			line, ok := m.anchorLine()
			if !ok {
				// Knowable without asking: a removed line is not in the new
				// file, so there is nothing for the server to anchor to.
				// Refused here rather than at the network call, which would
				// hide the reason behind an error code — the precedent
				// armDraft sets.
				m.status, m.failed = "that line was removed by this pull request, so a comment has nothing to attach to", true
				return m, nil
			}
			input := textinput.New()
			input.Prompt = fmt.Sprintf("comment on line %d: ", line)
			input.Focus()
			m.commentPrompt, m.commentLine = &input, line
			m.status, m.failed = "", false
			return m, textinput.Blink

		case key.Matches(msg, keyTop):
			m.cursor = 0
			m.invalidate()
			return m, nil
		case key.Matches(msg, keyBottom):
			m.cursor = max(0, len(m.lines)-1)
			m.invalidate()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// moveCursor steps the cursor and stops at either end.
//
// It does not wrap, which the landing menu and the pickers do. Those are
// short lists of alternatives where wrapping saves a keystroke; this is a
// file, and jumping from the last line to the first because a key repeated
// once too often is disorienting rather than convenient.
func (m *FileView) moveCursor(by int) {
	if len(m.lines) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+by, 0), len(m.lines)-1)
	m.invalidate()
}

// anchorLine is the new-file line a review comment written here would be
// anchored to, and whether there is one at all. A removed line has none: it
// is not in the file any more, so there is nothing for the server to hang a
// comment on.
func (m *FileView) anchorLine() (int, bool) {
	if m.cursor < 0 || m.cursor >= len(m.lines) {
		return 0, false
	}
	row := m.lines[m.cursor]
	return row.New, row.New > 0
}

func (m *FileView) invalidate() { m.rendered = false }

func (m *FileView) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height

	if !m.rendered || m.renderedAt != width || m.renderedDiff != m.showDiff {
		m.viewport.SetContent(m.render(width))
		m.renderedAt, m.renderedDiff, m.rendered = width, m.showDiff, true
	}
	m.follow()
	return m.viewport.View()
}

// follow keeps the cursor on screen without moving the pane any further than
// it has to, so paging down a file reads as a page rather than as a jump.
func (m *FileView) follow() {
	row := m.screenRow(m.cursor)
	switch {
	case row < m.viewport.YOffset:
		m.viewport.SetYOffset(row)
	case row >= m.viewport.YOffset+m.viewport.Height:
		m.viewport.SetYOffset(row - m.viewport.Height + 1)
	}
}

// screenRow is which rendered row a line ended up on, which is not its index:
// hidden removals and the review comments written under a line both move
// everything below them down.
func (m *FileView) screenRow(index int) int {
	row := 0
	for i, l := range m.lines {
		if i == index {
			return row
		}
		if !m.visible(l) {
			continue
		}
		row++
		row += m.commentRows(l)
	}
	return row
}

// visible reports whether a composed line is drawn at all. With the diff off
// the removed lines are not: they are not in the file, and the point of
// turning it off is to read the file.
func (m *FileView) visible(l fileLine) bool {
	return m.showDiff || l.Kind != udiff.Delete
}

// commentRows is how many rows the threads under a line take.
func (m *FileView) commentRows(l fileLine) int {
	n := 0
	for _, t := range m.threadsAt(l) {
		n += 1 + len(t.Comments)
	}
	return n
}

// threadsAt is the discussion written against a line.
func (m *FileView) threadsAt(l fileLine) []azdo.Thread {
	if l.New == 0 {
		return nil
	}
	var out []azdo.Thread
	for _, t := range m.threads {
		if t.RightSide && t.Line == l.New && m.filter.keep(t) {
			out = append(out, t)
		}
	}
	return out
}

func (m *FileView) render(width int) string {
	var b strings.Builder

	// The gutter is a mark, both line numbers' width, and two spaces. Sized
	// from the file rather than fixed, so a short file does not carry a
	// column of blanks for line numbers it will never reach.
	digits := len(fmt.Sprint(max(1, len(m.lines))))

	for _, l := range m.lines {
		if !m.visible(l) {
			continue
		}
		m.writeLine(&b, l, digits, width)
		for _, t := range m.threadsAt(l) {
			writeThread(&b, t, width)
		}
	}
	return b.String()
}

func (m *FileView) writeLine(b *strings.Builder, l fileLine, digits, width int) {
	mark, markStyle := " ", normalRow
	number := l.New
	if m.showDiff {
		switch l.Kind {
		case udiff.Insert:
			mark, markStyle = "+", addedLine
		case udiff.Delete:
			mark, markStyle, number = "-", removedLine, l.Old
		}
	}

	gutter := markStyle.Render(mark) + " " +
		chromeStyle.Render(fmt.Sprintf("%*d", digits, number))

	// A removed line is old content and is dimmed rather than highlighted;
	// the highlighted text is indexed by the new file's numbering, which a
	// removal has no place in.
	text := l.Text
	if l.Kind == udiff.Delete {
		text = chromeStyle.Render(expandTabs(l.Text))
	} else if i := l.New - 1; i >= 0 && i < len(m.highlighted) {
		text = m.highlighted[i]
	}

	room := max(10, width-digits-3)
	fmt.Fprintf(b, "%s %s\n", gutter, truncate(expandTabs(text), room))
}

func (m *FileView) Title() string {
	return fmt.Sprintf("%s · !%d · %s", m.path, m.pr.ID, m.pr.Repo)
}

func (m *FileView) Keys() help.KeyMap {
	own := []key.Binding{keyComment, keyFileDiff, keyThreadFilter, keyLineUp, keyLineDown, keyTop, keyBottom}
	return keyMap{
		short:  []key.Binding{keyComment, keyFileDiff, keyBack, keyHelp},
		groups: [][]key.Binding{own, {keyCopyID, keyOpen}, navBindings()},
	}
}

// Status names the line the cursor is on, which is the one a review comment
// would be written against. It is the only thing on screen that says so.
func (m *FileView) Status() (string, bool) {
	if m.commentPrompt != nil {
		return m.commentPrompt.View(), false
	}
	if m.status != "" {
		return m.status, m.failed
	}
	if n, ok := m.anchorLine(); ok {
		return chromeStyle.Render(fmt.Sprintf("line %d", n)), false
	}
	return chromeStyle.Render("removed line"), false
}

// Prompting reports whether the comment prompt is open, so Root leaves esc
// to it rather than treating it as navigation.
func (m *FileView) Prompting() bool { return m.commentPrompt != nil }
