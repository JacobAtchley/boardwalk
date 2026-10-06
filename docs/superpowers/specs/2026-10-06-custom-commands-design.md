# boardwalk: custom commands

Bind a key on a screen to a shell command of your own, run with what that
screen is showing — so "review this pull request with Claude in a new herdr
tab" is one keypress away, without boardwalk knowing anything about herdr or
Claude.

## Goals

- The config file maps a single key, on a kind of screen, to a shell command.
- The command learns what the screen is showing through `BOARDWALK_*`
  environment variables.
- Commands run detached: boardwalk stays on screen and reports the outcome on
  the status line.
- Custom commands appear in the `?` panel and the command palette like any
  built-in binding.
- A custom key can never shadow a built-in one.

## Non-goals (v1)

- Template substitution in the command string (`{{.ID}}`). Possible later; it
  would need every value shell-quoted.
- Running in the foreground (suspending boardwalk for `vim`, `lazygit`).
- Multi-key sequences (`ctrl+x c`).
- A global scope with no subject.
- Action types other than a shell command.
- Reloading the config without a restart.

## Motivating example

On a pull request, `ctrl+r` opens a new herdr tab in the current workspace and
starts a Claude Code session running a PR review skill for that pull request.
The herdr and Claude details live in the user's script; boardwalk supplies the
key, the pull request, and the environment herdr's own variables pass through
in.

## Config

`~/.config/boardwalk.json` gains a `commands` list:

```json
{
  "org": "my-org",
  "project": "MyProject",
  "commands": [
    {
      "on": "pullRequest",
      "key": "ctrl+r",
      "name": "review with claude",
      "run": "~/bin/pr-review.sh"
    }
  ]
}
```

| field  | meaning |
|--------|---------|
| `on`   | the kind of screen: `pullRequest`, `workItem` or `build` |
| `key`  | one key, spelled as bubbletea names it — the same spelling as `paletteKey` |
| `name` | the help text in `?`, the palette and the status line |
| `run`  | a shell command, run with `sh -c` |

There is no `type` field. A later action kind gets a field of its own beside
`run` (for example `open`), and a command sets exactly one of them.

### Validation at startup

Loading fails, naming the config file, when a command:

- has an `on` that is not one of the three kinds;
- has a `key` that `keyMsgFor` cannot parse;
- has a `key` in `reservedKeys` (`esc`, `q`, `?`, `ctrl+c`, `enter`, `up`,
  `down`, `j`, `k`) or equal to the palette key;
- shares its `on` and `key` with another command;
- has an empty `name` or `run`.

### Collisions with a screen's own keys

Which keys a view binds is only known once the view exists, so a custom key
that a screen already uses cannot be caught at startup. The built-in binding
always wins: on that screen the custom command is left out of `?` and the
palette, and pressing the key does what it always did. A user therefore cannot
shadow approve, reject or any other built-in act by accident.

The collision check matches the custom key against every key of every binding
in the top view's `Keys().FullHelp()`, plus the keys `SharedAction` claims on
list views (`y`, `ctrl+y`, `s`, `ctrl+s`, `o`, `ctrl+o`) — the same set of
bindings the palette is built from.

## Environment

The command inherits boardwalk's environment, with these added:

