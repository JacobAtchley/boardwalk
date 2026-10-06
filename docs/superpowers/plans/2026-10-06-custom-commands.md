# Custom Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the config bind a single key, on a kind of screen, to a user shell command that runs detached with the screen's subject in `BOARDWALK_*` environment variables.

**Architecture:** `config` gains a `Command` list and validates what it can alone. `ui` gains a runner (`custom.go`, `sh -c`, detached, reports within a 2 s window), a `subject` that eight views expose through an optional `Subject()` method, and Root-level dispatch: Root matches the key before the view does, skips it while the view is prompting or binds the key itself, runs the command, and delivers the outcome as a `StatusMsg` to the view it ran on — wherever that view now sits in the stack. Three views that mishandle `StatusMsg` today (Logs ignores it; PullRequestDetail and Item drop its error flag) are fixed first. Custom commands join the `?` panel, and so the palette, through a `help.KeyMap` wrapper like the existing `paletteKeys`.

**Tech Stack:** Go 1.27, bubbletea, bubbles (`key`, `help`, `list`), `os/exec`.

**Spec:** `docs/superpowers/specs/2026-10-06-custom-commands-design.md`

## Global Constraints

- Kinds are exactly `pullRequest`, `workItem`, `build`.
- Variables are `BOARDWALK_` + `KIND`, `ORG`, `PROJECT`, `ID`, `TITLE`, `URL`; pullRequest adds `REPO`, `SOURCE_BRANCH`, `TARGET_BRANCH`, `AUTHOR`, `IS_DRAFT`; workItem adds `TYPE`, `STATE`; build adds `NUMBER`, `PIPELINE`, `BRANCH`, `RESULT`. Branches have `refs/heads/` stripped.
- Values reach the command only as environment variables — never spliced into the command string.
- Commands run detached: stdin `/dev/null`, stdout discarded, stderr captured (capped), own process group; boardwalk never waits more than `commandWait` (2 s).
- Status texts, verbatim: `running "<name>"…`, `ran "<name>"`, `started "<name>"`, `"<name>" failed: <last stderr line or exit error>`, `could not run "<name>": <err>`, `nothing selected`.
- A built-in key always wins over a custom one; reserved keys, list-movement keys and the palette key are refused at startup.
- CI builds on Windows and macOS: unix-only syscall fields go in a `//go:build !windows` file; Windows runs `cmd /C`. Tests that need `sh` skip on Windows.
- Code style: match the repo — doc comments that explain *why*, table-driven tests, indirected package variables for side effects (`copyToClipboard` pattern).
- Commit messages: conventional (`feat:`, `docs:`, `test:`), ending with the attribution lines from the session.

## Review Focus

1. **A key the list moves with but does not list in `Keys()`** (`l`, `h`, `b`, `u`, `f`, `d`, `pgup`…) — expected: refused at startup, not silently stealing paging on list views. Pinned in Task 5 (`TestCheckCommandsRefusesListMovementKeys`).
2. **The custom key pressed while a vote or draft toggle is armed** — expected: the arm sees the key (cancels), the command does not run. Pinned in Task 5 (`TestRootSkipsACommandWhileAVoteIsArmed`).
3. **Navigating away before the result arrives** — expected: the result is not shown as the status of a different screen, and is waiting on the screen it ran on when the user comes back. Pinned in Task 5 (`TestRootDeliversAResultToTheViewItRanOn`).
4. **A command that writes a lot to stderr, over many lines** — expected: only the last line is shown, memory stays bounded. Pinned in Task 2 (`TestStartShellReportsOnlyTheLastStderrLine`).
5. **A title holding shell syntax** (`$(touch pwned)`) — expected: inert. Pinned in Task 2 (`TestStartShellNeverEvaluatesValues`).

---

## File Structure

| file | responsibility |
|------|----------------|
| `internal/config/config.go` (modify) | `Command`, kind constants, `ValidateCommands` |
| `internal/ui/custom.go` (create) | runner: `startShell`, `runShell` var, `commandWait`, `tailBuffer` |
| `internal/ui/custom_unix.go` (create) | `shellCommand` for unix: `sh -c`, `Setpgid` |
| `internal/ui/custom_windows.go` (create) | `shellCommand` for Windows: `cmd /C`, new process group |
| `internal/ui/subject.go` (create) | `subject`, `subjecter`, the three subject builders |
| `internal/ui/rootcommands.go` (create) | `WithCommands`, `CheckCommands`, dispatch, help wrapper |
| 8 view files (modify) | one `Subject()` method each; Logs, PullRequestDetail and Item also learn to show a `StatusMsg` error |
| `internal/ui/root.go`, `rootpalette.go` (modify) | call into rootcommands.go |
| `main.go` (modify) | wire validation and `WithCommands` |
| `README.md` (modify) | document `commands` |

---

### Task 1: Config — the `commands` list

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Command struct { On, Key, Name, Run string }` (json `on`, `key`, `name`, `run`)
  - `const KindPullRequest = "pullRequest"`, `KindWorkItem = "workItem"`, `KindBuild = "build"`
  - `var CommandKinds = []string{KindPullRequest, KindWorkItem, KindBuild}`
  - `Config.Commands []Command` (json `commands,omitempty`)
  - `func (c Config) ValidateCommands(path string) error`

- [ ] **Step 1: Write the failing tests** — append to `internal/config/config_test.go`:

```go
func TestLoadReadsCommands(t *testing.T) {
	t.Setenv(EnvPath, writeConfig(t, `{
		"org": "acme", "project": "Platform",
		"commands": [{"on": "pullRequest", "key": "ctrl+r", "name": "review", "run": "echo hi"}]
	}`))

	c, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	want := Command{On: "pullRequest", Key: "ctrl+r", Name: "review", Run: "echo hi"}
	if len(c.Commands) != 1 || c.Commands[0] != want {
		t.Errorf("commands = %+v, want [%+v]", c.Commands, want)
	}
}

