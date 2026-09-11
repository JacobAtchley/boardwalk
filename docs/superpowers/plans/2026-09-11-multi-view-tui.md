# Multi-View TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn boardwalk from a single work item browser into a three-view Azure DevOps terminal client — work items, pull requests, and pipeline builds — reached from an ASCII banner menu, with branch creation, state changes, and branch association performed in process.

**Architecture:** A `Root` model owns an ASCII banner menu, a stack of child views, and all screen chrome. Child views satisfy a five-method `View` interface and are built on a shared list-plus-detail scaffold (`Browser`) so the fuzzy filter, the copy and open actions, and the pane split exist once. The `azdo` package grows one file per Azure DevOps resource area behind a common HTTP helper layer.

**Tech Stack:** Go 1.27, bubbletea 1.3, bubbles 1.0, lipgloss 1.1. Azure DevOps REST 7.1. Tokens from the `az` CLI. Tests are stdlib `testing` plus `net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-09-11-multi-view-tui-design.md`

## Global Constraints

- Go module is `github.com/JacobAtchley/boardwalk`. Internal packages are `internal/azdo` and `internal/ui`.
- `azdo.APIVersion` is pinned to `"7.1"`. Only the work item comments endpoint may deviate, using `"7.1-preview.3"`.
- boardwalk implements no auth flow. Every token comes from `az account get-access-token`.
- No new third-party dependencies. Everything ships with bubbletea, bubbles, lipgloss, or the standard library.
- Every exported identifier carries a doc comment. Comments explain why, not what — match the density and voice of the existing files.
- Tests never make real network calls. Either the function under test is pure, or the test stands up an `httptest.Server`.
- Test-first throughout: write the failing test, watch it fail, write the minimal implementation, watch it pass, commit.
- Run `make test` and `make vet` before every commit. Both must be clean.
- No fetch failure may exit the program. Errors render on the status line.

---

### Task 1: HTTP transport helpers

`client.go` today only has `post`. Every new endpoint needs `GET`, the branch
flow needs `PATCH` with a non-standard content type, and build logs come back as
plain text rather than JSON. All four go through one `do`.

**Files:**
- Modify: `internal/azdo/client.go:73-108`
- Create: `internal/azdo/client_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func (c *Client) get(url string, out any) error`
  - `func (c *Client) getText(url string) (string, error)`
  - `func (c *Client) post(url string, body, out any) error` (unchanged signature)
  - `func (c *Client) patch(url, contentType string, body, out any) error`
  - Test helper `func testClient(t *testing.T, h http.HandlerFunc) *Client` in `client_test.go`, reused by every later `azdo` test.

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/client_test.go`:

```go
package azdo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClient points a Client at a stub server. Every azdo test that needs a
// round trip builds its client this way rather than reaching the network.
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{
		Org:     "acme",
		Project: "Platform",
		Me:      "dev@acme.test",
		token:   "test-token",
		http:    &http.Client{Timeout: 5 * time.Second},
		baseURL: srv.URL,
	}
}

func TestGetDecodesJSONAndSendsTheToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want the bearer token", got)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Write([]byte(`{"count":2}`))
	})

	var out struct {
		Count int `json:"count"`
	}
	if err := c.get(c.baseURL+"/thing", &out); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

func TestGetTextReturnsTheBodyVerbatim(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("line one\nline two\n"))
	})

	got, err := c.getText(c.baseURL + "/log")
	if err != nil {
		t.Fatalf("getText returned %v", err)
	}
	if got != "line one\nline two\n" {
		t.Errorf("getText = %q, want the body unchanged", got)
	}
}

func TestPatchSendsTheGivenContentType(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json-patch+json" {
			t.Errorf("Content-Type = %q, want application/json-patch+json", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"System.State"`) {
			t.Errorf("body = %q, want the patch document", body)
		}
		w.Write([]byte(`{"id":7}`))
	})

	patch := []map[string]any{{"op": "add", "path": "/fields/System.State", "value": "Active"}}
	var out struct {
		ID int `json:"id"`
	}
	if err := c.patch(c.baseURL+"/wi/7", "application/json-patch+json", patch, &out); err != nil {
		t.Fatalf("patch returned %v", err)
	}
	if out.ID != 7 {
		t.Errorf("id = %d, want 7", out.ID)
	}
}

func TestErrorsCarryTheAzureMessage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"TF401019: repository does not exist"}`))
	})

	err := c.get(c.baseURL+"/missing", &struct{}{})
	if err == nil {
		t.Fatal("get on a 404 returned no error")
	}
	if !strings.Contains(err.Error(), "TF401019") {
		t.Errorf("error = %q, want the Azure DevOps message in it", err)
	}
}

