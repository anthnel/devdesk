# Button and badge contrast, DEVDESK-774: the focused danger and success buttons, the error row, the INFO and UNKNOWN badges (part A), the unfocused danger and success buttons (part B)

Measurement note of 2026-11-25. It follows `2026-11-24-theme-text-contrast.md`, `2026-11-24-theme-text-contrast-2.md` and
`2026-11-24-theme-text-contrast-3.md`, which are frozen: the third one lists these pairs under "Not done here", and this
note is what closes them for the focused buttons, the error row and INFO and UNKNOWN. Nothing in those notes is
rewritten here. The decision is D183 in `docs/backlog.md`. Parts A and B are in this note: part A below, part B
under "Part B" at the end.

| | |
|---|---|
| Method | `theme.ContrastRatio(foreground, background)` and `theme.ColorDistance` (CIE76 delta E), after `ApplyTheme(DefaultTheme())` then `ApplyTheme(<theme>)` for the default theme and each file of `themes/*.json`, read by a throwaway test (deleted) run on the tree before this change (themes read with `git show HEAD:themes/<name>.json`, commit `9793cec0` plus the new tests) and on this one. Each pair is read from the style the screen paints: `ButtonStyle(true, "danger")`, `ButtonStyle(true, "success")`, `TableStylesForState("error").Selected`, `TableStylesForSeverity("INFO" or "UNKNOWN").Selected` |
| Thresholds | 4.5:1 for each text pair (WCAG 1.4.3), `TestEveryTextPairReachesItsContrastOnEveryTheme` and `TestEverySeverityBadgeReachesTextContrastOnEveryTheme`; the new background colours must not fall on the application background, `TestErrorOkAndButtonsKeepTheirRatioOnTheBackground` (the floor is the figure measured before, minus half a hundredth) |
| Retuning rule | the background moves, not the text. `color_error` takes the theme's own `severity_critical` (a tone already tuned for 4.5:1 on the background, D177 and D181); `color_ok` is darkened, same hue, to the smallest step that reaches 4.5:1; on catppuccin-latte `severity_info_fg` is darkened by the smallest step, on solarized-light `severity_info` (the badge background) is darkened and the text stays `#fdf6e3`. No key is added |
| Themes | the 13 shipped ones. `default` and `catppuccin-mocha` are the same palette |

## What changed

| Key | Theme | Moves | delta E |
|---|---|---|---|
| `color_error` | dark-plus | `#cd3131` to `#d85e5e` (its `severity_critical`) | 20.8 |
| `color_error` | nord | `#bf616a` to `#cf898f` (its `severity_critical`) | 16.7 |
| `color_error` | one-dark | `#e06c75` to `#e17079` (its `severity_critical`) | 1.8 |
| `color_error` | solarized-dark | `#dc322f` to `#e46562` (its `severity_critical`) | 24.6 |
| `color_error` | solarized-light | `#dc322f` to `#da2825` (its `severity_critical`) | 4.4 |
| `color_ok` | catppuccin-latte | `#40a02b` to `#327c21` | 18.0 |
| `color_ok` | solarized-light | `#859900` to `#687800` | 16.3 |
| `severity_info_fg` | catppuccin-latte | `#4c4f69` to `#4a4d66` | 1.1 |
| `severity_info` | solarized-light | `#2aa198` to `#217d76` | 14.1 |

`color_error` is also the colour of the error text and icons, and `color_ok` of the success ones. Both are painted on the
application background, so the move may only raise their ratio there; it does. The five themes that moved
`color_error` go from 3.24, 3.05, 4.38, 3.25 and 4.29:1 on the background (dark-plus, nord, one-dark, solarized-dark,
solarized-light) to 4.51, 4.54, 4.53, 4.53 and 4.52:1. The two themes that moved `color_ok` go from 2.96 and 2.97:1 to
4.60 and 4.55:1 (latte, solarized-light). The other themes did not move.

## Ratios, before and after

Each cell is the value after the change when it did not move, and `before to after` when it did. A cell under 4.5 in the
"before" half is a failure the new tests report.

