# boardwalk

A terminal browser for Azure DevOps: work items, pull requests, and pipeline
builds. Fuzzy-find something, then open it, copy its id, copy a Slack-ready
link, or — for a work item — start a branch for it, all without leaving the
shell.

```
  ╔╗ ╔═╗╔═╗╦═╗╔╦╗╦ ╦╔═╗╦  ╦╔═
  ╠╩╗║ ║╠═╣╠╦╝ ║║║║║╠═╣║  ╠╩╗
  ╚═╝╚═╝╩ ╩╩╚══╩╝╚╩╝╩ ╩╩═╝╩ ╩
 ═══╬══════╬══════╬══════╬════
    ║      ║      ║      ║
 ≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈

▸ work items       browse, branch and set state
  pull requests    drafts, branches, comments and age
  builds           pipeline runs, current step and logs
  status           what boardwalk resolved about this session

acme/Platform · ↑↓ move · enter open · q quit
```

## Install

```sh
git clone https://github.com/JacobAtchley/boardwalk.git
cd boardwalk
make install          # builds and drops the binary in ~/.local/bin
```

Requires Go 1.27+, the [Azure CLI](https://learn.microsoft.com/cli/azure/), and
a current `az login`.

**macOS, Windows and Linux.** The copy and open actions are the only part that
knows what it is running on, and they shell out to whatever the platform has:

| | copy | open |
|---|---|---|
| macOS | `pbcopy` | `open` |
| Windows | `clip.exe` | `rundll32 url.dll,FileProtocolHandler` |
| Linux and the rest | `wl-copy`, `xclip` or `xsel` | `xdg-open` |
| WSL | `clip.exe` — behind the Linux tools when there is a display | `wslview`, then `xdg-open` |

Nothing else is platform-specific. When none of the candidates is on `PATH` the
status line names the ones it looked for, rather than claiming the copy
happened.

On Linux the order is read off the session rather than fixed: `wl-copy` is
first under Wayland and last under X11, since being installed is not the same
as working — `wl-copy` on an X11 desktop fails on a machine where `xclip`
copies fine.

## Setup

Everything boardwalk needs to know lives in one file at
`~/.config/boardwalk.json`:

```json
{
  "org": "my-org",
  "project": "MyProject",
  "reviewGroups": ["platform-devs"]
}
```

| key | |
|---|---|
| `org` | the Azure DevOps organisation — the first path segment of `https://dev.azure.com/{org}` |
| `project` | the team project within it |
| `reviewGroups` | optional — extra teams and security groups to treat as yours, named as Azure DevOps displays them |

Azure DevOps scopes a group's display name to wherever it lives —
`[TEAM FOUNDATION]\platform-devs` for a collection group, `[MyProject]\Team
Name` for a project one. Name the group on its own (`platform-devs`) and the
scope is ignored; write the scope out and it has to match, which is how two
projects can each have a `developers` and only one of them be yours.

`org` and `project` are required; boardwalk will tell you which is missing, and
show you the shape, if either is absent.

`reviewGroups` is optional. What it affects is `v` in the pull request view
finding work assigned to a group rather than to you, and your being able to
vote on a pull request only a group of yours is reviewing — and boardwalk
works that out for itself at startup, through the Identities API, expanding
your identity through however many levels of nested group it takes.

Name a group here and it is honoured on top of whatever that found. That is
worth doing in two cases: your token cannot read your organisation's
identities, so nothing was resolved at all; or you want a group you are not formally a member
of to count as yours anyway. It costs no requests and works offline, at the
price of going stale when your memberships change.

Set `BOARDWALK_CONFIG` to read the file from somewhere else, which is how to
keep more than one — a second organisation, or a project you only visit
occasionally:

```sh
BOARDWALK_CONFIG=~/.config/boardwalk.other.json boardwalk
```

`XDG_CONFIG_HOME` is honoured if you set it. The default is spelled `~/.config`
on every platform rather than following the OS convention, which on macOS would
put it under `~/Library/Application Support`.

boardwalk implements no auth flow of its own. It asks the az CLI for a token
against the Azure DevOps resource id and rides whatever session `az login`
already established, so there is nothing extra to configure or store.

## Usage

```sh
boardwalk              # the menu
boardwalk items        # straight to work items
boardwalk prs          # straight to pull requests
boardwalk builds       # straight to pipeline builds
boardwalk -mine        # work items assigned to you
boardwalk -all         # include closed/done/resolved/removed
boardwalk -dump        # print work item rows and exit — for scripts and pipes
boardwalk -timing      # report the -dump fetch duration on stderr
```

A subcommand comes first, before any flags: `boardwalk items -mine`, not
`boardwalk -mine items`. The second form is an error rather than a silent
misreading, since the flag package stops at the first non-flag word.

Every view fetches when it is opened, so the menu paints immediately and a
fetch that fails lands on the status line with `r` to try again, rather than
taking the program down with it.

### Every list view

Work items, pull requests and builds share these. The log pane is a pager
rather than a list and binds only its own keys, below.

| key | |
|---|---|
| `/` | fuzzy filter |
| `y` | copy the id |
| `s` | copy a Slack message — `[#4021 title](link)` |
| `o` | open in the browser |
| `r` | refresh |
| `^u` / `^d` | scroll the detail pane |
| `?` | show every binding for the current view |
| `esc` | back to the menu |
| `q` | quit |

The footer lists the keys that fit on one line; `?` opens the rest. The
bindings and the footer come from the same declarations, so a key that works
is a key that is listed.

`esc` never quits: it pops one level, and from a top-level view that is the
menu. `q` quits from a top-level view and goes back from a drill-down. `^c`
quits from anywhere, including while a prompt is open.

A view that comes back with nothing says so over a pair of gulls, and names the
filter that emptied it — "nothing waiting on your review" rather than a blank
list that reads as a fetch that broke:

```
        ⌒v⌒            ⌒v⌒
   ≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈

      nothing waiting on your review
```

### Work items

| key | |
|---|---|
| `enter` | open the full item |
| `^t` | toggle between everyone's items and yours |
| `a` | set the item Active |
| `b` | create a branch, set the item Active, link the branch to it, and copy the checkout |

The side pane is a summary — id, title, assignee, tags and iteration — so it
stays readable while the cursor moves. `enter` opens the item itself: every
field, the pull requests it is linked to, and the whole discussion, with the
description, acceptance criteria and comments rendered as markdown rather than
flattened to prose. `p` opens the first of those pull requests, and they are
listed newest first.

### Pull requests

| key | |
|---|---|
| `enter` | open the full pull request |
| `v` | only pull requests waiting on your review |
| `d` | cycle drafts hidden → drafts only → all |
| `P` | publish a draft, or put a published pull request back into draft |
| `^t` | toggle between this repository and the whole project |

`P` asks before it acts: the first press arms the toggle and says what the
second one will do, `esc` cancels. Publishing notifies every reviewer on the
pull request, which is not something a mistyped key should be able to do. It
works from the list and from the pull request itself. A merged or abandoned
pull request cannot be toggled at all, so the key is neither offered nor armed
on one; whether *you* may toggle an open one is Azure DevOps's call, and a
refusal comes back on the status line in its own words.

The side pane is a summary. `enter` opens the pull request itself: the
description, every reviewer's vote, the work items it is linked to, and every
discussion — whole threads, not just the line each one opens with — unresolved
first. `w` opens the first linked work item. `c` replies to the first
unresolved thread and `R` resolves it, both without leaving the view. `D` opens
the diff.

`A` approves, `W` waits for the author and `X` rejects, each on a second press
of the same key. They show when the pull request is yours to vote on — named
directly, or through a group of yours, whether it was resolved at startup or
named in `reviewGroups`. Voting on a group's
behalf casts the vote under your own identity, which boardwalk reads once at
startup from `connectionData`; Azure DevOps then adds you as a reviewer in your
own right and leaves the group entry alone, exactly as the web UI does.

### Diff

| key | |
|---|---|
| `D` | from a pull request, open its diff |
| `^u` / `^d` | scroll the file's diff |
| `enter` | open the whole file |

The changed files are the list and the selected file's diff is the pane beside
it, as a unified diff: additions green, deletions red, three lines of context.
Files are read one at a time as the cursor reaches them, so a pull request
touching two hundred files still opens at once.

A binary file, or one over 256 KiB, says what it is instead of being rendered.
A renamed file reads as a new one: the change entry names only where the file
landed, so its previous content is at a path boardwalk cannot ask for.

`enter` opens the selected file whole — the change marked in the gutter
rather than colouring the code, so it reads as source. `d` takes the marks
away and leaves the file. Syntax highlighting comes from the file's name, and
a language boardwalk has no lexer for is shown plainly rather than guessed
at. Lines the pull request removed sit where they were, dimmed, and go away
with `d`.

`c` writes a review comment against the line the cursor is on, and it appears
under that line as soon as the server takes it. A line the pull request
removed is not in the file any more, so there is nothing to attach a comment
to and `c` says so rather than failing at the request. Replying to an
existing thread and resolving one stay on the pull request pane behind `esc`.

Review comments sit under the line they were written against, marked resolved
or unresolved, and a file with discussion on it carries the count in the list
beside its name. A comment whose line the diff's three lines of context never
reach — or one written against the file rather than a line of it — is
collected under "elsewhere in this file" rather than dropped. Replying and
resolving stay on the detail pane behind `esc`; this one is for reading.

Between them those two close the loop `b` opens: branch from a work item, and
the work item knows about the pull request that branch became, and the pull
request knows which work item it belongs to.

`v` matches a pull request where you are a reviewer and have not voted, and
excludes your own. A pull request can name a group as its reviewer rather than
a person — `platform-devs` rather than you — and nothing in the pull request
says who is in that group. boardwalk works that out at startup through the
Identities API, expanding your identity to every group it transitively
belongs to, so a team you were added to this morning counts this morning.

`reviewGroups` in the config is still read, as an override on top of that: a
name written there is honoured whatever was or was not resolved, and is the
only source at all if your token cannot read your organisation's identities.
A tenant where that is locked down behaves exactly as boardwalk did before
any of this. See Setup.

### File

| key | |
|---|---|
| `j` / `k` | move the line cursor |
| `c` | comment on the line under the cursor |
| `g` / `G` | top / bottom |
| `d` | show / hide the change marks |

### Session

| key | |
|---|---|
| `r` | resolve it all again |

`session` on the menu — or `boardwalk status` — shows what boardwalk worked
out at startup: the organisation and project, which config file it read, who
`az` says you are, the identity GUID it votes with, and every review group it
resolved you into, alongside whatever `reviewGroups` adds.

It is there because all of that used to be invisible and several parts of it
fail quietly. A token that cannot read your organisation's identities, an
`az account show`
that named no user, a working directory that is not one of the project's
repositories — each leaves boardwalk running and apparently fine, and each
turns up somewhere else as a list with nothing in it.

So the screen says what each gap costs, not just that it exists: an unknown
user reads as "nothing reads as yours — the mine scope is empty and `v`
matches no pull request at all". It also tells "you are in no groups" apart
from "your groups could not be resolved", which look identical everywhere
else in the program.

`r` re-runs both lookups, so an `az login` renewed in another shell is picked
up without restarting.

### Builds

| key | |
|---|---|
| `enter` | open the logs |
| `p` | the pull request this run built |
| `Q` | re-run — this pipeline, this branch |
| `N` | new run — asks which branch |
| `C` | cancel a run that has not finished |

`Q` and `C` arm on the first press and fire on the second, the way the draft
toggle does: they start and stop work on a shared build pool, and a stray
keystroke cannot be taken back by pressing the key again. `esc` disarms, and
pressing the other one re-arms to that instead. `N` asks for a branch first,
prefilled with the selected run's, and that prompt is the deliberate act the
arm would otherwise be.

Capitals because the lower-case letters are taken in views reachable from
here: `r` refreshes everywhere, `c` replies on a pull request the build list
links to.

### Logs

| key | |
|---|---|
| `g` / `G` | top / bottom |
| `t` | show / hide timestamps |
| `r` | refresh |
| `y` | copy the build id |
| `o` | open the build in the browser |
| `esc` / `q` | back to the build list |

A running build's logs append on their own every few seconds, and stop when the
build finishes.

Azure Pipelines marks up its own logs, and the pane reads that markup rather
than guessing: sections and group headings stand out, commands are set apart
from their output, warnings are amber and errors red, and `##[debug]` lines
recede. Colour the build tools emitted themselves is left alone. The timestamp
every line carries is hidden by default — it is the same twenty-eight columns
on every row — and `t` brings it back when you are timing something.

## The branch flow

`b` on a work item does the whole thing against the REST API — creates the
branch off the repository's default branch, moves the item to Active, and links
the branch to the item — then puts the checkout on your clipboard:

```
git fetch origin && git checkout feature/4021-retry-webhook-delivery-on-5xx && git pull
```

boardwalk stays open. Open another shell, go to the repository, and paste. A
child process cannot move its parent's working tree, so the checkout has to
happen in a shell you are in — but that is no reason to lose the session you
were in the middle of.

The command is copied as soon as the branch exists on the server, even if the
state change or the link then fails, since the branch is there either way. If
the clipboard command is missing or refuses, the command goes on the status
line to be read off instead.

The repository is usually the one you are standing in: boardwalk reads the
`origin` remote and matches it against the project's repositories. Run it
somewhere else — outside a checkout, or in a repository belonging to somewhere
else — and it asks instead, one repository at a time on the status line, `j`
and `k` to move and `enter` to pick.

## Notes from building it

**`az boards query` caps at 1000 work items.** Going through the REST API
directly returns the whole project; on the board this was built against that
was 3931 open items, so the CLI had been hiding three quarters of them with no
warning.

**It is faster despite fetching far more.** WIQL returns only ids, so the
fields come from a second pass against `workitemsbatch`, 200 ids per request,
eight requests in flight. Roughly 2.9s for ~3900 items, against 5.2s for the
az CLI's truncated 1000.

**`workitemsbatch` ignores the order ids are sent in** and answers ascending,
so the query's `ORDER BY [System.Id] DESC` has to be reapplied client-side.

**Graph's `memberships?direction=up` omits Azure AD groups.** Asking Graph
which groups a user belongs to returns only `vssgp.` containers — Azure
DevOps's own groups — and silently leaves out the `aadgp.` ones an
organisation federates in from Entra. The membership is genuinely there:
asking for the edge directly answers 200, and asking the group who it
contains lists the user. Only the upward listing drops it, and it reports
success while doing so.

That is why review groups are resolved through the Identities API
(`identities?queryMembership=Expanded`) instead. On the tenant this was found
against, the Graph walk resolved 35 groups and not one of the ones actually
used as pull request reviewers; the identity expansion returns all 74. It is
also five requests at startup rather than about seventy.

**A CRLF file leaves a carriage return on every line.** Splitting a file on
`\n` keeps the `\r`, and a terminal reading one returns the cursor to column
zero — so the padding written after the text would overwrite the text.
Bubbles strips control characters before it paints, which made this invisible
rather than broken, but the pane's correctness should not rest on that. The
file view drops the carriage return where it splits the lines.

**The build log's `startLine` parameter is undocumented as 0- or 1-based.**
Azure DevOps's reference calls it only "the start line." boardwalk treats it
as a 0-based offset equal to the number of lines already consumed on each
tail poll. If it turns out to be 1-based instead, every poll of a running
build re-shows one line that was already displayed — watch for that when
tailing a live build.

## Roadmap

Features:

- Search inside the log pane
- Pull request creation from a work item's branch
- Caching views in the menu, so re-entering one does not refetch the project
- `--json` output, so a dump can be piped into something else
- A watch mode that polls for pull requests newly waiting on you

Known rough edges, none of them load-bearing:

- `Comments()` reads one page, so a very long discussion is silently truncated.
  The endpoint supports a `continuationToken`.
- `IterationChanges()` has the same gap: a pull request touching more files than
  the server returns in one page is listed short, with nothing on screen saying
  so.
- A build row whose timeline fetch failed retries every few seconds for as long
  as a log pane is tailing, because the hidden build list still receives the
  tail's messages.
- A row refresh that lands while the branch prompt is open rebuilds the list
  underneath it. If the server's order changed, the branch could be named for
  one work item and attached to another.
- Lazily arriving rows scroll the detail pane back to the top. The diff view is
  the exception — it re-renders only the pane whose file just landed.
- A copy or open failure is reported, but the raw `exec` error is not always
  the clearest thing to read.
- The state picker draws every state on one row, so a work item type with an
  unusually long workflow runs off the edge of a narrow terminal.
- Straight after assigning an item to yourself the row shows your email rather
  than your name. `az account show` does not report a display name, so that is
  the best available until the next fetch replaces it.
- `stateSetMsg`, `assigneeSetMsg` and `draftSetMsg` report success without
  checking the row is still listed. The row sync itself is keyed by id and
  safely does nothing, but the status line can claim a change to something no
  longer on screen.
- The pull request list hands its cached threads to the detail view, which
  writes to them in place. Resolving a thread and reopening the pull request
  shows it resolved while the list row's own counts still say otherwise.
- Leaving the log pane with `esc` or `q` does not clear the builds view's
  hidden-view guard, because `Root` consumes both keys before any view sees
  them. An in-flight `p` lookup landing in that gap is dropped; pressing `p`
  again re-runs it.

Internal cleanups, invisible from outside but worth doing:

- The three single-line prompts — branch name, pull request reply, work item
  comment — are near-identical, and more to the point the rule that every modal
  must be reported by `Prompting()` is currently kept by hand in each of the six
  views, which between them now carry seven kinds of modal state: the three
  prompts, the fuzzy filter, the state picker, an armed vote and an armed draft
  toggle. Extracting one prompt type, and one armed-confirm type, would make
  that rule structural instead of remembered.
- `SharedAction` runs before a view's own key switch in the list views and after
  it in the item and pull request detail views. Nothing collides today, which is
  luck rather than design.
- Re-arming a vote to a different value repeats the reviewer-id lookup it
  already did. It costs nothing but a little work in memory.
- The comment above the diff view's size cap says the cost is paid before the
  hunks exist. Both sides are in fact fully fetched and decoded before the cap is
  checked, so it bounds the diff and the render, not the download.
- `selectedThread`'s comment says `renderThreads` sorts unresolved threads to the
  top. It partitions them across two render passes; the conclusion holds but the
  wording does not describe the code.

Asserted but never seen against a live tenant. Each is tested against
`httptest`, which proves boardwalk sends what it means to send and nothing about
what Azure DevOps does with it:

- Whether `PUT .../reviewers/{id}` accepts a body carrying only `{"vote": n}`.
  **Worth smoke-testing one approve against a real pull request before relying
  on the vote keys** — it is the one of these that fails closest to something
  destructive.
- Whether a binary blob's `content` comes back raw or base64-encoded. If it is
  base64 the NUL-byte check never fires and a binary renders as base64 text
  rather than being declined. It carries no escape bytes either way, so the
  reason the check exists still holds.
- Whether an empty `System.AssignedTo` unassigns rather than failing validation.
  Unreachable in normal use: both call sites refuse to send an empty assignee.
- Whether every identity provider puts the same GUID on a reviewer entry as
  the Identities API reports for the group. Against the one live tenant this
  has been run on they match exactly, for both an Azure DevOps group and an
  Azure AD one, so the id is what fires; `Groups.Has` also accepts the display
  name, which has not been needed there.

## License

MIT License. See the LICENSE file for full text.
