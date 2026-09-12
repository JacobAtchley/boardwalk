package azdo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClient points a Client at a stub server. Every azdo test that needs a
// round trip builds its client this way rather than reaching the network.
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{
		Org:     "acme",
		Project: "Platform",
		Me:      "dev@acme.test",
		token:   "test-token",
		http:    &http.Client{Timeout: 5 * time.Second},
		baseURL: srv.URL,
	}
}

func TestGetDecodesJSONAndSendsTheToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want the bearer token", got)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Write([]byte(`{"count":2}`))
	})

	var out struct {
		Count int `json:"count"`
	}
	if err := c.get(c.baseURL+"/thing", &out); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

func TestGetTextReturnsTheBodyVerbatim(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("line one\nline two\n"))
	})

	got, err := c.getText(c.baseURL + "/log")
	if err != nil {
		t.Fatalf("getText returned %v", err)
	}
	if got != "line one\nline two\n" {
		t.Errorf("getText = %q, want the body unchanged", got)
	}
}

func TestPatchSendsTheGivenContentType(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json-patch+json" {
			t.Errorf("Content-Type = %q, want application/json-patch+json", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `System.State`) {
			t.Errorf("body = %q, want the patch document", body)
		}
		w.Write([]byte(`{"id":7}`))
	})

	patch := []map[string]any{{"op": "add", "path": "/fields/System.State", "value": "Active"}}
	var out struct {
		ID int `json:"id"`
	}
	if err := c.patch(c.baseURL+"/wi/7", "application/json-patch+json", patch, &out); err != nil {
		t.Fatalf("patch returned %v", err)
	}
	if out.ID != 7 {
		t.Errorf("id = %d, want 7", out.ID)
	}
}

func TestErrorsCarryTheAzureMessage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"TF401019: repository does not exist"}`))
	})

	err := c.get(c.baseURL+"/missing", &struct{}{})
	if err == nil {
		t.Fatal("get on a 404 returned no error")
	}
	if !strings.Contains(err.Error(), "TF401019") {
		t.Errorf("error = %q, want the Azure DevOps message in it", err)
	}
}

func TestDiscardingTheBodyIsAllowed(t *testing.T) {
	// The branch flow's ref creation has a response nobody reads.
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})
	if err := c.get(c.baseURL+"/thing", nil); err != nil {
		t.Fatalf("get with a nil out returned %v", err)
	}
}
