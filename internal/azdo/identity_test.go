package azdo

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// identityFixture stands in for the Identities API: expanded is the memberOf
// list the signed-in user's identity comes back with, and resolved maps each
// of those descriptors to the id and display name it resolves to.
type identityFixture struct {
	expanded []string
	resolved map[string]identityRow
	// chunks records the size of each descriptors request, so a test can
	// prove a long list is broken up rather than sent as one unusable URL.
	chunks []int
	// failDescriptors, when set, makes any batch containing it answer 404 —
	// the way an over-long URL does.
	failDescriptors string
}

type identityRow struct{ id, name string }

func (f *identityFixture) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if ids := q.Get("identityIds"); ids != "" {
			if got := q.Get("queryMembership"); got != "Expanded" {
				t.Errorf("queryMembership = %q, want Expanded — anything else stops at direct membership", got)
			}
			quoted := make([]string, len(f.expanded))
			for i, d := range f.expanded {
				quoted[i] = fmt.Sprintf("%q", d)
			}
			fmt.Fprintf(w, `{"value":[{"id":%q,"memberOf":[%s]}]}`, ids, strings.Join(quoted, ","))
			return
		}

		descriptors := strings.Split(q.Get("descriptors"), ",")
		f.chunks = append(f.chunks, len(descriptors))

		if f.failDescriptors != "" {
			for _, d := range descriptors {
				if d == f.failDescriptors {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"message":"not found"}`)
					return
				}
			}
		}

		var rows []string
		for _, d := range descriptors {
			row, ok := f.resolved[d]
			if !ok {
				continue
			}
			rows = append(rows, fmt.Sprintf(`{"id":%q,"descriptor":%q,"providerDisplayName":%q}`,
				row.id, d, row.name))
		}
		fmt.Fprintf(w, `{"count":%d,"value":[%s]}`, len(rows), strings.Join(rows, ","))
	}
}

func identityClient(t *testing.T, f *identityFixture) *Client {
	t.Helper()
	c := testClient(t, f.handler(t))
	c.MyID = "me-guid"
	return c
}

// TestMyGroupsFindsAnAzureADGroup is the regression this whole change exists
// for.
//
// boardwalk used to walk Graph upward from the user's descriptor. That walk
// returns only vssgp containers — Azure DevOps's own groups — and silently
// omits aadgp ones, even though the membership demonstrably exists: asking
// for the edge directly answers 200, and asking the group who it contains
// lists the user. Every group a real organisation adds as a pull request
// reviewer turned out to be the kind the walk could not see, so "needs my
// review" matched nothing and said nothing about why.
func TestMyGroupsFindsAnAzureADGroup(t *testing.T) {
	const aadGroup = "Microsoft.TeamFoundation.Identity;S-1-9-aad-platform-devs"
	const vssGroup = "Microsoft.TeamFoundation.Identity;S-1-9-vss-pr-reviewers"

	c := identityClient(t, &identityFixture{
		expanded: []string{vssGroup, aadGroup},
		resolved: map[string]identityRow{
			vssGroup: {"guid-pr-reviewers", `[ControlTower]\PR Reviewers`},
			aadGroup: {"guid-platform-devs", `[TEAM FOUNDATION]\platform-devs`},
		},
	})

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	if !groups.IDs["guid-platform-devs"] {
		t.Errorf("the Azure AD group was not resolved: %+v", groups)
	}
	if !groups.IDs["guid-pr-reviewers"] {
		t.Errorf("the Azure DevOps group was not resolved: %+v", groups)
	}
}

// TestMyGroupsMatchesTheReviewerEntry — the end the whole thing is for. A
// pull request names a group by the id and display name Azure DevOps puts on
// the reviewer entry, and both have to land.
func TestMyGroupsMatchesTheReviewerEntry(t *testing.T) {
	const aadGroup = "Microsoft.TeamFoundation.Identity;S-1-9-aad-platform-devs"
	c := identityClient(t, &identityFixture{
		expanded: []string{aadGroup},
		resolved: map[string]identityRow{
			aadGroup: {"6e0e47dc-3108-4de8-ad3b-8aef9da9eaaa", `[TEAM FOUNDATION]\platform-devs`},
		},
	})

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}

	// Exactly as a pull request lists it.
	if !groups.Has("6e0e47dc-3108-4de8-ad3b-8aef9da9eaaa", `[TEAM FOUNDATION]\platform-devs`) {
		t.Errorf("the reviewer entry did not match what was resolved: %+v", groups)
	}
	// And by either half on its own, since which one lines up is not
	// something boardwalk can rely on across identity providers.
	if !groups.Has("6e0e47dc-3108-4de8-ad3b-8aef9da9eaaa", "") {
		t.Error("the id alone did not match")
	}
	if !groups.Has("", "platform-devs") {
		t.Error("the bare name alone did not match")
	}
}

// TestMyGroupsBatchesTheDescriptorLookup — every descriptor is ~127
// characters and they go in the query string, so the whole list in one
// request is a URL the server answers 404 to. Measured against a live
// tenant: 30 descriptors is fine, 40 is not.
func TestMyGroupsBatchesTheDescriptorLookup(t *testing.T) {
	f := &identityFixture{resolved: map[string]identityRow{}}
	for i := range 74 {
		d := fmt.Sprintf("Microsoft.TeamFoundation.Identity;S-1-9-%d", i)
		f.expanded = append(f.expanded, d)
		f.resolved[d] = identityRow{fmt.Sprintf("guid-%d", i), fmt.Sprintf("group %d", i)}
	}
	c := identityClient(t, f)

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	if len(groups.IDs) != 74 {
		t.Errorf("resolved %d groups, want all 74", len(groups.IDs))
	}
	if len(f.chunks) < 2 {
		t.Fatalf("the descriptors went in %d request(s) — a list this long has to be broken up", len(f.chunks))
	}
	for i, n := range f.chunks {
		if n > descriptorBatch {
			t.Errorf("batch %d held %d descriptors, want at most %d", i, n, descriptorBatch)
		}
	}
}

// TestMyGroupsKeepsWhatResolvedWhenABatchFails — one refused batch must not
// cost the groups that did come back. Half an answer is the difference
// between the review filter working for most groups and working for none.
func TestMyGroupsKeepsWhatResolvedWhenABatchFails(t *testing.T) {
	f := &identityFixture{resolved: map[string]identityRow{}}
	for i := range 40 {
		d := fmt.Sprintf("Microsoft.TeamFoundation.Identity;S-1-9-%d", i)
		f.expanded = append(f.expanded, d)
		f.resolved[d] = identityRow{fmt.Sprintf("guid-%d", i), fmt.Sprintf("group %d", i)}
	}
	f.failDescriptors = f.expanded[0] // poisons the first batch only
	c := identityClient(t, f)

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	if len(groups.IDs) == 0 {
		t.Fatal("one failed batch cost every group")
	}
	if groups.IDs["guid-0"] {
		t.Error("a group from the batch that failed was resolved anyway")
	}
}

func TestMyGroupsWithNoIdentityDoesNotAsk(t *testing.T) {
	f := &identityFixture{}
	c := identityClient(t, f)
	c.MyID = "" // connectionData failed at startup

	if _, err := c.MyGroups(); err == nil {
		t.Fatal("MyGroups reported success with no identity to expand")
	}
	if len(f.chunks) != 0 {
		t.Errorf("it asked anyway: %v", f.chunks)
	}
}

func TestMyGroupsReportsAFailedExpansion(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"the token is not scoped for identity"}`)
	})
	c.MyID = "me-guid"

	_, err := c.MyGroups()
	if err == nil {
		t.Fatal("MyGroups reported success against an identity service it cannot read")
	}
	if !strings.Contains(err.Error(), "not scoped for identity") {
		t.Errorf("error = %v, want the server's own message in it", err)
	}
}

