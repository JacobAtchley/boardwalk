package azdo

import "strings"

// NeedsReviewFrom reports whether a pull request is waiting on this person.
//
// Waiting means they are a reviewer and have not voted. They can be a reviewer
// twice over: named directly, or through a group the pull request lists instead
// of its members. Nothing in the payload says who belongs to a group, so
// Client.myGroup answers that — from the Graph API where it could be reached,
// and from Client.ReviewGroups either way.
//
// Their own pull requests never count: authoring one is not reviewing it.
func (c *Client) NeedsReviewFrom(pr PullRequest) bool {
	me := c.Me
	if me == "" || pr.AuthorKey == me {
		return false
	}

	var viaGroup bool
	for _, r := range pr.Reviewers {
		if r.IsGroup {
			if c.myGroup(r) {
				viaGroup = true
			}
			continue
		}

		if r.Key == me {
			// A direct entry is the whole answer either way. Azure DevOps adds
			// you individually once you vote on a group's behalf, and leaves
			// the group entry in place — so a vote here settles it even when a
			// group of yours is still listed.
			return r.Vote == 0
		}
	}
	return viaGroup
}

// MyReviewerID finds the id the vote endpoint would need to address me on pr.
//
// Client.Me is an email address (uniqueName), but the vote endpoint is keyed
// by GUID. A pull request that names me directly carries that GUID next to the
// uniqueName it can be matched against, and it is the one to prefer — it is
// the entry the pull request itself already holds.
//
// Otherwise the vote falls back to Client.MyID, the identity fetched at
// startup, but only where a group of mine is a reviewer: being able to open
// somebody else's pull request is not being asked to review it. Voting with
// that id is what the web UI does when a group member approves — Azure DevOps
// adds the person as a reviewer in their own right and leaves the group entry
// alone.
//
// No direct entry, no group of mine, or no identity fetched at startup: there
// is nothing to vote with, and the caller says so rather than sending a
// request that cannot work.
func (c *Client) MyReviewerID(pr PullRequest) (string, bool) {
	if c.Me == "" {
		return "", false
	}

	var viaGroup bool
	for _, r := range pr.Reviewers {
		if r.IsGroup {
			if c.myGroup(r) {
				viaGroup = true
			}
			continue
		}
		if r.Key == c.Me {
			if r.ID != "" {
				return r.ID, true
			}
			// Named without an id would be a server bug; my own identity
			// addresses the same person, so use it rather than refuse.
			return c.MyID, c.MyID != ""
		}
	}

	if viaGroup {
		return c.MyID, c.MyID != ""
	}
	return "", false
}

// myGroup reports whether a group standing in as a reviewer is one of the
// signed-in user's.
//
// Two sources, checked in that order. Graph resolved the memberships at
// startup and is the one that cannot go stale, so it is asked first. The
// configured names are then asked regardless — they are an override, and the
// only source at all on a tenant whose Graph the token cannot read.
func (c *Client) myGroup(r Reviewer) bool {
	if c.groupsLoaded && c.groups.Has(r.ID, r.Name) {
		return true
	}
	return inGroups(r.Name, c.ReviewGroups)
}

// inGroups reports whether a reviewer group is one of the configured ones.
//
// Azure DevOps scopes a group's display name to wherever it lives —
// "[TEAM FOUNDATION]\platform-devs" for a collection group, "[MyProject]\Team
// Name" for a project one — and nobody writes that in a config file; they
// write the name the group is called. So a configured name with no scope is
// matched against the group's bare name, and a configured name that carries
// one is matched whole: writing the scope out means meaning it, and two
// projects can each have a "developers".
func inGroups(name string, groups []string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	bare := unscoped(name)

	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if strings.ContainsRune(g, '\\') {
			if strings.EqualFold(g, name) {
				return true
			}
			continue
		}
		if bare != "" && strings.EqualFold(g, bare) {
			return true
		}
	}
	return false
}

// unscoped drops the "[scope]\" Azure DevOps prefixes a group name with,
// leaving the name the group is known by.
func unscoped(name string) string {
	if i := strings.LastIndex(name, `\`); i >= 0 {
		return strings.TrimSpace(name[i+1:])
	}
	return name
}