func TestDiscardingTheBodyIsAllowed(t *testing.T) {
	// The branch flow's ref creation has a response nobody reads.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})
	if err := c.get(c.baseURL+"/thing", nil); err != nil {
		t.Fatalf("get with a nil out returned %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestGet|TestPatch|TestErrors|TestDiscarding' -v`

Expected: compile failure — `c.baseURL undefined`, `c.get undefined`, `c.getText undefined`, `c.patch undefined`.

- [ ] **Step 3: Add the baseURL field**

In `internal/azdo/client.go`, add to the `Client` struct below `http`:

```go
	// baseURL overrides https://dev.azure.com in tests. Empty in production.
	baseURL string
}
```

And add the accessor every endpoint builder will use:

```go
// root is the API host. Tests point it at an httptest server; everything else
// talks to Azure DevOps.
func (c *Client) root() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://dev.azure.com"
}
```

- [ ] **Step 4: Replace post with the four helpers over one do**

Replace the body of `post` in `internal/azdo/client.go` with:

```go
func (c *Client) get(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// getText fetches a response that is not JSON. Build logs come back as plain
// text, so decoding them would fail.
func (c *Client) getText(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := c.send(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func (c *Client) post(url string, body, out any) error {
	req, err := jsonRequest(http.MethodPost, url, "application/json", body)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// patch takes its content type explicitly because work item updates use
// application/json-patch+json, which the rest of the API does not.
func (c *Client) patch(url, contentType string, body, out any) error {
	req, err := jsonRequest(http.MethodPatch, url, contentType, body)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func jsonRequest(method, url, contentType string, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return req, nil
}

// do runs a request and decodes its JSON body. A nil out discards the body,
// which suits the calls made only for their side effect.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// send attaches credentials and turns any non-2xx into an error carrying the
// message Azure DevOps put in the body.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", resp.Status, apiError(resp.Body))
	}
	return resp, nil
}
```

Add `"io"` to the import block. Change `apiError`'s parameter type from the
inline interface to `io.Reader`.

- [ ] **Step 5: Point NewClient at the real host**

In `NewClient`, nothing changes — `baseURL` stays empty. Confirm `workitems.go`
still compiles: its two `fmt.Sprintf` endpoint builders hardcode
`https://dev.azure.com`. Replace both with `c.root()`:

```go
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/wiql?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), APIVersion)
```

```go
	endpoint := fmt.Sprintf("%s/%s/_apis/wit/workitemsbatch?api-version=%s",
		c.root(), url.PathEscape(c.Org), APIVersion)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `make test && make vet`

Expected: PASS, no vet output.

- [ ] **Step 7: Commit**

```bash
git add internal/azdo/client.go internal/azdo/client_test.go internal/azdo/workitems.go
git commit -m "refactor(azdo): route every request through one transport helper"
```

---

### Task 2: Display formatting helpers

Three pure functions the views share. They live in `ui` because they are about
presentation, not about Azure DevOps.

**Files:**
- Create: `internal/ui/format.go`
- Create: `internal/ui/format_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func humanAge(then, now time.Time) string`
  - `func shortRef(ref string) string`
  - `func branchName(kind string, id int, title string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/format_test.go`:

```go
package ui

import (
	"testing"
	"time"
)

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		then time.Time
		want string
	}{
		{"seconds read as a minute", now.Add(-30 * time.Second), "1m"},
		{"minutes", now.Add(-42 * time.Minute), "42m"},
		{"hours", now.Add(-5 * time.Hour), "5h"},
		{"just under a day is hours", now.Add(-23 * time.Hour), "23h"},
		{"days", now.AddDate(0, 0, -3), "3d"},
		{"a week becomes weeks", now.AddDate(0, 0, -7), "1w"},
		{"weeks", now.AddDate(0, 0, -20), "2w"},
		{"a year", now.AddDate(-1, 0, 0), "1y"},
		{"the future clamps to now", now.Add(time.Hour), "1m"},
		{"a zero time is unknown", time.Time{}, "-"},
	} {
		if got := humanAge(tc.then, now); got != tc.want {
			t.Errorf("%s: humanAge = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestShortRef(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"refs/heads/main", "main"},
		{"refs/heads/feature/4021-retry", "feature/4021-retry"},
		{"refs/pull/512/merge", "refs/pull/512/merge"},
		{"main", "main"},
		{"", "-"},
	} {
		if got := shortRef(tc.in); got != tc.want {
			t.Errorf("shortRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBranchName(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		id    int
		title string
		want  string
	}{
		{"User Story", 4021, "Retry webhook delivery on 5xx", "feature/4021-retry-webhook-delivery-on-5xx"},
		{"Bug", 77, "Login 500s", "bugfix/77-login-500s"},
		{"Defect", 78, "Crash on save", "bugfix/78-crash-on-save"},
		{"Task", 90, "Bump deps", "task/90-bump-deps"},
		{"Enhancement", 91, "Tidy copy", "feature/91-tidy-copy"},
		{"Feature", 92, "  Multiple   spaces  ", "feature/92-multiple-spaces"},
		{"Bug", 93, "Punctuation: it's, / broken!", "bugfix/93-punctuation-it-s-broken"},
		{"Bug", 94, "", "bugfix/94"},
		{"Bug", 95, "A title so long that it has to be cut somewhere sensible rather than running on", "bugfix/95-a-title-so-long-that-it-has-to-be-cut-somewhere"},
	} {
		if got := branchName(tc.kind, tc.id, tc.title); got != tc.want {
			t.Errorf("branchName(%q, %d, %q) = %q, want %q", tc.kind, tc.id, tc.title, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestHumanAge|TestShortRef|TestBranchName' -v`

Expected: compile failure — the three functions are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/ui/format.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"time"
)

// slugMax caps the title half of a generated branch name. Long titles make
// unwieldy branches, and the work item id already identifies the branch.
const slugMax = 50

// humanAge renders an elapsed duration in the single largest unit that fits, so
// a column stays narrow. now is a parameter rather than time.Now so the tests
// are not clock-dependent.
func humanAge(then, now time.Time) string {
	if then.IsZero() {
		return "-"
	}

	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// shortRef trims the refs/heads prefix off a branch ref. Anything else — a pull
// request merge ref, say — is left alone rather than mangled.
func shortRef(ref string) string {
	if ref == "" {
		return "-"
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

// branchName builds the branch a work item's branch flow proposes. The user can
// edit it before it is created, so this only has to be a good default.
func branchName(kind string, id int, title string) string {
	prefix := "feature"
	switch strings.ToLower(kind) {
	case "bug", "defect", "issue":
		prefix = "bugfix"
	case "task":
		prefix = "task"
	}

	slug := slugify(title)
	if slug == "" {
		return fmt.Sprintf("%s/%d", prefix, id)
	}
	return fmt.Sprintf("%s/%d-%s", prefix, id, slug)
}

// slugify lowercases a title and keeps only characters git is happy with in a
// ref, collapsing every run of anything else into a single hyphen.
func slugify(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	slug := b.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	if len(slug) > slugMax {
		slug = slug[:slugMax]
		// Cutting mid-word reads as a typo, so back up to the last boundary.
		if i := strings.LastIndex(slug, "-"); i > 0 {
			slug = slug[:i]
		}
	}
	return strings.Trim(slug, "-")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestHumanAge|TestShortRef|TestBranchName' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/format.go internal/ui/format_test.go
git commit -m "feat(ui): add age, ref and branch name formatting"
```

---

### Task 3: Repositories, refs, and branch creation

**Files:**
- Create: `internal/azdo/git.go`
- Create: `internal/azdo/git_test.go`

**Interfaces:**
- Consumes: `c.get`, `c.post`, `c.root()` from Task 1.
- Produces:
  - `type Repo struct { ID, Name, ProjectID, DefaultBranch string }`
  - `func (c *Client) Repos() ([]Repo, error)`
  - `func (c *Client) RefHead(repoID, ref string) (string, error)`
  - `func (c *Client) CreateBranch(repoID, branch, fromSHA string) error`
  - `func RepoFromRemote(remote string) string`
  - `func CurrentRepo() string`

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/git_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestRepo|TestRefHead|TestCreateBranch' -v`

Expected: compile failure — `Repo`, `Repos`, `RefHead`, `CreateBranch`, `RepoFromRemote` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/azdo/git.go`:

```go
package azdo

import (
	"fmt"
	"net/url"
	"os/exec"
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
	if len(resp.Value) == 0 {
		return "", fmt.Errorf("no ref matching %s", ref)
	}
	return resp.Value[0].ObjectID, nil
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/azdo/git.go internal/azdo/git_test.go
git commit -m "feat(azdo): add repositories, refs and branch creation"
```

---

### Task 4: Work item state changes and branch association

**Files:**
- Create: `internal/azdo/update.go`
- Create: `internal/azdo/update_test.go`

**Interfaces:**
- Consumes: `c.patch`, `c.root()` from Task 1; `Repo` from Task 3.
- Produces:
  - `func (c *Client) SetState(id int, state string) error`
  - `func (c *Client) LinkBranch(id int, projectID, repoID, branch string) error`
  - `func BranchArtifactURL(projectID, repoID, branch string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/update_test.go`:

```go
package azdo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBranchArtifactURL(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		project, repo, branch  string
		want                   string
	}{
		{
			name: "a flat branch name",
			project: "p1", repo: "r1", branch: "main",
			want: "vstfs:///Git/Ref/p1%2Fr1%2FGBmain",
		},
		{
			// The separators and the slashes inside the branch are both encoded,
			// which is why the branch cannot simply be concatenated.
			name: "a branch containing a slash",
			project: "p1", repo: "r1", branch: "feature/4021-retry",
			want: "vstfs:///Git/Ref/p1%2Fr1%2FGBfeature%2F4021-retry",
		},
	} {
		if got := BranchArtifactURL(tc.project, tc.repo, tc.branch); got != tc.want {
			t.Errorf("%s: BranchArtifactURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSetStateSendsAJSONPatch(t *testing.T) {
	var body, contentType string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.SetState(4021, "Active"); err != nil {
		t.Fatalf("SetState returned %v", err)
	}
	if contentType != "application/json-patch+json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	for _, want := range []string{`"op":"add"`, `"/fields/System.State"`, `"Active"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestLinkBranchAppendsAnArtifactRelation(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.LinkBranch(4021, "p1", "r1", "feature/4021-retry"); err != nil {
		t.Fatalf("LinkBranch returned %v", err)
	}
	for _, want := range []string{
		`"/relations/-"`,
		`"rel":"ArtifactLink"`,
		`vstfs:///Git/Ref/p1%2Fr1%2FGBfeature%2F4021-retry`,
		`"name":"Branch"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestSetStateReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"TF401320: rule error"}`))
	})

	err := c.SetState(4021, "Active")
	if err == nil {
		t.Fatal("SetState against a refusing server returned no error")
	}
	if !strings.Contains(err.Error(), "TF401320") {
		t.Errorf("error = %q, want the server message", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestBranchArtifact|TestSetState|TestLinkBranch' -v`

Expected: compile failure — the three functions are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/azdo/update.go`:

```go
package azdo

import (
	"fmt"
	"net/url"
)

// patchContentType is what the work item update endpoint requires. Sending
// application/json instead gets a 400 with no useful detail.
const patchContentType = "application/json-patch+json"

// operation is one JSON Patch step against a work item.
type operation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// SetState moves a work item to a new state, for example Active.
func (c *Client) SetState(id int, state string) error {
	return c.update(id, []operation{{
		Op:    "add",
		Path:  "/fields/System.State",
		Value: state,
	}})
}

// LinkBranch adds a branch to the work item's Development section, which is
// what Azure DevOps shows when a branch is associated with an item.
func (c *Client) LinkBranch(id int, projectID, repoID, branch string) error {
	return c.update(id, []operation{{
		Op:   "add",
		Path: "/relations/-",
		Value: map[string]any{
			"rel":        "ArtifactLink",
			"url":        BranchArtifactURL(projectID, repoID, branch),
			"attributes": map[string]string{"name": "Branch"},
		},
	}})
}

func (c *Client) update(id int, ops []operation) error {
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workitems/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	return c.patch(endpoint, patchContentType, ops, nil)
}

// BranchArtifactURL builds the vstfs identifier Azure DevOps uses for a branch.
// The three parts are joined with encoded slashes, and the branch name's own
// slashes are encoded the same way, so a nested branch does not read as extra
// path segments.
func BranchArtifactURL(projectID, repoID, branch string) string {
	return fmt.Sprintf("vstfs:///Git/Ref/%s%%2F%s%%2FGB%s",
		url.PathEscape(projectID), url.PathEscape(repoID), url.PathEscape(branch))
}
```

Note `url.PathEscape` escapes `/` as `%2F`, which is exactly what the branch
half needs.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/azdo/update.go internal/azdo/update_test.go
git commit -m "feat(azdo): add work item state changes and branch links"
```

---

### Task 5: Acceptance criteria and work item discussion

**Files:**
- Modify: `internal/azdo/workitems.go:11-30` (struct), `:129-146` (batch fields), `:147-176` (decode)
- Create: `internal/azdo/comments.go`
- Create: `internal/azdo/comments_test.go`

**Interfaces:**
- Consumes: `c.get`, `c.root()` from Task 1; `StripHTML` from the existing `workitems.go`.
- Produces:
  - `WorkItem` gains `AcceptanceCriteria string`
  - `type Comment struct { Author string; Created time.Time; Text string }`
  - `func (c *Client) Comments(id int) ([]Comment, error)`
  - `const CommentsAPIVersion = "7.1-preview.3"`

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/comments_test.go`:

```go
package azdo

import (
	"net/http"
	"strings"
	"testing"
)

func TestCommentsParsesAndStripsHTML(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-version"); got != CommentsAPIVersion {
			t.Errorf("api-version = %q, want %q", got, CommentsAPIVersion)
		}
		w.Write([]byte(`{"count":2,"comments":[
			{"id":1,"text":"<div>Looks good &amp; shipped</div>","createdBy":{"displayName":"Dev Example"},"createdDate":"2026-09-10T09:00:00Z"},
			{"id":2,"text":"<p>Second</p>","createdBy":{"displayName":"Other Dev"},"createdDate":"2026-09-11T10:30:00Z"}
		]}`))
	})

	comments, err := c.Comments(4021)
	if err != nil {
		t.Fatalf("Comments returned %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	if comments[0].Text != "Looks good & shipped" {
		t.Errorf("text = %q, want the HTML stripped", comments[0].Text)
	}
	if comments[0].Author != "Dev Example" {
		t.Errorf("author = %q", comments[0].Author)
	}
	if comments[0].Created.IsZero() {
		t.Error("createdDate did not parse")
	}
}

func TestCommentsOnAnItemWithNone(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":0,"comments":[]}`))
	})

	comments, err := c.Comments(4021)
	if err != nil {
		t.Fatalf("Comments returned %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("got %d comments, want none", len(comments))
	}
}

func TestCommentsSurfacesAnError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"access denied"}`))
	})

	if _, err := c.Comments(4021); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("error = %v, want the server message", err)
	}
}
```

Add to `internal/azdo/workitems_test.go`:

```go
func TestBatchFieldsIncludeAcceptanceCriteria(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"value":[{"id":4021,"fields":{
			"System.Title":"Retry webhooks",
			"Microsoft.VSTS.Common.AcceptanceCriteria":"<ul><li>Retries three times</li></ul>"
		}}]}`))
	})

	items, err := c.batch([]int{4021})
	if err != nil {
		t.Fatalf("batch returned %v", err)
	}
	if !strings.Contains(body, "Microsoft.VSTS.Common.AcceptanceCriteria") {
		t.Error("the batch request did not ask for acceptance criteria")
	}
	if items[0].AcceptanceCriteria != "Retries three times" {
		t.Errorf("acceptance criteria = %q, want the HTML stripped", items[0].AcceptanceCriteria)
	}
}
```

Add `"io"`, `"net/http"` and `"strings"` to `workitems_test.go`'s imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestComments|TestBatchFields' -v`

Expected: compile failure — `CommentsAPIVersion`, `Comments`, and the
`AcceptanceCriteria` field are undefined.

- [ ] **Step 3: Add acceptance criteria to the work item fetch**

In `internal/azdo/workitems.go`, add to the `WorkItem` struct after
`Description`:

```go
	AcceptanceCriteria string
```

In `batch`, add the field name to the `"fields"` slice:

```go
		"fields": []string{
			"System.Id", "System.Title", "System.WorkItemType", "System.State",
			"System.AssignedTo", "System.Tags", "System.IterationPath", "System.Description",
			"Microsoft.VSTS.Common.AcceptanceCriteria",
		},
```

Add to the anonymous `Fields` struct:

```go
				AcceptanceCriteria string `json:"Microsoft.VSTS.Common.AcceptanceCriteria"`
```

And to the `WorkItem` literal built in the decode loop:

```go
			AcceptanceCriteria: StripHTML(v.Fields.AcceptanceCriteria),
```

- [ ] **Step 4: Write the comments fetch**

Create `internal/azdo/comments.go`:

```go
package azdo

import (
	"fmt"
	"net/url"
	"time"
)

// CommentsAPIVersion overrides the pinned version for this one endpoint. Work
// item comments have never left preview, and 7.1 alone returns a 404.
const CommentsAPIVersion = "7.1-preview.3"

// Comment is one entry in a work item's discussion.
type Comment struct {
	Author  string
	Created time.Time
	Text    string
}

// Comments returns a work item's discussion, oldest first, which is the order
// the API answers in and the order a conversation reads in.
func (c *Client) Comments(id int) ([]Comment, error) {
	var resp struct {
		Comments []struct {
			Text      string    `json:"text"`
			Created   time.Time `json:"createdDate"`
			CreatedBy struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
		} `json:"comments"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workItems/%d/comments?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, CommentsAPIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	comments := make([]Comment, 0, len(resp.Comments))
	for _, v := range resp.Comments {
		comments = append(comments, Comment{
			Author:  v.CreatedBy.DisplayName,
			Created: v.Created,
			Text:    StripHTML(v.Text),
		})
	}
	return comments, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/azdo/comments.go internal/azdo/comments_test.go internal/azdo/workitems.go internal/azdo/workitems_test.go
git commit -m "feat(azdo): fetch acceptance criteria and work item discussion"
```

---

### Task 6: Pull requests and comment threads

**Files:**
- Create: `internal/azdo/pullrequests.go`
- Create: `internal/azdo/pullrequests_test.go`

**Interfaces:**
- Consumes: `c.get`, `c.root()` from Task 1; `StripHTML` from `workitems.go`.
- Produces:
  - `type Reviewer struct { Name string; Vote int }`
  - `type PullRequest struct { ID int; Title, Repo, RepoID, Author, AuthorKey, Source, Target, Description string; IsDraft bool; Created time.Time; Reviewers []Reviewer }`
  - `type OpenThread struct { Author, Text string }`
  - `type ThreadCounts struct { Resolved, Unresolved int; Open []OpenThread }`
  - `func (c *Client) PullRequests() ([]PullRequest, error)`
  - `func (c *Client) Threads(repoID string, prID int) (ThreadCounts, error)`
  - `func (c *Client) PullRequestURL(repo string, id int) string`
  - `func (r Reviewer) VoteLabel() string`

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/pullrequests_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestPullRequest|TestVoteLabel|TestThreads' -v`

Expected: compile failure — the types and methods are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/azdo/pullrequests.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/azdo/pullrequests.go internal/azdo/pullrequests_test.go
git commit -m "feat(azdo): list pull requests and summarise their threads"
```

---

### Task 7: Builds and timelines

**Files:**
- Create: `internal/azdo/builds.go`
- Create: `internal/azdo/builds_test.go`

**Interfaces:**
- Consumes: `c.get`, `c.root()` from Task 1.
- Produces:
  - `type BuildStatus int` with constants `StatusQueued`, `StatusRunning`, `StatusSucceeded`, `StatusFailed`, `StatusPartial`, `StatusCanceled`, and `func (s BuildStatus) String() string`, `func (s BuildStatus) Done() bool`
  - `type Build struct { ID int; Number, Pipeline, RequestedFor, SourceBranch string; Status BuildStatus; Queued time.Time }`
  - `type Record struct { Name, Type, State, Result string; Order, LogID, ErrorCount int }`
  - `type Progress struct { CurrentStep string; Errors int }`
  - `func classify(status, result string) BuildStatus`
  - `func summarize(records []Record, s BuildStatus) Progress`
  - `func (c *Client) Builds(top int) ([]Build, error)`
  - `func (c *Client) BuildByID(id int) (Build, error)`
  - `func (c *Client) Timeline(buildID int) (Progress, []Record, error)`
  - `func (c *Client) BuildURL(id int) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/azdo/builds_test.go`:

```go
package azdo

import (
	"net/http"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		status, result string
		want           BuildStatus
	}{
		{"notStarted", "", StatusQueued},
		{"postponed", "", StatusQueued},
		{"inProgress", "", StatusRunning},
		{"cancelling", "", StatusRunning},
		{"completed", "succeeded", StatusSucceeded},
		{"completed", "partiallySucceeded", StatusPartial},
		{"completed", "failed", StatusFailed},
		{"completed", "canceled", StatusCanceled},
		{"completed", "", StatusFailed},
		{"", "", StatusQueued},
	} {
		if got := classify(tc.status, tc.result); got != tc.want {
			t.Errorf("classify(%q, %q) = %v, want %v", tc.status, tc.result, got, tc.want)
		}
	}
}

func TestBuildStatusDone(t *testing.T) {
	for s, want := range map[BuildStatus]bool{
		StatusQueued:    false,
		StatusRunning:   false,
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusPartial:   true,
		StatusCanceled:  true,
	} {
		if got := s.Done(); got != want {
			t.Errorf("%v.Done() = %v, want %v", s, got, want)
		}
	}
}

func TestSummarizeRunningBuildNamesTheStepInProgress(t *testing.T) {
	records := []Record{
		{Name: "Restore", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
		{Name: "Build", Type: "Task", State: "inProgress", Order: 2},
		{Name: "Test", Type: "Task", State: "pending", Order: 3},
	}

	got := summarize(records, StatusRunning)
	if got.CurrentStep != "Build" {
		t.Errorf("current step = %q, want Build", got.CurrentStep)
	}
	if got.Errors != 0 {
		t.Errorf("errors = %d, want 0", got.Errors)
	}
}

func TestSummarizeRunningBuildPrefersTheLowestOrder(t *testing.T) {
	// The timeline arrives unordered, and two tasks can be in flight at once.
	records := []Record{
		{Name: "Later", Type: "Task", State: "inProgress", Order: 5},
		{Name: "Earlier", Type: "Task", State: "inProgress", Order: 2},
	}

	if got := summarize(records, StatusRunning); got.CurrentStep != "Earlier" {
		t.Errorf("current step = %q, want Earlier", got.CurrentStep)
	}
}

func TestSummarizeFailedBuildNamesTheFailingStep(t *testing.T) {
	records := []Record{
		{Name: "Restore", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
		{Name: "Test", Type: "Task", State: "completed", Result: "failed", Order: 2, ErrorCount: 3},
		{Name: "Publish", Type: "Task", State: "completed", Result: "skipped", Order: 3},
	}

	got := summarize(records, StatusFailed)
	if got.CurrentStep != "Test" {
		t.Errorf("current step = %q, want Test", got.CurrentStep)
	}
	if got.Errors != 3 {
		t.Errorf("errors = %d, want 3", got.Errors)
	}
}

func TestSummarizeCleanBuildHasNoCurrentStep(t *testing.T) {
	records := []Record{
		{Name: "Build", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
	}

	got := summarize(records, StatusSucceeded)
	if got.CurrentStep != "" {
		t.Errorf("current step = %q, want it empty", got.CurrentStep)
	}
	if got.Errors != 0 {
		t.Errorf("errors = %d, want 0", got.Errors)
	}
}

func TestSummarizeCountsErrorsWithoutAnErrorCount(t *testing.T) {
	// Some tasks fail without reporting a count, so a failed record is worth at
	// least one error.
	records := []Record{
		{Name: "Test", Type: "Task", State: "completed", Result: "failed", Order: 1},
	}

	if got := summarize(records, StatusFailed); got.Errors != 1 {
		t.Errorf("errors = %d, want 1", got.Errors)
	}
}

func TestSummarizeQueuedBuild(t *testing.T) {
	if got := summarize(nil, StatusQueued); got.CurrentStep != "" || got.Errors != 0 {
		t.Errorf("summarize of an empty timeline = %+v, want it empty", got)
	}
}

func TestBuildsParsesTheListing(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("queryOrder"); got != "queueTimeDescending" {
			t.Errorf("queryOrder = %q, want queueTimeDescending", got)
		}
		if got := r.URL.Query().Get("$top"); got != "50" {
			t.Errorf("$top = %q, want 50", got)
		}
		w.Write([]byte(`{"value":[
		 {"id":9001,"buildNumber":"20260911.3","status":"inProgress",
		  "queueTime":"2026-09-11T11:00:00Z","sourceBranch":"refs/heads/main",
		  "definition":{"name":"platform-ci"},"requestedFor":{"displayName":"Dev Example"}},
		 {"id":9000,"buildNumber":"20260911.2","status":"completed","result":"failed",
		  "queueTime":"2026-09-11T10:00:00Z","sourceBranch":"refs/heads/feature/x",
		  "definition":{"name":"platform-web-ci"},"requestedFor":{"displayName":"Other Dev"}}
		]}`))
	})

	builds, err := c.Builds(50)
	if err != nil {
		t.Fatalf("Builds returned %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("got %d builds, want 2", len(builds))
	}
	if builds[0].Pipeline != "platform-ci" || builds[0].Status != StatusRunning {
		t.Errorf("first build = %+v", builds[0])
	}
	if builds[1].Status != StatusFailed {
		t.Errorf("second build status = %v, want failed", builds[1].Status)
	}
}

func TestTimelineSummarisesAndReturnsRecords(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"records":[
		 {"name":"Build","type":"Task","state":"inProgress","order":2,"log":{"id":7}},
		 {"name":"Restore","type":"Task","state":"completed","result":"succeeded","order":1,"log":{"id":6}},
		 {"name":"Job","type":"Job","state":"inProgress","order":1}
		]}`))
	})

	progress, records, err := c.Timeline(9001)
	if err != nil {
		t.Fatalf("Timeline returned %v", err)
	}
	if progress.CurrentStep != "Build" {
		t.Errorf("current step = %q, want Build — a Job is not a step", progress.CurrentStep)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want all 3 returned for the log pane", len(records))
	}
	if records[0].Name != "Restore" {
		t.Errorf("records[0] = %q, want them sorted by order", records[0].Name)
	}
}

func TestBuildURL(t *testing.T) {
	c := &Client{Org: "acme", Project: "Platform"}
	want := "https://dev.azure.com/acme/Platform/_build/results?buildId=9001"
	if got := c.BuildURL(9001); got != want {
		t.Errorf("BuildURL = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestClassify|TestBuild|TestSummarize|TestTimeline' -v`

Expected: compile failure — the types and functions are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/azdo/builds.go`:

```go
package azdo

import (
	"fmt"
	"net/url"
	"sort"
	"time"
)

// BuildStatus collapses Azure DevOps's status and result pair into the single
// value a list column can show.
type BuildStatus int

const (
	StatusQueued BuildStatus = iota
	StatusRunning
	StatusSucceeded
	StatusFailed
	StatusPartial
	StatusCanceled
)

func (s BuildStatus) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusSucceeded:
		return "succeeded"
	case StatusFailed:
		return "failed"
	case StatusPartial:
		return "partial"
	case StatusCanceled:
		return "canceled"
	default:
		return "queued"
	}
}

// Done reports whether the build has stopped changing, which is what tells the
// log pane to stop polling.
func (s BuildStatus) Done() bool {
	return s != StatusQueued && s != StatusRunning
}

// classify folds the API's two fields into one. A completed build with no
// result is treated as failed rather than as a success nobody recorded.
func classify(status, result string) BuildStatus {
	switch status {
	case "inProgress", "cancelling":
		return StatusRunning
	case "completed":
		switch result {
		case "succeeded":
			return StatusSucceeded
		case "partiallySucceeded":
			return StatusPartial
		case "canceled":
			return StatusCanceled
		default:
			return StatusFailed
		}
	default:
		return StatusQueued
	}
}

// Build is one pipeline run.
type Build struct {
	ID           int
	Number       string
	Pipeline     string
	RequestedFor string
	SourceBranch string
	Status       BuildStatus
	Queued       time.Time
}

// Record is one entry in a build's timeline: a stage, a job, or a task.
type Record struct {
	Name       string
	Type       string
	State      string
	Result     string
	Order      int
	LogID      int
	ErrorCount int
}

// Progress is what the build list shows beyond the status glyph.
type Progress struct {
	CurrentStep string
	Errors      int
}

// summarize picks the step worth naming and totals the errors. For a running
// build that is whatever is executing; for a failed one it is what broke; for a
// build that finished clean there is nothing to say.
func summarize(records []Record, s BuildStatus) Progress {
	var p Progress
	var current *Record

	for i := range records {
		r := records[i]
		if r.Result == "failed" {
			if r.ErrorCount > 0 {
				p.Errors += r.ErrorCount
			} else {
				// A task can fail without filling in a count.
				p.Errors++
			}
		}

		// Stages and jobs are containers, and naming one says less than naming
		// the task inside it.
		if r.Type != "Task" {
			continue
		}
		if !worthNaming(r, s) {
			continue
		}
		// Two tasks can be in flight at once, and the timeline is unordered, so
		// the earliest is the one to name.
		if current == nil || r.Order < current.Order {
			current = &records[i]
		}
	}

	if current != nil {
		p.CurrentStep = current.Name
	}
	return p
}

// worthNaming reports whether a task is the one the list should point at: what
// is executing on a running build, what broke on a failed one, and nothing at
// all on a build that finished clean.
func worthNaming(r Record, s BuildStatus) bool {
	if !s.Done() {
		return s == StatusRunning && r.State == "inProgress"
	}
	if s == StatusSucceeded {
		return false
	}
	return r.Result == "failed"
}

// Builds lists the project's most recent pipeline runs, newest first.
func (c *Client) Builds(top int) ([]Build, error) {
	var resp struct {
		Value []buildJSON `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/build/builds?$top=%d&queryOrder=queueTimeDescending&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), top, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	builds := make([]Build, 0, len(resp.Value))
	for _, v := range resp.Value {
		builds = append(builds, v.build())
	}
	return builds, nil
}

// BuildByID refetches one build, which is how the log pane notices that the
// run it is tailing has finished.
func (c *Client) BuildByID(id int) (Build, error) {
	var v buildJSON
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	if err := c.get(endpoint, &v); err != nil {
		return Build{}, err
	}
	return v.build(), nil
}

type buildJSON struct {
	ID           int       `json:"id"`
	Number       string    `json:"buildNumber"`
	Status       string    `json:"status"`
	Result       string    `json:"result"`
	Queued       time.Time `json:"queueTime"`
	SourceBranch string    `json:"sourceBranch"`
	Definition   struct {
		Name string `json:"name"`
	} `json:"definition"`
	RequestedFor struct {
		DisplayName string `json:"displayName"`
	} `json:"requestedFor"`
}

func (v buildJSON) build() Build {
	return Build{
		ID:           v.ID,
		Number:       v.Number,
		Pipeline:     v.Definition.Name,
		RequestedFor: v.RequestedFor.DisplayName,
		SourceBranch: v.SourceBranch,
		Status:       classify(v.Status, v.Result),
		Queued:       v.Queued,
	}
}

// Timeline returns a build's progress summary and its records in execution
// order. The records carry the log ids the log pane concatenates.
func (c *Client) Timeline(buildID int) (Progress, []Record, error) {
	var resp struct {
		Records []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			State      string `json:"state"`
			Result     string `json:"result"`
			Order      int    `json:"order"`
			ErrorCount int    `json:"errorCount"`
			Log        *struct {
				ID int `json:"id"`
			} `json:"log"`
		} `json:"records"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/timeline?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), buildID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return Progress{}, nil, err
	}

	records := make([]Record, 0, len(resp.Records))
	for _, v := range resp.Records {
		r := Record{
			Name:       v.Name,
			Type:       v.Type,
			State:      v.State,
			Result:     v.Result,
			Order:      v.Order,
			ErrorCount: v.ErrorCount,
		}
		if v.Log != nil {
			r.LogID = v.Log.ID
		}
		records = append(records, r)
	}

	// The timeline arrives in no particular order, and both the current-step
	// pick and the log concatenation depend on execution order.
	sort.SliceStable(records, func(i, j int) bool { return records[i].Order < records[j].Order })

	build, err := c.BuildByID(buildID)
	if err != nil {
		return Progress{}, records, err
	}
	return summarize(records, build.Status), records, nil
}

// BuildURL is the browser URL for a build's results page.
func (c *Client) BuildURL(id int) string {
	return fmt.Sprintf("%s/%s/%s/_build/results?buildId=%d",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS. `TestTimelineSummarisesAndReturnsRecords`'s stub server answers
every path with the timeline body, including the `BuildByID` call `Timeline`
makes; that decodes to a zero `buildJSON` whose status classifies as queued,
which is fine for the record-ordering assertions. If the current-step assertion
fails because of it, change the stub to branch on `r.URL.Path` and return
`{"id":9001,"status":"inProgress"}` for the build path.

- [ ] **Step 5: Commit**

```bash
git add internal/azdo/builds.go internal/azdo/builds_test.go
git commit -m "feat(azdo): list builds and summarise their timelines"
```

---

### Task 8: Build logs and tailing

The log pane concatenates every task's log in execution order and, while the
build runs, re-reads only the part it has not shown. Azure DevOps's log endpoint
takes a `startLine`, so the cursor is a line number rather than a byte offset —
simpler than the spec's phrasing and supported directly by the API.

**Files:**
- Modify: `internal/azdo/builds.go`
- Modify: `internal/azdo/builds_test.go`

**Interfaces:**
- Consumes: `c.getText`, `c.root()` from Task 1; `Record` from Task 7.
- Produces:
  - `type LogChunk struct { Task string; LogID int; Lines []string }`
  - `type LogCursor map[int]int`
  - `func (c *Client) LogLines(buildID, logID, startLine int) ([]string, error)`
  - `func (c *Client) NewLogChunks(buildID int, records []Record, cursor LogCursor) ([]LogChunk, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/azdo/builds_test.go`:

```go
func TestLogLinesSplitsTheBodyAndPassesStartLine(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("startLine"); got != "3" {
			t.Errorf("startLine = %q, want 3", got)
		}
		w.Write([]byte("third\nfourth\n"))
	})

	lines, err := c.LogLines(9001, 7, 3)
	if err != nil {
		t.Fatalf("LogLines returned %v", err)
	}
	if len(lines) != 2 || lines[0] != "third" || lines[1] != "fourth" {
		t.Errorf("lines = %q, want the two lines without the trailing blank", lines)
	}
}

func TestLogLinesOnAnEmptyLog(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})

	lines, err := c.LogLines(9001, 7, 0)
	if err != nil {
		t.Fatalf("LogLines returned %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %q, want none", lines)
	}
}

func TestNewLogChunksReadsEachTaskOnceAndAdvancesTheCursor(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("startLine") {
		case "", "0":
			w.Write([]byte("one\ntwo\n"))
		default:
			w.Write([]byte("three\n"))
		}
	})

	records := []Record{
		{Name: "Restore", Type: "Task", Order: 1, LogID: 6},
		{Name: "Build", Type: "Task", Order: 2, LogID: 7},
		{Name: "Job", Type: "Job", Order: 1},          // no log of its own
		{Name: "Pending", Type: "Task", Order: 3},     // not started, LogID zero
	}
	cursor := LogCursor{}

	chunks, err := c.NewLogChunks(9001, records, cursor)
	if err != nil {
		t.Fatalf("NewLogChunks returned %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 — only tasks with a log id", len(chunks))
	}
	if chunks[0].Task != "Restore" || len(chunks[0].Lines) != 2 {
		t.Errorf("first chunk = %+v", chunks[0])
	}
	if cursor[6] != 2 || cursor[7] != 2 {
		t.Errorf("cursor = %v, want each log advanced to 2", cursor)
	}

	// A second pass must return only what has been appended since.
	again, err := c.NewLogChunks(9001, records, cursor)
	if err != nil {
		t.Fatalf("second NewLogChunks returned %v", err)
	}
	if len(again) != 2 || len(again[0].Lines) != 1 || again[0].Lines[0] != "three" {
		t.Errorf("second pass = %+v, want one new line per log", again)
	}
	if cursor[6] != 3 {
		t.Errorf("cursor = %v, want 3 after the second pass", cursor)
	}
}

func TestNewLogChunksSkipsALogWithNothingNew(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})

	records := []Record{{Name: "Build", Type: "Task", Order: 1, LogID: 7}}
	chunks, err := c.NewLogChunks(9001, records, LogCursor{7: 10})
	if err != nil {
		t.Fatalf("NewLogChunks returned %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("chunks = %+v, want none when the log has not grown", chunks)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/azdo/ -run 'TestLogLines|TestNewLogChunks' -v`

Expected: compile failure — `LogLines`, `NewLogChunks`, `LogChunk`, `LogCursor`
are undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/azdo/builds.go`:

```go
// LogChunk is one task's log text, or the part of it that has not been shown
// yet.
type LogChunk struct {
	Task  string
	LogID int
	Lines []string
}

// LogCursor remembers how many lines of each log have already been consumed, so
// tailing a running build re-reads nothing.
type LogCursor map[int]int

// LogLines fetches one build log from startLine onward. The endpoint answers
// plain text, so this is the one call that does not decode JSON.
func (c *Client) LogLines(buildID, logID, startLine int) ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/build/builds/%d/logs/%d?startLine=%d&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		buildID, logID, startLine, APIVersion)

	body, err := c.getText(endpoint)
	if err != nil {
		return nil, err
	}

	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil, nil
	}
	return strings.Split(body, "\n"), nil
}

// NewLogChunks returns whatever each task's log has gained since the cursor was
// last advanced, in execution order, and advances the cursor. Calling it on a
// fresh cursor reads the whole build; calling it again while the build runs
// reads only the tail.
func (c *Client) NewLogChunks(buildID int, records []Record, cursor LogCursor) ([]LogChunk, error) {
	var chunks []LogChunk

	for _, r := range records {
		// Stages and jobs have no log of their own, and a task that has not
		// started yet has no log id.
		if r.Type != "Task" || r.LogID == 0 {
			continue
		}

		lines, err := c.LogLines(buildID, r.LogID, cursor[r.LogID])
		if err != nil {
			return chunks, err
		}
		if len(lines) == 0 {
			continue
		}

		cursor[r.LogID] += len(lines)
		chunks = append(chunks, LogChunk{Task: r.Name, LogID: r.LogID, Lines: lines})
	}
	return chunks, nil
}
```

Add `"strings"` to the import block in `builds.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/azdo/ -v && make vet`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/azdo/builds.go internal/azdo/builds_test.go
git commit -m "feat(azdo): read build logs incrementally for tailing"
```

---

### Task 9: The view contract and the shared browser scaffold

This is the refactor the whole UI rests on. It moves the list, the detail
viewport, the pane split, the fuzzy filter, and the shared copy and open actions
out of `workitems.go` so the three views do not each own a copy.

**Files:**
- Create: `internal/ui/view.go`
- Create: `internal/ui/browser.go`
- Create: `internal/ui/actions.go`
- Create: `internal/ui/browser_test.go`
- Modify: `internal/ui/style.go`
- Modify: `internal/ui/workitems.go` (remove `SlackLink`, `copyToClipboard`, `openBrowser` — they move to `actions.go`)

**Interfaces:**
- Consumes: `humanAge` from Task 2.
- Produces:
  - `type View interface { Update(tea.Msg) (View, tea.Cmd); Body(width, height int) string; Title() string; Hints() string; Status() (string, bool) }`
  - `type PushMsg struct { View View }`
  - `type PopMsg struct{}`
  - `type StatusMsg struct { Text string; Err bool }`
  - `type ShellCommandMsg struct { Command string }`
  - `type ErrMsg struct { Err error }`
  - `type Row interface { FilterValue() string; Render(width int) string; CopyID() string; Label() string; URL() string }`
  - `type Browser struct { Detail func(Row, int) string; ... }`
  - `func NewBrowser() Browser`
  - `func (b *Browser) SetRows(rows []Row)`
  - `func (b *Browser) SetSize(width, height int)`
  - `func (b *Browser) Selected() (Row, bool)`
  - `func (b *Browser) Update(msg tea.Msg) tea.Cmd`
  - `func (b Browser) View() string`
  - `func (b Browser) Filtering() bool`
  - `func (b Browser) FilterView() string`
  - `func (b Browser) Len() int`
  - `func SharedAction(r Row, msg tea.KeyMsg) (string, bool)`
  - `func SlackLink(title, url string) string`
  - `func CopyToClipboard(s string) error`
  - `func OpenBrowser(url string) error`
  - `const SharedHints = "/ filter · y copy id · s slack · o open"`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/browser_test.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeRow is a Row with no Azure DevOps behind it, so the scaffold can be
// tested on its own.
type fakeRow struct {
	id    int
	title string
}

func (r fakeRow) FilterValue() string      { return fmt.Sprintf("%d %s", r.id, r.title) }
func (r fakeRow) Render(width int) string  { return truncate(fmt.Sprintf("%d %s", r.id, r.title), width) }
func (r fakeRow) CopyID() string           { return fmt.Sprint(r.id) }
func (r fakeRow) Label() string            { return fmt.Sprintf("#%d %s", r.id, r.title) }
func (r fakeRow) URL() string              { return fmt.Sprintf("https://example.test/%d", r.id) }

func testBrowser(t *testing.T) Browser {
	t.Helper()
	b := NewBrowser()
	b.Detail = func(r Row, width int) string { return "detail for " + r.CopyID() }
	b.SetRows([]Row{
		fakeRow{1, "first thing"},
		fakeRow{2, "second thing"},
		fakeRow{3, "third thing"},
	})
	b.SetSize(100, 20)
	return b
}

func TestBrowserRendersRowsAndTheSelectedDetail(t *testing.T) {
	b := testBrowser(t)

	view := b.View()
	for _, want := range []string{"first thing", "second thing", "detail for 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestBrowserMovingTheCursorReRendersTheDetail(t *testing.T) {
	b := testBrowser(t)
	b.Update(tea.KeyMsg{Type: tea.KeyDown})

	if !strings.Contains(b.View(), "detail for 2") {
		t.Errorf("the detail pane did not follow the cursor:\n%s", b.View())
	}
}

func TestBrowserSelectedReportsTheRow(t *testing.T) {
	b := testBrowser(t)
	row, ok := b.Selected()
	if !ok {
		t.Fatal("Selected on a populated browser returned nothing")
	}
	if row.CopyID() != "1" {
		t.Errorf("selected = %s, want 1", row.CopyID())
	}
}

func TestBrowserSelectedOnAnEmptyList(t *testing.T) {
	b := NewBrowser()
	b.Detail = func(Row, int) string { return "" }
	b.SetRows(nil)
	b.SetSize(100, 20)

	if _, ok := b.Selected(); ok {
		t.Error("Selected on an empty browser reported a row")
	}
	// Rendering an empty browser must not panic.
	b.View()
}

func TestBrowserFilteringSwallowsActionKeys(t *testing.T) {
	b := testBrowser(t)
	b.Update(runes("/"))

	if !b.Filtering() {
		t.Fatal("pressing / did not start filtering")
	}
	// While the prompt is open, "o" is a letter, not the open action.
	b.Update(runes("o"))
	if !strings.Contains(b.FilterView(), "o") {
		t.Errorf("the filter input did not take the keystroke: %q", b.FilterView())
	}
}

func TestSharedActionHandlesTheThreeCommonKeys(t *testing.T) {
	row := fakeRow{42, "a thing"}

	for _, tc := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{runes("y"), "copied id 42"},
		{runes("s"), "copied Slack link for #42"},
		{runes("o"), "opened #42"},
	} {
		got, handled := SharedAction(row, tc.key)
		if !handled {
			t.Fatalf("%v was not handled", tc.key)
		}
		if got != tc.want {
			t.Errorf("status = %q, want %q", got, tc.want)
		}
	}

	if _, handled := SharedAction(row, runes("z")); handled {
		t.Error("an unrelated key was claimed as a shared action")
	}
}

func TestSlackLinkEscapesBracketsInTheTitle(t *testing.T) {
	got := SlackLink("[#4021] a title", "https://example.test/4021")
	want := "((#4021)) a title"
	if !strings.HasPrefix(got, "["+want+"]") {
		t.Errorf("SlackLink = %q, want the brackets turned into parentheses", got)
	}
}
```

`runes` already exists in `workitems_test.go` in the same package, so it does
not need redefining.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestBrowser|TestSharedAction|TestSlackLink' -v`

Expected: compile failure — `Browser`, `Row`, `NewBrowser`, `SharedAction` are
undefined, and `SlackLink` is still a method-free function on `workitems.go`
that will collide once `actions.go` defines it.

- [ ] **Step 3: Write the view contract**

Create `internal/ui/view.go`:

```go
package ui

import tea "github.com/charmbracelet/bubbletea"

// View is one screen inside boardwalk. Root owns the header, the hint line and
// the status line, so a view only renders its own body — which keeps the chrome
// identical everywhere and in one place.
type View interface {
	// Update handles a message and returns the view to carry on with. It
	// returns View rather than tea.Model so Root can hold the stack without
	// type assertions.
	Update(tea.Msg) (View, tea.Cmd)

	// Body renders the view at the size Root has left for it.
	Body(width, height int) string

	// Title is the header line, for example "pull requests (42) · acme/Platform".
	Title() string

	// Hints is the key line at the bottom.
	Hints() string

	// Status is the transient message under the hints, and whether it is an
	// error, which decides its colour.
	Status() (string, bool)
}

// PushMsg asks Root to open a view on top of the current one, for example a
// build's logs.
type PushMsg struct{ View View }

// PopMsg asks Root to return to the view underneath.
type PopMsg struct{}

// StatusMsg sets the status line from inside a command.
type StatusMsg struct {
	Text string
	Err  bool
}

// ErrMsg is a failed fetch. Root renders it on the status line and the view
// keeps whatever data it already had.
type ErrMsg struct{ Err error }

// ShellCommandMsg quits, printing a command for the shell wrapper to put on the
// prompt. A child process cannot drive its parent's line editor, so this is the
// only way to hand work back to the shell.
type ShellCommandMsg struct{ Command string }
```

- [ ] **Step 4: Write the shared actions**

Create `internal/ui/actions.go`, moving the three functions out of
`workitems.go`:

```go
package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SharedHints is the part of the key line every view has in common.
const SharedHints = "/ filter · y copy id · s slack · o open"

// SharedAction runs the copy and open actions every view binds. It reports
// whether it claimed the key, so a view can fall through to its own bindings.
func SharedAction(r Row, msg tea.KeyMsg) (string, bool) {
	switch msg.String() {
	case "y", "ctrl+y":
		CopyToClipboard(r.CopyID())
		return fmt.Sprintf("copied id %s", r.CopyID()), true
	case "s", "ctrl+s":
		CopyToClipboard(SlackLink(r.Label(), r.URL()))
		return fmt.Sprintf("copied Slack link for %s", firstToken(r.Label())), true
	case "o", "ctrl+o":
		OpenBrowser(r.URL())
		return fmt.Sprintf("opened %s", firstToken(r.Label())), true
	}
	return "", false
}

// firstToken is the identifier at the head of a row label — "#4021" or "!512" —
// which is all the status line needs to name what was acted on.
func firstToken(label string) string {
	if i := strings.IndexByte(label, ' '); i > 0 {
		return label[:i]
	}
	return label
}

// SlackLink renders a markdown link, which Slack's composer turns into a real
// link on paste. Square brackets in the title would end the link text early, so
// they become parentheses.
func SlackLink(title, url string) string {
	title = strings.NewReplacer("[", "(", "]", ")", "\n", " ", "\r", " ").Replace(title)
	return fmt.Sprintf("[%s](%s)", strings.TrimSpace(title), url)
}

func CopyToClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func OpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}
```

Delete `SlackLink`, `copyToClipboard`, and `openBrowser` from `workitems.go`,
and update its call sites to the exported names. `workitems_test.go` calls
`SlackLink` already, so that test keeps working.

- [ ] **Step 5: Write the browser scaffold**

Create `internal/ui/browser.go`:

```go
package ui

import (
	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// listShare is the fraction of the width the list takes, leaving the rest for
// the detail pane.
const listShare = 3.0 / 5.0

// Row is one line in a browser. Every view's row type implements it, which is
// what lets the copy and open actions live in one place.
type Row interface {
	// FilterValue is what the fuzzy matcher sees.
	FilterValue() string

	// Render draws the row at the given width, without the selection marker.
	Render(width int) string

	// CopyID is what the copy-id action puts on the clipboard.
	CopyID() string

	// Label is the human name, leading with an identifier: "#4021 title".
	Label() string

	// URL is where the open action goes.
	URL() string
}

// Browser is the list-and-detail scaffold all three views are built on. A view
// supplies rows and a Detail renderer; everything else is here.
type Browser struct {
	// Detail renders the pane on the right for the selected row.
	Detail func(Row, int) string

	list   list.Model
	detail viewport.Model

	width, height int
}

func NewBrowser() Browser {
	l := list.New(nil, rowDelegate{width: 80}, 0, 0)
	// The list keeps a title-bar row for the filter prompt even with the title
	// hidden, which would push the rows a line below the detail pane. The
	// filter input is rendered on boardwalk's own status line instead.
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.InfiniteScrolling = false

	return Browser{
		Detail: func(Row, int) string { return "" },
		list:   l,
		detail: viewport.New(0, 0),
	}
}

func (b *Browser) SetRows(rows []Row) {
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = rowItem{r}
	}
	b.list.SetItems(items)
	b.renderDetail()
}

func (b *Browser) SetSize(width, height int) {
	b.width, b.height = width, height

	listWidth := int(float64(width) * listShare)
	detailWidth := width - listWidth - 4

	b.list.SetSize(listWidth, height)
	b.list.SetDelegate(rowDelegate{width: listWidth - 2})
	b.detail.Width = max(10, detailWidth)
	b.detail.Height = height
	b.renderDetail()
}

func (b *Browser) Selected() (Row, bool) {
	it, ok := b.list.SelectedItem().(rowItem)
	if !ok {
		return nil, false
	}
	return it.Row, true
}

// Update forwards a message to the list and re-renders the detail pane when the
// cursor has moved.
func (b *Browser) Update(msg tea.Msg) tea.Cmd {
	before := b.list.Index()

	var cmd tea.Cmd
	b.list, cmd = b.list.Update(msg)

	if b.list.Index() != before {
		b.renderDetail()
	}
	return cmd
}

// RefreshDetail re-renders the pane in place, for a view whose detail data
// arrived after the cursor landed.
func (b *Browser) RefreshDetail() { b.renderDetail() }

func (b *Browser) renderDetail() {
	row, ok := b.Selected()
	if !ok {
		b.detail.SetContent("")
		return
	}
	b.detail.SetContent(b.Detail(row, max(20, b.detail.Width-2)))
	b.detail.GotoTop()
}

func (b Browser) View() string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		b.list.View(),
		detailPane.Render(b.detail.View()),
	)
}

func (b Browser) Filtering() bool    { return b.list.FilterState() == list.Filtering }
func (b Browser) FilterView() string { return b.list.FilterInput.View() }
func (b Browser) Len() int           { return len(b.list.Items()) }

// rowItem adapts a Row to the list widget, which wants its own interface.
type rowItem struct{ Row }

type rowDelegate struct{ width int }

func (d rowDelegate) Height() int                         { return 1 }
func (d rowDelegate) Spacing() int                        { return 0 }
func (d rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(rowItem)
	if !ok {
		return
	}

	row := it.Render(d.width)
	if index == m.Index() {
		io.WriteString(w, selectedRow.Render("▸ "+row))
		return
	}
	io.WriteString(w, normalRow.Render("  "+row))
}
```

- [ ] **Step 6: Add the styles the later views need**

Append to `internal/ui/style.go`'s `var` block:

```go
	colErr     = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colWarn    = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	colDraft   = lipgloss.AdaptiveColor{Light: "97", Dark: "141"}

	errStyle   = lipgloss.NewStyle().Foreground(colErr)
	warnStyle  = lipgloss.NewStyle().Foreground(colWarn)
	draftStyle = lipgloss.NewStyle().Foreground(colDraft)
	bannerRow  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	menuItem   = lipgloss.NewStyle().PaddingLeft(2)
	menuPicked = lipgloss.NewStyle().PaddingLeft(0).Foreground(colSelected).Bold(true)
```

And add the build status colouring helper at the end of `style.go`:

```go
// statusGlyph pairs a build status with a coloured marker, so the state reads
// at a glance without the word taking a column of its own.
func statusGlyph(s azdo.BuildStatus) string {
	switch s {
	case azdo.StatusRunning:
		return warnStyle.Render("◐ running")
	case azdo.StatusSucceeded:
		return statusStyle.Render("✓ succeeded")
	case azdo.StatusFailed:
		return errStyle.Render("✗ failed")
	case azdo.StatusPartial:
		return warnStyle.Render("~ partial")
	case azdo.StatusCanceled:
		return chromeStyle.Render("⊘ canceled")
	default:
		return chromeStyle.Render("· queued")
	}
}
```

Add the `azdo` import to `style.go`.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && make vet`

Expected: the browser and action tests PASS. `workitems.go` still compiles
because only the three moved functions changed name. If `workitems_test.go`
fails on anything else, leave it — Task 10 rebuilds that view.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/view.go internal/ui/browser.go internal/ui/actions.go internal/ui/browser_test.go internal/ui/style.go internal/ui/workitems.go
git commit -m "refactor(ui): extract the view contract and list/detail scaffold"
```

---

### Task 10: Work items rebuilt on the scaffold

The existing view is reimplemented on `Browser` and gains acceptance criteria
and a lazily fetched discussion. The branch flow arrives in Task 11.

**Files:**
- Modify: `internal/ui/workitems.go` (rewrite)
- Modify: `internal/ui/workitems_test.go`

**Interfaces:**
- Consumes: `Browser`, `Row`, `View`, `SharedAction`, `StatusMsg`, `ErrMsg` from Task 9; `humanAge` from Task 2; `azdo.Comment`, `azdo.Client.Comments` from Task 5.
- Produces:
  - `type WorkItems struct { ... }` implementing `View`
  - `func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly bool) *WorkItems`
  - `type workItemRow struct{ azdo.WorkItem }` implementing `Row`
  - `type commentsMsg struct { ID int; Comments []azdo.Comment }`

- [ ] **Step 1: Write the failing tests**

Rewrite `internal/ui/workitems_test.go`'s helpers and add the new assertions.
Keep `fixture()` as it is, add `AcceptanceCriteria` to the first item, and
replace `sized` and `press` with versions that work against the pointer view:

```go
func sized(t *testing.T, m *WorkItems, w, h int) *WorkItems {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(*WorkItems)
}

func press(t *testing.T, m *WorkItems, key tea.KeyMsg) (*WorkItems, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key)
	return updated.(*WorkItems), cmd
}

func body(t *testing.T, m *WorkItems) string {
	t.Helper()
	return m.Body(120, 20)
}
```

Then the new tests:

```go
func TestWorkItemsShowsAcceptanceCriteria(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	if !strings.Contains(body(t, m), "Retries three times") {
		t.Errorf("the detail pane is missing the acceptance criteria:\n%s", body(t, m))
	}
}

func TestWorkItemsRequestsTheDiscussionForTheSelectedItem(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	// The fetch is a command rather than a call, so the test can run it or not.
	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the selected item's discussion")
	}
}

func TestWorkItemsRendersAFetchedDiscussion(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, _ := m.Update(commentsMsg{ID: 4021, Comments: []azdo.Comment{
		{Author: "Other Dev", Created: time.Now().Add(-2 * time.Hour), Text: "Why the retry cap?"},
	}})
	m = updated.(*WorkItems)

	view := body(t, m)
	if !strings.Contains(view, "Why the retry cap?") || !strings.Contains(view, "Other Dev") {
		t.Errorf("the discussion did not render:\n%s", view)
	}
}

func TestWorkItemsIgnoresADiscussionForAnotherItem(t *testing.T) {
	// A slow fetch can land after the cursor has moved on.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, _ := m.Update(commentsMsg{ID: 3998, Comments: []azdo.Comment{
		{Author: "Other Dev", Text: "stale comment"},
	}})
	m = updated.(*WorkItems)

	if strings.Contains(body(t, m), "stale comment") {
		t.Error("a discussion for an unselected item was rendered")
	}
}

func TestWorkItemsToggleSwitchesScope(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	if !strings.Contains(m.Title(), "all 3") {
		t.Errorf("title = %q, want the full count", m.Title())
	}
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.Title(), "mine 2") {
		t.Errorf("title = %q, want the mine count", m.Title())
	}
}

func TestWorkItemsErrorGoesToTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, _ := m.Update(ErrMsg{Err: errors.New("no network")})
	m = updated.(*WorkItems)

	text, isErr := m.Status()
	if !isErr || !strings.Contains(text, "no network") {
		t.Errorf("status = %q, isErr = %v; want the error reported", text, isErr)
	}
	// The list must survive the failure.
	if !strings.Contains(body(t, m), "Retry webhook delivery") {
		t.Error("the rows were lost when a fetch failed")
	}
}
```

Add `"errors"`, `"time"`, and the `azdo` import as needed. Delete any existing
test that asserts on the removed `Command` field or on `View()`; the equivalent
assertions now go through `Body`, `Title`, and `Status`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestWorkItems -v`

Expected: compile failure — `NewWorkItems` returns a value not a pointer,
`Body`, `Title`, `Status`, and `commentsMsg` are undefined.

- [ ] **Step 3: Rewrite the view**

Replace `internal/ui/workitems.go` with:

```go
// Package ui holds boardwalk's terminal views.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// workItemRow adapts a work item to the browser. FilterValue spans id, type,
// state, assignee and title, so typing "25701" or "atchley defect" both land.
type workItemRow struct {
	azdo.WorkItem
	url string
}

func (r workItemRow) FilterValue() string {
	return fmt.Sprintf("%d %s %s %s %s", r.ID, r.Type, r.State, r.Assigned, r.Title)
}

func (r workItemRow) Render(width int) string {
	return truncate(fmt.Sprintf("%-7d %-14s %-16s %-18s %s",
		r.ID,
		truncate("["+r.Type+"]", 14),
		truncate(r.State, 16),
		truncate(r.Assigned, 18),
		r.Title), width)
}

func (r workItemRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r workItemRow) Label() string  { return fmt.Sprintf("#%d %s", r.ID, r.Title) }
func (r workItemRow) URL() string    { return r.url }

// commentsMsg carries a fetched discussion. It names the work item it belongs
// to because a slow fetch can land after the cursor has moved on.
type commentsMsg struct {
	ID       int
	Comments []azdo.Comment
}

// WorkItems is the work item browser.
type WorkItems struct {
	client  *azdo.Client
	browser Browser

	all      []Row
	mine     []Row
	mineOnly bool

	// comments is keyed by work item id and filled lazily as the cursor moves.
	comments map[int][]azdo.Comment
	loading  map[int]bool

	status string
	failed bool
	now    func() time.Time
}

func NewWorkItems(c *azdo.Client, items []azdo.WorkItem, mineOnly bool) *WorkItems {
	m := &WorkItems{
		client:   c,
		browser:  NewBrowser(),
		mineOnly: mineOnly,
		comments: map[int][]azdo.Comment{},
		loading:  map[int]bool{},
		now:      time.Now,
	}

	for _, wi := range items {
		row := workItemRow{wi}
		row.url = c.WorkItemURL(wi.ID)
		m.all = append(m.all, row)
		if c.Me != "" && wi.AssignedKey == c.Me {
			m.mine = append(m.mine, row)
		}
	}

	m.browser.Detail = m.renderDetail
	m.applyScope()
	return m
}

// Init asks for the first selected item's discussion.
func (m *WorkItems) Init() tea.Cmd { return m.fetchComments() }

func (m *WorkItems) applyScope() {
	rows := m.all
	if m.mineOnly {
		rows = m.mine
	}
	m.browser.SetRows(rows)
}

func (m *WorkItems) selected() (workItemRow, bool) {
	row, ok := m.browser.Selected()
	if !ok {
		return workItemRow{}, false
	}
	it, ok := row.(workItemRow)
	return it, ok
}

// fetchComments loads the selected item's discussion unless it is already
// loaded or in flight.
func (m *WorkItems) fetchComments() tea.Cmd {
	it, ok := m.selected()
	if !ok {
		return nil
	}
	if _, done := m.comments[it.ID]; done || m.loading[it.ID] {
		return nil
	}

	m.loading[it.ID] = true
	client, id := m.client, it.ID
	return func() tea.Msg {
		comments, err := client.Comments(id)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not load the discussion for #%d: %w", id, err)}
		}
		return commentsMsg{ID: id, Comments: comments}
	}
}

func (m *WorkItems) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case commentsMsg:
		delete(m.loading, msg.ID)
		m.comments[msg.ID] = msg.Comments
		if it, ok := m.selected(); ok && it.ID == msg.ID {
			m.browser.RefreshDetail()
		}
		return m, nil

	case ErrMsg:
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		// While the filter prompt is open every key belongs to it, or typing
		// "o" would open a browser instead of entering a letter.
		if m.browser.Filtering() {
			break
		}

		if it, ok := m.selected(); ok {
			if status, handled := SharedAction(it, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+t":
			m.mineOnly = !m.mineOnly
			m.applyScope()
			return m, m.fetchComments()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchComments())
}

func (m *WorkItems) renderDetail(row Row, width int) string {
	it, ok := row.(workItemRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(it.Label(), width)))
	for _, field := range [][2]string{
		{"type", it.Type},
		{"state", it.State},
		{"assigned", it.Assigned},
		{"tags", orDash(strings.ReplaceAll(it.Tags, "; ", ", "))},
		{"iteration", orDash(it.Iteration)},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-10s", field[0]+":")),
			truncate(field[1], width-11))
	}

	section(&b, "description", it.Description, "(no description)", width)
	section(&b, "acceptance criteria", it.AcceptanceCriteria, "(none)", width)

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("discussion"))
	switch comments, loaded := m.comments[it.ID]; {
	case !loaded:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("loading…"))
	case len(comments) == 0:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(no comments)"))
	default:
		for _, c := range comments {
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(fmt.Sprintf("%s · %s", c.Author, humanAge(c.Created, m.now()))),
				wordwrap(c.Text, width))
		}
	}
	return b.String()
}

