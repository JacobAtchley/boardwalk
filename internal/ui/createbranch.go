package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// cbField is the part of the create branch screen that has the keyboard.
type cbField int

const (
	fieldRepo cbField = iota
	fieldName
	fieldSource
	fieldCount
)

var (
	keyNextField = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
	keyPrevField = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "previous field"))
	keyChoose    = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose"))
	keyCreate    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create"))
)

// CreateBranch is the screen that creates a branch for a work item: which
// repository, which branch to create it from, and what to call it. It used to
// be a name prompt on the work item list's status line that always branched
// off the repository's default branch, which left a branch for a work item on
// a feature or hotfix line to be made somewhere else.
//
// The screen only gathers the answers. Enter hands the flow to the view that
// opened it — the work item list or an item's own pane — and goes back to it,
// so that view reports the outcome.
type CreateBranch struct {
	client *azdo.Client
	item   azdo.WorkItem
	focus  cbField

	repos       []azdo.Repo
	reposLoaded bool
	repoCursor  int

	// branches are the selected repository's branches, and branchesFor the
	// repository they belong to — empty while a listing is out, so a stale
	// answer can be told apart from the one being waited on.
	branches    []azdo.Ref
	branchesFor string
	// targets are the branches' short names, which is what the filter
	// matches and what is shown.
	targets []string
	filter  textinput.Model
	shown   []list.Rank
	cursor  int

	name textinput.Model

	status string
	failed bool

	// owner is the view that opened the screen, which reports the outcome;
	// see branchDoneMsg.
	owner View

	// currentRepo is azdo.CurrentRepo, a field so a test can say where it is
	// standing; create is branchCmd, a field so a test can see what enter
	// asked for without a server.
	currentRepo func() string
	create      func(id int, branch string, repo azdo.Repo, from azdo.Ref) tea.Cmd
}

// NewCreateBranch builds the screen for a work item. currentRepo answers
// which repository the working directory is in, to preselect it, and owner is
// the view opening it, which the outcome is reported to.
func NewCreateBranch(c *azdo.Client, it azdo.WorkItem, currentRepo func() string, owner View) *CreateBranch {
	filter := textinput.New()
	filter.Prompt = "> "
	filter.Placeholder = "type to filter"
	// Static cursors: blink messages would be broadcast to every view on the
	// stack twice a second.
	filter.Cursor.SetMode(cursor.CursorStatic)

	name := textinput.New()
	name.Prompt = ""
	name.Cursor.SetMode(cursor.CursorStatic)
	name.SetValue(branchName(it.Type, it.ID, it.Title))
	name.CursorEnd()

	m := &CreateBranch{
		client: c, item: it, filter: filter, name: name,
		owner:       owner,
		currentRepo: currentRepo,
		create: func(id int, branch string, repo azdo.Repo, from azdo.Ref) tea.Cmd {
			return branchCmd(c, owner, id, branch, repo, from)
		},
		status: "finding the repositories…",
	}
	return m
}

// Init fetches the project's repositories.
func (m *CreateBranch) Init() tea.Cmd {
	return reposCmd(m.client, m.item.ID)
}

func (m *CreateBranch) Title() string {
	return fmt.Sprintf("new branch · #%d %s", m.item.ID, m.item.Title)
}

func (m *CreateBranch) Keys() help.KeyMap {
	return keyMap{
		short:  []key.Binding{keyNextField, keyCreate, keyBack, keyHelp},
		groups: [][]key.Binding{{keyNextField, keyPrevField, keyChoose, keyCreate, keyBack}},
	}
}

func (m *CreateBranch) Status() (string, bool) { return m.status, m.failed }

// Prompting is true while a text field has the keyboard, so every letter —
// q and ? included — goes into it rather than to Root. The repository field
// takes no text, so there Root keeps esc, q, ? and the palette as everywhere
// else.
func (m *CreateBranch) Prompting() bool { return m.focus != fieldRepo }

// repo is the selected repository.
func (m *CreateBranch) repo() (azdo.Repo, bool) {
	if m.repoCursor < 0 || m.repoCursor >= len(m.repos) {
		return azdo.Repo{}, false
	}
	return m.repos[m.repoCursor], true
}

