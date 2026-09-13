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

func TestMyReviewerID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pr     PullRequest
		wantID string
		wantOK bool
	}{
		{
			name:   "a direct reviewer with an id",
			pr:     pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, ID: "guid-1"}),
			wantID: "guid-1",
			wantOK: true,
		},
		{
			// Only a group covers me: the payload never names me individually,
			// so there is no id anywhere to vote with.
			name:   "only a group covers me",
			pr:     pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			wantOK: false,
		},
		{
			name:   "not a reviewer at all",
			pr:     pr("other@acme.test", Reviewer{Name: "Third Dev", Key: "third@acme.test", ID: "guid-3"}),
			wantOK: false,
		},
		{
			// A direct entry with no id would be a server bug, but the caller
			// still needs a clean "no" rather than an empty string it might
			// send to the endpoint.
			name:   "a direct reviewer with no id",
			pr:     pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me}),
			wantOK: false,
		},
	} {
		id, ok := MyReviewerID(tc.pr, me)
		if id != tc.wantID || ok != tc.wantOK {
			t.Errorf("%s: MyReviewerID = (%q, %v), want (%q, %v)", tc.name, id, ok, tc.wantID, tc.wantOK)
		}
	}
}

func TestMyReviewerIDWithoutAnIdentity(t *testing.T) {
	p := pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, ID: "guid-1"})
	if _, ok := MyReviewerID(p, ""); ok {
		t.Error("an unknown identity matched a reviewer")
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
