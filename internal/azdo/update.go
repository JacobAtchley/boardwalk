package azdo

import (
	"fmt"
	"net/url"
)

// patchContentType is what the work item update endpoint requires. Sending
// application/json instead gets a 400 with no useful detail.
const patchContentType = "application/json-patch+json"

// operation is one JSON Patch step against a work item.
type operation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// SetState moves a work item to a new state, for example Active.
func (c *Client) SetState(id int, state string) error {
	return c.update(id, []operation{{
		Op:    "add",
		Path:  "/fields/System.State",
		Value: state,
	}})
}

// SetAssignee changes who a work item is assigned to. uniqueName is the
// account's uniqueName — the same string AssignedKey is compared against —
// and an empty string unassigns, since that is what an empty AssignedTo
// field means to Azure DevOps. This method does not refuse an empty
// uniqueName: the UI layer is what has to, since it is the one that can
// reach here with Client.Me empty by accident.
func (c *Client) SetAssignee(id int, uniqueName string) error {
	return c.update(id, []operation{{
		Op:    "add",
		Path:  "/fields/System.AssignedTo",
		Value: uniqueName,
	}})
}

// LinkBranch adds a branch to the work item's Development section, which is
// what Azure DevOps shows when a branch is associated with an item.
func (c *Client) LinkBranch(id int, projectID, repoID, branch string) error {
	return c.update(id, []operation{{
		Op:   "add",
		Path: "/relations/-",
		Value: map[string]any{
			"rel":        "ArtifactLink",
			"url":        BranchArtifactURL(projectID, repoID, branch),
			"attributes": map[string]string{"name": "Branch"},
		},
	}})
}

func (c *Client) update(id int, ops []operation) error {
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workitems/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	return c.patch(endpoint, patchContentType, ops, nil)
}

// BranchArtifactURL builds the vstfs identifier Azure DevOps uses for a branch.
// The three parts are joined with encoded slashes, and the branch name's own
// slashes are encoded the same way, so a nested branch does not read as extra
// path segments.
func BranchArtifactURL(projectID, repoID, branch string) string {
	return fmt.Sprintf("vstfs:///Git/Ref/%s%%2F%s%%2FGB%s",
		url.PathEscape(projectID), url.PathEscape(repoID), url.PathEscape(branch))
}