// section writes a titled block, or a placeholder when the field is empty.
func section(b *strings.Builder, title, text, empty string, width int) {
	if strings.TrimSpace(text) == "" {
		text = empty
	}
	fmt.Fprintf(b, "\n%s\n%s\n", labelStyle.Render(title), wordwrap(text, width))
}

func (m *WorkItems) Body(width, height int) string {
	m.browser.SetSize(width, height)
	return m.browser.View()
}

func (m *WorkItems) Title() string {
	scope := fmt.Sprintf("all %d", len(m.all))
	if m.mineOnly {
		scope = fmt.Sprintf("mine %d", len(m.mine))
	}
	return fmt.Sprintf("work items (%s) · %s/%s", scope, m.client.Org, m.client.Project)
}

func (m *WorkItems) Hints() string {
	return "^t mine/all · " + SharedHints + " · esc back"
}

func (m *WorkItems) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && make vet`

Expected: PASS. `main.go` will not compile yet — it still calls
`tea.NewProgram(ui.NewWorkItems(...))` and reads `m.Command`. Task 16 fixes it;
until then use `go test ./internal/...` rather than `make test`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/workitems.go internal/ui/workitems_test.go
git commit -m "feat(ui): rebuild work items on the scaffold with criteria and discussion"
```

---

