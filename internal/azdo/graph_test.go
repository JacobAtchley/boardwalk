package azdo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// graphServer stands in for the Graph API. memberships maps a descriptor to
// the descriptors it belongs to, so a test can describe a nesting in one
// literal; groups maps a group descriptor to the display name and origin id
// the group endpoint would answer with.
type graphFixture struct {
	myDescriptor string
	memberships  map[string][]string
	groups       map[string]graphGroupFixture
	// requests counts every call, so a test can prove the walk does not
	// re-ask for a group it has already seen.
	requests map[string]int
}

type graphGroupFixture struct {
	displayName string
	originID    string
}

func (f *graphFixture) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	if f.requests == nil {
		f.requests = map[string]int{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		f.requests[path]++

		switch {
		case strings.Contains(path, "/_apis/graph/descriptors/"):
			fmt.Fprintf(w, `{"value":%q}`, f.myDescriptor)

		case strings.Contains(path, "/_apis/graph/memberships/"):
			if got := r.URL.Query().Get("direction"); got != "up" {
				t.Errorf("direction = %q, want up — down would list a group's members", got)
			}
			subject := path[strings.LastIndex(path, "/")+1:]
			var entries []string
			for _, container := range f.memberships[subject] {
				entries = append(entries, fmt.Sprintf(
					`{"containerDescriptor":%q,"memberDescriptor":%q}`, container, subject))
			}
			fmt.Fprintf(w, `{"count":%d,"value":[%s]}`, len(entries), strings.Join(entries, ","))

		case strings.Contains(path, "/_apis/graph/groups/"):
			descriptor := path[strings.LastIndex(path, "/")+1:]
			g, ok := f.groups[descriptor]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"no such group"}`)
				return
			}
			fmt.Fprintf(w, `{"descriptor":%q,"displayName":%q,"originId":%q}`,
				descriptor, g.displayName, g.originID)

		default:
			t.Errorf("unexpected Graph request to %s", path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// graphClient is a client pointed at a fake Graph API. Both roots go to the
// same test server; production splits them across two hosts.
func graphClient(t *testing.T, f *graphFixture) *Client {
	t.Helper()
	c := testClient(t, f.handler(t))
	c.MyID = "me-guid"
	return c
}

func TestMyGroupsWalksNestedMemberships(t *testing.T) {
	// me → developers → engineering → everyone, plus a second direct group.
	c := graphClient(t, &graphFixture{
		myDescriptor: "aad.me",
		memberships: map[string][]string{
			"aad.me":            {"vssgp.developers", "vssgp.release-managers"},
			"vssgp.developers":  {"vssgp.engineering"},
			"vssgp.engineering": {"vssgp.everyone"},
		},
		groups: map[string]graphGroupFixture{
			"vssgp.developers":       {"platform-devs", "guid-devs"},
			"vssgp.release-managers": {"release-managers", "guid-release"},
			"vssgp.engineering":      {"engineering", "guid-eng"},
			"vssgp.everyone":         {"[TEAM FOUNDATION]\\Project Valid Users", "guid-everyone"},
		},
	})

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}

	// Every group reachable upward, not just the two named directly: a pull
	// request can list any of them as its reviewer.
	for _, want := range []string{"guid-devs", "guid-release", "guid-eng", "guid-everyone"} {
		if !groups.IDs[want] {
			t.Errorf("membership of %s was not resolved: %+v", want, groups)
		}
	}
	for _, want := range []string{"platform-devs", "engineering"} {
		if !groups.Names[strings.ToLower(want)] {
			t.Errorf("group name %q was not collected: %+v", want, groups)
		}
	}
}

// TestMyGroupsSurvivesACycle — Azure DevOps should not let a group contain
// itself transitively, but a walk that trusted it not to would hang the
// program rather than report anything.
func TestMyGroupsSurvivesACycle(t *testing.T) {
	c := graphClient(t, &graphFixture{
		myDescriptor: "aad.me",
		memberships: map[string][]string{
			"aad.me":  {"vssgp.a"},
			"vssgp.a": {"vssgp.b"},
			"vssgp.b": {"vssgp.a"},
		},
		groups: map[string]graphGroupFixture{
			"vssgp.a": {"a", "guid-a"},
			"vssgp.b": {"b", "guid-b"},
		},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		groups, err := c.MyGroups()
		if err != nil {
			t.Errorf("MyGroups returned %v", err)
		}
		if !groups.IDs["guid-a"] || !groups.IDs["guid-b"] {
			t.Errorf("the cycle cost the groups themselves: %+v", groups)
		}
	}()
	<-done
}

func TestMyGroupsAsksForEachGroupOnce(t *testing.T) {
	f := &graphFixture{
		myDescriptor: "aad.me",
		memberships: map[string][]string{
			"aad.me":  {"vssgp.a", "vssgp.b"},
			"vssgp.a": {"vssgp.shared"},
			"vssgp.b": {"vssgp.shared"},
		},
		groups: map[string]graphGroupFixture{
			"vssgp.a":      {"a", "guid-a"},
			"vssgp.b":      {"b", "guid-b"},
			"vssgp.shared": {"shared", "guid-shared"},
		},
	}
	c := graphClient(t, f)

	if _, err := c.MyGroups(); err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	// Both branches reach the shared parent. Asking twice is a request per
	// extra path through the graph, on the startup path.
	for path, n := range f.requests {
		if n > 1 {
			t.Errorf("%s was requested %d times, want once", path, n)
		}
	}
}

func TestMyGroupsWithNoIdentityDoesNotAsk(t *testing.T) {
	f := &graphFixture{myDescriptor: "aad.me"}
	c := graphClient(t, f)
	c.MyID = "" // connectionData failed at startup

	if _, err := c.MyGroups(); err == nil {
		t.Fatal("MyGroups reported success with no identity to resolve")
	}
	if len(f.requests) != 0 {
		t.Errorf("it asked anyway: %v", f.requests)
	}
}

func TestMyGroupsReportsAFailedDescriptorLookup(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"the token is not scoped for Graph"}`)
	})
	c.MyID = "me-guid"

	if _, err := c.MyGroups(); err == nil {
		t.Fatal("MyGroups reported success against a Graph it cannot read")
	}
}

// TestMyGroupsKeepsWhatItResolvedWhenOneGroupRefuses — a single group the
// token cannot read must not cost the memberships that did resolve. Half an
// answer is what makes the difference between "needs my review" working for
// most groups and working for none.
func TestMyGroupsKeepsWhatItResolvedWhenOneGroupRefuses(t *testing.T) {
	c := graphClient(t, &graphFixture{
		myDescriptor: "aad.me",
		memberships: map[string][]string{
			"aad.me": {"vssgp.readable", "vssgp.hidden"},
		},
		groups: map[string]graphGroupFixture{
			"vssgp.readable": {"platform-devs", "guid-readable"},
			// vssgp.hidden is absent, so the fixture answers 404.
		},
	})

	groups, err := c.MyGroups()
	if err != nil {
		t.Fatalf("MyGroups returned %v", err)
	}
	if !groups.IDs["guid-readable"] {
		t.Errorf("one unreadable group cost a readable one: %+v", groups)
	}
}

func TestGraphRootIsTheIdentityHost(t *testing.T) {
	// Not asserted through the test client, which points both roots at one
	// server: Graph lives on vssps, and a client that sent these to
	// dev.azure.com would 404 against a real tenant.
	c := &Client{Org: "acme", Project: "Platform"}
	if got := c.graphRoot(); got != "https://vssps.dev.azure.com" {
		t.Errorf("graphRoot() = %q, want the identity host", got)
	}
}
