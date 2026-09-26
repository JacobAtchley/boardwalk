# Command Palette Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans. Steps use checkbox (`- [x]`) syntax.

**Goal:** A configurable-key command palette listing every action on the current screen, with fuzzy filtering, persisted recents, and "go to" entries for the three main views.

**Spec:** `docs/superpowers/specs/2026-09-26-command-palette-design.md`

## Global Constraints

- No new third-party dependencies. Fuzzy matching is `bubbles/list.DefaultFilter`.
- No view changes: actions are replayed as key messages.
- Test-first; `make test` and `make vet` clean before each commit.

### Task 1: key string → tea.KeyMsg
- [x] Test: every binding key in the package round-trips through `keyMsgFor(...).String()`; unknown names fail.
- [x] Implement `internal/ui/keymsg.go` with a lazily built reverse table.

### Task 2: history store
- [x] Test: record moves to front, dedupes, caps, persists across `Open`, tolerates a missing/corrupt file, memory-only with empty path.
- [x] Implement `internal/history/history.go`.

### Task 3: palette model
- [x] Test: empty query orders recent → go to → actions; query fuzzy-filters; cursor clamps; enter chooses; esc closes; toggle key closes unless printable.
- [x] Implement `internal/ui/palette.go`.

### Task 4: Root integration
- [x] Test: palette key opens from menu and from a view; not while prompting; running an action replays its key; go-to cuts back or rebuilds; history recorded; help panel lists the palette key.
- [x] Implement options `WithPaletteKey`, `WithHistory`, `goToMsg`, rendering.

### Task 5: config and wiring
- [x] Test: `paletteKey` parses; default `ctrl+p`; `HistoryPath` beside config; invalid key rejected.
- [x] Wire in `main.go`; document in README.
