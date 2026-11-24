# Text contrast, part A of DEVDESK-770: the colours the retune of DEVDESK-764 left under the bar

Measurement note of 2026-11-24. It follows `2026-11-24-theme-text-contrast.md`, which is frozen (it is the state after
DEVDESK-764 and its "Not done here" is what this note works from). Nothing in that note is rewritten here.

| | |
|---|---|
| Method | `theme.ContrastRatio(foreground, surface)` (WCAG relative luminance), after `ApplyTheme(DefaultTheme())` then `ApplyTheme(<theme>)` for the default theme and each file of `themes/*.json`, read by a throwaway test that walks `textInks()` and `textPairs()` of `internal/ui/theme/text_contrast_test.go` (twice: on the commit before this change and on this one). The guards that hold the result are `TestEveryTextColourReachesItsContrastOnEveryTheme` and `TestEveryTextPairReachesItsContrastOnEveryTheme` |
| Thresholds | 4.5:1 for text (WCAG 1.4.3); 3:1 for a glyph (the activity dot, the icons: WCAG 1.4.11) and for the frame border (non-text object, WCAG 1.4.11) |
| Surfaces | as in the 2026-11-24 note: background, selected row (`ColorTableLineSelected`), raised surface (`ColorCmdLineBg`); a pair is a foreground on the background of its own component |
| Retuning rule | same hue and same HSL saturation, lightness moved by steps of 0.005 away from the surface, by the smallest step that holds every surface (`minimalToneFor` in `tune_test.go`); for a pair, the side (text or surface) whose delta E (CIE76) is smaller, except the unfocused buttons: the text moves first and the button background keeps its tone, so the button shape does not regress (review of 2026-11-24) |
| Themes | the 13 shipped ones. `default` and `catppuccin-mocha` are the same palette |

## What this change holds

| Colour or pair | Where it is painted | Needs |
|---|---|---|
| `ColorSecondary` | header keys, activity dot, update mark, icons | 4.5:1 on the background, 3:1 on the selected row |
| `ColorTableHeaderFg` | table header | 4.5:1 on the background |
| `ColorPrimary`, `ColorTitleFg`, `ColorAppTitleFg` | the logo, titles | 4.5:1 on the background |
| `ColorViewportBorder` | the frame border | 3:1 on the background |
| active tab | `ColorTabActiveFg` on `ColorTabActiveBg` | 4.5:1 |
| primary button, focused | `ColorButtonFg` on `ColorButtonBg` | 4.5:1 |
| primary and secondary button, unfocused | `ColorButtonInactiveFg` on `ColorButtonInactiveBg` | 4.5:1 |
| command line, inactive | `ColorCmdLineInactiveFg` on `ColorCmdLineBg` | 4.5:1 |

Before this change 65 of the held rows below (13 themes, 25 held rows each) were under their threshold, in all 13 themes (frappe 4, latte 7,
macchiato 3, mocha 1, dark-plus 11, default 1, github-dark 2, github-light 2, nord 6, one-dark 4, solarized-dark 11,
solarized-light 11, tokyo-night 2). After it, none.

**The decision on the frame border.** The user decided that the border reaches 3:1 on all 13 themes: it lightens on
the dark themes and darkens on the light ones. That departs from the third-party palettes on 10 themes (all but default, mocha and macchiato, whose border was
already at 3.36:1, 3.36:1 and 3.15:1): github-dark `#30363d` and
github-light `#d0d7de` are the largest moves (delta E 18.7 and 25.1), since GitHub's own border tone is a hairline
by design. `color_border` follows `viewport_border` (the same hex on every theme; `viewport_border` falls back on it).

## Ratios, before and after

Each cell is the ratio after the change when it did not move, and `before to after` when it did. Every cell reaches
the "need" of its row after the change. Rows marked "(not held)" are shown for information: they are not held by a
test, and they are the ones that carry `ColorError`, `ColorOK` or `ColorPrimary` on the inactive button background
(see "Not done here").