| Theme | danger button, focused | success button, focused | error row (selected) | INFO and UNKNOWN badge |
|---|---|---|---|---|
| catppuccin-frappe | 4.65 | 7.10 | 4.65 | 4.68 |
| catppuccin-latte | 4.80 | 2.96 to 4.60 | 4.80 | 4.39 to 4.54 |
| catppuccin-macchiato | 5.96 | 9.17 | 5.96 | 5.59 |
| catppuccin-mocha | 7.08 | 11.03 | 7.08 | 6.31 |
| dark-plus | 3.24 to 4.51 | 8.49 | 4.08 to 5.68 | 5.96 |
| default | 7.08 | 11.03 | 7.08 | 6.31 |
| github-dark | 5.65 | 7.45 | 5.65 | 9.07 |
| github-light | 5.36 | 5.08 | 5.36 | 4.93 |
| nord | 3.05 to 4.54 | 6.13 | 3.05 to 4.54 | 6.24 |
| one-dark | 4.38 to 4.53 | 6.94 | 4.38 to 4.53 | 4.58 |
| solarized-dark | 3.25 to 4.53 | 4.69 | 3.25 to 4.53 | 4.75 |
| solarized-light | 4.29 to 4.52 | 2.97 to 4.55 | 4.29 to 4.52 | 2.93 to 4.57 |
| tokyo-night | 6.46 | 9.86 | 6.81 | 9.96 |

Before this change 16 pairs were under 4.5:1: the focused danger button on five themes, the focused success button on two,
the error row on five and the INFO and UNKNOWN badge (one style, so two levels) on two. After it, none is.

On dark-plus the error row and the danger button are not the same pair (`ColorBlack` `#000000` against `button_fg`
`#1e1e1e` on the same `color_error`), hence 4.08 and 3.24 before and 5.68 and 4.51 after. The lowest result is dark-plus danger at 4.51, 0.01 above the bar; it holds, but with no margin.

### Ratios on the application background

`TestErrorOkAndButtonsKeepTheirRatioOnTheBackground` holds the figure each key had before the change as a floor, on all 13
themes, for `ColorError`, `ColorOK`, `ColorButtonBg` and `ColorButtonInactiveBg`. `button_bg` and `button_inactive_bg` do
not move on any theme.

## Guards that stayed green

- `TestSeverityHighAndCriticalStayApart` (CIE76 of at least 8): no `severity_critical` or `severity_high` moved.
- `TestTokenColorsAreNotSeverityOrGaugeColours` (delta E of at least 20 from `ColorOK` and `severity_info`, among
  others): the darkened `color_ok` of latte and solarized-light, and the darkened `severity_info` of solarized-light,
  stay 20 away from every token colour. The test is unchanged and no token colour moved.
- `TestGuardNoHexColorOutsideColorsGo`: hexadecimals only in `themes/*.json`.

## Part B: the unfocused danger and success buttons

The label of an unfocused "Yes" button was `ColorError` (danger) or `ColorOK` (success) on `button_inactive_bg`, 1.55 to
4.89:1. That background is shared with the primary and secondary buttons (the "No" labels), so it does not move on any
theme: the label moves, through two new theme keys, `button_danger_fg` and `button_success_fg`, read only by the unfocused
branches of `ButtonStyle`. A theme that omits a key keeps `ColorError` or `ColorOK`, as before (`applyColor` fallback).
`color_error`, `color_ok`, the focused buttons and `button_inactive_bg` are unchanged (`TestTheButtonBackgroundsDoNotMoveInPartB`).

| | |
|---|---|
| Method | as above; the "before" figures are the tree with part A and without part B, the "after" ones this tree. The label is read from `ButtonStyle(false, "danger")` and `ButtonStyle(false, "success")`, the "No" label from `ButtonStyle(false, "primary")` (`button_inactive_fg`) |
| Thresholds | 4.5:1 for the label on `button_inactive_bg` (`TestEveryTextPairReachesItsContrastOnEveryTheme`, "danger button, unfocused" and "success button, unfocused"); CIE76 delta E of at least 8 between the new label and the "No" label (`TestTheUnfocusedYesLabelsStayApartFromTheNoLabel`); the ratio may not drop (`TestTheUnfocusedDangerAndSuccessLabelsDoNotLoseContrast`) |
| Retuning rule | text only, same hue and HSL saturation, lightness moved by the smallest step that reaches 4.5:1 on `button_inactive_bg` (`minimalTone`; `suggestForPair` never proposes to move the shared background, `TestTextOnlyPairsSuggestOnlyTheText`). A key is written only where the label was under 4.5:1 |

