package azdo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const prListBody = `{"value":[
 {"pullRequestId":512,"title":"Retry webhooks","isDraft":false,
  "sourceRefName":"refs/heads/feature/4021-retry","targetRefName":"refs/heads/main",
  "creationDate":"2026-09-10T09:00:00Z","description":"<p>Adds backoff</p>",
  "createdBy":{"displayName":"Dev Example","uniqueName":"Dev@Acme.test"},
  "repository":{"id":"r1","name":"platform-api"},
  "reviewers":[{"displayName":"Other Dev","vote":10},{"displayName":"Third Dev","vote":-5}]},
 {"pullRequestId":511,"title":"WIP cache","isDraft":true,
  "sourceRefName":"refs/heads/spike","targetRefName":"refs/heads/main",
  "creationDate":"2026-09-09T09:00:00Z",
  "createdBy":{"displayName":"Dev Example","uniqueName":"dev@acme.test"},
  "repository":{"id":"r2","name":"platform-web"}}
]}`

func TestPullRequestsParsesTheListing(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("searchCriteria.status"); got != "active" {
			t.Errorf("status = %q, want active", got)
		}
		w.Write([]byte(prListBody))
	})

	prs, err := c.PullRequests()
	if err != nil {
		t.Fatalf("PullRequests returned %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d pull requests, want 2", len(prs))
	}

	first := prs[0]
	if first.ID != 512 || first.Repo != "platform-api" || first.RepoID != "r1" {
		t.Errorf("identity = %+v", first)
	}
	if first.IsDraft {
		t.Error("first pull request read as a draft")
	}
	if !prs[1].IsDraft {
		t.Error("second pull request did not read as a draft")
	}
	if first.Description != "Adds backoff" {
		t.Errorf("description = %q, want the HTML stripped", first.Description)
	}
	// The identity comparison against Client.Me is lowercased at the source, so
	// a mixed-case uniqueName still matches.
	if first.AuthorKey != "dev@acme.test" {
		t.Errorf("author key = %q, want it lowercased", first.AuthorKey)
	}
	if len(first.Reviewers) != 2 || first.Reviewers[0].VoteLabel() != "approved" {
		t.Errorf("reviewers = %+v", first.Reviewers)
	}
}

func TestPullRequestsSortsNewestFirst(t *testing.T) {
	// The API's ordering is not guaranteed, so the sort has to be ours.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
		 {"pullRequestId":1,"creationDate":"2026-09-01T09:00:00Z","repository":{"id":"r1","name":"a"}},
		 {"pullRequestId":2,"creationDate":"2026-09-11T09:00:00Z","repository":{"id":"r1","name":"a"}},
		 {"pullRequestId":3,"creationDate":"2026-09-05T09:00:00Z","repository":{"id":"r1","name":"a"}}
		]}`))
	})

	prs, _ := c.PullRequests()
	if prs[0].ID != 2 || prs[1].ID != 3 || prs[2].ID != 1 {
		t.Errorf("order = %d, %d, %d; want 2, 3, 1", prs[0].ID, prs[1].ID, prs[2].ID)
	}
}

func TestVoteLabel(t *testing.T) {
	for _, tc := range []struct {
		vote int
		want string
	}{
		{10, "approved"},
		{5, "approved with suggestions"},
		{0, "no vote"},
		{-5, "waiting for author"},
		{-10, "rejected"},
	} {
		if got := (Reviewer{Vote: tc.vote}).VoteLabel(); got != tc.want {
			t.Errorf("vote %d = %q, want %q", tc.vote, got, tc.want)
		}
	}
}

func TestThreadsCountsByStatusAndDropsSystemNoise(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
		 {"id":1,"status":"active","isDeleted":false,
		  "comments":[{"commentType":"text","content":"Why the retry cap?","author":{"displayName":"Other Dev"}}]},
		 {"id":2,"status":"pending","isDeleted":false,
		  "comments":[{"commentType":"text","content":"Nit: naming","author":{"displayName":"Third Dev"}}]},
		 {"id":3,"status":"fixed","isDeleted":false,
		  "comments":[{"commentType":"text","content":"Done","author":{"displayName":"Dev Example"}}]},
		 {"id":4,"status":"closed","isDeleted":false,
		  "comments":[{"commentType":"text","content":"Moot","author":{"displayName":"Dev Example"}}]},
		 {"id":5,"status":"wontFix","isDeleted":false,
		  "comments":[{"commentType":"text","content":"No","author":{"displayName":"Dev Example"}}]},
		 {"id":6,"status":"byDesign","isDeleted":false,
		  "comments":[{"commentType":"text","content":"Intended","author":{"displayName":"Dev Example"}}]},
		 {"id":7,"isDeleted":false,
		  "comments":[{"commentType":"system","content":"Dev Example added Other Dev as a reviewer","author":{"displayName":"Azure DevOps"}}]},
		 {"id":8,"status":"active","isDeleted":true,
		  "comments":[{"commentType":"text","content":"Deleted","author":{"displayName":"Other Dev"}}]}
		]}`))
	})

	threads, err := c.Threads("r1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	counts := Summarize(threads)
	if counts.Unresolved != 2 {
		t.Errorf("unresolved = %d, want 2 (active and pending)", counts.Unresolved)
	}
	if counts.Resolved != 4 {
		t.Errorf("resolved = %d, want 4 (fixed, closed, wontFix, byDesign)", counts.Resolved)
	}
	if len(counts.Open) != 2 {
		t.Fatalf("got %d open threads, want 2", len(counts.Open))
	}
	if counts.Open[0].Opener().Author != "Other Dev" || counts.Open[0].Opener().Text != "Why the retry cap?" {
		t.Errorf("first open thread = %+v", counts.Open[0])
	}
}

