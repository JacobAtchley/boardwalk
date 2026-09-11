package ui

import (
	"fmt"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// BranchResult is what the branch flow did. Steps names everything that
// succeeded, so a failure halfway through still reports the work that landed
// rather than reading as a clean failure. Activated is set structurally,
// alongside the "set #%d Active" step, rather than recovered by matching that
// step's text — so the row can be synced to the server even when the flow
// fails on the link step afterward.
type BranchResult struct {
	Branch    string
	ID        int
	Steps     []string
	Activated bool
	Err       error
}

type branchDoneMsg struct{ BranchResult }

type stateSetMsg struct {
	ID    int
	State string
	Err   error
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

// branchCmd runs the flow off the UI goroutine, resolving the repository first.
func branchCmd(c *azdo.Client, id int, branch string) tea.Cmd {
	return func() tea.Msg {
		repos, err := c.Repos()
		if err != nil {
			return branchDoneMsg{BranchResult{Branch: branch, Err: err}}
		}

		repo, ok := pickRepo(repos, azdo.CurrentRepo())
		if !ok {
			return branchDoneMsg{BranchResult{Branch: branch, Err: fmt.Errorf(
				"run boardwalk inside one of the project's repositories, or create the branch there — "+
					"the working directory is not an Azure DevOps repository in %s", c.Project)}}
		}
		return branchDoneMsg{runBranchFlow(c, id, branch, repo)}
	}
}

// stateCmd moves a work item to a state without the rest of the branch flow.
func stateCmd(c *azdo.Client, id int, state string) tea.Cmd {
	return func() tea.Msg {
		return stateSetMsg{ID: id, State: state, Err: c.SetState(id, state)}
	}
}
