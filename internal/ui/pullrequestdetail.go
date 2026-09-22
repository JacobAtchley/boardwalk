package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// threadsLoadedMsg carries a fetched discussion. It names its pull request
// because Root broadcasts data to every view in the stack, and the list this
// was opened from is still underneath fetching counts of its own.
type threadsLoadedMsg struct {
	PR      int
	Threads []azdo.Thread
	Err     error
}

// linkedItemsMsg carries the work items a pull request is linked to.
type linkedItemsMsg struct {
	PR    int
	Items []azdo.WorkItem
	Err   error
}

// replySentMsg carries the outcome of answering a thread. PR names which pull
// request it belongs to for the same reason threadsLoadedMsg does: Root
// broadcasts data to every view in the stack, and two of these views can be
// open at once (a pull request whose linked work item links back to another).
type replySentMsg struct {
	PR       int
	ThreadID int
	Comment  azdo.ThreadComment
	Err      error
}

// threadResolvedMsg carries the outcome of resolving a thread.
type threadResolvedMsg struct {
	PR       int
	ThreadID int
	Err      error
}

// gateBuildsMsg carries the runs this pull request's build validation
// policies produced. It names its pull request for the same reason
// threadsLoadedMsg does: Root broadcasts data to every view in the stack.
type gateBuildsMsg struct {
	PR    int
	Gates []azdo.GateBuild
	Err   error
}

// voteCastMsg carries the outcome of casting a reviewer vote.
type voteCastMsg struct {
	PR         int
	ReviewerID string
	Vote       int
	Err        error
}

// pendingVote is non-nil while a vote is armed, waiting for a second press of
// the same key to confirm. Approving or rejecting someone else's work is not
// something a stray keystroke should be able to do, so the key that requests
// it only arms the vote; firing it takes pressing that key again.
type pendingVote struct {
	reviewerID string
	vote       int
	label      string // status-line word for the vote, e.g. "approve"
}

// voteBindings pairs each vote key with the vote it casts and the word the
// status line uses for it — the one place that mapping lives, so arming and
// confirming can never disagree about what a key does.
var voteBindings = []struct {
	binding key.Binding
	vote    int
	label   string
}{
	{keyApprove, azdo.VoteApproved, "approve"},
	{keyWait, azdo.VoteWaitingForAuthor, "wait for author"},
	{keyReject, azdo.VoteRejected, "reject"},
}

// matchVoteBinding reports which vote, if any, msg requests.
func matchVoteBinding(msg tea.KeyMsg) (vote int, label string, ok bool) {
	for _, vb := range voteBindings {
		if key.Matches(msg, vb.binding) {
			return vb.vote, vb.label, true
		}
	}
	return 0, "", false
}

// PullRequestDetail is one pull request in full: the description, every
// reviewer's vote, and every discussion rather than the opening line of the
// unresolved ones.
//
// The name is long on purpose. PullRequests is the list, and a type differing
// from it by one letter would be a trap for the next reader.
type PullRequestDetail struct {
	client *azdo.Client
	pr     azdo.PullRequest

	viewport viewport.Model
	threads  []azdo.Thread

	// filter is which half of the discussion is on screen. A review with a
	// lot of feedback on it is why: see threadFilter.
	filter threadFilter

	loaded bool
	failed bool

	// linked are the work items this pull request closes or contributes to.
	linked       []azdo.WorkItem
	linkedLoaded bool
	linkedErr    bool

	// renderedAt is the width the pane was last laid out for. Glamour hard
	// wraps, so a resize means rendering again rather than reflowing.
	renderedAt int

	status string
	work   work
	now    func() time.Time

	// replyPrompt is non-nil while a reply to a thread is being typed.
	replyPrompt *textinput.Model
	// replyThread and replyParent name the thread and the comment being
	// answered. They are captured when the prompt opens rather than
	// recomputed from replyPrompt.Value() alone, because selectedThread
	// cannot be asked again once the reply's network round trip returns —
	// by then the discussion may have changed under it.
	replyThread, replyParent int

	// armedVote is non-nil while a vote is waiting on its confirming
	// keystroke. It and replyPrompt are never both set: the reply prompt
	// swallows every key before this view's own bindings are even
	// considered, so there is nothing left to arm a vote while it is open.
	armedVote *pendingVote

	// gatesPending is true while a build gate lookup is in flight, so a
	// second press does not start another, and a late answer knows whether
	// it is still the one being waited on.
	gatesPending bool

	// hidden is true from the moment this view pushes something on top of
	// itself until it is back on top. Root broadcasts gateBuildsMsg to the
	// whole stack, so a lookup that resolves while the diff pane is showing
	// must not push the build list over it. A KeyMsg is proof of being back
	// on top — Root routes keys to the top view alone — so the flag clears
	// the moment one arrives. Same arrangement as the builds view; see its
	// hidden field.
	hidden bool

	// armedDraft is the same arrangement for the draft toggle, and for the
	// same reason — see draft.go. No two of the three modal states can be
	// open at once: each of them swallows every key that is not its own
	// confirm or cancel, so none can be reached while another is up.
	armedDraft *armedDraft
}

