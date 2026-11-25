---
paths:
  - "internal/ui/**"
  - "internal/app/**"
  - "themes/**"
---

# TUI — Theme, Colors & Background

### Rule 102 : Centralized management of colors and styles

- All colors must be defined in `internal/ui/theme/colors.go`
- Never hardcode colors directly in views
- Reusable styles must be defined in `internal/ui/theme/styles.go`
- Use the theme package's constants (ColorPrimary, ColorError, etc.)
- A text colour reaches 4.5:1 (`theme.ContrastRatio`) on the surface it is painted on, on each shipped theme: `ColorText`, `ColorDim`, `ColorSubTitleFg`, `ColorWarn`, `ColorFooterWarn`, `ColorFooterError` (`TestEveryTextColourReachesItsContrastOnEveryTheme`, DEVDESK-764), and, from DEVDESK-770, `ColorSecondary` (4.5:1 on the background, 3:1 on the selected row), `ColorTableHeaderFg`, `ColorPrimary`, `ColorTitleFg`, `ColorAppTitleFg`, `ColorSeverityHigh` and `ColorSeverityLow` (4.5:1 on the background, 3:1 on the selected row; the badge text is `ColorBackground`, D181), plus the pairs of the active tab, the focused primary button, the unfocused primary and secondary buttons, the inactive command line, and, from DEVDESK-774 part A (D183), the focused danger and success buttons, the unfocused ones and the error row (`TestEveryTextPairReachesItsContrastOnEveryTheme`), and the six severity badges, INFO and UNKNOWN included (`TestEverySeverityBadgeReachesTextContrastOnEveryTheme`); on the focused pairs the background moved (`color_error`, `color_ok`, `severity_info`), not the text; the unfocused danger and success labels have their own keys, `button_danger_fg` and `button_success_fg` (fallback `ColorError` and `ColorOK`), because their background `button_inactive_bg` is shared with the other buttons and does not move, and each is at least delta E 8 from the "No" label (`TestTheUnfocusedYesLabelsStayApartFromTheNoLabel`); and `ColorError`, `ColorOK` and the button backgrounds may not fall on the application background (`TestErrorOkAndButtonsKeepTheirRatioOnTheBackground`); the frame border `ColorViewportBorder` reaches 3:1 on the background (a non-text object, WCAG 1.4.11, D179), and it is also the gauge rail (D181), faint under the cursor. What tells "greyed" or "secondary" apart is not always the shade, and the code holds exactly this: the disabled button is italic (`TestTheDisabledButtonDiffersFromAnEnabledOneByMoreThanColour`); a greyed shortcut keeps its key without bold or a vivid colour; secondary text (`DimStyle`) gets its hierarchy from position and weight, not from italics. On solarized-dark, solarized-light and catppuccin-latte `ColorDim` and `ColorText` are nearly the same tone (delta E 5.6, 0.8 and 4.6), a known limit measured in `docs/measurements/2026-11-24-theme-text-contrast.md` (Rule 130)

### Rule 105 : Help message format

- Standard format: `[key] action  [key] action`
- Displayed with `theme.HelpStyle` (italic + ColorDim)
- Keys in square brackets `[]`
- Separated by at least 2 spaces

### Rule 115 : Uniform background handling (lipgloss)

With `app.theme: auto` the OSC 11 query comes before the theme's background: `View().BackgroundColor` stays `nil` while the detection is pending, so the terminal answers with its own color.

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

`lipgloss.Place` → always use `WithWhitespaceStyle(lipgloss.NewStyle().Background(theme.ColorBackground))`
(Lip Gloss v2 removed `WithWhitespaceBackground`).

**Safety net, not a replacement (DEVDESK-209).** The router asks the terminal to
use `ColorBackground` as its default background (`tea.View.BackgroundColor`). The
helpers above stay mandatory: the native background is absent in 16-colour
terminals, under `NO_COLOR`, without a TTY, in a terminal or tmux that ignores
OSC 11, and during a `tea.ExecProcess` (the child sees the terminal's own
default). A bare cell there shows the user's background. See
`docs/architecture/app-shell.md`.

