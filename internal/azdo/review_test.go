package azdo

import (
	"strings"
	"testing"
)

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
		c := &Client{Me: me, ReviewGroups: tc.groups}
		if got := c.NeedsReviewFrom(tc.pr); got != tc.want {
			t.Errorf("%s: NeedsReviewFrom = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestInGroupsMatchesTheScopeAzureDevOpsPrefixes(t *testing.T) {
	// Azure DevOps hands back a group's display name scoped to wherever it
	// lives — "[TEAM FOUNDATION]\platform-devs" for a collection group,
	// "[MyProject]\Team Name" for a project one — while people write the
	// bare name in the config file.
	for _, tc := range []struct {
		name   string
		group  string
		config []string
		want   bool
	}{
		{
			name:   "a collection-scoped group named barely",
			group:  `[TEAM FOUNDATION]\platform-devs`,
			config: []string{"platform-devs"},
			want:   true,
		},
		{
			name:   "a project-scoped group named barely",
			group:  `[ControlTower]\Platform Team`,
			config: []string{"platform team"},
			want:   true,
		},
		{
			name:   "a scoped group named in full",
			group:  `[TEAM FOUNDATION]\platform-devs`,
			config: []string{`[team foundation]\platform-devs`},
			want:   true,
		},
		{
			// A config naming a scope means it: two projects can each have a
			// "developers" group, and the one written down is the one meant.
			name:   "a scope named in full, against another scope's group",
			group:  `[TEAM FOUNDATION]\platform-devs`,
			config: []string{`[ControlTower]\platform-devs`},
			want:   false,
		},
		{
			name:   "an unscoped group, as before",
			group:  "platform-devs",
			config: []string{"platform-devs"},
			want:   true,
		},
		{
			name:   "a scoped group I am not in",
			group:  `[TEAM FOUNDATION]\other-team`,
			config: []string{"platform-devs"},
			want:   false,
		},
		{
			name:   "a name that is nothing but a scope",
			group:  `[TEAM FOUNDATION]\`,
			config: []string{"platform-devs"},
			want:   false,
		},
	} {
		p := pr("other@acme.test", Reviewer{Name: tc.group, IsGroup: true})
		c := &Client{Me: me, MyID: "my-guid", ReviewGroups: tc.config}

		if got := c.NeedsReviewFrom(p); got != tc.want {
			t.Errorf("%s: NeedsReviewFrom = %v, want %v", tc.name, got, tc.want)
		}
		if _, got := c.MyReviewerID(p); got != tc.want {
			t.Errorf("%s: MyReviewerID ok = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMyReviewerID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pr     PullRequest
		myID   string
		groups []string
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
			// The direct entry is the one the pull request already knows
			// about, so it wins over the identity fetched at startup even
			// though both name the same person.
			name:   "a direct reviewer with an id, and an identity of my own",
			pr:     pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, ID: "guid-1"}),
			myID:   "my-guid",
			wantID: "guid-1",
			wantOK: true,
		},
		{
			// The bug this fallback exists for: Azure DevOps names only the
			// group, so there is no entry for me — but my own identity is a
			// perfectly good id to vote with, and voting adds the entry.
			name:   "only a group of mine covers me",
			pr:     pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			myID:   "my-guid",
			groups: []string{"platform-devs"},
			wantID: "my-guid",
			wantOK: true,
		},
		{
			name:   "only a group of mine covers me, with no identity fetched",
			pr:     pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			groups: []string{"platform-devs"},
			wantOK: false,
		},
		{
			name:   "a group I am not in",
			pr:     pr("other@acme.test", Reviewer{Name: "other-team", IsGroup: true}),
			myID:   "my-guid",
			groups: []string{"platform-devs"},
			wantOK: false,
		},
		{
			name:   "a group, with nothing configured",
			pr:     pr("other@acme.test", Reviewer{Name: "platform-devs", IsGroup: true}),
			myID:   "my-guid",
			wantOK: false,
		},
		{
			name:   "not a reviewer at all",
			pr:     pr("other@acme.test", Reviewer{Name: "Third Dev", Key: "third@acme.test", ID: "guid-3"}),
			myID:   "my-guid",
			wantOK: false,
		},
		{
			// A direct entry with no id would be a server bug. The identity
			// fetched at startup covers for it rather than refusing a vote
			// the person is plainly entitled to cast.
			name:   "a direct reviewer with no id",
			pr:     pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me}),
			myID:   "my-guid",
			wantID: "my-guid",
			wantOK: true,
		},
		{
			name:   "a direct reviewer with no id and no identity fetched",
			pr:     pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me}),
			wantOK: false,
		},
	} {
		c := &Client{Me: me, MyID: tc.myID, ReviewGroups: tc.groups}
		id, ok := c.MyReviewerID(tc.pr)
		if id != tc.wantID || ok != tc.wantOK {
			t.Errorf("%s: MyReviewerID = (%q, %v), want (%q, %v)", tc.name, id, ok, tc.wantID, tc.wantOK)
		}
	}
}

func TestMyReviewerIDWithoutAnIdentity(t *testing.T) {
	p := pr("other@acme.test", Reviewer{Name: "Dev Example", Key: me, ID: "guid-1"})
	c := &Client{MyID: "my-guid", ReviewGroups: []string{"platform-devs"}}
	if _, ok := c.MyReviewerID(p); ok {
		t.Error("an unknown identity matched a reviewer")
	}
}

func TestNeedsReviewFromWithoutAnIdentity(t *testing.T) {
	// az account show can fail without being fatal, leaving Me empty. Matching
	// everything then would be worse than matching nothing.
	p := pr("other@acme.test", Reviewer{Name: "Nobody", Key: ""})

	if (&Client{}).NeedsReviewFrom(p) {
		t.Error("an unknown identity matched a reviewer with no unique name")
	}
}

// resolvedClient is a client whose Graph memberships have already come back,
// which is the state NewClient leaves it in on a tenant where Graph is
// readable.
func resolvedClient(ids, names []string) *Client {
	g := Groups{IDs: map[string]bool{}, Names: map[string]bool{}}
	for _, id := range ids {
		g.IDs[id] = true
	}
	for _, n := range names {
		g.Names[strings.ToLower(n)] = true
	}
	return &Client{Me: "dev@acme.test", MyID: "me-guid", groups: g, groupsLoaded: true}
}

func groupReviewedPR(groupID, groupName string) PullRequest {
	return PullRequest{
		AuthorKey: "someone@acme.test",
		Reviewers: []Reviewer{{Name: groupName, ID: groupID, IsGroup: true}},
	}
}

// TestGraphMembershipMakesAGroupReviewMine — the point of the whole walk: a
// group nobody wrote in a config file is still mine, because Graph says so.
func TestGraphMembershipMakesAGroupReviewMine(t *testing.T) {
	c := resolvedClient([]string{"guid-devs"}, []string{"platform-devs"})

	if !c.NeedsReviewFrom(groupReviewedPR("guid-devs", "[Platform]\\platform-devs")) {
		t.Error("a group Graph resolved me into was not treated as mine")
	}
	if c.NeedsReviewFrom(groupReviewedPR("guid-other", "[Platform]\\some-other-team")) {
		t.Error("a group I am not in was treated as mine")
	}
}

// TestGraphMembershipMatchesOnTheNameWhenTheIDDiffers — an Azure AD group's
// originId is its AAD object id, which is not the GUID Azure DevOps puts on
// the reviewer entry. The name is what bridges that, and without it a tenant
// using AAD groups would resolve nothing while appearing to work.
func TestGraphMembershipMatchesOnTheNameWhenTheIDDiffers(t *testing.T) {
	c := resolvedClient([]string{"aad-object-id"}, []string{"platform-devs"})

	if !c.NeedsReviewFrom(groupReviewedPR("a-different-guid", "platform-devs")) {
		t.Error("a group whose id differs from its Graph originId was not matched by name")
	}
}

// TestConfiguredGroupsStillWinAfterGraph — the config is the override. A name
// written there is honoured whatever Graph did or did not resolve, which is
// what keeps boardwalk working on a tenant where Graph is locked down.
func TestConfiguredGroupsStillWinAfterGraph(t *testing.T) {
	c := resolvedClient([]string{"guid-devs"}, []string{"platform-devs"})
	c.ReviewGroups = []string{"release-managers"}

	if !c.NeedsReviewFrom(groupReviewedPR("guid-release", "[Platform]\\release-managers")) {
		t.Error("a group named in the config was not treated as mine")
	}
}

// TestConfiguredGroupsCarryATenantWithNoGraph — Graph failed at startup, so
// nothing was resolved. The config path has to be exactly what it was.
func TestConfiguredGroupsCarryATenantWithNoGraph(t *testing.T) {
	c := &Client{Me: "dev@acme.test", ReviewGroups: []string{"platform-devs"}}

	if !c.NeedsReviewFrom(groupReviewedPR("guid-devs", "[Platform]\\platform-devs")) {
		t.Error("with Graph unavailable the configured group stopped working")
	}
}

func TestMyReviewerIDUsesAGraphResolvedGroup(t *testing.T) {
	c := resolvedClient([]string{"guid-devs"}, []string{"platform-devs"})

	id, ok := c.MyReviewerID(groupReviewedPR("guid-devs", "platform-devs"))
	if !ok {
		t.Fatal("no reviewer id for a group Graph resolved me into")
	}
	if id != "me-guid" {
		t.Errorf("reviewer id = %q, want my own identity — a group is voted on as myself", id)
	}
}