// NewPullRequestDetail builds the view. threads is whatever the list had
// cached; Init fetches when it has none.
func NewPullRequestDetail(c *azdo.Client, pr azdo.PullRequest, threads []azdo.Thread) *PullRequestDetail {
	return &PullRequestDetail{
		client:   c,
		pr:       pr,
		viewport: viewport.New(0, 0),
		threads:  threads,
		loaded:   threads != nil,
		work:     newWork(),
		now:      time.Now,
	}
}

// Init fetches what it does not already have. The two halves are independent:
// the list hands over the discussion it cached but knows nothing about the
// linked work items, so returning early on the discussion alone would leave the
// links permanently unfetched.
func (m *PullRequestDetail) Init() tea.Cmd {
	var cmds []tea.Cmd
	if !m.loaded {
		cmds = append(cmds, m.fetchThreads())
	}
	if !m.linkedLoaded {
		cmds = append(cmds, m.fetchLinked())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(tea.Batch(cmds...), m.work.begin(len(cmds)))
}

func (m *PullRequestDetail) fetchThreads() tea.Cmd {
	client, repo, id := m.client, m.pr.RepoID, m.pr.ID
	return func() tea.Msg {
		threads, err := client.Threads(repo, id)
		if err != nil {
			return threadsLoadedMsg{PR: id, Err: fmt.Errorf("could not load the discussion for !%d: %w", id, err)}
		}
		return threadsLoadedMsg{PR: id, Threads: threads}
	}
}

// fetchLinked resolves the work items this pull request is attached to. The
// endpoint returns bare ids, so the titles come from one batch fetch.
// fetchGateBuilds reads the runs this pull request's build validation
// policies produced. It is asked for on the keypress rather than fetched on
// entry, the same reasoning the builds view gives for its pull request link:
// most visits to this pane never ask, and the evaluations are a call of their
// own.
func (m *PullRequestDetail) fetchGateBuilds() tea.Cmd {
	client, pr := m.client, m.pr
	return func() tea.Msg {
		gates, err := client.PullRequestGateBuilds(pr)
		if err != nil {
			return gateBuildsMsg{PR: pr.ID, Err: fmt.Errorf("could not load the build gates for !%d: %w", pr.ID, err)}
		}
		return gateBuildsMsg{PR: pr.ID, Gates: gates}
	}
}

// gateBuildIDs is the runs, in the order the policies came back in.
func gateBuildIDs(gates []azdo.GateBuild) []int {
	ids := make([]int, 0, len(gates))
	for _, g := range gates {
		ids = append(ids, g.BuildID)
	}
	return ids
}

// gateSummary is the status line the build list is opened with. The policy
// verdict is worth carrying over because it is not the build's: a rejected
// gate whose run succeeded means the policy was evaluated against an earlier
// iteration, and the build list alone would show nothing but a green run.
func gateSummary(gates []azdo.GateBuild) string {
	if len(gates) == 1 {
		return fmt.Sprintf("%s: %s", gates[0].Policy, gates[0].Status)
	}
	return fmt.Sprintf("%d build gates", len(gates))
}

func (m *PullRequestDetail) fetchLinked() tea.Cmd {
	client, repo, id := m.client, m.pr.RepoID, m.pr.ID
	return func() tea.Msg {
		ids, err := client.PullRequestWorkItemIDs(repo, id)
		if err != nil {
			return linkedItemsMsg{PR: id, Err: fmt.Errorf("could not load the work items for !%d: %w", id, err)}
		}
		items, err := client.WorkItemsByID(ids)
		if err != nil {
			return linkedItemsMsg{PR: id, Err: fmt.Errorf("could not load the work items for !%d: %w", id, err)}
		}
		return linkedItemsMsg{PR: id, Items: items}
	}
}

// selectedThread is the thread c and R act on. The discussion has no cursor
// of its own — renderThreads already sorts unresolved threads to the top, so
// the first one found is the one sitting at the top of the section, the same
// "first" precedent renderLinked uses for the linked work item w opens.
//
// A thread the filter is hiding is not a candidate: the marker naming what c
// and R will act on is drawn beside a thread, so a key that reached past the
// filter would act on something with nothing on screen pointing at it.
func (m *PullRequestDetail) selectedThread() (azdo.Thread, bool) {
	for _, t := range m.threads {
		if !t.Resolved && m.filter.keep(t) {
			return t, true
		}
	}
	return azdo.Thread{}, false
}

// replyCmd posts a reply to threadID, answering parentCommentID.
func (m *PullRequestDetail) replyCmd(threadID, parentCommentID int, text string) tea.Cmd {
	client, repo, pr := m.client, m.pr.RepoID, m.pr.ID
	return func() tea.Msg {
		comment, err := client.ReplyToThread(repo, pr, threadID, parentCommentID, text)
		if err != nil {
			return replySentMsg{PR: pr, ThreadID: threadID, Err: fmt.Errorf("could not post the reply: %w", err)}
		}
		return replySentMsg{PR: pr, ThreadID: threadID, Comment: comment}
	}
}

// resolveCmd marks threadID fixed.
func (m *PullRequestDetail) resolveCmd(threadID int) tea.Cmd {
	client, repo, pr := m.client, m.pr.RepoID, m.pr.ID
	return func() tea.Msg {
		if err := client.SetThreadStatus(repo, pr, threadID, "fixed"); err != nil {
			return threadResolvedMsg{PR: pr, ThreadID: threadID, Err: fmt.Errorf("could not resolve the thread: %w", err)}
		}
		return threadResolvedMsg{PR: pr, ThreadID: threadID}
	}
}

// voteCmd casts reviewerID's vote on this pull request.
func (m *PullRequestDetail) voteCmd(reviewerID string, vote int) tea.Cmd {
	client, repo, pr := m.client, m.pr.RepoID, m.pr.ID
	return func() tea.Msg {
		if err := client.SetVote(repo, pr, reviewerID, vote); err != nil {
			return voteCastMsg{PR: pr, ReviewerID: reviewerID, Vote: vote, Err: fmt.Errorf("could not cast the vote: %w", err)}
		}
		return voteCastMsg{PR: pr, ReviewerID: reviewerID, Vote: vote}
	}
}

// armVote starts (or restarts) the confirm step for vote, naming what the
// second press will do on the status line.
func (m *PullRequestDetail) armVote(vote int, label string) {
	id, ok := m.client.MyReviewerID(m.pr)
	if !ok {
		// Two different refusals wear the same "no id" face, and telling them
		// apart is the difference between a fact about this pull request and
		// a fact about this session. Arming anyway would only fail later at
		// the network call, which hides both reasons.
		m.armedVote = nil
		m.status, m.failed = noVoteReason(m.client.MyID), true
		return
	}
	m.armedVote = &pendingVote{reviewerID: id, vote: vote, label: label}
	m.status, m.failed = fmt.Sprintf("press again to %s — esc cancels", label), false
}

// noVoteReason says why a vote cannot be cast. An empty identity means
// connectionData failed at startup, which is worth saying plainly: the vote
// may well be this person's to cast, and a stale `az login` is something they
// can fix. Otherwise they simply are not on the hook for this pull request.
func noVoteReason(myID string) string {
	if myID == "" {
		return "boardwalk could not work out your Azure DevOps identity at startup — restart it with a current `az login` to vote"
	}
	return "you are not a reviewer on this pull request — neither directly nor through a group in reviewGroups"
}

// handleArmedVote resolves a keypress while a vote is armed. esc cancels, the
// same vote's key confirms and fires it, and a different vote's key re-arms
// to that vote instead of forcing an esc round trip first. Anything else is
// swallowed — the arm is a modal state, the same way the reply prompt owns
// every key while it is open.
func (m *PullRequestDetail) handleArmedVote(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, keyBack) {
		m.armedVote = nil
		m.status, m.failed = "", false
		return nil
	}
	vote, label, ok := matchVoteBinding(msg)
	if !ok {
		return nil
	}
	if vote != m.armedVote.vote {
		m.armVote(vote, label)
		return nil
	}
	id := m.armedVote.reviewerID
	m.armedVote = nil
	m.status, m.failed = "casting vote: "+label+"…", false
	return m.voteCmd(id, vote)
}

