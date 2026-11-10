# Spike DEVDESK-198: Charm v2 migration (Bubble Tea, Bubbles, Lip Gloss, ntcharts)

2026-11-10, Linux, Go 1.27.3. Base: `origin/main` at 63ebd72 (DEVDESK-197, 199, 200 merged).
Experiment code: branch `spike/charm-v2-code` (commit 217b389), **to be discarded, never merged**. This report is the only deliverable.
Pinned versions (not reopened): `charm.land/bubbletea/v2` v2.1.1, `charm.land/bubbles/v2` v2.2.1, `charm.land/lipgloss/v2` v2.0.6, `github.com/charmbracelet/x/ansi` v0.11.9, `github.com/NimbleMarkets/ntcharts/v2` v2.9.0 (exact module path confirmed with `go list -m`; its `go.mod` requires bubbletea v2.1.1 and lipgloss v2.0.6, no `replace`). `go mod tidy` also pulled `go-runewidth` v0.0.16 to v0.0.31 and `colorprofile` v0.4.3.

Method: sed the imports, let the compiler lead, fix by category. The spike ended **with everything compiling and `go test ./...` green** (run once, `-timeout 100s`; `go vet ./...` clean). The only deleted tests are the three of `internal/ui/testutil` (`keys_test.go`, `source_guard_test.go`, `testutil_test.go`) and `colorprofile_test.go`, which test v1 APIs and are to be rewritten (see criteria 4 and 5). Not run: `mise run lint`, `test-race` (needs cgo), the app in a real terminal.

## Summary

| # | Criterion | Verdict | One line |
|---|---|---|---|
| 1 | Imports on theme, datatable, about | OK | Mechanical. datatable: 0 production line beyond imports; theme: 4 logic spots; about: 1 method. The plan's counts hold, with two corrections (`termenv`, tests) |
| 2 | Palette `color.Color` | OK with reserve | 91 type sites (13 calls stay valid), 3 logic spots; Rule 119 holds; **`lipgloss.Color("")` is `NoColor` whose `RGBA()` is opaque black** and an invalid hex is silently `NoColor` |
| 3 | `Render()` + `View() tea.View` + `Screen` | OK | Compiles, **0 `handle*` signature changed** (271 of them, 429 `(tea.Model, tea.Cmd)` returns). Cost: 13 `.(Screen)` assertions in the router, tests renamed to `Render()` |
| 4 | Colour profile | OK with reserve | `tea.WithColorProfile` verified on a real `tea.Program`. 13 test files with termenv was really 3; **7 tests silently depended on the Ascii profile** |
| 5 | `testutil.Msgs` and `tea.Sequence` | OK | Reflection works unchanged: `sequenceMsg` is still `[]tea.Cmd` |
| 6 | ntcharts v2 sparkline | OK | One import line changed; `WithStyle(lipgloss.Style)` (v2); dashboard tests green |
| 7 | Rules 125 / 116, runewidth vs engine | OK with reserve | Layout byte-identical to v1 at 3 sizes. The engine's width depends on terminal mode 2027: **cannot be verified without a real terminal** |
| 8 | Viewport keys, `PasteMsg` | OK with reserve | Viewport: no change needed. **Paste is lost by the router and by every `KeyMsg`-gated field** (fixed in the router and `FilterBar`; ~34 sites remain to audit) |
| 9 | Commit split and effort | OK, adjusted | The seven commits and their order are those of `charm-v2-impact.md` §4; only their content is refined (sizes, what goes where); effort L confirmed (2 to 3 days of coding, plus manual terminal checks) |

## 1. Imports on theme, datatable, about

Counts on `main` (63ebd72). Method: `git grep -l '"github.com/charmbracelet/<pkg>"' 63ebd72 -- '*.go' | wc -l` for files (the bubbletea figure includes the two guard tests that quote the path as a string; a reviewer recount gave 258, so treat it as 257 to 258); `git grep -nP 'lipgloss\.Color\b(?!\()' 63ebd72 -- '*.go' ':!*_test.go'` for type sites (91; a reviewer recount gave 93, difference not explained, so 91 to 93) and the same without the lookahead trick (`lipgloss\.Color\(`) for calls (13); `git grep -n '\.(Screen)' spike/charm-v2-code -- 'internal/app/*.go' ':!*_test.go'` for assertions (13). Against §1 of `charm-v2-impact.md`:

