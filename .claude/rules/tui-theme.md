# TUI — Theme, Colors & Background

### Rule 102 : Centralized management of colors and styles

- All colors must be defined in `internal/ui/theme/colors.go`
- Never hardcode colors directly in views
- Reusable styles must be defined in `internal/ui/theme/styles.go`
- Use the theme package's constants (ColorPrimary, ColorError, etc.)

### Rule 105 : Help message format

- Standard format: `[key] action  [key] action`
- Displayed with `theme.HelpStyle` (italic + ColorDim)
- Keys in square brackets `[]`
- Separated by at least 2 spaces

### Rule 115 : Uniform background handling (lipgloss)

**lipgloss does NOT propagate a parent's background to its children.**

`JoinHorizontal`/`JoinVertical` insert "bare" spaces that show the terminal's native background.

| Forbidden | Alternative |
|----------|-------------|
| `lipgloss.JoinHorizontal` to assemble blocks | Build each line manually with `PadWithBg()` |
| `lipgloss.JoinVertical` to stack lines | `strings.Join(lines, "\n")` with each line padded |
| `strings.Repeat("\n", n)` for blank lines | `theme.EmptyLineBg(width)` |
| `lipgloss.NewStyle().Render(text)` with no background | `.Background(ColorBackground)` mandatory |
| Hardcoded hex color | Global variables (`ColorBackground`, `ColorDim`, etc.) |

`JoinHorizontal`/`JoinVertical` are acceptable **only** if the result is wrapped in a `.Background(ColorBackground).Width(w)` style.

`lipgloss.Place` → always use `WithWhitespaceBackground(theme.ColorBackground)`.

### Rule 117 : Centralized helpers of the `theme` package

**Never recreate `bg()`, `padWithBg()`, `bgWrap()` locally.**

| Function | Usage |
|----------|-------|
| `theme.Bg(s)` | Plain text with the app background |
| `theme.PadWithBg(content, width)` | Pad up to `width` with background |
| `theme.BgLine(s, width)` | Text + background + pad up to `width` |
| `theme.BgWrap(s, width)` | Background + pad on each line (multi-line) |
| `theme.EmptyLineBg(width)` | Blank line filled with background |
| `theme.OverlayBoxStyle()` | Standard style for overlay modals |

- **Inline text** → `theme.Bg("text")`
- **Full line** → `theme.BgLine("text", width)`
- **Multi-line paragraph** → `theme.BgWrap(text, width)`
- **Blank line** → `theme.EmptyLineBg(width)`
- **Modal overlay** → `theme.OverlayBoxStyle().Render(content)`

### Rule 118 : Standardizing table and tab colors

| Element | Foreground | Background |
|---------|-----------|------------|
| Table header | `ColorSecondary` | — |
| Selected row (normal) | **each column's own** | `ColorTableLineSelected`, bold |
| Selected row (error) | `ColorBlack` | `ColorError` |
| Active tab | `ColorBlack` | `ColorSecondary` |
| Inactive tab | `ColorDim` | `ColorCommandLineBg` |

**Selected row (normal) is experimental** (§3.72 in the backlog). It used to
be one flat `ColorBlack` on `ColorSecondary`, dropping every column's own
colour — see Rule 122's "Selected row" section for why that used to be
mandatory and why it no longer is for this one variant: every cell now
repaints `ColorTableLineSelected` and bold **itself**, which is what makes
keeping the per-cell foreground safe instead of a return of the defect.
`error`, `busy` (`TableStylesForState`) and severity
(`TableStylesForSeverity`) are untouched — a solid background there still
means "this whole row is in that state," which per-cell colour would fight.

`ColorTableLineSelected` (`table_line_selected` in a theme file) is its own
key rather than an alias of `ColorSeverityLow`, on purpose: `ColorSeverityLow`
now points at the same tone as `ColorDim` — the colour the `:sec` inventory
already uses for a "0" count (Rule 122's "Color discipline") — so a theme
tuning "how muted is a LOW finding" no longer also retunes the selection
highlight by coincidence. Both keys default to the same hex the "normal"
selection has always used, so nothing changes on screen from this split by
itself.

Mandatory centralized functions:
- `theme.DefaultTableStyles()` — base style
- `theme.TableStylesForState(state)` — `"normal"` or `"error"`
- `theme.TableStylesForSeverity(severity)` — `"CRITICAL"`, `"HIGH"`, `"MEDIUM"`, `"LOW"`, `"UNKNOWN"`

Forbidden:
- ❌ Selection styles defined locally in views
- ❌ `ColorPrimary` for active tabs or headers (use `ColorSecondary`)

### Rule 119 : No hardcoded hex color outside `colors.go`

**`lipgloss.Color("#...")` must appear ONLY in `internal/ui/theme/colors.go`.**

3-level architecture:

| Level | File | Role |
|--------|---------|------|
| **1. Base palette** | `colors.go` lines 6-32 | Raw hex colors (Catppuccin) |
| **2. Semantic colors** | `colors.go` lines 34+ | Aliases by usage, referencing the palette |
| **3. Styles** | `styles.go`, views | lipgloss styles using the semantic colors |

If a hex color is needed with no semantic equivalent:
1. Add it to the base palette (`colors.go`, hex section)
2. Create a semantic alias that references the palette

```go
// ✅ CORRECT
var ColorMaroon = lipgloss.Color("#eba0ac")  // dans palette de base
ColorSeverityHigh = ColorMaroon              // in the semantic section

// ❌ WRONG
ColorSeverityHigh = lipgloss.Color("#eba0ac")  // hardcoded hex in the semantic section
```

`manager.go`'s fallbacks → reference `colors.go`, never hex values.

### Rule 127 : Relative time — `theme.TimeAgo`

**Every view displaying a relative time must use `theme.TimeAgo(t time.Time) string`** (`internal/ui/theme/timeago.go`).

| Duration | Output |
|-------|--------|
| < 1 min | `"now"` |
| < 1 hour | `"59 min ago"` |
| < 24 h | `"23 hr ago"` |
| < 30 days | `"30 days ago"` |
| < 12 months | `"11 mo ago"` |
| else | `"99 yr ago"` |
| zero | `""` |

Forbidden: local implementations, long formats, compact with no space.

### Rule 129 : Interface and log language — English US only

**All UI text and all logs must be in American English.**

| Category | Rule |
|-----------|-------|
| Labels, titles, UI messages | ✅ English US |
| Error messages (footer) | ✅ English US |
| Help (`GetHelpContent`, `GetShortcuts`) | ✅ English US |
| Cell values, empty messages | ✅ English US |
| Logs (`log.Printf`) | ✅ English US |
| Code comments | ✅ English only (Rule 307) |

```go
// ✅ CORRECT
theme.SpinnerMessage(m.spinner.View(), "Loading workspaces...")
m.footerError = "Failed to refresh — check logs"

// ❌ WRONG
theme.SpinnerMessage(m.spinner.View(), "Chargement des workspaces...")
m.footerError = "Échec du rafraîchissement — voir les logs"
```
