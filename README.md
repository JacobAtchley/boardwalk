# boardwalk

A terminal browser for Azure DevOps: work items, pull requests, and pipeline
builds. Fuzzy-find something, then open it, copy its id, copy a Slack-ready
link, or — for a work item — start a branch for it, all without leaving the
shell.

```
 ██████╗  ██████╗  █████╗ ██████╗ ██████╗ ██╗    ██╗ █████╗ ██╗     ██╗  ██╗
 ██╔══██╗██╔═══██╗██╔══██╗██╔══██╗██╔══██╗██║    ██║██╔══██╗██║     ██║ ██╔╝
 ██████╔╝██║   ██║███████║██████╔╝██║  ██║██║ █╗ ██║███████║██║     █████╔╝
 ██╔══██╗██║   ██║██╔══██║██╔══██╗██║  ██║██║███╗██║██╔══██║██║     ██╔═██╗
 ██████╔╝╚██████╔╝██║  ██║██║  ██║██████╔╝╚███╔███╔╝██║  ██║███████╗██║  ██╗
 ╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝  ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝

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

Requires Go 1.24+, the [Azure CLI](https://learn.microsoft.com/cli/azure/), and
a current `az login`.

## Setup

```sh
export AZDO_ORG=my-org
export AZDO_PROJECT=MyProject
```

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
boardwalk -timing      # report fetch duration on stderr
```

### Everywhere

| key | |
|---|---|
| `/` | fuzzy filter |
| `y` | copy the id |
| `s` | copy a Slack message — `[#4021 title](link)` |
| `o` | open in the browser |
| `^u` / `^d` | scroll the detail pane |
| `esc` | back one level (or quit, from a top-level view) |
| `q` | back one level (or quit, from a top-level view) |

### Work items

| key | |
|---|---|
| `^t` | toggle between everyone's items and yours |
| `a` | set the item Active |
| `b` | create a branch, set the item Active, and link the branch to it |

### Pull requests

| key | |
|---|---|
| `d` | cycle drafts hidden → drafts only → all |
| `^t` | toggle between this repository and the whole project |

### Builds

| key | |
|---|---|
| `enter` | open the logs |
| `r` | refetch |

### Logs

| key | |
|---|---|
| `g` / `G` | top / bottom |
| `r` | refetch |

A running build's logs append on their own every few seconds, and stop when the
build finishes.

## Shell integration

A child process cannot change its parent's directory or drive its line editor,
so the branch action prints a command rather than running it. Bind a widget
that puts whatever boardwalk prints onto your prompt:

```zsh
boardwalk-widget() {
  zle -I
  local out
  out=$(boardwalk)
  if [[ -n "$out" ]]; then
    BUFFER="$out"
    CURSOR=${#BUFFER}
    zle accept-line
  else
    zle reset-prompt
  fi
}
zle -N boardwalk-widget
bindkey -M emacs '^xw' boardwalk-widget
```

`^x` is a good prefix because zsh leaves it unbound as a pure prefix — the
chord fires immediately, with no `KEYTIMEOUT` wait and no fallback action to
lose.

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

- A repository picker for the branch flow, for running boardwalk outside a repo
- Search inside the log pane
- Pull request creation from a work item's branch