| Import | Plan | Measured |
|---|---|---|
| bubbletea | 239 files | 257 files (tests included) |
| lipgloss | 147 | 150 |
| bubbles spinner / textinput / table / viewport / key | 39 / 26 / 21 / 6 / 1 | 39 / 26 / 21 / 6 / 1 (identical) |
| x/ansi | 52 `ansi.*` | 53 imports |
| `termenv` | 13 test files | **3 files** (DEVDESK-199 funnelled it into `internal/testutil/colorprofile`; 50 callers of that helper stay unchanged) |

One `sed` over the whole repo (376 files) replaced them. Also needed: two `string`-literal fixes, because the sed hit import paths quoted in two guard tests.

What compiles and what breaks, per package, after the import swap only:

| Package | Production code | Tests |
|---|---|---|
| `theme` | `colors.go`: 76 declarations `lipgloss.Color` to `color.Color` (type, mechanical). 15 more type sites elsewhere. `contrast.go`, `tokencolors.go` (read hex from the string): rewritten. `StyleTextInput` (textinput styles): rewritten. `manager.go`: **0 change** (`lipgloss.Color(hex)` is now a function and still compiles) | 72 compile errors in 13 files: 37 string constants given as `color.Color`, ~12 `string(c)`, 8 `lipgloss.TerminalColor`, rest `string` literals. Fixed with a 40-line script (wrap in `lipgloss.Color(...)`, `string(c)` to `theme.Hex(c)`). 1 failing test: the SGR reset is now `\x1b[m`, not `\x1b[0m` |
| `datatable` | **0 line** (builds as is, it does not use `bubbles/table` rendering) | compiles after `testutil` is ported; 4 tests fail until 8 literals `"\x1b[0m"` become `"\x1b[m"` |
| `about` | `View() string` becomes `Render() string` plus a 1-line `View() tea.View` | 3 `.View()` to `.Render()`; 1 test (see criterion 4) |
| `components` (dependency) | 2 sites `textinput.Width = n` to `SetWidth(n)` (23 such sites repo-wide) | 14 lines |

Whole repo, production code: 209 files, +747 / -623. Tests: 203 files, +1070 / -1585 (the deletions are the four removed test files). The compiler found everything except the five silent points listed in criteria 2, 4, 8 and below.

**Silent points actually hit** (compiler-silent, caught by tests): the space key (`keymap.Space` is `" "` in v1; flipping that one constant to `"space"` took the failures from about 50 to 11, which is the DEVDESK-200 payoff); the paste (criterion 8); the SGR reset `\x1b[m` (9 literals in 6 test files: 8 in datatable, 1 regexp in theme; `overlay.go` already accepts both); 7 tests that assumed no ANSI (criterion 4).

Mechanical changes beyond imports, with site counts: `WithWhitespaceBackground` to `WithWhitespaceStyle(NewStyle().Background(c))` 16 sites (exact match with the plan); `textinput` `.Width` 23; textinput style assignments 3 (`theme.StyleTextInput`, `setup/styles.go`, `paletteInputView`); viewport `New(w,h)` 4, `.Width/.Height/.YOffset` reads and writes about 50 (including tests); `tea.WithAltScreen` 2 (now `View.AltScreen`). `bubbles/table` and `spinner` needed **no** change. `tea.KeyMsg` stays compilable (it is now an interface matching press and release): 103 production sites still use it; it works but should become `tea.KeyPressMsg` (release events only exist with keyboard enhancements, which we do not request).

## 2. Palette

The criterion announced "12 sites outside tests". Those are the *calls* `lipgloss.Color("...")` (measured 13: 12 in `theme/manager.go` `ApplyTheme`/`applyColor`, 1 in `app_header.go`), which stay valid because `Color` is now a function. The ~91 are the *type* uses `lipgloss.Color` as a declared type (76 in the `colors.go` var block, 15 elsewhere), a different count that the compiler forces to `color.Color`; the plan's "12 sites" list never included the var block.

Port (what I did): every `var Color… lipgloss.Color` becomes `color.Color` (`import "image/color"`). Semantic aliases (`ColorTableLineSelected`, `ColorSyntax*`, `ColorFooter*`, `IconColor`, `TokenColor`, `indicator.Color`, `cmdsuggest.Fg/Bg`) carry the same `color.Color`; an alias is an assignment, so `ApplyTheme` is unchanged.