// handleArmedDraft resolves a keypress while the draft toggle is armed: esc
// cancels, a second P fires it, and anything else is swallowed.
func (m *PullRequestDetail) handleArmedDraft(msg tea.KeyMsg) tea.Cmd {
	confirm, cancel := resolveDraftKey(msg)
	switch {
	case cancel:
		m.armedDraft = nil
		m.status, m.failed = "", false
	case confirm:
		armed := m.armedDraft
		m.armedDraft = nil
		m.status, m.failed = draftDoing(armed.draft), false
		return setDraftCmd(m.client, armed.repoID, armed.prID, armed.draft)
	}
	return nil
}

// updateReviewerVote rewrites the cached reviewer's vote in place, the same
// cache-over-refetch approach updateThread takes below: the vote this view
// just sent is already known without asking the server again.
func (m *PullRequestDetail) updateReviewerVote(reviewerID string, vote int) {
	for i, r := range m.pr.Reviewers {
		if r.ID == reviewerID {
			m.pr.Reviewers[i].Vote = vote
			return
		}
	}

	// Voting on a group's behalf: there was no entry for me, and Azure DevOps
	// has just made one. Adding it here keeps the reviewers list honest about
	// what was cast, the same way rewriting an existing entry does.
	m.pr.Reviewers = append(m.pr.Reviewers, azdo.Reviewer{
		Name: m.client.Me,
		Key:  m.client.Me,
		ID:   reviewerID,
		Vote: vote,
	})
}