### Ratios and keys, before and after

`dE old` is the distance from the previous label (`ColorError` or `ColorOK`) to the key. `dE No` is the distance to the "No" label.
A dash means no key: the label already held and stays `ColorError` or `ColorOK`.

| Theme | danger before | danger after | key `button_danger_fg` | dE old | dE No | success before | success after | key `button_success_fg` | dE old | dE No |
|---|---|---|---|---|---|---|---|---|---|---|
| catppuccin-frappe | 2.08 | 4.54 | #fae4e5 | 43.5 | 12.6 | 3.18 | 4.52 | #ddeed2 | 28.6 | 23.7 |
| catppuccin-latte | 2.51 | 4.59 | #880a25 | 27.6 | 56.9 | 2.40 | 4.62 | #1f4c14 | 25.7 | 51.5 |
| catppuccin-macchiato | 2.51 | 4.59 | #f9d5da | 34.9 | 19.2 | 3.86 | 4.52 | #c3e6b7 | 14.6 | 40.3 |
| catppuccin-mocha | 2.88 | 4.50 | #f9c7d5 | 27.6 | 23.0 | 4.49 | 4.54 | #a8e4a3 | 0.8 | 53.8 |
| dark-plus | 1.55 | 4.53 | #f7dfdf | 57.1 | 8.8 | 2.32 | 4.54 | #93f8d1 | 31.7 | 39.4 |
| default | 2.88 | 4.50 | #f9c7d5 | 27.6 | 23.0 | 4.49 | 4.54 | #a8e4a3 | 0.8 | 53.8 |
| github-dark | 3.64 | 4.50 | #f9746e | 18.9 | 64.0 | 4.80 | 4.80 | - | - | 74.1 |
| github-light | 3.69 | 4.54 | #b51e28 | 9.6 | 74.7 | 3.50 | 4.61 | #166a2e | 10.5 | 55.0 |
| nord | 2.68 | 4.57 | #e7c3c6 | 23.2 | 17.4 | 3.62 | 4.54 | #bed1ae | 11.9 | 25.7 |
| one-dark | 1.96 | 4.52 | #f7d7da | 45.3 | 12.9 | 3.00 | 4.54 | #d2e5c5 | 28.6 | 20.2 |
| solarized-dark | 1.62 | 4.53 | #fbe7e6 | 59.5 | 10.9 | 1.68 | 4.52 | #d9fa00 | 44.7 | 87.2 |
| solarized-light | 1.82 | 4.58 | #6a1312 | 44.1 | 49.8 | 1.84 | 4.63 | #313800 | 35.1 | 34.9 |
| tokyo-night | 3.38 | 4.51 | #f99fb0 | 19.4 | 41.4 | 4.89 | 4.89 | - | - | 76.9 |

Before part B, 13 of 13 themes had a danger label and 11 of 13 a success label under 4.5:1 (mocha and `default` at 4.49,
just under); after it none is. The tightest results are the mocha and `default` danger label at 4.50 and the github-dark
danger label at 4.50. The tightest distance to the "No" label is dark-plus danger, 8.8 (bar 8.0).

Mocha and `default` success move by 0.8, which is the smallest step that crosses 4.5:1 from 4.49; the key is still
written so that the pair is held by a value and not by a rounding.

### Where the part A and part B moves come closest to another colour

Computed distances, no terminal read. The part A moves of `color_error` and `color_ok` brought two pairs of theme colours
closer, both still far above the 8 that `TestSeverityHighAndCriticalStayApart` asks of the severity pair:

- solarized-light `color_ok` against `color_warn` (`#836300`): 34.6 before part A, 25.0 now (`#859900` to `#687800`).
- nord `color_error` against `color_primary` (`#b691af`): 32.2 before part A, 20.1 now (`#bf616a` to `#cf898f`).

## Not done here

- The severity counts of the `:sec` inventory under the cursor, and the rail under the cursor, as in
  `2026-11-24-theme-text-contrast-3.md`.
- No terminal was read by eye: the figures are computed.