**Rule 119** (hex only in `colors.go`): holds. `colors.go` only declares; the hex strings live in `manager.go`'s `DefaultTheme` and theme files, and `ApplyTheme` calls `lipgloss.Color(hexString)` exactly as before. A grep for `#rrggbb` literals outside `manager.go`/`colors.go` finds comments only. The one addition is `theme.Hex(c color.Color) string` (formats `#rrggbb` or `none`), used by tests in place of `string(c)`; it produces hex, it does not hold any.

`contrast.go` reads components through `RGBA()` (`uint32`, 16-bit, shift by 8), after a type check:

```go
if _, ok := c.(color.RGBA); !ok { return error }   // only "#rgb"/"#rrggbb" come out as color.RGBA
r, g, b, _ := c.RGBA()
```

Without the type check the old contract ("hex only, else error") is lost, because of the empty-colour semantics below. `TestContrast*`, `ColorDistance` pass; their "error" table (`"", "red", "12", "ffffff", "#ggg"...`) still errors, now because those values become `NoColor` or `ansi.BasicColor`.

**Empty colour** (`iconcolors.go`, measured):

| Input | v2 type | `RGBA()` | `Render` |
|---|---|---|---|
| `lipgloss.Color("")` | `lipgloss.NoColor` | **0 0 0 65535 (opaque black)** | no SGR emitted ("inherits", same effect as v1) |
| `Color("red")`, `Color("#ggg")` | `NoColor` (silently) | opaque black | none |
| `Color("#fff")` | `color.RGBA` | ok | 38;2 |
| `Color("12")` | `ansi.BasicColor` | palette | 38;5/ANSI |
| `NewStyle().GetForeground()` (unset) | `NoColor{}`, `== Color("")` is true | | |

Consequences: (a) the rendering semantics of "no colour" are preserved, so the `IconColor` rule (unknown role falls back to `ColorText`, never the zero Color) still protects Rule 122 and `IconColor("nope") == ColorText` passes; (b) **anything reading components must type-check first**, because `NoColor.RGBA()` is black; (c) `app_header.go:190` (`GetForeground() != lipgloss.Color("")`) keeps working through interface equality; (d) **a typo in a theme hex is now silently no colour**, where `lipgloss.Color` kept the string; if `ApplyTheme` should reject it, validate at load (not done in the spike, flagged as a risk). Reserve: the `textinput` virtual cursor style is `NewStyle().Foreground(Cursor.Color)` only, so the cursor cell loses the old `Background(ColorBackground)` (Rule 115); visual check needed.

## 3. Architecture: `Render() string` + `View() tea.View` + `Screen`

Done as decided:

```go
// each of the 17 views
func (m Model) Render() string { ... }                         // was View() string
func (m Model) View() tea.View { return tea.NewView(m.Render()) }

// router
type Screen interface { tea.Model; Render() string }
views map[command.ViewType]Screen
func (a *App) View() tea.View { v := tea.NewView(a.Render()); v.AltScreen = true; return v }
```

`main.go` loses `tea.WithAltScreen()`; the setup wizard (its own root program) sets `AltScreen` in its own `View`.

Result: **compiles, and no `handle*` signature changed.** `grep` finds 271 `handle*(...) (tea.Model, tea.Cmd)` and 429 `(tea.Model, tea.Cmd)` returns in production code (the plan said ~480); the diff touches none of them (the only added one is `handlePasteMsg`). Reason: `tea.Model` is still satisfied by the view.

What it costs:
- 17 views: 17 renames + 17 one-line wrappers (scripted).
- Router: `Update` of a view returns `tea.Model`, so every `a.views[x] = updated` needs `updated.(Screen)`: **13 sites** in `internal/app` (it panics if a view stops implementing `Render`). A helper `a.setView(name, tea.Model)` would centralise it; I did not.
- 5 fake views in `internal/app/*_test.go` (+2 program wrappers): rename plus wrapper; `router()`, `routerAt()`, `commanding()` take a `Screen`.
- Tests: `.View()` on a view model becomes `.Render()`. 450 `.View()` calls in 104 test files; a script renamed all in the 17 view packages and `internal/app`, then reverted the 115 that were components (`datatable`, `FilterBar`, forms, which keep `View() string`). Tests may equally call `View().Content` (a `string` field, set by `NewView`); `Render()` is shorter and does not allocate a `tea.View`. Recommendation: `Render()` in tests.
- Components (`datatable`, `components.*`, `status/components`) keep `View() string`, as planned. Guards keyed on the method name `View` do not exist (`grep '"View"'` finds one test map), so the AST guards (`TestNoTableViewRendersALoadingBody`, footer guards) are unaffected by the rename.

