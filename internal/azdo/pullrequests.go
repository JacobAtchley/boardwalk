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
	// ID is the reviewer's GUID, which is how the vote endpoint addresses
	// them — uniqueName does not work there. It is only ever populated for
	// entries actually returned in a pull request's own reviewers list; see
	// MyReviewerID in review.go for why that matters.
	ID   string
	Vote int
	// IsGroup marks a team or security group standing in for its members.
	IsGroup bool
}

// Azure DevOps's reviewer vote scale. VoteLabel below decodes them; nothing in
// between -10 and 10 other than 5 and -5 means anything to the API.
const (
	VoteApproved                = 10
	VoteApprovedWithSuggestions = 5
	VoteNoVote                  = 0
	VoteWaitingForAuthor        = -5
	VoteRejected                = -10
)

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
	ID     int
	Title  string
	Repo   string
	RepoID string
	// ProjectID is the GUID of the team project the repository belongs to.
	// The project is known by name everywhere else in boardwalk, but the
	// policy endpoint addresses a pull request by an artifact id built from
	// the GUID, and the name will not do there.
	ProjectID string
	Author    string
	AuthorKey string // uniqueName, lowercased, compared against Client.Me
	IsDraft   bool
	// Status is active, completed or abandoned. The project-wide listing only
	// asks for active ones, but a work item links to pull requests long after
	// they merge.
	Status      string
	Source      string // full ref
	Target      string // full ref
	Created     time.Time
	Description string
	Reviewers   []Reviewer
	// SourceCommit and TargetCommit are the two commits the diff view reads a
	// file at. They are the *merge* commits Azure DevOps maintains, not the tips
	// of the two branches: the target tip moves as other work merges, and
	// diffing against it would show this pull request as also containing
	// everybody else's. Both are empty on a pull request whose merge Azure
	// DevOps has not computed yet — a conflicted one, or one only just opened.
	SourceCommit string
	TargetCommit string
}

// ThreadComment is one comment in a pull request discussion.
type ThreadComment struct {
	ID      int
	Author  string
	Created time.Time
	Text    string
}

// Thread is one discussion on a pull request: the whole exchange, not just its
// opening comment, because the detail view reads it as a conversation.
type Thread struct {
	ID       int
	Status   string
	Resolved bool
	// File is set when the thread is anchored to a file of the diff rather
	// than to the pull request as a whole.
	File string
	// Line is the line within File the thread was written against, or 0 for
	// a thread on the file as a whole. RightSide says which side of the diff
	// that line is numbered by: the new file, or — for a comment on text the
	// pull request removed — the old one.
	//
	// A thread on a line that was changed rather than added carries both
	// sides, and the new one is taken. It is where the reader is looking,
	// and it is the side the diff pane numbers its own lines by.
	Line      int
	RightSide bool
	Comments  []ThreadComment
}

// ThreadsForFile picks out the threads written against one file, in the order
// the server listed them.
//
// The paths are compared without their leading slash. Azure DevOps writes one
// on both a thread's filePath and a change's path, so they match as they
// stand — but a mismatch there would show up as a file with no discussion
// rather than as an error, which is the kind of thing nobody notices.
func ThreadsForFile(threads []Thread, path string) []Thread {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return nil
	}

	var out []Thread
	for _, t := range threads {
		if strings.TrimPrefix(t.File, "/") == path {
			out = append(out, t)
		}
	}
	return out
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
		Value []pullRequestJSON `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/pullrequests?searchCriteria.status=active&$top=%d&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), prPageSize, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	prs := make([]PullRequest, 0, len(resp.Value))
	for _, v := range resp.Value {
		prs = append(prs, v.pullRequest())
	}

	// The endpoint's own ordering is undocumented, so newest-first is enforced
	// here rather than assumed.
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Created.After(prs[j].Created) })
	return prs, nil
}

// pullRequestJSON is the wire shape, shared by the project-wide listing and the
// single fetch a linked pull request needs.
type pullRequestJSON struct {
	ID        int       `json:"pullRequestId"`
	Title     string    `json:"title"`
	IsDraft   bool      `json:"isDraft"`
	Status    string    `json:"status"`
	Source    string    `json:"sourceRefName"`
	Target    string    `json:"targetRefName"`
	Created   time.Time `json:"creationDate"`
	Desc      string    `json:"description"`
	CreatedBy struct {
		DisplayName string `json:"displayName"`
		UniqueName  string `json:"uniqueName"`
	} `json:"createdBy"`
	Repository struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	} `json:"repository"`
	LastMergeSourceCommit struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeSourceCommit"`
	LastMergeTargetCommit struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeTargetCommit"`
	Reviewers []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		UniqueName  string `json:"uniqueName"`
		Vote        int    `json:"vote"`
		IsContainer bool   `json:"isContainer"`
	} `json:"reviewers"`
}

