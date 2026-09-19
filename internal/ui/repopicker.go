package ui

import (
	"fmt"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
)

// repoPicker is the modal that asks which repository the branch flow should
// create its branch in. It opens only when the working directory does not
// answer that question by itself — see reposFetchedMsg's handler — so running
// boardwalk inside a repository is unchanged, and running it anywhere else
// now works rather than being refused.
//
// Like statePicker it is a field on the view that opened it rather than a
// pushed pane, and it owns every key while it is up.
//
// Unlike statePicker it draws one repository at a time rather than the whole
// list. A project can hold dozens, and the status line is one row: a list of
// forty names truncated to fit would hide the very thing being chosen. The
// name under the cursor and its position in the list both fit at any width.
type repoPicker struct {
	// itemID and branch are the flow this picker is standing in the middle
	// of, captured when it opened. They cannot be recovered from the list
	// afterwards: a refresh landing while the picker is open rebuilds the
	// rows underneath it.
	itemID  int
	branch  string
	options []azdo.Repo
	cursor  int
}

// newRepoPicker opens the picker over the repositories a project holds. The
// caller checks for an empty list first: a picker offering nothing is a
// question with no answers.
func newRepoPicker(id int, branch string, repos []azdo.Repo) *repoPicker {
	return &repoPicker{itemID: id, branch: branch, options: repos}
}

// up and down move the cursor, wrapping the way the state picker's do.
func (p *repoPicker) up() {
	if len(p.options) == 0 {
		return
	}
	p.cursor = (p.cursor - 1 + len(p.options)) % len(p.options)
}

func (p *repoPicker) down() {
	if len(p.options) == 0 {
		return
	}
	p.cursor = (p.cursor + 1) % len(p.options)
}

// selected is the repository under the cursor.
func (p *repoPicker) selected() (azdo.Repo, bool) {
	if p.cursor < 0 || p.cursor >= len(p.options) {
		return azdo.Repo{}, false
	}
	return p.options[p.cursor], true
}

// View renders the picker into the status line's slot, the same slot the
// branch prompt it follows renders into.
func (p *repoPicker) View() string {
	repo, ok := p.selected()
	if !ok {
		return chromeStyle.Render("no repositories to choose from")
	}
	return fmt.Sprintf("repository:  %s%s",
		selectedRow.Render("▸ "+repo.Name),
		chromeStyle.Render(fmt.Sprintf("   (%d of %d · j/k moves · enter picks · esc cancels)",
			p.cursor+1, len(p.options))))
}
