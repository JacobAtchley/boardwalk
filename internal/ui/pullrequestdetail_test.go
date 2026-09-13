package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	tea "github.com/charmbracelet/bubbletea"
)

func prDetailFixture() (*azdo.Client, azdo.PullRequest) {
	c := &azdo.Client{Org: "acme", Project: "Platform", Me: "dev@acme.test"}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	return c, azdo.PullRequest{
		ID: 512, Title: "Retry webhooks", Repo: "platform-api", RepoID: "r1",
		Author: "Other Dev", AuthorKey: "other@acme.test",
		Source: "refs/heads/feature/4021-retry", Target: "refs/heads/main",
		Created:     now.Add(-3 * time.Hour),
		Description: "Adds **backoff** to delivery.",
		Reviewers: []azdo.Reviewer{
			{Name: "Dev Example", Key: "dev@acme.test", ID: "guid-1", Vote: 10},
			{Name: "platform-devs", IsGroup: true},
		},
	}
}

func newPRDetail(t *testing.T, threads []azdo.Thread) *PullRequestDetail {
	t.Helper()
	c, pr := prDetailFixture()
	m := NewPullRequestDetail(c, pr, threads)
	m.now = func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestPullRequestDetailShowsWhatTheListSummaryCannot(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{
		{Status: "active", File: "/internal/azdo/client.go", Comments: []azdo.ThreadComment{
			{Author: "Other Dev", Text: "Why the retry cap?"},
			{Author: "Dev Example", Text: "Three felt right."},
		}},
	})
	view := drive(t, m, 100, 60).frame()

	for _, want := range []string{
		"!512", "Retry webhooks", "platform-api", "Other Dev",
		"feature/4021-retry", "main", "3h ago",
		"Dev Example", "approved", "platform-devs", "(group)",
		"backoff", "Why the retry cap?",
		// The reply is the whole reason this view exists: the list's side pane
		// only ever showed a thread's opening comment.
		"Three felt right.",
		"/internal/azdo/client.go",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the pull request view is missing %q:\n%s", want, view)
		}
	}
}

func TestPullRequestDetailPutsUnresolvedThreadsFirst(t *testing.T) {
	// Settled discussion is history; what is unresolved is why you opened it.
	m := newPRDetail(t, []azdo.Thread{
		{Status: "fixed", Resolved: true, Comments: []azdo.ThreadComment{{Author: "A", Text: "SETTLED"}}},
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "B", Text: "OUTSTANDING"}}},
	})
	view := drive(t, m, 100, 60).frame()

	settled, outstanding := strings.Index(view, "SETTLED"), strings.Index(view, "OUTSTANDING")
	if settled < 0 || outstanding < 0 {
		t.Fatalf("a thread is missing entirely:\n%s", view)
	}
	if outstanding > settled {
		t.Errorf("the resolved thread came first:\n%s", view)
	}
}

func TestPullRequestDetailCountsItsThreads(t *testing.T) {
	m := newPRDetail(t, threadsResolved(3, 2))
	view := drive(t, m, 100, 60).frame()

	if !strings.Contains(view, "3 resolved, 2 unresolved") {
		t.Errorf("the discussion heading does not carry the tally:\n%s", view)
	}
}

func TestPullRequestDetailFetchesWhenTheListHadNothingCached(t *testing.T) {
	m := newPRDetail(t, nil)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("the view did not ask for the discussion")
	}
	if !m.work.busy() {
		t.Error("the spinner is not running while the fetch is in flight")
	}
}

func TestPullRequestDetailUsesThreadsTheListAlreadyHad(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{{Status: "active", Comments: []azdo.ThreadComment{{Author: "A", Text: "hi"}}}})
	m.Init()

	if !m.loaded {
		t.Error("the view did not treat the threads it was handed as loaded")
	}
	if !strings.Contains(drive(t, m, 100, 60).frame(), "hi") {
		t.Error("the handed-over discussion did not render")
	}
}

func TestPullRequestDetailFetchesItsLinksEvenWithThreadsInHand(t *testing.T) {
	// The list caches threads and knows nothing about linked work items.
	m := newPRDetail(t, []azdo.Thread{{Status: "active", Comments: []azdo.ThreadComment{{Author: "A", Text: "hi"}}}})

	if cmd := m.Init(); cmd == nil {
		t.Fatal("a view handed its threads did not fetch its work item links")
	}
}

func TestPullRequestDetailRendersThreadsThatArriveLater(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 512, Threads: []azdo.Thread{
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "Third Dev", Text: "Landed later"}}},
	}})

	if !strings.Contains(r.frame(), "Landed later") {
		t.Errorf("the fetched discussion did not render:\n%s", r.frame())
	}
	// Init issues two fetches — the discussion and the work item links.
	if !m.work.busy() {
		t.Error("stopped spinning while the linked work items were still in flight")
	}
	r.send(linkedItemsMsg{PR: 512})
	if m.work.busy() {
		t.Error("still spinning after both fetches landed")
	}
}