func TestValidateCommands(t *testing.T) {
	ok := Command{On: KindPullRequest, Key: "ctrl+r", Name: "review", Run: "echo hi"}
	with := func(edit func(*Command)) Command { c := ok; edit(&c); return c }

	for _, tc := range []struct {
		name string
		cmds []Command
		want string // substring of the error; empty means valid
	}{
		{"none", nil, ""},
		{"one good", []Command{ok}, ""},
		{"same key on two kinds", []Command{ok, with(func(c *Command) { c.On = KindBuild })}, ""},
		{"unknown kind", []Command{with(func(c *Command) { c.On = "pr" })}, `on "pr"`},
		{"no key", []Command{with(func(c *Command) { c.Key = "" })}, "sets no key"},
		{"no name", []Command{with(func(c *Command) { c.Name = " " })}, "sets no name"},
		{"no run", []Command{with(func(c *Command) { c.Run = "" })}, "sets no run"},
		{"duplicate", []Command{ok, ok}, "as commands[0] already does"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Config{Commands: tc.cmds}.ValidateCommands("/cfg.json")
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("got %v, want valid", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), "/cfg.json"):
				t.Errorf("%v does not name the file", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -run 'TestLoadReadsCommands|TestValidateCommands'`
Expected: FAIL — `undefined: Command`.

- [ ] **Step 3: Implement** — in `internal/config/config.go`, add `"slices"` to imports, add the field to `Config` after `PaletteKey`:

```go
	// Commands bind a key on one kind of screen to a shell command of the
	// user's own. See Command.
	Commands []Command `json:"commands,omitempty"`
```

and below `Config`:

```go
// The kinds of screen a command can be bound on, named for what the screen
// shows rather than which view shows it: a pull request is a pull request
// whether it is a row in the list or the pane it opens.
const (
	KindPullRequest = "pullRequest"
	KindWorkItem    = "workItem"
	KindBuild       = "build"
)

// CommandKinds is every kind a command's On may name.
var CommandKinds = []string{KindPullRequest, KindWorkItem, KindBuild}

// Command is one user-defined action: on a screen of kind On, the key Key
// runs Run with sh -c. Name is what the help panel, the palette and the
// status line call it.
//
// Run is the only action there is. Another kind of action would be a field
// beside it rather than a type tag, so a config written today keeps meaning
// what it says.
type Command struct {
	On   string `json:"on"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Run  string `json:"run"`
}

// ValidateCommands reports the first command that cannot work, naming the
// file and the command's place in the list. Whether Key names a real key, and
// one boardwalk does not need itself, is ui.CheckCommands' job: only the UI
// knows its keys.
func (c Config) ValidateCommands(path string) error {
	seen := make(map[[2]string]int, len(c.Commands))
	for i, cmd := range c.Commands {
		where := fmt.Sprintf("%s: commands[%d]", path, i)
		switch {
		case !slices.Contains(CommandKinds, cmd.On):
			return fmt.Errorf("%s has on %q — use one of %s", where, cmd.On, strings.Join(CommandKinds, ", "))
		case cmd.Key == "":
			return fmt.Errorf("%s sets no key", where)
		case strings.TrimSpace(cmd.Name) == "":
			return fmt.Errorf("%s sets no name", where)
		case strings.TrimSpace(cmd.Run) == "":
			return fmt.Errorf("%s sets no run", where)
		}
		k := [2]string{cmd.On, cmd.Key}
		if j, dup := seen[k]; dup {
			return fmt.Errorf("%s binds %q on %s, as commands[%d] already does", where, cmd.Key, cmd.On, j)
		}
		seen[k] = i
	}
	return nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: read custom commands from the config"
```

---

### Task 2: The runner

**Files:**
- Create: `internal/ui/custom.go`, `internal/ui/custom_unix.go`, `internal/ui/custom_windows.go`
- Test: `internal/ui/custom_test.go`

**Interfaces:**
- Produces:
  - `var runShell func(name, script string, env []string) StatusMsg` (defaults to `startShell`)
  - `var commandWait = 2 * time.Second`
  - `func shellCommand(script string) *exec.Cmd` (per platform)

- [ ] **Step 1: Write the failing tests** — `internal/ui/custom_test.go`:

```go
package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// needSh skips a test that runs a real shell script on a platform without sh.
func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the runner tests drive sh")
	}
}

func shortWait(t *testing.T, d time.Duration) {
	t.Helper()
	before := commandWait
	commandWait = d
	t.Cleanup(func() { commandWait = before })
}

func TestStartShellPassesTheEnvironment(t *testing.T) {
	needSh(t)
	out := filepath.Join(t.TempDir(), "env")

	got := startShell("dump", `env > "$OUT"`, []string{"OUT=" + out, "BOARDWALK_ID=4021"})

	if got.Err || got.Text != `ran "dump"` {
		t.Fatalf("status = %+v", got)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "BOARDWALK_ID=4021\n") {
		t.Errorf("the command did not see BOARDWALK_ID:\n%s", body)
	}
	if !strings.Contains(string(body), "PATH=") {
		t.Error("the command did not inherit boardwalk's environment")
	}
}

func TestStartShellReportsFailure(t *testing.T) {
	needSh(t)
	for _, tc := range []struct{ script, want string }{
		{`echo "no such tab" >&2; exit 3`, `"go" failed: no such tab`},
		{`exit 3`, `"go" failed: exit status 3`},
	} {
		got := startShell("go", tc.script, nil)
		if !got.Err || got.Text != tc.want {
			t.Errorf("%s: status = %+v, want error %q", tc.script, got, tc.want)
		}
	}
}

func TestStartShellReportsOnlyTheLastStderrLine(t *testing.T) {
	needSh(t)
	// Ten thousand lines of noise and then the reason: the status line has
	// room for one, and the last is the one a failing script ends on.
	got := startShell("noisy", `i=0; while [ $i -lt 10000 ]; do echo "noise $i" >&2; i=$((i+1)); done; echo "the reason" >&2; exit 1`, nil)
	if got.Text != `"noisy" failed: the reason` {
		t.Errorf("status = %q", got.Text)
	}
}

func TestStartShellStopsWaitingOnALongCommand(t *testing.T) {
	needSh(t)
	shortWait(t, 100*time.Millisecond)

	start := time.Now()
	got := startShell("slow", "sleep 5", nil)

	if got.Err || got.Text != `started "slow"` {
		t.Errorf("status = %+v", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the runner waited on the command instead of detaching")
	}
}

func TestStartShellNeverEvaluatesValues(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	// A title is written by somebody else. Quoted as a variable it is data.
	got := startShell("t", `printf '%s' "$BOARDWALK_TITLE" > /dev/null`,
		[]string{"BOARDWALK_TITLE=$(touch " + filepath.Join(dir, "pwned") + ")"})
	if got.Err {
		t.Fatalf("status = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("a value was run as shell")
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	var b tailBuffer
	for range 100 {
		b.Write([]byte(strings.Repeat("x", 100) + "\n"))
	}
	b.Write([]byte("last\n\n"))
	if len(b.b) > tailLimit {
		t.Errorf("buffer grew to %d bytes", len(b.b))
	}
	if b.lastLine() != "last" {
		t.Errorf("lastLine = %q", b.lastLine())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestStartShell|TestTailBuffer'`
Expected: FAIL — `undefined: startShell`.

- [ ] **Step 3: Implement** — `internal/ui/custom.go`:

```go
package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// commandWait is how long a custom command is watched before boardwalk stops
// waiting and reports it as started. A command like "open a terminal tab"
// finishes well inside it, so its real outcome is what the status line says;
// one that keeps running is left to run. A variable so tests can shorten it.
var commandWait = 2 * time.Second

// runShell runs a custom command, indirected so Root's tests can see what
// would have run without running it.
var runShell = startShell

// startShell runs script detached from the terminal and reports what became
// of it within commandWait.
//
// Nothing the command prints may reach the terminal: bubbletea is drawing on
// it, and a stray line would tear the screen until the next repaint. So
// stdin is /dev/null, stdout is dropped, and stderr is kept — its last line
// is the reason a failing script gives.
func startShell(name, script string, env []string) StatusMsg {
	cmd := shellCommand(script)
	cmd.Env = append(os.Environ(), env...)
	var stderr tailBuffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return StatusMsg{Text: fmt.Sprintf("could not run %q: %v", name, err), Err: true}
	}

	// Wait runs on its own goroutine so a command still going after
	// commandWait is reaped when it does finish, rather than left a zombie.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return StatusMsg{Text: fmt.Sprintf("ran %q", name)}
		}
		if line := stderr.lastLine(); line != "" {
			return StatusMsg{Text: fmt.Sprintf("%q failed: %s", name, line), Err: true}
		}
		return StatusMsg{Text: fmt.Sprintf("%q failed: %v", name, err), Err: true}
	case <-time.After(commandWait):
		return StatusMsg{Text: fmt.Sprintf("started %q", name)}
	}
}

// tailLimit bounds what tailBuffer keeps of a command's stderr.
const tailLimit = 4096

// tailBuffer keeps the end of what is written to it. Only the last line is
// ever shown, and a chatty command must not grow boardwalk without bound.
//
// It is written by exec's copying goroutine and read only after Wait has
// returned, which orders the two, so it needs no lock.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > tailLimit {
		t.b = append(t.b[:0], t.b[len(t.b)-tailLimit:]...)
	}
	return len(p), nil
}

// lastLine is the last non-blank line written.
func (t *tailBuffer) lastLine() string {
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
```

`internal/ui/custom_unix.go`:

```go
//go:build !windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with sh, in a process group of its own. The
// group is what keeps the command alive past boardwalk: a ctrl+c that quits
// boardwalk is delivered by the terminal to its foreground group, and the
// command is no longer in it.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
```

`internal/ui/custom_windows.go`:

```go
//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

// shellCommand runs script with cmd, in a process group of its own, for the
// reason the unix version gives.
func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}
```

- [ ] **Step 4: Run to verify pass, and cross-compile**

Run: `go test ./internal/ui/ -run 'TestStartShell|TestTailBuffer' && GOOS=windows go vet ./internal/ui/`
Expected: PASS, and vet clean for Windows.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/custom.go internal/ui/custom_unix.go internal/ui/custom_windows.go internal/ui/custom_test.go
git commit -m "feat: run a shell command detached and report how it went"
```

---

### Task 3: Subjects

**Files:**
- Create: `internal/ui/subject.go`
- Modify: `internal/ui/pullrequests.go`, `pullrequestdetail.go`, `prdiff.go`, `fileview.go`, `workitems.go`, `item.go`, `builds.go`, `logs.go` (one method each, placed after the view's `Prompting` or `Status`)
- Test: `internal/ui/subject_test.go`

**Interfaces:**
- Consumes: `config.KindPullRequest`, `config.KindWorkItem`, `config.KindBuild` (Task 1)
- Produces:
  - `type subject struct { kind string; env map[string]string }` — `env` keys have no `BOARDWALK_` prefix and hold `ID`, `TITLE`, `URL` plus the kind's own
  - `type subjecter interface { Subject() (subject, bool) }` — `kind` is set even when the bool is false
  - `pullRequestSubject(c *azdo.Client, pr azdo.PullRequest) subject`, `workItemSubject(c *azdo.Client, wi azdo.WorkItem) subject`, `buildSubject(c *azdo.Client, b azdo.Build) subject`

- [ ] **Step 1: Write the failing tests** — `internal/ui/subject_test.go`:

```go
package ui

import (
	"maps"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
)

func subjectPR() azdo.PullRequest {
	return azdo.PullRequest{ID: 812, Title: "Retry on 5xx", Repo: "platform", Author: "Dev Example",
		IsDraft: true, Source: "refs/heads/feature/retry", Target: "refs/heads/main"}
}

func TestPullRequestSubject(t *testing.T) {
	c, _ := fixture()
	want := map[string]string{
		"ID": "812", "TITLE": "Retry on 5xx", "URL": c.PullRequestURL("platform", 812),
		"REPO": "platform", "SOURCE_BRANCH": "feature/retry", "TARGET_BRANCH": "main",
		"AUTHOR": "Dev Example", "IS_DRAFT": "true",
	}
	got := pullRequestSubject(c, subjectPR())
	if got.kind != config.KindPullRequest || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestWorkItemSubject(t *testing.T) {
	c, items := fixture()
	wi := items[0]
	want := map[string]string{
		"ID": "4021", "TITLE": wi.Title, "URL": c.WorkItemURL(4021),
		"TYPE": "User Story", "STATE": "Active",
	}
	got := workItemSubject(c, wi)
	if got.kind != config.KindWorkItem || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestBuildSubject(t *testing.T) {
	c, _ := fixture()
	b := buildStub()
	b.SourceBranch = "refs/heads/main"
	want := map[string]string{
		"ID": "9001", "TITLE": "platform-ci #20260911.3", "URL": c.BuildURL(9001),
		"NUMBER": "20260911.3", "PIPELINE": "platform-ci", "BRANCH": "main", "RESULT": "succeeded",
	}
	got := buildSubject(c, b)
	if got.kind != config.KindBuild || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestEveryViewWithASubjectNamesIt(t *testing.T) {
	c, items := fixture()
	pr := subjectPR()

	prs := NewPullRequests(c)
	prs.Update(prsMsg{PRs: []azdo.PullRequest{pr}})
	wis := NewWorkItems(c, items, false, false)
	builds := NewBuilds(c)
	builds.Update(buildsMsg{Builds: []azdo.Build{buildStub()}})

	for _, tc := range []struct {
		name string
		view View
		kind string
		id   string
	}{
		{"pr list", prs, config.KindPullRequest, "812"},
		{"pr detail", NewPullRequestDetail(c, pr, nil), config.KindPullRequest, "812"},
		{"pr diff", NewPullRequestDiff(c, pr, nil, threadsAll), config.KindPullRequest, "812"},
		{"work items", wis, config.KindWorkItem, "4021"},
		{"item", NewItem(c, items[0], nil), config.KindWorkItem, "4021"},
		{"builds", builds, config.KindBuild, "9001"},
		{"logs", NewLogs(c, buildStub(), nil), config.KindBuild, "9001"},
	} {
		s, ok := tc.view.(subjecter)
		if !ok {
			t.Errorf("%s has no Subject", tc.name)
			continue
		}
		got, ok := s.Subject()
		if !ok || got.kind != tc.kind || got.env["ID"] != tc.id {
			t.Errorf("%s: Subject() = %+v, %v; want %s %s", tc.name, got, ok, tc.kind, tc.id)
		}
	}
}

func TestAnEmptyListHasNoSubjectButKeepsItsKind(t *testing.T) {
	c, _ := fixture()
	for _, tc := range []struct {
		view subjecter
		kind string
	}{
		{NewPullRequests(c), config.KindPullRequest},
		{NewWorkItems(c, nil, false, false), config.KindWorkItem},
		{NewBuilds(c), config.KindBuild},
	} {
		got, ok := tc.view.Subject()
		if ok || got.kind != tc.kind {
			t.Errorf("%T: Subject() = %+v, %v; want kind %s and nothing selected", tc.view, got, ok, tc.kind)
		}
	}
}
```

Before running, check two names against the code and adjust the test (not the code) if they differ: the zero `threadFilter` constant passed to `NewPullRequestDiff` (`grep -n "threadFilter = iota" -A3 internal/ui/threadfilter.go`), and whether `NewPullRequests`/`NewBuilds` need a `tea.WindowSizeMsg` before `Selected()` returns a row (if so, send `tea.WindowSizeMsg{Width: 140, Height: 30}` first). `FileView` is covered in Task 5 through Root, because its constructor needs file text; give it the method here all the same.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'Subject'`
Expected: FAIL — `undefined: pullRequestSubject`.

- [ ] **Step 3: Implement** — `internal/ui/subject.go`:

```go
package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
)

// subject is the one thing a screen is showing that a custom command can act
// on: its kind, and the variables that describe it, named without their
// BOARDWALK_ prefix. Root adds the prefix and the variables every kind shares.
type subject struct {
	kind string
	env  map[string]string
}

// subjecter is a view a custom command can run on. kind is set even when ok
// is false — an empty list is still a pull request screen, and Root has to
// know which commands to say "nothing selected" for.
type subjecter interface {
	Subject() (s subject, ok bool)
}

// branchName strips refs/heads/. shortRef is not used: it answers "-" for an
// empty ref, which reads well in a column and badly in a script.
func branchName(ref string) string { return strings.TrimPrefix(ref, "refs/heads/") }

func pullRequestSubject(c *azdo.Client, pr azdo.PullRequest) subject {
	return subject{kind: config.KindPullRequest, env: map[string]string{
		"ID":            strconv.Itoa(pr.ID),
		"TITLE":         pr.Title,
		"URL":           c.PullRequestURL(pr.Repo, pr.ID),
		"REPO":          pr.Repo,
		"SOURCE_BRANCH": branchName(pr.Source),
		"TARGET_BRANCH": branchName(pr.Target),
		"AUTHOR":        pr.Author,
		"IS_DRAFT":      strconv.FormatBool(pr.IsDraft),
	}}
}

func workItemSubject(c *azdo.Client, wi azdo.WorkItem) subject {
	return subject{kind: config.KindWorkItem, env: map[string]string{
		"ID":    strconv.Itoa(wi.ID),
		"TITLE": wi.Title,
		"URL":   c.WorkItemURL(wi.ID),
		"TYPE":  wi.Type,
		"STATE": wi.State,
	}}
}

func buildSubject(c *azdo.Client, b azdo.Build) subject {
	return subject{kind: config.KindBuild, env: map[string]string{
		"ID":       strconv.Itoa(b.ID),
		"TITLE":    fmt.Sprintf("%s #%s", b.Pipeline, b.Number),
		"URL":      c.BuildURL(b.ID),
		"NUMBER":   b.Number,
		"PIPELINE": b.Pipeline,
		"BRANCH":   branchName(b.SourceBranch),
		"RESULT":   b.Status.String(),
	}}
}
```

Then one method per view. The list views read the selected row:

```go
// pullrequests.go
// Subject is the selected pull request, for a custom command.
func (m *PullRequests) Subject() (subject, bool) {
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(prRow); ok {
			return pullRequestSubject(m.client, r.PullRequest), true
		}
	}
	return subject{kind: config.KindPullRequest}, false
}

// workitems.go
// Subject is the selected work item, for a custom command.
func (m *WorkItems) Subject() (subject, bool) {
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(workItemRow); ok {
			return workItemSubject(m.client, r.WorkItem), true
		}
	}
	return subject{kind: config.KindWorkItem}, false
}

// builds.go
// Subject is the selected run, for a custom command.
func (m *Builds) Subject() (subject, bool) {
	if row, ok := m.browser.Selected(); ok {
		if r, ok := row.(buildRow); ok {
			return buildSubject(m.client, r.Build), true
		}
	}
	return subject{kind: config.KindBuild}, false
}
```

The detail views always have theirs:

```go
// pullrequestdetail.go
// Subject is the pull request this pane shows, for a custom command.
func (m *PullRequestDetail) Subject() (subject, bool) { return pullRequestSubject(m.client, m.pr), true }

// prdiff.go
// Subject is the pull request whose change this is, for a custom command.
func (m *PullRequestDiff) Subject() (subject, bool) { return pullRequestSubject(m.client, m.pr), true }

// fileview.go
// Subject is the pull request this file belongs to, for a custom command.
func (m *FileView) Subject() (subject, bool) { return pullRequestSubject(m.client, m.pr), true }

// item.go
// Subject is the work item this pane shows, for a custom command.
func (m *Item) Subject() (subject, bool) { return workItemSubject(m.client, m.item), true }

// logs.go
// Subject is the run whose log this is, for a custom command.
func (m *Logs) Subject() (subject, bool) { return buildSubject(m.client, m.build), true }
```

Add the `config` import to `pullrequests.go`, `workitems.go` and `builds.go` if absent.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ui/`
Expected: PASS, whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat: name what each screen shows for custom commands"
```

---

### Task 4: Every subject view shows a StatusMsg, error colour included

A custom command reports through `StatusMsg`, delivered to the view it ran on. Three of the eight views mishandle it today: `Logs` has no `case StatusMsg` at all, and `PullRequestDetail` and `Item` keep the text but drop `Err`. Their `failed` field cannot simply take `Err`: in both it also means "the discussion did not load", and the body reads it to replace the discussion with `could not load the discussion — press r to try again`.

The fix records which status text was an error, rather than adding a sticky flag: any later status assignment changes `m.status`, and the red goes with it without every existing assignment having to clear a flag.

**Files:**
- Modify: `internal/ui/logs.go` (`Update`), `internal/ui/pullrequestdetail.go` (struct, `Update`, `Status`), `internal/ui/item.go` (struct, `Update`, `Status`)
- Test: `internal/ui/statusmsg_test.go`

**Interfaces:**
- Produces: on `Logs`, `PullRequestDetail` and `Item`, `Update(StatusMsg{Text, Err})` makes `Status()` return `(… + Text, Err)` until the view next sets its status; the body is unaffected.

- [ ] **Step 1: Write the failing tests** — `internal/ui/statusmsg_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func TestSubjectViewsShowAStatusMsgWithItsColour(t *testing.T) {
	c, items := fixture()
	for _, tc := range []struct {
		name string
		view View
	}{
		{"logs", NewLogs(c, buildStub(), nil)},
		{"pr detail", NewPullRequestDetail(c, subjectPR(), nil)},
		{"item", NewItem(c, items[0], nil)},
	} {
		v, _ := tc.view.Update(StatusMsg{Text: "boom", Err: true})
		if text, isErr := v.Status(); !strings.HasSuffix(text, "boom") || !isErr {
			t.Errorf("%s: after an error, Status() = %q, %v", tc.name, text, isErr)
		}
		v, _ = v.Update(StatusMsg{Text: "fine"})
		if text, isErr := v.Status(); !strings.HasSuffix(text, "fine") || isErr {
			t.Errorf("%s: after a success, Status() = %q, %v", tc.name, text, isErr)
		}
	}
}

func TestAStatusErrorDoesNotClaimTheDiscussionFailed(t *testing.T) {
	// failed means the discussion did not load, and the body says so. A
	// command failing is not that.
	c, items := fixture()
	for _, tc := range []struct {
		name string
		view View
	}{
		{"pr detail", NewPullRequestDetail(c, subjectPR(), nil)},
		{"item", NewItem(c, items[0], nil)},
	} {
		v, _ := tc.view.Update(StatusMsg{Text: "boom", Err: true})
		if body := v.Body(120, 40); strings.Contains(body, "could not load the discussion") {
			t.Errorf("%s: a status error replaced the discussion:\n%s", tc.name, body)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestSubjectViewsShowAStatusMsg|TestAStatusErrorDoesNot'`
Expected: FAIL — logs shows no "boom"; pr detail and item report `isErr` false.

- [ ] **Step 3: Implement**

`internal/ui/logs.go`, in `Update`'s switch, beside the other cases:

```go
	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil
```

(`Logs.failed` only means "the last status was an error" — it keeps a failed poll's message from being cleared — so it can take `Err` directly.)

`internal/ui/pullrequestdetail.go` — add to the struct, after `status string`:

```go
	// errStatus is the status text, when a StatusMsg said it was an error.
	// It is not a flag: failed already means the discussion did not load, and
	// a flag would stay set after the next status replaced this one. Comparing
	// text means the red goes when the message does.
	errStatus string
```

replace the `StatusMsg` case:

```go
	case StatusMsg:
		m.status, m.errStatus = msg.Text, ""
		if msg.Err {
			m.errStatus = msg.Text
		}
		return m, nil
```

and the end of `Status`:

```go
	return m.work.View() + m.status, m.failed || (m.errStatus != "" && m.errStatus == m.status)
```

`internal/ui/item.go` — the same field after `status string`, with the same comment; the same `StatusMsg` case; and `Status`'s last line:

```go
	return m.work.View() + m.status, m.failed || m.stateErr || m.assignErr || m.commentErr ||
		(m.errStatus != "" && m.errStatus == m.status)
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ui/`
Expected: PASS, whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/logs.go internal/ui/pullrequestdetail.go internal/ui/item.go internal/ui/statusmsg_test.go
git commit -m "fix: show a status message, and its error colour, on every detail pane"
```

---

### Task 5: Root — dispatch, help and palette

**Files:**
- Create: `internal/ui/rootcommands.go`
- Modify: `internal/ui/root.go` (fields, `Update`, `key`, `View`), `internal/ui/rootpalette.go` (`paletteEntries`, `paletteBinding`)
- Test: `internal/ui/rootcommands_test.go`

**Interfaces:**
- Consumes: `config.Command` (Task 1); `runShell` (Task 2); `subject`, `subjecter` (Task 3); every subject view honouring `StatusMsg.Err` (Task 4)
- Produces:
  - `func WithCommands(cmds []config.Command) RootOption`
  - `func CheckCommands(cmds []config.Command, paletteKey string) error`

- [ ] **Step 1: Write the failing tests** — `internal/ui/rootcommands_test.go`:

```go
package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

var ctrlR = tea.KeyMsg{Type: tea.KeyCtrlR}

func reviewCommand() config.Command {
	return config.Command{On: config.KindPullRequest, Key: "ctrl+r", Name: "review", Run: "review.sh"}
}

type ranCommand struct {
	name, script string
	env          []string
}

// stubRunner records what would have run, and reports it ran.
func stubRunner(t *testing.T) *[]ranCommand {
	t.Helper()
	var ran []ranCommand
	before := runShell
	runShell = func(name, script string, env []string) StatusMsg {
		ran = append(ran, ranCommand{name, script, env})
		return StatusMsg{Text: `ran "` + name + `"`}
	}
	t.Cleanup(func() { runShell = before })
	return &ran
}

// rootOn builds a Root with cmds and v pushed on top.
func rootOn(t *testing.T, v View, cmds ...config.Command) *Root {
	t.Helper()
	c, _ := fixture()
	r := NewRoot(c, false, false, "", WithCommands(cmds))
	r, _ = send(t, r, tea.WindowSizeMsg{Width: 140, Height: 30}, PushMsg{View: v})
	return r
}

// run executes a command's tea.Cmd and delivers what it produced, the way
// bubbletea would. A batch is unpacked one level.
func run(t *testing.T, r *Root, cmd tea.Cmd) *Root {
	t.Helper()
	if cmd == nil {
		return r
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			r = run(t, r, c)
		}
		return r
	}
	r, _ = send(t, r, msg)
	return r
}

func statusOf(r *Root) (string, bool) {
	top, _ := r.top()
	return top.Status()
}

func TestRootRunsACommandWithTheSubject(t *testing.T) {
	ran := stubRunner(t)
	c, _ := fixture()
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), reviewCommand())

	r, cmd := send(t, r, ctrlR)
	if text, _ := statusOf(r); text != `running "review"…` {
		t.Errorf("status while running = %q", text)
	}
	r = run(t, r, cmd)

	if len(*ran) != 1 || (*ran)[0].script != "review.sh" {
		t.Fatalf("ran = %+v", *ran)
	}
	env := (*ran)[0].env
	for _, want := range []string{
		"BOARDWALK_KIND=pullRequest", "BOARDWALK_ORG=acme", "BOARDWALK_PROJECT=Platform",
		"BOARDWALK_ID=812", "BOARDWALK_SOURCE_BRANCH=feature/retry",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env is missing %s: %v", want, env)
		}
	}
	if text, isErr := statusOf(r); text != `ran "review"` || isErr {
		t.Errorf("status = %q, %v", text, isErr)
	}
}

func TestRootIgnoresACommandForAnotherKind(t *testing.T) {
	ran := stubRunner(t)
	c, items := fixture()
	r := rootOn(t, NewItem(c, items[0], nil), reviewCommand())

	r, cmd := send(t, r, ctrlR)
	r = run(t, r, cmd)

	if len(*ran) != 0 {
		t.Errorf("a pull request command ran on a work item: %+v", *ran)
	}
}

func TestRootSaysNothingSelectedOnAnEmptyList(t *testing.T) {
	ran := stubRunner(t)
	c, _ := fixture()
	r := rootOn(t, NewPullRequests(c), reviewCommand())

	r, cmd := send(t, r, ctrlR)
	r = run(t, r, cmd)

	if len(*ran) != 0 {
		t.Errorf("ran with nothing selected: %+v", *ran)
	}
	if text, _ := statusOf(r); text != "nothing selected" {
		t.Errorf("status = %q", text)
	}
}

func TestRootLetsTheViewsOwnKeyWin(t *testing.T) {
	// A on the pull request pane approves. A custom command on A must not
	// shadow it, and must not be advertised where it would not run.
	ran := stubRunner(t)
	c, _ := fixture()
	approveAlias := config.Command{On: config.KindPullRequest, Key: "A", Name: "my approve", Run: "x"}
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), approveAlias)

	r, cmd := send(t, r, runes("A"))
	r = run(t, r, cmd)

	if len(*ran) != 0 {
		t.Errorf("the custom command shadowed a built-in key: %+v", *ran)
	}
	top, _ := r.top()
	for _, g := range r.keysFor(top).FullHelp() {
		for _, b := range g {
			if b.Help().Desc == "my approve" {
				t.Error("a shadowed command is listed in help")
			}
		}
	}
}

func TestRootSkipsACommandWhileAVoteIsArmed(t *testing.T) {
	ran := stubRunner(t)
	c, _ := fixture()
	c.MyID = "me"
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), reviewCommand())

	r, _ = send(t, r, runes("A")) // arms the approve vote
	r, cmd := send(t, r, ctrlR)
	r = run(t, r, cmd)

	if len(*ran) != 0 {
		t.Errorf("ran while a vote was armed: %+v", *ran)
	}
}