## 4. Colour profile

`lipgloss.SetColorProfile` is gone; `Style.Render` now always emits full colour (`\x1b[38;2;r;g;bm`). `internal/testutil/colorprofile` therefore keeps its API (`TrueColor(t)`, `Force()`) as **no-ops** (50 callers untouched) and its own test file is deleted; `termenv` leaves `go.mod`. `source_guard_test.go` still needs the same rule re-aimed ("no import of `colorprofile` other than the helper, no hand-built `tea.KeyPressMsg{}`").

Replacement validated on a real `tea.Program` (`internal/app/spike_profile_test.go`, 3 cases, output captured in a buffer):

| `tea.WithColorProfile(...)` | `38;2;` | `38;5;` | `38;` | alt screen `\x1b[?1049h` | bytes |
|---|---|---|---|---|---|
| TrueColor | yes | | | yes | 3499 |
| ANSI256 | no | yes | | yes | 2634 |
| Ascii | no | no | no | yes | 1402 |

So downsampling happens at the output, and `View.AltScreen` replaces the program option. `ColorProfileMsg` is sent by the program after capability detection; the `main.go` `COLORTERM`/`WT_SESSION` workaround can become `tea.WithColorProfile(colorprofile.TrueColor)` when `WT_SESSION` is set (not changed in the spike; WSL/Windows Terminal not verified).

Guards:
- `TestNoViewStylesItsOwnFooterMessage`: AST scan of function names containing `RenderFooter`/`InfoLine`/`InfoText`; passes unchanged, keeps its meaning.
- `TestNoPackageLevelVarRendersAString`: passes unchanged. Its doc comment lists two causes; **cause 2 (lipgloss `sync.Once` locking the profile at init) no longer exists**, cause 1 (string frozen on the default theme) remains and is enough to keep the test. Update the comment.
- datatable tests: all pass after the eight `\x1b[0m` literals. The Rule 116 tests (`TestSelectedRowWithEmojiMeasuresRenderedWidth`) pass with x/ansi v0.11.9.
- **Hidden dependency found**: 7 tests asserted "blank line" or "empty info line" on raw `Render()` output and passed only because `go test` had no TTY, hence the Ascii profile (`about_test.go` says so in a comment: "under `go test` lipgloss finds no TTY and emits none"). In v2 the same line is `ESC[38;2;…m   ESC[m`. Fixed with `ansi.Strip` in `about`, `configuration`, `dashboard`, `netdiag`, `oci_resources`, `security` (x2). These tests did not check what they claimed to; `ansi.Strip` is the right form.

## 5. `testutil.Msgs` and `tea.Sequence`

Works unchanged. bubbletea v2.1.1 still has `type sequenceMsg []Cmd` (unexported) and `type BatchMsg []Cmd` (exported); the reflection (`Kind()==Slice && Elem()==TypeFor[tea.Cmd]()`) matches as before. Tested: `Msgs(tea.Sequence(c(1), tea.Batch(c(2), c(3)), c(4)))` returns the four messages in order. No replacement needed. Risk to note: this relies on the type staying a `[]Cmd`; `internal/ui/testutil` should keep the test that fails if bubbletea changes it.

Other `testutil` changes (one 60-line file rewritten, all of `Key`/`Type`/`Paste`/`IsText`/`Text`): `Key` returns `tea.KeyPressMsg{Code, Text, Mod}`; space is `{Code: KeySpace, Text: " "}` (its `String()` is `"space"`); `alt+x` must clear `Text` (otherwise `String()` returns `x`, found by a test); `shift+tab` is `{Code: KeyTab, Mod: ModShift}`; `Paste` is `tea.PasteMsg{Content}`. The 1295 `testutil.Key` call sites needed no change; 22 tests that name `tea.KeyMsg` kept compiling.

## 6. ntcharts v2 sparkline

OK. `internal/ui/dashboard/chart.go`: **one line** (the import path). All used API exists in `github.com/NimbleMarkets/ntcharts/v2/sparkline` v2.9.0: `New(w, h, opts...)`, `WithStyle(lipgloss.Style)` (lip gloss v2 type), `WithMaxValue`, `WithNoAutoMaxValue`, `PushAll`, `Draw`, `DrawBraille`, `Model.View() string`. Styles are `lipgloss.Style`, not a raw colour, so `color.Color` does not appear. The six chart tests (`TestEveryChartCellCarriesABackground`, `TestAChartLineIsExactlyTheColumnWidth`, …) pass.

