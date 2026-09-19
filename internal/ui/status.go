package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Status is what boardwalk worked out about this session, on screen.
//
// It exists because every one of those answers used to be invisible, and
// several of them fail quietly. A token that cannot read the Graph API, an
// `az account show` that named no user, a working directory that is not one
// of the project's repositories — each leaves boardwalk running and
// apparently fine, and each shows up somewhere else entirely as a list with
// nothing in it. There was no way to tell those apart from genuinely having
// no work.
//
// So this view does not only report state. Wherever something is missing it
// says what that costs, because "(unknown)" is a fact and "so nothing reads
// as yours" is the answer to the question the reader actually has.
type Status struct {
	client  *azdo.Client
	session azdo.Session

	viewport viewport.Model

	// repos is the project's repository list, fetched on entry so the
	// working directory can be resolved against it, and reposLoaded says
	// whether it arrived. reposErr is why not.
	repos       []azdo.Repo
	reposLoaded bool
	reposErr    error

	// renderedAt is the width the pane was last laid out for.
	renderedAt int

	status string
	failed bool
	work   work

	// configPath, workingDir and currentRepo are the surroundings, as
	// functions so a test can describe somewhere other than the machine it
	// is running on. The real ones read the filesystem and shell out to git,
	// which would otherwise make this view's output depend on where the test
	// suite happens to be checked out.
	configPath  func() (string, error)
	workingDir  func() (string, error)
	currentRepo func() string
}

// sessionMsg carries a re-resolved session back from the refresh key.
type sessionMsg struct{ Session azdo.Session }

// NewStatus builds the view over a session already resolved. The client comes
// too, for the repository list and for re-resolving.
func NewStatus(c *azdo.Client, s azdo.Session) *Status {
	return &Status{
		client:      c,
		session:     s,
		viewport:    viewport.New(0, 0),
		work:        newWork(),
		configPath:  config.Path,
		workingDir:  os.Getwd,
		currentRepo: azdo.CurrentRepo,
	}
}

// Init lists the project's repositories, which is the one thing on this
// screen that is not already in hand.
func (m *Status) Init() tea.Cmd {
	return tea.Batch(reposCmd(m.client, 0, ""), m.work.begin(1))
}

// resolveCmd re-runs the startup lookups off the UI goroutine.
func resolveSessionCmd(c *azdo.Client) tea.Cmd {
	return func() tea.Msg { return sessionMsg{Session: c.Resolve()} }
}

func (m *Status) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case reposFetchedMsg:
		m.work.done()
		m.reposLoaded, m.repos, m.reposErr = true, msg.Repos, msg.Err
		m.invalidate()
		return m, nil

	case sessionMsg:
		m.session = msg.Session
		m.status, m.failed = "re-resolved", false
		m.invalidate()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keyBack), key.Matches(msg, keyQuit):
			return m, func() tea.Msg { return PopMsg{} }
		case key.Matches(msg, keyRefresh):
			m.status, m.failed = "resolving…", false
			return m, tea.Batch(resolveSessionCmd(m.client), reposCmd(m.client, 0, ""))
		case key.Matches(msg, keyTop):
			m.viewport.GotoTop()
			return m, nil
		case key.Matches(msg, keyBottom):
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Status) invalidate() { m.renderedAt = 0 }

func (m *Status) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height
	if m.renderedAt != width {
		offset := m.viewport.YOffset
		m.viewport.SetContent(m.render(width))
		m.renderedAt = width
		m.viewport.SetYOffset(offset)
	}
	return m.viewport.View()
}

func (m *Status) render(width int) string {
	var b strings.Builder

	m.renderWhere(&b, width)
	m.renderIdentity(&b, width)
	m.renderGroups(&b, width)
	m.renderRepo(&b, width)
	return b.String()
}

// field writes one labelled line, with the label column the same width
// throughout so the values line up into a column of their own.
func field(b *strings.Builder, label, value string, width int) {
	fmt.Fprintf(b, "%s %s\n",
		labelStyle.Render(fmt.Sprintf("%-14s", label)),
		truncate(value, max(10, width-15)))
}

// consequence writes the sentence under a missing value that says what it
// costs. Indented under the field it belongs to and in the error colour,
// because it is the half of this screen worth reading.
func consequence(b *strings.Builder, text string, width int) {
	for _, l := range strings.Split(wordwrap(text, max(20, width-15)), "\n") {
		fmt.Fprintf(b, "%s %s\n", strings.Repeat(" ", 14), errStyle.Render(l))
	}
}

func (m *Status) renderWhere(b *strings.Builder, width int) {
	fmt.Fprintf(b, "%s\n\n", detailTitle.Render("where boardwalk is pointed"))
	field(b, "organisation:", m.session.Org, width)
	field(b, "project:", m.session.Project, width)

	path, err := m.configPath()
	if err != nil {
		field(b, "config:", "(could not be located: "+err.Error()+")", width)
		return
	}
	field(b, "config:", path, width)
}