func (v pullRequestJSON) pullRequest() PullRequest {
	pr := PullRequest{
		ID:           v.ID,
		Title:        v.Title,
		Repo:         v.Repository.Name,
		RepoID:       v.Repository.ID,
		ProjectID:    v.Repository.Project.ID,
		Author:       v.CreatedBy.DisplayName,
		AuthorKey:    strings.ToLower(v.CreatedBy.UniqueName),
		IsDraft:      v.IsDraft,
		Status:       v.Status,
		Source:       v.Source,
		Target:       v.Target,
		Created:      v.Created,
		Description:  Markdown(v.Desc),
		SourceCommit: v.LastMergeSourceCommit.CommitID,
		TargetCommit: v.LastMergeTargetCommit.CommitID,
	}
	for _, r := range v.Reviewers {
		pr.Reviewers = append(pr.Reviewers, Reviewer{
			Name:    r.DisplayName,
			Key:     strings.ToLower(r.UniqueName),
			ID:      r.ID,
			Vote:    r.Vote,
			IsGroup: r.IsContainer,
		})
	}
	return pr
}

// threadPosition is one end of the range a review comment was written
// against. Only the line is read: the column is what the web UI uses to
// underline part of a line, and a terminal pane showing whole lines has
// nothing to do with it.
type threadPosition struct {
	Line int `json:"line"`
}

