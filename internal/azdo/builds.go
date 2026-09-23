package azdo

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// BuildStatus collapses Azure DevOps's status and result pair into the single
// value a list column can show.
type BuildStatus int

const (
	// StatusQueued means the build has not yet started.
	StatusQueued BuildStatus = iota
	// StatusRunning means the build is in progress.
	StatusRunning
	// StatusSucceeded means the build completed successfully.
	StatusSucceeded
	// StatusFailed means the build completed with failures.
	StatusFailed
	// StatusPartial means the build partially succeeded.
	StatusPartial
	// StatusCanceled means the build was canceled.
	StatusCanceled
)

// String reports the human-readable name of the build status.
func (s BuildStatus) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusSucceeded:
		return "succeeded"
	case StatusFailed:
		return "failed"
	case StatusPartial:
		return "partial"
	case StatusCanceled:
		return "canceled"
	default:
		return "queued"
	}
}

// Done reports whether the build has stopped changing, which is what tells the
// log pane to stop polling.
func (s BuildStatus) Done() bool {
	return s != StatusQueued && s != StatusRunning
}

// classify folds the API's two fields into one. A completed build with no
// result is treated as failed rather than as a success nobody recorded.
func classify(status, result string) BuildStatus {
	switch status {
	case "inProgress", "cancelling":
		return StatusRunning
	case "completed":
		switch result {
		case "succeeded":
			return StatusSucceeded
		case "partiallySucceeded":
			return StatusPartial
		case "canceled":
			return StatusCanceled
		default:
			return StatusFailed
		}
	default:
		return StatusQueued
	}
}

// Build is one pipeline run.
type Build struct {
	ID     int
	Number string
	// Pipeline is the definition's display name, and DefinitionID is the
	// definition itself. Both are kept because they answer different
	// questions: the name is what a row shows, and the id is the only thing
	// the queue endpoint will accept — queueing a run by pipeline name is
	// not something the API offers.
	DefinitionID int
	Pipeline     string
	RequestedFor string
	SourceBranch string
	Status       BuildStatus
	Queued       time.Time
}

// Issue is one problem a timeline record reported: the message Azure DevOps
// itself puts on a run's summary page, rather than a line somebody has to find
// in the log.
type Issue struct {
	Type    string // "error" or "warning"
	Message string
}

// Record is one entry in a build's timeline: a stage, a job, or a task.
type Record struct {
	Name       string
	Type       string
	State      string
	Result     string
	Order      int
	LogID      int
	ErrorCount int
	Issues     []Issue
}

// Failure is one failed task and the errors it reported, which together are
// the answer to why a build failed.
type Failure struct {
	Task   string
	Errors []string
}

// Failures reads the timeline as the account of what broke: every failed task
// in execution order, each with the error messages it published. Containers
// are left out for the reason summarize gives — Azure DevOps cascades a failed
// task's result up through its job and stage, and reporting those repeats one
// failure three times under names that did nothing.
//
// A task that failed without publishing an issue is still named. Its errors
// are empty rather than invented; what it has to say is in its log, and
// leaving it out would hide the failure entirely.
func Failures(records []Record) []Failure {
	failed := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Type == "Task" && r.Result == "failed" {
			failed = append(failed, r)
		}
	}
	// The timeline arrives in no particular order, and a digest that reads
	// out of order is a worse account than none.
	sort.SliceStable(failed, func(i, j int) bool { return failed[i].Order < failed[j].Order })

	out := make([]Failure, 0, len(failed))
	for _, r := range failed {
		f := Failure{Task: r.Name}
		for _, issue := range r.Issues {
			if issue.Type == "error" {
				f.Errors = append(f.Errors, issue.Message)
			}
		}
		out = append(out, f)
	}
	return out
}

// Progress is what the build list shows beyond the status glyph.
type Progress struct {
	CurrentStep string
	Errors      int
}