// updateThread rewrites the cached thread with id in place and reports
// whether one was found. A reply or a resolve is rewritten here rather than
// by refetching the discussion: refetching would also throw away the scroll
// position renderThreads' viewport is holding, for one round trip whose
// result — the single comment or status this view just sent — is already
// known without asking the server again.
func (m *PullRequestDetail) updateThread(id int, edit func(*azdo.Thread)) bool {
	for i, t := range m.threads {
		if t.ID == id {
			edit(&t)
			m.threads[i] = t
			return true
		}
	}
	return false
}

func (m *PullRequestDetail) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case threadsLoadedMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		m.work.done()
		m.loaded = true
		if msg.Err != nil {
			m.failed, m.status = true, msg.Err.Error()
		} else {
			m.threads, m.failed = msg.Threads, false
		}
		m.invalidate()
		return m, nil

	case linkedItemsMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		m.work.done()
		m.linkedLoaded = true
		if msg.Err != nil {
			m.linkedErr, m.status = true, msg.Err.Error()
		} else {
			m.linked = msg.Items
		}
		m.invalidate()
		return m, nil

	case replySentMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.updateThread(msg.ThreadID, func(t *azdo.Thread) {
			t.Comments = append(t.Comments, msg.Comment)
		})
		m.status, m.failed = "reply sent", false
		m.invalidate()
		return m, nil

	case threadResolvedMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.updateThread(msg.ThreadID, func(t *azdo.Thread) {
			t.Status, t.Resolved = "fixed", true
		})
		m.status, m.failed = "thread resolved", false
		m.invalidate()
		return m, nil

	case voteCastMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.updateReviewerVote(msg.ReviewerID, msg.Vote)
		m.status, m.failed = "vote cast: "+(azdo.Reviewer{Vote: msg.Vote}).VoteLabel(), false
		m.invalidate()
		return m, nil

	case draftSetMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.pr.IsDraft = msg.Draft
		m.status, m.failed = fmt.Sprintf("!%d %s", m.pr.ID, draftDone(msg.Draft)), false
		m.invalidate()
		return m, nil

	case gateBuildsMsg:
		m.work.done()
		if msg.PR != m.pr.ID || !m.gatesPending {
			// Either an answer for the other pull request this stack can have
			// open, or one this view is no longer waiting on.
			return m, nil
		}
		m.gatesPending = false
		if msg.Err != nil {
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		if len(msg.Gates) == 0 {
			m.status, m.failed = fmt.Sprintf("no build policy has run for !%d", m.pr.ID), false
			return m, nil
		}
		if m.hidden {
			// The lookup was still in flight when the user opened something
			// else — see the hidden field's doc.
			return m, nil
		}
		m.status, m.failed = gateSummary(msg.Gates), false
		builds := NewBuildsForPullRequest(m.client, m.pr.ID, gateBuildIDs(msg.Gates))
		return m, func() tea.Msg { return PushMsg{View: builds} }

	case StatusMsg:
		m.status = msg.Text
		return m, nil

	case tea.KeyMsg:
		// A key is proof this view is back on top: Root routes keys to the
		// top view alone.
		m.hidden = false
		// The reply prompt owns every key while it is open, including esc and
		// the letters that are otherwise actions — typing "o" into it must add
		// the letter, not open a browser. Mirrors the branch prompt in
		// workitems.go.
		if m.replyPrompt != nil {
			switch msg.Type {
			case tea.KeyEsc:
				m.replyPrompt = nil
				m.status, m.failed = "", false
				return m, nil
			case tea.KeyEnter:
				text := strings.TrimSpace(m.replyPrompt.Value())
				m.replyPrompt = nil
				if text == "" {
					return m, nil
				}
				m.status, m.failed = "sending reply…", false
				return m, m.replyCmd(m.replyThread, m.replyParent, text)
			}
			input, cmd := m.replyPrompt.Update(msg)
			m.replyPrompt = &input
			return m, cmd
		}

		// A vote is the other modal state this view has, alongside the reply
		// prompt above — only one is ever active, and this one takes every
		// key too, so that a key meant to confirm or cancel the vote cannot
		// be read as something else instead.
		if m.armedVote != nil {
			return m, m.handleArmedVote(msg)
		}

		// The draft toggle arms the same way, and takes every key while it is
		// up for the same reason.
		if m.armedDraft != nil {
			return m, m.handleArmedDraft(msg)
		}

		switch {
		case key.Matches(msg, keyBack):
			return m, func() tea.Msg { return PopMsg{} }
		case key.Matches(msg, keyTop):
			m.viewport.GotoTop()
			return m, nil
		case key.Matches(msg, keyBottom):
			m.viewport.GotoBottom()
			return m, nil
		case key.Matches(msg, keyThreadFilter):
			m.filter = m.filter.next()
			m.invalidate()
			return m, nil
		case key.Matches(msg, keyRefresh):
			m.loaded, m.failed = false, false
			m.linkedLoaded, m.linkedErr = false, false
			return m, m.Init()
		case key.Matches(msg, keyDiff):
			// Reachable only with neither modal state open, because both of
			// them are handled above and swallow every key they do not
			// recognise. That is the right answer rather than an oversight: a
			// vote armed by A is confirmed by a second A, and pushing a whole
			// view over the top would leave that arm alive underneath a screen
			// with no room to say so. The status line keeps saying what is
			// pending, so esc-then-D is one keystroke away and the reader can
			// see why.
			if m.pr.SourceCommit == "" || m.pr.TargetCommit == "" {
				// Nothing to diff against: see PullRequest.SourceCommit.
				m.status, m.failed = "Azure DevOps has not computed this pull request's merge yet, so there is nothing to diff", false
				return m, nil
			}
			// The discussion goes with it: this view has already fetched it,
			// and the diff pane shows each comment against the line it was
			// written on rather than fetching the same threads again.
			// The filter goes with it too: the diff pane is where a filtered
			// discussion is followed up into the code.
			diff := NewPullRequestDiff(m.client, m.pr, m.threads, m.filter)
			m.hidden = true
			return m, func() tea.Msg { return PushMsg{View: diff} }
		case key.Matches(msg, keyGateBuilds):
			if m.gatesPending {
				return m, nil // already looking them up
			}
			m.gatesPending = true
			m.status, m.failed = "looking for the build gates…", false
			return m, tea.Batch(m.fetchGateBuilds(), m.work.begin(1))
		case key.Matches(msg, keyLinkedItem):
			if len(m.linked) == 0 {
				m.status = "no work items are linked to this pull request"
				return m, nil
			}
			item := NewItem(m.client, m.linked[0], nil)
			m.hidden = true
			return m, func() tea.Msg { return PushMsg{View: item} }
		case key.Matches(msg, keyReply):
			t, ok := m.selectedThread()
			if !ok {
				m.status, m.failed = "no unresolved thread to reply to", false
				return m, nil
			}
			input := textinput.New()
			input.Prompt = "reply: "
			input.Focus()
			m.replyPrompt = &input
			m.replyThread, m.replyParent = t.ID, t.Opener().ID
			return m, textinput.Blink
		case key.Matches(msg, keyResolve):
			t, ok := m.selectedThread()
			if !ok {
				m.status, m.failed = "no unresolved thread to resolve", false
				return m, nil
			}
			m.status, m.failed = "resolving…", false
			return m, m.resolveCmd(t.ID)
		case key.Matches(msg, keyDraftToggle):
			m.armedDraft, m.status = armDraft(m.pr)
			// A refusal is reported as one: the key did nothing, and an
			// ordinary-looking status line reads as though it had.
			m.failed = m.armedDraft == nil
			return m, nil
		case key.Matches(msg, keyApprove), key.Matches(msg, keyWait), key.Matches(msg, keyReject):
			vote, label, _ := matchVoteBinding(msg)
			m.armVote(vote, label)
			return m, nil
		}

		if status, handled := SharedAction(prDetailRow{m.pr, m.client.PullRequestURL(m.pr.Repo, m.pr.ID)}, msg); handled {
			m.status = status.Text
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *PullRequestDetail) invalidate() { m.renderedAt = 0 }

func (m *PullRequestDetail) Body(width, height int) string {
	m.viewport.Width, m.viewport.Height = width, height

	if m.renderedAt != width {
		offset := m.viewport.YOffset
		m.viewport.SetContent(m.render(width))
		m.renderedAt = width
		m.viewport.SetYOffset(offset)
	}
	return m.viewport.View()
}

func (m *PullRequestDetail) render(width int) string {
	var b strings.Builder

	title := fmt.Sprintf("!%d  %s", m.pr.ID, m.pr.Title)
	if m.pr.IsDraft {
		title += "  " + draftStyle.Render("◌ draft")
	}
	fmt.Fprintf(&b, "%s\n\n", detailTitle.Render(truncate(title, width)))

	for _, field := range [][2]string{
		{"repo", m.pr.Repo},
		{"author", m.pr.Author},
		{"source", shortRef(m.pr.Source)},
		{"target", shortRef(m.pr.Target)},
		{"opened", humanAge(m.pr.Created, m.now()) + " ago"},
	} {
		fmt.Fprintf(&b, "%s %s\n",
			labelStyle.Render(fmt.Sprintf("%-8s", field[0]+":")),
			truncate(field[1], width-9))
	}

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("reviewers"))
	if len(m.pr.Reviewers) == 0 {
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(none)"))
	}
	for _, r := range m.pr.Reviewers {
		name := r.Name
		if r.IsGroup {
			// Worth marking: a group reviewer means the vote is owed by
			// whoever is in it, which is not obvious from a display name.
			name += chromeStyle.Render(" (group)")
		}
		fmt.Fprintf(&b, "  %s — %s\n", name, voteStyle(r.Vote).Render(r.VoteLabel()))
	}
	if _, ok := m.client.MyReviewerID(m.pr); ok {
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("  A approves · W waits for author · X rejects"))
	}

	fmt.Fprintf(&b, "\n%s\n%s\n", labelStyle.Render("description"),
		renderMarkdown(m.pr.Description, "_no description_", width))

	m.renderLinked(&b, width)
	m.renderThreads(&b, width)
	return b.String()
}