// TestMyGroupsOnAnIdentityInNoGroups — a real answer, and a different one
// from a failure. The session view renders them differently and can only do
// that if this reports success.
func TestMyGroupsOnAnIdentityInNoGroups(t *testing.T) {
	c := identityClient(t, &identityFixture{})

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v for somebody in no groups, want success", err)
	}
	if len(groups.IDs) != 0 {
		t.Errorf("groups = %+v, want none", groups)
	}
}

func TestIdentityRootIsTheIdentityHost(t *testing.T) {
	// Not asserted through the test client, which points both roots at one
	// server: identities live on vssps, and a client sending these to
	// dev.azure.com would 404 against a real tenant.
	c := &Client{Org: "acme", Project: "Platform"}
	if got := c.identityRoot(); got != "https://vssps.dev.azure.com" {
		t.Errorf("identityRoot() = %q, want the identity host", got)
	}
}

// TestDescriptorsAreSentEscaped — a descriptor holds semicolons and
// backslashes, which have to survive the query string intact or the server
// resolves nothing and says only that it found nothing.
func TestDescriptorsAreSentEscaped(t *testing.T) {
	const d = `Microsoft.TeamFoundation.Identity;S-1-9-1551374245-1204400969`
	var raw string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("identityIds") != "" {
			fmt.Fprintf(w, `{"value":[{"memberOf":[%q]}]}`, d)
			return
		}
		raw = r.URL.RawQuery
		fmt.Fprintf(w, `{"value":[{"id":"g","descriptor":%q,"providerDisplayName":"g"}]}`, d)
	})
	c.MyID = "me-guid"

	if _, err := c.MyGroups(); err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	if !strings.Contains(raw, url.QueryEscape(d)) {
		t.Errorf("descriptors were not escaped into the query: %s", raw)
	}
}

