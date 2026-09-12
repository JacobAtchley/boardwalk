package azdo

import (
	"net/http"
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

	counts, err := c.Threads("r1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
	if counts.Unresolved != 2 {
		t.Errorf("unresolved = %d, want 2 (active and pending)", counts.Unresolved)
	}
	if counts.Resolved != 4 {
		t.Errorf("resolved = %d, want 4 (fixed, closed, wontFix, byDesign)", counts.Resolved)
	}
	if len(counts.Open) != 2 {
		t.Fatalf("got %d open threads, want 2", len(counts.Open))
	}
	if counts.Open[0].Author != "Other Dev" || counts.Open[0].Text != "Why the retry cap?" {
		t.Errorf("first open thread = %+v", counts.Open[0])
	}
}

func TestThreadsOnAPullRequestWithNoDiscussion(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})

	counts, err := c.Threads("r1", 512)
	if err != nil {
		t.Fatalf("Threads returned %v", err)
	}
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
