package azdo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBranchArtifactURL(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		project, repo, branch string
		want                  string
	}{
		{
			name:    "a flat branch name",
			project: "p1", repo: "r1", branch: "main",
			want: "vstfs:///Git/Ref/p1%2Fr1%2FGBmain",
		},
		{
			// The separators and the slashes inside the branch are both encoded,
			// which is why the branch cannot simply be concatenated.
			name:    "a branch containing a slash",
			project: "p1", repo: "r1", branch: "feature/4021-retry",
			want: "vstfs:///Git/Ref/p1%2Fr1%2FGBfeature%2F4021-retry",
		},
	} {
		if got := BranchArtifactURL(tc.project, tc.repo, tc.branch); got != tc.want {
			t.Errorf("%s: BranchArtifactURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSetStateSendsAJSONPatch(t *testing.T) {
	var body, contentType string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.SetState(4021, "Active"); err != nil {
		t.Fatalf("SetState returned %v", err)
	}
	if contentType != "application/json-patch+json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	for _, want := range []string{`"op":"add"`, `"/fields/System.State"`, `"Active"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestLinkBranchAppendsAnArtifactRelation(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.LinkBranch(4021, "p1", "r1", "feature/4021-retry"); err != nil {
		t.Fatalf("LinkBranch returned %v", err)
	}
	for _, want := range []string{
		`"/relations/-"`,
		`"rel":"ArtifactLink"`,
		`vstfs:///Git/Ref/p1%2Fr1%2FGBfeature%2F4021-retry`,
		`"name":"Branch"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestSetAssigneeSendsAJSONPatch(t *testing.T) {
	var body, contentType string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.SetAssignee(4021, "dev@acme.test"); err != nil {
		t.Fatalf("SetAssignee returned %v", err)
	}
	if contentType != "application/json-patch+json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	for _, want := range []string{`"op":"add"`, `"/fields/System.AssignedTo"`, `"dev@acme.test"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestSetAssigneeWithAnEmptyNameUnassigns(t *testing.T) {
	// The client layer supports deliberate unassignment; guarding against an
	// empty identity is the UI's job, not this method's — see Client.Me.
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"id":4021}`))
	})

	if err := c.SetAssignee(4021, ""); err != nil {
		t.Fatalf("SetAssignee returned %v", err)
	}
	for _, want := range []string{`"op":"add"`, `"/fields/System.AssignedTo"`, `"value":""`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s, want it to contain %s", body, want)
		}
	}
}

func TestSetAssigneeReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"TF401320: rule error"}`))
	})

	err := c.SetAssignee(4021, "dev@acme.test")
	if err == nil {
		t.Fatal("SetAssignee against a refusing server returned no error")
	}
	if !strings.Contains(err.Error(), "TF401320") {
		t.Errorf("error = %q, want the server message", err)
	}
}

func TestSetStateReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"TF401320: rule error"}`))
	})

	err := c.SetState(4021, "Active")
	if err == nil {
		t.Fatal("SetState against a refusing server returned no error")
	}
	if !strings.Contains(err.Error(), "TF401320") {
		t.Errorf("error = %q, want the server message", err)
	}
}