### Task 11: The work item branch flow

`a` sets the state to Active. `b` prompts for a branch name, creates it, sets
Active, and links it — reporting each step, and leaving the completed steps done
if a later one fails.

**Files:**
- Create: `internal/ui/branchflow.go`
- Create: `internal/ui/branchflow_test.go`
- Modify: `internal/ui/workitems.go` (key handling, prompt rendering)

**Interfaces:**
- Consumes: `branchName` from Task 2; `azdo.Repo`, `Repos`, `RefHead`, `CreateBranch`, `CurrentRepo` from Task 3; `SetState`, `LinkBranch` from Task 4; `StatusMsg`, `ShellCommandMsg` from Task 9.
- Produces:
  - `type BranchResult struct { Branch string; Steps []string; Err error }`
  - `func runBranchFlow(c *azdo.Client, id int, branch string, repo azdo.Repo) BranchResult`
  - `func pickRepo(repos []azdo.Repo, want string) (azdo.Repo, bool)`
  - `type branchDoneMsg struct{ BranchResult }`
  - `type stateSetMsg struct { ID int; State string; Err error }`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/branchflow_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPickRepo(t *testing.T) {
	repos := []azdo.Repo{
		{ID: "r1", Name: "platform-api"},
		{ID: "r2", Name: "platform-web"},
	}

	if got, ok := pickRepo(repos, "platform-web"); !ok || got.ID != "r2" {
		t.Errorf("pickRepo by name = %+v, %v", got, ok)
	}
	if _, ok := pickRepo(repos, "not-here"); ok {
		t.Error("pickRepo matched a repository that is not in the project")
	}
	if _, ok := pickRepo(repos, ""); ok {
		t.Error("pickRepo with no name reported a match")
	}
	if _, ok := pickRepo(nil, "platform-api"); ok {
		t.Error("pickRepo against no repositories reported a match")
	}
}

