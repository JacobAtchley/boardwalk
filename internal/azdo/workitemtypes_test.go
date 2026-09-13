package azdo

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStatesFetchesTheWorkItemTypesEndpoint(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"value":[{"name":"New"},{"name":"Active"},{"name":"Resolved"},{"name":"Closed"}]}`))
	})

	states, err := c.States("Bug")
	if err != nil {
		t.Fatalf("States returned %v", err)
	}

	wantPath := "/acme/Platform/_apis/wit/workitemtypes/Bug/states"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}

	var names []string
	for _, s := range states {
		names = append(names, s.Name)
	}
	want := []string{"New", "Active", "Resolved", "Closed"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("states = %v, want %v", names, want)
	}
}

func TestStatesEscapesAWorkItemTypeContainingASpace(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"value":[]}`))
	})

	if _, err := c.States("User Story"); err != nil {
		t.Fatalf("States returned %v", err)
	}
	if gotPath != "/acme/Platform/_apis/wit/workitemtypes/User Story/states" {
		t.Errorf("path = %q, want the type path-escaped", gotPath)
	}
}

func TestStatesCachesPerTypeForTheSession(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"value":[{"name":"New"},{"name":"Active"}]}`))
	})

	if _, err := c.States("Bug"); err != nil {
		t.Fatalf("first States call returned %v", err)
	}
	if _, err := c.States("Bug"); err != nil {
		t.Fatalf("second States call returned %v", err)
	}
	if calls != 1 {
		t.Errorf("server was hit %d times, want the second call served from the cache", calls)
	}

	if _, err := c.States("Task"); err != nil {
		t.Fatalf("States for a different type returned %v", err)
	}
	if calls != 2 {
		t.Errorf("server was hit %d times, want a different type to bypass the cache", calls)
	}
}

func TestStatesDoesNotCacheAFailure(t *testing.T) {
	// A transient failure must not poison the cache for the rest of the
	// session — the next attempt has to reach the network again rather than
	// replaying the same error forever.
	calls := 0
	failing := true
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if failing {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"TF401232: the work item type does not exist"}`))
			return
		}
		w.Write([]byte(`{"value":[{"name":"New"}]}`))
	})

	_, err := c.States("Bug")
	if err == nil {
		t.Fatal("States against a refusing server returned no error")
	}
	if !strings.Contains(err.Error(), "TF401232") {
		t.Errorf("error = %q, want the server message", err)
	}

	failing = false
	if _, err := c.States("Bug"); err != nil {
		t.Fatalf("the retry after the failure returned %v", err)
	}
	if calls != 2 {
		t.Errorf("server was hit %d times, want the failed attempt not cached", calls)
	}
}

// TestStatesIsSafeForConcurrentCallers proves the stateCache's mutex actually
// does something. Client is shared by every view, and each one issues its
// fetches from inside a tea.Cmd closure on its own goroutine — two pickers
// asking about the same type at once (a cache-hit racing a populate) or two
// different types (concurrent inserts into the map) is the ordinary case in
// production, not an edge case, so this has to be exercised under -race
// rather than trusted by inspection. A start channel holds every goroutine at
// the gate so they are released together, rather than trickling in one at a
// time and never actually overlapping.
func TestStatesIsSafeForConcurrentCallers(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// A little latency widens the window a populate and a concurrent
		// cache-hit have to overlap in, rather than one finishing before the
		// next caller even reaches the lock.
		time.Sleep(time.Millisecond)
		w.Write([]byte(`{"value":[{"name":"New"},{"name":"Active"}]}`))
	})

	types := []string{"Bug", "Task", "User Story", "Bug", "Task"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := range 100 {
		typ := types[i%len(types)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := c.States(typ); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("a concurrent States call returned %v", err)
	}
}
