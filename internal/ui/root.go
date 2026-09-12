package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// initView is the optional initialiser a view implements when it has data to
// fetch on entry. Root calls it when the view is pushed rather than at program
// start, so the menu paints without waiting on the network.
type initView interface{ Init() tea.Cmd }

// menuEntry is one line of the landing menu.
type menuEntry struct {
	key   string // the -start name and the subcommand name
	label string
	blurb string
}

var menuEntries = []menuEntry{
	{"items", "work items", "browse, branch and set state"},
	{"prs", "pull requests", "drafts, branches, comments and age"},
	{"builds", "builds", "pipeline runs, current step and logs"},
}

// Root owns the menu, the view stack, and every piece of chrome. Views render
// only their own body.
type Root struct {
	client *azdo.Client

	// mineOnly and includeClosed are the -mine and -all flags, carried so the
	// work item view fetches the scope boardwalk was launched with.
	mineOnly      bool
	includeClosed bool

	// reviewGroups comes from the config file, for the pull request view's
	// needs-my-review filter.
	reviewGroups []string

	stack  []View
	choice int

	// help renders a view's bindings: one line by default, every group when
	// the user presses "?".
	help help.Model

	width, height int

	// command is printed on exit for the shell wrapper to put on the prompt.
	command string
}

// NewRoot builds the root model. When start names a menu entry, that view is
// pushed immediately so boardwalk can be launched straight into it; otherwise
// the menu is the first thing shown. No data comes in here: every view fetches
// on entry, so the menu paints before anything touches the network — and a
// view reached from the menu loads its own data however boardwalk was started.
func NewRoot(c *azdo.Client, mineOnly, includeClosed bool, reviewGroups []string, start string) *Root {
	r := &Root{client: c, mineOnly: mineOnly, includeClosed: includeClosed, reviewGroups: reviewGroups, help: newHelp()}
	if start != "" {
		if v := r.build(start); v != nil {
			r.stack = append(r.stack, v)
		}
	}
	return r
}

// Init runs the starting view's fetch, if boardwalk was launched straight into
// one.
func (r *Root) Init() tea.Cmd {
	if len(r.stack) == 0 {
		return nil
	}
	return initialise(r.stack[len(r.stack)-1])
}

// ShellCommand is what to print after the program exits, or an empty string.
func (r *Root) ShellCommand() string { return r.command }

// build constructs the view named by a menu entry's key, or nil for an
// unrecognised name.
func (r *Root) build(name string) View {
	switch name {
	case "items":
		return NewWorkItems(r.client, nil, r.mineOnly, r.includeClosed)
	case "prs":
		return NewPullRequests(r.client, r.reviewGroups)
	case "builds":
		return NewBuilds(r.client)
	default:
		return nil
	}
}

// initialise runs a view's fetch, if it has one.
func initialise(v View) tea.Cmd {
	if iv, ok := v.(initView); ok {
		return iv.Init()
	}
	return nil
}

// top is the view on the top of the stack, if any.
func (r *Root) top() (View, bool) {
	if len(r.stack) == 0 {
		return nil, false
	}
	return r.stack[len(r.stack)-1], true
}

// Update handles a message, satisfying tea.Model.
func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		return r, nil

	case PushMsg:
		r.stack = append(r.stack, msg.View)
		return r, initialise(msg.View)

	case PopMsg:
		if len(r.stack) > 0 {
			r.stack = r.stack[:len(r.stack)-1]
		}
		return r, nil

	case ShellCommandMsg:
		r.command = msg.Command
		return r, tea.Quit

	case tea.KeyMsg:
		if cmd, handled := r.key(msg); handled {
			return r, cmd
		}
	}

	top, ok := r.top()
	if !ok {
		return r, nil
	}

	if topOnly(msg) {
		updated, cmd := top.Update(msg)
		r.stack[len(r.stack)-1] = updated
		return r, cmd
	}

	// Everything else is data, and data goes to every view in the stack. A
	// fetch a view started before the user drilled into something resolves
	// while that view is no longer on top: delivering it to the top alone
	// dropped it, and the view that asked for it kept its in-flight guard set
	// forever. The views already key their own messages by id and ignore
	// types that are not theirs, so a broadcast reaches exactly one of them.
	var cmds []tea.Cmd
	for i, v := range r.stack {
		updated, cmd := v.Update(msg)
		r.stack[i] = updated
		cmds = append(cmds, cmd)
	}
	return r, tea.Batch(cmds...)
}