func TestBranchPromptOpensPrefilledAndIsEditable(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	if m.branchPrompt == nil {
		t.Fatal("b did not open the branch prompt")
	}
	if got := m.branchPrompt.Value(); got != "feature/4021-retry-webhook-delivery-on-5xx" {
		t.Fatalf("prompt = %q, want it prefilled with the generated name", got)
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if strings.HasSuffix(m.branchPrompt.Value(), "5xx") {
		t.Error("the prompt did not take the edit")
	}
}

func TestBranchPromptEscapeCancels(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling the prompt started work anyway")
	}
	if m.branchPrompt != nil {
		t.Error("escape did not close the prompt")
	}
}

func TestBranchPromptSwallowsActionKeys(t *testing.T) {
	// With the prompt open, "o" is a letter rather than the open action.
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	m, _ = press(t, m, runes("b"))
	m, _ = press(t, m, runes("o"))
	if !strings.HasSuffix(m.branchPrompt.Value(), "o") {
		t.Errorf("prompt = %q, want the keystroke in it", m.branchPrompt.Value())
	}
}

func TestBranchDoneReportsEveryStep(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch: "feature/4021-retry",
		Steps:  []string{"created feature/4021-retry", "set #4021 Active", "linked the branch"},
	}})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr {
		t.Error("a successful flow reported as an error")
	}
	for _, want := range []string{"created", "Active", "linked"} {
		if !strings.Contains(status, want) {
			t.Errorf("status = %q, want it to mention %q", status, want)
		}
	}
	if cmd == nil {
		t.Error("a successful flow did not hand a checkout command to the shell")
	}
}

func TestBranchDoneKeepsTheStepsThatSucceeded(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, cmd := m.Update(branchDoneMsg{BranchResult{
		Branch: "feature/4021-retry",
		Steps:  []string{"created feature/4021-retry"},
		Err:    errTest,
	}})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if !isErr {
		t.Error("a partial failure did not report as an error")
	}
	if !strings.Contains(status, "created feature/4021-retry") {
		t.Errorf("status = %q, want the completed step still named", status)
	}
	if cmd != nil {
		t.Error("a failed flow still handed a command to the shell")
	}
}

func TestSetStateReportsOnTheStatusLine(t *testing.T) {
	c, items := fixture()
	m := sized(t, NewWorkItems(c, items, false), 120, 24)

	updated, _ := m.Update(stateSetMsg{ID: 4021, State: "Active"})
	m = updated.(*WorkItems)

	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "Active") {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
	// The row itself has to show the new state, not just the status line.
	if !strings.Contains(body(t, m), "Active") {
		t.Error("the row did not pick up the new state")
	}
}
```

Add to the test file:

```go
var errTest = errors.New("the server said no")
```

with `"errors"` imported.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestPickRepo|TestBranch|TestSetState' -v`

Expected: compile failure — `pickRepo`, `branchDoneMsg`, `stateSetMsg`,
`BranchResult` are undefined.

- [ ] **Step 3: Write the flow**

Create `internal/ui/branchflow.go`:

```go
package ui

import (
	"fmt"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// BranchResult is what the branch flow did. Steps names everything that
// succeeded, so a failure halfway through still reports the work that landed
// rather than reading as a clean failure.
type BranchResult struct {
	Branch string
	Steps  []string
	Err    error
}

type branchDoneMsg struct{ BranchResult }

type stateSetMsg struct {
	ID    int
	State string
	Err   error
}

// pickRepo finds the repository the working directory belongs to among the
// project's repositories.
func pickRepo(repos []azdo.Repo, want string) (azdo.Repo, bool) {
	if want == "" {
		return azdo.Repo{}, false
	}
	for _, r := range repos {
		if r.Name == want {
			return r, true
		}
	}
	return azdo.Repo{}, false
}

// runBranchFlow creates the branch, moves the work item to Active, and links
// the branch to it. Each step is recorded before the next runs, so a later
// failure does not erase what already happened.
func runBranchFlow(c *azdo.Client, id int, branch string, repo azdo.Repo) BranchResult {
	res := BranchResult{Branch: branch}

	head, err := c.RefHead(repo.ID, repo.DefaultBranch)
	if err != nil {
		res.Err = fmt.Errorf("could not read %s in %s: %w", shortRef(repo.DefaultBranch), repo.Name, err)
		return res
	}

	if err := c.CreateBranch(repo.ID, branch, head); err != nil {
		res.Err = err
		return res
	}
	res.Steps = append(res.Steps, fmt.Sprintf("created %s in %s", branch, repo.Name))

	if err := c.SetState(id, "Active"); err != nil {
		res.Err = fmt.Errorf("could not set #%d Active: %w", id, err)
		return res
	}
	res.Steps = append(res.Steps, fmt.Sprintf("set #%d Active", id))

	if err := c.LinkBranch(id, repo.ProjectID, repo.ID, branch); err != nil {
		res.Err = fmt.Errorf("could not link the branch to #%d: %w", id, err)
		return res
	}
	res.Steps = append(res.Steps, "linked the branch")

	return res
}

// branchCmd runs the flow off the UI goroutine, resolving the repository first.
func branchCmd(c *azdo.Client, id int, branch string) tea.Cmd {
	return func() tea.Msg {
		repos, err := c.Repos()
		if err != nil {
			return branchDoneMsg{BranchResult{Branch: branch, Err: err}}
		}

		repo, ok := pickRepo(repos, azdo.CurrentRepo())
		if !ok {
			return branchDoneMsg{BranchResult{Branch: branch, Err: fmt.Errorf(
				"run boardwalk inside one of the project's repositories, or create the branch there — "+
					"the working directory is not an Azure DevOps repository in %s", c.Project)}}
		}
		return branchDoneMsg{runBranchFlow(c, id, branch, repo)}
	}
}

// stateCmd moves a work item to a state without the rest of the branch flow.
func stateCmd(c *azdo.Client, id int, state string) tea.Cmd {
	return func() tea.Msg {
		return stateSetMsg{ID: id, State: state, Err: c.SetState(id, state)}
	}
}
```

The repository picker the spec describes is deliberately narrowed here: rather
than a second interactive list, a working directory outside the project's
repositories reports what to do. A picker can be added later without changing
this flow's shape.

- [ ] **Step 4: Wire the keys and the prompt into the view**

In `internal/ui/workitems.go`, add to the `WorkItems` struct:

```go
	// branchPrompt is non-nil while the branch name is being edited.
	branchPrompt *textinput.Model
```

with `"github.com/charmbracelet/bubbles/textinput"` imported.

In `Update`'s `tea.KeyMsg` case, before the filter check:

```go
		if m.branchPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.branchPrompt = nil
				m.status, m.failed = "", false
				return m, nil
			case tea.KeyEnter:
				branch := strings.TrimSpace(m.branchPrompt.Value())
				m.branchPrompt = nil
				it, ok := m.selected()
				if !ok || branch == "" {
					return m, nil
				}
				m.status, m.failed = "creating "+branch+"…", false
				return m, branchCmd(m.client, it.ID, branch)
			}
			input, cmd := m.branchPrompt.Update(msg)
			m.branchPrompt = &input
			return m, cmd
		}
```

Add to the key switch, beside `ctrl+t`:

```go
		case "a":
			if it, ok := m.selected(); ok {
				m.status, m.failed = fmt.Sprintf("setting #%d Active…", it.ID), false
				return m, stateCmd(m.client, it.ID, "Active")
			}
			return m, nil

		case "b":
			it, ok := m.selected()
			if !ok {
				return m, nil
			}
			input := textinput.New()
			input.Prompt = "branch: "
			input.SetValue(branchName(it.Type, it.ID, it.Title))
			input.CursorEnd()
			input.Focus()
			m.branchPrompt = &input
			return m, textinput.Blink
```

Add the two message cases to `Update`:

```go
	case branchDoneMsg:
		if msg.Err != nil {
			m.failed = true
			m.status = msg.Err.Error()
			if len(msg.Steps) > 0 {
				m.status = strings.Join(msg.Steps, ", ") + "; then " + msg.Err.Error()
			}
			return m, nil
		}
		m.status, m.failed = strings.Join(msg.Steps, " · "), false
		// The shell has to do the checkout: a child process cannot move its
		// parent's working tree.
		return m, func() tea.Msg {
			return ShellCommandMsg{Command: fmt.Sprintf("git fetch origin && git checkout %s", msg.Branch)}
		}

	case stateSetMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.setRowState(msg.ID, msg.State)
		m.status, m.failed = fmt.Sprintf("#%d is now %s", msg.ID, msg.State), false
		return m, nil
```

Add the row updater, so the list reflects the change without a refetch:

```go
// setRowState rewrites a row in place after a successful state change, so the
// list agrees with the server without refetching the project.
func (m *WorkItems) setRowState(id int, state string) {
	for _, set := range [][]Row{m.all, m.mine} {
		for i, row := range set {
			if it, ok := row.(workItemRow); ok && it.ID == id {
				it.State = state
				set[i] = it
			}
		}
	}
	m.applyScope()
	m.browser.RefreshDetail()
}
```

Extend `Status` so the prompt owns the line while it is open:

```go
func (m *WorkItems) Status() (string, bool) {
	if m.branchPrompt != nil {
		return m.branchPrompt.View(), false
	}
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
```

And extend `Hints`:

```go
func (m *WorkItems) Hints() string {
	return "^t mine/all · a active · b branch · " + SharedHints + " · esc back"
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && go vet ./internal/...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/branchflow.go internal/ui/branchflow_test.go internal/ui/workitems.go
git commit -m "feat(ui): create branches, set Active and link them in process"
```

---

### Task 12: The pull request view

**Files:**
- Create: `internal/ui/pullrequests.go`
- Create: `internal/ui/pullrequests_test.go`

**Interfaces:**
- Consumes: `Browser`, `Row`, `View`, `SharedAction`, `ErrMsg` from Task 9; `humanAge`, `shortRef` from Task 2; `azdo.PullRequest`, `ThreadCounts`, `PullRequests`, `Threads`, `PullRequestURL`, `CurrentRepo` from Tasks 3 and 6.
- Produces:
  - `type PullRequests struct { ... }` implementing `View`
  - `func NewPullRequests(c *azdo.Client) *PullRequests`
  - `type prsMsg struct { PRs []azdo.PullRequest }`
  - `type threadsMsg struct { PR int; Counts azdo.ThreadCounts; Err error }`
  - `type draftFilter int` with `draftsHidden`, `draftsOnly`, `draftsAll`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/pullrequests_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func prFixture() (*azdo.Client, []azdo.PullRequest) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, []azdo.PullRequest{
		{ID: 512, Title: "Retry webhooks", Repo: "platform-api", RepoID: "r1",
			Author: "Dev Example", AuthorKey: "dev@acme.test",
			Source: "refs/heads/feature/4021-retry", Target: "refs/heads/main",
			Created: now.Add(-3 * time.Hour), Description: "Adds backoff",
			Reviewers: []azdo.Reviewer{{Name: "Other Dev", Vote: 10}}},
		{ID: 511, Title: "WIP cache", Repo: "platform-web", RepoID: "r2",
			Author: "Other Dev", IsDraft: true,
			Source: "refs/heads/spike", Target: "refs/heads/main",
			Created: now.Add(-48 * time.Hour)},
	}
}

