package azdo

import (
	"fmt"
	"net/url"
	"strings"
)

// The Identities API answers the one question a pull request's payload never
// does: who is in the group it lists as a reviewer.
//
// It lives on a different host from everything else boardwalk calls —
// vssps.dev.azure.com rather than dev.azure.com — and it answers in two
// steps: expand the signed-in user's identity to every group it is
// transitively a member of, then resolve those descriptors to the ids and
// display names a reviewer entry is written with.
//
// Naming the groups in the config file still works and still counts on top
// of this; this removes the need to, and the staleness that came with it.
//
// # Why not the Graph API
//
// This was the Graph API first, walking memberships upward from the user's
// subject descriptor. That walk is wrong in a way nothing about it announces:
// `memberships?direction=up` on a user returns only `vssgp.` containers —
// Azure DevOps's own groups — and silently omits `aadgp.` ones, the Azure AD
// groups an organisation federated into it.
//
// The membership is really there. Asking for the edge directly
// (`memberships/{user}/{group}`) answers 200, and asking the group who it
// contains (`direction=down`) lists the user. Only the upward listing leaves
// it out.
//
// On the tenant this was found against, every group actually used as a pull
// request reviewer was an Azure AD group, so the walk resolved 35 groups,
// none of them the ones that mattered, and the review filter matched nothing
// while looking entirely healthy. The Identities API returns all 74 in one
// call, Azure AD groups included.
//
// It is cheaper as well as correct: the walk cost roughly seventy requests at
// startup — one per node, plus one per group — against five here.

// identityAPIVersion is the plain version. Unlike Graph, identities are not
// preview at 7.1.
const identityAPIVersion = APIVersion

// descriptorBatch is how many descriptors go in one resolve request.
//
// They travel in the query string and each runs to around 130 characters, so
// the whole list at once is a URL the server rejects — with a bare 404, which
// reads as "no such identities" rather than as "that request was too long".
// Measured against a live tenant: thirty descriptors answered, forty did not.
// Twenty leaves room for descriptors longer than the ones measured.
const descriptorBatch = 20

// Groups is who the signed-in user is, as far as reviewing goes: the ids of
// every group they belong to, directly or through nesting, and those groups'
// display names.
//
// Both are kept because which one lines up with a reviewer entry is not
// something to rely on across identity providers. On the tenant this was
// built against the id matches exactly, and the name is the belt to that
// braces — it costs nothing, since the resolve returns both anyway.
//
// Names are lowercased, because that is how they are compared.
type Groups struct {
	IDs   map[string]bool
	Names map[string]bool
}

// Has reports whether a reviewer group — named and identified as a pull
// request lists it — is one of these.
func (g Groups) Has(id, name string) bool {
	if id != "" && g.IDs[id] {
		return true
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	// Azure DevOps scopes a group's display name to wherever it lives, and
	// the two sides of this comparison do not always agree about whether to
	// include the scope — so both forms are tried, exactly as inGroups does
	// for a name written in the config file.
	return g.Names[strings.ToLower(name)] || g.Names[strings.ToLower(unscoped(name))]
}

// identityRoot is the identity host. Tests point it at an httptest server
// along with root(); production splits them, which is the whole reason this
// exists separately.
func (c *Client) identityRoot() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://vssps.dev.azure.com"
}

// MyGroups resolves every group the signed-in user belongs to.
//
// It needs MyID, which connectionData supplies at startup. Without one there
// is nothing to expand, and it says so rather than asking a question with a
// hole in it.
//
// A batch that cannot be read is skipped rather than fatal: half an answer is
// the difference between the review filter working for most of somebody's
// groups and working for none of them.
func (c *Client) MyGroups() (Groups, error) {
	groups := Groups{IDs: map[string]bool{}, Names: map[string]bool{}}

	if c.MyID == "" {
		return groups, fmt.Errorf("no Azure DevOps identity to resolve memberships for")
	}

	descriptors, err := c.memberOf(c.MyID)
	if err != nil {
		return groups, fmt.Errorf("could not expand your group memberships: %w", err)
	}

	for start := 0; start < len(descriptors); start += descriptorBatch {
		end := min(start+descriptorBatch, len(descriptors))

		rows, err := c.identities(descriptors[start:end])
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r.ID != "" {
				groups.IDs[r.ID] = true
			}
			if r.Name != "" {
				groups.Names[strings.ToLower(r.Name)] = true
				groups.Names[strings.ToLower(unscoped(r.Name))] = true
			}
		}
	}

	return groups, nil
}

// memberOf expands an identity to every group it belongs to.
//
// queryMembership=Expanded is what makes this one request rather than a
// walk: it is transitive, so a group reached only through two other groups
// is in the list alongside the direct ones.
func (c *Client) memberOf(id string) ([]string, error) {
	var body struct {
		Value []struct {
			MemberOf []string `json:"memberOf"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/identities?identityIds=%s&queryMembership=Expanded&api-version=%s",
		c.identityRoot(), url.PathEscape(c.Org), url.QueryEscape(id), identityAPIVersion)
	if err := c.get(endpoint, &body); err != nil {
		return nil, err
	}

	var out []string
	for _, v := range body.Value {
		out = append(out, v.MemberOf...)
	}
	return out, nil
}

// identityGroup is one resolved group: the id a reviewer entry carries and
// the name it is displayed under.
type identityGroup struct {
	ID   string `json:"id"`
	Name string `json:"providerDisplayName"`
}

// identities resolves a batch of descriptors. The caller keeps the batches
// within descriptorBatch.
func (c *Client) identities(descriptors []string) ([]identityGroup, error) {
	var body struct {
		Value []identityGroup `json:"value"`
	}

	// A descriptor holds semicolons and backslashes, which have to survive
	// the query string intact — unescaped, the server resolves nothing and
	// reports only that it found nothing.
	endpoint := fmt.Sprintf("%s/%s/_apis/identities?descriptors=%s&api-version=%s",
		c.identityRoot(), url.PathEscape(c.Org),
		url.QueryEscape(strings.Join(descriptors, ",")), identityAPIVersion)
	if err := c.get(endpoint, &body); err != nil {
		return nil, err
	}
	return body.Value, nil
}
