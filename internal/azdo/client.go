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

type Client struct {
	Org     string
	Project string

	// Me is the signed-in user's email, lowercased, used to tell "mine" from
	// everyone else's without a second round trip.
	Me string

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

	return &Client{
		Org:     org,
		Project: project,
		Me:      strings.ToLower(me),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
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
