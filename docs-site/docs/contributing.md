# Contributing

DevDesk takes pull requests. `main` is not pushed to directly — the
project's mirror rejects it — so every change goes through a branch and a
PR.

## Setup

```bash
git clone https://github.com/anthnel/devdesk.git
cd devdesk
go mod download
```

## Workflow

```bash
git switch -c my-feature
# make changes
mise run check       # fmt + vet + lint + test — run this before committing
git add -A && git commit -m "..."
git push origin my-feature
gh pr create --base main --head my-feature
```

`mise run check` is the pre-commit checklist in one command: `go fmt`,
`go vet`, `golangci-lint run`, `go test ./...`. A PR with a `golangci-lint`
warning or a failing test doesn't get merged — fix locally first.

## Commands

```bash
mise run dev        # go run . — fastest iteration loop
mise run build      # → bin/dk
mise run test       # go test ./...
mise run test-race  # race detector — needs a C toolchain (gcc/clang) for cgo
mise run cover      # coverage per package
mise run lint       # golangci-lint, mandatory before committing
mise run tidy       # go mod tidy
```

`mise tasks` lists every task with its description.

## Code conventions

- **Never modify model state inside a `tea.Cmd`.** Bubble Tea's `Update()`,
  `View()` and `Cmd`s run concurrently; only `Update()` may mutate the
  model. A `Cmd` does I/O and returns a message.
- **Never call `style.Render()` inside a `table.Row{}`.** ANSI escape
  sequences break the table's width measurement and corrupt every row after
  the truncated one. Rows stay plain text; styling goes through
  `table.SetStyles()`.
- **All UI text and log messages are English US.** Code comments may be
  French or English.
- **A closed set of values uses the `←/→` cycle pattern**, never a dropdown
  and never `Enter` to cycle.
- **Background color isn't inherited** by `lipgloss.JoinHorizontal/Vertical`
  — use the `theme.PadWithBg()` / `theme.BgLine()` / `theme.EmptyLineBg()`
  helpers instead of assembling layout by hand.
- **A `datatable`'s body is always the table itself** — loading, empty, or
  filtered down to nothing, it's still a table with a header and zero rows,
  never a spinner or a message swapped in for the body.
- **Message naming**: `[ComponentName][Action]Msg`, e.g. `ScanCompleteMsg`.
- **`Update()` cases over 5 lines** get extracted into a
  `handle[MessageType]()` method returning `(tea.Model, tea.Cmd)`.

The complete, current rule set — including the reasoning behind each one —
lives in `.claude/rules/` in the repository; it's what both human
contributors and the project's coding agent follow.

## Adding a new view

1. `internal/ui/<viewname>/model.go` with a `Model` implementing `tea.Model`
2. A `ViewType` constant in `internal/command/parser.go`
3. Register it in `internal/app/app.go` (lazy-load, same pattern as existing views)
4. Implement `GetHelpContent()` (the `?` key needs it)
5. Implement `InEditMode() bool` if the view has forms, so command mode
   doesn't hijack a keystroke meant for a field
6. Follow the keybinding vocabulary — see [Keybindings](reference/keybindings.md)

## Testing

```bash
go test ./...                        # everything
go test -v ./internal/status/...     # one package, verbose
go test -race ./...                  # race detector
go test -run TestParseCommand ./...  # one test by name
```

Table-driven tests are the convention; `*_test.go` files sit next to the
code they test.

## Architecture docs

`docs/architecture/*.md` in the repository — one file per subsystem — is
where a change's rationale should land in the same commit as the code, if
it changes the "why" and not just the "what". This site's
[Explanation](explanation/index.md) section is adapted from those files for
readers outside the project; the repository copies are the ones kept in
sync with each PR.