func TestThreadsOnAPullRequestWithNoDiscussion(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})

	threads, err := c.Threads("r1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	counts := Summarize(threads)
	if counts.Resolved != 0 || counts.Unresolved != 0 || len(counts.Open) != 0 {
		t.Errorf("counts = %+v, want everything zero", counts)
	}
}

func TestPullRequestURL(t *testing.T) {
	c := &Client{Org: "acme", Project: "Platform"}
	want := "https://dev.azure.com/acme/Platform/_git/platform-api/pullrequest/512"
	if got := c.PullRequestURL("platform-api", 512); got != want {
		t.Errorf("PullRequestURL = %q, want %q", got, want)
	}
}

func TestPullRequestsMarksGroupReviewers(t *testing.T) {
	// A group standing in for its members arrives in the same list as a person
	// and is told apart only by isContainer.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"pullRequestId":700,"repository":{"id":"r1","name":"a"},
		 "reviewers":[
		   {"displayName":"Dev Example","uniqueName":"Dev@Acme.test","vote":0},
		   {"displayName":"platform-devs","isContainer":true,"vote":0}]}]}`))
	})

	prs, err := c.PullRequests()
	if err != nil {
		t.Fatalf("PullRequests returned %v", err)
	}

	person, group := prs[0].Reviewers[0], prs[0].Reviewers[1]
	if person.IsGroup {
		t.Error("a person was marked as a group")
	}
	if person.Key != "dev@acme.test" {
		t.Errorf("reviewer key = %q, want it lowercased for comparison against Me", person.Key)
	}
	if !group.IsGroup {
		t.Error("a group reviewer was not marked as one")
	}
}

func TestThreadsKeepsTheWholeConversation(t *testing.T) {
	// The list's column only needs a count, but the detail view reads a thread
	// as an exchange — so the reply has to survive the fetch.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
		 {"id":1,"status":"active","isDeleted":false,
		  "threadContext":{"filePath":"/internal/azdo/client.go"},
		  "comments":[
		    {"id":1,"commentType":"system","content":"Dev added a reviewer","author":{"displayName":"Azure DevOps"}},
		    {"id":2,"commentType":"text","content":"Why the retry cap?","publishedDate":"2026-09-11T09:00:00Z","author":{"displayName":"Other Dev"}},
		    {"id":3,"commentType":"text","content":"Three felt right.","publishedDate":"2026-09-11T10:00:00Z","author":{"displayName":"Dev Example"}}]}
		]}`))
	})

	threads, err := c.Threads("r1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}

	th := threads[0]
	if th.ID != 1 {
		t.Errorf("thread id = %d, want 1, so a reply can address it", th.ID)
	}
	if len(th.Comments) != 2 {
		t.Fatalf("got %d comments, want the two real ones without the system entry", len(th.Comments))
	}
	if th.Comments[1].Text != "Three felt right." {
		t.Errorf("the reply is missing: %+v", th.Comments)
	}
	if th.Comments[0].ID != 2 {
		t.Errorf("opener comment id = %d, want 2, so a reply can name it as the parent", th.Comments[0].ID)
	}
	if th.Comments[0].Created.IsZero() {
		t.Error("publishedDate did not parse")
	}
	if th.File != "/internal/azdo/client.go" {
		t.Errorf("file = %q, want the thread's anchor", th.File)
	}
	if th.Resolved {
		t.Error("an active thread read as resolved")
	}
	if th.Opener().Author != "Other Dev" {
		t.Errorf("Opener = %+v, want the first real comment", th.Opener())
	}
}

func TestReplyToThreadPostsAComment(t *testing.T) {
	var method, path, body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":9,"content":"Three felt right.",
		 "publishedDate":"2026-09-11T10:00:00Z","author":{"displayName":"Dev Example"}}`))
	})

	comment, err := c.ReplyToThread("r1", 512, 1, 2, "Three felt right.")
	if err != nil {
		t.Fatalf("ReplyToThread returned %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/acme/Platform/_apis/git/repositories/r1/pullRequests/512/threads/1/comments" {
		t.Errorf("path = %q", path)
	}
	for _, want := range []string{`"content":"Three felt right."`, `"parentCommentId":2`, `"commentType":"text"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s, want it to contain %s", body, want)
		}
	}
	if comment.ID != 9 || comment.Author != "Dev Example" || comment.Text != "Three felt right." {
		t.Errorf("comment = %+v, want the server's created comment", comment)
	}
	if comment.Created.IsZero() {
		t.Error("publishedDate did not parse")
	}
}

func TestReplyToThreadReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"TF401180: thread does not exist"}`))
	})

	_, err := c.ReplyToThread("r1", 512, 99, 1, "hi")
	if err == nil || !strings.Contains(err.Error(), "TF401180") {
		t.Errorf("error = %v, want the server message", err)
	}
}

func TestSetThreadStatusSendsPlainJSON(t *testing.T) {
	var method, path, contentType, body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":1,"status":"fixed"}`))
	})

	if err := c.SetThreadStatus("r1", 512, 1, "fixed"); err != nil {
		t.Fatalf("SetThreadStatus returned %v", err)
	}
	if method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", method)
	}
	if path != "/acme/Platform/_apis/git/repositories/r1/pullRequests/512/threads/1" {
		t.Errorf("path = %q", path)
	}
	// Unlike a work item update, this is ordinary JSON rather than a patch
	// document.
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if body != `{"status":"fixed"}` {
		t.Errorf("body = %q", body)
	}
}

func TestSetThreadStatusReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"TF401320: invalid status"}`))
	})

	err := c.SetThreadStatus("r1", 512, 1, "fixed")
	if err == nil || !strings.Contains(err.Error(), "TF401320") {
		t.Errorf("error = %v, want the server message", err)
	}
}
