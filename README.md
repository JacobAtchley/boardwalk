# boardwalk

A terminal browser for Azure DevOps boards. Fuzzy-find a work item, then open
it, copy its id, copy a Slack-ready link, or start a branch for it — without
leaving the shell.

```
work items (all 3) · acme/Platform
▸ 4021    [User Story]   Active           Dev Example        Retry webh…│  #4021  Retry webhook delivery on 5xx
  4020    [Enhancement]  Needs Refinement (unassigned)       Tidy up th…│
  3998    [Feature]      Pending QA       Dev Example        Cache tena…│  type:      User Story
                                                                       │  state:     Active
                                                                       │  assigned:  Dev Example
                                                                       │  tags:      webhooks, reliability
                                                                       │  iteration: Platform\Sprint 42
                                                                       │
                                                                       │  Deliveries that fail with a 5xx should
                                                                       │  retry with exponential backoff rather than
                                                                       │  dropping on the floor.
^t mine/all · / filter · o open · y copy id · s slack · b branch · q quit
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
boardwalk              # every open work item in the project
boardwalk -mine        # start filtered to items assigned to you
boardwalk -all         # include closed/done/resolved/removed
boardwalk -dump        # print rows and exit, no TUI — for scripts and pipes
boardwalk -timing      # report fetch duration on stderr
```

| key | |
|---|---|
| `/` | fuzzy filter across id, type, state, assignee and title |
| `^t` | toggle between everyone's items and yours |
| `o` | open in the browser |
| `y` | copy the work item id |
| `s` | copy a Slack message — `[#4021 title](link)` |
| `b` | hand `azdo-branch <id>` to the shell |
| `q` | quit |

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

## Roadmap

- Pull requests and pipeline builds, as sibling views
- Branch creation in-process, rather than shelling out to `azdo-branch`
- `boardwalk` as a multi-command binary (`boardwalk prs`, `boardwalk builds`)
