package ui

import (
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDraftVerbReadsFromWhereThePullRequestIsNow(t *testing.T) {
	// The key does two opposite things depending on the pull request it is
	// pressed on, so every place that names it — the status line, the help
	// panel — asks here rather than spelling out its own wording.
	if got := draftVerb(true); got != "publish" {
		t.Errorf("draftVerb(draft) = %q, want publish", got)
	}
	if got := draftVerb(false); got != "mark draft" {
		t.Errorf("draftVerb(published) = %q, want mark draft", got)
	}
	// draftDone reads the state the pull request has landed in, which is the
	// opposite of the one draftVerb was asked about.
	if got := draftDone(true); got != "marked as a draft" {
		t.Errorf("draftDone(now a draft) = %q, want marked as a draft", got)
	}
	if got := draftDone(false); got != "published" {
		t.Errorf("draftDone(now published) = %q, want published", got)
	}
	// draftVerb is a phrase, not a stem: suffixing "ing" to it reads
	// "mark drafting…" in one of the two directions, which is why the
	// in-flight wording is its own function rather than built from the verb.
	if got := draftDoing(true); got != "marking as a draft…" {
		t.Errorf("draftDoing(now a draft) = %q, want marking as a draft…", got)
	}
	if got := draftDoing(false); got != "publishing…" {
		t.Errorf("draftDoing(now published) = %q, want publishing…", got)
	}
}

func TestDraftInFlightStatusReadsAsEnglishInBothDirections(t *testing.T) {
	for _, tc := range []struct {
		name string
		pr   azdo.PullRequest
		want string
	}{
		{"published pull request", azdo.PullRequest{ID: 512, RepoID: "r1"}, "marking as a draft…"},
		{"draft", azdo.PullRequest{ID: 512, RepoID: "r1", IsDraft: true}, "publishing…"},
	} {
		m := newPRDetail(t, nil)
		m.pr = tc.pr
		r := drive(t, m, 100, 60)
		r.send(runes("P"))
		r.send(runes("P"))

		if status, _ := m.Status(); !strings.Contains(status, tc.want) {
			t.Errorf("%s: status = %q, want %q", tc.name, status, tc.want)
		}
	}
}

func TestDraftBindingHelpNamesWhatThePressWillDo(t *testing.T) {
	if got := draftBinding(true).Help().Desc; got != "publish" {
		t.Errorf("help on a draft = %q, want publish", got)
	}
	if got := draftBinding(false).Help().Desc; got != "mark draft" {
		t.Errorf("help on a published pull request = %q, want mark draft", got)
	}
}

// --- the detail view ---

func TestDraftKeyArmsAndRequiresConfirmation(t *testing.T) {
	// Publishing a draft notifies every reviewer on it, which is not something
	// a stray keystroke should be able to do — the same reason a vote arms.
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	if cmd := r.send(runes("P")); cmd != nil {
		t.Error("arming the draft toggle must not start work by itself")
	}
	if m.armedDraft == nil {
		t.Fatal("P did not arm the draft toggle")
	}
	if !m.Prompting() {
		t.Error("Prompting did not report the armed toggle")
	}
	status, isErr := m.Status()
	if isErr || !strings.Contains(status, "press again to mark draft") {
		t.Errorf("status = %q, isErr = %v, want it to say what the second press does", status, isErr)
	}
}

func TestDraftKeyRefusesAPullRequestThatIsNoLongerOpen(t *testing.T) {
	// The detail view is also reached from a work item's or a build's linked
	// pull request, which is routinely one that has already merged. Azure
	// DevOps would refuse the toggle, but it is refusing something boardwalk
	// can already see — the same reason armVote checks for a reviewer id
	// before arming rather than after the round trip.
	for _, tc := range []struct {
		status string
		draft  bool
		want   string
	}{
		{"completed", false, "merged"},
		{"abandoned", false, "abandoned"},
		// An abandoned draft is the case that catches naming the state with
		// prStatusLabel, which answers "draft" first and would have this
		// message give the opposite of the reason.
		{"abandoned", true, "abandoned"},
		{"completed", true, "merged"},
	} {
		m := newPRDetail(t, nil)
		m.pr.Status, m.pr.IsDraft = tc.status, tc.draft
		r := drive(t, m, 100, 60)

		if cmd := r.send(runes("P")); cmd != nil {
			t.Errorf("%s: P started work on a pull request that is not open", tc.status)
		}
		if m.armedDraft != nil {
			t.Errorf("%s: P armed the toggle on a pull request that is not open", tc.status)
		}
		got, isErr := m.Status()
		if !strings.Contains(got, tc.want) {
			t.Errorf("status = %q, want it to say the pull request is %s", got, tc.want)
		}
		if !isErr {
			// armVote reports its own refusal as an error, and this is the
			// same kind of answer: the key did nothing and the user needs to
			// see that rather than read past it.
			t.Errorf("status = %q was not reported as a refusal", got)
		}
	}
}

func TestDraftKeyArmsOnAnActivePullRequestWhicheverWayTheStatusIsSpelled(t *testing.T) {
	// The project-wide listing returns "active"; a single fetch of a pull
	// request that has not been touched can leave it empty.
	for _, status := range []string{"", "active"} {
		m := newPRDetail(t, nil)
		m.pr.Status = status
		r := drive(t, m, 100, 60)
		r.send(runes("P"))

		if m.armedDraft == nil {
			t.Errorf("status %q: P did not arm the toggle on an open pull request", status)
		}
	}
}

func TestDraftKeyIsNotOfferedOnAPullRequestThatIsNoLongerOpen(t *testing.T) {
	m := newPRDetail(t, nil)
	if !helpMentions(m.Keys(), "mark draft") {
		t.Error("the help panel does not offer the toggle on an open pull request")
	}

	m.pr.Status = "completed"
	if helpMentions(m.Keys(), "mark draft") || helpMentions(m.Keys(), "publish") {
		t.Error("the help panel offers a toggle that would be refused")
	}
}

// helpMentions reports whether any of a view's bindings describes itself as
// desc, which is how the help panel would read to a user.
func helpMentions(k help.KeyMap, desc string) bool {
	for _, group := range k.FullHelp() {
		for _, b := range group {
			if b.Help().Desc == desc {
				return true
			}
		}
	}
	return false
}

func TestDraftKeyAndTheVoteKeysCannotArmEachOther(t *testing.T) {
	// Both are modal and both swallow what they do not recognise. Today that
	// is asserted in a comment; this is the assertion.
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	r.send(runes("A")) // arm a vote
	r.send(runes("P"))
	if m.armedDraft != nil {
		t.Error("P armed the draft toggle underneath an armed vote")
	}
	if m.armedVote == nil {
		t.Error("P cleared the armed vote instead of being swallowed")
	}

	r.send(tea.KeyMsg{Type: tea.KeyEsc})
	r.send(runes("P")) // arm the toggle
	r.send(runes("A"))
	if m.armedVote != nil {
		t.Error("A armed a vote underneath an armed draft toggle")
	}
	if m.armedDraft == nil {
		t.Error("A cleared the armed toggle instead of being swallowed")
	}
}

func TestDraftKeyEscCancelsTheArm(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("P"))

	if cmd := r.send(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Error("cancelling the armed toggle started work anyway")
	}
	if m.armedDraft != nil {
		t.Error("esc did not disarm the toggle")
	}
	if m.Prompting() {
		t.Error("Prompting still reports a prompt after the arm was cancelled")
	}
}

