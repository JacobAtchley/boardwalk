package azdo

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
		// Without this a test whose server answers 401 would shell out to
		// the real az CLI, which passes on a developer's machine and fails
		// on a build agent that has never logged in.
		newToken: func() (string, error) { return "refreshed-token", nil },
		http:     &http.Client{Timeout: 5 * time.Second},
		baseURL:  srv.URL,
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

func TestPutSendsTheBodyAndDecodesTheResponse(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"vote":10`) {
			t.Errorf("body = %q, want the vote in it", body)
		}
		w.Write([]byte(`{"id":9}`))
	})

	var out struct {
		ID int `json:"id"`
	}
	if err := c.put(c.baseURL+"/reviewer", struct {
		Vote int `json:"vote"`
	}{Vote: 10}, &out); err != nil {
		t.Fatalf("put returned %v", err)
	}
	if out.ID != 9 {
		t.Errorf("id = %d, want 9", out.ID)
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

func TestLoadMyIDReadsTheAuthenticatedUsersGUID(t *testing.T) {
	var path, query string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.Query().Get("api-version")
		w.Write([]byte(`{"authenticatedUser":{"id":"my-guid","providerDisplayName":"Dev Example"}}`))
	})

	if err := c.loadMyID(); err != nil {
		t.Fatalf("loadMyID returned %v", err)
	}
	if c.MyID != "my-guid" {
		t.Errorf("MyID = %q, want the authenticated user's id", c.MyID)
	}
	if path != "/acme/_apis/connectionData" {
		t.Errorf("path = %q, want the organization's connectionData", path)
	}
	// connectionData is preview-only: plain 7.1 is rejected outright with
	// "the -preview flag must be supplied in the api-version".
	if query != connectionDataAPIVersion {
		t.Errorf("api-version = %q, want %q", query, connectionDataAPIVersion)
	}
}

func TestLoadMyIDReportsAnEmptyIdentity(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"authenticatedUser":{"id":""}}`))
	})

	if err := c.loadMyID(); err == nil {
		t.Error("an identity with no id was accepted")
	}
	if c.MyID != "" {
		t.Errorf("MyID = %q, want it left empty", c.MyID)
	}
}

func TestAnExpiredTokenIsRefreshedAndTheRequestRetried(t *testing.T) {
	var sent []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, r.Header.Get("Authorization"))
		if len(sent) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"TF400813: the user is not authorized"}`))
			return
		}
		w.Write([]byte(`{"count":2}`))
	})
	c.newToken = func() (string, error) { return "fresh-token", nil }

	var out struct {
		Count int `json:"count"`
	}
	if err := c.get(c.baseURL+"/thing", &out); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want the retried request's body", out.Count)
	}
	want := []string{"Bearer test-token", "Bearer fresh-token"}
	if len(sent) != 2 || sent[0] != want[0] || sent[1] != want[1] {
		t.Errorf("authorization headers = %q, want %q", sent, want)
	}
	if c.token != "fresh-token" {
		t.Errorf("token = %q, want the refreshed one kept for later requests", c.token)
	}
}

func TestASignInPageCountsAsARejectedToken(t *testing.T) {
	// An expired token does not always come back as a 401: Azure DevOps also
	// answers 203 with the sign-in page, which reads as a success.
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusNonAuthoritativeInfo)
			w.Write([]byte("<html>sign in</html>"))
			return
		}
		w.Write([]byte(`{"count":5}`))
	})
	c.newToken = func() (string, error) { return "fresh-token", nil }

	var out struct {
		Count int `json:"count"`
	}
	if err := c.get(c.baseURL+"/thing", &out); err != nil {
		t.Fatalf("get returned %v", err)
	}
	if out.Count != 5 {
		t.Errorf("count = %d, want the retried request's body", out.Count)
	}
}

func TestARetriedWriteSendsItsBodyAgain(t *testing.T) {
	var bodies []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"TF400813: the user is not authorized"}`))
			return
		}
		w.Write([]byte(`{"id":7}`))
	})
	c.newToken = func() (string, error) { return "fresh-token", nil }

	patch := []map[string]any{{"op": "add", "path": "/fields/System.State", "value": "Active"}}
	var out struct {
		ID int `json:"id"`
	}
	if err := c.patch(c.baseURL+"/wi/7", "application/json-patch+json", patch, &out); err != nil {
		t.Fatalf("patch returned %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want the first and its retry", len(bodies))
	}
	if bodies[1] != bodies[0] || !strings.Contains(bodies[1], "System.State") {
		t.Errorf("retried body = %q, want the same patch document as %q", bodies[1], bodies[0])
	}
}

func TestAFailedRefreshReportsTheLoginAndStopsTrying(t *testing.T) {
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"TF400813: the user is not authorized"}`))
	})
	c.newToken = func() (string, error) { return "", fmt.Errorf("az account get-access-token: please run az login") }

	err := c.get(c.baseURL+"/thing", &struct{}{})
	if err == nil {
		t.Fatal("a request with an unrefreshable token returned no error")
	}
	if !strings.Contains(err.Error(), "az login") {
		t.Errorf("error = %q, want it to name az login", err)
	}
	// The request's own failure is half the story: a 401 and a 403 the token
	// could never have satisfied both end up here, and only the server's
	// message tells them apart.
	if !strings.Contains(err.Error(), "TF400813") {
		t.Errorf("error = %q, want the rejected request's message in it too", err)
	}
	if calls != 1 {
		t.Errorf("server saw %d requests, want the one that failed and no retry", calls)
	}
}

func TestARejectionThatSurvivesTheRefreshIsReported(t *testing.T) {
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"TF400813: the user is not authorized"}`))
	})
	c.newToken = func() (string, error) { return "fresh-token", nil }

	err := c.get(c.baseURL+"/thing", &struct{}{})
	if err == nil {
		t.Fatal("a request rejected twice returned no error")
	}
	if !strings.Contains(err.Error(), "TF400813") {
		t.Errorf("error = %q, want the Azure DevOps message in it", err)
	}
	if calls != 2 {
		t.Errorf("server saw %d requests, want one retry and no more", calls)
	}
}

func TestConcurrentRejectionsRefreshOnce(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"TF400813: the user is not authorized"}`))
			return
		}
		w.Write([]byte(`{"count":1}`))
	})

	var mints int32
	c.newToken = func() (string, error) {
		atomic.AddInt32(&mints, 1)
		return "fresh-token", nil
	}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = c.get(c.baseURL+"/thing", &struct{}{})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d returned %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&mints); got != 1 {
		t.Errorf("minted %d tokens, want one shared refresh", got)
	}
}

func TestReplayableRestoresABodyThatWasAlreadyRead(t *testing.T) {
	req, err := jsonRequest(http.MethodPatch, "https://dev.azure.test/wi/7", "application/json", map[string]string{"state": "Active"})
	if err != nil {
		t.Fatalf("jsonRequest returned %v", err)
	}
	if _, err := io.ReadAll(req.Body); err != nil {
		t.Fatalf("draining the body returned %v", err)
	}

	replay, err := replayable(req)
	if err != nil {
		t.Fatalf("replayable returned %v", err)
	}
	body, err := io.ReadAll(replay.Body)
	if err != nil {
		t.Fatalf("reading the replayed body returned %v", err)
	}
	if !strings.Contains(string(body), `"state":"Active"`) {
		t.Errorf("replayed body = %q, want the original payload", body)
	}
}
