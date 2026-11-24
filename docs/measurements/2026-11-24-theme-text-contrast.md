# Text contrast of the six text colours on the 13 shipped themes, after the retune (DEVDESK-764)

Measurement note of 2026-11-24. It follows `2026-11-23-sec-detail-contrast.md`, which is frozen and found
`ColorDim`, `ColorSubTitleFg`, `ColorWarn` and `ColorFooterError` under 4.5:1 on several themes. This note is the
state after DEVDESK-764 retuned them. A dated measure is never corrected afterwards: if the palette changes again,
add a new note.

| | |
|---|---|
| Method | `theme.ContrastRatio(foreground, surface)` (WCAG relative luminance), after `ApplyTheme(DefaultTheme())` then `ApplyTheme(<theme>)` for the default theme and each file of `themes/*.json` (harness `eachShippedTheme`). The figures below were printed by a throwaway test; the guard that holds them is `TestEveryTextColourReachesItsContrastOnEveryTheme` (`internal/ui/theme/text_contrast_test.go`) |
| Threshold | 4.5:1 for text (WCAG 1.4.3); 3:1 for a glyph (WCAG 1.4.11), see "Surfaces" |
| Themes | the 13 shipped ones. `default` and `catppuccin-mocha` are the same palette |

## Surfaces retained

A text colour is measured on every surface it is really painted on, not on `ColorBackground` alone.

| Surface | Why | Measured for |
|---|---|---|
| `ColorBackground` | under everything (Rule 115): the footer line, the header, the title lines, plain table cells | all six |
| `ColorTableLineSelected` | the plain "normal" selection keeps each cell's own foreground and repaints this background under it (Rule 118, Rule 122): any colour a table cell can take is read on it | `ColorText`, `ColorDim`, `ColorWarn` at 4.5:1; `ColorFooterWarn`, `ColorFooterError` at 3:1 (there they are the activity dot and the severity glyph, not text) |
| `ColorCmdLineBg` | the chart, the palette, the disabled button; the same hex as the selected row on all 13 themes but its own key | `ColorText`, `ColorDim` |
| `ColorFooterError`, `ColorFooterWarn` on the selected row at 4.5:1 | **not required**: they are also the CRITICAL and MEDIUM badge backgrounds, and a text-only requirement there would retune the severity palette | |

`ColorSubTitleFg` is only ever a heading on `ColorBackground`.

Also held, for the same reason: the label of an inactive tab (`ColorTabInactiveFg` on `ColorTabInactiveBg`, Rule 118
says it is `ColorDim`) and the label of a disabled button (see "Inactive components").

## Ratios after

Each cell is `on ColorBackground`; where the colour has a second requirement, the lowest ratio over its other surfaces
follows after a slash (4.5:1 surfaces for `ColorText`, `ColorDim`, `ColorWarn`; the 3:1 glyph surface for
`ColorFooterWarn` and `ColorFooterError`).

| Theme | `ColorText` | `ColorDim` | `ColorSubTitleFg` | `ColorWarn` | `ColorFooterWarn` | `ColorFooterError` |
|---|---|---|---|---|---|---|
| catppuccin-frappe | 8.06 / 6.19 | 5.89 / 4.53 | 6.72 | 5.89 / 4.52 | 5.80 / 4.45 | 4.65 / 3.57 |
| catppuccin-latte | 7.06 / 5.17 | 6.18 / 4.52 | 4.51 | 6.19 / 4.54 | 4.51 / 3.30 | 4.80 / 3.52 |
| catppuccin-macchiato | 9.92 / 7.55 | 5.95 / 4.53 | 8.17 | 7.62 / 5.79 | 7.62 / 5.79 | 5.96 / 4.54 |
| catppuccin-mocha | 11.34 / 8.69 | 5.89 / 4.52 | 9.17 | 9.27 / 7.10 | 9.27 / 7.10 | 7.08 / 5.43 |
| dark-plus | 10.38 / 10.38 | 4.52 / 4.52 | 4.51 | 12.34 / 12.34 | 12.34 / 12.34 | 4.51 / 4.51 |
| default | 11.34 / 8.69 | 5.89 / 4.52 | 9.17 | 9.27 / 7.10 | 9.27 / 7.10 | 7.08 / 5.43 |
| github-dark | 12.26 / 11.21 | 6.15 / 5.62 | 7.49 | 7.50 / 6.85 | 7.50 / 6.85 | 5.65 / 5.16 |
| github-light | 15.80 / 14.84 | 5.25 / 4.93 | 5.19 | 4.87 / 4.57 | 4.87 / 4.57 | 5.36 / 5.03 |
| nord | 9.25 / 7.45 | 5.61 / 4.52 | 4.64 | 8.00 / 6.44 | 8.00 / 6.44 | 4.54 / 3.65 |
| one-dark | 6.57 / 6.11 | 4.88 / 4.54 | 5.92 | 8.10 / 7.54 | 8.10 / 7.54 | 4.53 / 4.22 |
| solarized-dark | 5.61 / 4.86 | 5.22 / 4.52 | 4.51 | 5.21 / 4.51 | 4.68 / 4.05 | 4.53 / 3.92 |
| solarized-light | 5.18 / 4.56 | 5.13 / 4.51 | 4.52 | 5.19 / 4.57 | 4.51 / 3.97 | 4.52 / 3.98 |
| tokyo-night | 10.59 / 10.59 | 4.55 / 4.55 | 6.79 | 8.55 / 8.55 | 8.55 / 8.55 | 6.46 / 6.46 |

