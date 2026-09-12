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

// Reviewer is one reviewer on a pull request, with the vote they cast.
//
// A reviewer can be a group rather than a person — a team or a security group
// added to the pull request — and Azure DevOps returns both in the same list,
// telling them apart with isContainer. Nothing in the payload says who is in
// the group.
type Reviewer struct {
	Name string
	Key  string // uniqueName, lowercased, compared against Client.Me
	Vote int
	// IsGroup marks a team or security group standing in for its members.
	IsGroup bool
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

// ThreadComment is one comment in a pull request discussion.
type ThreadComment struct {
	Author  string
	Created time.Time
	Text    string
}

// Thread is one discussion on a pull request: the whole exchange, not just its
// opening comment, because the detail view reads it as a conversation.
type Thread struct {
	Status   string
	Resolved bool
	// File is set when the thread is anchored to a line of the diff rather
	// than to the pull request as a whole.
	File     string
	Comments []ThreadComment
}

// Opener is the comment a thread starts with, which is what a summary shows.
func (t Thread) Opener() ThreadComment {
	if len(t.Comments) == 0 {
		return ThreadComment{}
	}
	return t.Comments[0]
}

// ThreadCounts summarises a pull request's discussion.
type ThreadCounts struct {
	Resolved   int
	Unresolved int
	Open       []Thread
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
				UniqueName  string `json:"uniqueName"`
				Vote        int    `json:"vote"`
				IsContainer bool   `json:"isContainer"`
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
			Description: Markdown(v.Desc),
		}
		for _, r := range v.Reviewers {
			pr.Reviewers = append(pr.Reviewers, Reviewer{
				Name:    r.DisplayName,
				Key:     strings.ToLower(r.UniqueName),
				Vote:    r.Vote,
				IsGroup: r.IsContainer,
			})
		}
		prs = append(prs, pr)
	}

	// The endpoint's own ordering is undocumented, so newest-first is enforced
	// here rather than assumed.
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Created.After(prs[j].Created) })
	return prs, nil
}

// Threads summarises one pull request's comment threads.
func (c *Client) Threads(repoID string, prID int) ([]Thread, error) {
	var resp struct {
		Value []struct {
			Status    string `json:"status"`
			IsDeleted bool   `json:"isDeleted"`
			Context   *struct {
				FilePath string `json:"filePath"`
			} `json:"threadContext"`
			Comments []struct {
				Content     string    `json:"content"`
				CommentType string    `json:"commentType"`
				Published   time.Time `json:"publishedDate"`
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
		return nil, err
	}

	var threads []Thread
	for _, t := range resp.Value {
		if t.IsDeleted {
			continue
		}

		// Azure DevOps files its own activity — reviewers added, the source
		// branch updated — as threads. They carry only system comments, and
		// keeping them would make every pull request look busy with discussion
		// nobody wrote.
		thread := Thread{Status: t.Status, Resolved: resolvedStatus(t.Status)}
		if t.Context != nil {
			thread.File = t.Context.FilePath
		}
		for _, cm := range t.Comments {
			if cm.CommentType == "system" {
				continue
			}
			thread.Comments = append(thread.Comments, ThreadComment{
				Author:  cm.Author.DisplayName,
				Created: cm.Published,
				// Pull request comments are written in markdown, so this is a
				// pass-through unless someone pasted HTML in.
				Text: Markdown(cm.Content),
			})
		}
		if len(thread.Comments) == 0 {
			continue
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

// Summarize counts a pull request's threads for the list's column, and keeps
// the unresolved ones for its side pane.
//
// A thread whose status is neither resolved nor unresolved — one Azure DevOps
// left unset — is counted in neither, on purpose: it is discussion that exists
// but is not waiting on anybody.
func Summarize(threads []Thread) ThreadCounts {
	var counts ThreadCounts
	for _, t := range threads {
		switch {
		case t.Resolved:
			counts.Resolved++
		case unresolvedStatus(t.Status):
			counts.Unresolved++
			counts.Open = append(counts.Open, t)
		}
	}
	return counts
}

func resolvedStatus(s string) bool {
	switch s {
	case "fixed", "closed", "wontFix", "byDesign":
		return true
	}
	return false
}

func unresolvedStatus(s string) bool { return s == "active" || s == "pending" }

// PullRequestURL is the browser URL for a pull request.
func (c *Client) PullRequestURL(repo string, id int) string {
	return fmt.Sprintf("%s/%s/%s/_git/%s/pullrequest/%d",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(repo), id)
}
