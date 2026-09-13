package ui

import (
	"fmt"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxDiffBytes is the largest either side of a file may be before the pane
// declines to render it.
//
// A quarter of a megabyte is far more than anything a person reads line by line
// in a terminal — it is a generated client, a lock file or a vendored bundle,
// and those are exactly the files a reviewer scrolls past. The cap is on the
// fetched text rather than on the number of hunks because the costs it is there
// to bound are paid before the hunks exist: both sides come down the wire, are
// held whole in a message, and are then walked by the diff.
const maxDiffBytes = 256 << 10

// binarySniff is how much of a file is examined for the NUL byte that says it
// is not text. It is git's own heuristic and its own window: a real binary
// carries a NUL long before this, and a text file that somehow carries one past
// it would be rendered as text, which is the failure worth having of the two.
const binarySniff = 8000

// diffListShare is narrower than the browser's default. The rows here are file
// paths, which stay readable truncated; the pane beside them is source code,
// which does not.
const diffListShare = 2.0 / 5.0

// tabWidth is what a tab becomes before a diff line is measured and cut. A real
// tab is left to the terminal to expand, which puts the character at a column
// neither truncate nor the pane border can predict, so a line of tab-indented
// code overflowed into the list beside it.
const tabWidth = 4

// changeRow adapts one changed file to the browser.
type changeRow struct {
	azdo.Change
	url string
	// read is the view's map of files whose diffs have landed, held by
	// reference rather than flattened into a bool per row. A row can then say
	// what the lazy fetch has reached without the list being rebuilt to tell
	// it, and rebuilding the list is the thing worth avoiding: SetRows
	// re-renders the detail pane, which returns it to the top. A slow fetch for
	// a file the cursor has since left would otherwise yank the pane out from
	// under whoever is scrolled into it.
	//
	// Shared mutable state read during a render, which is only safe because
	// bubbletea runs Update and View on one goroutine — the map is written in
	// Update and read here, never from a command.
	read map[string]fileDiff
}

func (r changeRow) FilterValue() string { return r.Path }

func (r changeRow) Render(width int) string {
	pending := " "
	if _, done := r.read[r.Path]; !done {
		pending = chromeStyle.Render("·")
	}
	// The glyph and the pending marker are one display column each, styled or
	// not, so the path can be cut against the width they leave.
	return fmt.Sprintf("%s %s %s", changeGlyph(r.ChangeType), pending,
		shortPath(r.Path, max(4, width-4)))
}

// shortPath fits a repository path to width by dropping leading directories
// rather than the tail. Cutting from the right loses the file name, which is
// the half of a path a reader picks a row by — and this list is the narrow one,
// so most paths in a real repository do not fit.
func shortPath(path string, width int) string {
	if lipgloss.Width(path) <= width {
		return path
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := 1; i < len(parts); i++ {
		if candidate := "…/" + strings.Join(parts[i:], "/"); lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	// Not even the bare file name fits, so there is nothing left but to cut it.
	return truncate(parts[len(parts)-1], width)
}

func (r changeRow) CopyID() string { return r.Path }
func (r changeRow) Label() string  { return r.Path }
func (r changeRow) URL() string    { return r.url }

// changeGlyph reads a change type as a single coloured column. Azure DevOps
// reports combinations — "edit, rename" — so the tests are for containment
// rather than equality, most specific first.
func changeGlyph(changeType string) string {
	switch {
	case isChange(changeType, "delete"):
		return removedLine.Render("−")
	case isChange(changeType, "add"):
		return addedLine.Render("+")
	case isChange(changeType, "rename"):
		return warnStyle.Render("→")
	default:
		return chromeStyle.Render("~")
	}
}

// isChange reports whether a change type includes a given kind. The comparison
// is case-insensitive because Azure DevOps names some of them in camel case —
// "sourceRename" and "targetRename" carry a capital R that a plain containment
// test against "rename" misses.
func isChange(changeType, kind string) bool {
	return strings.Contains(strings.ToLower(changeType), kind)
}

// hasOldSide and hasNewSide report which commits a change has content at.
// Fetching a side that does not exist is a round trip that can only 404.
//
// A rename is counted as having no old side. The change entry names only where
// the file landed, not where it came from, so its previous content is at a path
// this view cannot address — a renamed file therefore reads as a new one rather
// than as a move.
func hasOldSide(changeType string) bool {
	return !isChange(changeType, "add") && !isChange(changeType, "rename")
}

func hasNewSide(changeType string) bool { return !isChange(changeType, "delete") }

// fileDiff is everything the pane needs about one file: the hunks to draw, or
// the one line explaining why there are none.
type fileDiff struct {
	hunks []*udiff.Hunk
	// note says why a file is shown as a sentence rather than as a diff —
	// binary, or past the size cap.
	note string
	err  string
}

// prChangesMsg carries the files a pull request touches. It names its pull
// request for the same reason threadsLoadedMsg does: Root broadcasts data to
// every view in the stack, and the detail view this was opened from is
// underneath, still fetching things of its own.
type prChangesMsg struct {
	PR      int
	Changes []azdo.Change
	Err     error
}

// fileDiffMsg carries one file's diff, named by path because a slow fetch can
// land long after the cursor has moved off the row that asked for it.
//
// generation is stamped with the view's generation counter at the moment the
// fetch was issued, unexported because nothing outside this file has a
// reason to set it to anything but the zero value a fresh fetch is compared
// against. Path alone is not enough to tell a stale answer from a current
// one: refresh clears diffs and loading and asks again for the same path, so
// a diff that was already in flight when refresh was pressed lands with a
// path that matches perfectly and content that belongs to the fetch refresh
// just superseded.
type fileDiffMsg struct {
	PR         int
	Path       string
	Diff       fileDiff
	generation int
}

// PullRequestDiff is the code behind a pull request: the changed files as a
// list, the selected file's diff beside it.
type PullRequestDiff struct {
	client  *azdo.Client
	pr      azdo.PullRequest
	browser Browser

	changes []azdo.Change
	diffs   map[string]fileDiff
	loading map[string]bool

	// generation counts refreshes. Bumped before Init re-fetches the file
	// list, it is what lets fileDiffMsg tell a fetch issued before the refresh
	// apart from one issued after — see fileDiffMsg's doc.
	generation int

	loaded bool
	status string
	failed bool
	work   work
}

// NewPullRequestDiff builds the view. It fetches nothing itself — Init does
// that — so it can be constructed without a client that can reach the network.
func NewPullRequestDiff(c *azdo.Client, pr azdo.PullRequest) *PullRequestDiff {
	m := &PullRequestDiff{
		client:  c,
		pr:      pr,
		browser: NewBrowser(),
		diffs:   map[string]fileDiff{},
		loading: map[string]bool{},
		work:    newWork(),
	}
	m.browser.ListShare = diffListShare
	m.browser.Detail = m.renderDetail
	return m
}

// Init lists the files the pull request touches. The two round trips are one
// command: the second needs the first's answer, and splitting them would put a
// message on the wire whose only content is an id nothing else ever reads.
func (m *PullRequestDiff) Init() tea.Cmd {
	client, repo, id := m.client, m.pr.RepoID, m.pr.ID
	fetch := func() tea.Msg {
		iterations, err := client.PullRequestIterations(repo, id)
		if err != nil {
			return prChangesMsg{PR: id, Err: fmt.Errorf("could not list the pull request's iterations: %w", err)}
		}
		if len(iterations) == 0 {
			return prChangesMsg{PR: id}
		}

		// The last iteration is the pull request as it stands now; the earlier
		// ones are the states it passed through as the author pushed.
		latest := iterations[len(iterations)-1].ID
		changes, err := client.IterationChanges(repo, id, latest)
		if err != nil {
			return prChangesMsg{PR: id, Err: fmt.Errorf("could not list the changed files: %w", err)}
		}
		return prChangesMsg{PR: id, Changes: changes}
	}
	return tea.Batch(fetch, m.work.begin(1))
}

// applyRows rebuilds the browser's rows. It is called only when the file list
// itself changes — a diff landing does not need it, since the rows read m.diffs
// live.
func (m *PullRequestDiff) applyRows() {
	rows := make([]Row, 0, len(m.changes))
	for _, ch := range m.changes {
		rows = append(rows, changeRow{
			Change: ch,
			url:    m.client.PullRequestFileURL(m.pr.Repo, m.pr.ID, ch.Path),
			read:   m.diffs,
		})
	}
	m.browser.SetRows(rows)
}

// selectedPath is the file under the cursor, or false when the list is empty.
func (m *PullRequestDiff) selectedPath() (string, bool) {
	row, ok := m.browser.Selected()
	if !ok {
		return "", false
	}
	r, ok := row.(changeRow)
	if !ok {
		return "", false
	}
	return r.Path, true
}

// fetchSelected loads the diff for the row under the cursor, and only that row.
// A pull request touching two hundred files would otherwise open by asking for
// four hundred file bodies nobody is going to look at — the whole reason this
// is per-file rather than the eager-first-screenful approach the two list views
// take, where a row's extra fetch is one small summary rather than two whole
// files.
func (m *PullRequestDiff) fetchSelected() tea.Cmd {
	row, ok := m.browser.Selected()
	if !ok {
		return nil
	}
	r, ok := row.(changeRow)
	if !ok {
		return nil
	}
	if _, done := m.diffs[r.Path]; done || m.loading[r.Path] {
		return nil
	}
	m.loading[r.Path] = true

	client, repo, prID := m.client, m.pr.RepoID, m.pr.ID
	path, changeType := r.Path, r.ChangeType
	oldSHA, newSHA := m.pr.TargetCommit, m.pr.SourceCommit
	gen := m.generation

	fetch := func() tea.Msg {
		return fileDiffMsg{
			PR:         prID,
			Path:       path,
			Diff:       buildFileDiff(client, repo, path, changeType, oldSHA, newSHA),
			generation: gen,
		}
	}
	return tea.Batch(fetch, m.work.begin(1))
}

// buildFileDiff fetches both sides of one file and diffs them. It runs inside
// the command goroutine rather than in Update because diffing two long files is
// the one piece of work here that can take long enough to drop a frame, and
// bubbletea's Update runs on the same goroutine that paints.
func buildFileDiff(c *azdo.Client, repoID, path, changeType, oldSHA, newSHA string) fileDiff {
	var before, after string

	if hasOldSide(changeType) && oldSHA != "" {
		text, err := c.FileAtCommit(repoID, path, oldSHA)
		if err != nil {
			return fileDiff{err: fmt.Sprintf("could not read the previous version: %v", err)}
		}
		before = text
	}
	if hasNewSide(changeType) && newSHA != "" {
		text, err := c.FileAtCommit(repoID, path, newSHA)
		if err != nil {
			return fileDiff{err: fmt.Sprintf("could not read the new version: %v", err)}
		}
		after = text
	}

	if note := unrenderable(before, after); note != "" {
		return fileDiff{note: note}
	}

	// Lines rather than Strings: Strings diffs runes and only then widens the
	// edits to line boundaries, which reports a line whose neighbour changed as
	// removed and re-added. On a review diff that is noise — the reader has to
	// compare the two halves character by character to see that nothing
	// happened — and it makes the work superlinear in the file's length rather
	// than in its line count.
	unified, err := udiff.ToUnifiedDiff(path, path, before, udiff.Lines(before, after), udiff.DefaultContextLines)
	if err != nil {
		return fileDiff{err: fmt.Sprintf("could not diff the file: %v", err)}
	}
	return fileDiff{hunks: unified.Hunks}
}

// unrenderable reports why a file will not be shown as a diff, or "" when it
// will be. Rendering a binary as text is not merely unreadable: its escape
// bytes are interpreted by the terminal, which can leave the whole pane in a
// colour or a character set nothing on screen asked for.
func unrenderable(before, after string) string {
	switch {
	case isBinary(before) || isBinary(after):
		return "binary file — not shown"
	case len(before) > maxDiffBytes || len(after) > maxDiffBytes:
		return fmt.Sprintf("file is larger than %d KiB — not shown", maxDiffBytes>>10)
	}
	return ""
}

// isBinary reports whether content looks like something that is not text.
func isBinary(s string) bool {
	if len(s) > binarySniff {
		s = s[:binarySniff]
	}
	return strings.IndexByte(s, 0) >= 0
}

// Update handles input and fetch results. It satisfies View.
func (m *PullRequestDiff) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return m, m.work.tick(msg)

	case prChangesMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		m.work.done()
		if msg.Err != nil {
			// loaded stays false so Body keeps the placeholder, which renders
			// the failure and the r that retries it. Flipping it here would put
			// the empty state's "this pull request changes no files" on screen
			// instead, over a status line contradicting it.
			m.status, m.failed = msg.Err.Error(), true
			return m, nil
		}
		m.loaded = true
		m.changes, m.status, m.failed = msg.Changes, "", false
		m.applyRows()
		return m, m.fetchSelected()

	case fileDiffMsg:
		if msg.PR != m.pr.ID {
			return m, nil
		}
		// work.done() runs regardless of generation: this fetch was counted
		// when it was issued, and a refresh does not un-issue it, so skipping
		// this would leave the spinner counting a fetch that will never
		// answer again.
		m.work.done()
		if msg.generation != m.generation {
			// Superseded by a refresh. Storing this anyway would win the race
			// against the fetch refresh just issued for the same path: this
			// answer would land in m.diffs first, and fetchSelected would then
			// find the path already present and never ask again — refresh
			// would silently keep the reader on the diff it was trying to
			// replace. Dropping it leaves the path exactly as refresh left it,
			// pending, until the current generation's own fetch answers.
			return m, nil
		}
		// Cleared before anything else: a fetch that failed must still leave
		// the path free to be asked for again, rather than stuck loading.
		delete(m.loading, msg.Path)

		m.diffs[msg.Path] = msg.Diff
		if msg.Diff.err != "" {
			m.status, m.failed = msg.Diff.err, true
		}

		// Only the pane showing this file is re-rendered, and the rows pick the
		// landing up on their own. Re-rendering unconditionally would return the
		// detail pane to the top whichever file had landed, so a slow fetch for
		// a row the cursor had already left would snap the pane the reader is
		// scrolled into back to line one mid-read.
		if selected, ok := m.selectedPath(); ok && selected == msg.Path {
			m.browser.RefreshDetail()
		}
		return m, nil

	// No case for ErrMsg here on purpose. This view never emits one — its own
	// failures are prChangesMsg.Err and fileDiffMsg.Diff.err, both named by
	// pull request or path — so ErrMsg reaching Update is always another
	// view's fetch failing lower in the stack. Root broadcasts it exactly like
	// every other data message (see topOnly in root.go); reacting to it here
	// used to mean printing someone else's error over this view's own status
	// line and decrementing this view's work counter for a fetch this view
	// never started, which left the actual owner spinning forever.

	case StatusMsg:
		m.status, m.failed = msg.Text, msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.browser.Filtering() {
			break
		}

		if row, ok := m.browser.Selected(); ok {
			if status, handled := SharedAction(row, msg); handled {
				m.status, m.failed = status.Text, status.Err
				return m, nil
			}
		}

		if key.Matches(msg, keyRefresh) {
			// Emptied in place rather than replaced: the rows already on screen
			// hold a reference to this map, and handing the view a new one
			// would leave them reporting what the old one said until the fresh
			// file list lands.
			clear(m.diffs)
			clear(m.loading)
			// Bumped so any fileDiffMsg already in flight from before this
			// refresh is recognisable as stale when it lands — see
			// fileDiffMsg's doc and the generation check in its case above.
			m.generation++
			m.status, m.failed = "refreshing…", false
			return m, m.Init()
		}
	}

	cmd := m.browser.Update(msg)
	return m, tea.Batch(cmd, m.fetchSelected())
}

func (m *PullRequestDiff) renderDetail(row Row, width int) string {
	r, ok := row.(changeRow)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n",
		detailTitle.Render(truncate(r.Path, width)),
		chromeStyle.Render(changeLabel(r.ChangeType)))

	d, known := m.diffs[r.Path]
	switch {
	case !known:
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render(m.work.View()+"reading the file…"))
	case d.err != "":
		fmt.Fprintf(&b, "%s\n", errStyle.Render(wordwrap(d.err, width)))
	case d.note != "":
		fmt.Fprintf(&b, "%s\n", warnStyle.Render(d.note))
	case len(d.hunks) == 0:
		// A file Azure DevOps lists as changed but whose text is identical at
		// both commits: a mode change, or a merge that reverted it.
		fmt.Fprintf(&b, "%s\n", chromeStyle.Render("(no textual changes)"))
	default:
		renderHunks(&b, d.hunks, width)
	}
	return b.String()
}