| Pair | catppuccin-frappe | catppuccin-latte | catppuccin-macchiato | catppuccin-mocha | dark-plus |
|---|---|---|---|---|---|
| ColorText on background — need 4.5:1 | 8.06 | 7.06 | 9.92 | 11.34 | 10.38 |
| ColorText on selected row — need 4.5:1 | 6.19 | 5.17 | 7.55 | 8.69 | 13.08 |
| ColorText on raised surface — need 4.5:1 | 6.19 | 5.17 | 7.55 | 8.69 | 13.08 |
| ColorDim on background — need 4.5:1 | 5.89 | 6.18 | 5.95 | 5.89 | 4.52 |
| ColorDim on selected row — need 4.5:1 | 4.53 | 4.52 | 4.53 | 4.52 | 5.69 |
| ColorDim on raised surface — need 4.5:1 | 4.53 | 4.52 | 4.53 | 4.52 | 5.69 |
| ColorSubTitleFg on background — need 4.5:1 | 6.72 | 4.51 | 8.17 | 9.17 | 4.51 |
| ColorWarn on background — need 4.5:1 | 5.89 | 6.19 | 7.62 | 9.27 | 12.34 |
| ColorWarn on selected row — need 4.5:1 | 4.52 | 4.54 | 5.79 | 7.10 | 15.55 |
| ColorFooterWarn on background — need 4.5:1 | 5.80 | 4.51 | 7.62 | 9.27 | 12.34 |
| ColorFooterWarn on selected row — need 3.0:1 | 4.45 | 3.30 | 5.79 | 7.10 | 15.55 |
| ColorFooterError on background — need 4.5:1 | 4.65 | 4.80 | 5.96 | 7.08 | 4.51 |
| ColorFooterError on selected row — need 3.0:1 | 3.57 | 3.52 | 4.54 | 5.43 | 5.68 |
| ColorSecondary on background — need 4.5:1 | 6.72 | 4.34 → 4.57 | 8.17 | 9.17 | 3.43 → 4.56 |
| ColorSecondary on selected row — need 3.0:1 | 5.16 | 3.18 → 3.35 | 6.21 | 7.03 | 4.32 → 5.75 |
| ColorTableHeaderFg on background — need 4.5:1 | 6.72 | 2.81 → 4.57 | 8.17 | 9.17 | 3.43 → 4.56 |
| ColorPrimary on background — need 4.5:1 | 5.60 | 4.79 | 6.84 | 8.07 | 3.63 → 4.57 |
| ColorTitleFg on background — need 4.5:1 | 5.60 | 4.79 | 6.84 | 8.07 | 3.63 → 4.57 |
| ColorAppTitleFg on background — need 4.5:1 | 5.60 | 4.79 | 6.84 | 8.07 | 3.63 → 4.57 |
| frame border (`ColorViewportBorder`) on background — need 3.0:1 | 2.87 → 3.03 | 2.30 → 3.03 | 3.15 | 3.36 | 2.90 → 3.04 |
| active tab (ColorTabActiveFg on ColorTabActiveBg) — need 4.5:1 | 6.72 | 2.81 → 4.57 | 8.17 | 9.17 | 3.43 → 4.56 |
| primary button, focused — need 4.5:1 | 5.60 | 4.79 | 6.84 | 8.07 | 3.63 → 4.57 |
| primary button, unfocused — need 4.5:1 | 3.61 → 4.55 | 3.69 → 4.56 | 4.17 → 4.55 | 4.62 | 3.58 → 4.52 |
| secondary button, unfocused — need 4.5:1 | 3.61 → 4.55 | 3.69 → 4.56 | 4.17 → 4.55 | 4.62 | 3.58 → 4.52 |
| command line, inactive (ColorCmdLineInactiveFg on raised surface) — need 4.5:1 | 1.72 → 4.53 | 1.40 → 4.52 | 1.81 → 4.53 | 1.88 → 4.52 | 3.66 → 5.69 |
| danger button, unfocused (not held) | 2.08 | 2.51 | 2.51 | 2.88 | 1.12 |
| success button, unfocused (not held) | 3.18 | 1.55 | 3.86 | 4.49 | 2.32 |
| default button, unfocused (not held) | 2.50 | 2.50 | 2.88 | 3.29 | 1.25 → 1.57 |
| unfocused button shape (button background on app background, not held) | 2.23 | 1.91 | 2.38 | 2.46 | 2.90 |