func TestDraftKeySwallowsOtherKeysWhileArmed(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("P"))

	if cmd := r.send(runes("r")); cmd != nil {
		t.Error("an unrelated key produced a command while the toggle was armed")
	}
	if m.armedDraft == nil {
		t.Error("an unrelated key cleared the arm — only esc and a second press should")
	}
}

func TestDraftKeySecondPressConfirmsAndAppliesInPlace(t *testing.T) {
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)
	r.send(runes("P"))

	if cmd := r.send(runes("P")); cmd == nil {
		t.Fatal("the second press did not fire the toggle")
	}
	if m.armedDraft != nil {
		t.Error("the arm is still set after confirming")
	}

	// The command that talks to Azure DevOps is never run in a test; its
	// result is delivered directly, as voteCastMsg is above.
	r.send(draftSetMsg{PR: 512, Draft: true})

	status, isErr := m.Status()
	if isErr {
		t.Errorf("status = %q, reported as an error", status)
	}
	if !strings.Contains(status, "marked as a draft") {
		t.Errorf("status = %q, want it to say what happened", status)
	}
	if !m.pr.IsDraft {
		t.Error("the cached pull request was not flipped, so the header still reads published")
	}
	if !strings.Contains(r.frame(), "draft") {
		t.Errorf("the view does not show the pull request as a draft:\n%s", r.frame())
	}
}

func TestDraftSetFailureLeavesTheCachedStateAlone(t *testing.T) {
	// Azure DevOps refuses when the signed-in user lacks the permission. The
	// view must not show a state the server rejected.
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	r.send(draftSetMsg{PR: 512, Draft: true, Err: errTest})

	status, isErr := m.Status()
	if !isErr || !strings.Contains(status, errTest.Error()) {
		t.Errorf("status = %q, isErr = %v, want the server's refusal", status, isErr)
	}
	if m.pr.IsDraft {
		t.Error("a refused toggle was applied to the cached pull request anyway")
	}
}

func TestDraftSetForAnotherPullRequestIsIgnored(t *testing.T) {
	// A toggle fired from the list can land after its detail view has been
	// pushed, the same race threadsLoadedMsg guards against.
	m := newPRDetail(t, nil)
	r := drive(t, m, 100, 60)

	r.send(draftSetMsg{PR: 999, Draft: true})

	if m.pr.IsDraft {
		t.Error("a message for another pull request was applied to this one")
	}
}

// --- the list view ---