// changeLabel names a change type the way a person would say it.
func changeLabel(changeType string) string {
	if changeType == "" {
		return "changed"
	}
	return changeType
}

// renderHunks writes a unified diff in the palette's colours: additions green,
// deletions red, and each hunk header in the same label colour every other
// section heading uses, so the headers read as structure rather than as content.
func renderHunks(b *strings.Builder, hunks []*udiff.Hunk, width int) {
	for i, h := range hunks {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "%s\n", labelStyle.Render(hunkHeader(h)))

		for _, l := range h.Lines {
			marker, style := diffLineStyle(l.Kind)

			// Content keeps the newline it was split on; a line missing one is
			// the last line of a file that does not end with one.
			text, terminated := strings.CutSuffix(l.Content, "\n")
			fmt.Fprintf(b, "%s\n", style.Render(truncate(marker+expandTabs(text), width)))
			if !terminated {
				fmt.Fprintf(b, "%s\n", chromeStyle.Render(`\ no newline at end of file`))
			}
		}
	}
}

// diffLineStyle pairs a line's kind with the marker and colour it is drawn in.
// It is a function of its own rather than a switch inside renderHunks so that
// the one thing a diff pane has to get right — which colour means arrived and
// which means went — can be asserted without a terminal, since lipgloss drops
// colour entirely when the output is not one.
func diffLineStyle(kind udiff.OpKind) (marker string, style lipgloss.Style) {
	switch kind {
	case udiff.Insert:
		return "+", addedLine
	case udiff.Delete:
		return "-", removedLine
	default:
		return " ", normalRow
	}
}