// summarize picks the step worth naming and totals the errors. For a running
// build that is whatever is executing; for a failed one it is what broke; for a
// build that finished clean there is nothing to say.
func summarize(records []Record, s BuildStatus) Progress {
	var p Progress
	var current *Record

	for i := range records {
		r := records[i]

		// Stages and jobs are containers, and naming one says less than naming
		// the task inside it. Azure DevOps cascades a failed task's result up
		// the container hierarchy, so only count task failures.
		if r.Type != "Task" {
			continue
		}

		if r.Result == "failed" {
			if r.ErrorCount > 0 {
				p.Errors += r.ErrorCount
			} else {
				// A task can fail without filling in a count.
				p.Errors++
			}
		}
		if !worthNaming(r, s) {
			continue
		}
		// Two tasks can be in flight at once, and the timeline is unordered, so
		// the earliest is the one to name.
		if current == nil || r.Order < current.Order {
			current = &records[i]
		}
	}

	if current != nil {
		p.CurrentStep = current.Name
	}
	return p
}

// worthNaming reports whether a task is the one the list should point at: what
// is executing on a running build, what broke on a failed one, and nothing at
// all on a build that finished clean.
func worthNaming(r Record, s BuildStatus) bool {
	if !s.Done() {
		return s == StatusRunning && r.State == "inProgress"
	}
	if s == StatusSucceeded {
		return false
	}
	return r.Result == "failed"
}

// Builds lists the project's most recent pipeline runs, newest first.
func (c *Client) Builds(top int) ([]Build, error) {
	var resp struct {
		Value []buildJSON `json:"value"`
	}

	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/build/builds?$top=%d&queryOrder=queueTimeDescending&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), top, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	builds := make([]Build, 0, len(resp.Value))
	for _, v := range resp.Value {
		builds = append(builds, v.build())
	}
	return builds, nil
}

// BuildByID refetches one build, which is how the log pane notices that the
// run it is tailing has finished.
func (c *Client) BuildByID(id int) (Build, error) {
	var v buildJSON
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	if err := c.get(endpoint, &v); err != nil {
		return Build{}, err
	}
	return v.build(), nil
}