// source is the branch under the source list's cursor, if the list is in and
// anything matches.
func (m *CreateBranch) source() (azdo.Ref, bool) {
	if m.branchesFor == "" || m.cursor < 0 || m.cursor >= len(m.shown) {
		return azdo.Ref{}, false
	}
	return m.branches[m.shown[m.cursor].Index], true
}

func (m *CreateBranch) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case reposFetchedMsg:
		if msg.ID != m.item.ID {
			return m, nil
		}
		return m, m.reposArrived(msg)

	case branchesFetchedMsg:
		repo, ok := m.repo()
		if msg.ID != m.item.ID || !ok || msg.RepoID != repo.ID {
			return m, nil
		}
		m.branchesArrived(msg, repo)
		return m, nil

	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *CreateBranch) reposArrived(msg reposFetchedMsg) tea.Cmd {
	m.reposLoaded = true
	if msg.Err != nil {
		m.status, m.failed = fmt.Sprintf("could not list the project's repositories: %v", msg.Err), true
		return nil
	}
	if len(msg.Repos) == 0 {
		m.status, m.failed = fmt.Sprintf("%s has no Git repositories to branch in", m.client.Project), true
		return nil
	}
	m.repos = msg.Repos
	m.repoCursor = 0
	if want, ok := pickRepo(m.repos, m.currentRepo()); ok {
		for i, r := range m.repos {
			if r.ID == want.ID {
				m.repoCursor = i
			}
		}
	}
	return m.loadBranches()
}

// loadBranches drops the listing on screen and fetches the selected
// repository's.
func (m *CreateBranch) loadBranches() tea.Cmd {
	repo, ok := m.repo()
	if !ok {
		return nil
	}
	m.branches, m.targets, m.shown, m.branchesFor = nil, nil, nil, ""
	m.status, m.failed = fmt.Sprintf("loading %s's branches…", repo.Name), false
	return branchesCmd(m.client, m.item.ID, repo.ID)
}

func (m *CreateBranch) branchesArrived(msg branchesFetchedMsg, repo azdo.Repo) {
	if msg.Err != nil {
		m.status, m.failed = fmt.Sprintf("could not list %s's branches: %v", repo.Name, msg.Err), true
		return
	}
	m.branches, m.branchesFor = msg.Refs, repo.ID
	m.targets = make([]string, len(msg.Refs))
	for i, r := range msg.Refs {
		m.targets[i] = shortRef(r.Name)
	}
	m.filter.SetValue("")
	m.refilter()
	m.status, m.failed = "", false
	if len(m.branches) == 0 {
		m.status, m.failed = fmt.Sprintf("%s has no branches to create from", repo.Name), true
	}
}

// refilter recomputes the shown branches from the query. With no query the
// cursor rests on the repository's default branch, which is what most
// branches are made from; with one it rests on the best match.
func (m *CreateBranch) refilter() {
	q := m.filter.Value()
	m.cursor = 0
	if q != "" {
		m.shown = list.DefaultFilter(q, m.targets)
		return
	}
	m.shown = m.shown[:0]
	repo, _ := m.repo()
	for i, b := range m.branches {
		m.shown = append(m.shown, list.Rank{Index: i})
		if b.Name == repo.DefaultBranch {
			m.cursor = i
		}
	}
}

func (m *CreateBranch) key(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, keyBack):
		return func() tea.Msg { return PopMsg{} }
	case key.Matches(msg, keyCreate):
		return m.submit()
	case key.Matches(msg, keyNextField):
		m.setFocus((m.focus + 1) % fieldCount)
		return nil
	case key.Matches(msg, keyPrevField):
		m.setFocus((m.focus + fieldCount - 1) % fieldCount)
		return nil
	}

	switch m.focus {
	case fieldRepo:
		switch msg.String() {
		case "up", "k":
			return m.moveRepo(-1)
		case "down", "j":
			return m.moveRepo(1)
		}
	case fieldSource:
		switch msg.String() {
		case "up":
			m.cursor = max(0, m.cursor-1)
			return nil
		case "down":
			m.cursor = min(max(0, len(m.shown)-1), m.cursor+1)
			return nil
		}
		before := m.filter.Value()
		m.filter, _ = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.refilter()
		}
	case fieldName:
		m.name, _ = m.name.Update(msg)
	}
	return nil
}

