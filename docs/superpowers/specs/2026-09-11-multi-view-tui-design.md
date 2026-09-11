# boardwalk: multi-view TUI

Design for turning boardwalk from a single work item browser into a three-view
Azure DevOps terminal client: work items, pull requests, and pipeline builds,
reached from a banner menu.

## Goals

- A landing screen with an ASCII banner and a menu for the three views.
- A pull request view with draft filtering, branch pairs, comment resolution
  counts, and age, newest first.
- A build view showing pipeline name, current step, and errors, drilling into
  logs that tail while the build runs.
- A richer work item detail pane: iteration, tags, assignee, description,
  acceptance criteria, and discussion.
- The same copy-id, copy-Slack-link, and open-in-browser keys on every view.
- Work item branch creation, state changes, and branch association performed in
  process rather than delegated to an external script.

## Non-goals

- Creating, editing, or commenting on pull requests.
- Queuing, cancelling, or retrying builds.
- Editing work item fields other than state.
- Any authentication of boardwalk's own. Tokens continue to come from the az
  CLI.

## Architecture

### View stack

`internal/ui/root.go` holds a `Root` model that owns the menu, a stack of
child views, the window size, and the status line. Children satisfy a small
interface:

```go
type View interface {
    Update(tea.Msg) (View, tea.Cmd)
    Body(width, height int) string
    Title() string
    Hints() string
}
```

`Body` returns only the body. Root draws the header, the hint line, and the
status line, so chrome is identical everywhere and changes in one place.

`esc` pops the stack: a drill-down returns to its list, a list returns to the
menu, and the menu exits. `q` exits from anywhere.

### Shared browser scaffold

Today's `workitems.go` hardcodes a list and detail pane, a three-fifths split, a
fuzzy filter, and the copy and open actions. `internal/ui/browser.go` extracts
that scaffold and parameterizes it by three things a concrete view supplies:

- a row renderer, given the row and the available width,
- a detail renderer, given the selected row and the pane width,
- an extra-keys handler for whatever the view adds beyond the shared actions.

Work items, pull requests, and builds are each built on it.

### Loading

Views fetch on entry through a `tea.Cmd` and show a spinner, rather than the
program fetching before the TUI starts. The menu therefore paints immediately.
`main.go` loses its eager work item fetch.

### Entry points

```
boardwalk           # banner and menu
boardwalk items     # straight to work items
boardwalk prs       # straight to pull requests
boardwalk builds    # straight to builds
```

Existing flags are unchanged in meaning. `-mine` and `-all` imply `items` when
no subcommand is given. `-dump` keeps printing work item rows and exiting.

### Files

Added:

```
internal/ui/root.go            menu, view stack, chrome
internal/ui/banner.go          embedded ASCII banner
internal/ui/browser.go         shared list and detail scaffold
internal/ui/pullrequests.go
internal/ui/builds.go
internal/ui/logs.go            log pager and tail
internal/ui/actions.go         copy, Slack, open
internal/azdo/pullrequests.go
internal/azdo/builds.go
internal/azdo/git.go           repositories, refs, remote detection
internal/azdo/update.go        JSON Patch against work items
```

Changed: `main.go` gains subcommand dispatch; `internal/ui/workitems.go` is
rebuilt on the browser scaffold and shrinks; `internal/azdo/client.go` gains
`get` and `patch`; `internal/azdo/workitems.go` gains acceptance criteria and
comments; `internal/ui/style.go` gains build and PR status colors.

## Data layer

`client.go` grows `get(url, out)` and `patch(url, contentType, body, out)`
beside the existing `post`, all routed through one `do` that attaches the bearer
token and unwraps Azure DevOps error bodies.

| Need | Call |
| --- | --- |
| Pull requests, all repositories | `GET /{project}/_apis/git/pullrequests?searchCriteria.status=active&$top=200` |
| Comment threads | `GET /{project}/_apis/git/repositories/{repoId}/pullRequests/{id}/threads` |
| Builds | `GET /{project}/_apis/build/builds?$top=50&queryOrder=queueTimeDescending` |
| Current step and errors | `GET /{project}/_apis/build/builds/{id}/timeline` |
| Log list and content | `GET /{project}/_apis/build/builds/{id}/logs`, then `/logs/{logId}` |
| Work item discussion | `GET /{project}/_apis/wit/workItems/{id}/comments` |
| Work item state | `PATCH /_apis/wit/workitems/{id}` as `application/json-patch+json` |
| Repositories | `GET /{project}/_apis/git/repositories` |
| Create a branch | `POST /_apis/git/repositories/{repoId}/refs` |

The comments endpoint is only available under `7.1-preview.3`, so it overrides
the pinned `7.1` for that one call. Every other call uses the pin.