Before (2026-11-23, on `ColorBackground` only): `ColorDim` under 4.5 on 9 themes (default 2.46, nord 1.69),
`ColorSubTitleFg` on latte 2.81, dark-plus 3.43, solarized-dark 4.08, solarized-light 3.41, `ColorWarn` on latte 2.64
and solarized-light 2.98, `ColorFooterError` on dark-plus 3.24, nord 3.73, one-dark 4.38, solarized-dark 3.49,
solarized-light 4.29. Measured on the real surfaces, `ColorDim` was also under on the selected row of all 13 themes but
the two github ones, and `ColorWarn` on the selected row of frappe (4.45), latte (1.93), solarized-dark (4.05) and
solarized-light (2.62).

## Inactive components

| Theme | inactive tab | disabled button, unfocused | disabled button, focused |
|---|---|---|---|
| catppuccin-frappe | 4.53 | 4.53 | 4.53 |
| catppuccin-latte | 4.52 | 4.52 | 4.52 |
| catppuccin-macchiato | 4.53 | 4.53 | 4.53 |
| catppuccin-mocha | 4.52 | 4.52 | 4.52 |
| dark-plus | 5.69 | 5.69 | 5.69 |
| default | 4.52 | 4.52 | 4.52 |
| github-dark | 5.62 | 5.62 | 5.62 |
| github-light | 4.93 | 4.93 | 4.93 |
| nord | 4.52 | 4.52 | 4.52 |
| one-dark | 4.54 | 4.54 | 4.54 |
| solarized-dark | 4.52 | 4.52 | 4.52 |
| solarized-light | 4.51 | 4.51 | 4.51 |
| tokyo-night | 4.80 | 4.80 | 4.80 |

Before: the inactive tab was `tab_inactive_fg` on its own, between 1.36:1 (nord) and 3.66:1 (dark-plus); the disabled
button painted `ColorDim` on `ColorButtonInactiveBg`, the same hex on nine themes, that is 1.00:1 (a label that cannot
be read). It now paints `ColorDim` on `ColorCmdLineBg`, and the two colours swap under focus.

## What was retouched

Third-party palettes depart from their official palette for the faulty text colours only, as decided: the same hue and
saturation (HSL), the lightness moved by the smallest step that reaches 4.5:1 on every surface above, away from the
background. Hex literals stay in `themes/*.json` and in `DefaultTheme` (`manager.go`).

| Theme | Retouched (before to after) |
|---|---|
| default, catppuccin-mocha | `color_dim` #585b70 to #979aad; `tab_inactive_fg` the same |
| catppuccin-frappe | `color_dim` #626880 to #afb3c2; `color_warn` #ef9f76 to #efa178 |
| catppuccin-macchiato | `color_dim` #5b6078 to #a0a4b7 |
| catppuccin-latte | `color_dim` #5c5f77 to #55586e; `subtitle_fg` #7287fd to #3f5bfc; `color_warn` #fe640b to #9c3901 |
| dark-plus | `color_dim` #666666 to #858585; `subtitle_fg` #2472c8 to #3b87dc; `severity_critical` #cd3131 to #d85e5e |
| github-dark, github-light | none |
| nord | `color_dim` #4c566a to #a6aebf; `severity_critical` #c7757d to #cf898f |
| one-dark | `color_dim` #5c6370 to #9299a5; `severity_critical` #e06c75 to #e17079 |
| solarized-dark | `color_dim` #586e75 to #869ca4; `subtitle_fg` #268bd2 to #3093da; `color_warn` #b58900 to #c09100; `severity_critical` #de3f3c to #e46562 |
| solarized-light | `color_dim` #586e75 to #576c73; `subtitle_fg` #268bd2 to #2076b3; `color_warn` #b58900 to #836300; `severity_critical` #dc322f to #da2825 |
| tokyo-night | `color_dim` #565f89 to #7982ab |