func (m *CreateBranch) setFocus(f cbField) {
	m.focus = f
	m.filter.Blur()
	m.name.Blur()
	switch f {
	case fieldSource:
		m.filter.Focus()
	case fieldName:
		m.name.Focus()
	}
}

// moveRepo changes the selected repository, wrapping, and fetches its
// branches: another repository's are no answer.
func (m *CreateBranch) moveRepo(by int) tea.Cmd {
	if len(m.repos) < 2 {
		return nil
	}
	m.repoCursor = (m.repoCursor + by + len(m.repos)) % len(m.repos)
	return m.loadBranches()
}

// submit hands the flow to the work item list and goes back to it, or says
// what is still missing.
func (m *CreateBranch) submit() tea.Cmd {
	repo, ok := m.repo()
	if !ok {
		m.status, m.failed = "no repository to create the branch in", true
		return nil
	}
	from, ok := m.source()
	if !ok {
		m.status, m.failed = "choose a branch to create from first", true
		return nil
	}
	branch := strings.TrimSpace(m.name.Value())
	if branch == "" {
		m.status, m.failed = "the branch needs a name", true
		return nil
	}

	id := m.item.ID
	started := branchStartedMsg{Owner: m.owner, ID: id, Branch: branch, Repo: repo.Name, From: shortRef(from.Name)}
	return tea.Batch(
		func() tea.Msg { return PopMsg{} },
		func() tea.Msg { return started },
		m.create(id, branch, repo, from),
	)
}

// Body renders the three fields, the source list taking whatever height is
// left.
func (m *CreateBranch) Body(width, height int) string {
	var b strings.Builder

	b.WriteString(m.label(fieldRepo, "repository"))
	switch repo, ok := m.repo(); {
	case ok:
		b.WriteString(selectedRow.Render(repo.Name))
		if len(m.repos) > 1 {
			b.WriteString(chromeStyle.Render(fmt.Sprintf("   (%d of %d · j/k)", m.repoCursor+1, len(m.repos))))
		}
	case m.reposLoaded:
		b.WriteString(chromeStyle.Render("(none)"))
	default:
		b.WriteString(chromeStyle.Render("loading…"))
	}
	b.WriteString("\n\n")

	b.WriteString(m.label(fieldName, "name"))
	m.name.Width = max(1, width-labelWidth-1)
	b.WriteString(m.name.View())
	b.WriteString("\n\n")

	b.WriteString(m.label(fieldSource, "from"))
	m.filter.Width = max(1, width-labelWidth-lipgloss.Width(m.filter.Prompt)-1)
	b.WriteString(m.filter.View())
	b.WriteString("\n")

	rows := max(1, height-6)
	indent := strings.Repeat(" ", labelWidth)
	switch {
	case m.branchesFor == "":
		b.WriteString(indent + chromeStyle.Render("loading…"))
	case len(m.shown) == 0:
		b.WriteString(indent + chromeStyle.Render("nothing matches"))
	}
	offset := max(0, m.cursor-rows+1)
	end := min(len(m.shown), offset+rows)
	for i := offset; i < end; i++ {
		r := m.shown[i]
		marker, style := "  ", normalRow
		if i == m.cursor {
			marker, style = "▸ ", selectedRow
		}
		name := ansi.Truncate(m.targets[r.Index], max(1, width-labelWidth-2), "…")
		b.WriteString(indent + style.Render(marker) + highlight(name, r.MatchedIndexes, style))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// labelWidth is the column the fields' values start in.
const labelWidth = 13

// label renders a field's name, marked when it has the keyboard.
func (m *CreateBranch) label(f cbField, name string) string {
	text := fmt.Sprintf("%-*s", labelWidth, name)
	if m.focus == f {
		return coralStyle.Render(text)
	}
	return chromeStyle.Render(text)
}
