package azdo

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// prPageSize is how many active pull requests to ask for. A project with more
// than this many open at once is not a list anyone reads top to bottom.
const prPageSize = 200

// Reviewer is one person on a pull request, with the vote they cast.
type Reviewer struct {
	Name string
	Vote int
}

// VoteLabel renders Azure DevOps's numeric vote as the words its own UI uses.
func (r Reviewer) VoteLabel() string {
	switch {
	case r.Vote >= 10:
		return "approved"
	case r.Vote > 0:
		return "approved with suggestions"
	case r.Vote == 0:
		return "no vote"
	case r.Vote > -10:
		return "waiting for author"
	default:
		return "rejected"
	}
}

// PullRequest is one active pull request, in any repository in the project.
type PullRequest struct {
	ID          int
	Title       string
	Repo        string
	RepoID      string
	Author      string
	AuthorKey   string // uniqueName, lowercased, compared against Client.Me
	IsDraft     bool
	Source      string // full ref
	Target      string // full ref
	Created     time.Time
	Description string
	Reviewers   []Reviewer
}

// OpenThread is the opening comment of an unresolved discussion, which is what
// the detail pane shows.
type OpenThread struct {
	Author string
	Text   string
}

// ThreadCounts summarises a pull request's discussion.
type ThreadCounts struct {
	Resolved   int
	Unresolved int
	Open       []OpenThread
}

// PullRequests lists every active pull request in the project, across all of
// its repositories, newest first.
func (c *Client) PullRequests() ([]PullRequest, error) {
	var resp struct {
		Value []struct {
			ID        int       `json:"pullRequestId"`
			Title     string    `json:"title"`
			IsDraft   bool      `json:"isDraft"`
			Source    string    `json:"sourceRefName"`
			Target    string    `json:"targetRefName"`
			Created   time.Time `json:"creationDate"`
			Desc      string    `json:"description"`
			CreatedBy struct {
				DisplayName string `json:"displayName"`
				UniqueName  string `json:"uniqueName"`
			} `json:"createdBy"`
			Repository struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"repository"`
			Reviewers []struct {
				DisplayName string `json:"displayName"`
				Vote        int    `json:"vote"`
			} `json:"reviewers"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/pullrequests?searchCriteria.status=active&$top=%d&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), prPageSize, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	prs := make([]PullRequest, 0, len(resp.Value))
	for _, v := range resp.Value {
		pr := PullRequest{
			ID:          v.ID,
			Title:       v.Title,
			Repo:        v.Repository.Name,
			RepoID:      v.Repository.ID,
			Author:      v.CreatedBy.DisplayName,
			AuthorKey:   strings.ToLower(v.CreatedBy.UniqueName),
			IsDraft:     v.IsDraft,
			Source:      v.Source,
			Target:      v.Target,
			Created:     v.Created,
			Description: StripHTML(v.Desc),
		}
		for _, r := range v.Reviewers {
			pr.Reviewers = append(pr.Reviewers, Reviewer{Name: r.DisplayName, Vote: r.Vote})
		}
		prs = append(prs, pr)
	}

	// The endpoint's own ordering is undocumented, so newest-first is enforced
	// here rather than assumed.
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Created.After(prs[j].Created) })
	return prs, nil
}

// Threads summarises one pull request's comment threads.
func (c *Client) Threads(repoID string, prID int) (ThreadCounts, error) {
	var resp struct {
		Value []struct {
			Status    string `json:"status"`
			IsDeleted bool   `json:"isDeleted"`
			Comments  []struct {
				Content     string `json:"content"`
				CommentType string `json:"commentType"`
				Author      struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
			} `json:"comments"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/threads?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return ThreadCounts{}, err
	}

	var counts ThreadCounts
	for _, t := range resp.Value {
		if t.IsDeleted {
			continue
		}

		// Azure DevOps files its own activity — reviewers added, the source
		// branch updated — as threads. They carry no status and only system
		// comments, and counting them would make every pull request look busy.
		var first *OpenThread
		for _, cm := range t.Comments {
			if cm.CommentType == "system" {
				continue
			}
			first = &OpenThread{Author: cm.Author.DisplayName, Text: StripHTML(cm.Content)}
			break
		}
		if first == nil {
			continue
		}

		switch t.Status {
		case "fixed", "closed", "wontFix", "byDesign":
			counts.Resolved++
		case "active", "pending":
			counts.Unresolved++
			counts.Open = append(counts.Open, *first)
		}
	}
	return counts, nil
}

// PullRequestURL is the browser URL for a pull request.
func (c *Client) PullRequestURL(repo string, id int) string {
	return fmt.Sprintf("%s/%s/%s/_git/%s/pullrequest/%d",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(repo), id)
}
