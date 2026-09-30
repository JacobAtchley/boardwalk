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

// branchDoneMsg is a finished flow. Owner is the view that opened the create
// branch screen: it alone reports the outcome and copies the checkout, so the
// command is copied once however many views the broadcast reaches. Any view
// holding the item may still sync what landed on the server.
type branchDoneMsg struct {
	BranchResult
	Owner View
}

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

// reposFetchedMsg carries the project's repositories. ID names the work item
// the create branch screen fetched them for — Root broadcasts data to every
// view in the stack, so the answer has to say whose it is. The session view
// fetches them too, with an ID of zero.
type reposFetchedMsg struct {
	ID    int
	Repos []azdo.Repo
	Err   error
}

// branchesFetchedMsg carries a repository's branches, the candidates to
// create from. RepoID is carried alongside ID because the repository can
// change while the fetch is out, and a listing for the one just left is
// stale.
type branchesFetchedMsg struct {
	ID     int
	RepoID string
	Refs   []azdo.Ref
	Err    error
}

// branchStartedMsg tells the view that opened the create branch screen — its
// Owner — that a flow is running, so its status line can say so while the
// screen itself is gone.
type branchStartedMsg struct {
	Owner  View
	ID     int
	Branch string
	Repo   string
	From   string
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

// runBranchFlow creates the branch from the commit from points at, moves the work item to Active, and links
// the branch to it. Each step is recorded before the next runs, so a later
// failure does not erase what already happened.
func runBranchFlow(c *azdo.Client, id int, branch string, repo azdo.Repo, from azdo.Ref) BranchResult {
	res := BranchResult{Branch: branch, ID: id}

	if err := c.CreateBranch(repo.ID, branch, from.ObjectID); err != nil {
		res.Err = err
		return res
	}
	res.Steps = append(res.Steps, fmt.Sprintf("created %s from %s in %s", branch, shortRef(from.Name), repo.Name))
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

// reposCmd lists the project's repositories, off the UI goroutine. id is the
// work item the answer is for; see reposFetchedMsg.
func reposCmd(c *azdo.Client, id int) tea.Cmd {
	return func() tea.Msg {
		repos, err := c.Repos()
		return reposFetchedMsg{ID: id, Repos: repos, Err: err}
	}
}

// branchesCmd lists a repository's branches, off the UI goroutine.
func branchesCmd(c *azdo.Client, id int, repoID string) tea.Cmd {
	return func() tea.Msg {
		refs, err := c.Branches(repoID)
		return branchesFetchedMsg{ID: id, RepoID: repoID, Refs: refs, Err: err}
	}
}

// branchCmd runs the flow off the UI goroutine against a repository and a
// source branch already decided on.
func branchCmd(c *azdo.Client, owner View, id int, branch string, repo azdo.Repo, from azdo.Ref) tea.Cmd {
	return func() tea.Msg {
		return branchDoneMsg{BranchResult: runBranchFlow(c, id, branch, repo, from), Owner: owner}
	}
}

// branchReport is the status line for a finished flow, and copies the
// checkout command once the branch is on the server. It is the owner's to
// call; see branchDoneMsg.
func branchReport(res BranchResult) (status string, failed bool) {
	// The checkout command is only worth handing over once the ref is really
	// on the server, but it is worth handing over even when a later step
	// failed — the branch is there either way, and retyping it by hand is
	// exactly what this saves.
	copied := false
	if res.Created {
		copied = copyToClipboard(CheckoutCommand(res.Branch)) == nil
	}

	if res.Err != nil {
		if len(res.Steps) > 0 {
			return strings.Join(res.Steps, ", ") + "; then " + res.Err.Error(), true
		}
		return res.Err.Error(), true
	}

	status = strings.Join(res.Steps, " · ")
	// boardwalk cannot move another shell's working tree, so the checkout
	// goes on the clipboard: open a shell in the repository and paste.
	if copied {
		return status + " · checkout command copied — paste it in the repository", false
	}
	return status + " · could not copy the checkout command: " + CheckoutCommand(res.Branch), false
}

// branchStartedStatus is the status line while a flow runs.
func branchStartedStatus(msg branchStartedMsg) string {
	return fmt.Sprintf("creating %s from %s in %s…", msg.Branch, msg.From, msg.Repo)
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
