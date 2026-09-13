package azdo

import (
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// zeroObjectID is what the refs endpoint wants as oldObjectId to mean "this ref
// does not exist yet".
const zeroObjectID = "0000000000000000000000000000000000000000"

// Repo is one Git repository in the project.
type Repo struct {
	ID            string
	Name          string
	ProjectID     string
	DefaultBranch string // full ref, e.g. refs/heads/main
}

// Repos lists the project's Git repositories.
func (c *Client) Repos() ([]Repo, error) {
	var resp struct {
		Value []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			DefaultBranch string `json:"defaultBranch"`
			Project       struct {
				ID string `json:"id"`
			} `json:"project"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/git/repositories?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	repos := make([]Repo, 0, len(resp.Value))
	for _, v := range resp.Value {
		repos = append(repos, Repo{
			ID:            v.ID,
			Name:          v.Name,
			ProjectID:     v.Project.ID,
			DefaultBranch: v.DefaultBranch,
		})
	}
	return repos, nil
}

// RefHead returns the commit a ref points at. ref is the full form,
// refs/heads/main.
func (c *Client) RefHead(repoID, ref string) (string, error) {
	var resp struct {
		Value []struct {
			Name     string `json:"name"`
			ObjectID string `json:"objectId"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/refs?filter=%s&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), url.QueryEscape(strings.TrimPrefix(ref, "refs/")), APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return "", err
	}
	// The endpoint's filter is a starts-with match, so filter=heads/main also
	// returns refs/heads/main-2 and refs/heads/main-hotfix, in unspecified
	// order. Taking the first entry back would branch off whichever ref the
	// server happened to list first — the right name is the only way to know
	// which ref this is.
	for _, v := range resp.Value {
		if v.Name == ref {
			return v.ObjectID, nil
		}
	}
	return "", fmt.Errorf("no ref matching %s", ref)
}

// MergeRefPullRequestID parses the pull request id out of a merge ref, the
// refs/pull/{id}/merge form Azure DevOps builds against instead of a branch
// when a pipeline runs for a pull request rather than a push. ok is false for
// anything else, a plain branch ref included, so a caller can fall back to
// matching branch names without checking the ref's shape itself.
func MergeRefPullRequestID(ref string) (id int, ok bool) {
	const prefix, suffix = "refs/pull/", "/merge"
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, suffix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(ref, prefix), suffix))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// CreateBranch creates refs/heads/<branch> pointing at fromSHA. branch is the
// short name, without the refs/heads prefix.
func (c *Client) CreateBranch(repoID, branch, fromSHA string) error {
	body := []map[string]string{{
		"name":        "refs/heads/" + branch,
		"oldObjectId": zeroObjectID,
		"newObjectId": fromSHA,
	}}

	var resp struct {
		Value []struct {
			Success       bool   `json:"success"`
			CustomMessage string `json:"customMessage"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/refs?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), APIVersion)
	if err := c.post(endpoint, body, &resp); err != nil {
		return err
	}

	// The endpoint answers 200 even for a ref it refused, so the per-ref flag
	// is the only place a rejection shows up.
	for _, v := range resp.Value {
		if !v.Success {
			detail := v.CustomMessage
			if detail == "" {
				detail = "the server rejected the ref without saying why"
			}
			return fmt.Errorf("could not create %s: %s", branch, detail)
		}
	}
	return nil
}

// CurrentRepo names the Azure DevOps repository the working directory belongs
// to, or an empty string when the directory is not one.
func CurrentRepo() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return RepoFromRemote(strings.TrimSpace(string(out)))
}

// RepoFromRemote pulls the repository name out of an Azure DevOps remote URL.
// Anything that is not one — a GitHub remote, say — yields an empty string, so
// boardwalk does not filter a project list down to a repository that is not in
// it.
func RepoFromRemote(remote string) string {
	if !isAzureRemote(remote) {
		return ""
	}

	remote = strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	// The HTTPS and legacy forms both name the repo after a _git or _ssh
	// segment; the SSH form simply ends with it.
	for _, marker := range []string{"/_git/", "/_ssh/"} {
		if i := strings.Index(remote, marker); i >= 0 {
			return remote[i+len(marker):]
		}
	}
	if i := strings.LastIndex(remote, "/"); i >= 0 {
		return remote[i+1:]
	}
	return ""
}

func isAzureRemote(remote string) bool {
	for _, host := range []string{"dev.azure.com", "visualstudio.com"} {
		if strings.Contains(remote, host) {
			return true
		}
	}
	return false
}