// renderLinked writes the work items this pull request is attached to, which is
// the other half of the loop the branch flow opens: b links a branch to an
// item, and this is where that link comes back.
func (m *PullRequestDetail) renderLinked(b *strings.Builder, width int) {
	fmt.Fprintf(b, "\n%s\n", labelStyle.Render("work items"))

	switch {
	case m.linkedErr:
		fmt.Fprintf(b, "%s\n", errStyle.Render("could not load the links — press r to try again"))
	case !m.linkedLoaded:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render(m.work.View()+"loading…"))
	case len(m.linked) == 0:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("(none linked)"))
	default:
		for i, wi := range m.linked {
			marker := " "
			if i == 0 {
				marker = "▸" // what w opens
			}
			fmt.Fprintf(b, "%s %s\n", marker, truncate(fmt.Sprintf("#%d %s — %s · %s",
				wi.ID, wi.Title, wi.State, wi.Assigned), width-2))
		}
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("  w opens the first"))
	}
}

// renderThreads writes every discussion, unresolved first: those are the ones
// still waiting on somebody, and burying them under settled ones would defeat
// the point of opening the view.
func (m *PullRequestDetail) renderThreads(b *strings.Builder, width int) {
	// The tally counts the whole discussion however it is filtered: it is
	// what says how much of it is off screen.
	counts := azdo.Summarize(m.threads)
	head := fmt.Sprintf("discussion — %d resolved, %d unresolved", counts.Resolved, counts.Unresolved)
	if m.filter != filterAll {
		head += " · " + m.filter.label()
	}
	fmt.Fprintf(b, "\n%s%s\n", labelStyle.Render(head), chromeStyle.Render("  f filters"))

	switch {
	case m.failed:
		fmt.Fprintf(b, "%s\n", errStyle.Render("could not load the discussion — press r to try again"))
		return
	case !m.loaded:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render(m.work.View()+"loading…"))
		return
	case len(m.threads) == 0:
		fmt.Fprintf(b, "%s\n", chromeStyle.Render("(no comments)"))
		return
	}

	selected, hasSelected := m.selectedThread()

	shown := 0
	for _, pass := range []bool{false, true} {
		for _, t := range m.threads {
			if t.Resolved != pass || !m.filter.keep(t) {
				continue
			}
			shown++
			m.renderThread(b, t, hasSelected && t.ID == selected.ID, width)
		}
	}
	if shown == 0 {
		// Not "(no comments)": there is discussion here, the filter is
		// hiding it, and a reader who cannot tell those apart will think the
		// feedback they came for has gone.
		fmt.Fprintf(b, "%s\n", chromeStyle.Render(fmt.Sprintf("(no %s comments)", m.filter.noun())))
	}
}