| Pair | default | github-dark | github-light | nord |
|---|---|---|---|---|
| ColorText on background — need 4.5:1 | 11.34 | 12.26 | 15.80 | 9.25 |
| ColorText on selected row — need 4.5:1 | 8.69 | 11.21 | 14.84 | 7.45 |
| ColorText on raised surface — need 4.5:1 | 8.69 | 11.21 | 14.84 | 7.45 |
| ColorDim on background — need 4.5:1 | 5.89 | 6.15 | 5.25 | 5.61 |
| ColorDim on selected row — need 4.5:1 | 4.52 | 5.62 | 4.93 | 4.52 |
| ColorDim on raised surface — need 4.5:1 | 4.52 | 5.62 | 4.93 | 4.52 |
| ColorSubTitleFg on background — need 4.5:1 | 9.17 | 7.49 | 5.19 | 4.64 |
| ColorWarn on background — need 4.5:1 | 9.27 | 7.50 | 4.87 | 8.00 |
| ColorWarn on selected row — need 4.5:1 | 7.10 | 6.85 | 4.57 | 6.44 |
| ColorFooterWarn on background — need 4.5:1 | 9.27 | 7.50 | 4.87 | 8.00 |
| ColorFooterWarn on selected row — need 3.0:1 | 7.10 | 6.85 | 4.57 | 6.44 |
| ColorFooterError on background — need 4.5:1 | 7.08 | 5.65 | 5.36 | 4.54 |
| ColorFooterError on selected row — need 3.0:1 | 5.43 | 5.16 | 5.03 | 3.65 |
| ColorSecondary on background — need 4.5:1 | 9.17 | 7.49 | 5.19 | 4.64 |
| ColorSecondary on selected row — need 3.0:1 | 7.03 | 6.85 | 4.88 | 3.74 |
| ColorTableHeaderFg on background — need 4.5:1 | 9.17 | 7.49 | 5.19 | 4.64 |
| ColorPrimary on background — need 4.5:1 | 8.07 | 5.64 | 5.05 | 4.41 → 4.55 |
| ColorTitleFg on background — need 4.5:1 | 8.07 | 5.64 | 5.05 | 4.41 → 4.55 |
| ColorAppTitleFg on background — need 4.5:1 | 8.07 | 5.64 | 5.05 | 4.41 → 4.55 |
| frame border (`ColorViewportBorder`) on background — need 3.0:1 | 3.36 | 1.55 → 3.00 | 1.45 → 3.00 | 1.69 → 3.01 |
| active tab (ColorTabActiveFg on ColorTabActiveBg) — need 4.5:1 | 9.17 | 7.49 | 5.19 | 4.64 |
| primary button, focused — need 4.5:1 | 8.07 | 5.64 | 5.05 | 4.41 → 4.55 |
| primary button, unfocused — need 4.5:1 | 4.62 | 7.91 | 10.88 | 5.46 |
| secondary button, unfocused — need 4.5:1 | 4.62 | 7.91 | 10.88 | 5.46 |
| command line, inactive (ColorCmdLineInactiveFg on raised surface) — need 4.5:1 | 1.88 → 4.52 | 1.42 → 5.62 | 1.36 → 4.93 | 1.36 → 4.52 |
| danger button, unfocused (not held) | 2.88 | 3.64 | 3.69 | 1.80 |
| success button, unfocused (not held) | 4.49 | 4.80 | 3.50 | 3.62 |
| default button, unfocused (not held) | 3.29 | 3.64 | 3.47 | 2.60 → 2.69 |
| unfocused button shape (button background on app background, not held) | 2.46 | 1.55 | 1.45 | 1.69 |