func TestRootDeliversAResultToTheViewItRanOn(t *testing.T) {
	// The user can move on in the two seconds a command is watched. Its
	// result belongs to the pane it ran on: not shown as news about the pane
	// now on top, and there to read on coming back.
	stubRunner(t)
	c, items := fixture()
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), reviewCommand())

	r, cmd := send(t, r, ctrlR)
	r, _ = send(t, r, PushMsg{View: NewItem(c, items[0], nil)})
	r = run(t, r, cmd)

	if text, _ := statusOf(r); strings.Contains(text, "review") {
		t.Errorf("the work item pane shows the pull request command's result: %q", text)
	}
	r, _ = send(t, r, PopMsg{})
	if text, _ := statusOf(r); text != `ran "review"` {
		t.Errorf("back on the pull request, status = %q", text)
	}
}

func TestRootDropsAResultForAViewThatIsGone(t *testing.T) {
	stubRunner(t)
	c, _ := fixture()
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), reviewCommand())

	r, cmd := send(t, r, ctrlR)
	r, _ = send(t, r, PopMsg{})
	r = run(t, r, cmd) // must not panic, and has nowhere to go

	if len(r.stack) != 0 {
		t.Errorf("stack = %v", r.stack)
	}
}

func TestRootListsCommandsInHelpAndRunsThemFromThePalette(t *testing.T) {
	ran := stubRunner(t)
	c, _ := fixture()
	r := rootOn(t, NewPullRequestDetail(c, subjectPR(), nil), reviewCommand())

	r, _ = send(t, r, runes("?"))
	if !strings.Contains(r.View(), "review") {
		t.Errorf("the help panel does not list the command:\n%s", r.View())
	}
	r, _ = send(t, r, runes("?"))

	r, cmd := search(t, r, "review")
	r = run(t, r, cmd)
	if len(*ran) != 1 {
		t.Errorf("running the palette entry ran %d commands", len(*ran))
	}
}