func newPRs(t *testing.T) *PullRequests {
	t.Helper()
	c, prs := prFixture()
	m := NewPullRequests(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	updated, _ := m.Update(prsMsg{PRs: prs})
	m = updated.(*PullRequests)
	m.Body(160, 20)
	return m
}

func TestPullRequestsRowShowsBranchesAndAge(t *testing.T) {
	m := newPRs(t)
	view := m.Body(160, 20)

	for _, want := range []string{"platform-api", "!512", "feature/4021-retry", "main", "3h"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "refs/heads/") {
		t.Error("the refs/heads prefix was not trimmed")
	}
}

func TestPullRequestsHidesDraftsByDefault(t *testing.T) {
	m := newPRs(t)

	if strings.Contains(m.Body(160, 20), "WIP cache") {
		t.Error("a draft showed up in the default view")
	}
	if !strings.Contains(m.Title(), "drafts hidden") {
		t.Errorf("title = %q, want the filter named", m.Title())
	}
}

func TestPullRequestsDraftFilterCycles(t *testing.T) {
	m := newPRs(t)

	// hidden -> only
	updated, _ := m.Update(runes("d"))
	m = updated.(*PullRequests)
	view := m.Body(160, 20)
	if !strings.Contains(view, "WIP cache") || strings.Contains(view, "Retry webhooks") {
		t.Errorf("drafts-only did not filter:\n%s", view)
	}

	// only -> all
	updated, _ = m.Update(runes("d"))
	m = updated.(*PullRequests)
	view = m.Body(160, 20)
	if !strings.Contains(view, "WIP cache") || !strings.Contains(view, "Retry webhooks") {
		t.Errorf("all did not show both:\n%s", view)
	}

	// all -> hidden
	updated, _ = m.Update(runes("d"))
	m = updated.(*PullRequests)
	if strings.Contains(m.Body(160, 20), "WIP cache") {
		t.Error("the filter did not cycle back to hidden")
	}
}

func TestPullRequestsThreadCountsRenderOnceFetched(t *testing.T) {
	m := newPRs(t)

	if !strings.Contains(m.Body(160, 20), "…") {
		t.Error("an unfetched thread count did not render as an ellipsis")
	}

	updated, _ := m.Update(threadsMsg{PR: 512, Counts: azdo.ThreadCounts{
		Resolved: 4, Unresolved: 2,
		Open: []azdo.OpenThread{{Author: "Other Dev", Text: "Why the retry cap?"}},
	}})
	m = updated.(*PullRequests)

	view := m.Body(160, 20)
	if !strings.Contains(view, "4/2") {
		t.Errorf("the counts did not render as resolved/unresolved:\n%s", view)
	}
	if !strings.Contains(view, "Why the retry cap?") {
		t.Errorf("the open thread did not reach the detail pane:\n%s", view)
	}
}

func TestPullRequestsThreadFetchFailureDoesNotLoseTheRow(t *testing.T) {
	m := newPRs(t)

	updated, _ := m.Update(threadsMsg{PR: 512, Err: errTest})
	m = updated.(*PullRequests)

	if _, isErr := m.Status(); !isErr {
		t.Error("a thread fetch failure was not reported")
	}
	if !strings.Contains(m.Body(160, 20), "Retry webhooks") {
		t.Error("the row was lost when its thread fetch failed")
	}
}

func TestPullRequestsRepoScopeToggle(t *testing.T) {
	m := newPRs(t)
	m.repo = "platform-api"
	m.repoOnly = true

	if strings.Contains(m.Body(160, 20), "platform-web") {
		t.Error("the repository scope did not filter")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(*PullRequests)

	// Widening shows the other repository's pull request, drafts aside.
	if !strings.Contains(m.Title(), "all repos") {
		t.Errorf("title = %q, want the widened scope named", m.Title())
	}
}

func TestPullRequestsLabelAndURL(t *testing.T) {
	_, prs := prFixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	row := prRow{PullRequest: prs[0], url: c.PullRequestURL(prs[0].Repo, prs[0].ID)}

	if got := row.Label(); got != "!512 Retry webhooks" {
		t.Errorf("Label = %q", got)
	}
	if got := row.CopyID(); got != "512" {
		t.Errorf("CopyID = %q", got)
	}
	if !strings.HasSuffix(row.URL(), "/_git/platform-api/pullrequest/512") {
		t.Errorf("URL = %q", row.URL())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestPullRequests -v`

Expected: compile failure — `NewPullRequests`, `prsMsg`, `threadsMsg`, `prRow`
are undefined.

- [ ] **Step 3: Write the view**

Create `internal/ui/pullrequests.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// eagerThreads is how many rows' comment threads are fetched up front. Beyond
// that they load as the cursor reaches them, so a project with two hundred open
// pull requests still opens instantly.
const eagerThreads = 20

// draftFilter is the three-state draft toggle.
type draftFilter int

const (
	draftsHidden draftFilter = iota
	draftsOnly
	draftsAll
)

func (f draftFilter) String() string {
	switch f {
	case draftsOnly:
		return "drafts only"
	case draftsAll:
		return "drafts shown"
	default:
		return "drafts hidden"
	}
}

func (f draftFilter) keeps(isDraft bool) bool {
	switch f {
	case draftsOnly:
		return isDraft
	case draftsAll:
		return true
	default:
		return !isDraft
	}
}

// prRow adapts a pull request to the browser.
type prRow struct {
	azdo.PullRequest
	url    string
	counts *azdo.ThreadCounts // nil until the threads fetch lands
	now    time.Time
}

func (r prRow) FilterValue() string {
	return fmt.Sprintf("%d %s %s %s %s", r.ID, r.Repo, r.Author, shortRef(r.Source), r.Title)
}

func (r prRow) Render(width int) string {
	draft := " "
	if r.IsDraft {
		draft = draftStyle.Render("◌")
	}

	return truncate(fmt.Sprintf("%-16s %-6s %s %-34s %-28s %-7s %s", // draft is one display column, styled or not
		truncate(r.Repo, 16),
		fmt.Sprintf("!%d", r.ID),
		draft,
		truncate(r.Title, 34),
		truncate(shortRef(r.Source)+" → "+shortRef(r.Target), 28),
		r.countColumn(),
		humanAge(r.Created, r.now)), width)
}

// countColumn reads resolved over unresolved, and stays an ellipsis until the
// threads for this row have been fetched.
func (r prRow) countColumn() string {
	if r.counts == nil {
		return "…"
	}
	return fmt.Sprintf("%d/%d", r.counts.Resolved, r.counts.Unresolved)
}

func (r prRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r prRow) Label() string  { return fmt.Sprintf("!%d %s", r.ID, r.Title) }
func (r prRow) URL() string    { return r.url }

type prsMsg struct{ PRs []azdo.PullRequest }

type threadsMsg struct {
	PR     int
	Counts azdo.ThreadCounts
	Err    error
}

// PullRequests is the pull request browser.
type PullRequests struct {
	client  *azdo.Client
	browser Browser

	prs     []azdo.PullRequest
	counts  map[int]azdo.ThreadCounts
	loading map[int]bool

	drafts   draftFilter
	repo     string // the working directory's repository, if it is one
	repoOnly bool

	loaded bool
	status string
	failed bool
	now    func() time.Time
}

func NewPullRequests(c *azdo.Client) *PullRequests {
	m := &PullRequests{
		client:  c,
		browser: NewBrowser(),
		counts:  map[int]azdo.ThreadCounts{},
		loading: map[int]bool{},
		repo:    azdo.CurrentRepo(),
		now:     time.Now,
	}
	// Starting narrow when boardwalk is run inside a repository matches what
	// the user is looking at; ^t widens.
	m.repoOnly = m.repo != ""
	m.browser.Detail = m.renderDetail
	return m
}

// Init fetches the project's active pull requests.
func (m *PullRequests) Init() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		prs, err := client.PullRequests()
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch pull requests: %w", err)}
		}
		return prsMsg{PRs: prs}
	}
}

func (m *PullRequests) visible() []azdo.PullRequest {
	var out []azdo.PullRequest
	for _, pr := range m.prs {
		if !m.drafts.keeps(pr.IsDraft) {
			continue
		}
		if m.repoOnly && m.repo != "" && pr.Repo != m.repo {
			continue
		}
		out = append(out, pr)
	}
	return out
}

func (m *PullRequests) applyFilters() {
	prs := m.visible()
	rows := make([]Row, 0, len(prs))
	for _, pr := range prs {
		row := prRow{
			PullRequest: pr,
			url:         m.client.PullRequestURL(pr.Repo, pr.ID),
			now:         m.now(),
		}
		if counts, ok := m.counts[pr.ID]; ok {
			row.counts = &counts
		}
		rows = append(rows, row)
	}
	m.browser.SetRows(rows)
}

// fetchThreads loads the first screenful of rows up front and whatever the
// cursor has reached since.
func (m *PullRequests) fetchThreads() tea.Cmd {
	prs := m.visible()

	want := map[int]azdo.PullRequest{}
	for i, pr := range prs {
		if i < eagerThreads {
			want[pr.ID] = pr
		}
	}
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(prRow); ok {
			want[r.ID] = r.PullRequest
		}
	}

	var cmds []tea.Cmd
	for id, pr := range want {
		if _, done := m.counts[id]; done || m.loading[id] {
			continue
		}
		m.loading[id] = true

		client, repoID, prID := m.client, pr.RepoID, pr.ID
		cmds = append(cmds, func() tea.Msg {
			counts, err := client.Threads(repoID, prID)
			return threadsMsg{PR: prID, Counts: counts, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *PullRequests) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case prsMsg:
		m.prs, m.loaded = msg.PRs, true
		m.applyFilters()
		return m, m.fetchThreads()

	case threadsMsg:
		delete(m.loading, msg.PR)
		if msg.Err != nil {
			m.status, m.failed = fmt.Sprintf("could not load threads for !%d: %v", msg.PR, msg.Err), true
			return m, nil
		}
		m.counts[msg.PR] = msg.Counts
		m.applyFilters()
		return m, nil

	case ErrMsg:
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.browser.Filtering() {
			break
		}

		if row, ok := m.browser.Selected(); ok {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "d":
			m.drafts = (m.drafts + 1) % 3
			m.applyFilters()
			return m, m.fetchThreads()
		case "ctrl+t":
			m.repoOnly = !m.repoOnly
			m.applyFilters()
			return m, m.fetchThreads()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchThreads())
}

func (m *PullRequests) renderDetail(row Row, width int) string {
	r, ok := row.(prRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(r.Label(), width)))
	for _, field := range [][2]string{
		{"repo", r.Repo},
		{"author", r.Author},
		{"source", shortRef(r.Source)},
		{"target", shortRef(r.Target)},
		{"opened", humanAge(r.Created, r.now) + " ago"},
		{"threads", r.countColumn() + " resolved/unresolved"},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-9s", field[0]+":")),
			truncate(field[1], width-10))
	}

	if len(r.Reviewers) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("reviewers"))
		for _, rev := range r.Reviewers {
			fmt.Fprintf(&b, "  %s — %s\n", truncate(rev.Name, width-24), rev.VoteLabel())
		}
	}

	section(&b, "description", r.Description, "(no description)", width)

	if r.counts != nil && len(r.counts.Open) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("unresolved"))
		for _, t := range r.counts.Open {
			fmt.Fprintf(&b, "\n%s\n%s\n",
				labelStyle.Render(t.Author), wordwrap(t.Text, width))
		}
	}
	return b.String()
}

func (m *PullRequests) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return chromeStyle.Render("fetching pull requests…")
	}
	return m.browser.View()
}

func (m *PullRequests) Title() string {
	scope := "all repos"
	if m.repoOnly && m.repo != "" {
		scope = m.repo
	}
	return fmt.Sprintf("pull requests (%d · %s · %s) · %s/%s",
		m.browser.Len(), scope, m.drafts, m.client.Org, m.client.Project)
}

func (m *PullRequests) Hints() string {
	return "d drafts · ^t repo/all · " + SharedHints + " · esc back"
}

func (m *PullRequests) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && go vet ./internal/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/pullrequests.go internal/ui/pullrequests_test.go
git commit -m "feat(ui): add the pull request view"
```

---

### Task 13: The build view

**Files:**
- Create: `internal/ui/builds.go`
- Create: `internal/ui/builds_test.go`

**Interfaces:**
- Consumes: `Browser`, `Row`, `View`, `SharedAction`, `PushMsg`, `ErrMsg` from Task 9; `humanAge`, `shortRef` from Task 2; `statusGlyph` from Task 9's style changes; `azdo.Build`, `Builds`, `Timeline`, `Progress`, `Record`, `BuildURL` from Task 7.
- Produces:
  - `type Builds struct { ... }` implementing `View`
  - `func NewBuilds(c *azdo.Client) *Builds`
  - `type buildsMsg struct { Builds []azdo.Build }`
  - `type timelineMsg struct { Build int; Progress azdo.Progress; Records []azdo.Record; Err error }`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/builds_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func buildFixture() (*azdo.Client, []azdo.Build) {
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, []azdo.Build{
		{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: azdo.StatusRunning,
			Queued: now.Add(-20 * time.Minute), SourceBranch: "refs/heads/main", RequestedFor: "Dev Example"},
		{ID: 9000, Number: "20260911.2", Pipeline: "platform-web-ci", Status: azdo.StatusFailed,
			Queued: now.Add(-2 * time.Hour), SourceBranch: "refs/heads/feature/x", RequestedFor: "Other Dev"},
	}
}

func newBuilds(t *testing.T) *Builds {
	t.Helper()
	c, builds := buildFixture()
	m := NewBuilds(c)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	updated, _ := m.Update(buildsMsg{Builds: builds})
	m = updated.(*Builds)
	m.Body(160, 20)
	return m
}

func TestBuildsRowShowsPipelineStatusAndAge(t *testing.T) {
	m := newBuilds(t)
	view := m.Body(160, 20)

	for _, want := range []string{"platform-ci", "20260911.3", "running", "20m", "failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestBuildsShowsTheCurrentStepOnceTheTimelineLands(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{
		Build:    9001,
		Progress: azdo.Progress{CurrentStep: "Run tests"},
		Records:  []azdo.Record{{Name: "Run tests", Type: "Task", State: "inProgress", Order: 1, LogID: 7}},
	})
	m = updated.(*Builds)

	if !strings.Contains(m.Body(160, 20), "Run tests") {
		t.Errorf("the current step did not render:\n%s", m.Body(160, 20))
	}
}

func TestBuildsShowsTheErrorCount(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{
		Build:    9000,
		Progress: azdo.Progress{CurrentStep: "Test", Errors: 3},
	})
	m = updated.(*Builds)

	if !strings.Contains(m.Body(160, 20), "3 errors") {
		t.Errorf("the error count did not render:\n%s", m.Body(160, 20))
	}
}

func TestBuildsEnterOpensTheLogs(t *testing.T) {
	m := newBuilds(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg := cmd()
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", msg)
	}
	if !strings.Contains(push.View.Title(), "20260911.3") {
		t.Errorf("pushed view = %q, want the selected build's logs", push.View.Title())
	}
}

func TestBuildsFilterSpansThePipelineName(t *testing.T) {
	m := newBuilds(t)
	row, _ := m.browser.Selected()

	if !strings.Contains(row.FilterValue(), "platform-ci") {
		t.Errorf("FilterValue = %q, want the pipeline name in it", row.FilterValue())
	}
	if !strings.Contains(row.FilterValue(), "20260911.3") {
		t.Errorf("FilterValue = %q, want the build number in it", row.FilterValue())
	}
}

func TestBuildsLabelAndURL(t *testing.T) {
	_, builds := buildFixture()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	row := buildRow{Build: builds[0], url: c.BuildURL(9001)}

	if got := row.Label(); got != "platform-ci #20260911.3" {
		t.Errorf("Label = %q", got)
	}
	if got := row.CopyID(); got != "9001" {
		t.Errorf("CopyID = %q", got)
	}
	if !strings.HasSuffix(row.URL(), "buildId=9001") {
		t.Errorf("URL = %q", row.URL())
	}
}

func TestBuildsTimelineFailureDoesNotLoseTheRow(t *testing.T) {
	m := newBuilds(t)

	updated, _ := m.Update(timelineMsg{Build: 9001, Err: errTest})
	m = updated.(*Builds)

	if _, isErr := m.Status(); !isErr {
		t.Error("a timeline failure was not reported")
	}
	if !strings.Contains(m.Body(160, 20), "platform-ci") {
		t.Error("the row was lost when its timeline fetch failed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestBuilds -v`

Expected: compile failure — `NewBuilds`, `buildsMsg`, `timelineMsg`, `buildRow`
are undefined.

- [ ] **Step 3: Write the view**

Create `internal/ui/builds.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// buildPageSize is how many recent runs to list. Beyond this the list stops
// being something anyone scrolls.
const buildPageSize = 50

// eagerTimelines is how many rows' timelines are fetched up front, on the same
// reasoning as the pull request thread counts.
const eagerTimelines = 15

// buildRow adapts a build to the browser.
type buildRow struct {
	azdo.Build
	url      string
	progress *azdo.Progress // nil until the timeline lands
	now      time.Time
}

func (r buildRow) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s", r.Pipeline, r.Number, r.Status, shortRef(r.SourceBranch))
}

func (r buildRow) Render(width int) string {
	// The status and error cells carry styling, so they are padded by display
	// width rather than by a format verb.
	return truncate(fmt.Sprintf("%-22s %-13s %s %-26s %s %s",
		truncate(r.Pipeline, 22),
		truncate(r.Number, 13),
		padRight(statusGlyph(r.Status), 13),
		truncate(r.stepColumn(), 26),
		padRight(r.errorColumn(), 9),
		humanAge(r.Queued, r.now)), width)
}

func (r buildRow) stepColumn() string {
	if r.progress == nil {
		return "…"
	}
	if r.progress.CurrentStep == "" {
		return "—"
	}
	return r.progress.CurrentStep
}

func (r buildRow) errorColumn() string {
	if r.progress == nil || r.progress.Errors == 0 {
		return ""
	}
	return errStyle.Render(fmt.Sprintf("%d errors", r.progress.Errors))
}

func (r buildRow) CopyID() string { return fmt.Sprint(r.ID) }
func (r buildRow) Label() string  { return fmt.Sprintf("%s #%s", r.Pipeline, r.Number) }
func (r buildRow) URL() string    { return r.url }

type buildsMsg struct{ Builds []azdo.Build }

type timelineMsg struct {
	Build    int
	Progress azdo.Progress
	Records  []azdo.Record
	Err      error
}

// Builds is the pipeline run browser.
type Builds struct {
	client  *azdo.Client
	browser Browser

	builds   []azdo.Build
	progress map[int]azdo.Progress
	records  map[int][]azdo.Record
	loading  map[int]bool

	loaded bool
	status string
	failed bool
	now    func() time.Time
}

func NewBuilds(c *azdo.Client) *Builds {
	m := &Builds{
		client:   c,
		browser:  NewBrowser(),
		progress: map[int]azdo.Progress{},
		records:  map[int][]azdo.Record{},
		loading:  map[int]bool{},
		now:      time.Now,
	}
	m.browser.Detail = m.renderDetail
	return m
}

// Init fetches the project's recent runs.
func (m *Builds) Init() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		builds, err := client.Builds(buildPageSize)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("could not fetch builds: %w", err)}
		}
		return buildsMsg{Builds: builds}
	}
}

func (m *Builds) applyRows() {
	rows := make([]Row, 0, len(m.builds))
	for _, b := range m.builds {
		row := buildRow{Build: b, url: m.client.BuildURL(b.ID), now: m.now()}
		if p, ok := m.progress[b.ID]; ok {
			row.progress = &p
		}
		rows = append(rows, row)
	}
	m.browser.SetRows(rows)
}

// fetchTimelines loads the first screenful of timelines and whatever the cursor
// has reached since.
func (m *Builds) fetchTimelines() tea.Cmd {
	want := map[int]bool{}
	for i, b := range m.builds {
		if i < eagerTimelines {
			want[b.ID] = true
		}
	}
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(buildRow); ok {
			want[r.ID] = true
		}
	}

	var cmds []tea.Cmd
	for id := range want {
		if _, done := m.progress[id]; done || m.loading[id] {
			continue
		}
		m.loading[id] = true

		client, buildID := m.client, id
		cmds = append(cmds, func() tea.Msg {
			progress, records, err := client.Timeline(buildID)
			return timelineMsg{Build: buildID, Progress: progress, Records: records, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Builds) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case buildsMsg:
		m.builds, m.loaded = msg.Builds, true
		m.applyRows()
		return m, m.fetchTimelines()

	case timelineMsg:
		delete(m.loading, msg.Build)
		if msg.Err != nil {
			m.status, m.failed = fmt.Sprintf("could not load the timeline for build %d: %v", msg.Build, msg.Err), true
			return m, nil
		}
		m.progress[msg.Build] = msg.Progress
		m.records[msg.Build] = msg.Records
		m.applyRows()
		return m, nil

	case ErrMsg:
		m.status, m.failed = msg.Err.Error(), true
		return m, nil

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.browser.Filtering() {
			break
		}

		row, hasRow := m.browser.Selected()
		if hasRow {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status, false
				return m, nil
			}
		}

		switch msg.String() {
		case "enter":
			r, ok := row.(buildRow)
			if !hasRow || !ok {
				return m, nil
			}
			logs := NewLogs(m.client, r.Build, m.records[r.ID])
			return m, func() tea.Msg { return PushMsg{View: logs} }

		case "r":
			m.progress = map[int]azdo.Progress{}
			m.records = map[int][]azdo.Record{}
			m.loading = map[int]bool{}
			m.status, m.failed = "refreshing…", false
			return m, m.Init()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchTimelines())
}

func (m *Builds) renderDetail(row Row, width int) string {
	r, ok := row.(buildRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(r.Label(), width)))
	for _, field := range [][2]string{
		{"status", r.Status.String()},
		{"step", r.stepColumn()},
		{"branch", shortRef(r.SourceBranch)},
		{"queued", humanAge(r.Queued, r.now) + " ago"},
		{"by", orDash(r.RequestedFor)},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-8s", field[0]+":")),
			truncate(field[1], width-9))
	}

	records := m.records[r.ID]
	if len(records) == 0 {
		fmt.Fprintf(&b, "\n%s\n", chromeStyle.Render("loading the timeline…"))
		return b.String()
	}

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("steps"))
	for _, rec := range records {
		if rec.Type != "Task" {
			continue
		}
		marker := "·"
		style := chromeStyle
		switch {
		case rec.Result == "failed":
			marker, style = "✗", errStyle
		case rec.State == "inProgress":
			marker, style = "◐", warnStyle
		case rec.Result == "succeeded":
			marker, style = "✓", statusStyle
		}
		fmt.Fprintf(&b, "%s %s\n", style.Render(marker), truncate(rec.Name, width-2))
	}
	return b.String()
}

func (m *Builds) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return chromeStyle.Render("fetching builds…")
	}
	return m.browser.View()
}

func (m *Builds) Title() string {
	return fmt.Sprintf("builds (%d) · %s/%s", m.browser.Len(), m.client.Org, m.client.Project)
}

func (m *Builds) Hints() string {
	return "enter logs · r refresh · " + SharedHints + " · esc back"
}

func (m *Builds) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.status, m.failed
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v`