### Comment resolution

A pull request thread carries a `status`. `fixed`, `closed`, `wontFix`, and
`byDesign` count as resolved; `active` and `pending` count as unresolved.
Threads whose comments are all `commentType: "system"` are the automatic
"added a reviewer" and "updated the source branch" entries, and are dropped
before counting. Without that exclusion every pull request reports a dozen
threads nobody wrote.

Threads are fetched eagerly for the first screenful of rows and lazily as the
cursor moves beyond it, cached by pull request id. The count column shows an
ellipsis until its fetch lands.

### Build status and current step

Status is derived from the build's `status` and `result` fields into one of
queued, running, succeeded, failed, partial, or canceled, each with its own
color.

Current step comes from the timeline: the first record in `inProgress` for a
running build, the failing record for a failed build, and an em dash for a
build that finished clean. Error count is the number of timeline records whose
result is `failed`.

## Views

### Pull requests

Columns: repository, `#id`, a draft marker, title, `source → target` with
`refs/heads/` stripped from both, resolved and unresolved counts, and age
rendered compactly (`4h`, `3d`, `2w`). Sorted by creation date, newest first.

`d` cycles a three-state draft filter: exclude drafts, which is the default,
then drafts only, then all.

When the working directory is an Azure DevOps repository the list starts
filtered to it, and `^t` widens to the whole project. That mirrors `^t` for
mine and all on work items.

The detail pane shows the description, the author, reviewers with their votes,
and the first comment of each unresolved thread.

### Builds

Columns: pipeline name, build number, status glyph, current step, error count,
and age. The fuzzy filter spans pipeline name and build number, so a flat list
of recent runs is still narrowable to one pipeline. `enter` drills into logs.

### Logs

A viewport pager over the build's logs, concatenated in timeline order with a
`── task name ──` rule between them.

While the build is in progress a three-second poll appends new content. Each
log's consumed byte offset is tracked, so a poll re-fetches only the tail of the
log still being written. Polling stops when the build's status becomes
completed.

Keys: `g` and `G` for top and bottom, `/` to search, `esc` to go back.

### Work items

The list is unchanged. The detail pane becomes scrollable and shows, in order:
iteration, tags, assignee, description, acceptance criteria, and discussion.
Acceptance criteria comes from `Microsoft.VSTS.Common.AcceptanceCriteria`, added
to the existing batch field list. Discussion is fetched lazily on selection and
each comment renders as `author · age` above its stripped body.

## Actions

Every view binds `y` to copy the id, `s` to copy a Slack link, and `o` to open
in a browser. Each view supplies its own URL and label shape: `#4021 title` for
work items, `!512 title` for pull requests, and `pipeline #buildNumber` for
builds.

Work items bind two more.

`a` sets the state to Active and reports the outcome on the status line.

`b` runs the branch flow in process:

1. Prompt with a prefilled branch name of the form
   `{type}/{id}-{slug of title}`, for example
   `feature/4021-retry-webhook-delivery`. The name is editable before it is
   used.
2. Choose the target repository: the working directory's repository when there
   is one, otherwise a picker over the project's repositories.
3. Read the head commit of the repository's default branch.
4. Create the ref, posting `oldObjectId` as forty zeroes.
5. Set the work item state to Active.
6. Append an `ArtifactLink` relation pointing at
   `vstfs:///Git/Ref/{projectId}/{repoId}/GB{branch}`, path-segment encoded,
   with `attributes.name` of `Branch`.

Each step reports on the status line. A later step failing leaves the earlier
steps done and says which ones succeeded, rather than reporting the whole flow
as a failure.

On success boardwalk still prints `git fetch origin && git checkout <branch>`
on exit for the shell wrapper to place on the prompt, so the existing zsh widget
keeps working.

The `azdo-branch` shell-out is removed.

## Errors

Fetches run as commands returning either a data message or an error message.
Errors render on the status line in red and the view stays usable with whatever
data it already had. No fetch failure exits the program. Write operations report
per-step outcomes as described above.

## Testing

The existing tests exercise pure functions with no network, and the new work
keeps that shape. Logic lives in pure functions and is tested directly:

- compact age rendering, branch name slugging, `refs/heads/` stripping, and the
  Slack label shapes for all three entity types
- thread counting, covering system-thread exclusion and each status bucket
- timeline to current step and error count, for running, failed, and clean
  builds
- git remote URL to repository name, for both the HTTPS and SSH Azure DevOps
  forms
- artifact link construction, including a branch name containing a slash

The client's new `get` and `patch` paths and its error-body unwrapping are
tested against an `httptest.Server`.

Development is test-first throughout.

## Open questions

None. The branch name convention and the edit-before-create prompt were
confirmed during design.