| Pair | one-dark | solarized-dark | solarized-light | tokyo-night |
|---|---|---|---|---|
| ColorText on background — need 4.5:1 | 6.57 | 5.61 | 5.18 | 10.59 |
| ColorText on selected row — need 4.5:1 | 6.11 | 4.86 | 4.56 | 11.16 |
| ColorText on raised surface — need 4.5:1 | 6.11 | 4.86 | 4.56 | 11.16 |
| ColorDim on background — need 4.5:1 | 4.88 | 5.22 | 5.13 | 4.55 |
| ColorDim on selected row — need 4.5:1 | 4.54 | 4.52 | 4.51 | 4.80 |
| ColorDim on raised surface — need 4.5:1 | 4.54 | 4.52 | 4.51 | 4.80 |
| ColorSubTitleFg on background — need 4.5:1 | 5.92 | 4.51 | 4.52 | 6.79 |
| ColorWarn on background — need 4.5:1 | 8.10 | 5.21 | 5.19 | 8.55 |
| ColorWarn on selected row — need 4.5:1 | 7.54 | 4.51 | 4.57 | 9.01 |
| ColorFooterWarn on background — need 4.5:1 | 8.10 | 4.68 | 4.51 | 8.55 |
| ColorFooterWarn on selected row — need 3.0:1 | 7.54 | 4.05 | 3.97 | 9.01 |
| ColorFooterError on background — need 4.5:1 | 4.53 | 4.53 | 4.52 | 6.46 |
| ColorFooterError on selected row — need 3.0:1 | 4.22 | 3.92 | 3.98 | 6.81 |
| ColorSecondary on background — need 4.5:1 | 5.92 | 4.08 → 4.56 | 3.41 → 4.53 | 6.79 |
| ColorSecondary on selected row — need 3.0:1 | 5.51 | 3.53 → 3.95 | 3.00 → 3.99 | 7.16 |
| ColorTableHeaderFg on background — need 4.5:1 | 5.92 | 4.08 → 4.56 | 3.41 → 4.53 | 6.79 |
| ColorPrimary on background — need 4.5:1 | 4.75 | 3.43 → 4.56 | 4.06 → 4.56 | 7.39 |
| ColorTitleFg on background — need 4.5:1 | 4.75 | 3.43 → 4.56 | 4.06 → 4.56 | 7.39 |
| ColorAppTitleFg on background — need 4.5:1 | 4.75 | 3.43 → 4.56 | 4.06 → 4.56 | 7.39 |
| frame border (`ColorViewportBorder`) on background — need 3.0:1 | 1.79 → 3.04 | 2.79 → 3.07 | 2.48 → 3.01 | 1.91 → 3.03 |
| active tab (ColorTabActiveFg on ColorTabActiveBg) — need 4.5:1 | 5.92 | 4.08 → 4.56 | 3.41 → 4.53 | 6.79 |
| primary button, focused — need 4.5:1 | 4.75 | 3.43 → 4.56 | 4.06 → 4.56 | 7.39 |
| primary button, unfocused — need 4.5:1 | 2.84 → 4.52 | 1.70 → 4.99 | 1.67 → 4.56 | 5.53 |
| secondary button, unfocused — need 4.5:1 | 2.84 → 4.52 | 1.70 → 4.99 | 1.67 → 4.56 | 5.53 |
| command line, inactive (ColorCmdLineInactiveFg on raised surface) — need 4.5:1 | 2.16 → 4.54 | 2.42 → 4.52 | 2.18 → 4.51 | 2.02 → 4.80 |
| danger button, unfocused (not held) | 1.89 | 1.16 | 1.73 | 3.38 |
| success button, unfocused (not held) | 3.00 | 1.68 | 1.20 | 4.89 |
| default button, unfocused (not held) | 2.05 | 1.23 → 1.64 | 1.64 → 1.84 | 3.86 |
| unfocused button shape (button background on app background, not held) | 2.32 | 2.79 | 2.48 | 1.91 |


## What was retouched, and delta E

Key, before, after and the CIE76 distance of each move. A key listed with several names moved as one (the names
are the keys that fed each other: `color_secondary` feeds the header keys, the table header and the active tab
when they are absent, `color_primary` the titles and the primary button; here they are all written in the theme
files, so each has its own line in the JSON and they were moved together to keep the family one colour).