func (m *Status) renderIdentity(b *strings.Builder, width int) {
	fmt.Fprintf(b, "\n%s\n\n", detailTitle.Render("who boardwalk thinks you are"))

	if m.session.Me == "" {
		field(b, "signed in as:", chromeStyle.Render("(unknown)"), width)
		consequence(b, "az account show did not report a user name, so nothing reads as yours: "+
			"the mine scope is empty and v matches no pull request at all.", width)
	} else {
		field(b, "signed in as:", m.session.Me, width)
	}

	switch {
	case m.session.MyID != "":
		field(b, "identity:", m.session.MyID, width)
	case m.session.IdentityErr != nil:
		field(b, "identity:", chromeStyle.Render("(unknown)"), width)
		consequence(b, "connectionData failed — "+m.session.IdentityErr.Error()+
			" — so a vote can only be cast on a pull request that names you directly.", width)
	default:
		field(b, "identity:", chromeStyle.Render("(unknown)"), width)
		consequence(b, "no identity was resolved, so a vote can only be cast on a pull request "+
			"that names you directly.", width)
	}
}

func (m *Status) renderGroups(b *strings.Builder, width int) {
	fmt.Fprintf(b, "\n%s\n\n", detailTitle.Render("review groups"))

	switch {
	case !m.session.GroupsResolved && m.session.GroupsErr != nil:
		field(b, "from Graph:", chromeStyle.Render("(not resolved)"), width)
		consequence(b, m.session.GroupsErr.Error()+
			" — only groups named in reviewGroups below will count as yours.", width)

	case !m.session.GroupsResolved:
		field(b, "from Graph:", chromeStyle.Render("(not resolved)"), width)
		consequence(b, "the Graph walk has not run — only groups named in reviewGroups below "+
			"will count as yours.", width)

	default:
		names := sortedNames(m.session.Groups)
		if len(names) == 0 {
			// Resolved and empty is a real answer, and a different one from
			// the two above. Saying so is the whole point of keeping
			// GroupsResolved separate from the groups themselves.
			field(b, "from Graph:", chromeStyle.Render("resolved — you are in no groups"), width)
			break
		}
		field(b, "from Graph:", fmt.Sprintf("%d resolved", len(names)), width)
		for _, n := range names {
			fmt.Fprintf(b, "%s%s\n", strings.Repeat(" ", 15), truncate(n, max(10, width-16)))
		}
	}

	if len(m.session.ReviewGroups) == 0 {
		field(b, "configured:", chromeStyle.Render("(none)"), width)
		return
	}
	field(b, "configured:", strings.Join(m.session.ReviewGroups, ", "), width)
}

// sortedNames is the group names to show, in a stable order.
//
// Groups.Names holds every group under both its scoped and its bare name, so
// that a reviewer entry matches whichever form it arrives in. Showing both
// would list each group twice, so a name that is the tail of a scoped one
// already listed is left out.
func sortedNames(g azdo.Groups) []string {
	all := make([]string, 0, len(g.Names))
	for n := range g.Names {
		all = append(all, n)
	}
	sort.Strings(all)

	scoped := map[string]bool{}
	for _, n := range all {
		if i := strings.LastIndex(n, `\`); i >= 0 {
			scoped[strings.TrimSpace(n[i+1:])] = true
		}
	}

	out := make([]string, 0, len(all))
	for _, n := range all {
		if !strings.Contains(n, `\`) && scoped[n] {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m *Status) renderRepo(b *strings.Builder, width int) {
	fmt.Fprintf(b, "\n%s\n\n", detailTitle.Render("where you are standing"))

	dir, err := m.workingDir()
	if err != nil {
		field(b, "working dir:", "(could not be read: "+err.Error()+")", width)
	} else {
		field(b, "working dir:", dir, width)
	}

	repo := m.currentRepo()
	if repo == "" {
		field(b, "repository:", chromeStyle.Render("not an Azure DevOps repository"), width)
		// Deliberately not an error: running boardwalk outside a checkout is
		// ordinary, and b now asks which repository to branch in.
		fmt.Fprintf(b, "%s%s\n", strings.Repeat(" ", 15),
			chromeStyle.Render("b will ask which repository to branch in"))
		return
	}

	switch {
	case !m.reposLoaded:
		field(b, "repository:", repo+chromeStyle.Render("  "+m.work.View()+"checking…"), width)
	case m.reposErr != nil:
		field(b, "repository:", repo+chromeStyle.Render("  (could not check the project's list)"), width)
	default:
		if _, ok := pickRepo(m.repos, repo); ok {
			field(b, "repository:", repo+statusStyle.Render("  in this project"), width)
			break
		}
		field(b, "repository:", repo+warnStyle.Render("  not one of this project's repositories"), width)
		fmt.Fprintf(b, "%s%s\n", strings.Repeat(" ", 15),
			chromeStyle.Render("b will ask which repository to branch in"))
	}
}

func (m *Status) Title() string {
	return fmt.Sprintf("session · %s/%s", m.session.Org, m.session.Project)
}

// Keys is short: this view reads, it does not act.
func (m *Status) Keys() help.KeyMap {
	own := []key.Binding{keyRefresh}
	return keyMap{
		short:  append(append([]key.Binding{}, own...), keyBack, keyHelp),
		groups: [][]key.Binding{{keyRefresh, keyTop, keyBottom}, navBindings()},
	}
}

// Status reports a session that will not work as a failure, so the problem is
// on the status line as well as in the body. Not every gap qualifies: being
// outside a repository is ordinary, and belonging to no groups may simply be
// true.
func (m *Status) Status() (string, bool) {
	if m.status != "" {
		return m.work.View() + m.status, m.failed
	}
	switch {
	case m.session.Me == "":
		return errStyle.Render("boardwalk does not know who you are — see above"), true
	case m.session.GroupsErr != nil:
		return errStyle.Render("your group memberships could not be resolved — see above"), true
	}
	return m.work.View(), false
}
