package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// helpLine is what a view's footer advertises, flattened so a test can assert a
// key is offered without depending on how the help component spaces it.
func helpLine(k help.KeyMap) string {
	var b strings.Builder
	for _, bind := range k.ShortHelp() {
		h := bind.Help()
		fmt.Fprintf(&b, "%s %s · ", h.Key, h.Desc)
	}
	return b.String()
}

// helpPanel is the same for the full panel behind "?".
func helpPanel(k help.KeyMap) string {
	var b strings.Builder
	for _, group := range k.FullHelp() {
		for _, bind := range group {
			h := bind.Help()
			fmt.Fprintf(&b, "%s %s · ", h.Key, h.Desc)
		}
	}
	return b.String()
}

func TestEveryBindingCarriesHelpText(t *testing.T) {
	// A binding with no help text renders as a gap in the footer, which is how
	// a key ends up working without ever being advertised.
	for name, b := range map[string]key.Binding{
		"filter": keyFilter, "copy id": keyCopyID, "slack": keySlack,
		"open": keyOpen, "detail up": keyDetailUp, "detail down": keyDetailDown,
		"refresh": keyRefresh, "back": keyBack, "quit": keyQuit, "help": keyHelp,
		"scope": keyScope, "active": keyActive, "assign": keyAssign, "set state": keyState, "branch": keyBranch,
		"drafts": keyDrafts, "logs": keyLogs, "top": keyTop, "bottom": keyBottom,
		"review": keyReview, "linked pull request": keyLinkedPR, "linked work item": keyLinkedItem,
		"item": keyItem, "pull request": keyPullRequest, "reply": keyReply, "resolve": keyResolve,
		"comment": keyComment, "diff": keyDiff, "approve": keyApprove, "wait": keyWait, "reject": keyReject,
	} {
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("%s: help = %+v, want both a key and a description", name, h)
		}
		if len(b.Keys()) == 0 {
			t.Errorf("%s: binding matches no keys", name)
		}
	}
}

func TestListKeysPutsTheViewsOwnKeysFirst(t *testing.T) {
	k := listKeys([]key.Binding{keyDrafts, keyScope}, keyDrafts, keyScope)

	line := helpLine(k)
	if !strings.HasPrefix(line, "d drafts") {
		t.Errorf("short help = %q, want the view's own keys leading", line)
	}
	for _, want := range []string{"d drafts", "^t scope", "esc back", "? keys"} {
		if !strings.Contains(line, want) {
			t.Errorf("short help = %q, missing %q", line, want)
		}
	}
	// filter and refresh are never on the short line — see listKeys's own
	// doc — so the panel is where they have to be found instead.
	panel := helpPanel(k)
	for _, want := range []string{"/ filter", "r refresh"} {
		if strings.Contains(line, want) {
			t.Errorf("short help = %q, carries %q which belongs in the panel", line, want)
		}
		if !strings.Contains(panel, want) {
			t.Errorf("full panel = %q, missing %q", panel, want)
		}
	}
}

func TestTheFullPanelCarriesKeysTheShortLineOmits(t *testing.T) {
	// The point of the panel is the keys that do not fit on one line.
	k := listKeys([]key.Binding{keyLogs}, keyLogs)

	line, panel := helpLine(k), helpPanel(k)
	for _, want := range []string{"y copy id", "s copy slack link", "o open in browser", "^u detail up", "^d detail down", "q quit"} {
		if !strings.Contains(panel, want) {
			t.Errorf("full panel = %q, missing %q", panel, want)
		}
	}
	if strings.Contains(line, "detail up") {
		t.Error("the short line carries a key meant for the panel; it will not fit")
	}
}

// viewKeyMaps builds the key map every view in boardwalk currently ships,
// keyed by name, so the bindings tests below are data-driven over them. There
// is no view registry to walk, so this is a hand-maintained map literal, not
// an automatic sweep: a ninth view is covered by these tests only once it is
// added here too.
func viewKeyMaps(t *testing.T) map[string]help.KeyMap {
	t.Helper()
	itemsClient, items := fixture()
	return map[string]help.KeyMap{
		"work items":          NewWorkItems(itemsClient, items, false, false).Keys(),
		"pull requests":       newPRs(t).Keys(),
		"builds":              newBuilds(t).Keys(),
		"logs":                newLogs(t, azdo.StatusSucceeded).Keys(),
		"pull request detail": newPRDetail(t, nil).Keys(),
		"item":                newItem(t, nil).Keys(),
		"pull request diff":   newPRDiff(t).view.Keys(),
	}
}

// renderShortHelp runs a view's short help through the real help component at
// a given terminal width, the same rendering root.go does — help.ShortHelpView
// truncates in place, silently dropping whatever does not fit rather than
// wrapping it, so measuring string length is not a substitute for this.
func renderShortHelp(km help.KeyMap, width int) string {
	h := help.New()
	h.Width = width
	return h.ShortHelpView(km.ShortHelp())
}

// TestEveryViewsShortHelpSurvivesOrdinaryWidths guards the branch review's
// Finding 1. Eight tasks each added one or two bindings to a view's own key
// set, each addition reasonable in isolation, and three views' short lines
// grew long enough that esc and/or ? were truncated off the end at 80 columns
// (work items still lost ? at 120). keys.go's own listKeys documents why that
// must not happen: back is the only way out of a view, and a user who cannot
// see it has to guess. This is driven off viewKeyMaps rather than a hand-picked
// subset of views — the first version of this test named only the two views
// the finding called out and missed that the same defect was live on two
// others, including the two views every session starts from.
func TestEveryViewsShortHelpSurvivesOrdinaryWidths(t *testing.T) {
	for name, km := range viewKeyMaps(t) {
		t.Run(name, func(t *testing.T) {
			for _, width := range []int{80, 120} {
				line := renderShortHelp(km, width)
				if !strings.Contains(line, "esc back") {
					t.Errorf("width %d: short help = %q, missing \"esc back\"", width, line)
				}
				if !strings.Contains(line, "? keys") {
					t.Errorf("width %d: short help = %q, missing \"? keys\"", width, line)
				}
			}
		})
	}
}

// TestNoViewBindsTwoActionsToTheSameKey is Finding 4: twelve bindings were
// allocated across eight tasks by hand, and nothing checked that two of them
// never landed on the same key within one view. Two bindings sharing a key
// across different views is fine and expected — c means reply on the pull
// request detail view and comment on the item view, enter means something on
// every view — so a key is only flagged when two DIFFERENT actions inside the
// SAME view's key map both claim it, which is what would make the second one
// unreachable.
func TestNoViewBindsTwoActionsToTheSameKey(t *testing.T) {
	for name, km := range viewKeyMaps(t) {
		t.Run(name, func(t *testing.T) {
			claimedBy := map[string]string{} // physical key -> the description already claiming it
			claim := func(b key.Binding) {
				desc := b.Help().Desc
				for _, physicalKey := range b.Keys() {
					if by, ok := claimedBy[physicalKey]; ok && by != desc {
						t.Errorf("%q is bound to both %q and %q", physicalKey, by, desc)
					}
					claimedBy[physicalKey] = desc
				}
			}
			for _, b := range km.ShortHelp() {
				claim(b)
			}
			for _, group := range km.FullHelp() {
				for _, b := range group {
					claim(b)
				}
			}
		})
	}
}
