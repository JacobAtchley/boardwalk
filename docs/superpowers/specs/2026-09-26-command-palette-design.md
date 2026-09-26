# boardwalk: command palette

A fuzzy-searchable list of everything the current screen can do, opened with
one configurable key, so nobody has to remember forty bindings to use them.

## Goals

- One key (default `ctrl+p`, configurable) opens the palette from the menu or
  any view.
- The palette lists every action the screen on top binds, named by its help
  text and showing the key it stands for, so using the palette teaches the key.
- A fuzzy filter narrows the list as you type; matched characters are
  highlighted.
- Recently run entries are listed first, and survive a restart.
- "go to work items / pull requests / builds" is always offered, from any depth.
- The key bindings and the `?` panel stay exactly as they are.

## Non-goals

- Arguments to actions (a palette entry runs exactly what its key would).
- A second, palette-only set of actions. If a key does not exist, neither does
  the entry.

## Design

### Actions come from the bindings

Every view already returns its bindings from `Keys()`, and the `?` panel is
`FullHelp()` of that map. The palette flattens the same groups, so an action
is in the palette if and only if it is in the panel — the two cannot drift.

Running an entry synthesises the `tea.KeyMsg` of the binding's first key and
feeds it back through `Root.Update`. The view cannot tell the difference, so no
view changes and no action is implemented twice. Bindings are deduplicated by
description, and `?` (the panel) is left out; a disabled binding is skipped.

`keyMsgFor` turns a binding's key string back into a `tea.KeyMsg`. bubbletea
keeps its name table private, so the reverse table is built once, lazily, by
asking every `KeyType` for its name.

### Navigation

A "go to" entry is a `goToMsg{name}`. If the stack's root is already that
view, the stack is cut back to it, keeping its loaded rows and cursor;
otherwise the stack is replaced with a freshly built view.

### History

`Root` depends on a two-method `History` interface (`Recent`, `Record`).
`internal/history.Store` implements it as a most-recent-first, capped, JSON
file beside the config: `~/.config/boardwalk.json` keeps its history in
`~/.config/boardwalk.history.json`. A second config therefore has its own
history. A failed read or write is ignored — history is a convenience, and
must never cost the user the action they asked for.

Entry ids are `nav:<view>` and `act:<description>`. Recent entries are those
history ids present on the current screen, so a recent action from another
view does not appear where it would do nothing.

### Ranking

An empty query lists recent entries, then "go to", then actions. A query is
ranked by `list.DefaultFilter` — the same fuzzy matcher the list views' `/`
filter uses, so no new dependency — and the sort is stable, so recent entries
win ties.

### Keys inside the palette

`↑`/`↓`, `ctrl+j`/`ctrl+k` and `ctrl+n`/`ctrl+p` move, `enter` runs, `esc`
closes, and the palette key itself closes it unless it is a printable
character (which is typed into the query instead). `ctrl+c` still quits.
While a view has a prompt open the palette key is left to the prompt, like
every other Root key.

### Configuration

```json
{ "paletteKey": "ctrl+k" }
```

Validated at startup: an unknown key name, or one of the keys Root cannot give
up (`esc`, `q`, `?`, `ctrl+c`), is an error naming the file.

### Files

Added: `internal/ui/palette.go`, `internal/ui/keymsg.go`,
`internal/history/history.go`, with tests.
Changed: `internal/ui/root.go`, `internal/config/config.go`, `main.go`,
`README.md`.
