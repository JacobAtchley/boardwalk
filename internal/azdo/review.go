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