// The startup lookups keep the reason they did not happen, rather than
// discarding it. Every one of these failures looks from the outside like "you
// are in no groups" or "you cannot vote", which is also what a perfectly
// ordinary session looks like — the session view exists to tell them apart,
// and it can only do that if the reason is still here.

func TestGroupsAreReportedWithWhyTheyAreMissing(t *testing.T) {
	const group = "Microsoft.TeamFoundation.Identity;S-1-9-devs"
	c := identityClient(t, &identityFixture{
		expanded: []string{group},
		resolved: map[string]identityRow{group: {"guid-devs", "platform-devs"}},
	})
	c.loadMyGroups()

	if !c.GroupsResolved() {
		t.Fatalf("a successful resolve did not report as resolved: %v", c.GroupsError())
	}
	if err := c.GroupsError(); err != nil {
		t.Errorf("GroupsError() = %v on a successful resolve, want none", err)
	}
	if got := c.ResolvedGroups(); !got.IDs["guid-devs"] {
		t.Errorf("ResolvedGroups() = %+v, want the group that was resolved", got)
	}
}

func TestGroupsKeepTheReasonTheResolveFailed(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"the token is not scoped for identity"}`)
	})
	c.MyID = "me-guid"
	c.loadMyGroups()

	if c.GroupsResolved() {
		t.Fatal("a failed resolve reported as resolved")
	}
	err := c.GroupsError()
	if err == nil {
		t.Fatal("the reason the resolve failed was discarded")
	}
	if !strings.Contains(err.Error(), "not scoped for identity") {
		t.Errorf("GroupsError() = %v, want the server's own message in it", err)
	}
}

// TestIdentityErrorIsKept — the same argument for connectionData: without
// MyID a group vote cannot be cast, and the reason belongs on screen rather
// than in a discarded return value.
func TestIdentityErrorIsKept(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"TF400813: the user is not authorized"}`)
	})

	_ = c.loadMyID()
	if err := c.IdentityError(); err == nil || !strings.Contains(err.Error(), "TF400813") {
		t.Errorf("IdentityError() = %v, want the reason connectionData failed", err)
	}
}

func TestIdentityErrorIsClearedBySuccess(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authenticatedUser":{"id":"me-guid"}}`)
	})
	c.idErr = fmt.Errorf("a previous attempt failed")

	_ = c.loadMyID()
	if err := c.IdentityError(); err != nil {
		t.Errorf("IdentityError() = %v after a successful retry, want none", err)
	}
}