// topOnly reports whether a message belongs to the view the user is looking at
// and to no other. Keys must not be acted on by a hidden view, and the status
// line is chrome Root renders once — a hidden view setting it would describe
// something that is not on screen.
func topOnly(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyMsg, StatusMsg, ErrMsg:
		return true
	}
	return false
}

// key handles the bindings Root owns. Everything else falls through to the view
// on top, and nothing is intercepted while a view has a text prompt open —
// which is why the view is asked for its status first.
func (r *Root) key(msg tea.KeyMsg) (tea.Cmd, bool) {
	top, hasView := r.top()

	if !hasView {
		switch msg.String() {
		case "up", "k":
			r.choice = (r.choice - 1 + len(menuEntries)) % len(menuEntries)
			return nil, true
		case "down", "j":
			r.choice = (r.choice + 1) % len(menuEntries)
			return nil, true
		case "enter":
			v := r.build(menuEntries[r.choice].key)
			if v == nil {
				return nil, true
			}
			r.stack = append(r.stack, v)
			return initialise(v), true
		case "q", "esc", "ctrl+c":
			return tea.Quit, true
		}
		return nil, false
	}

	// ctrl+c is above the typing guard on purpose. bubbletea does not quit on
	// it by itself — the model has to — and the branch prompt is a bare
	// textinput that does not bind it, so yielding it to the view left the
	// universal terminal interrupt doing nothing at all.
	if msg.String() == "ctrl+c" {
		return tea.Quit, true
	}

	// Otherwise a view that is taking typed input owns every key, including
	// esc and q.
	if typing(top) {
		return nil, false
	}

	if key.Matches(msg, keyHelp) {
		r.help.ShowAll = !r.help.ShowAll
		return nil, true
	}

	switch msg.String() {
	case "esc":
		r.stack = r.stack[:len(r.stack)-1]
		return nil, true
	case "q":
		// A drill-down treats q as "go back"; from a top-level view it quits.
		if len(r.stack) > 1 {
			r.stack = r.stack[:len(r.stack)-1]
			return nil, true
		}
		return tea.Quit, true
	}
	return nil, false
}

// typing reports whether the view on top has a prompt open — the fuzzy filter
// or the branch name editor — in which case Root must not claim esc or q.
func typing(v View) bool {
	type prompter interface{ Prompting() bool }
	if p, ok := v.(prompter); ok {
		return p.Prompting()
	}
	return false
}

// View renders the chrome and whatever is on top of the stack, satisfying
// tea.Model.
func (r *Root) View() string {
	if r.width == 0 {
		return "loading…"
	}

	top, ok := r.top()
	if !ok {
		return r.menuView()
	}

	// The help panel is one line closed and several open, so the body is sized
	// from what it actually renders rather than from a constant. The title and
	// the status line are one row each.
	r.help.Width = r.width
	helpView := r.help.View(top.Keys())
	body := top.Body(r.width, max(1, r.height-lipgloss.Height(helpView)-2))

	status, isErr := top.Status()
	style := statusStyle
	if isErr {
		style = errStyle
	}

	return strings.Join([]string{
		chromeStyle.Render(top.Title()),
		body,
		helpView,
		style.Render(truncate(status, r.width)),
	}, "\n")
}

// menuView renders the banner and the landing menu.
func (r *Root) menuView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", renderBanner(r.width))

	for i, e := range menuEntries {
		line := fmt.Sprintf("%-16s %s", e.label, chromeStyle.Render(e.blurb))
		if i == r.choice {
			fmt.Fprintf(&b, "%s\n", menuPicked.Render("▸ "+line))
			continue
		}
		fmt.Fprintf(&b, "%s\n", menuItem.Render(line))
	}

	fmt.Fprintf(&b, "\n%s\n", chromeStyle.Render(
		fmt.Sprintf("%s/%s · ↑↓ move · enter open · q quit", r.client.Org, r.client.Project)))
	return b.String()
}
