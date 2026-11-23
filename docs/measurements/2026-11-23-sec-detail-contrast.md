# Text contrast of the colours the `:sec` finding detail could use, on the 13 shipped themes (DEVDESK-751, AC7)

Measurement note of 2026-11-23. It backs the decision of DEVDESK-751 on AC7 ("the Fix block reads at 4.5:1 on every
theme"). A dated measure is never corrected afterwards: if the palette changes, add a new note.

| | |
|---|---|
| Method | `theme.ContrastRatio(foreground, ColorBackground)` (`internal/ui/theme/contrast.go`, WCAG relative luminance), run after `ApplyTheme(DefaultTheme())` then `ApplyTheme(<theme>)` for the default theme and each file of `themes/*.json` (the harness is `eachShippedTheme`, `palette_colors_test.go`). The figures were printed by a throwaway test that was not kept |
| Threshold | 4.5:1, WCAG 1.4.3 for text |
| Themes | the 13 shipped ones. `default` and `catppuccin-mocha` are the same palette |
| Foreground | each colour as the theme sets it (`ColorDim`, `ColorSubTitleFg`, `ColorTitleFg`, `ColorWarn`, `ColorFooterError`, `ColorFooterWarn`, `ColorText`), and the foreground of `DetailLabelStyle` |
| Background | `ColorBackground` of the same theme |

## Ratios

A figure under 4.5 is marked with `*`.

| Theme | `ColorDim` | `ColorSubTitleFg` | `ColorTitleFg` | `ColorWarn` | `ColorFooterError` | `ColorFooterWarn` | `ColorText` | `DetailLabelStyle` |
|---|---|---|---|---|---|---|---|---|
| catppuccin-frappe | 2.23 * | 6.72 | 5.60 | 5.80 | 4.65 | 5.80 | 8.06 | 8.06 |
| catppuccin-latte | 5.53 | 2.81 * | 4.79 | 2.64 * | 4.80 | 4.51 | 7.06 | 7.06 |
| catppuccin-macchiato | 2.38 * | 8.17 | 6.84 | 7.62 | 5.96 | 7.62 | 9.92 | 9.92 |
| catppuccin-mocha | 2.46 * | 9.17 | 8.07 | 9.27 | 7.08 | 9.27 | 11.34 | 11.34 |
| dark-plus | 2.90 * | 3.43 * | 3.63 * | 12.34 | 3.24 * | 12.34 | 10.38 | 10.38 |
| default | 2.46 * | 9.17 | 8.07 | 9.27 | 7.08 | 9.27 | 11.34 | 11.34 |
| github-dark | 6.15 | 7.49 | 5.64 | 7.50 | 5.65 | 7.50 | 12.26 | 12.26 |
| github-light | 5.25 | 5.19 | 5.05 | 4.87 | 5.36 | 4.87 | 15.80 | 15.80 |
| nord | 1.69 * | 4.64 | 4.41 * | 8.00 | 3.73 * | 8.00 | 9.25 | 9.25 |
| one-dark | 2.32 * | 5.92 | 4.75 | 8.10 | 4.38 * | 8.10 | 6.57 | 6.57 |
| solarized-dark | 2.79 * | 4.08 * | 3.43 * | 4.68 | 3.49 * | 4.68 | 5.61 | 5.61 |
| solarized-light | 4.99 | 3.41 * | 4.06 * | 2.98 * | 4.29 * | 4.51 | 5.18 | 5.18 |
| tokyo-night | 2.76 * | 6.79 | 7.39 | 8.55 | 6.46 | 8.55 | 10.59 | 10.59 |

## Reading

| Colour | Themes under 4.5:1 | Count |
|---|---|---|
| `ColorDim` (greyed key, shortcut descriptions, `DimStyle`) | frappe, macchiato, mocha, default, dark-plus, nord, one-dark, solarized-dark, tokyo-night | 9 of 13 (8 palettes, `default` being the mocha one) |
| `ColorSubTitleFg` | latte, dark-plus, solarized-dark, solarized-light | 4 |
| `ColorTitleFg` | dark-plus, nord, solarized-dark, solarized-light | 4 |
| `ColorWarn` | latte, solarized-light | 2 |
| `ColorFooterError` | dark-plus, nord, one-dark, solarized-dark, solarized-light | 5 |
| `ColorFooterWarn` | none (lowest: latte and solarized-light, 4.51) | 0 |
| `ColorText`, `DetailLabelStyle` | none (lowest: solarized-light, 5.18) | 0 |

The architect's plan counted `ColorDim` as "10/13"; the list it gave names nine themes (`default` and `mocha`
counted once as the same palette), and the table above has nine. This note is the measure.

## Decision (option A of the plan)

- The Fix block uses no `ColorDim`, `ColorSubTitleFg`, `ColorTitleFg` or `ColorWarn`. Its labels use
  `theme.DetailLabelStyle` (bold, `ColorText`), all its text is `ColorText`, and the word `Warning:` carries the
  warning. `TestTheFixBlockStylesReachTextContrastOnEveryTheme` (`internal/ui/theme/detail_contrast_test.go`) holds
  4.5:1 on the 13 themes for both.
- `ColorDim` is not changed. It marks a greyed key, an inactive component, which WCAG 1.4.3 exempts, and the reason
  for the greying is shown in the footer at level Warn (`ColorFooterWarn`, 4.51:1 or more on every theme).
- The header and the severity line of the detail keep the colours they had in DEVDESK-750 (`ColorTitleFg` for the ID,
  the severity colours): they are not part of the Fix block.
- **Not done here:** retuning `ColorDim`, `sub_title`, `title`, `warn` and `footer_error` in the themes above. It
  changes the colours of every view and needs a visual review. It is left to a separate story (to be opened by the
  orchestrator: "Retune the palette to 4.5:1 text contrast on the 13 shipped themes").
- AC7 is therefore met for the Fix block, and for the detail's own text, not for the palette as a whole.