func TestPullRequestDetailSaysSoWhenTheFetchFails(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 512, Err: errTest})

	if !strings.Contains(r.frame(), "could not load the discussion") {
		t.Errorf("the discussion section does not report the failure:\n%s", r.frame())
	}
	if status, isErr := m.Status(); !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
	r.send(linkedItemsMsg{PR: 512})
	if m.work.busy() {
		t.Error("still spinning after both fetches settled")
	}
}

func TestPullRequestDetailIgnoresAnotherPullRequestsThreads(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(threadsLoadedMsg{PR: 511, Threads: []azdo.Thread{
		{Status: "active", Comments: []azdo.ThreadComment{{Author: "Nobody", Text: "stale comment"}}},
	}})

	if strings.Contains(r.frame(), "stale comment") {
		t.Error("a discussion belonging to another pull request was rendered")
	}
}

func TestPullRequestDetailScrollSurvivesARepaint(t *testing.T) {
	var threads []azdo.Thread
	for i := range 30 {
		threads = append(threads, azdo.Thread{Status: "active", Comments: []azdo.ThreadComment{
			{Author: "Dev Example", Text: strings.Repeat("filler ", 8) + string(rune('a'+i%26))},
		}})
	}
	m := newPRDetail(t, threads)
	r := drive(t, m, 100, 12)

	top := r.frame()
	r.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	bottom := r.frame()

	if top == bottom {
		t.Error("G did not move the pager")
	}
	if r.frame() != bottom {
		t.Error("the scroll position was undone by the next repaint")
	}
}

func TestPullRequestDetailEscapeGoesBack(t *testing.T) {
	m := newPRDetail(t, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced no command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("escape produced %T, want a PopMsg", cmd())
	}
}

func TestPullRequestsEnterOpensTheDetail(t *testing.T) {
	m := newPRs(t)
	m.drafts = draftsAll
	m.applyFilters()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "!512") {
		t.Errorf("pushed view = %q, want the selected pull request", push.View.Title())
	}
}

func TestPullRequestsEnterCarriesThreadsItAlreadyFetched(t *testing.T) {
	// Opening a pull request whose counts are already on screen must not go
	// back to the server for the discussion behind them.
	m := newPRs(t)
	updated, _ := m.Update(threadsMsg{PR: 512, Threads: threadsResolved(1, 1)})
	m = updated.(*PullRequests)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push := cmd().(PushMsg)

	detail, ok := push.View.(*PullRequestDetail)
	if !ok {
		t.Fatalf("pushed %T, want a PullRequestDetail", push.View)
	}
	if !detail.loaded {
		t.Error("the detail view did not receive the discussion the list already had")
	}
}

func TestPullRequestDetailShowsItsLinkedWorkItems(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)

	r.send(linkedItemsMsg{PR: 512, Items: []azdo.WorkItem{
		{ID: 4021, Title: "Retry webhook delivery", State: "Active", Assigned: "Dev Example"},
	}})

	view := r.frame()
	for _, want := range []string{"#4021", "Retry webhook delivery", "Active", "Dev Example"} {
		if !strings.Contains(view, want) {
			t.Errorf("the linked work items are missing %q:\n%s", want, view)
		}
	}
}

func TestPullRequestDetailOpensItsLinkedWorkItem(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)
	r.send(linkedItemsMsg{PR: 512, Items: []azdo.WorkItem{{ID: 4021, Title: "Retry webhook delivery"}}})

	cmd := r.send(runes("w"))
	if cmd == nil {
		t.Fatal("w produced no command")
	}
	push, ok := cmd().(PushMsg)
	if !ok {
		t.Fatalf("w produced %T, want a PushMsg", cmd())
	}
	if !strings.Contains(push.View.Title(), "#4021") {
		t.Errorf("pushed %q, want the linked work item", push.View.Title())
	}
}

func TestPullRequestDetailSaysSoWhenThereIsNoWorkItemToOpen(t *testing.T) {
	m := newPRDetail(t, nil)
	m.Init()
	r := drive(t, m, 100, 60)
	r.send(linkedItemsMsg{PR: 512})

	r.send(runes("w"))

	if status, _ := m.Status(); !strings.Contains(status, "no work items are linked") {
		t.Errorf("status = %q, want w to explain that there is nothing to open", status)
	}
}

// threadWithOpener builds a single-comment thread whose opener carries id,
// which is what a reply has to name as its parent.
func threadWithOpener(threadID, openerID int, resolved bool, author, text string) azdo.Thread {
	status := "active"
	if resolved {
		status = "fixed"
	}
	return azdo.Thread{ID: threadID, Status: status, Resolved: resolved,
		Comments: []azdo.ThreadComment{{ID: openerID, Author: author, Text: text}}}
}