| Theme | Key | Before | After | ΔE (CIE76) |
|---|---|---|---|---|
| default | `cmd_line_inactive_fg` | #585b70 | #979aad | 24.9 |
| catppuccin-frappe | `button_inactive_fg` | #c6d0f5 | #e4e9fa | 13.8 |
| catppuccin-frappe | `cmd_line_inactive_fg` | #626880 | #afb3c2 | 29.4 |
| catppuccin-frappe | `viewport_border` and `color_border` | #737994 | #777d97 | 1.7 |
| catppuccin-latte | `button_inactive_fg` | #4c4f69 | #3f4257 | 6.4 |
| catppuccin-latte | `cmd_line_inactive_fg` | #acb0be | #55586e | 34.5 |
| catppuccin-latte | `color_secondary` | #1e66f5 | #1761f5 | 3.9 |
| catppuccin-latte | `table_header_fg`, `tab_active_bg` | #7287fd | #3d5afc | 32.2 |
| catppuccin-latte | `viewport_border` and `color_border` | #9ca0b0 | #858a9e | 8.7 |
| catppuccin-macchiato | `button_inactive_fg` | #cad3f5 | #d5dcf7 | 4.9 |
| catppuccin-macchiato | `cmd_line_inactive_fg` | #5b6078 | #a0a4b7 | 26.8 |
| catppuccin-mocha | `cmd_line_inactive_fg` | #585b70 | #979aad | 24.9 |
| dark-plus | `button_inactive_fg` | #cccccc | #e4e4e4 | 8.5 |
| dark-plus | `cmd_line_inactive_fg` | #666666 | #858585 | 12.3 |
| dark-plus | `color_primary`, `title_fg`, `app_title_fg`, `button_bg` | #bc3fbc | #c85ac8 | 9.7 |
| dark-plus | `color_secondary`, `table_header_fg`, `tab_active_bg` | #2472c8 | #3c88dc | 8.9 |
| dark-plus | `viewport_border` and `color_border` | #666666 | #696969 | 1.2 |
| github-dark | `cmd_line_inactive_fg` | #30363d | #8b949e | 38.6 |
| github-dark | `viewport_border` and `color_border` | #30363d | #56616e | 18.7 |
| github-light | `cmd_line_inactive_fg` | #d0d7de | #656d76 | 40.0 |
| github-light | `viewport_border` and `color_border` | #d0d7de | #8597a9 | 25.1 |
| nord | `cmd_line_inactive_fg` | #4c566a | #a6aebf | 34.7 |
| nord | `color_primary`, `title_fg`, `app_title_fg`, `button_bg` | #b48ead | #b691af | 1.2 |
| nord | `viewport_border` and `color_border` | #4c566a | #6f7d98 | 16.1 |
| one-dark | `button_inactive_fg` | #abb2bf | #dcdfe4 | 17.0 |
| one-dark | `cmd_line_inactive_fg` | #5c6370 | #9299a5 | 21.3 |
| one-dark | `viewport_border` and `color_border` | #4b5263 | #6b758d | 14.8 |
| solarized-dark | `button_inactive_fg` | #839496 | #fdf6e3 | 39.4 |
| solarized-dark | `cmd_line_inactive_fg` | #586e75 | #869ca4 | 18.0 |
| solarized-dark | `color_primary`, `title_fg`, `app_title_fg`, `button_bg` | #6c71c4 | #8488cd | 12.4 |
| solarized-dark | `color_secondary`, `table_header_fg`, `tab_active_bg` | #268bd2 | #3094da | 3.6 |
| solarized-dark | `viewport_border` and `color_border` | #586e75 | #5d757c | 2.8 |
| solarized-light | `button_inactive_fg` | #657b83 | #2d373b | 28.2 |
| solarized-light | `cmd_line_inactive_fg` | #93a1a1 | #576c73 | 21.5 |
| solarized-light | `color_primary`, `title_fg`, `app_title_fg`, `button_bg` | #6c71c4 | #6368c0 | 4.8 |
| solarized-light | `color_secondary`, `table_header_fg`, `tab_active_bg` | #268bd2 | #2076b2 | 9.8 |
| solarized-light | `viewport_border` and `color_border` | #93a1a1 | #819292 | 6.0 |
| tokyo-night | `cmd_line_inactive_fg` | #414868 | #7982ab | 24.0 |
| tokyo-night | `viewport_border` and `color_border` | #414868 | #5b6592 | 14.1 |

Notes on the choices:

- `cmd_line_inactive_fg` is aligned on `color_dim` on every theme (the same hex), as `tab_inactive_fg` is
  (Rule 118), instead of the smallest step of `tune_test.go`. The two differ by 1.1 delta E or less on 11 themes
  (default included), by 5.9 on github-dark and 6.3 on dark-plus; aligning removes a third tone of inactive text that
  could drift. `ColorCmdLineInactiveFg` is not read by any view today (`grep`), so no frame changes with it.
