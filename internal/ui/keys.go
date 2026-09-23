package ui

import "github.com/charmbracelet/bubbles/key"

// The key bindings live here rather than beside the code that acts on them so
// that a binding and the help text describing it cannot drift apart. They did
// once already: the detail-pane scroll keys worked for a whole branch without
// appearing in the README's key table, because the table was prose maintained
// by hand.
var (
	keyFilter     = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	keyCopyID     = key.NewBinding(key.WithKeys("y", "ctrl+y"), key.WithHelp("y", "copy id"))
	keySlack      = key.NewBinding(key.WithKeys("s", "ctrl+s"), key.WithHelp("s", "copy slack link"))
	keyOpen       = key.NewBinding(key.WithKeys("o", "ctrl+o"), key.WithHelp("o", "open in browser"))
	keyDetailUp   = key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "detail up"))
	keyDetailDown = key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^d", "detail down"))
	keyRefresh    = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	keyBack       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keyQuit       = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	keyHelp       = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys"))

	keyScope  = key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "scope"))
	keyActive = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "set active"))
	// keyAssign is "m" for "mine": a is already taken by the fast path to
	// Active, so assigning gets the mnemonic instead of a letter that would
	// need its own justification.
	keyAssign = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "assign to me"))
	// keyState is capital because s is already "copy slack link"; a is kept
	// alongside it rather than folded in, since Active is the common case the
	// picker would otherwise make one keystroke slower.
	keyState    = key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "set state"))
	keyBranch   = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch"))
	keyDrafts   = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "drafts"))
	keyReview   = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "needs my review"))
	keyLinkedPR = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "linked pull request"))
	// keyGateBuilds opens the runs a pull request's build validation policies
	// produced — the reverse of keyLinkedPR, which finds the pull request a
	// run built. It is capital because "b" is the branch key on the work item
	// list, and that list reaches the pull request pane this binding lives on
	// through keyLinkedPR: a key meaning one thing on a list and another on a
	// pane that list opens is the trap keyDiff's comment names.
	keyGateBuilds  = key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "build gates"))
	keyLinkedItem  = key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "linked work item"))
	keyLogs        = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "logs"))
	keyItem        = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open item"))
	keyPullRequest = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open pull request"))
	keyTop         = key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top"))
	keyBottom      = key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom"))
	keyReply       = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "reply"))
	keyResolve     = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "resolve"))
	// keyThreadFilter cycles the discussion between all threads, the
	// unresolved ones and the resolved ones. It is lower case because it is a
	// way of looking rather than an act — the same weight as keyStamps and
	// keyFileDiff — and "f" is free on all three views that show a thread.
	// It is not "/": that is the fuzzy filter over a list's rows everywhere
	// else in the program, and the discussion is not a list of rows.
	keyThreadFilter = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "comment filter"))
	// keyComment shares its letter with keyReply on purpose: answering a
	// thread and commenting on a work item are the same action — add to the
	// discussion — on two different views, so the same mnemonic serves both
	// rather than one of them needing an unrelated letter to stay free.
	keyComment = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment"))
	// keyDiff is capital D because d is already the draft filter on the pull
	// request list, and a key that means one thing on the list and another on
	// the pane it opens is the sort of thing nobody remembers.
	keyDiff = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "diff"))

	// keyDraftToggle toggles a pull request between draft and published. The
	// name is not keyDraft, which would differ from the keyDrafts filter above
	// by one letter while doing something else entirely — the same trap the
	// PullRequestDetail type comment names. P rather
	// than a lowercase letter because d is the draft filter and D the diff,
	// and because it is the only key here that changes what other people get
	// told: publishing notifies every reviewer. Its help text is built per
	// pull request by draftBinding, which is what the views actually show —
	// this is the binding the keypress is matched against.
	keyDraftToggle = key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "publish / mark draft"))

	// keyStamps toggles the timestamp Azure Pipelines writes at the head of
	// every build log line. It is off by default: the prefix is the same 28
	// columns on every row, and it earns its space only when you are timing
	// something. "t" rather than a capital because ctrl+t is the only other
	// claim on the letter and it belongs to a view this one cannot reach.
	keyStamps = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "timestamps"))

	// keyErrorsOnly cuts the log pane down to its error lines and the task
	// headings that own them. It is lower case for the reason keyThreadFilter
	// gives — it is a way of looking rather than an act — and "e" is claimed
	// by nothing else in the program.
	keyErrorsOnly = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "errors only"))

	// keyNextError jumps the log pane to the next "##[error]" line, wrapping
	// at the end. There is deliberately no key for the previous one: the
	// obvious pair — N and p — both already mean something on the build list
	// this pane opens from, and a key that means one thing on a list and
	// another on the pane it opens is the trap keyDiff's comment names.
	// Wrapping is what a single key buys instead: pressing it again from the
	// last error comes back round to the first.
	keyNextError = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next error"))

	// The three build actions are capitals because their lower-case letters
	// are all spoken for in views reachable from the build list: r refreshes
	// everywhere, c replies on a pull request the builds view links to, and n
	// is free today but sits one letter from them. A capital also suits what
	// they do — starting and stopping CI is not the same weight of act as
	// moving a cursor.
	keyRerun       = key.NewBinding(key.WithKeys("Q"), key.WithHelp("Q", "re-run"))
	keyQueue       = key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "new run"))
	keyCancelBuild = key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "cancel run"))

	// keyFileDiff toggles the change marks in the file view off, leaving the
	// file and its highlighting. "d" is the drafts filter on the pull request
	// list, which is two views further up — keyDiff's objection is to a key
	// meaning one thing on a list and another on the pane that list opens,
	// and this is not that pane.
	// keyFileOpen opens the selected file whole, from the diff pane's list.
	// enter is free there and means "open this" on every other list in the
	// program.
	keyFileOpen = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open the file"))
	keyFileDiff = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff marks"))
	// keyLineUp and keyLineDown move the file view's line cursor. The list
	// views read the same keys straight off msg.String(); this one is
	// matched through a binding because the cursor it moves is what a review
	// comment anchors to, and the help panel has to name it.
	keyLineUp   = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "line up"))
	keyLineDown = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "line down"))

	keyApprove = key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "approve"))
	keyWait    = key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "wait for author"))
	keyReject  = key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "reject"))
)

