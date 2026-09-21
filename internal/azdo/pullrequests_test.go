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
		   {"id":"guid-1","displayName":"Dev Example","uniqueName":"Dev@Acme.test","vote":0},
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
	if person.ID != "guid-1" {
		t.Errorf("reviewer id = %q, want the payload's guid, so a vote can address them", person.ID)
	}
	if !group.IsGroup {
		t.Error("a group reviewer was not marked as one")
	}
	if group.ID != "" {
		t.Errorf("group id = %q, want none: a group is never who a vote is cast for", group.ID)
	}
}

func TestSetVotePutsTheVoteToTheReviewerID(t *testing.T) {
	var method, path, contentType, body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"vote":10}`))
	})

	if err := c.SetVote("r1", 512, "guid-1", VoteApproved); err != nil {
		t.Fatalf("SetVote returned %v", err)
	}
	if method != http.MethodPut {
		t.Errorf("method = %s, want PUT", method)
	}
	if path != "/acme/Platform/_apis/git/repositories/r1/pullRequests/512/reviewers/guid-1" {
		t.Errorf("path = %q", path)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if body != `{"vote":10}` {
		t.Errorf("body = %q", body)
	}
}

func TestSetVoteReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"TF401027: not authorized"}`))
	})

	err := c.SetVote("r1", 512, "guid-1", VoteRejected)
	if err == nil || !strings.Contains(err.Error(), "TF401027") {
		t.Errorf("error = %v, want the server message", err)
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

func TestSetDraftPatchesThePullRequest(t *testing.T) {
	var method, path, contentType, body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"pullRequestId":512,"isDraft":false}`))
	})

	if err := c.SetDraft("r1", 512, false); err != nil {
		t.Fatalf("SetDraft returned %v", err)
	}
	if method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", method)
	}
	if path != "/acme/Platform/_apis/git/repositories/r1/pullRequests/512" {
		t.Errorf("path = %q", path)
	}
	// Ordinary JSON, not the json-patch document a work item update needs.
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	// isDraft alone: a PATCH carrying any other field would overwrite that
	// field with whatever its zero value happens to be.
	if body != `{"isDraft":false}` {
		t.Errorf("body = %q", body)
	}
}

func TestSetDraftSendsTrueWhenMarkingADraft(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"pullRequestId":512,"isDraft":true}`))
	})

	if err := c.SetDraft("r1", 512, true); err != nil {
		t.Fatalf("SetDraft returned %v", err)
	}
	if body != `{"isDraft":true}` {
		t.Errorf("body = %q", body)
	}
}

func TestSetDraftReportsAServerRefusal(t *testing.T) {
	// Publishing someone else's pull request needs a permission the signed-in
	// user may not have, and the refusal is the server's to make: boardwalk
	// does not guess at who is allowed, it repeats what it was told.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"TF401027: you need Contribute permission"}`))
	})

	err := c.SetDraft("r1", 512, false)
	if err == nil || !strings.Contains(err.Error(), "TF401027") {
		t.Errorf("error = %v, want the server message", err)
	}
}

// TestThreadsCarryTheLineTheyWereWrittenAgainst — filePath alone says which
// file a review comment is about; the diff view needs to know which line, and
// threadContext carries that too.
func TestThreadsCarryTheLineTheyWereWrittenAgainst(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
			{"id":1,"status":"active","threadContext":{"filePath":"/internal/ui/logs.go",
				"rightFileStart":{"line":42,"offset":1},"rightFileEnd":{"line":42,"offset":9}},
			 "comments":[{"id":1,"content":"this leaks","commentType":"text"}]},
			{"id":2,"status":"active","threadContext":{"filePath":"/internal/ui/old.go",
				"leftFileStart":{"line":7,"offset":1}},
			 "comments":[{"id":1,"content":"why was this dropped","commentType":"text"}]},
			{"id":3,"status":"active","threadContext":{"filePath":"/README.md"},
			 "comments":[{"id":1,"content":"file-level note","commentType":"text"}]},
			{"id":4,"status":"active",
			 "comments":[{"id":1,"content":"about the whole pull request","commentType":"text"}]}
		]}`))
	})

	threads, err := c.Threads("repo-1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	if len(threads) != 4 {
		t.Fatalf("got %d threads, want 4", len(threads))
	}

	for _, tc := range []struct {
		name      string
		thread    Thread
		wantFile  string
		wantLine  int
		wantRight bool
	}{
		{"on the new side", threads[0], "/internal/ui/logs.go", 42, true},
		{"on the old side", threads[1], "/internal/ui/old.go", 7, false},
		{"on a file but no line", threads[2], "/README.md", 0, false},
		{"on the pull request itself", threads[3], "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.thread.File != tc.wantFile {
				t.Errorf("File = %q, want %q", tc.thread.File, tc.wantFile)
			}
			if tc.thread.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", tc.thread.Line, tc.wantLine)
			}
			if tc.thread.RightSide != tc.wantRight {
				t.Errorf("RightSide = %v, want %v", tc.thread.RightSide, tc.wantRight)
			}
		})
	}
}

// TestThreadOnBothSidesPrefersTheNewOne — a comment on a line that was
// changed rather than added carries both. The new side is where the reader is
// looking, and it is the side the diff pane numbers its lines by.
func TestThreadOnBothSidesPrefersTheNewOne(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"id":1,"status":"active","threadContext":{"filePath":"/a.go",
			"leftFileStart":{"line":10},"rightFileStart":{"line":12}},
			"comments":[{"id":1,"content":"x","commentType":"text"}]}]}`))
	})

	threads, err := c.Threads("repo-1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	if threads[0].Line != 12 || !threads[0].RightSide {
		t.Errorf("Line = %d RightSide = %v, want 12 and the new side",
			threads[0].Line, threads[0].RightSide)
	}
}