func TestPullRequestsDraftToggleArmsThenFires(t *testing.T) {
	m := newPRs(t)
	r := drive(t, m, 160, 20)

	if cmd := r.send(runes("P")); cmd != nil {
		t.Error("arming from the list must not start work by itself")
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "press again to mark draft") {
		t.Errorf("status = %q, isErr = %v, want the armed toggle named", status, isErr)
	}
	if cmd := r.send(runes("P")); cmd == nil {
		t.Fatal("the second press did not fire the toggle")
	}

	r.send(draftSetMsg{PR: 512, Draft: true})

	if status, isErr := m.Status(); isErr || !strings.Contains(status, "marked as a draft") {
		t.Errorf("status = %q, isErr = %v, want the toggle reported", status, isErr)
	}
	for _, pr := range m.prs {
		if pr.ID == 512 && !pr.IsDraft {
			t.Error("the row's own pull request was not flipped, so the list badge is stale")
		}
	}
}

func TestPullRequestsDraftToggleEscCancels(t *testing.T) {
	m := newPRs(t)
	r := drive(t, m, 160, 20)
	r.send(runes("P"))

	if cmd := r.send(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Error("cancelling the armed toggle started work anyway")
	}
	if m.Prompting() {
		t.Error("Prompting still reports a prompt after the arm was cancelled")
	}
	if cmd := r.send(runes("P")); cmd != nil {
		t.Error("the cancelled arm fired on the next press instead of re-arming")
	}
}

func TestPullRequestsDraftToggleSwallowsTheDraftFilterWhileArmed(t *testing.T) {
	// d cycles the draft filter, which would re-sort the list out from under
	// a toggle armed on one of its rows.
	m := newPRs(t)
	r := drive(t, m, 160, 20)
	r.send(runes("P"))

	r.send(runes("d"))

	if m.drafts != draftsHidden {
		t.Errorf("the draft filter cycled to %v while a toggle was armed", m.drafts)
	}
}

func TestPullRequestsDraftTogglePublishesADraftRow(t *testing.T) {
	// The other direction: the fixture's draft row, reachable once the filter
	// shows drafts, arms to publish rather than to mark.
	m := newPRs(t)
	r := drive(t, m, 160, 20)
	r.send(runes("d")) // drafts only, so the cursor lands on the draft

	r.send(runes("P"))

	if status, _ := m.Status(); !strings.Contains(status, "press again to publish") {
		t.Errorf("status = %q, want publish rather than mark draft", status)
	}
	r.send(runes("P"))
	r.send(draftSetMsg{PR: 511, Draft: false})

	for _, pr := range m.prs {
		if pr.ID == 511 && pr.IsDraft {
			t.Error("the published pull request is still cached as a draft")
		}
	}
	if status, isErr := m.Status(); isErr || !strings.Contains(status, "published") {
		t.Errorf("status = %q, isErr = %v, want the publish reported", status, isErr)
	}
}

func TestPullRequestsDraftToggleSurvivesTheListReSortingUnderIt(t *testing.T) {
	// Thread counts land as they are fetched and rebuild the rows each time.
	// The arm captures the pull request it was made against, so a rebuild
	// between the two presses cannot move the toggle onto a different row.
	m := newPRs(t)
	// Drafts only, so the list is the one draft in the fixture and the cursor
	// is on it.
	updated, _ := m.Update(runes("d"))
	m = updated.(*PullRequests)
	r := drive(t, m, 160, 20)
	r.send(runes("P")) // arm: publish !511

	// !511 is published from somewhere else — a second boardwalk, the web UI —
	// and the row drops out of a drafts-only list, leaving nothing selected.
	r.send(draftSetMsg{PR: 511, Draft: false})
	if _, ok := m.browser.Selected(); ok {
		t.Fatal("the armed row is still selected, so this proves nothing")
	}

	armed := m.armedDraft
	if armed == nil {
		t.Fatal("the row leaving the list disarmed the toggle")
	}
	if armed.prID != 511 {
		t.Errorf("armedDraft = %+v, want it still aimed at !511", armed)
	}
	// The confirming press fires against the pull request the user armed it
	// on, which is the whole reason the arm captures it rather than reading
	// the cursor again when the second press arrives.
	if cmd := r.send(runes("P")); cmd == nil {
		t.Fatal("the confirming press did not fire once the row had left the list")
	}
}

func TestPullRequestsDraftToggleWithNoRowSelected(t *testing.T) {
	// An empty list has nothing under the cursor, so P does nothing at all —
	// no arm, no command. It says nothing either: the view is already showing
	// its own empty state, which explains the absence better than a status
	// line answering a key the user pressed into an empty screen.
	m := NewPullRequests(&azdo.Client{Org: "acme", Project: "Platform"}, nil)
	m.repoOnly = false
	updated, _ := m.Update(prsMsg{PRs: nil})
	m = updated.(*PullRequests)
	r := drive(t, m, 160, 20)

	if cmd := r.send(runes("P")); cmd != nil {
		t.Error("pressing P on an empty list started work")
	}
	if m.Prompting() {
		t.Error("P armed a toggle with no pull request under the cursor")
	}
}
