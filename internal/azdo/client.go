// Package azdo is a small Azure DevOps REST client.
//
// It deliberately does not implement an auth flow. Tokens come from the az CLI,
// so boardwalk inherits whatever `az login` session the machine already has.
package azdo

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func (c *Client) post(url string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, apiError(resp.Body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// apiError pulls the human-readable half out of an Azure DevOps error body.
func apiError(r interface{ Read([]byte) (int, error) }) string {
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r).Decode(&body); err == nil && body.Message != "" {
		return body.Message
	}
	return "no error detail returned"
}
