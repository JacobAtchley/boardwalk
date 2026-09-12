package azdo

import "testing"

const me = "dev@acme.test"

func pr(author string, reviewers ...Reviewer) PullRequest {
	return PullRequest{ID: 1, AuthorKey: author, Reviewers: reviewers}
}

func TestNeedsReviewFrom(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pr     PullRequest
		groups []string
		want   bool
	}{
		{
			name: "a direct reviewer who has not voted",
			pr:   pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me}),
			want: true,
		},
		{
			name: "a direct reviewer who already approved",
			pr:   pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, Vote: 10}),
			want: false,
		},
		{
			name: "a direct reviewer who rejected it",
			pr:   pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, Vote: -10}),
			want: false,
		},
		{
			name:   "a group I am in",
			pr:     pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			groups: []string{"platform-devs"},
			want:   true,
		},
		{
			name:   "a group I am not in",
			pr:     pr("other@acme.test", Reviewer{Name: "other-team", IsGroup: true}),
			groups: []string{"platform-devs"},
			want:   false,
		},
		{
			name: "a group with nothing configured",
			pr:   pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			want: false,
		},
		{
			// Azure DevOps adds you individually once you vote on a group's
			// behalf, and the group entry stays. Without this the pull request
			// would never leave the list.
			name: "a group I am in, after I have voted myself",
			pr: pr("other@acme.test",
				Reviewer{Name: "platform-devs", IsGroup: true},
				Reviewer{Name: "Dev Example", Key: me, Vote: 10}),
			groups: []string{"platform-devs"},
			want:   false,
		},
		{
			name: "my own pull request, with me as a reviewer",
			pr:   pr(me, Reviewer{Name: "Dev Example", Key: me}),
			want: false,
		},
		{
			name:   "my own pull request, reviewed by my group",
			pr:     pr(me, Reviewer{Name: "platform-devs", IsGroup: true}),
			groups: []string{"platform-devs"},
			want:   false,
		},
		{
			name: "someone else's review",
			pr:   pr("other@acme.test", Reviewer{Name: "Third Dev", Key: "third@acme.test"}),
			want: false,
		},
		{
			name: "no reviewers at all",
			pr:   pr("other@acme.test"),
			want: false,
		},
	} {
		if got := NeedsReviewFrom(tc.pr, me, tc.groups); got != tc.want {
			t.Errorf("%s: NeedsReviewFrom = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNeedsReviewFromWithoutAnIdentity(t *testing.T) {
	// az account show can fail without being fatal, leaving Me empty. Matching
	// everything then would be worse than matching nothing.
	p := pr("other@acme.test", Reviewer{Name: "Nobody", Key: ""})

	if NeedsReviewFrom(p, "", nil) {
		t.Error("an unknown identity matched a reviewer with no unique name")
	}
}