Expected: still a compile failure until Task 14 supplies `NewLogs`. Write Task
14 next and run both suites together, or stub `NewLogs` now and fill it in
there. Prefer writing Task 14 immediately — the two are one commit's worth of
work split for reviewability.

- [ ] **Step 5: Commit (after Task 14 compiles)**

```bash
git add internal/ui/builds.go internal/ui/builds_test.go
git commit -m "feat(ui): add the build view"
```

---

### Task 14: The log pane with tailing

**Files:**
- Create: `internal/ui/logs.go`
- Create: `internal/ui/logs_test.go`

**Interfaces:**
- Consumes: `View`, `PopMsg`, `ErrMsg` from Task 9; `azdo.Build`, `Record`, `LogChunk`, `LogCursor`, `NewLogChunks`, `BuildByID`, `Timeline`, `BuildURL` from Tasks 7 and 8.
- Produces:
  - `type Logs struct { ... }` implementing `View`
  - `func NewLogs(c *azdo.Client, b azdo.Build, records []azdo.Record) *Logs`
  - `type logChunksMsg struct { Chunks []azdo.LogChunk; Status azdo.BuildStatus; Records []azdo.Record; Err error }`
  - `type tailTickMsg struct{}`
  - `const tailInterval = 3 * time.Second`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/logs_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func newLogs(t *testing.T, status azdo.BuildStatus) *Logs {
	t.Helper()
	c := &azdo.Client{Org: "acme", Project: "Platform"}
	build := azdo.Build{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: status}
	m := NewLogs(c, build, []azdo.Record{{Name: "Build", Type: "Task", Order: 1, LogID: 7}})
	m.Body(120, 20)
	return m
}

func TestLogsRendersChunksWithTaskRules(t *testing.T) {
	m := newLogs(t, azdo.StatusSucceeded)

	updated, _ := m.Update(logChunksMsg{
		Status: azdo.StatusSucceeded,
		Chunks: []azdo.LogChunk{
			{Task: "Restore", LogID: 6, Lines: []string{"restoring"}},
			{Task: "Build", LogID: 7, Lines: []string{"compiling", "done"}},
		},
	})
	m = updated.(*Logs)

	view := m.Body(120, 20)
	for _, want := range []string{"Restore", "restoring", "Build", "compiling", "done"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestLogsAppendsWithoutRepeatingTheRule(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, _ := m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"first"}}}})
	m = updated.(*Logs)
	updated, _ = m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"second"}}}})
	m = updated.(*Logs)

	view := m.Body(120, 40)
	if strings.Count(view, "── Build ──") != 1 {
		t.Errorf("the task rule was repeated on an append:\n%s", view)
	}
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Errorf("appended lines are missing:\n%s", view)
	}
}

func TestLogsKeepsTailingWhileTheBuildRuns(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	_, cmd := m.Update(logChunksMsg{Status: azdo.StatusRunning})
	if cmd == nil {
		t.Fatal("a running build did not schedule another poll")
	}
}

func TestLogsStopsTailingWhenTheBuildFinishes(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, cmd := m.Update(logChunksMsg{Status: azdo.StatusSucceeded})
	m = updated.(*Logs)

	if cmd != nil {
		t.Error("a finished build scheduled another poll")
	}
	if !strings.Contains(m.Title(), "succeeded") {
		t.Errorf("title = %q, want the final status", m.Title())
	}
	if !strings.Contains(m.Hints(), "r refresh") {
		t.Errorf("hints = %q, want refresh offered once tailing stops", m.Hints())
	}
}

func TestLogsTickFetches(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	_, cmd := m.Update(tailTickMsg{})
	if cmd == nil {
		t.Error("a tick did not fetch")
	}
}

func TestLogsEscapePops(t *testing.T) {
	m := newLogs(t, azdo.StatusSucceeded)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced no command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("escape produced %T, want a PopMsg", cmd())
	}
}

func TestLogsErrorIsReportedWithoutLosingTheText(t *testing.T) {
	m := newLogs(t, azdo.StatusRunning)

	updated, _ := m.Update(logChunksMsg{Status: azdo.StatusRunning,
		Chunks: []azdo.LogChunk{{Task: "Build", LogID: 7, Lines: []string{"compiling"}}}})
	m = updated.(*Logs)
	updated, _ = m.Update(logChunksMsg{Err: errTest})
	m = updated.(*Logs)

	if _, isErr := m.Status(); !isErr {
		t.Error("a log fetch failure was not reported")
	}
	if !strings.Contains(m.Body(120, 20), "compiling") {
		t.Error("the log text was lost when a poll failed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestLogs -v`

Expected: compile failure — `NewLogs`, `Logs`, `logChunksMsg`, `tailTickMsg` are
undefined.

- [ ] **Step 3: Write the view**

Create `internal/ui/logs.go`:

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// tailInterval is how often a running build's logs are re-read. Three seconds
// keeps the pane feeling live without hammering the API for a build whose steps
// take minutes.
const tailInterval = 3 * time.Second

type logChunksMsg struct {
	Chunks  []azdo.LogChunk
	Status  azdo.BuildStatus
	Records []azdo.Record
	Err     error
}

type tailTickMsg struct{}

// Logs pages through one build's logs, appending while the build runs.
type Logs struct {
	client *azdo.Client
	build  azdo.Build

	viewport viewport.Model
	records  []azdo.Record
	cursor   azdo.LogCursor

	// lastTask is the task whose rule was written most recently, so an append
	// to the same log does not repeat the heading.
	lastTask string
	text     strings.Builder

	status string
	failed bool
}

func NewLogs(c *azdo.Client, b azdo.Build, records []azdo.Record) *Logs {
	return &Logs{
		client:   c,
		build:    b,
		viewport: viewport.New(0, 0),
		records:  records,
		cursor:   azdo.LogCursor{},
	}
}

// Init reads the log from the beginning.
func (m *Logs) Init() tea.Cmd { return m.fetch() }

// fetch reads whatever the logs have gained, and refreshes the timeline so a
// build that has started new tasks picks up their logs too.
func (m *Logs) fetch() tea.Cmd {
	client, build, cursor := m.client, m.build, m.cursor
	records := m.records

	return func() tea.Msg {
		// A running build grows new records, and a record that had no log id
		// when the pane opened may have one now.
		if !build.Status.Done() {
			if _, fresh, err := client.Timeline(build.ID); err == nil {
				records = fresh
			}
		}

		chunks, err := client.NewLogChunks(build.ID, records, cursor)
		if err != nil {
			return logChunksMsg{Err: fmt.Errorf("could not read the build log: %w", err)}
		}

		current, err := client.BuildByID(build.ID)
		if err != nil {
			return logChunksMsg{Chunks: chunks, Records: records, Status: build.Status}
		}
		return logChunksMsg{Chunks: chunks, Records: records, Status: current.Status}
	}
}

func tailTick() tea.Cmd {
	return tea.Tick(tailInterval, func(time.Time) tea.Msg { return tailTickMsg{} })
}

func (m *Logs) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case logChunksMsg:
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			// A failed poll is not a reason to stop watching a running build.
			if !m.build.Status.Done() {
				return m, tailTick()
			}
			return m, nil
		}

		if msg.Records != nil {
			m.records = msg.Records
		}
		m.append(msg.Chunks)
		m.build.Status = msg.Status
		m.status, m.failed = "", false

		if m.build.Status.Done() {
			return m, nil
		}
		return m, tailTick()

	case tailTickMsg:
		return m, m.fetch()

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return PopMsg{} }
		case "g":
			m.viewport.GotoTop()
			return m, nil
		case "G":
			m.viewport.GotoBottom()
			return m, nil
		case "r":
			m.status, m.failed = "refreshing…", false
			return m, m.fetch()
		case "o":
			OpenBrowser(m.client.BuildURL(m.build.ID))
			m.status, m.failed = "opened the build in a browser", false
			return m, nil
		case "y":
			CopyToClipboard(fmt.Sprint(m.build.ID))
			m.status, m.failed = fmt.Sprintf("copied id %d", m.build.ID), false
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// append writes new chunks, heading each task's output with a rule the first
// time that task is seen.
func (m *Logs) append(chunks []azdo.LogChunk) {
	if len(chunks) == 0 {
		return
	}

	atBottom := m.viewport.AtBottom()
	for _, c := range chunks {
		if c.Task != m.lastTask {
			fmt.Fprintf(&m.text, "\n%s\n", chromeStyle.Render("── "+c.Task+" ──"))
			m.lastTask = c.Task
		}
		for _, line := range c.Lines {
			m.text.WriteString(line)
			m.text.WriteByte('\n')
		}
	}

	m.viewport.SetContent(m.text.String())
	// Following the tail is only useful if the reader has not scrolled away.
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Logs) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height
	if m.text.Len() == 0 {
		return chromeStyle.Render("fetching the build log…")
	}
	return m.viewport.View()
}

func (m *Logs) Title() string {
	return fmt.Sprintf("%s #%s · %s · %s/%s",
		m.build.Pipeline, m.build.Number, m.build.Status, m.client.Org, m.client.Project)
}

func (m *Logs) Hints() string {
	if m.build.Status.Done() {
		return "g/G top/bottom · r refresh · y copy id · o open · esc back"
	}
	return "g/G top/bottom · tailing · y copy id · o open · esc back"
}

func (m *Logs) Status() (string, bool) { return m.status, m.failed }
```

The spec's `/` search inside the log pane is dropped: the viewport widget has no
search, and writing one is a feature of its own rather than part of this view.
`g`, `G`, and the scroll keys carry the navigation. Note this in the README's
key table by simply not listing a search key.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && go vet ./internal/...`

Expected: PASS, including Task 13's build tests now that `NewLogs` exists.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/logs.go internal/ui/logs_test.go internal/ui/builds.go internal/ui/builds_test.go
git commit -m "feat(ui): add the build log pane with tailing"
```

---

### Task 15: The banner, the menu, and the root model

**Files:**
- Create: `internal/ui/banner.go`
- Create: `internal/ui/root.go`
- Create: `internal/ui/root_test.go`

**Interfaces:**
- Consumes: every view from Tasks 10 to 14; every message type from Task 9.
- Produces:
  - `type Root struct { ... }` implementing `tea.Model`
  - `func NewRoot(c *azdo.Client, items []azdo.WorkItem, mineOnly bool, start string) *Root`
  - `func (r Root) ShellCommand() string`
  - `const Banner string`
  - `type initView interface { Init() tea.Cmd }`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/root_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newRoot(t *testing.T, start string) *Root {
	t.Helper()
	c, items := fixture()
	r := NewRoot(c, items, false, start)
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return updated.(*Root)
}

func TestRootShowsTheBannerAndMenu(t *testing.T) {
	r := newRoot(t, "")
	view := r.View()

	for _, want := range []string{"work items", "pull requests", "builds"} {
		if !strings.Contains(view, want) {
			t.Errorf("the menu is missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, strings.Split(Banner, "\n")[1]) {
		t.Error("the banner did not render")
	}
}

func TestRootMenuOpensTheChosenView(t *testing.T) {
	r := newRoot(t, "")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r = updated.(*Root)

	if !strings.Contains(r.View(), "work items (all 3)") {
		t.Errorf("enter on the first entry did not open work items:\n%s", r.View())
	}
}

func TestRootMenuMovesBetweenEntries(t *testing.T) {
	r := newRoot(t, "")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyDown})
	r = updated.(*Root)
	updated, _ = r.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r = updated.(*Root)

	if !strings.Contains(r.View(), "pull requests") {
		t.Errorf("the second entry did not open pull requests:\n%s", r.View())
	}
}

func TestRootStartJumpsStraightToAView(t *testing.T) {
	r := newRoot(t, "prs")

	if !strings.Contains(r.View(), "pull requests") {
		t.Errorf("start=prs did not open the pull request view:\n%s", r.View())
	}
}

func TestRootEscapeReturnsToTheMenu(t *testing.T) {
	r := newRoot(t, "items")

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	r = updated.(*Root)

	if !strings.Contains(r.View(), "pull requests") || strings.Contains(r.View(), "^t mine/all") {
		t.Errorf("escape did not return to the menu:\n%s", r.View())
	}
}

func TestRootEscapeOnTheMenuQuits(t *testing.T) {
	r := newRoot(t, "")

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape on the menu produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("escape on the menu produced %T, want a quit", cmd())
	}
}

func TestRootPushAndPop(t *testing.T) {
	r := newRoot(t, "items")

	pushed := NewLogs(r.client, buildStub(), nil)
	updated, _ := r.Update(PushMsg{View: pushed})
	r = updated.(*Root)
	if !strings.Contains(r.View(), "platform-ci") {
		t.Errorf("the pushed view did not render:\n%s", r.View())
	}

	updated, _ = r.Update(PopMsg{})
	r = updated.(*Root)
	if !strings.Contains(r.View(), "work items") {
		t.Errorf("pop did not return to the view underneath:\n%s", r.View())
	}
}

func TestRootRendersChromeAroundTheView(t *testing.T) {
	r := newRoot(t, "items")
	view := r.View()

	if !strings.Contains(view, "acme/Platform") {
		t.Error("the header is missing")
	}
	if !strings.Contains(view, "esc back") {
		t.Error("the hint line is missing")
	}
}

func TestRootShellCommandQuits(t *testing.T) {
	r := newRoot(t, "items")

	updated, cmd := r.Update(ShellCommandMsg{Command: "git checkout feature/x"})
	r = updated.(*Root)

	if r.ShellCommand() != "git checkout feature/x" {
		t.Errorf("ShellCommand = %q", r.ShellCommand())
	}
	if cmd == nil {
		t.Fatal("a shell command did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("a shell command produced %T, want a quit", cmd())
	}
}

func TestRootQuitsOnQFromTheMenu(t *testing.T) {
	r := newRoot(t, "")

	_, cmd := r.Update(runes("q"))
	if cmd == nil {
		t.Fatal("q on the menu produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want a quit", cmd())
	}
}
```

Add the build stub the push test needs:

```go
func buildStub() azdo.Build {
	return azdo.Build{ID: 9001, Number: "20260911.3", Pipeline: "platform-ci", Status: azdo.StatusSucceeded}
}
```

with the `azdo` import.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestRoot -v`

Expected: compile failure — `Root`, `NewRoot`, `Banner` are undefined.

- [ ] **Step 3: Write the banner**

Create `internal/ui/banner.go`:

```go
package ui

import "strings"

