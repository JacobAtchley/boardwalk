package azdo

import (
	"net/http"
	"testing"
)

func TestParsePullRequestArtifact(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     PullRequestRef
		ok       bool
	}{
		{
			// The three parts are joined with encoded slashes, exactly as
			// BranchArtifactURL writes them for a branch.
			name: "the encoded form Azure DevOps returns",
			in:   "vstfs:///Git/PullRequestId/p1%2Fr1%2F512",
			want: PullRequestRef{RepoID: "r1", ID: 512},
			ok:   true,
		},
		{
			name: "decoded slashes, which some responses carry",
			in:   "vstfs:///Git/PullRequestId/p1/r1/512",
			want: PullRequestRef{RepoID: "r1", ID: 512},
			ok:   true,
		},
		{
			name: "a repository id with its own hyphens",
			in:   "vstfs:///Git/PullRequestId/8f3a-11ee%2Fb2c4-44d1%2F9",
			want: PullRequestRef{RepoID: "b2c4-44d1", ID: 9},
			ok:   true,
		},
		{name: "a branch link is not a pull request", in: "vstfs:///Git/Ref/p1%2Fr1%2FGBmain"},
		{name: "a commit link is not a pull request", in: "vstfs:///Git/Commit/p1%2Fr1%2Fabc123"},
		{name: "a non-numeric id", in: "vstfs:///Git/PullRequestId/p1%2Fr1%2Fnotanumber"},
		{name: "too few parts", in: "vstfs:///Git/PullRequestId/p1%2F512"},
		{name: "empty", in: ""},
	} {
		got, ok := ParsePullRequestArtifact(tc.in)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("%s: ref = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestWorkItemPullRequestsReadsTheRelations(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("$expand"); got != "relations" {
			t.Errorf("$expand = %q, want relations — the relations are not returned otherwise", got)
		}
		w.Write([]byte(`{"id":4021,"relations":[
		 {"rel":"ArtifactLink","url":"vstfs:///Git/Ref/p1%2Fr1%2FGBfeature%2F4021","attributes":{"name":"Branch"}},
		 {"rel":"ArtifactLink","url":"vstfs:///Git/PullRequestId/p1%2Fr1%2F512","attributes":{"name":"Pull Request"}},
		 {"rel":"ArtifactLink","url":"vstfs:///Git/PullRequestId/p1%2Fr2%2F513","attributes":{"name":"Pull Request"}},
		 {"rel":"System.LinkTypes.Hierarchy-Forward","url":"https://dev.azure.com/acme/_apis/wit/workItems/4030"}
		]}`))
	})

	refs, err := c.WorkItemPullRequests(4021)
	if err != nil {
		t.Fatalf("WorkItemPullRequests returned %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want the two pull requests and neither the branch nor the child item", len(refs))
	}
	if refs[0] != (PullRequestRef{RepoID: "r1", ID: 512}) || refs[1] != (PullRequestRef{RepoID: "r2", ID: 513}) {
		t.Errorf("refs = %+v", refs)
	}
}

func TestWorkItemPullRequestsOnAnItemWithNoLinks(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":4021}`))
	})

	refs, err := c.WorkItemPullRequests(4021)
	if err != nil {
		t.Fatalf("WorkItemPullRequests returned %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %+v, want none", refs)
	}
}

func TestPullRequestWorkItemIDs(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"id":"4021"},{"id":"3998"}]}`))
	})

	ids, err := c.PullRequestWorkItemIDs("r1", 512)
	if err != nil {
		t.Fatalf("PullRequestWorkItemIDs returned %v", err)
	}
	// The ids come back as strings, which is the wrinkle worth pinning.
	if len(ids) != 2 || ids[0] != 4021 || ids[1] != 3998 {
		t.Errorf("ids = %v, want [4021 3998]", ids)
	}
}

func TestPullRequestWorkItemIDsSkipsAnUnparseableID(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"id":"4021"},{"id":""},{"id":"nope"}]}`))
	})

	ids, err := c.PullRequestWorkItemIDs("r1", 512)
	if err != nil {
		t.Fatalf("PullRequestWorkItemIDs returned %v", err)
	}
	if len(ids) != 1 || ids[0] != 4021 {
		t.Errorf("ids = %v, want just the one that parsed", ids)
	}
}
