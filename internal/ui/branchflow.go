package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// BranchResult is what the branch flow did. Steps names everything that
// succeeded, so a failure halfway through still reports the work that landed
// rather than reading as a clean failure. Activated is set structurally,
// alongside the "set #%d Active" step, rather than recovered by matching that
// step's text — so the row can be synced to the server even when the flow
// fails on the link step afterward. Created is set the same way: the checkout
// command is worth handing over the moment the ref exists, whatever the rest
// of the flow then does.
type BranchResult struct {
	Branch    string
	ID        int
	Steps     []string
	Created   bool
	Activated bool
	Err       error
}

type branchDoneMsg struct{ BranchResult }

type stateSetMsg struct {
	ID    int
	State string
	Err   error
}

// assigneeSetMsg is stateSetMsg's counterpart for assign-to-me. It carries
// Assigned and AssignedKey rather than leaving the view to reconstruct them:
// a row has to update both together on success, since AssignedKey is what
// the mine scope filter reads and Assigned is what the person sees, and
// updating one without the other would leave them disagreeing.
type assigneeSetMsg struct {
	ID          int
	Assigned    string
	AssignedKey string
	Err         error
}

// reposFetchedMsg carries the project's repositories, fetched once a branch
// name has been typed. It names the work item and the branch it was fetched
// for: Root broadcasts data to every view in the stack, and the answer has to
// carry the flow it belongs to because nothing else on the receiving side
// still holds it.
type reposFetchedMsg struct {
	ID     int
	Branch string
	Repos  []azdo.Repo
	Err    error
}

// pickRepo finds the repository the working directory belongs to among the
// project's repositories.
func pickRepo(repos []azdo.Repo, want string) (azdo.Repo, bool) {
	if want == "" {
		return azdo.Repo{}, false
	}
	for _, r := range repos {
		if r.Name == want {
			return r, true
		}
	}
	return azdo.Repo{}, false
}

// runBranchFlow creates the branch, moves the work item to Active, and links
// the branch to it. Each step is recorded before the next runs, so a later
// failure does not erase what already happened.
func runBranchFlow(c *azdo.Client, id int, branch string, repo azdo.Repo) BranchResult {
	res := BranchResult{Branch: branch, ID: id}

	head, err := c.RefHead(repo.ID, repo.DefaultBranch)
	if err != nil {
		res.Err = fmt.Errorf("could not read %s in %s: %w", shortRef(repo.DefaultBranch), repo.Name, err)
		return res
	}

	if err := c.CreateBranch(repo.ID, branch, head); err != nil {
		res.Err = err
		return res
	}
	res.Steps = append(res.Steps, fmt.Sprintf("created %s in %s", branch, repo.Name))
	res.Created = true

	if err := c.SetState(id, "Active"); err != nil {
		res.Err = fmt.Errorf("could not set #%d Active: %w", id, err)
		return res
	}
	res.Steps = append(res.Steps, fmt.Sprintf("set #%d Active", id))
	res.Activated = true

	if err := c.LinkBranch(id, repo.ProjectID, repo.ID, branch); err != nil {
		res.Err = fmt.Errorf("could not link the branch to #%d: %w", id, err)
		return res
	}
	res.Steps = append(res.Steps, "linked the branch")

	return res
}

// reposCmd lists the project's repositories, off the UI goroutine. It is the
// first half of the branch flow: the view decides from the answer whether the
// working directory settles the question or the picker has to ask.
//
// The repository used to be resolved inside the flow's own command, which
// meant a working directory that was not one of the project's repositories
// could only be reported as a failure — there was nowhere left to ask. The
// fetch is separated from the flow so that there is.
func reposCmd(c *azdo.Client, id int, branch string) tea.Cmd {
	return func() tea.Msg {
		repos, err := c.Repos()
		return reposFetchedMsg{ID: id, Branch: branch, Repos: repos, Err: err}
	}
}

// branchCmd runs the flow off the UI goroutine against a repository already
// decided on.
func branchCmd(c *azdo.Client, id int, branch string, repo azdo.Repo) tea.Cmd {
	return func() tea.Msg {
		return branchDoneMsg{runBranchFlow(c, id, branch, repo)}
	}
}

// CheckoutCommand is what the user pastes into another shell to land on a
// branch boardwalk just created on the server. boardwalk cannot move the
// working tree of a shell it is not running in — and quitting to hand the
// command back to one meant losing the session — so the command goes on the
// clipboard instead and boardwalk stays open.
//
// The pull is there for the case where the branch already existed and has
// commits on it; on a ref created a moment ago it is a no-op.
func CheckoutCommand(branch string) string {
	return fmt.Sprintf("git fetch origin && git checkout %s && git pull", shellQuote(branch))
}

// shellQuote wraps a branch name in single quotes when it holds anything a
// shell would act on. Git allows characters in a ref name that a shell treats
// as syntax — & and ; among them — and this command is written to be pasted
// into a prompt, so a name carrying one has to arrive as a single word.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '.' || r == '_' || r == '-' || r == '/':
			return false
		}
		return true
	}) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// stateCmd moves a work item to a state without the rest of the branch flow.
func stateCmd(c *azdo.Client, id int, state string) tea.Cmd {
	return func() tea.Msg {
		return stateSetMsg{ID: id, State: state, Err: c.SetState(id, state)}
	}
}

// assignCmd assigns a work item to the signed-in user. It is the caller's job
// to not reach here with Client.Me empty — see Client.Me's own doc — since an
// empty uniqueName is a deliberate unassign at the client layer, not
// something this command guards against a second time.
//
// Assigned is set to the same string as AssignedKey: az account show only
// hands NewClient a uniqueName, never a display name, so that is what the
// row shows until a real fetch replaces it with what the server has.
func assignCmd(c *azdo.Client, id int) tea.Cmd {
	return func() tea.Msg {
		me := c.Me
		return assigneeSetMsg{ID: id, Assigned: me, AssignedKey: me, Err: c.SetAssignee(id, me)}
	}
}