func (m *PullRequestDetail) renderThread(b *strings.Builder, t azdo.Thread, selected bool, width int) {
	marker := warnStyle.Render("● unresolved")
	if t.Resolved {
		marker = statusStyle.Render("✓ resolved")
	}

	head := marker
	if t.File != "" {
		// The line as well as the file: "logs.go:42" is where the reader
		// would go looking, and the file alone leaves them to find it.
		where := t.File
		if t.Line > 0 {
			where = fmt.Sprintf("%s:%d", t.File, t.Line)
		}
		head += chromeStyle.Render("  " + truncate(where, max(10, width-20)))
	}
	if selected {
		// c and R always act on the first unresolved thread — the same
		// "first" precedent renderLinked uses for w — so that thread says so
		// rather than leaving the reader to guess which one a keypress hits.
		head += chromeStyle.Render("  c replies · R resolves")
	}
	fmt.Fprintf(b, "\n%s\n", head)

	for _, c := range t.Comments {
		fmt.Fprintf(b, "%s\n%s\n",
			labelStyle.Render(fmt.Sprintf("  %s · %s", c.Author, humanAge(c.Created, m.now()))),
			renderMarkdown(c.Text, "_(empty)_", max(20, width-2)))
	}
}

func (m *PullRequestDetail) Title() string {
	return fmt.Sprintf("!%d %s · %s · %s/%s",
		m.pr.ID, m.pr.Title, m.pr.Repo, m.client.Org, m.client.Project)
}