func TestReplyKeyOpensAPromptOnTheFirstUnresolvedThread(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{
		threadWithOpener(1, 10, true, "A", "settled"),
		threadWithOpener(2, 20, false, "Other Dev", "Why the retry cap?"),
	})
	r := drive(t, m, 100, 60)

	r.send(runes("c"))

	if m.replyPrompt == nil {
		t.Fatal("c did not open the reply prompt")
	}
	if !m.Prompting() {
		t.Error("Prompting did not report the open prompt")
	}
	// Replying always answers the unresolved thread's opener — the fixed
	// thread's comment is not a valid target even though it comes first in
	// m.threads.
	if m.replyThread != 2 || m.replyParent != 20 {
		t.Errorf("reply target = thread %d, parent %d; want the unresolved thread's opener (2, 20)",
			m.replyThread, m.replyParent)
	}
}

func TestReplyKeyWithNothingUnresolvedSaysSo(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, true, "A", "settled")})
	r := drive(t, m, 100, 60)

	r.send(runes("c"))

	if m.replyPrompt != nil {
		t.Error("c opened a prompt with no unresolved thread to answer")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "no unresolved thread") {
		t.Errorf("status = %q, isErr = %v, want it to explain there is nothing to reply to", status, isErr)
	}
}

func TestReplyPromptEscapeCancels(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "Other Dev", "Why?")})
	r := drive(t, m, 100, 60)
	r.send(runes("c"))

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling the prompt started work anyway")
	}
	if m.replyPrompt != nil {
		t.Error("escape did not close the prompt")
	}
}

func TestReplyPromptSwallowsActionKeys(t *testing.T) {
	// With the prompt open, "o" is a letter rather than the open action.
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "Other Dev", "Why?")})
	r := drive(t, m, 100, 60)
	r.send(runes("c"))

	r.send(runes("o"))
	if !strings.HasSuffix(m.replyPrompt.Value(), "o") {
		t.Errorf("prompt = %q, want the keystroke in it", m.replyPrompt.Value())
	}
}

func TestReplyPromptEnterSendsAndAppendsTheCommentWithoutARefetch(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "Other Dev", "Why the retry cap?")})
	r := drive(t, m, 100, 60)
	r.send(runes("c"))
	r.send(runes("Three felt right."))

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	if m.replyPrompt != nil {
		t.Error("the prompt is still open after sending")
	}

	// The command that actually talks to Azure DevOps is never run in a test
	// — its result is delivered directly, the same way the branch flow's
	// tests drive branchDoneMsg without executing branchCmd.
	r.send(replySentMsg{PR: 512, ThreadID: 1, Comment: azdo.ThreadComment{ID: 30, Author: "Dev Example", Text: "Three felt right."}})

	status, isErr := m.Status()
	if isErr {
		t.Errorf("status = %q, reported as an error", status)
	}
	if !strings.Contains(r.frame(), "Three felt right.") {
		t.Error("the new comment did not appear in the thread — the cache was not updated in place")
	}
}

func TestReplyPromptEnterOnBlankTextClosesWithoutSending(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "Other Dev", "Why?")})
	r := drive(t, m, 100, 60)
	r.send(runes("c"))

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("sending a blank reply started work anyway")
	}
	if m.replyPrompt != nil {
		t.Error("the prompt did not close")
	}
}

func TestReplySentReportsAFailure(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "A", "hi")})
	r := drive(t, m, 100, 60)

	r.send(replySentMsg{PR: 512, ThreadID: 1, Err: errTest})

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestReplySentIgnoresAnotherPullRequests(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "A", "hi")})
	r := drive(t, m, 100, 60)

	r.send(replySentMsg{PR: 511, ThreadID: 1, Comment: azdo.ThreadComment{Text: "stale"}})

	if strings.Contains(r.frame(), "stale") {
		t.Error("a reply belonging to another pull request was applied")
	}
}

func TestResolveKeyMarksTheThreadFixedWithoutARefetch(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "Other Dev", "hi")})
	r := drive(t, m, 100, 60)

	cmd := r.send(runes("R"))
	if cmd == nil {
		t.Fatal("R produced no command")
	}

	r.send(threadResolvedMsg{PR: 512, ThreadID: 1})

	status, isErr := m.Status()
	if isErr {
		t.Errorf("status = %q, reported as an error", status)
	}
	if !strings.Contains(r.frame(), "1 resolved, 0 unresolved") {
		t.Errorf("the tally did not update without a refetch:\n%s", r.frame())
	}
	if !strings.Contains(r.frame(), "✓ resolved") {
		t.Error("the thread does not render as resolved")
	}
}

