// Package azdo is a small Azure DevOps REST client.
//
// It deliberately does not implement an auth flow. Tokens come from the az CLI,
// so boardwalk inherits whatever `az login` session the machine already has.
package azdo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Resource is the Azure DevOps application id. Asking the az CLI for a token
// against it yields a bearer token the REST API accepts.
const Resource = "499b84ac-1321-427f-aa17-267ca6975798"

// APIVersion is pinned so a server-side default bump cannot change payloads
// underneath us.
const APIVersion = "7.1"

// connectionDataAPIVersion is APIVersion's preview twin. connectionData is the
// one endpoint boardwalk calls that is still preview-only at 7.1: asking for
// the plain version comes back as "the -preview flag must be supplied in the
// api-version for such requests".
const connectionDataAPIVersion = APIVersion + "-preview"

type Client struct {
	Org     string
	Project string

	// Me is the signed-in user's email, lowercased, used to tell "mine" from
	// everyone else's without a second round trip.
	Me string

	// MyID is the signed-in user's Azure DevOps identity GUID, which is how
	// the vote endpoint addresses a reviewer — an email does not work there.
	// It is best-effort: empty when connectionData could not be reached, in
	// which case only a pull request that names me directly carries an id I
	// can vote with.
	MyID string

	// ReviewGroups are the teams and security groups the signed-in user
	// declared in the config file. They are an override rather than the only
	// source now: the Graph API resolves memberships on its own at startup,
	// and a name written here is honoured on top of whatever that found — so
	// a tenant whose Graph the token cannot read works exactly as it did.
	ReviewGroups []string

	// groups is what Graph resolved at startup, and groupsLoaded says
	// whether it got that far. They are separate because an empty Groups is
	// ambiguous on its own: a person who genuinely belongs to no group and a
	// Graph that refused the request look identical, and only one of those
	// is worth mentioning.
	groups       Groups
	groupsLoaded bool
	// groupsErr and idErr are why the two startup lookups did not happen,
	// kept rather than discarded. Both failures are survivable and neither
	// is worth stopping for — but both look from the outside like "you are
	// in no groups" and "you cannot vote", which is the same thing an
	// ordinary, correct session looks like. The session view exists to tell
	// those apart, and it can only do that if the reason is still here.
	groupsErr error
	idErr     error

	// mu guards token, which outlives the token itself: an az token is good
	// for about an hour and boardwalk is meant to be left open longer than
	// that, so send replaces it mid-session. Requests run on several
	// goroutines, so the swap needs a lock.
	mu    sync.Mutex
	token string

	// newToken mints a replacement. It is a field so tests can hand over a
	// token without an az install, the same way baseURL stands in for the
	// host; production leaves it nil and falls back to the az CLI.
	newToken func() (string, error)

	http *http.Client

	// baseURL overrides https://dev.azure.com in tests. Empty in production.
	baseURL string

	// states caches each work item type's states for the session. See
	// States and stateCache's own doc for why that is safe to hold onto.
	states stateCache
}

func NewClient(org, project string) (*Client, error) {
	if org == "" || project == "" {
		return nil, fmt.Errorf("organization and project are both required")
	}

	token, err := azToken()
	if err != nil {
		return nil, fmt.Errorf("could not get an Azure DevOps token (is `az login` current?): %w", err)
	}

	// Not fatal: without it every item simply reads as someone else's.
	me, _ := az("account", "show", "--query", "user.name", "-o", "tsv")

	c := &Client{
		Org:     org,
		Project: project,
		Me:      strings.ToLower(me),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}

	// Also not fatal: without it, voting still works wherever the pull
	// request names me directly, which is where it worked before there was
	// an identity to fall back on.
	_ = c.loadMyID()

	// Nor is this: without it, a group reviewer is only recognised when it
	// was named in the config, which is where boardwalk was before Graph was
	// asked at all.
	//
	// It runs here, before the program starts, rather than lazily on first
	// use. NeedsReviewFrom is called from a list filter on the UI goroutine,
	// and a few requests' worth of latency there would freeze the interface
	// — a mistake this codebase has already made once, with glamour.
	c.loadMyGroups()

	return c, nil
}

// loadMyGroups resolves the signed-in user's group memberships, keeping
// whatever came back. A failure is recorded as "not loaded" rather than as an
// empty answer: see the groups field.
func (c *Client) loadMyGroups() {
	groups, err := c.MyGroups()
	if err != nil {
		c.groupsErr = err
		return
	}
	c.groups, c.groupsLoaded, c.groupsErr = groups, true, nil
}

// Session is everything boardwalk worked out about this session at startup,
// gathered into one value.
//
// It exists so that the view showing it can be handed data rather than a
// client: the interesting states are the broken ones — no user name, a Graph
// that refused — and reaching those through a live client would mean either a
// fake server per case or test-only setters on Client. A struct literal says
// the same thing in one line and keeps the test scaffolding out of the
// production type.
type Session struct {
	Org, Project string
	// Me is the signed-in user's email, empty when az could not name one.
	Me string
	// MyID is the identity GUID, empty when connectionData could not be
	// reached; IdentityErr is why.
	MyID        string
	IdentityErr error
	// Groups is what the Graph walk resolved and GroupsResolved whether it
	// got that far; GroupsErr is why it did not. All three, because an empty
	// Groups means nothing on its own.
	Groups         Groups
	GroupsResolved bool
	GroupsErr      error
	// ReviewGroups is what the config file named, which is honoured on top
	// of whatever Graph found.
	ReviewGroups []string
}