- **Unfocused buttons: the text moved, never the button background.** `button_inactive_bg` is unchanged on all 13
  themes, so the shape of the unfocused button against the app background is identical before and after (row
  "unfocused button shape" above: 1.45 on github-light to 2.90 on dark-plus, the same figures on the commit before).
  A first version moved the background where it cost the smaller delta E; it lowered that shape (solarized-dark
  2.79 to 1.05, one-dark 2.32 to 1.45, frappe 2.23 to 1.77, dark-plus 2.90 to 2.24) and was dropped. In every
  case the 4.5:1 of the label was reachable by the text alone, so no theme needs its background moved:
  `button_inactive_fg` frappe `#c6d0f5` to `#e4e9fa` (13.8), macchiato `#cad3f5` to `#d5dcf7` (4.9), dark-plus
  `#cccccc` to `#e4e4e4` (8.5), one-dark `#abb2bf` to `#dcdfe4` (17.0), solarized-dark `#839496` to `#fdf6e3` (39.4,
  Solarized's base3 rather than the smallest step, `#e9eced`, 33.6; 4.99:1 on `#586e75`), latte `#4c4f69` to
  `#3f4257` (6.4) and solarized-light `#657b83` to `#2d373b` (28.2). The danger, success and default unfocused
  buttons keep the background they had, so they do not change either (rows "not held": only the solarized-light
  default button moves, 1.64 to 1.84, because `color_primary` moved there).
- catppuccin-latte keeps its lavender family for the table header and the active tab, darkened: `#7287fd` to
  `#3d5afc` (same hue 231, same saturation). It is now the same blue as `subtitle_fg` (`#3f5bfc`, DEVDESK-764) within
  delta E 0.7, so the header and the subtitle read as one colour. Assumed gap: the same holds, by a delta E of 0.8, 0.8 and 0.6, for
  `color_secondary` and `subtitle_fg` on dark-plus, solarized-dark and solarized-light (`subtitle_fg` had been retuned
  to the same step by DEVDESK-764).
- `color_secondary` of latte (header keys) moved by 3.9 only (4.34 to 4.57 on the background, 3.18 to 3.35 on the
  selected row), the table header and the tab did not follow it, as before: they were two different colours in the file.
- default and mocha: only `cmd_line_inactive_fg` (24.9). Their `color_secondary` and `color_primary` already hold.

## Visual review

Read from the SGR, not from a screenshot (same method as the 2026-11-24 note): the router at 140x45 on `:ws`, `:sec`,
`:agent-usage`, `:configuration` (empty states) and a `ConfirmModal` with each button focused, decoded with
`testutil.Cells`; every (foreground, background) pair under 4.5:1 for a visible glyph, the frame lines and block
glyphs excluded (3:1 objects). The whole result, with the pairs, is in the body of the pull request. Pairs under
4.5:1 on `default`, `nord`, `catppuccin-latte` and `solarized-light`:

| Theme | Before | After |
|---|---|---|
| default | the danger button (`Yes`) unfocused, 2.88 | the same |
| nord | `ColorPrimary` `#b48ead` on the background, 4.41 (the logo and the titles; four views and the modal); `Yes` pairs 3.05 and 1.80; `No` focused 4.41 | the `Yes` pairs, 3.05 and 1.80 |
| catppuccin-latte | `ColorSecondary` 4.34 (header keys); the table header and the label on the active tab 2.81; `No` unfocused 3.69; `Yes` 2.51 | `Yes` unfocused 2.51 |
| solarized-light | `ColorSecondary` 3.41; `ColorPrimary` 4.06; `No` unfocused 1.67 and focused 4.06; `Yes` pairs 1.73 and 4.29 | `Yes` pairs 1.73 and 4.29 |

On the 13 themes, no pair of the four views is left under 4.5:1 (the frame excluded); every pair left is the `Yes`
(danger) button of the modal, unfocused (`ColorError` on `ColorButtonInactiveBg`, unchanged from before) or focused (`ColorButtonFg` on
`ColorError`: dark-plus 3.24, nord 3.05, solarized-dark 3.25, solarized-light 4.29, one-dark 4.38). All of them are in
"Not done here".

## Not done here

Left under their threshold, deliberately, for the next part of DEVDESK-770 or a decision:

- **The LOW and HIGH badges** (part B): `severity_low_fg` on `severity_low` (1.2 to 3.6:1) and `severity_high_fg` on
  `severity_high` (one-dark 2.95, solarized-dark 3.26, catppuccin-latte 3.48), and `ColorSeverityLow` as a text colour.
  `ColorSeverityHigh` and `ColorSeverityLow` were not touched.
- **The gauge rail** (`theme.GaugeTrackStyle()`, aliasing `ColorSeverityLow`): untouched, its rule is Rule 122's.
- **Secondary text told apart by more than colour**, on solarized-dark, solarized-light and catppuccin-latte, where
  `ColorDim` and `ColorText` are within delta E 5.6, 0.8 and 4.6: out of scope by decision of the user.
- **The `Yes` / success buttons**, unfocused (`ColorError` or `ColorOK` on `ColorButtonInactiveBg`: 1.1 to 4.5:1,
  from the table above) and focused (`ColorButtonFg` or `ColorBlack` on `ColorError` or `ColorOK`: dark-plus 3.24,
  nord 3.05, solarized-dark 3.25, solarized-light 4.29, one-dark 4.38). They need their own surface or their own pair
  per theme, not one tone.
- **INFO and UNKNOWN** (`severity_info_fg` on `severity_info`, and the UNKNOWN badge): not measured here.
- `ColorCmdLineInactiveFg` has no reader in the views (checked with `grep`): the pair is held so that a view which
  uses it later starts from a readable tone.
