package azdo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRepoFromRemote(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"https form", "https://acme@dev.azure.com/acme/Platform/_git/platform-api", "platform-api"},
		{"https without the user", "https://dev.azure.com/acme/Platform/_git/platform-api", "platform-api"},
		{"https with a .git suffix", "https://dev.azure.com/acme/Platform/_git/platform-api.git", "platform-api"},
		{"https with a trailing slash", "https://dev.azure.com/acme/Platform/_git/platform-api/", "platform-api"},
		{"ssh form", "git@ssh.dev.azure.com:v3/acme/Platform/platform-api", "platform-api"},
		{"ssh url form", "ssh://acme@vs-ssh.visualstudio.com:22/acme/_ssh/platform-api", "platform-api"},
		{"legacy visualstudio.com", "https://acme.visualstudio.com/Platform/_git/platform-api", "platform-api"},
		{"a github remote is not ours", "git@github.com:JacobAtchley/boardwalk.git", ""},
		{"empty", "", ""},
	} {
		if got := RepoFromRemote(tc.in); got != tc.want {
			t.Errorf("%s: RepoFromRemote(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestReposParsesTheListing(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/_apis/git/repositories") {
			t.Errorf("path = %q, want the repositories endpoint", r.URL.Path)
		}
		w.Write([]byte(`{"value":[
			{"id":"r1","name":"platform-api","defaultBranch":"refs/heads/main","project":{"id":"p1"}},
			{"id":"r2","name":"platform-web","defaultBranch":"refs/heads/develop","project":{"id":"p1"}}
		]}`))
	})

	repos, err := c.Repos()
	if err != nil {
		t.Fatalf("Repos returned %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0] != (Repo{ID: "r1", Name: "platform-api", ProjectID: "p1", DefaultBranch: "refs/heads/main"}) {
		t.Errorf("first repo = %+v", repos[0])
	}
}

func TestRefHeadReturnsTheObjectID(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter"); got != "heads/main" {
			t.Errorf("filter = %q, want heads/main", got)
		}
		w.Write([]byte(`{"value":[{"name":"refs/heads/main","objectId":"abc123"}]}`))
	})

	sha, err := c.RefHead("r1", "refs/heads/main")
	if err != nil {
		t.Fatalf("RefHead returned %v", err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want abc123", sha)
	}
}

func TestRefHeadMatchesTheNameExactly(t *testing.T) {
	// The refs endpoint's filter is a starts-with match, so filter=heads/main
	// also returns refs/heads/main-hotfix — in unspecified order. Taking the
	// first entry would branch a repo off whichever ref happened to come back
	// first, silently using the wrong commit.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
			{"name":"refs/heads/main-hotfix","objectId":"wrong1"},
			{"name":"refs/heads/main-2","objectId":"wrong2"},
			{"name":"refs/heads/main","objectId":"right"}
		]}`))
	})

	sha, err := c.RefHead("r1", "refs/heads/main")
	if err != nil {
		t.Fatalf("RefHead returned %v", err)
	}
	if sha != "right" {
		t.Errorf("sha = %q, want the exactly matching ref's commit", sha)
	}
}

func TestRefHeadOnABranchNameWithASlash(t *testing.T) {
	// Branch names carry the type prefix boardwalk generates, so the common
	// case has a slash in it and has to survive both the query escaping and
	// the exact-name comparison.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter"); got != "heads/feature/4021-retry" {
			t.Errorf("filter = %q, want heads/feature/4021-retry", got)
		}
		w.Write([]byte(`{"value":[{"name":"refs/heads/feature/4021-retry","objectId":"abc123"}]}`))
	})

	sha, err := c.RefHead("r1", "refs/heads/feature/4021-retry")
	if err != nil {
		t.Fatalf("RefHead returned %v", err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want abc123", sha)
	}
}

func TestRefHeadOnAPrefixOnlyMatch(t *testing.T) {
	// A filter that matches only siblings is a miss, not a hit on the first
	// one returned.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"name":"refs/heads/main-hotfix","objectId":"wrong1"}]}`))
	})

	if _, err := c.RefHead("r1", "refs/heads/main"); err == nil {
		t.Fatal("RefHead returned no error for a ref only a sibling matched")
	}
}

func TestRefHeadOnAMissingBranch(t *testing.T) {
	// A filter that matches nothing comes back 200 with an empty list, so the
	// error has to be synthesised rather than read off the status code.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})

	if _, err := c.RefHead("r1", "refs/heads/nope"); err == nil {
		t.Fatal("RefHead on a missing branch returned no error")
	}
}

func TestCreateBranchPostsAZeroOldObjectID(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"value":[{"success":true}]}`))
	})

	if err := c.CreateBranch("r1", "feature/4021-retry", "abc123"); err != nil {
		t.Fatalf("CreateBranch returned %v", err)
	}
	for _, want := range []string{
		`"name":"refs/heads/feature/4021-retry"`,
		`"oldObjectId":"0000000000000000000000000000000000000000"`,
		`"newObjectId":"abc123"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestMergeRefPullRequestID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ref    string
		wantID int
		wantOK bool
	}{
		{"a merge ref", "refs/pull/512/merge", 512, true},
		{"a single digit id", "refs/pull/7/merge", 7, true},
		{"a plain branch ref", "refs/heads/feature/4021-retry", 0, false},
		{"a branch ref that merely contains pull", "refs/heads/pull/512/merge", 0, false},
		{"missing the merge suffix", "refs/pull/512", 0, false},
		{"a non-numeric id", "refs/pull/abc/merge", 0, false},
		{"a zero id", "refs/pull/0/merge", 0, false},
		{"empty", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := MergeRefPullRequestID(tc.ref)
			if id != tc.wantID || ok != tc.wantOK {
				t.Errorf("MergeRefPullRequestID(%q) = (%d, %v), want (%d, %v)", tc.ref, id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestCreateBranchSurfacesAPerRefFailure(t *testing.T) {
	// Ref creation answers 200 even when the ref was rejected. The per-ref
	// success flag is the only signal.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[{"success":false,"customMessage":"branch already exists"}]}`))
	})

	err := c.CreateBranch("r1", "feature/dup", "abc123")
	if err == nil {
		t.Fatal("CreateBranch on a rejected ref returned no error")
	}
	if !strings.Contains(err.Error(), "branch already exists") {
		t.Errorf("error = %q, want the custom message", err)
	}
}