func TestCheckCommands(t *testing.T) {
	cmd := func(k string) []config.Command {
		return []config.Command{{On: config.KindBuild, Key: k, Name: "n", Run: "r"}}
	}
	for _, tc := range []struct {
		key, palette, want string
	}{
		{"ctrl+r", "", ""},
		{"K", "ctrl+k", ""},
		{"ctrl+banana", "", "not a key"},
		{"q", "", "needs for itself"},
		{"ctrl+c", "", "needs for itself"},
		{"ctrl+p", "", "opens the command palette"},
		{"ctrl+k", "ctrl+k", "opens the command palette"},
	} {
		err := CheckCommands(cmd(tc.key), tc.palette)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: got %v", tc.key, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: got %v, want %q", tc.key, err, tc.want)
		}
	}
}

func TestCheckCommandsRefusesListMovementKeys(t *testing.T) {
	// A list moves its cursor on keys no view lists in its help: l pages
	// right, b pages back. Root sees a key before the view does, so a command
	// on one would quietly take paging away from every list.
	for _, k := range []string{"l", "h", "b", "u", "f", "pgup", "home", "G"} {
		err := CheckCommands([]config.Command{{On: config.KindBuild, Key: k, Name: "n", Run: "r"}}, "")
		if err == nil || !strings.Contains(err.Error(), "moves the cursor") {
			t.Errorf("%s: got %v", k, err)
		}
	}
}
```

Before running, confirm two helpers exist as named: `runes` (in `workitems_test.go`) and `search` (in `rootpalette_test.go`). Confirm `azdo.Client` has a `MyID` field (`grep -n MyID internal/azdo/client.go`) — `armVote` refuses to arm without it.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestRoot.*Command|TestRootSays|TestRootLets|TestRootSkips|TestRootDrops|TestRootIgnores|TestCheckCommands'`
Expected: FAIL — `undefined: WithCommands`.