`tab_inactive_fg` follows `color_dim` on every theme that had set it to something else (all 11 non-github ones; the
github ones already equal it). `severity_critical` doubles as the CRITICAL badge background: its `_fg` pairing went from
3.24 to 4.51 (dark-plus), 3.73 to 4.54 (nord), 4.38 to 4.53 (one-dark), 3.49 to 4.53 (solarized-dark) and 4.29 to 4.52
(solarized-light), so the badge improved too.

**Knock-on, not text.** Raising `ColorDim` put it within a delta E of 20 of the token bar's grey, which
`TestTokenColorsAreNotSeverityOrGaugeColours` forbids. `token_cache_read` moved to a lighter neutral on nine themes
(frappe #979eba to #e8eaf0, macchiato #9198b6 to #d8dae5, mocha and default #9399b2 to #cdd0db, dark-plus #9e9e9e to
#bbbbbb, nord #d8dee9 to #dfe4ed, one-dark #8f9aa3 to #cacfd3, solarized-dark #8fa3a2 to #c4cfce, tokyo-night #9391b6 to
#b1b0ca), and on nord `token_input` #889abf to #7b91bf and `token_output` #b18ba5 to #bb81a9 (against the new dim and
the new critical).

## The distance between `ColorDim` and `ColorText`

On the selected row `ColorDim` has to stay at 4.5:1, so it cannot be much darker than the text. On the dark catppuccin
themes the gap stays wide (mocha: 11.34 against 5.89 on the background). On the two solarized themes it was already
narrow and now is almost closed (solarized-dark: text 5.61, dim 5.22; solarized-light: text 5.18, dim 5.13). The
palettes' own base tones are that close; the hierarchy there rests on weight, italics and position, not on the shade.

## Visual review

Read from the SGR, not from a screenshot: a throwaway test built the router on `:ws`, `:sec`, `:agent-usage` and
`:configuration` (empty states, 140x45, no fixtures), decoded the frame with `testutil.Cells` and listed every
(foreground, background) pair under 4.5:1 for a visible glyph. Pairs left, outside the six colours and the border
(a frame line, 3:1 at most, untouched):

| Theme | Before | After |
|---|---|---|
| nord | dim #4c566a on #2e3440 (1.69) and on #3b4252 (1.36, inactive tab); `ColorPrimary` #b48ead on the background (4.41, the logo) | the logo only |
| catppuccin-latte | `ColorSecondary` #1e66f5 (4.34, header keys); `table_header_fg` #7287fd (2.81); inactive tab #acb0be on #ccd0da (1.40); a #df8e1d icon (2.31); the label on the #7287fd tab | all but the inactive tab |
| solarized-light | `ColorSecondary` #268bd2 (3.41); `ColorPrimary` #6c71c4 (4.06); inactive tab #93a1a1 (2.18); a #2aa198 icon (2.93) | all but the inactive tab |

The audit ran on every theme: catppuccin-frappe, macchiato and mocha, one-dark and tokyo-night went from 5 pairs to
none; nord went from 9 pairs to 4,
catppuccin-latte from 11 to 9, solarized-light from 11 to 10, dark-plus and solarized-dark from 14 to 9 each. What
remains on dark-plus, solarized-dark and solarized-light is `ColorSecondary` (#2472c8, #268bd2), `ColorPrimary`
(#bc3fbc, #6c71c4) and the label on the active tab. The github themes had none to begin with. A pair is counted once
per distinct (foreground, background) over the four views, the frame border excluded.

## Not done here

Text colours that are not among the six and still fall under 4.5:1 on some themes: `ColorSecondary` (header keys, table
header, active tab background: latte 4.34, dark-plus 3.43, solarized-dark 4.08, solarized-light 3.41),
`table_header_fg` on latte (#7287fd, 2.81), `ColorPrimary` / `ColorTitleFg` (nord 4.41, dark-plus 3.63, solarized-dark
3.43, solarized-light 4.06), `ColorSeverityLow` as a text colour (it kept the old dim tone, because it is also the LOW
badge background), `button_inactive_fg` on `button_inactive_bg` (solarized-light 2.09), and the frame border (1.69 on
nord, 2.30 on latte, under the 3:1 non-text threshold).
