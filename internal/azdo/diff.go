package azdo

import (
	"fmt"
	"net/url"
	"time"
)

// Iteration is one state a pull request has passed through. Azure DevOps files
// a new one every time the author pushes to the source branch, so the last one
// in the list is the pull request as it stands now.
type Iteration struct {
	ID      int
	Created time.Time
}

// Change is one file a pull request touches.
type Change struct {
	Path       string
	ChangeType string
	IsFolder   bool
}

// PullRequestIterations lists a pull request's iterations, oldest first.
func (c *Client) PullRequestIterations(repoID string, prID int) ([]Iteration, error) {
	var resp struct {
		Value []struct {
			ID      int       `json:"id"`
			Created time.Time `json:"createdDate"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/iterations?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	iterations := make([]Iteration, 0, len(resp.Value))
	for _, v := range resp.Value {
		iterations = append(iterations, Iteration{ID: v.ID, Created: v.Created})
	}
	return iterations, nil
}

// IterationChanges lists the files one iteration touches.
//
// Folders are dropped. Azure DevOps files a directory as a change of its own
// whenever a file inside it is added or removed, and a directory has no
// content to diff — it would be a row that can only ever say "nothing to show".
func (c *Client) IterationChanges(repoID string, prID, iterationID int) ([]Change, error) {
	// This endpoint answers with changeEntries rather than the value array
	// every other list endpoint uses.
	var resp struct {
		ChangeEntries []struct {
			ChangeType string `json:"changeType"`
			Item       struct {
				Path     string `json:"path"`
				IsFolder bool   `json:"isFolder"`
			} `json:"item"`
		} `json:"changeEntries"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/iterations/%d/changes?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, iterationID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	var changes []Change
	for _, e := range resp.ChangeEntries {
		if e.Item.IsFolder {
			continue
		}
		changes = append(changes, Change{
			Path:       e.Item.Path,
			ChangeType: e.ChangeType,
			IsFolder:   e.Item.IsFolder,
		})
	}
	return changes, nil
}

// FileAtCommit reads one file's text as of a commit.
//
// $format=json is sent explicitly and the content comes out of the JSON
// envelope, rather than asking for the body as plain text through getText. The
// items endpoint content-negotiates: with no $format it answers with the raw
// file for one Accept header and with a JSON envelope for another, and an
// envelope rendered as source is not an error anybody sees — it is a diff pane
// quietly full of {"objectId":… instead of code. Pinning the format means the
// shape cannot depend on what a proxy did to the request, and a file whose own
// contents are JSON still cannot be confused with the envelope around it.
// Content is a pointer so that a field which is absent can be told from one
// that is present and empty. They mean opposite things: an empty string is a
// genuinely empty file, while a missing field is an envelope that did not carry
// the content at all — includeContent ignored, or an item that is not a blob.
// Reading both as "" would render that side of the diff as an empty file, and
// an ordinary edit would read as a wholesale addition. That is the silent
// failure asking for $format=json was meant to rule out, so it is an error here
// rather than a quietly wrong diff.
func (c *Client) FileAtCommit(repoID, path, commitSHA string) (string, error) {
	var resp struct {
		Content *string `json:"content"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/items?path=%s"+
			"&versionDescriptor.version=%s&versionDescriptor.versionType=commit"+
			"&includeContent=true&$format=json&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), url.QueryEscape(path), url.QueryEscape(commitSHA), APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return "", err
	}
	if resp.Content == nil {
		return "", fmt.Errorf("no content was returned for %s at %s", path, commitSHA)
	}
	return *resp.Content, nil
}

// PullRequestFileURL is the browser URL for one file inside a pull request,
// so the open action from the diff view lands on the file being read rather
// than on the top of the pull request.
func (c *Client) PullRequestFileURL(repo string, id int, path string) string {
	return fmt.Sprintf("%s?_a=files&path=%s",
		c.PullRequestURL(repo, id), url.QueryEscape(path))
}