// Keys omits the filter: there is nothing here to filter.
//
// The short line carries only the four bindings this view exists for; voting,
// jumping to either end of the pager, refreshing and copying the id are still
// one keystroke away, but behind "?" rather than on the line itself. Eight
// tasks each added one or two keys to this view's own set by hand, and every
// review looked reasonable in isolation — the branch review that looked at
// all of them together found the short line had grown to thirteen bindings
// with descriptions long enough ("linked work item", not just "linked") that
// even trimming it to Diff, the link, reply, resolve and just refresh still
// pushed esc and ? off the end of it at 80 columns — a defect that, once
// looked for, turned out to also be live on two other views built on
// listKeys. See TestEveryViewsShortHelpSurvivesOrdinaryWidths in
// keys_test.go, which renders every view's footer through the real help
// component rather than trusting a character count.
func (m *PullRequestDetail) Keys() help.KeyMap {
	short := []key.Binding{keyDiff, keyGateBuilds, keyReply, keyResolve}
	// The draft toggle is in the panel rather than on the footer, labelled for
	// the pull request it is looking at — "publish" on a draft, "mark draft"
	// on a published one — and absent altogether on a merged or abandoned one,
	// which this view reaches through a work item's or a build's link. Offering
	// a key that would only be refused is worse than not offering it.
	full := []key.Binding{keyDiff, keyGateBuilds, keyLinkedItem, keyReply, keyResolve, keyThreadFilter}
	if draftable(m.pr) {
		full = append(full, draftBinding(m.pr.IsDraft))
	}
	full = append(full, keyApprove, keyWait, keyReject, keyTop, keyBottom, keyRefresh)
	return keyMap{
		short:  append(append([]key.Binding{}, short...), keyBack, keyHelp),
		groups: [][]key.Binding{full, {keyCopyID, keySlack, keyOpen}, navBindings()},
	}
}

// Status shows the reply prompt in place of the transient line while it is
// open, the same swap the branch prompt makes in workitems.go.
func (m *PullRequestDetail) Status() (string, bool) {
	if m.replyPrompt != nil {
		return m.replyPrompt.View(), false
	}
	return m.work.View() + m.status, m.failed
}

// Prompting reports whether the reply prompt or one of the two arms is open,
// so Root leaves esc and q to this view rather than treating them as
// navigation or quit — esc has to cancel the arm, not pop the whole pane.
func (m *PullRequestDetail) Prompting() bool {
	return m.replyPrompt != nil || m.armedVote != nil || m.armedDraft != nil
}

// prDetailRow adapts the pull request to the shared copy and open actions, so
// this view names and links it exactly as the list does.
type prDetailRow struct {
	azdo.PullRequest
	url string
}

func (r prDetailRow) FilterValue() string     { return "" }
func (r prDetailRow) Render(width int) string { return "" }
func (r prDetailRow) CopyID() string          { return fmt.Sprint(r.ID) }
func (r prDetailRow) Label() string           { return fmt.Sprintf("!%d %s", r.ID, r.Title) }
func (r prDetailRow) URL() string             { return r.url }
