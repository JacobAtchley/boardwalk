package azdo

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// pullRequestArtifact is the prefix Azure DevOps gives a linked pull request.
// A work item's relations hold branches, commits and pull requests in the same
// list, told apart only by this.
const pullRequestArtifact = "vstfs:///Git/PullRequestId/"

// PullRequestRef names a pull request without fetching it. The repository id
// comes along because every pull request endpoint is scoped to one.
type PullRequestRef struct {
	RepoID string
	ID     int
}

// ParsePullRequestArtifact reads a linked pull request out of a work item
// relation's vstfs URL.
//
// The three parts are joined with encoded slashes, the same shape
// BranchArtifactURL writes for a branch — but responses are not consistent
// about whether they arrive encoded, so both forms are accepted.
func ParsePullRequestArtifact(artifact string) (PullRequestRef, bool) {
	if !strings.HasPrefix(artifact, pullRequestArtifact) {
		return PullRequestRef{}, false
	}

	rest := strings.TrimPrefix(artifact, pullRequestArtifact)
	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}

	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return PullRequestRef{}, false
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		return PullRequestRef{}, false
	}
	return PullRequestRef{RepoID: parts[1], ID: id}, true
}

// WorkItemPullRequests lists the pull requests linked to a work item.
//
// Relations are not part of the default response and are not something
// workitemsbatch returns for a whole project's worth of items, so this is a
// single-item fetch made when one is opened.
func (c *Client) WorkItemPullRequests(id int) ([]PullRequestRef, error) {
	var resp struct {
		Relations []struct {
			Rel string `json:"rel"`
			URL string `json:"url"`
		} `json:"relations"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workitems/%d?$expand=relations&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	var refs []PullRequestRef
	for _, rel := range resp.Relations {
		if ref, ok := ParsePullRequestArtifact(rel.URL); ok {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// PullRequestWorkItemIDs lists the work items a pull request is linked to.
func (c *Client) PullRequestWorkItemIDs(repoID string, prID int) ([]int, error) {
	var resp struct {
		Value []struct {
			// A string, not a number, unlike every other work item id the API
			// returns.
			ID string `json:"id"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/workitems?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	var ids []int
	for _, v := range resp.Value {
		if id, err := strconv.Atoi(v.ID); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// WorkItemsByID fetches a specific set of work items, for the handful a pull
// request links to.
func (c *Client) WorkItemsByID(ids []int) ([]WorkItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return c.workItemFields(ids)
}

// PullRequestByID fetches one pull request, for the handful a work item links
// to. The project-wide listing only carries active ones, and a work item's
// links routinely point at pull requests that have already merged.
func (c *Client) PullRequestByID(repoID string, prID int) (PullRequest, error) {
	var v pullRequestJSON

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	if err := c.get(endpoint, &v); err != nil {
		return PullRequest{}, err
	}
	return v.pullRequest(), nil
}