// sharedBindings are the keys every list view carries, in the order the short
// help line reads them.
func sharedBindings() []key.Binding {
	return []key.Binding{keyFilter, keyCopyID, keySlack, keyOpen}
}

// navBindings are the keys Root owns on a view's behalf.
func navBindings() []key.Binding {
	return []key.Binding{keyDetailUp, keyDetailDown, keyBack, keyQuit, keyHelp}
}

// keyMap adapts a view's bindings to what the help component wants: a short
// line for the footer and grouped columns for the panel behind "?".
type keyMap struct {
	short  []key.Binding
	groups [][]key.Binding
}

func (k keyMap) ShortHelp() []key.Binding  { return k.short }
func (k keyMap) FullHelp() [][]key.Binding { return k.groups }

// listKeys builds the key map for a list view from the bindings it adds on
// top of the shared set. short is what actually appears on the footer, and is
// meant to stay small: filter and refresh are never in it, because they are
// already one press of "?" away in the groups below, and full is every
// view-specific binding — short's contents included — so a binding left off
// the footer is still discoverable rather than gone. back and help are always
// appended to short; they are the only way out of a view and the only way to
// see what it did not fit on the footer, and a user who cannot see either has
// to guess.
//
// The split exists because listKeys used to put every one of a view's own
// bindings straight onto the footer alongside filter and refresh. Each list
// view's own set was sized to fit that way in isolation, so nothing caught it
// until a whole-branch review rendered all three through the real help
// component at once: work items and pull requests both lost esc and ? off the
// short line at 80 columns, and builds passed with one column to spare.
func listKeys(short []key.Binding, full ...key.Binding) keyMap {
	line := append(append([]key.Binding{}, short...), keyBack, keyHelp)
	return keyMap{
		short: line,
		groups: [][]key.Binding{
			append(append([]key.Binding{}, full...), keyRefresh),
			sharedBindings(),
			navBindings(),
		},
	}
}
