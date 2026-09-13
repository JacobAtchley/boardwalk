package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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

// linkedPRsMsg carries the pull requests a work item links to. It names its
// work item because Root broadcasts data to every view in the stack.
type linkedPRsMsg struct {
	ID  int
	PRs []azdo.PullRequest
	Err error
}

// commentSentMsg carries the outcome of posting a new comment. It names its
// work item for the same reason every message in this file does: Root
// broadcasts data to every view in the stack, and the user may have moved on
// to a different item before the round trip returns.
type commentSentMsg struct {
	ID      int
	Comment azdo.Comment
	Err     error
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

	// linked are the pull requests this item is attached to — what the branch
	// flow's link becomes once someone opens a pull request from that branch.
	linked       []azdo.PullRequest
	linkedLoaded bool
	linkedErr    bool

	// renderedAt is the width the pane was last laid out for. Glamour hard
	// wraps, so a resize means rendering again rather than reflowing.
	renderedAt int

	status string
	work   work
	now    func() time.Time

	// statePicker is non-nil while the state picker is open. See the type's
	// own doc in statepicker.go; it holds the same modal discipline the
	// branch prompt in workitems.go and the reply prompt in
	// pullrequestdetail.go use for theirs.
	statePicker *statePicker
	// stateErr is set when the last state-change status was a rejected
	// transition, and read alongside failed in Status. It is its own field
	// rather than reusing failed, which render reads specifically to mean
	// "the discussion did not load" — setting it for an unrelated state
	// error would draw that section as broken when it never was.
	stateErr bool
	// assignErr mirrors stateErr for the same reason, for an assign that
	// failed or was refused before it was even attempted (an unknown Me).
	assignErr bool

	// commentPrompt is non-nil while a new comment is being typed, in the same
	// shape as the reply prompt in pullrequestdetail.go — c means the same
	// thing here: add to the discussion. It and statePicker are never both
	// set: statePicker's own key handling below swallows every key while it
	// is open, including c, so the prompt can only open once it is nil.
	commentPrompt *textinput.Model
	// commentErr mirrors stateErr and assignErr: its own flag rather than
	// reusing failed, which render reads specifically for "the discussion did
	// not load".
	commentErr bool
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

// Init fetches what it does not already have. The two halves are independent:
// the list hands over the discussion it cached but knows nothing about the
// linked pull requests, so returning early on the discussion alone would leave
// the links permanently unfetched.
func (m *Item) Init() tea.Cmd {
	var cmds []tea.Cmd
	if !m.loaded {
		cmds = append(cmds, m.fetchComments())
	}
	if !m.linkedLoaded {
		cmds = append(cmds, m.fetchLinked())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(tea.Batch(cmds...), m.work.begin(len(cmds)))
}

func (m *Item) fetchComments() tea.Cmd {
	client, id := m.client, m.item.ID
	return func() tea.Msg {
		comments, err := client.Comments(id)
		if err != nil {
			return commentsErrMsg{ID: id, Err: fmt.Errorf("could not load the discussion for #%d: %w", id, err)}
		}
		return commentsMsg{ID: id, Comments: comments}
	}
}

// fetchLinked resolves the work item's pull request links. The relations name
// them but carry nothing else, so each one is fetched: the project-wide listing
// only holds active pull requests, and a work item stays linked to its pull
// request long after it merges.
func (m *Item) fetchLinked() tea.Cmd {
	client, id := m.client, m.item.ID
	return func() tea.Msg {
		refs, err := client.WorkItemPullRequests(id)
		if err != nil {
			return linkedPRsMsg{ID: id, Err: fmt.Errorf("could not load the pull requests for #%d: %w", id, err)}
		}

		prs := make([]azdo.PullRequest, 0, len(refs))
		for _, ref := range refs {
			pr, err := client.PullRequestByID(ref.RepoID, ref.ID)
			if err != nil {
				// One unreadable link should not cost the others; a deleted
				// repository is enough to produce it.
				continue
			}
			prs = append(prs, pr)
		}
		// Newest first, so the p key opens the one most likely to be current.
		sort.SliceStable(prs, func(i, j int) bool { return prs[i].Created.After(prs[j].Created) })
		return linkedPRsMsg{ID: id, PRs: prs}
	}
}

// commentCmd posts text as a new comment on the item's discussion.
func (m *Item) commentCmd(text string) tea.Cmd {
	client, id := m.client, m.item.ID
	return func() tea.Msg {
		comment, err := client.AddComment(id, text)
		if err != nil {
			return commentSentMsg{ID: id, Err: fmt.Errorf("could not post the comment: %w", err)}
		}
		return commentSentMsg{ID: id, Comment: comment}
	}
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

	case linkedPRsMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		m.work.done()
		m.linkedLoaded = true
		if msg.Err != nil {
			m.linkedErr, m.status = true, msg.Err.Error()
		} else {
			m.linked = msg.PRs
		}
		m.invalidate()
		return m, nil

	case statesFetchedMsg:
		// Named by id, the same broadcast discipline every message in this
		// file already follows: the picker this landed for may have already
		// been cancelled, or this pane may since have been sent back to the
		// list and reopened on a different item.
		if m.statePicker == nil || m.statePicker.itemID != msg.ID {
			return m, nil
		}
		m.statePicker.resolve(msg)
		return m, nil

	case stateSetMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.stateErr = msg.Err.Error(), true
			return m, nil
		}
		m.item.State = msg.State
		m.status, m.stateErr = fmt.Sprintf("#%d is now %s", msg.ID, msg.State), false
		m.invalidate()
		return m, nil

	case assigneeSetMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.assignErr = msg.Err.Error(), true
			return m, nil
		}
		m.item.Assigned, m.item.AssignedKey = msg.Assigned, msg.AssignedKey
		m.status, m.assignErr = fmt.Sprintf("#%d is now assigned to you", msg.ID), false
		m.invalidate()
		return m, nil

	case commentSentMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.commentErr = msg.Err.Error(), true
			return m, nil
		}
		// Appended in place rather than refetched, the same trade-off
		// replySentMsg makes in pullrequestdetail.go: the discussion is a
		// scrolling viewport (see Body), and a refetch's fresh SetContent
		// would throw away the reader's place in it for a result — this one
		// comment — that a second round trip cannot say anything new about.
		m.comments = append(m.comments, msg.Comment)
		m.status, m.commentErr = "comment posted", false
		m.invalidate()
		return m, nil

	case StatusMsg:
		m.status = msg.Text
		return m, nil

	case tea.KeyMsg:
		// The comment prompt owns every key while it is open, including esc
		// and the letters that are otherwise actions — typing "S" into it
		// must add the letter, not open the state picker. Mirrors the reply
		// prompt in pullrequestdetail.go.
		if m.commentPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.commentPrompt = nil
				m.status, m.commentErr = "", false
				return m, nil
			case tea.KeyEnter:
				text := strings.TrimSpace(m.commentPrompt.Value())
				m.commentPrompt = nil
				if text == "" {
					return m, nil
				}
				m.status, m.commentErr = "posting comment…", false
				return m, m.commentCmd(text)
			}
			input, cmd := m.commentPrompt.Update(msg)
			m.commentPrompt = &input
			return m, cmd
		}

		// The state picker owns every key while it is open, the same
		// discipline the branch prompt (workitems.go) and the reply prompt
		// (pullrequestdetail.go) use for theirs.
		if m.statePicker != nil {
			switch msg.String() {
			case "esc":
				m.statePicker = nil
				m.status, m.stateErr = "", false
			case "enter":
				state, ok := m.statePicker.selected()
				id := m.statePicker.itemID
				m.statePicker = nil
				if !ok {
					return m, nil
				}
				m.status, m.stateErr = fmt.Sprintf("setting #%d %s…", id, state), false
				return m, stateCmd(m.client, id, state)
			case "up", "k":
				m.statePicker.up()
			case "down", "j":
				m.statePicker.down()
			}
			return m, nil
		}

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
			m.linkedLoaded, m.linkedErr = false, false
			return m, m.Init()
		case key.Matches(msg, keyState):
			m.statePicker = newStatePicker(m.item.ID)
			return m, statesCmd(m.client, m.item.ID, m.item.Type)
		case key.Matches(msg, keyComment):
			input := textinput.New()
			input.Prompt = "comment: "
			input.Focus()
			m.commentPrompt = &input
			return m, textinput.Blink
		case key.Matches(msg, keyAssign):
			// An empty Me means az account show failed at startup — sending it
			// as the assignee would unassign the item instead of claiming it,
			// which is the opposite of what the key means and destructive.
			if m.client.Me == "" {
				m.status, m.assignErr = "cannot assign: the signed-in user is unknown", true
				return m, nil
			}
			m.status, m.assignErr = fmt.Sprintf("assigning #%d to you…", m.item.ID), false
			return m, assignCmd(m.client, m.item.ID)
		case key.Matches(msg, keyLinkedPR):
			// The newest link: a work item that has been through more than one
			// pull request is almost always asking about its latest.
			if len(m.linked) == 0 {
				m.status = "no pull requests are linked to this work item"
				return m, nil
			}
			detail := NewPullRequestDetail(m.client, m.linked[0], nil)
			return m, func() tea.Msg { return PushMsg{View: detail} }
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

	m.renderLinked(&b, width)

	fmt.Fprintf(&b, "\n%s%s\n", labelStyle.Render("discussion"), chromeStyle.Render("  c comments"))
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

