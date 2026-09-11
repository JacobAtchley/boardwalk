package ui

import (
	"testing"
	"time"
)

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		then time.Time
		want string
	}{
		{"seconds read as a minute", now.Add(-30 * time.Second), "1m"},
		{"minutes", now.Add(-42 * time.Minute), "42m"},
		{"hours", now.Add(-5 * time.Hour), "5h"},
		{"just under a day is hours", now.Add(-23 * time.Hour), "23h"},
		{"days", now.AddDate(0, 0, -3), "3d"},
		{"a week becomes weeks", now.AddDate(0, 0, -7), "1w"},
		{"weeks", now.AddDate(0, 0, -20), "2w"},
		{"a year", now.AddDate(-1, 0, 0), "1y"},
		{"the future clamps to now", now.Add(time.Hour), "1m"},
		{"a zero time is unknown", time.Time{}, "-"},
	} {
		if got := humanAge(tc.then, now); got != tc.want {
			t.Errorf("%s: humanAge = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestShortRef(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"refs/heads/main", "main"},
		{"refs/heads/feature/4021-retry", "feature/4021-retry"},
		{"refs/pull/512/merge", "refs/pull/512/merge"},
		{"main", "main"},
		{"", "-"},
	} {
		if got := shortRef(tc.in); got != tc.want {
			t.Errorf("shortRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBranchName(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		id    int
		title string
		want  string
	}{
		{"User Story", 4021, "Retry webhook delivery on 5xx", "feature/4021-retry-webhook-delivery-on-5xx"},
		{"Bug", 77, "Login 500s", "bugfix/77-login-500s"},
		{"Defect", 78, "Crash on save", "bugfix/78-crash-on-save"},
		{"Task", 90, "Bump deps", "task/90-bump-deps"},
		{"Enhancement", 91, "Tidy copy", "feature/91-tidy-copy"},
		{"Feature", 92, "  Multiple   spaces  ", "feature/92-multiple-spaces"},
		{"Bug", 93, "Punctuation: it's, / broken!", "bugfix/93-punctuation-it-s-broken"},
		{"Bug", 94, "", "bugfix/94"},
		{"Bug", 95, "A title so long that it has to be cut somewhere sensible rather than running on", "bugfix/95-a-title-so-long-that-it-has-to-be-cut-somewhere"},
	} {
		if got := branchName(tc.kind, tc.id, tc.title); got != tc.want {
			t.Errorf("branchName(%q, %d, %q) = %q, want %q", tc.kind, tc.id, tc.title, got, tc.want)
		}
	}
}