## 7. Rules 125 and 116, measure divergence

Method: the same dump test run on `origin/main` (v1) and on the spike (v2), at 80x24, 120x40, 200x50, for (a) a datatable with an untitled `IconColumnWidth` (2) Nerd Font column (U+F07B, U+F0849, U+E725…) and rows holding VS16 emoji, flags, ZWJ, CJK, combining marks; (b) the viewer on a log with wide characters; (c) the setup wizard form. Per line: `ansi.StringWidth`, `runewidth.StringWidth` of the stripped text, and the text.

- Layout (stripped text and `ansi.StringWidth` of every line): **identical v1 and v2 for the 9 dumps**. Table lines are all 78/118/198 cells (viewport minus the 2 border cells); Rule 116 holds, and `TestSelectedRowWithEmojiMeasuresRenderedWidth` is green. Rule 125 holds: the icon column is 2 cells, glyph plus one cell, with no clipping.
- The only v1/v2 difference is the `runewidth` figure of lines containing a flag: 77 vs 78 (go-runewidth v0.0.16 to v0.0.31 now counts regional-indicator pairs as 2).
- `runewidth` vs the engine's measure (`ansi.StringWidth`, grapheme) vs `ansi.StringWidthWc` (per-rune wcwidth):

| String | grapheme | wcwidth | runewidth v0.0.31 |
|---|---|---|---|
| `a❤️x` | 4 | 3 | 3 |
| `✔️`, `🛡️` | 2 | 1 | 1 |
| flag FR | 2 | 2 | 2 |
| ZWJ family | 2 | **6** | 2 |
| Nerd Font glyph (PUA) | 1 | 1 | 1 |
| CJK, `e`+U+0301 | 6 / 1 | 6 / 1 | 6 / 1 |

  So `datatable`'s `runewidth` agrees with the grapheme measure except for VS16 emoji (3 vs 4), the case the Rule 116 test pins. **Not verifiable here:** which method the renderer uses. In bubbletea v2.1.1 the cell renderer starts in per-rune width and switches to `ansi.GraphemeWidth` only when the terminal answers the DECRQM query for mode 2027 (`tea.go` handling of `ModeReportMsg`). On a terminal without 2027 (support in Windows Terminal, tmux, Terminal.app not checked) a VS16 emoji or a ZWJ sequence would be laid out with another width than `datatable` assumes, shifting the rest of the line. Mitigation if it shows: keep emoji out of columns, or measure with `ansi.StringWidthWc` when mode 2027 is absent. Needs a real terminal test.
- 19 `runewidth.*` call sites in 3 production files remain (not 37/10: the rest are tests).

## 8. Viewport and paste

**Viewport.** The criterion's hypothesis (←/→ scroll by default and must be disabled to respect Rule 135, where ←/→ change level or value) is **refuted for DevDesk**: the default does scroll horizontally in v2, but no viewport that would conflict ever receives ←/→, and the one that receives raw keys already has no Left/Right binding. Measured with `internal/app/spike_viewport_test.go`: a default v2 viewport scrolls horizontally on `right` (XOffset 6) and on `l`, and vertically on `j`. But only **one** viewport ever receives a raw key: the help overlay (`help_overlay.go:68`, `helpViewport.Update(msg)`), and it already installs `arrowOnlyScroll()`, whose `KeyMap` has no `Left`/`Right`. Test result: with that keymap, `left right h l j k space d u b f` leave X and Y at 0; `down` and `pgdown` still scroll. The other four viewports (app body, viewer, security and netdiag details) never get `Update`, they are driven with `SetYOffset`. So there is **nothing to disable**, contrary to the plan's "5 viewports". It is also not a v2 novelty: bubbles v0.21's `KeyMap` already has `Left`/`Right` (`left,h` / `right,l`), found in the module source. Keep the test, and the rule "a viewport that receives keys sets an explicit `KeyMap`".