func TestThreadsForFileGroupsByPath(t *testing.T) {
	threads := []Thread{
		{ID: 1, File: "/a.go", Line: 3},
		{ID: 2, File: "/b.go", Line: 9},
		{ID: 3, File: "/a.go", Line: 40},
		{ID: 4}, // on the pull request itself, not on any file
	}

	got := ThreadsForFile(threads, "/a.go")
	if len(got) != 2 {
		t.Fatalf("got %d threads for /a.go, want 2", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 3 {
		t.Errorf("got threads %d and %d, want 1 and 3", got[0].ID, got[1].ID)
	}

	// Azure DevOps writes a thread's path with a leading slash and the change
	// list does too, but matching has to survive one of them arriving without
	// it rather than silently finding nothing.
	if len(ThreadsForFile(threads, "a.go")) != 2 {
		t.Error("a path without its leading slash matched nothing")
	}
	if len(ThreadsForFile(threads, "/nothing.go")) != 0 {
		t.Error("a file with no discussion matched something")
	}
}

func TestCreateThreadAnchorsToTheLine(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(body)
		w.Write([]byte(`{"id":77,"status":"active","threadContext":{"filePath":"/internal/ui/tail.go",
			"rightFileStart":{"line":42}},
			"comments":[{"id":1,"content":"this still races","commentType":"text",
			"author":{"displayName":"Dev Example"}}]}`))
	})

	thread, err := c.CreateThread("repo-1", 512, "/internal/ui/tail.go", 42, "this still races")
	if err != nil {
		t.Fatalf("CreateThread returned %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/acme/Platform/_apis/git/repositories/repo-1/pullRequests/512/threads"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	for _, want := range []string{
		`"filePath":"/internal/ui/tail.go"`,
		`"rightFileStart"`,
		`"rightFileEnd"`,
		`"line":42`,
		`"this still races"`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body is missing %s:\n%s", want, gotBody)
		}
	}

	// The created thread comes back so the view can show it without
	// refetching the whole discussion.
	if thread.ID != 77 {
		t.Errorf("thread id = %d, want the one the server made", thread.ID)
	}
	if thread.Line != 42 || !thread.RightSide {
		t.Errorf("thread anchored at line %d right=%v, want 42 on the new side", thread.Line, thread.RightSide)
	}
	if len(thread.Comments) != 1 || thread.Comments[0].Text != "this still races" {
		t.Errorf("thread comments = %+v, want the one just written", thread.Comments)
	}
}

// TestCreateThreadSendsAnOffset — Azure DevOps positions a comment by line
// and column, and a context with no offset is rejected as incomplete rather
// than defaulted.
func TestCreateThreadSendsAnOffset(t *testing.T) {
	var gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Write([]byte(`{"id":1,"comments":[{"id":1,"content":"x","commentType":"text"}]}`))
	})

	if _, err := c.CreateThread("repo-1", 512, "/a.go", 7, "x"); err != nil {
		t.Fatalf("CreateThread returned %v", err)
	}
	if !strings.Contains(gotBody, `"offset"`) {
		t.Errorf("body carries no offset:\n%s", gotBody)
	}
}

func TestCreateThreadReportsTheServersRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"the line is outside the file's changes"}`))
	})

	if _, err := c.CreateThread("repo-1", 512, "/a.go", 9000, "x"); err == nil {
		t.Fatal("CreateThread reported success on a rejection")
	} else if !strings.Contains(err.Error(), "outside the file's changes") {
		t.Errorf("error = %v, want the server's own message in it", err)
	}
}

func TestPullRequestParsesTheProjectItsRepositoryBelongsTo(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
 {"pullRequestId":512,"title":"Retry webhooks","sourceRefName":"refs/heads/x","targetRefName":"refs/heads/main",
  "creationDate":"2026-09-10T09:00:00Z","createdBy":{"displayName":"Dev","uniqueName":"dev@acme.test"},
  "repository":{"id":"r1","name":"platform-api","project":{"id":"p-guid","name":"Platform"}}}]}`))
	})

	prs, err := c.PullRequests()
	if err != nil {
		t.Fatalf("PullRequests returned %v", err)
	}
	if prs[0].ProjectID != "p-guid" {
		t.Errorf("project id = %q, want p-guid", prs[0].ProjectID)
	}
}