// hunkHeader is the @@ line, counting each side's lines the way a unified diff
// does: a deletion belongs to the old file, an insertion to the new, and
// context to both.
func hunkHeader(h *udiff.Hunk) string {
	fromCount, toCount := 0, 0
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Delete:
			fromCount++
		case udiff.Insert:
			toCount++
		default:
			fromCount++
			toCount++
		}
	}
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.FromLine, fromCount, h.ToLine, toCount)
}

func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
}

// Body renders the browser at the size Root has left for it.
func (m *PullRequestDiff) Body(width, height int) string {
	m.browser.SetSize(width, height)
	if !m.loaded {
		return placeholder("the changed files", m.status, m.failed, m.work.View())
	}
	if m.browser.Len() == 0 {
		return emptyState(m.emptyMessage(), width, height)
	}
	return m.browser.View()
}

// emptyMessage says why there is nothing to read. A pull request with no merge
// commits is the case worth naming separately: Azure DevOps has not computed
// the merge yet, or could not, and saying "no files" about it would read as a
// pull request that changes nothing.
func (m *PullRequestDiff) emptyMessage() string {
	if m.pr.SourceCommit == "" || m.pr.TargetCommit == "" {
		return "Azure DevOps has not computed this pull request's merge yet"
	}
	return "this pull request changes no files"
}

// Title names the pull request and how many files it touches.
func (m *PullRequestDiff) Title() string {
	return fmt.Sprintf("!%d %s · %d files · %s · %s/%s",
		m.pr.ID, m.pr.Title, m.browser.Len(), m.pr.Repo, m.client.Org, m.client.Project)
}

// Keys are the shared list bindings. The view adds none of its own: everything
// here is reading. Filter and refresh sit in the panel behind "?" rather than
// on the footer, the same as every other list view now — see listKeys's own
// doc for why.
func (m *PullRequestDiff) Keys() help.KeyMap { return listKeys(nil) }

// Status is the transient status line, or the fuzzy filter prompt while one is
// open.
func (m *PullRequestDiff) Status() (string, bool) {
	if m.browser.Filtering() {
		return m.browser.FilterView(), false
	}
	return m.work.View() + m.status, m.failed
}

// Prompting reports whether a text prompt is open, so Root leaves esc and q to
// the prompt rather than treating them as navigation.
func (m *PullRequestDiff) Prompting() bool { return m.browser.Filtering() }