// Threads summarises one pull request's comment threads.
func (c *Client) Threads(repoID string, prID int) ([]Thread, error) {
	var resp struct {
		Value []threadJSON `json:"value"`
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
		thread := t.thread()
		// Azure DevOps files its own activity — reviewers added, the source
		// branch updated — as threads. They carry only system comments, so
		// once those are dropped there is nothing left, and keeping them
		// would make every pull request look busy with discussion nobody
		// wrote.
		if len(thread.Comments) == 0 {
			continue
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

// threadJSON is a thread as the API sends it. It is a named type rather than
// an anonymous one because two endpoints answer with it — listing a pull
// request's discussion, and creating one thread — and a second copy of this
// shape is a second place for the parsing to drift.
type threadJSON struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	IsDeleted bool   `json:"isDeleted"`
	Context   *struct {
		FilePath   string          `json:"filePath"`
		LeftStart  *threadPosition `json:"leftFileStart"`
		RightStart *threadPosition `json:"rightFileStart"`
	} `json:"threadContext"`
	Comments []struct {
		ID          int       `json:"id"`
		Content     string    `json:"content"`
		CommentType string    `json:"commentType"`
		Published   time.Time `json:"publishedDate"`
		Author      struct {
			DisplayName string `json:"displayName"`
		} `json:"author"`
	} `json:"comments"`
}

// thread converts the payload, dropping the system comments Azure DevOps
// files its own activity as.
func (t threadJSON) thread() Thread {
	thread := Thread{ID: t.ID, Status: t.Status, Resolved: resolvedStatus(t.Status)}
	if t.Context != nil {
		thread.File = t.Context.FilePath
		switch {
		case t.Context.RightStart != nil:
			thread.Line, thread.RightSide = t.Context.RightStart.Line, true
		case t.Context.LeftStart != nil:
			thread.Line = t.Context.LeftStart.Line
		}
	}
	for _, cm := range t.Comments {
		if cm.CommentType == "system" {
			continue
		}
		thread.Comments = append(thread.Comments, ThreadComment{
			ID:      cm.ID,
			Author:  cm.Author.DisplayName,
			Created: cm.Published,
			// Pull request comments are written in markdown, so this is a
			// pass-through unless someone pasted HTML in.
			Text: Markdown(cm.Content),
		})
	}
	return thread
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

// ReplyToThread posts a comment onto an existing thread, addressed to
// parentCommentID — the API answers a specific comment rather than the thread
// CreateThread starts a review thread against one line of one file.
//
// The line is the new side's — the file as the pull request leaves it — which
// is what the file view's cursor reports and the only side a comment can be
// anchored to from there. A line the pull request removed is not in the new
// file and has nothing to hang a comment on; the caller refuses that before
// reaching here.
//
// Azure DevOps positions a comment by a start and an end, each a line and a
// column, so a whole-line comment is sent as the full line: column one to
// column one of the same line. Sending a context without offsets is rejected
// as incomplete rather than defaulted.
//
// The thread comes back so the view can show it straight away rather than
// refetching the whole discussion for the one comment it already knows about
// — the same reasoning updateThread uses for a reply.
func (c *Client) CreateThread(repoID string, prID int, path string, line int, text string) (Thread, error) {
	type position struct {
		Line   int `json:"line"`
		Offset int `json:"offset"`
	}
	body := struct {
		Comments []map[string]any `json:"comments"`
		Status   string           `json:"status"`
		Context  struct {
			FilePath   string   `json:"filePath"`
			RightStart position `json:"rightFileStart"`
			RightEnd   position `json:"rightFileEnd"`
		} `json:"threadContext"`
	}{
		Comments: []map[string]any{{
			"parentCommentId": 0,
			"content":         text,
			"commentType":     "text",
		}},
		// A new review comment is something for somebody to answer, which is
		// what active means. The alternative is posting it already closed,
		// which is a note to nobody.
		Status: "active",
	}
	body.Context.FilePath = path
	body.Context.RightStart = position{Line: line, Offset: 1}
	body.Context.RightEnd = position{Line: line, Offset: 1}

	var resp threadJSON
	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/threads?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	if err := c.post(endpoint, body, &resp); err != nil {
		return Thread{}, err
	}
	return resp.thread(), nil
}

// as a whole, and the detail view only ever offers to answer the opener.
func (c *Client) ReplyToThread(repoID string, prID, threadID, parentCommentID int, text string) (ThreadComment, error) {
	body := struct {
		Content         string `json:"content"`
		ParentCommentID int    `json:"parentCommentId"`
		CommentType     string `json:"commentType"`
	}{Content: text, ParentCommentID: parentCommentID, CommentType: "text"}

	var resp struct {
		ID        int       `json:"id"`
		Content   string    `json:"content"`
		Published time.Time `json:"publishedDate"`
		Author    struct {
			DisplayName string `json:"displayName"`
		} `json:"author"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/threads/%d/comments?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, threadID, APIVersion)
	if err := c.post(endpoint, body, &resp); err != nil {
		return ThreadComment{}, err
	}
	return ThreadComment{
		ID:      resp.ID,
		Author:  resp.Author.DisplayName,
		Created: resp.Published,
		Text:    Markdown(resp.Content),
	}, nil
}

// SetThreadStatus moves a thread to a new status: active, fixed, wontFix,
// closed or pending. The resolve key only ever sends fixed, but the method
// takes the status rather than hard-coding it because there is nothing
// reply-specific about the endpoint.
func (c *Client) SetThreadStatus(repoID string, prID, threadID int, status string) error {
	body := struct {
		Status string `json:"status"`
	}{Status: status}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/threads/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, threadID, APIVersion)
	// Thread status is an ordinary JSON body, unlike the json-patch+json that
	// work item updates require.
	return c.patch(endpoint, "application/json", body, nil)
}

// SetVote casts reviewerID's vote on a pull request. The endpoint is a PUT
// rather than a PATCH — it replaces the whole reviewer entry — which is also
// how Azure DevOps adds someone as a reviewer who was not one before, though
// boardwalk only ever calls it with an id already in the pull request's own
// reviewers list.
func (c *Client) SetVote(repoID string, prID int, reviewerID string, vote int) error {
	body := struct {
		Vote int `json:"vote"`
	}{Vote: vote}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/reviewers/%s?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, url.PathEscape(reviewerID), APIVersion)
	return c.put(endpoint, body, nil)
}

// SetDraft publishes a draft pull request or puts a published one back into
// draft.
//
// The body carries isDraft and nothing else. This endpoint updates whatever
// fields it is given — title, description, target branch — so a struct with
// any other field in it would quietly overwrite that field with its zero
// value on every toggle.
//
// Whether the signed-in user may do this at all is Azure DevOps's call: it
// depends on repository permissions boardwalk cannot see from here, so a
// refusal comes back as the server's own message rather than a guess made
// before asking.
func (c *Client) SetDraft(repoID string, prID int, draft bool) error {
	body := struct {
		IsDraft bool `json:"isDraft"`
	}{IsDraft: draft}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.PathEscape(repoID), prID, APIVersion)
	// Ordinary JSON, like thread status above and unlike a work item update.
	return c.patch(endpoint, "application/json", body, nil)
}

// PullRequestURL is the browser URL for a pull request.
func (c *Client) PullRequestURL(repo string, id int) string {
	return fmt.Sprintf("%s/%s/%s/_git/%s/pullrequest/%d",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(repo), id)
}
