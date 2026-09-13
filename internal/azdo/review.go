package azdo

import "strings"

// NeedsReviewFrom reports whether a pull request is waiting on this person.
//
// Waiting means they are a reviewer and have not voted. They can be a reviewer
// twice over: named directly, or through a group the pull request lists instead
// of its members. Nothing in the payload says who belongs to a group, so the
// caller supplies the groups to treat as theirs.
//
// Their own pull requests never count: authoring one is not reviewing it.
func NeedsReviewFrom(pr PullRequest, me string, groups []string) bool {
	if me == "" || pr.AuthorKey == me {
		return false
	}

	var viaGroup bool
	for _, r := range pr.Reviewers {
		if r.IsGroup {
			if inGroups(r.Name, groups) {
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
// by GUID, and boardwalk never fetches the signed-in user's GUID on its own —
// there is no endpoint here that hands it over in isolation. The only place it
// appears is inside a pull request's own reviewers list, next to the
// uniqueName it can be matched against. So a vote is only possible when I am
// listed as a direct reviewer; if I am only covered by a group, there is
// nothing in the payload naming me and no id to vote with.
func MyReviewerID(pr PullRequest, me string) (string, bool) {
	if me == "" {
		return "", false
	}
	for _, r := range pr.Reviewers {
		if !r.IsGroup && r.Key == me {
			return r.ID, r.ID != ""
		}
	}
	return "", false
}

func inGroups(name string, groups []string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, g := range groups {
		if strings.EqualFold(strings.TrimSpace(g), name) {
			return true
		}
	}
	return false
}
