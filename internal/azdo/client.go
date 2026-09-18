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
	// belongs to, from the config file. Azure DevOps never says who is in a
	// group it lists as a reviewer, so membership has to be declared. It
	// lives next to Me and MyID because it answers the same question — who
	// this session is — and every caller asking it already holds the client.
	ReviewGroups []string

	token string
	http  *http.Client

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

	token, err := az("account", "get-access-token", "--resource", Resource, "--query", "accessToken", "-o", "tsv")
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

	return c, nil
}

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
		return err
	}
	if body.AuthenticatedUser.ID == "" {
		return fmt.Errorf("connectionData named no authenticated user")
	}

	c.MyID = body.AuthenticatedUser.ID
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
