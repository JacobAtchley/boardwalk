package azdo

import (
	"net/http"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		status, result string
		want           BuildStatus
	}{
		{"notStarted", "", StatusQueued},
		{"postponed", "", StatusQueued},
		{"inProgress", "", StatusRunning},
		{"cancelling", "", StatusRunning},
		{"completed", "succeeded", StatusSucceeded},
		{"completed", "partiallySucceeded", StatusPartial},
		{"completed", "failed", StatusFailed},
		{"completed", "canceled", StatusCanceled},
		{"completed", "", StatusFailed},
		{"", "", StatusQueued},
	} {
		if got := classify(tc.status, tc.result); got != tc.want {
			t.Errorf("classify(%q, %q) = %v, want %v", tc.status, tc.result, got, tc.want)
		}
	}
}

func TestBuildStatusDone(t *testing.T) {
	for s, want := range map[BuildStatus]bool{
		StatusQueued:    false,
		StatusRunning:   false,
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusPartial:   true,
		StatusCanceled:  true,
	} {
		if got := s.Done(); got != want {
			t.Errorf("%v.Done() = %v, want %v", s, got, want)
		}
	}
}

func TestSummarizeRunningBuildNamesTheStepInProgress(t *testing.T) {
	records := []Record{
		{Name: "Restore", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
		{Name: "Build", Type: "Task", State: "inProgress", Order: 2},
		{Name: "Test", Type: "Task", State: "pending", Order: 3},
	}

	got := summarize(records, StatusRunning)
	if got.CurrentStep != "Build" {
		t.Errorf("current step = %q, want Build", got.CurrentStep)
	}
	if got.Errors != 0 {
		t.Errorf("errors = %d, want 0", got.Errors)
	}
}

func TestSummarizeRunningBuildPrefersTheLowestOrder(t *testing.T) {
	// The timeline arrives unordered, and two tasks can be in flight at once.
	records := []Record{
		{Name: "Later", Type: "Task", State: "inProgress", Order: 5},
		{Name: "Earlier", Type: "Task", State: "inProgress", Order: 2},
	}

	if got := summarize(records, StatusRunning); got.CurrentStep != "Earlier" {
		t.Errorf("current step = %q, want Earlier", got.CurrentStep)
	}
}

func TestSummarizeFailedBuildNamesTheFailingStep(t *testing.T) {
	records := []Record{
		{Name: "Restore", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
		{Name: "Test", Type: "Task", State: "completed", Result: "failed", Order: 2, ErrorCount: 3},
		{Name: "Publish", Type: "Task", State: "completed", Result: "skipped", Order: 3},
	}

	got := summarize(records, StatusFailed)
	if got.CurrentStep != "Test" {
		t.Errorf("current step = %q, want Test", got.CurrentStep)
	}
	if got.Errors != 3 {
		t.Errorf("errors = %d, want 3", got.Errors)
	}
}

func TestSummarizeCountsOnlyTaskErrors(t *testing.T) {
	// Azure DevOps cascades a failed task's result up to the job and stage
	// containing it, so counting every failed record would report one broken
	// task three times.
	records := []Record{
		{Name: "CI", Type: "Stage", State: "completed", Result: "failed", Order: 1},
		{Name: "Build job", Type: "Job", State: "completed", Result: "failed", Order: 1},
		{Name: "Test", Type: "Task", State: "completed", Result: "failed", Order: 2, ErrorCount: 2},
	}

	got := summarize(records, StatusFailed)
	if got.Errors != 2 {
		t.Errorf("errors = %d, want 2 — the job and stage inherit the task's failure", got.Errors)
	}
	if got.CurrentStep != "Test" {
		t.Errorf("current step = %q, want Test", got.CurrentStep)
	}
}

func TestSummarizeCleanBuildHasNoCurrentStep(t *testing.T) {
	records := []Record{
		{Name: "Build", Type: "Task", State: "completed", Result: "succeeded", Order: 1},
	}

	got := summarize(records, StatusSucceeded)
	if got.CurrentStep != "" {
		t.Errorf("current step = %q, want it empty", got.CurrentStep)
	}
	if got.Errors != 0 {
		t.Errorf("errors = %d, want 0", got.Errors)
	}
}

func TestSummarizeCountsErrorsWithoutAnErrorCount(t *testing.T) {
	// Some tasks fail without reporting a count, so a failed record is worth at
	// least one error.
	records := []Record{
		{Name: "Test", Type: "Task", State: "completed", Result: "failed", Order: 1},
	}

	if got := summarize(records, StatusFailed); got.Errors != 1 {
		t.Errorf("errors = %d, want 1", got.Errors)
	}
}

func TestSummarizeQueuedBuild(t *testing.T) {
	if got := summarize(nil, StatusQueued); got.CurrentStep != "" || got.Errors != 0 {
		t.Errorf("summarize of an empty timeline = %+v, want it empty", got)
	}
}

func TestBuildsParsesTheListing(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("queryOrder"); got != "queueTimeDescending" {
			t.Errorf("queryOrder = %q, want queueTimeDescending", got)
		}
		if got := r.URL.Query().Get("$top"); got != "50" {
			t.Errorf("$top = %q, want 50", got)
		}
		w.Write([]byte(`{"value":[
		 {"id":9001,"buildNumber":"20260911.3","status":"inProgress",
		  "queueTime":"2026-09-11T11:00:00Z","sourceBranch":"refs/heads/main",
		  "definition":{"name":"platform-ci"},"requestedFor":{"displayName":"Dev Example"}},
		 {"id":9000,"buildNumber":"20260911.2","status":"completed","result":"failed",
		  "queueTime":"2026-09-11T10:00:00Z","sourceBranch":"refs/heads/feature/x",
		  "definition":{"name":"platform-web-ci"},"requestedFor":{"displayName":"Other Dev"}}
		]}`))
	})

	builds, err := c.Builds(50)
	if err != nil {
		t.Fatalf("Builds returned %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("got %d builds, want 2", len(builds))
	}
	if builds[0].Pipeline != "platform-ci" || builds[0].Status != StatusRunning {
		t.Errorf("first build = %+v", builds[0])
	}
	if builds[1].Status != StatusFailed {
		t.Errorf("second build status = %v, want failed", builds[1].Status)
	}
}

func TestTimelineSummarisesAndReturnsRecords(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Branch on the request path: timeline vs build endpoint
		if containsPath(r.URL.Path, "/timeline") {
			w.Write([]byte(`{"records":[
			 {"name":"Build","type":"Task","state":"inProgress","order":2,"log":{"id":7}},
			 {"name":"Restore","type":"Task","state":"completed","result":"succeeded","order":1,"log":{"id":6}},
			 {"name":"Job","type":"Job","state":"inProgress","order":1}
			]}`))
		} else {
			// BuildByID call
			w.Write([]byte(`{"id":9001,"buildNumber":"20260911.3","status":"inProgress"}`))
		}
	})

	progress, records, err := c.Timeline(9001)
	if err != nil {
		t.Fatalf("Timeline returned %v", err)
	}
	if progress.CurrentStep != "Build" {
		t.Errorf("current step = %q, want Build — a Job is not a step", progress.CurrentStep)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want all 3 returned for the log pane", len(records))
	}
	if records[0].Name != "Restore" {
		t.Errorf("records[0] = %q, want them sorted by order", records[0].Name)
	}
}

