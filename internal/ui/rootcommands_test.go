package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
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
	text, isErr := top.Status()
	// A pane pushed in a test never gets its fetches answered, so its spinner
	// is still turning in front of the status. It is not what is asserted on.
	return strings.TrimLeftFunc(text, func(r rune) bool { return r == ' ' || (r >= 0x2800 && r <= 0x28ff) }), isErr
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
	pr := subjectPR()
	pr.Reviewers = []azdo.Reviewer{{Name: "Me", Key: c.Me, ID: "me"}} // armVote refuses a non-reviewer
	r := rootOn(t, NewPullRequestDetail(c, pr, nil), reviewCommand())

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

func TestRootRunsACommandOnAFileView(t *testing.T) {
	// The file pane is reached by drilling in from a diff, so Root is the only
	// way to see its Subject at work.
	ran := stubRunner(t)
	c, _ := fixture()
	fv := NewFileView(c, subjectPR(), "internal/retry.go", "package retry\n", nil, nil, filterAll)
	r := rootOn(t, fv, reviewCommand())

	r, cmd := send(t, r, ctrlR)
	r = run(t, r, cmd)

	if len(*ran) != 1 || !slices.Contains((*ran)[0].env, "BOARDWALK_ID=812") {
		t.Errorf("ran = %+v", *ran)
	}
}
