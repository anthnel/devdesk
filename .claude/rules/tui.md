# TUI — Rule index

TUI rules are split across 5 topic files:

| File | Rules | Content |
|---------|--------|---------|
| [`tui-layout.md`](tui-layout.md) | 101, 107, 108, 111, 112, 123, 124, 130, 134, 137, 138 | General layout, keybindings, viewport, tabs, shortcuts header, format descriptions, shortcuts obviousness |
| [`tui-theme.md`](tui-theme.md) | 102, 105, 115, 117, 118, 119, 127, 129 | Colors, lipgloss background, `theme.*` helpers, language |
| [`tui-forms.md`](tui-forms.md) | 103, 104, 113, 114, 120, 121, 131, 132, 133, 135, 140 | Forms, modals, focus, status icons, value cycling, WrappedInput, key assignment, invalid field |
| [`tui-tables.md`](tui-tables.md) | 106, 116, 122, 125, 136, 139 | Tables, column widths, plain-text cells + color via `Style`, filter bar, body never replaced (spinner or message) |
| [`tui-behavior.md`](tui-behavior.md) | 109, 110, 126, 128 | Bubble Tea (Cmd/Update), scan cache, footer messages (three levels, one component) |

## Critical rules (summary)

**Rule 110** — NEVER modify the model inside a `Cmd` → race condition. Only `Update()` modifies the model.

**Rule 122** — `Cell` returns plain text (it gets measured), color goes through `Style`. A `style.Render(...)` inside `Cell` gets truncated in the middle of its escape sequence and bleeds onto every following row.

**Rule 128** — A footer message goes through `components.FooterMessage`: three levels (neutral info, orange warning, red error), always centered, a single implementation. A table's loading state is shown in the footer, never in the body: with `Spinner` + `SetSpinnerFrame` for a view's own loading, with the shared activity dot (`Status.Activity`, `●`/`○`, `SetBlinkPhase`) for work the router tracks. A new `Activity` value not driven by the jobs registry must implement `WantsBlink()`, or the dot stays fixed.

**Rule 139** — A `datatable`'s body is always `m.table.View()`, never a spinner or a message: an empty table (loading, nothing to list, a filter with no results) stays a table — a header, no rows. The count goes in `GetHeaderInfo`, the status in the footer.

**Rule 129** — All UI text and all logs in **English US**.

**Rule 140** — An invalid form field is underlined through `theme.StyleInvalidField` (the only entry point to the underline API), and the error is also in the footer at level Error, derived on every render. The underline is never the only carrier.

**Rule 133** — `bubbles/textarea` is forbidden. Use `components.WrappedInput` for any multi-line text field.

The per-topic TUI rule files above load when a matching file is read or edited; when reviewing a diff without opening files, read them explicitly.