- [ ] **Step 3: Implement** — `internal/ui/rootcommands.go`:

```go
package ui

import (
	"fmt"
	"maps"
	"slices"

	"github.com/JacobAtchley/boardwalk/internal/config"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// WithCommands binds the user's custom commands. Check them with
// CheckCommands first.
func WithCommands(cmds []config.Command) RootOption {
	return func(r *Root) { r.commands = cmds }
}

// listMovementKeys are the keys a list moves its cursor with. No view lists
// them in its help — they belong to bubbles/list — so the help-based
// collision check cannot see them, and Root sees a key before the view does.
func listMovementKeys() []string {
	km := list.DefaultKeyMap()
	var keys []string
	for _, b := range []key.Binding{km.CursorUp, km.CursorDown, km.PrevPage, km.NextPage, km.GoToStart, km.GoToEnd, km.Filter} {
		keys = append(keys, b.Keys()...)
	}
	return keys
}

// CheckCommands reports a command whose key cannot work: one that is not a
// key, one Root needs, one a list moves with, or the palette's.
func CheckCommands(cmds []config.Command, paletteKey string) error {
	if paletteKey == "" {
		paletteKey = DefaultPaletteKey
	}
	moves := listMovementKeys()
	for i, c := range cmds {
		switch {
		case !validKey(c.Key):
			return fmt.Errorf("commands[%d] key %q is not a key boardwalk recognises — try \"ctrl+r\"", i, c.Key)
		case slices.Contains(reservedKeys, c.Key):
			return fmt.Errorf("commands[%d] key %q is one boardwalk needs for itself", i, c.Key)
		case slices.Contains(moves, c.Key):
			return fmt.Errorf("commands[%d] key %q moves the cursor in lists", i, c.Key)
		case c.Key == paletteKey:
			return fmt.Errorf("commands[%d] key %q opens the command palette", i, c.Key)
		}
	}
	return nil
}

func validKey(k string) bool { _, ok := keyMsgFor(k); return ok }

// builtinKeys is every key the view on top answers to on its own: what its
// help lists, plus what Root and the shared actions claim on every view.
func builtinKeys(top View) map[string]bool {
	keys := map[string]bool{}
	add := func(bs ...key.Binding) {
		for _, b := range bs {
			for _, k := range b.Keys() {
				keys[k] = true
			}
		}
	}
	for _, g := range top.Keys().FullHelp() {
		add(g...)
	}
	add(sharedBindings()...)
	add(navBindings()...)
	add(keyRefresh)
	return keys
}

// activeCommands are the custom commands that run on the view on top: bound
// for its kind, on a key it does not use itself.
func (r *Root) activeCommands(top View) []config.Command {
	s, ok := top.(subjecter)
	if !ok || len(r.commands) == 0 {
		return nil
	}
	subj, _ := s.Subject()
	taken := builtinKeys(top)
	var out []config.Command
	for _, c := range r.commands {
		if c.On == subj.kind && !taken[c.Key] {
			out = append(out, c)
		}
	}
	return out
}

// commandKey runs the custom command bound to msg on the view on top, if
// there is one.
func (r *Root) commandKey(top View, msg tea.KeyMsg) (tea.Cmd, bool) {
	i := slices.IndexFunc(r.activeCommands(top), func(c config.Command) bool { return c.Key == msg.String() })
	if i < 0 {
		return nil, false
	}
	c := r.activeCommands(top)[i]

	subj, ok := top.(subjecter).Subject()
	if !ok {
		r.tell(top, StatusMsg{Text: "nothing selected"})
		return nil, true
	}

	env := r.commandEnv(subj)
	r.tell(top, StatusMsg{Text: fmt.Sprintf("running %q…", c.Name)})
	return func() tea.Msg {
		return commandDoneMsg{view: top, status: runShell(c.Name, c.Run, env)}
	}, true
}

// tell delivers a status to v wherever it sits in the stack, and does
// nothing if it is no longer there. It is called synchronously for "running…"
// rather than returned as a command: a command finishing faster than that
// message was delivered would have its result overwritten by it.
func (r *Root) tell(v View, s StatusMsg) {
	if i := slices.Index(r.stack, v); i >= 0 {
		r.stack[i], _ = v.Update(s)
	}
}

// commandEnv is the BOARDWALK_ variables for a subject, in a fixed order so
// a test — or a user reading `env` — sees the same thing every time.
func (r *Root) commandEnv(s subject) []string {
	env := []string{
		"BOARDWALK_KIND=" + s.kind,
		"BOARDWALK_ORG=" + r.client.Org,
		"BOARDWALK_PROJECT=" + r.client.Project,
	}
	for _, k := range slices.Sorted(maps.Keys(s.env)) {
		env = append(env, "BOARDWALK_"+k+"="+s.env[k])
	}
	return env
}

// commandDoneMsg is how a custom command turned out, addressed to the view
// it ran on. A plain StatusMsg would go to whatever is on top when it lands,
// which after two seconds may be somewhere else entirely.
type commandDoneMsg struct {
	view   View
	status StatusMsg
}

// keysFor is the view's bindings with the custom commands active on it
// added, which is what the help line, the "?" panel and the palette read.
func (r *Root) keysFor(top View) help.KeyMap {
	active := r.activeCommands(top)
	if len(active) == 0 {
		return top.Keys()
	}
	bindings := make([]key.Binding, len(active))
	for i, c := range active {
		bindings[i] = key.NewBinding(key.WithKeys(c.Key), key.WithHelp(shownKey(c.Key), c.Name))
	}
	return commandKeys{KeyMap: top.Keys(), commands: bindings}
}

// commandKeys adds a group of custom commands to a view's own bindings.
type commandKeys struct {
	help.KeyMap
	commands []key.Binding
}

func (k commandKeys) FullHelp() [][]key.Binding {
	groups := k.KeyMap.FullHelp()
	out := make([][]key.Binding, len(groups), len(groups)+1)
	copy(out, groups)
	return append(out, k.commands)
}
```