// renderLinked writes the pull requests this item is attached to. It sits above
// the discussion because it answers the question people open a work item to
// ask: is anyone working on this, and did it ship.
func (m *Item) renderLinked(b *strings.Builder, width int) {
	fmt.Fprintf(b, "\n%s\n", labelStyle.Render("pull requests"))

	switch {
	case m.linkedErr:
		fmt.Fprintf(b, "%s\n", errStyle.Render("could not load the links — press r to try again"))
	case !m.linkedLoaded:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render(m.work.View()+"loading…"))
	case len(m.linked) == 0:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("(none linked)"))
	default:
		for i, pr := range m.linked {
			marker := " "
			if i == 0 {
				marker = "▸" // what p opens
			}
			fmt.Fprintf(b, "%s %s\n", marker, truncate(fmt.Sprintf("!%d %s — %s → %s",
				pr.ID, pr.Title, prStatusLabel(pr), shortRef(pr.Target)), width-2))
		}
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("  p opens the first"))
	}
}

func (m *Item) Title() string {
	return fmt.Sprintf("#%d %s · %s/%s",
		m.item.ID, m.item.Title, m.client.Org, m.client.Project)
}

// Keys omits the filter: there is nothing here to filter.
//
// The short line carries only comment, set state and assign; opening the
// linked pull request, jumping to either end of the pager, refreshing and
// copying the id are still one keystroke away, but behind "?" rather than on
// the line itself. See the matching comment on PullRequestDetail.Keys: the
// same eight-tasks-by-hand growth pushed esc and ? off the short line here
// too, and "linked pull request" is long enough on its own that even the
// four view-specific bindings alone still overflowed 80 columns.
func (m *Item) Keys() help.KeyMap {
	short := []key.Binding{keyComment, keyState, keyAssign}
	full := []key.Binding{keyComment, keyState, keyAssign, keyLinkedPR, keyTop, keyBottom, keyRefresh}
	return keyMap{
		short:  append(append([]key.Binding{}, short...), keyBack, keyHelp),
		groups: [][]key.Binding{full, {keyCopyID, keySlack, keyOpen}, navBindings()},
	}
}

// Status shows the comment prompt in place of the transient line while it is
// open, the same swap the reply prompt makes in pullrequestdetail.go. The
// state picker takes precedence over the transient line too, and the two can
// never both be open — see commentPrompt's own doc — so checking one after
// the other is unambiguous.
func (m *Item) Status() (string, bool) {
	if m.commentPrompt != nil {
		return m.commentPrompt.View(), false
	}
	if m.statePicker != nil {
		return m.statePicker.View(), false
	}
	return m.work.View() + m.status, m.failed || m.stateErr || m.assignErr || m.commentErr
}

// Prompting reports whether the state picker or the comment prompt is open,
// so Root leaves esc and q to this view rather than treating them as
// navigation — the same reason PullRequestDetail and WorkItems implement it
// for their own modal state.
func (m *Item) Prompting() bool {
	return m.statePicker != nil || m.commentPrompt != nil
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