// Banner is the landing screen's wordmark. It is a literal rather than a
// generated figlet so the binary needs no font data and the shape cannot drift.
const Banner = `
 ██████╗  ██████╗  █████╗ ██████╗ ██████╗ ██╗    ██╗ █████╗ ██╗     ██╗  ██╗
 ██╔══██╗██╔═══██╗██╔══██╗██╔══██╗██╔══██╗██║    ██║██╔══██╗██║     ██║ ██╔╝
 ██████╔╝██║   ██║███████║██████╔╝██║  ██║██║ █╗ ██║███████║██║     █████╔╝
 ██╔══██╗██║   ██║██╔══██║██╔══██╗██║  ██║██║███╗██║██╔══██║██║     ██╔═██╗
 ██████╔╝╚██████╔╝██║  ██║██║  ██║██████╔╝╚███╔███╔╝██║  ██║███████╗██║  ██╗
 ╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝`

// renderBanner colours the wordmark, fading the lower rows so it reads as one
// object rather than a wall of the same colour. A terminal too narrow for the
// full width gets the plain name instead of a mangled one.
func renderBanner(width int) string {
	lines := strings.Split(strings.TrimLeft(Banner, "\n"), "\n")
	if width < 80 {
		return bannerRow.Render("boardwalk")
	}

	shades := []string{"39", "38", "37", "36", "30", "23"}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = bannerRow.Foreground(shade(shades, i)).Render(line)
	}
	return strings.Join(out, "\n")
}
```

Add the shade helper to `style.go`:

```go
// padRight pads to a display width, measuring with lipgloss so ANSI styling does
// not count. A styled cell cannot go through %-Ns: the escape bytes would eat
// the padding.
func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// shade picks the nth colour of a gradient, holding the last one for any row
// beyond the list.
func shade(shades []string, i int) lipgloss.Color {
	if i >= len(shades) {
		i = len(shades) - 1
	}
	return lipgloss.Color(shades[i])
}
```

- [ ] **Step 4: Write the root model**

Create `internal/ui/root.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

// initView is the optional initialiser a view implements when it has data to
// fetch on entry. Root calls it when the view is pushed rather than at program
// start, so the menu paints without waiting on the network.
type initView interface{ Init() tea.Cmd }

// menuEntry is one line of the landing menu.
type menuEntry struct {
	key   string // the -start name and the subcommand name
	label string
	blurb string
}

var menuEntries = []menuEntry{
	{"items", "work items", "browse, branch and set state"},
	{"prs", "pull requests", "drafts, branches, comments and age"},
	{"builds", "builds", "pipeline runs, current step and logs"},
}

// Root owns the menu, the view stack, and every piece of chrome. Views render
// only their own body.
type Root struct {
	client   *azdo.Client
	items    []azdo.WorkItem
	mineOnly bool

	stack  []View
	choice int

	width, height int

	// command is printed on exit for the shell wrapper to put on the prompt.
	command string
}

func NewRoot(c *azdo.Client, items []azdo.WorkItem, mineOnly bool, start string) *Root {
	r := &Root{client: c, items: items, mineOnly: mineOnly}
	if start != "" {
		if v := r.build(start); v != nil {
			r.stack = append(r.stack, v)
		}
	}
	return r
}

// Init runs the starting view's fetch, if boardwalk was launched straight into
// one.
func (r *Root) Init() tea.Cmd {
	if len(r.stack) == 0 {
		return nil
	}
	return initialise(r.stack[len(r.stack)-1])
}

// ShellCommand is what to print after the program exits, or an empty string.
func (r *Root) ShellCommand() string { return r.command }

func (r *Root) build(name string) View {
	switch name {
	case "items":
		return NewWorkItems(r.client, r.items, r.mineOnly)
	case "prs":
		return NewPullRequests(r.client)
	case "builds":
		return NewBuilds(r.client)
	default:
		return nil
	}
}

func initialise(v View) tea.Cmd {
	if iv, ok := v.(initView); ok {
		return iv.Init()
	}
	return nil
}

func (r *Root) top() (View, bool) {
	if len(r.stack) == 0 {
		return nil, false
	}
	return r.stack[len(r.stack)-1], true
}

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		return r, nil

	case PushMsg:
		r.stack = append(r.stack, msg.View)
		return r, initialise(msg.View)

	case PopMsg:
		if len(r.stack) > 0 {
			r.stack = r.stack[:len(r.stack)-1]
		}
		return r, nil

	case ShellCommandMsg:
		r.command = msg.Command
		return r, tea.Quit

	case tea.KeyMsg:
		if cmd, handled := r.key(msg); handled {
			return r, cmd
		}
	}

	top, ok := r.top()
	if !ok {
		return r, nil
	}

	updated, cmd := top.Update(msg)
	r.stack[len(r.stack)-1] = updated
	return r, cmd
}

// key handles the bindings Root owns. Everything else falls through to the view
// on top, and nothing is intercepted while a view has a text prompt open —
// which is why the view is asked for its status first.
func (r *Root) key(msg tea.KeyMsg) (tea.Cmd, bool) {
	top, hasView := r.top()

	if !hasView {
		switch msg.String() {
		case "up", "k":
			r.choice = (r.choice - 1 + len(menuEntries)) % len(menuEntries)
			return nil, true
		case "down", "j":
			r.choice = (r.choice + 1) % len(menuEntries)
			return nil, true
		case "enter":
			v := r.build(menuEntries[r.choice].key)
			if v == nil {
				return nil, true
			}
			r.stack = append(r.stack, v)
			return initialise(v), true
		case "q", "esc", "ctrl+c":
			return tea.Quit, true
		}
		return nil, false
	}

	// A view that is taking typed input owns every key, including esc and q.
	if typing(top) {
		return nil, false
	}

	switch msg.String() {
	case "esc":
		r.stack = r.stack[:len(r.stack)-1]
		return nil, true
	case "ctrl+c":
		return tea.Quit, true
	case "q":
		// A drill-down treats q as "go back"; from a top-level view it quits.
		if len(r.stack) > 1 {
			r.stack = r.stack[:len(r.stack)-1]
			return nil, true
		}
		return tea.Quit, true
	}
	return nil, false
}

// typing reports whether the view on top has a prompt open — the fuzzy filter
// or the branch name editor — in which case Root must not claim esc or q.
func typing(v View) bool {
	type prompter interface{ Prompting() bool }
	if p, ok := v.(prompter); ok {
		return p.Prompting()
	}
	return false
}

func (r *Root) View() string {
	if r.width == 0 {
		return "loading…"
	}

	top, ok := r.top()
	if !ok {
		return r.menuView()
	}

	// header, a blank line, hints, status
	body := top.Body(r.width, max(1, r.height-4))

	status, isErr := top.Status()
	style := statusStyle
	if isErr {
		style = errStyle
	}

	return strings.Join([]string{
		chromeStyle.Render(top.Title()),
		body,
		chromeStyle.Render(top.Hints()),
		style.Render(truncate(status, r.width)),
	}, "\n")
}

func (r *Root) menuView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", renderBanner(r.width))

	for i, e := range menuEntries {
		line := fmt.Sprintf("%-16s %s", e.label, chromeStyle.Render(e.blurb))
		if i == r.choice {
			fmt.Fprintf(&b, "%s\n", menuPicked.Render("▸ "+line))
			continue
		}
		fmt.Fprintf(&b, "%s\n", menuItem.Render(line))
	}

	fmt.Fprintf(&b, "\n%s\n", chromeStyle.Render(
		fmt.Sprintf("%s/%s · ↑↓ move · enter open · q quit", r.client.Org, r.client.Project)))
	return b.String()
}
```

- [ ] **Step 5: Expose the prompting state on the views**

`Root.typing` needs each view to say when a prompt is open. Add to
`internal/ui/workitems.go`:

```go
// Prompting reports whether a text prompt is open, so Root leaves esc and q to
// the prompt rather than treating them as navigation.
func (m *WorkItems) Prompting() bool {
	return m.branchPrompt != nil || m.browser.Filtering()
}
```

To `internal/ui/pullrequests.go` and `internal/ui/builds.go`:

```go
func (m *PullRequests) Prompting() bool { return m.browser.Filtering() }
```

```go
func (m *Builds) Prompting() bool { return m.browser.Filtering() }
```

`Logs` needs none — it has no prompt and handles esc itself.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -v && go vet ./internal/...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/banner.go internal/ui/root.go internal/ui/root_test.go internal/ui/style.go internal/ui/workitems.go internal/ui/pullrequests.go internal/ui/builds.go
git commit -m "feat(ui): add the banner menu and the root view stack"
```

---

### Task 16: Subcommands, entry points, and the README

**Files:**
- Modify: `main.go` (rewrite `main` and `run`)
- Modify: `README.md`

**Interfaces:**
- Consumes: `ui.NewRoot`, `ui.Root.ShellCommand` from Task 15.
- Produces: the `boardwalk`, `boardwalk items`, `boardwalk prs`, and `boardwalk builds` entry points.

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		start string
		rest  []string
	}{
		{"no arguments opens the menu", nil, "", nil},
		{"a subcommand jumps straight in", []string{"prs"}, "prs", nil},
		{"items is a subcommand too", []string{"items"}, "items", nil},
		{"builds is a subcommand", []string{"builds"}, "builds", nil},
		{"a flag is not a subcommand", []string{"-mine"}, "", []string{"-mine"}},
		{"flags after a subcommand survive", []string{"items", "-mine"}, "items", []string{"-mine"}},
		{"an unknown word is left to the flag parser", []string{"nonsense"}, "", []string{"nonsense"}},
	} {
		start, rest := parseArgs(tc.args)
		if start != tc.start {
			t.Errorf("%s: start = %q, want %q", tc.name, start, tc.start)
		}
		if len(rest) != len(tc.rest) {
			t.Errorf("%s: rest = %v, want %v", tc.name, rest, tc.rest)
		}
	}
}

func TestStartForFlags(t *testing.T) {
	// -mine and -all only make sense against work items, so they imply it.
	if got := startFor("", true, false); got != "items" {
		t.Errorf("startFor with -mine = %q, want items", got)
	}
	if got := startFor("", false, true); got != "items" {
		t.Errorf("startFor with -all = %q, want items", got)
	}
	if got := startFor("prs", true, false); got != "prs" {
		t.Errorf("an explicit subcommand = %q, want it respected", got)
	}
	if got := startFor("", false, false); got != "" {
		t.Errorf("startFor with no flags = %q, want the menu", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run 'TestParseArgs|TestStartFor' -v`

Expected: compile failure — `parseArgs` and `startFor` are undefined.

- [ ] **Step 3: Rewrite main.go**

```go
// boardwalk — a terminal browser for Azure DevOps.
//
//	boardwalk            # the menu: work items, pull requests, builds
//	boardwalk items      # straight to work items
//	boardwalk prs        # straight to pull requests
//	boardwalk builds     # straight to pipeline builds
//	boardwalk -mine      # work items assigned to you
//	boardwalk -all       # include closed/done/resolved/removed
//	boardwalk -dump      # print work item rows and exit, no TUI
//
// Reads AZDO_ORG and AZDO_PROJECT, and borrows the machine's existing
// `az login` session for its token.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is overridden at build time: -ldflags "-X main.version=$(git describe)"
var version = "dev"

// subcommands are the views boardwalk can open directly, skipping the menu.
var subcommands = map[string]bool{"items": true, "prs": true, "builds": true}

func main() {
	start, rest := parseArgs(os.Args[1:])

	fs := flag.NewFlagSet("boardwalk", flag.ExitOnError)
	var (
		mineOnly    = fs.Bool("mine", false, "start filtered to items assigned to you")
		all         = fs.Bool("all", false, "include closed/done/resolved/removed")
		dump        = fs.Bool("dump", false, "print work item rows and exit, no TUI")
		timing      = fs.Bool("timing", false, "report fetch duration on stderr")
		showVersion = fs.Bool("version", false, "print version and exit")
	)
	fs.Parse(rest)

	if *showVersion {
		fmt.Println("boardwalk", version)
		return
	}

	if err := run(startFor(start, *mineOnly, *all), *mineOnly, *all, *dump, *timing); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}

// parseArgs peels a leading subcommand off the arguments, leaving the rest for
// the flag package. Writing it by hand rather than reaching for a CLI library
// keeps the dependency list where it is.
func parseArgs(args []string) (start string, rest []string) {
	if len(args) > 0 && subcommands[args[0]] {
		return args[0], args[1:]
	}
	return "", args
}

// startFor decides which view to open. -mine and -all describe work items, so
// either implies that view when no subcommand was given.
func startFor(start string, mineOnly, all bool) string {
	if start != "" {
		return start
	}
	if mineOnly || all {
		return "items"
	}
	return ""
}

func run(start string, mineOnly, all, dump, timing bool) error {
	org, project := os.Getenv("AZDO_ORG"), os.Getenv("AZDO_PROJECT")
	if org == "" || project == "" {
		return fmt.Errorf("AZDO_ORG and AZDO_PROJECT must be set")
	}

	client, err := azdo.NewClient(org, project)
	if err != nil {
		return err
	}

	// Work items are the one view whose data is fetched before the program
	// starts: the whole project comes back in one pass, and -dump needs it
	// without a TUI at all. The other views fetch on entry.
	var items []azdo.WorkItem
	if dump || start == "items" || start == "" {
		fetchStart := time.Now()
		if items, err = client.WorkItems(all); err != nil {
			return fmt.Errorf("could not fetch work items: %w", err)
		}
		if timing {
			fmt.Fprintf(os.Stderr, "fetched %d work items in %s\n",
				len(items), time.Since(fetchStart).Round(time.Millisecond))
		}
	}

	if dump {
		if mineOnly {
			items = azdo.MineOf(items, client.Me)
		}
		for _, wi := range items {
			fmt.Printf("%-7d %-18s %-16s %-20s %s\n", wi.ID, "["+wi.Type+"]", wi.State, wi.Assigned, wi.Title)
		}
		return nil
	}

	root := ui.NewRoot(client, items, mineOnly, start)
	final, err := tea.NewProgram(root, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}

	// Anything the view wants the parent shell to run comes back on stdout for
	// the shell wrapper to put on the prompt.
	if r, ok := final.(*ui.Root); ok && r.ShellCommand() != "" {
		fmt.Println(r.ShellCommand())
	}
	return nil
}
```

- [ ] **Step 4: Run the whole suite**

Run: `make test && make vet && make build`

Expected: PASS, no vet output, a binary produced.

- [ ] **Step 5: Check it against a real board**

Run, with `AZDO_ORG` and `AZDO_PROJECT` set and `az login` current:

```sh
./boardwalk            # banner, menu, arrow keys, enter
./boardwalk prs        # branches, counts, ages; d cycles drafts
./boardwalk builds     # status and step; enter opens logs
./boardwalk -dump | head
```

Confirm: the banner renders, each view loads, `esc` returns to the menu, `y`,
`s` and `o` work on each view, and a running build's log pane grows on its own.

- [ ] **Step 6: Update the README**

Replace the sample session with one showing the menu, then replace the Usage and
key sections:

````markdown
## Usage

```sh
boardwalk              # the menu
boardwalk items        # straight to work items
boardwalk prs          # straight to pull requests
boardwalk builds       # straight to pipeline builds
boardwalk -mine        # work items assigned to you
boardwalk -all         # include closed/done/resolved/removed
boardwalk -dump        # print work item rows and exit — for scripts and pipes
boardwalk -timing      # report fetch duration on stderr
```

### Everywhere

| key | |
|---|---|
| `/` | fuzzy filter |
| `y` | copy the id |
| `s` | copy a Slack message — `[#4021 title](link)` |
| `o` | open in the browser |
| `esc` | back to the menu |
| `q` | quit |

### Work items

| key | |
|---|---|
| `^t` | toggle between everyone's items and yours |
| `a` | set the item Active |
| `b` | create a branch, set the item Active, and link the branch to it |

### Pull requests

| key | |
|---|---|
| `d` | cycle drafts hidden → drafts only → all |
| `^t` | toggle between this repository and the whole project |

### Builds

| key | |
|---|---|
| `enter` | open the logs |
| `r` | refetch |

### Logs

| key | |
|---|---|
| `g` / `G` | top / bottom |
| `r` | refetch |

A running build's logs append on their own every few seconds, and stop when the
build finishes.
````

Replace the Roadmap section, since all three of its entries are now done:

```markdown
## Roadmap

- A repository picker for the branch flow, for running boardwalk outside a repo
- Search inside the log pane
- Pull request creation from a work item's branch
```

- [ ] **Step 7: Commit**

```bash
git add main.go main_test.go README.md
git commit -m "feat: add subcommands and wire up the multi-view root"
```

---

## Self-review notes

Two deliberate departures from the spec, both narrowing rather than widening:

- **The branch flow's repository picker** (Task 11) reports what to do instead
  of opening a second interactive list when the working directory is not one of
  the project's repositories. The picker is on the README's roadmap.
- **Search inside the log pane** (Task 14) is dropped. The viewport widget has
  no search and writing one is a feature of its own. Also on the roadmap.

One implementation refinement: the spec describes tailing by byte offset; the
API takes a `startLine`, so the cursor is a line count (Task 8).