In `internal/ui/rootpalette.go`, extract the help spelling from `paletteBinding` so commands spell keys the same way:

```go
// shownKey spells a key the way the footer does: ^p, not ctrl+p.
func shownKey(k string) string {
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok && len(rest) == 1 {
		return "^" + rest
	}
	return k
}

func paletteBinding(k string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(shownKey(k), "commands"))
}
```

In `paletteEntries`, replace `top.Keys().FullHelp()` with `r.keysFor(top).FullHelp()`.

In `internal/ui/root.go`:

- add to `Root`, after `history`:

```go
	// commands are the user's custom commands — see rootcommands.go.
	commands []config.Command
```

  and the `config` import.

- in `Update`, add before `case tea.KeyMsg:`:

```go
	case commandDoneMsg:
		r.tell(msg.view, msg.status)
		return r, nil
```

- in `key`, after the palette-key block and before `switch msg.String() {` (the esc/q one):

```go
	if cmd, ok := r.commandKey(top, msg); ok {
		return cmd, true
	}
```

  It sits after the `typing(top)` guard, so a prompt, a filter or an armed vote or draft toggle sees the key first.

- in `View`, replace `withPalette(top.Keys(), r.paletteKey)` with `withPalette(r.keysFor(top), r.paletteKey)`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ui/`
Expected: PASS, whole package — the palette and help tests included.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat: run a custom command from its key, the help panel or the palette"
```

