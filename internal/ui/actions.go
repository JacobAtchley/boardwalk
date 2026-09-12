package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// copyToClipboard and openBrowser are indirected through variables so a test
// can drive both outcomes without depending on whether the machine has pbcopy
// and open — and without really opening a browser window while the suite runs.
var (
	copyToClipboard = CopyToClipboard
	openBrowser     = OpenBrowser
)

// SharedAction runs the copy and open actions every view binds, returning what
// to put on the status line and whether it claimed the key, so a view can fall
// through to its own bindings.
func SharedAction(r Row, msg tea.KeyMsg) (StatusMsg, bool) {
	switch msg.String() {
	case "y", "ctrl+y":
		return report(fmt.Sprintf("copied id %s", r.CopyID()),
			"could not copy to the clipboard", copyToClipboard(r.CopyID())), true
	case "s", "ctrl+s":
		return report(fmt.Sprintf("copied Slack link for %s", firstToken(r.Label())),
			"could not copy to the clipboard", copyToClipboard(SlackLink(r.Label(), r.URL()))), true
	case "o", "ctrl+o":
		return report(fmt.Sprintf("opened %s", firstToken(r.Label())),
			"could not open a browser", openBrowser(r.URL())), true
	}
	return StatusMsg{}, false
}

// report pairs what an action did with what to say when it did not. pbcopy and
// open are separate processes that can be missing or refuse, and both calls
// return an error: without this the status line said "copied id 4021" on a
// machine where nothing had been copied at all.
func report(done, failed string, err error) StatusMsg {
	if err != nil {
		return StatusMsg{Text: fmt.Sprintf("%s: %v", failed, err), Err: true}
	}
	return StatusMsg{Text: done}
}

// firstToken is the identifier at the head of a row label — "#4021" or "!512" —
// which is all the status line needs to name what was acted on.
func firstToken(label string) string {
	if i := strings.IndexByte(label, ' '); i > 0 {
		return label[:i]
	}
	return label
}

// SlackLink renders a markdown link, which Slack's composer turns into a real
// link on paste. Square brackets in the title would end the link text early, so
// they become parentheses.
func SlackLink(title, url string) string {
	title = strings.NewReplacer("[", "(", "]", ")", "\n", " ", "\r", " ").Replace(title)
	return fmt.Sprintf("[%s](%s)", strings.TrimSpace(title), url)
}

// CopyToClipboard puts s on the system clipboard.
func CopyToClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}
