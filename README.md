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

**macOS for now.** The copy and open actions shell out to `pbcopy` and `open`;
nothing else is platform-specific. On a machine without them the status line
says so rather than claiming the copy happened.

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
| `reviewGroups` | teams and security groups you belong to, named as Azure DevOps displays them |

`org` and `project` are required; boardwalk will tell you which is missing, and
show you the shape, if either is absent.

`reviewGroups` is what lets `v` in the pull request view find work assigned to a
group rather than to you. A pull request can list a group as its reviewer, and
nothing in the pull request payload says who is in that group — resolving it
would mean the Graph API, a second host and a walk through nested memberships.
Naming them here costs no requests and works offline, at the price of going
stale when your memberships change.

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
| `^t` | toggle between this repository and the whole project |

The side pane is a summary. `enter` opens the pull request itself: the
description, every reviewer's vote, the work items it is linked to, and every
discussion — whole threads, not just the line each one opens with — unresolved
first. `w` opens the first linked work item. `c` replies to the first
unresolved thread and `R` resolves it, both without leaving the view.

Between them those two close the loop `b` opens: branch from a work item, and
the work item knows about the pull request that branch became, and the pull
request knows which work item it belongs to.

`v` matches a pull request where you are a reviewer and have not voted, and
excludes your own. A pull request can name a group as its reviewer rather than
a person — `platform-devs` rather than you — and nothing in the pull request
says who is in that group, so boardwalk has to be told. See Setup.

### Builds

| key | |
|---|---|
| `enter` | open the logs |

### Logs

| key | |
|---|---|
| `g` / `G` | top / bottom |
| `r` | refresh |
| `y` | copy the build id |
| `o` | open the build in the browser |
| `esc` / `q` | back to the build list |

A running build's logs append on their own every few seconds, and stop when the
build finishes.

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
`pbcopy` is missing or refuses, the command goes on the status line to be read
off instead.

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

**The build log's `startLine` parameter is undocumented as 0- or 1-based.**
Azure DevOps's reference calls it only "the start line." boardwalk treats it
as a 0-based offset equal to the number of lines already consumed on each
tail poll. If it turns out to be 1-based instead, every poll of a running
build re-shows one line that was already displayed — watch for that when
tailing a live build.

## Roadmap

Features:

- A repository picker for the branch flow, for running boardwalk outside a repo
- Search inside the log pane
- Pull request creation from a work item's branch
- Caching views in the menu, so re-entering one does not refetch the project
- Queue, re-run and cancel a build from the builds view
- `--json` output, so a dump can be piped into something else
- A watch mode that polls for pull requests newly waiting on you
- Resolving review groups through the Graph API instead of naming them in the config

Known rough edges, none of them load-bearing:

- `Comments()` reads one page, so a very long discussion is silently truncated.
  The endpoint supports a `continuationToken`.
- A build row whose timeline fetch failed retries every few seconds for as long
  as a log pane is tailing, because the hidden build list still receives the
  tail's messages.
- A refresh that fails while you are inside a log pane reports nothing: the
  error is delivered to the pane on top and dropped, and the build list keeps
  reading `refreshing…`.
- A row refresh that lands while the branch prompt is open rebuilds the list
  underneath it. If the server's order changed, the branch could be named for
  one work item and attached to another.
- Lazily arriving rows scroll the detail pane back to the top.
- A copy or open failure is reported, but the raw `exec` error is not always
  the clearest thing to read.

## License

MIT License. See the LICENSE file for full text.
