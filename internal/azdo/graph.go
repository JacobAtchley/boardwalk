package azdo

import (
	"fmt"
	"net/url"
	"strings"
)

// The Graph API answers the one question a pull request's payload never
// does: who is in the group it lists as a reviewer.
//
// It lives on a different host from everything else boardwalk calls —
// vssps.dev.azure.com rather than dev.azure.com — and it is keyed by
// descriptor rather than by the GUIDs the rest of the API uses, so
// resolving a membership is a walk rather than a lookup: my id becomes a
// descriptor, the descriptor lists what it belongs to, and each of those
// lists what it belongs to in turn.
//
// Naming the groups in the config file still works and still wins where it
// is used; this removes the need to, and the staleness that came with it.

// graphAPIVersion is Graph's own version. It is preview at 7.1 and has been
// for every 7.x release: the plain version comes back as "the -preview flag
// must be supplied", the same refusal connectionData gives.
const graphAPIVersion = APIVersion + "-preview.1"

// maxGroupDepth caps how far up the membership chain the walk goes.
//
// A real organisation nests two or three deep — a team inside a department
// inside "Project Valid Users". The cap is not there for correctness, which
// the seen-set already provides; it is there so that a tenant with a
// pathological hierarchy costs a bounded number of requests at startup
// rather than an unbounded one.
const maxGroupDepth = 8

// Groups is who the signed-in user is, as far as reviewing goes: the origin
// ids of every group they belong to, directly or through nesting, and those
// groups' display names.
//
// Both are kept because it is not certain which one a pull request's group
// reviewer can be matched on. The reviewer entry carries an identity GUID,
// which is a Azure DevOps group's originId — but an Azure AD group's originId
// is its AAD object id, which is a different GUID from the identity Azure
// DevOps mints for it. Matching on either means a tenant using AAD groups as
// reviewers still resolves, through the name, rather than silently resolving
// nothing.
//
// Names are lowercased, because that is how they are compared.
type Groups struct {
	IDs   map[string]bool
	Names map[string]bool
}

// Has reports whether a reviewer group — named and identified as a pull
// request lists it — is one of these.
func (g Groups) Has(id, name string) bool {
	if g.IDs[id] && id != "" {
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

// graphRoot is the identity host. Tests point it at an httptest server along
// with root(); production splits them, which is the whole reason this exists
// separately.
func (c *Client) graphRoot() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://vssps.dev.azure.com"
}

// MyGroups resolves every group the signed-in user belongs to.
//
// It needs MyID, which connectionData supplies at startup. Without one there
// is nothing to start the walk from, and it says so rather than asking Graph
// a question with a hole in it.
//
// A group that cannot be read is skipped rather than fatal: a token scoped to
// read most of an organisation's groups and not all of them should resolve
// the ones it can. Half an answer is the difference between "needs my review"
// working for most groups and working for none.
func (c *Client) MyGroups() (Groups, error) {
	groups := Groups{IDs: map[string]bool{}, Names: map[string]bool{}}

	if c.MyID == "" {
		return groups, fmt.Errorf("no Azure DevOps identity to resolve memberships for")
	}

	me, err := c.subjectDescriptor(c.MyID)
	if err != nil {
		return groups, fmt.Errorf("could not resolve your Graph descriptor: %w", err)
	}

	// seen covers the descriptors already walked, which is what makes a
	// cycle terminate and what stops a diamond — two groups sharing a
	// parent — from asking about that parent twice.
	seen := map[string]bool{me: true}
	frontier := []string{me}

	for depth := 0; depth < maxGroupDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, descriptor := range frontier {
			containers, err := c.memberships(descriptor)
			if err != nil {
				// The same reasoning as an unreadable group: what has been
				// resolved so far is worth keeping.
				continue
			}
			for _, container := range containers {
				if seen[container] {
					continue
				}
				seen[container] = true
				next = append(next, container)

				g, err := c.group(container)
				if err != nil {
					continue
				}
				if g.OriginID != "" {
					groups.IDs[g.OriginID] = true
				}
				if g.DisplayName != "" {
					groups.Names[strings.ToLower(g.DisplayName)] = true
					groups.Names[strings.ToLower(unscoped(g.DisplayName))] = true
				}
			}
		}
		frontier = next
	}

	return groups, nil
}

// subjectDescriptor turns an identity GUID into the descriptor Graph is keyed
// by. It is the one bridge between the two halves of the API.
func (c *Client) subjectDescriptor(id string) (string, error) {
	var body struct {
		Value string `json:"value"`
	}
	endpoint := fmt.Sprintf("%s/%s/_apis/graph/descriptors/%s?api-version=%s",
		c.graphRoot(), url.PathEscape(c.Org), url.PathEscape(id), graphAPIVersion)
	if err := c.get(endpoint, &body); err != nil {
		return "", err
	}
	if body.Value == "" {
		return "", fmt.Errorf("Graph returned no descriptor for %s", id)
	}
	return body.Value, nil
}

// memberships lists the descriptors a subject belongs to.
//
// direction=up is the whole point: the default lists a group's members, which
// for "Project Valid Users" is everybody in the organisation and is not a
// question boardwalk ever wants to ask.
func (c *Client) memberships(descriptor string) ([]string, error) {
	var body struct {
		Value []struct {
			ContainerDescriptor string `json:"containerDescriptor"`
		} `json:"value"`
	}
	endpoint := fmt.Sprintf("%s/%s/_apis/graph/memberships/%s?direction=up&api-version=%s",
		c.graphRoot(), url.PathEscape(c.Org), url.PathEscape(descriptor), graphAPIVersion)
	if err := c.get(endpoint, &body); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(body.Value))
	for _, m := range body.Value {
		if m.ContainerDescriptor != "" {
			out = append(out, m.ContainerDescriptor)
		}
	}
	return out, nil
}

// graphGroup is what the group endpoint says about one group.
type graphGroup struct {
	DisplayName string `json:"displayName"`
	OriginID    string `json:"originId"`
}

func (c *Client) group(descriptor string) (graphGroup, error) {
	var g graphGroup
	endpoint := fmt.Sprintf("%s/%s/_apis/graph/groups/%s?api-version=%s",
		c.graphRoot(), url.PathEscape(c.Org), url.PathEscape(descriptor), graphAPIVersion)
	err := c.get(endpoint, &g)
	return g, err
}