**Paste.** `tea.PasteMsg` does **not** reach the focused field by default: the router's `update` only switches on `tea.KeyMsg`. Four tests of DEVDESK-199 failed, as they were meant to: `TestPasteIntoCommandPaletteReachesTheField` (2 cases), `TestPasteWhileHelpIsOpenTriggersNothing`, and `TestTypedTextReachesTheFieldUnchanged/FilterBar`. Fixed in the spike:
- router: `case tea.PasteMsg: return a.handlePasteMsg(msg)`, 14 lines. Dropped during a context switch, help or context list; to `commandInput` in command mode; to the active view only when `inEditMode()`; else ignored. A paste can no longer fire `q`, `?` or `:` (Rule 111). `textinput` v2 turns newlines into spaces (`l1\nl2` gives `l1 l2`).
- `FilterBar.Update`: accept `tea.PasteMsg` next to `tea.KeyMsg`.
- **Still open**: 36 `msg.(tea.KeyMsg)` / `case tea.KeyMsg` gates in production code route text to inputs (30 files use `textinput` or `WrappedInput`). Every form or view with an edit mode must also accept `PasteMsg`. The existing paste tests cover the router, the palette, `FilterBar` and the components; the views' forms need one table-driven test each (add during S2).

## Decisions for the migration story

Taken by the user after this report:
1. `ApplyTheme` rejects an invalid hex at load time (a typo no longer degrades silently to `lipgloss.NoColor`, see criterion 2).
2. The `COLORTERM`/`WT_SESSION` workaround in `main.go` is replaced by `tea.WithColorProfile(colorprofile.TrueColor)` (see criterion 4).

## 9. Conclusion: commit split and effort

The order of the plan (§4, S2 a to g) is confirmed unchanged. What the spike changes is the content of each commit, not the order:
1. **The build cannot be compiled between commits anyway** (all of `charm.land/*/v2` at once), so the "commits" are review units, not bisect points; say so in the PR. Squashing is not advised, reviewers need the split.
2. Content and size per commit:

| # | Commit | Content | Measured size |
|---|---|---|---|
| a | `theme` | `color.Color`, `contrast`/`tokencolors` via `RGBA()` + type check, `theme.Hex`, `StyleTextInput`, SGR reset literals | ~25 files |
| b | keys | `testutil` rewritten, `keymap.Space = "space"`, `tea.KeyMsg` to `tea.KeyPressMsg` (103 sites, optional but wanted), `source_guard_test` and `testutil` tests ported | ~10 files + the 103 sites |
| c | router | `Screen`, `Render`/`View` on 17 views + `App`, `AltScreen`, `.(Screen)` x13, `handlePasteMsg`, tests renamed to `Render()` | ~120 files, mostly scripted |
| d | bubbles | `textinput` `SetWidth` x23 and `SetStyles` x3, viewport options and accessors, `WithWhitespaceStyle` x16, paste in forms (36 gates), explicit `KeyMap` rule | ~60 files |
| e | ntcharts + ansi | import line, `go mod tidy` | 2 files |
| f | colour profile | `colorprofile` helper as no-op, drop `termenv`, `ansi.Strip` in the 7 tests, `main.go` `WithColorProfile` if `WT_SESSION` | ~15 files |
| g | docs | Rules 111 ("v1.3.10 does not enable"), 115 (cursor), 122, `app-shell.md`, `configuration.md`, `ui-components.md`; `TestNoPackageLevelVarRendersAString` comment | |

3. Effort: L confirmed. The spike (mostly scripted) touched 209 production and 203 test files with about 100 lines of real logic (theme 4 spots, testutil 60 lines, router 15, `StyleTextInput` x3). Estimated 2 to 3 days to land cleanly (lint, `test-race`, review of the diff), plus half a day of manual checks.

**Remaining risks and unknowns (not verified here):**
- Real terminals: mode 2027/2026 negotiation, WSL and Windows Terminal colours, Nerd Font rendering, tmux. Nothing was run in a TTY; only a captured `tea.Program` output.
- The four `tea.ExecProcess` sites (container shell `T`, OCI `-it`, pager `V`, workspace terminal): not exercised; API unchanged on paper.
- Visual check of the textinput virtual cursor background (Rule 115) and the footer filter bar.
- `TestMeasureRedrawCost` baseline not compared (the new renderer's cost is a claimed gain of the plan, not measured).
- `golangci-lint` and `test-race` not run on the spike.
- Silent `NoColor` on a bad theme hex (criterion 2); decide whether `ApplyTheme` validates.
- Router assertions `.(Screen)` (13) can panic at runtime; consider a typed `setView`.
- Paste in the 36 `KeyMsg`-gated views (criterion 8).
- `go-runewidth` moved to v0.0.31 through tidy, changing flag widths in the 19 remaining `runewidth` sites.
- `tea.KeyMsg` kept at 103 production sites matches key *release* events too; harmless until keyboard enhancements are requested, so migrate to `KeyPressMsg` in commit b.