---

### Task 6: Wire it up and document it

**Files:**
- Modify: `main.go` (`rootOptions`), `main_test.go`, `README.md`

**Interfaces:**
- Consumes: `Config.ValidateCommands` (Task 1); `ui.CheckCommands`, `ui.WithCommands` (Task 5)

- [ ] **Step 1: Write the failing test** — append to `main_test.go`:

```go
func TestRootOptionsChecksCommands(t *testing.T) {
	t.Setenv("BOARDWALK_CONFIG", t.TempDir()+"/boardwalk.json")
	good := config.Command{On: config.KindPullRequest, Key: "ctrl+r", Name: "review", Run: "review.sh"}

	opts, err := rootOptions(config.Config{Commands: []config.Command{good}})
	if err != nil || len(opts) != 2 {
		t.Errorf("rootOptions = %d options, %v; want the commands and the history", len(opts), err)
	}

	bad := good
	bad.On = "pr"
	if _, err := rootOptions(config.Config{Commands: []config.Command{bad}}); err == nil {
		t.Error("an unknown kind was accepted")
	}

	bad = good
	bad.Key = "q"
	if _, err := rootOptions(config.Config{Commands: []config.Command{bad}}); err == nil {
		t.Error("a reserved key was accepted")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test . -run TestRootOptionsChecksCommands`
