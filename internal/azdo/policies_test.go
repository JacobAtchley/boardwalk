package azdo

import (
	"net/http"
	"strings"
	"testing"
)

// gateBody is one pull request's policy evaluations: two build validations —
// one that has produced a run and one still queued behind the policy itself —
// and a reviewer policy that is not a build at all.
const gateBody = `{"value":[
 {"configuration":{"type":{"id":"0609b952-1397-4640-95ec-e00a01b2c241","displayName":"Build"},
   "settings":{"displayName":"CI gate","buildDefinitionId":12}},
  "status":"rejected","context":{"buildId":9001}},
 {"configuration":{"type":{"id":"0609b952-1397-4640-95ec-e00a01b2c241","displayName":"Build"},
   "settings":{"displayName":"Nightly gate","buildDefinitionId":13}},
  "status":"queued","context":{}},
 {"configuration":{"type":{"id":"fa4e907d-c16b-4a4c-9dfa-4906e5d171dd","displayName":"Minimum number of reviewers"},
   "settings":{"minimumApproverCount":2}},
  "status":"approved","context":{}}
]}`

func TestPullRequestGateBuildsKeepsTheBuildsAPolicyHasRun(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		want := "vstfs:///CodeReview/CodeReviewId/p-guid/512"
		if got := r.URL.Query().Get("artifactId"); got != want {
			t.Errorf("artifactId = %q, want %q", got, want)
		}
		if !strings.Contains(r.URL.Path, "/_apis/policy/evaluations") {
			t.Errorf("path = %q, want the policy evaluations endpoint", r.URL.Path)
		}
		// Azure DevOps refuses this endpoint at a plain version: "The
		// requested version "7.1" of the resource is under preview. The
		// -preview flag must be supplied in the api-version for such
		// requests."
		if got := r.URL.Query().Get("api-version"); !strings.HasSuffix(got, "-preview.1") {
			t.Errorf("api-version = %q, want the preview form the endpoint requires", got)
		}
		w.Write([]byte(gateBody))
	})

	gates, err := c.PullRequestGateBuilds(PullRequest{ID: 512, ProjectID: "p-guid"})
	if err != nil {
		t.Fatalf("PullRequestGateBuilds returned %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want only the one with a build", len(gates))
	}
	got := gates[0]
	if got.BuildID != 9001 {
		t.Errorf("build id = %d, want 9001", got.BuildID)
	}
	if got.Policy != "CI gate" {
		t.Errorf("policy = %q, want CI gate", got.Policy)
	}
	if got.Status != "rejected" {
		t.Errorf("status = %q, want rejected", got.Status)
	}
}

func TestPullRequestGateBuildsNamesAnUnnamedPolicy(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[
 {"configuration":{"type":{"id":"0609b952-1397-4640-95ec-e00a01b2c241","displayName":"Build"},"settings":{}},
  "status":"approved","context":{"buildId":42}}]}`))
	})

	gates, err := c.PullRequestGateBuilds(PullRequest{ID: 7, ProjectID: "p-guid"})
	if err != nil {
		t.Fatalf("PullRequestGateBuilds returned %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want 1", len(gates))
	}
	if gates[0].Policy != "build validation" {
		t.Errorf("policy = %q, want the fallback name", gates[0].Policy)
	}
}

func TestPullRequestGateBuildsWithoutAProjectIDSaysSo(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the evaluations endpoint was called without a project id")
	})

	if _, err := c.PullRequestGateBuilds(PullRequest{ID: 512}); err == nil {
		t.Fatal("PullRequestGateBuilds returned no error for a pull request with no project id")
	}
}
