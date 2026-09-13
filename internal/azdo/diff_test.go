package azdo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPullRequestIterationsReturnsThemOldestFirst(t *testing.T) {
	var gotPath, gotQuery string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Encode()
		w.Write([]byte(`{"value":[
			{"id":1,"createdDate":"2026-09-10T09:00:00Z"},
			{"id":2,"createdDate":"2026-09-11T09:00:00Z"}
		]}`))
	})

	iterations, err := c.PullRequestIterations("r1", 512)
	if err != nil {
		t.Fatalf("PullRequestIterations returned %v", err)
	}

	if want := "/acme/Platform/_apis/git/repositories/r1/pullRequests/512/iterations"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotQuery, "api-version="+APIVersion) {
		t.Errorf("query = %q, want the pinned api-version", gotQuery)
	}
	if len(iterations) != 2 {
		t.Fatalf("got %d iterations, want 2", len(iterations))
	}
	// The last one is what the view diffs against, so the order the endpoint
	// returned has to survive.
	if iterations[len(iterations)-1].ID != 2 {
		t.Errorf("last iteration = %d, want 2 — the newest must stay last", iterations[1].ID)
	}
	if iterations[0].Created.IsZero() {
		t.Error("createdDate did not decode")
	}
}

func TestIterationChangesDropsFolders(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// changeEntries, not the value array every other list endpoint uses.
		w.Write([]byte(`{"changeEntries":[
			{"changeType":"edit","item":{"path":"/internal/ui/logs.go","isFolder":false}},
			{"changeType":"add","item":{"path":"/internal/diff","isFolder":true}},
			{"changeType":"add","item":{"path":"/internal/diff/diff.go","isFolder":false}},
			{"changeType":"delete","item":{"path":"/old.go","isFolder":false}}
		]}`))
	})

	changes, err := c.IterationChanges("r1", 512, 2)
	if err != nil {
		t.Fatalf("IterationChanges returned %v", err)
	}

	if want := "/acme/Platform/_apis/git/repositories/r1/pullRequests/512/iterations/2/changes"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want the three files without the folder: %+v", len(changes), changes)
	}
	for _, ch := range changes {
		if ch.IsFolder {
			t.Errorf("%q came through as a folder; folders have nothing to diff", ch.Path)
		}
	}
	if changes[0].Path != "/internal/ui/logs.go" || changes[0].ChangeType != "edit" {
		t.Errorf("first change = %+v, want the edited file", changes[0])
	}
}

func TestIterationChangesOnAPullRequestTouchingNothing(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"changeEntries":[]}`))
	})

	changes, err := c.IterationChanges("r1", 512, 1)
	if err != nil {
		t.Fatalf("IterationChanges returned %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("got %d changes, want none", len(changes))
	}
}

// TestFileAtCommitReadsTheJSONContentField is the test that settled how this
// endpoint is read. $format=json is asked for explicitly and the file comes out
// of the envelope's content field, rather than taking the body as plain text
// through getText — see FileAtCommit for why.
func TestFileAtCommitReadsTheJSONContentField(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Write([]byte(`{"objectId":"abc","path":"/internal/ui/logs.go","content":"package ui\n\nfunc main() {}\n"}`))
	})

	text, err := c.FileAtCommit("r1", "/internal/ui/logs.go", "deadbeef")
	if err != nil {
		t.Fatalf("FileAtCommit returned %v", err)
	}

	if want := "/acme/Platform/_apis/git/repositories/r1/items"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	for key, want := range map[string]string{
		"path":                          "/internal/ui/logs.go",
		"versionDescriptor.version":     "deadbeef",
		"versionDescriptor.versionType": "commit",
		"includeContent":                "true",
		"$format":                       "json",
		"api-version":                   APIVersion,
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
	if want := "package ui\n\nfunc main() {}\n"; text != want {
		t.Errorf("content = %q, want %q", text, want)
	}
}

func TestFileAtCommitEscapesThePathAndTheCommit(t *testing.T) {
	// A path with a space and an ampersand would otherwise end the query
	// parameter early and silently fetch a different file.
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"content":""}`))
	})

	if _, err := c.FileAtCommit("r1", "/docs/release notes & plans.md", "abc/def"); err != nil {
		t.Fatalf("FileAtCommit returned %v", err)
	}
	if want := "/docs/release notes & plans.md"; got.Get("path") != want {
		t.Errorf("path = %q, want %q", got.Get("path"), want)
	}
	if want := "abc/def"; got.Get("versionDescriptor.version") != want {
		t.Errorf("version = %q, want %q", got.Get("versionDescriptor.version"), want)
	}
}

func TestFileAtCommitReportsAFailure(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"TF401174: the path does not exist"}`))
	})

	if _, err := c.FileAtCommit("r1", "/gone.go", "abc"); err == nil {
		t.Fatal("a missing file returned no error")
	} else if !strings.Contains(err.Error(), "TF401174") {
		t.Errorf("error = %q, want the Azure DevOps message in it", err)
	}
}

func TestPullRequestsCarryTheMergeCommits(t *testing.T) {
	// Without these two the diff view has nothing to read a file at.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{
			"pullRequestId":512,"title":"Retry webhooks","status":"active",
			"repository":{"id":"r1","name":"platform-api"},
			"lastMergeSourceCommit":{"commitId":"1111111"},
			"lastMergeTargetCommit":{"commitId":"2222222"}
		}]}`))
	})

	prs, err := c.PullRequests()
	if err != nil {
		t.Fatalf("PullRequests returned %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d pull requests, want 1", len(prs))
	}
	if prs[0].SourceCommit != "1111111" {
		t.Errorf("SourceCommit = %q, want the lastMergeSourceCommit", prs[0].SourceCommit)
	}
	if prs[0].TargetCommit != "2222222" {
		t.Errorf("TargetCommit = %q, want the lastMergeTargetCommit", prs[0].TargetCommit)
	}
}

func TestPullRequestFileURLNamesTheFile(t *testing.T) {
	c := &Client{Org: "acme", Project: "Platform"}

	got := c.PullRequestFileURL("platform-api", 512, "/internal/ui/logs.go")
	if !strings.HasPrefix(got, "https://dev.azure.com/acme/Platform/_git/platform-api/pullrequest/512") {
		t.Errorf("url = %q, want it to start at the pull request", got)
	}
	if !strings.Contains(got, "path=%2Finternal%2Fui%2Flogs.go") {
		t.Errorf("url = %q, want the file path escaped into the query", got)
	}
}
