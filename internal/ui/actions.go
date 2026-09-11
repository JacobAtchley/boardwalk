package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SharedHints is the part of the key line every view has in common.
const SharedHints = "/ filter · y copy id · s slack · o open · ^u/^d detail"

// SharedAction runs the copy and open actions every view binds. It reports
// whether it claimed the key, so a view can fall through to its own bindings.
func SharedAction(r Row, msg tea.KeyMsg) (string, bool) {
	switch msg.String() {
	case "y", "ctrl+y":
		CopyToClipboard(r.CopyID())
		return fmt.Sprintf("copied id %s", r.CopyID()), true
	case "s", "ctrl+s":
		CopyToClipboard(SlackLink(r.Label(), r.URL()))
		return fmt.Sprintf("copied Slack link for %s", firstToken(r.Label())), true
	case "o", "ctrl+o":
		OpenBrowser(r.URL())
		return fmt.Sprintf("opened %s", firstToken(r.Label())), true
	}
	return "", false
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