func TestResolveKeyWithNothingUnresolvedSaysSo(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, true, "A", "settled")})
	r := drive(t, m, 100, 60)

	cmd := r.send(runes("R"))
	if cmd != nil {
		t.Error("R started work with nothing unresolved to resolve")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "no unresolved thread") {
		t.Errorf("status = %q, isErr = %v, want it to explain there is nothing to resolve", status, isErr)
	}
}

func TestThreadResolvedReportsAFailure(t *testing.T) {
	m := newPRDetail(t, []azdo.Thread{threadWithOpener(1, 10, false, "A", "hi")})
	r := drive(t, m, 100, 60)

	r.send(threadResolvedMsg{PR: 512, ThreadID: 1, Err: errTest})

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestVoteKeyArmsAndRequiresConfirmation(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	if cmd := r.send(runes("X")); cmd != nil {
		t.Error("arming a vote must not start work by itself")
	}
	if m.armedVote == nil {
		t.Fatal("X did not arm the reject vote")
	}
	if !m.Prompting() {
		t.Error("Prompting did not report the armed vote")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "press again to reject") {
		t.Errorf("status = %q, isErr = %v, want it to say the vote is armed", status, isErr)
	}
}

func TestVoteKeyEscCancelsTheArm(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("X"))

	cmd := r.send(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Error("cancelling an armed vote started work anyway")
	}
	if m.armedVote != nil {
		t.Error("esc did not disarm the vote")
	}
	if m.Prompting() {
		t.Error("Prompting still reports a prompt after the arm was cancelled")
	}
}

func TestVoteKeySecondPressConfirmsAndAppliesInPlaceWithoutARefetch(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("X"))

	cmd := r.send(runes("X"))
	if cmd == nil {
		t.Fatal("the second press did not fire the vote")
	}
	if m.armedVote != nil {
		t.Error("the arm is still set after confirming")
	}

	// The command that actually talks to Azure DevOps is never run in a test —
	// its result is delivered directly, the same way replySentMsg is driven
	// in TestReplyPromptEnterSendsAndAppendsTheCommentWithoutARefetch above.
	r.send(voteCastMsg{PR: 512, ReviewerID: "guid-1", Vote: azdo.VoteRejected})

	status, isErr := m.Status()
	if isErr {
		t.Errorf("status = %q, reported as an error", status)
	}
	if !strings.Contains(r.frame(), "rejected") {
		t.Errorf("the reviewer's vote did not update in the cache:\n%s", r.frame())
	}
}

func TestVoteKeyPressingADifferentVoteReArmsInsteadOfConfirming(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("X")) // arm reject

	cmd := r.send(runes("A")) // change of mind, before confirming
	if cmd != nil {
		t.Error("switching the armed vote must not fire the old one")
	}
	if m.armedVote == nil || m.armedVote.vote != azdo.VoteApproved {
		t.Fatalf("armedVote = %+v, want it switched to approve", m.armedVote)
	}
	if status, _ := m.Status(); !strings.Contains(status, "press again to approve") {
		t.Errorf("status = %q, want it to name the newly armed vote", status)
	}
}

func TestVoteKeySwallowsOtherKeysWhileArmed(t *testing.T) {
	// The arm is a modal state: an unrelated binding must not fire, and must
	// not clear the arm either — only esc or the confirming key should.
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("X"))

	cmd := r.send(runes("r"))
	if cmd != nil {
		t.Error("an unrelated key produced a command while a vote was armed")
	}
	if m.armedVote == nil {
		t.Error("an unrelated key disarmed the vote")
	}
}

func TestVoteWithoutADirectReviewerIDReportsClearly(t *testing.T) {
	// Covered only by the group, not named individually: nothing in the
	// payload gives a vote something to address.
	c, pr := prDetailFixture()
	pr.Reviewers = []azdo.Reviewer{{Name: "platform-devs", IsGroup: true}}
	m := NewPullRequestDetail(c, pr, nil)
	r := drive(t, m, 100, 60)

	cmd := r.send(runes("A"))

	if cmd != nil {
		t.Error("arming a vote with no reviewer id to address it started work")
	}
	if m.armedVote != nil {
		t.Error("a vote armed with nothing to address it")
	}
	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, "not a direct reviewer") {
		t.Errorf("status = %q, isErr = %v, want it to explain there is no id to vote with", status, isErr)
	}
}

func TestVoteCastReportsAFailure(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	r.send(voteCastMsg{PR: 512, ReviewerID: "guid-1", Err: errTest})

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v", status, isErr)
	}
}

func TestVoteCastIgnoresAnotherPullRequests(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	r.send(voteCastMsg{PR: 511, ReviewerID: "guid-1", Vote: azdo.VoteRejected})

	if strings.Contains(r.frame(), "rejected") {
		t.Error("a vote belonging to another pull request was applied")
	}
}