**Known gap: the text input's cursor cell.** In Bubbles v2 the virtual cursor
(`textinput.Styles.Cursor`) carries a colour and no background, so that one cell
cannot take `ColorBackground`. `theme.StyleTextInput` sets every other state and
the cursor colour (`ColorHighlight`); the single-cell gap is accepted.

**Drawing over an already rendered screen** (the command palette) goes through
`overlay.Compose` (`internal/ui/overlay`) and nothing else: it splices with
`x/ansi` and never cuts an escape sequence. `lipgloss.Place` stays for full-screen
modals. Never slice a styled line by bytes or runes.

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
| `overlay.Compose(base, box, x, y, width)` | Box drawn over rendered lines, ANSI-safe |

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
| Selected row (error) | `ColorBlack` | `ColorError`, 4.5:1 on all 13 themes since DEVDESK-774 part A (D183: `color_error` is the theme's `severity_critical` on five themes) |
| Active tab | `ColorBlack` | `ColorSecondary` |
| Inactive tab | `ColorDim` | `ColorCommandLineBg` |

**Selected row (normal)** (§3.72 in the backlog; confirmed 2026-09-21). It used to
be one flat `ColorBlack` on `ColorSecondary`, dropping every column's own
colour — see Rule 122's "Selected row" section for why that used to be
mandatory and why it no longer is for this one variant: every cell now
repaints `ColorTableLineSelected` and bold **itself**, which is what makes
keeping the per-cell foreground safe instead of a return of the defect.
`error`, `busy` (`TableStylesForState`) and severity
(`TableStylesForSeverity`) are untouched — a solid background there still
means "this whole row is in that state," which per-cell colour would fight.

`ColorTableLineSelected` (`table_line_selected` in a theme file) is its own
key rather than an alias of `ColorSeverityLow`, on purpose, so a theme tuning
"how muted is a LOW finding" no longer also retunes the selection highlight by
coincidence. Since DEVDESK-770 part B (D181) `ColorSeverityLow` is `ColorDim` on
the dark themes and the LOW badge text is `ColorBackground` on every level
(`TestSeverityBadgeTextIsTheAppBackgroundOnEveryTheme`): a badge is the
selected row of a finding, held to 4.5:1 (`TestEverySeverityBadgeReachesTextContrastOnEveryTheme`). INFO and UNKNOWN share one muted badge (`severity_info_fg` on `severity_info`), held to the same 4.5:1 since DEVDESK-774 part A (D183): latte darkens the text, solarized-light darkens the badge background and keeps its text.
A theme that omits the two LOW keys gets `ColorDim` under `ColorBackground`.

Mandatory centralized functions:
- `theme.DefaultTableStyles()` — base style
- `theme.TableStylesForState(state)` — `"normal"` or `"error"`
- `theme.TableStylesForSeverity(severity)` — `"CRITICAL"`, `"HIGH"`, `"MEDIUM"`, `"LOW"`, `"UNKNOWN"`

Forbidden:
- ❌ Selection styles defined locally in views
- ❌ `ColorPrimary` for active tabs or headers (use `ColorSecondary`)

### Rule 119 : No hardcoded hex color outside `colors.go`

**A hex literal (`lipgloss.Color("#...")`, `color.RGBA{…}`) must appear ONLY in `internal/ui/theme/colors.go`.**

One exemption: the body of `DefaultTheme` in `internal/ui/theme/manager.go`, the embedded default theme. `TestGuardNoHexColorOutsideColorsGo` (`internal/ui/theme/source_guard_test.go`) enforces both. The invalid-field underline colour is `theme.ColorInvalidField`, an alias (Rule 140).

Palette variables are `color.Color` (`image/color`), not a Lip Gloss type. Read
one back with `theme.Hex(c)` (`#rrggbb`). A theme file is validated on load:
`theme.LoadTheme` refuses the whole file when a non-empty colour is not `#rgb`
or `#rrggbb` (see `docs/architecture/configuration.md`).

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
var ColorMaroon = lipgloss.Color("#eba0ac")  // in the base palette
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
m.footerStatus = "Loading workspaces..."
m.footerError = "Failed to refresh — check logs"

// ❌ WRONG
m.footerStatus = "Chargement des workspaces..."
m.footerError = "Échec du rafraîchissement — voir les logs"
```