| variable | set for | value |
|----------|---------|-------|
| `BOARDWALK_KIND` | all | `pullRequest`, `workItem` or `build` |
| `BOARDWALK_ORG` | all | the config's `org` |
| `BOARDWALK_PROJECT` | all | the config's `project` |
| `BOARDWALK_ID` | all | the pull request, work item or build id |
| `BOARDWALK_TITLE` | all | its title (a build's is its pipeline and number) |
| `BOARDWALK_URL` | all | its web URL |
| `BOARDWALK_REPO` | pullRequest | repository name |
| `BOARDWALK_SOURCE_BRANCH` | pullRequest | source branch, `refs/heads/` stripped |
| `BOARDWALK_TARGET_BRANCH` | pullRequest | target branch, `refs/heads/` stripped |
| `BOARDWALK_AUTHOR` | pullRequest | author display name |
| `BOARDWALK_IS_DRAFT` | pullRequest | `true` or `false` |
| `BOARDWALK_TYPE` | workItem | work item type, e.g. `Bug` |
| `BOARDWALK_STATE` | workItem | its state |
| `BOARDWALK_NUMBER` | build | the build number |
| `BOARDWALK_PIPELINE` | build | the pipeline name |
| `BOARDWALK_BRANCH` | build | the branch built, `refs/heads/` stripped |
| `BOARDWALK_RESULT` | build | its result, or its status while running |

Values are passed as environment variables rather than pasted into the command
string because titles, branch names and descriptions are written by other
people. A title of `fix $(rm -rf ~)` is inert in `"$BOARDWALK_TITLE"`; pasted
into shell source it would run.

## Architecture

### Subjects

Views gain one optional interface:

```go
// subjecter is a view showing one thing a custom command can act on.
type subjecter interface {
	Subject() (kind string, env map[string]string, ok bool)
}
```

`env` holds the kind-specific variables without their `BOARDWALK_` prefix;
Root adds the prefix and the variables common to every kind. `ok` is false
when there is nothing to act on — an empty list, or a detail view whose
subject has not loaded.

| kind | views |
|------|-------|
| `pullRequest` | `PullRequests` (selected row), `PullRequestDetail`, `PullRequestDiff`, `FileView` |
| `workItem` | `WorkItems` (selected row), `Item` |
| `build` | `Builds` (selected row), `Logs` |

The menu, `CreateBranch` and `Status` have no subject, and a custom key does
nothing there.

### Dispatch

Root already sees every key before the view on top does. A custom command
runs when all of these hold:

1. the palette is closed and the top view is not `Prompting()` — so the key
   typed into a reply box or a filter is text, not a command;
2. the top view is a `subjecter`;
3. a command's `on` matches the view's kind and its `key` matches the press;
4. the key is not one the view binds itself.

If the view's `Subject` returns `ok=false`, the status line reads
`nothing selected` and nothing runs.

### Help and palette

Root wraps the top view's `Keys()` the way `paletteKeys` adds the palette
key, appending a group of the custom commands active on that screen. The `?`
panel shows them; the palette, built from that panel, lists them, and running
one from the palette replays its key through `Root.Update` like any other
entry. Palette history ids follow the existing `act:<description>` scheme.

### Running

A new `internal/ui/custom.go`. The runner sits behind a package variable, as
`copyToClipboard` and `openBrowser` do, so tests can stub it.

- `exec.Command("sh", "-c", run)`, environment `os.Environ()` plus the
  `BOARDWALK_*` variables, working directory boardwalk's own.
- stdin is `/dev/null`, stdout is discarded, stderr goes to a capped buffer.
  Nothing the command prints may reach the terminal bubbletea is drawing.
- `SysProcAttr{Setpgid: true}`, so a `ctrl+c` that quits boardwalk is not also
  delivered to the command, and the command outlives boardwalk.
- The run is a `tea.Cmd` that waits up to two seconds, then returns a
  `StatusMsg`:
  - exited 0: `ran "<name>"`;
  - exited non-zero: an error, `"<name>" failed: <last stderr line>`, or the
    exit code when stderr is empty;
  - still running: `started "<name>"`; a goroutine waits on it so it does not
    become a zombie;
  - could not start: an error naming the reason.

## Wiring

- `config.Config` gains `Commands []Command`. `Validate` covers the rules
  that need nothing from the UI: a known `on`, a non-empty `name` and `run`,
  no duplicate `on` + `key`. The key rules — parseable, not reserved, not the
  palette key — need `keyMsgFor` and `reservedKeys`, so they live in
  `ui.CheckCommands(cmds, paletteKey)`, which `main` calls beside
  `CheckPaletteKey`. `config` keeps not importing `ui`.
- `ui.WithCommands([]config.Command)` hands them to Root, alongside
  `WithPaletteKey` and `WithHistory`. Root also needs the org and project for
  the common variables.
- `config.Example` stays as it is; the README documents `commands`.

## Documentation

A README section: the config shape, the environment table, the collision and
detachment rules, and the herdr example as a sample script.

## Testing

Test-first, following the existing table-driven style.

- **config**: parsing `commands`; each validation error, with the file named.
- **subjects**: for each view, the kind and variables for a known subject;
  `refs/heads/` stripped; an empty list gives `ok=false`.
- **Root**:
  - the key runs the stubbed runner with the right command and environment;
  - a built-in key on that screen wins and the command is absent from help;
  - nothing runs while the view is prompting;
  - a key bound for another kind does nothing;
  - `ok=false` reports `nothing selected`;
  - the command appears in the `?` panel and the palette, and choosing it
    from the palette runs it.
- **runner**, against a real `sh` with a short wait window:
  - the command sees the `BOARDWALK_*` variables (it writes `env` to a temp
    file);
  - exit 0, non-zero with stderr, non-zero without, and still-running each
    give the status described above.
