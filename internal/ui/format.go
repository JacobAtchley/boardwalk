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