func TestBuildURL(t *testing.T) {
	c := &Client{Org: "acme", Project: "Platform"}
	want := "https://dev.azure.com/acme/Platform/_build/results?buildId=9001"
	if got := c.BuildURL(9001); got != want {
		t.Errorf("BuildURL = %q, want %q", got, want)
	}
}

// Helper function to check if a URL path contains a substring
func containsPath(path, substr string) bool {
	for i := 0; i <= len(path)-len(substr); i++ {
		if path[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLogLinesSplitsTheBodyAndPassesStartLine(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("startLine"); got != "3" {
			t.Errorf("startLine = %q, want 3", got)
		}
		w.Write([]byte("third\nfourth\n"))
	})

	lines, err := c.LogLines(9001, 7, 3)
	if err != nil {
		t.Fatalf("LogLines returned %v", err)
	}
	if len(lines) != 2 || lines[0] != "third" || lines[1] != "fourth" {
		t.Errorf("lines = %q, want the two lines without the trailing blank", lines)
	}
}

func TestLogLinesOnAnEmptyLog(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})

	lines, err := c.LogLines(9001, 7, 0)
	if err != nil {
		t.Fatalf("LogLines returned %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %q, want none", lines)
	}
}

func TestNewLogChunksReadsEachTaskOnceAndAdvancesTheCursor(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("startLine") {
		case "", "0":
			w.Write([]byte("one\ntwo\n"))
		default:
			w.Write([]byte("three\n"))
		}
	})

	records := []Record{
		{Name: "Restore", Type: "Task", Order: 1, LogID: 6},
		{Name: "Build", Type: "Task", Order: 2, LogID: 7},
		{Name: "Job", Type: "Job", Order: 1},      // no log of its own
		{Name: "Pending", Type: "Task", Order: 3}, // not started, LogID zero
	}
	cursor := LogCursor{}

	chunks, err := c.NewLogChunks(9001, records, cursor)
	if err != nil {
		t.Fatalf("NewLogChunks returned %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 — only tasks with a log id", len(chunks))
	}
	if chunks[0].Task != "Restore" || len(chunks[0].Lines) != 2 {
		t.Errorf("first chunk = %+v", chunks[0])
	}
	if cursor[6] != 2 || cursor[7] != 2 {
		t.Errorf("cursor = %v, want each log advanced to 2", cursor)
	}

	// A second pass must return only what has been appended since.
	again, err := c.NewLogChunks(9001, records, cursor)
	if err != nil {
		t.Fatalf("second NewLogChunks returned %v", err)
	}
	if len(again) != 2 || len(again[0].Lines) != 1 || again[0].Lines[0] != "three" {
		t.Errorf("second pass = %+v, want one new line per log", again)
	}
	if cursor[6] != 3 {
		t.Errorf("cursor = %v, want 3 after the second pass", cursor)
	}
}

func TestNewLogChunksSkipsALogWithNothingNew(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})

	records := []Record{{Name: "Build", Type: "Task", Order: 1, LogID: 7}}
	chunks, err := c.NewLogChunks(9001, records, LogCursor{7: 10})
	if err != nil {
		t.Fatalf("NewLogChunks returned %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("chunks = %+v, want none when the log has not grown", chunks)
	}
}