Expected: FAIL — 1 option rather than 2.

- [ ] **Step 3: Implement** — in `rootOptions`, before the history block:

```go
	if len(cfg.Commands) > 0 {
		path, _ := config.Path()
		if err := cfg.ValidateCommands(path); err != nil {
			return nil, err
		}
		if err := ui.CheckCommands(cfg.Commands, cfg.PaletteKey); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		opts = append(opts, ui.WithCommands(cfg.Commands))
	}
```

and update its doc comment to say it also checks and binds the custom commands.

In `README.md`, add a row to the config table after `paletteKey`:

```markdown
| `commands` | optional — your own [custom commands](#custom-commands), each a key on a kind of screen |
```

and a section after `### Command palette`:

````markdown
### Custom commands

Bind a key on a kind of screen to a shell command of your own. boardwalk runs
it with `sh -c`, detached — it keeps the screen and reports on the status line
whether the command ran, failed (with the last line it wrote to stderr), or is
still going after two seconds and has been left to run.

```json
"commands": [
  {
    "on": "pullRequest",
    "key": "ctrl+r",
    "name": "review with claude",
    "run": "~/bin/pr-review.sh"
  }
]
```

`on` is the kind of screen: `pullRequest` (the list, the pull request, its
diff and files), `workItem` (the list and the item) or `build` (the list and
the logs). The command appears in the `?` panel and the palette there.

What the screen shows reaches the command as environment variables, never
pasted into the command, so a title full of shell syntax stays a title:

| variable | on | |
|---|---|---|
| `BOARDWALK_KIND` | all | `pullRequest`, `workItem` or `build` |
| `BOARDWALK_ORG`, `BOARDWALK_PROJECT` | all | from the config |
| `BOARDWALK_ID`, `BOARDWALK_TITLE`, `BOARDWALK_URL` | all | the selected thing |
| `BOARDWALK_REPO`, `BOARDWALK_SOURCE_BRANCH`, `BOARDWALK_TARGET_BRANCH`, `BOARDWALK_AUTHOR`, `BOARDWALK_IS_DRAFT` | pullRequest | branches without `refs/heads/` |
| `BOARDWALK_TYPE`, `BOARDWALK_STATE` | workItem | |
| `BOARDWALK_NUMBER`, `BOARDWALK_PIPELINE`, `BOARDWALK_BRANCH`, `BOARDWALK_RESULT` | build | |

The command also inherits boardwalk's own environment, so a terminal
multiplexer's variables pass straight through. For example, a script that
opens a [herdr](https://herdr.dev) tab beside boardwalk and starts a Claude
Code review there:

```sh
#!/bin/sh
# ~/bin/pr-review.sh — adjust to your herdr version and review skill.
herdr tab create --label "review !$BOARDWALK_ID" --focus
herdr pane send-text "claude '/review-pr $BOARDWALK_ID'"
herdr pane send-keys enter
```

A key is one key, spelled as for `paletteKey`. boardwalk refuses at startup a
key it needs itself, one lists move with (`j`, `k`, `l`, `h`, `b`, `u`, `f`,
`g`, `G`, `/`, the arrows, page keys), or the palette key. A key a screen
already uses keeps doing what it did there, and the command is left out of
that screen's help.
````

Before committing, run `herdr tab create --help` and `herdr pane send-text --help` and correct the sample script's flags to what this herdr accepts — the README must not ship a script that does not run. If `send-text` needs a pane id, read it from `herdr tab create`'s output (check whether it prints JSON).

- [ ] **Step 4: Run the whole suite and a build for both platforms**

Run: `go test ./... && go vet ./... && GOOS=windows go build ./...`
Expected: PASS, clean, builds.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go README.md
git commit -m "feat: bind custom commands from the config"
```

- [ ] **Step 6: Try it for real**

Write `/tmp/bw-env.sh` containing `env | grep BOARDWALK_ > /tmp/bw-env.out`, bind it to `ctrl+r` on `pullRequest` in a scratch config (`BOARDWALK_CONFIG=...`), run `go run . prs`, press `ctrl+r` on a row, and confirm `/tmp/bw-env.out` holds the variables and the status line read `ran "…"`. Remove the scratch config afterwards.
