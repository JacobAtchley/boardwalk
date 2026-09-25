package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/notify"
	tea "github.com/charmbracelet/bubbletea"
)

// watchInterval is how often watched builds are checked. A build takes
// minutes; learning it finished twenty seconds late costs nothing, and the
// check is one request per watched build.
const watchInterval = 20 * time.Second

// WatchBuildMsg asks Root to start watching a build, or to stop if it already
// is. A view sends it rather than keeping the watch itself because a watch has
// to outlive the view it was started from: the point is to stop looking at the
// build.
type WatchBuildMsg struct{ Build azdo.Build }

// buildWatchTickMsg is the watch's own clock.
type buildWatchTickMsg struct{}

// watchOutcome is one watched build that has finished, and what happened when
// the desktop was told.
type watchOutcome struct {
	Build    azdo.Build
	Failures []azdo.Failure
	SendErr  error
}

// buildWatchResultMsg is what one pass over the watched builds found.
type buildWatchResultMsg struct {
	Finished []watchOutcome
	Errs     []error
}

// buildWatch is the set of builds Root is waiting on, and the seams it talks
// to the world through. fetch, timeline and send are fields rather than calls
// on the client so a test can drive the whole cycle without a server or a
// desktop, the same way gateRuns takes its fetch.
type buildWatch struct {
	watched  map[int]azdo.Build
	ticking  bool
	interval time.Duration

	fetch    func(id int) (azdo.Build, error)
	timeline func(id int) (azdo.Progress, []azdo.Record, error)
	send     func(title, message string) error
}

func newBuildWatch(c *azdo.Client) *buildWatch {
	return &buildWatch{
		watched:  map[int]azdo.Build{},
		interval: watchInterval,
		fetch:    func(id int) (azdo.Build, error) { return c.BuildByID(id) },
		timeline: func(id int) (azdo.Progress, []azdo.Record, error) { return c.Timeline(id) },
		send:     notify.Send,
	}
}

// toggle starts or stops watching b, returning the status line to show and
// the tick to start, if this is the first build being watched.
func (w *buildWatch) toggle(b azdo.Build) (StatusMsg, tea.Cmd) {
	label := buildLabel(b)
	if _, ok := w.watched[b.ID]; ok {
		delete(w.watched, b.ID)
		return StatusMsg{Text: "stopped watching " + label}, nil
	}
	if b.Status.Done() {
		return StatusMsg{Text: fmt.Sprintf("%s has already %s", label, b.Status)}, nil
	}
	w.watched[b.ID] = b
	status := StatusMsg{Text: fmt.Sprintf("watching %s — you'll get a notification when it finishes", label)}
	if w.ticking {
		return status, nil
	}
	w.ticking = true
	return status, w.tick()
}

func (w *buildWatch) tick() tea.Cmd {
	return tea.Tick(w.interval, func(time.Time) tea.Msg { return buildWatchTickMsg{} })
}

// poll checks every watched build once, off the update loop: it makes a
// request per build and runs the notifier, and neither belongs on the goroutine
// that paints the screen. The ids are copied out first so the command never
// reads the map while Update writes it.
func (w *buildWatch) poll() tea.Cmd {
	if len(w.watched) == 0 {
		w.ticking = false
		return nil
	}
	ids := make([]int, 0, len(w.watched))
	for id := range w.watched {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	fetch, timeline, send := w.fetch, w.timeline, w.send
	return func() tea.Msg {
		var msg buildWatchResultMsg
		for _, id := range ids {
			b, err := fetch(id)
			if err != nil {
				msg.Errs = append(msg.Errs, fmt.Errorf("could not check build %d: %w", id, err))
				continue
			}
			if !b.Status.Done() {
				continue
			}
			var failures []azdo.Failure
			if b.Status == azdo.StatusFailed || b.Status == azdo.StatusPartial {
				// A timeline that will not load still leaves a finished build
				// worth announcing; it just cannot say where it broke.
				if _, records, err := timeline(id); err == nil {
					failures = azdo.Failures(records)
				}
			}
			title, body := watchMessage(b, failures)
			msg.Finished = append(msg.Finished, watchOutcome{Build: b, Failures: failures, SendErr: send(title, body)})
		}
		return msg
	}
}

// apply drops the builds that finished, and schedules the next pass while any
// are left. A build whose check failed stays watched and is retried.
func (w *buildWatch) apply(msg buildWatchResultMsg) (StatusMsg, tea.Cmd) {
	var parts []string
	var failed bool
	for _, o := range msg.Finished {
		delete(w.watched, o.Build.ID)
		parts = append(parts, watchSummary(o.Build, o.Failures))
		if o.SendErr != nil {
			parts = append(parts, "could not notify: "+o.SendErr.Error())
			failed = true
		}
	}
	for _, err := range msg.Errs {
		parts = append(parts, err.Error())
		failed = true
	}

	var next tea.Cmd
	if len(w.watched) > 0 {
		next = w.tick()
	} else {
		w.ticking = false
	}
	return StatusMsg{Text: strings.Join(parts, "; "), Err: failed}, next
}

// watchMessage is the notification for a finished build.
func watchMessage(b azdo.Build, failures []azdo.Failure) (title, body string) {
	title = "boardwalk · build " + watchOutcomeWord(b.Status)
	body = buildLabel(b)
	if b.SourceBranch != "" {
		body += " · " + shortRef(b.SourceBranch)
	}
	if len(failures) > 0 {
		f := failures[0]
		body += "\nfailed at " + f.Task
		if len(f.Errors) > 0 {
			body += ": " + truncate(f.Errors[0], 120)
		}
	}
	return title, body
}

// watchSummary is the status line for a finished build.
func watchSummary(b azdo.Build, failures []azdo.Failure) string {
	label := buildLabel(b)
	switch b.Status {
	case azdo.StatusSucceeded:
		return "✓ " + label + " succeeded"
	case azdo.StatusFailed, azdo.StatusPartial:
		line := "✗ " + label + " " + watchOutcomeWord(b.Status)
		if len(failures) > 0 {
			line += " at " + failures[0].Task
		}
		return line
	default:
		return label + " " + b.Status.String()
	}
}

// watchOutcomeWord is how a finished build's status reads in a sentence.
func watchOutcomeWord(s azdo.BuildStatus) string {
	if s == azdo.StatusPartial {
		return "partially succeeded"
	}
	return s.String()
}