type buildJSON struct {
	ID           int       `json:"id"`
	Number       string    `json:"buildNumber"`
	Status       string    `json:"status"`
	Result       string    `json:"result"`
	Queued       time.Time `json:"queueTime"`
	SourceBranch string    `json:"sourceBranch"`
	Definition   struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"definition"`
	RequestedFor struct {
		DisplayName string `json:"displayName"`
	} `json:"requestedFor"`
}

func (v buildJSON) build() Build {
	return Build{
		ID:           v.ID,
		Number:       v.Number,
		DefinitionID: v.Definition.ID,
		Pipeline:     v.Definition.Name,
		RequestedFor: v.RequestedFor.DisplayName,
		SourceBranch: v.SourceBranch,
		Status:       classify(v.Status, v.Result),
		Queued:       v.Queued,
	}
}

// Timeline returns a build's progress summary and its records in execution
// order. The records carry the log ids the log pane concatenates.
func (c *Client) Timeline(buildID int) (Progress, []Record, error) {
	var resp struct {
		Records []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			State      string `json:"state"`
			Result     string `json:"result"`
			Order      int    `json:"order"`
			ErrorCount int    `json:"errorCount"`
			Log        *struct {
				ID int `json:"id"`
			} `json:"log"`
			Issues []struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"issues"`
		} `json:"records"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/timeline?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), buildID, APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return Progress{}, nil, err
	}

	records := make([]Record, 0, len(resp.Records))
	for _, v := range resp.Records {
		r := Record{
			Name:       v.Name,
			Type:       v.Type,
			State:      v.State,
			Result:     v.Result,
			Order:      v.Order,
			ErrorCount: v.ErrorCount,
		}
		if v.Log != nil {
			r.LogID = v.Log.ID
		}
		for _, issue := range v.Issues {
			r.Issues = append(r.Issues, Issue{Type: issue.Type, Message: issue.Message})
		}
		records = append(records, r)
	}

	// The timeline arrives in no particular order, and both the current-step
	// pick and the log concatenation depend on execution order.
	sort.SliceStable(records, func(i, j int) bool { return records[i].Order < records[j].Order })

	build, err := c.BuildByID(buildID)
	if err != nil {
		return Progress{}, records, err
	}
	return summarize(records, build.Status), records, nil
}

// BuildURL is the browser URL for a build's results page.
func (c *Client) BuildURL(id int) string {
	return fmt.Sprintf("%s/%s/%s/_build/results?buildId=%d",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id)
}

// LogChunk is one task's log text, or the part of it that has not been shown
// yet.
type LogChunk struct {
	Task  string
	LogID int
	Lines []string
}

// LogCursor remembers how many lines of each log have already been consumed, so
// tailing a running build re-reads nothing.
type LogCursor map[int]int

// LogLines fetches one build log from startLine onward. The endpoint answers
// plain text, so this is the one call that does not decode JSON. The startLine
// parameter is treated as 0-based: the number of lines already consumed. The
// Azure DevOps API reference does not state the base; a 1-based base would
// re-show one line per poll.
func (c *Client) LogLines(buildID, logID, startLine int) ([]string, error) {
	endpoint := fmt.Sprintf(
		"%s/%s/%s/_apis/build/builds/%d/logs/%d?startLine=%d&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		buildID, logID, startLine, APIVersion)

	body, err := c.getText(endpoint)
	if err != nil {
		return nil, err
	}

	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil, nil
	}
	return strings.Split(body, "\n"), nil
}

// NewLogChunks returns whatever each task's log has gained since the cursor was
// last advanced, in execution order, and advances the cursor. Calling it on a
// fresh cursor reads the whole build; calling it again while the build runs
// reads only the tail.
func (c *Client) NewLogChunks(buildID int, records []Record, cursor LogCursor) ([]LogChunk, error) {
	var chunks []LogChunk

	for _, r := range records {
		// Stages and jobs have no log of their own, and a task that has not
		// started yet has no log id.
		if r.Type != "Task" || r.LogID == 0 {
			continue
		}

		lines, err := c.LogLines(buildID, r.LogID, cursor[r.LogID])
		if err != nil {
			return chunks, err
		}
		if len(lines) == 0 {
			continue
		}

		cursor[r.LogID] += len(lines)
		chunks = append(chunks, LogChunk{Task: r.Name, LogID: r.LogID, Lines: lines})
	}
	return chunks, nil
}

// QueueBuild starts a new run of a definition, optionally against a branch.
// The queued run comes back, so a caller can name it rather than only report
// that something was queued.
//
// branch is the full ref, refs/heads/main. An empty one is left out of the
// body altogether rather than sent as "": the pipeline's own default branch
// is what "no branch given" means, and an empty string would be read as a ref
// by that name.
func (c *Client) QueueBuild(definitionID int, branch string) (Build, error) {
	body := map[string]any{
		"definition": map[string]int{"id": definitionID},
	}
	if branch != "" {
		body["sourceBranch"] = branch
	}

	var v buildJSON
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), APIVersion)
	if err := c.post(endpoint, body, &v); err != nil {
		return Build{}, err
	}
	return v.build(), nil
}

// CancelBuild asks Azure DevOps to stop a run.
//
// "cancelling" rather than "cancelled" is deliberate and is what the API
// wants: cancelling is a request the agent has to notice and act on, not a
// state the caller can simply assert. The run stays in that state — which
// classify folds into StatusRunning — until the agent stops, which is why the
// log pane keeps tailing a cancelled build rather than treating it as done.
func (c *Client) CancelBuild(id int) error {
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, APIVersion)
	return c.patch(endpoint, "application/json", map[string]string{"status": "cancelling"}, nil)
}