// Session gathers what this client knows about itself.
func (c *Client) Session() Session {
	return Session{
		Org:            c.Org,
		Project:        c.Project,
		Me:             c.Me,
		MyID:           c.MyID,
		IdentityErr:    c.idErr,
		Groups:         c.groups,
		GroupsResolved: c.groupsLoaded,
		GroupsErr:      c.groupsErr,
		ReviewGroups:   c.ReviewGroups,
	}
}

// Resolve re-runs the two startup lookups and reports what they found. It is
// what the session view's refresh calls: a stale `az login` renewed in
// another shell should not need boardwalk restarted to be picked up.
func (c *Client) Resolve() Session {
	_ = c.loadMyID()
	c.loadMyGroups()
	return c.Session()
}

// ResolvedGroups is what the Graph walk found, for the session view to show.
func (c *Client) ResolvedGroups() Groups { return c.groups }

// GroupsResolved reports whether the walk got as far as an answer. An empty
// Groups is ambiguous on its own: somebody in no groups and a Graph that
// refused look identical.
func (c *Client) GroupsResolved() bool { return c.groupsLoaded }

// GroupsError is why the walk did not finish, or nil.
func (c *Client) GroupsError() error { return c.groupsErr }

// IdentityError is why connectionData did not name an authenticated user, or
// nil. Without one there is no id to cast a group's vote under.
func (c *Client) IdentityError() error { return c.idErr }

// loadMyID fetches the signed-in user's identity GUID.
//
// connectionData is the only endpoint that hands it over without already
// knowing it: every other route to an identity is keyed by the id itself or
// by a descriptor boardwalk does not have. One call at startup, so that
// voting does not depend on the pull request happening to name me.
func (c *Client) loadMyID() error {
	endpoint := fmt.Sprintf("%s/%s/_apis/connectionData?api-version=%s",
		c.root(), url.PathEscape(c.Org), connectionDataAPIVersion)

	var body struct {
		AuthenticatedUser struct {
			ID string `json:"id"`
		} `json:"authenticatedUser"`
	}
	if err := c.get(endpoint, &body); err != nil {
		c.idErr = err
		return err
	}
	if body.AuthenticatedUser.ID == "" {
		c.idErr = fmt.Errorf("connectionData named no authenticated user")
		return c.idErr
	}

	c.MyID, c.idErr = body.AuthenticatedUser.ID, nil
	return nil
}

func az(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("az", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("az %s: %s", strings.Join(args, " "), detail)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// root is the API host. Tests point it at an httptest server; everything else
// talks to Azure DevOps.
func (c *Client) root() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://dev.azure.com"
}

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

// put replaces a resource wholesale. The vote endpoint is the only caller so
// far: it treats a reviewer entry as something to overwrite, not append to.
func (c *Client) put(url string, body, out any) error {
	req, err := jsonRequest(http.MethodPut, url, "application/json", body)
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
//
// A token from az is good for about an hour, which is shorter than boardwalk
// is meant to be left open, so a session that was working goes on working
// only if the token is replaced underneath it. A rejected request is retried
// once against a fresh token rather than surfaced: the az login behind it is
// still current, and the user has nothing to do about an expiry they cannot
// see. When the refresh itself fails the login really is gone, and that is
// worth saying.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	stale := c.currentToken()
	resp, err := c.attempt(req, stale)
	if err != nil {
		return nil, err
	}

	if rejected(resp) {
		resp.Body.Close()
		fresh, err := c.refresh(stale)
		if err != nil {
			return nil, err
		}
		retry, err := replayable(req)
		if err != nil {
			return nil, err
		}
		if resp, err = c.attempt(retry, fresh); err != nil {
			return nil, err
		}
	}

	if rejected(resp) {
		defer resp.Body.Close()
		return nil, fmt.Errorf("%s: %s (is `az login` current?)", resp.Status, apiError(resp.Body))
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", resp.Status, apiError(resp.Body))
	}
	return resp, nil
}

// attempt runs one request under one token.
func (c *Client) attempt(req *http.Request, token string) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+token)
	return c.http.Do(req)
}

// rejected says whether a response is Azure DevOps refusing the credentials.
// It answers that two ways: a plain 401, and a 203 carrying the sign-in page,
// which is not an error status at all and would otherwise be decoded as the
// payload and fail as malformed JSON.
func rejected(resp *http.Response) bool {
	return resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusNonAuthoritativeInfo
}

// replayable copies a request that has already been sent, restoring the body
// a write consumed. Every request boardwalk builds carries a bytes.Reader, so
// net/http fills in GetBody and the copy is exact.
func replayable(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.GetBody == nil {
		return clone, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("could not resend the request after refreshing the token: %w", err)
	}
	clone.Body = body
	return clone, nil
}

// currentToken reads the token the next request should carry.
func (c *Client) currentToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// refresh replaces the token that was just refused and returns the new one.
//
// stale is the token the caller sent. When it no longer matches, a request on
// another goroutine hit the same expiry first and has already paid for a new
// one — asking az again would mint a second token for no reason.
func (c *Client) refresh(stale string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != stale {
		return c.token, nil
	}

	token, err := c.mint()
	if err != nil {
		return "", fmt.Errorf("could not refresh the Azure DevOps token (is `az login` current?): %w", err)
	}
	c.token = token
	return token, nil
}

// mint asks for a token, through the test seam when one is set.
func (c *Client) mint() (string, error) {
	if c.newToken != nil {
		return c.newToken()
	}
	return azToken()
}

// azToken asks the az CLI for a bearer token for the Azure DevOps API.
func azToken() (string, error) {
	return az("account", "get-access-token", "--resource", Resource, "--query", "accessToken", "-o", "tsv")
}

// apiError pulls the human-readable half out of an Azure DevOps error body.
func apiError(r io.Reader) string {
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r).Decode(&body); err == nil && body.Message != "" {
		return body.Message
	}
	return "no error detail returned"
}
