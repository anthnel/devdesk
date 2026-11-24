# Text contrast, part B of DEVDESK-770: the severity badges and the gauge rail

Measurement note of 2026-11-24. It follows `2026-11-24-theme-text-contrast.md` and `2026-11-24-theme-text-contrast-2.md`,
which are frozen: the second one lists the LOW and HIGH badges and the gauge rail under "Not done here", and this note
is what closes them. Nothing in those two notes is rewritten here. The decision is D181 in `docs/backlog.md`.

| | |
|---|---|
| Method | `theme.ContrastRatio(foreground, surface)` and `theme.ColorDistance` (CIE76 delta E), after `ApplyTheme(DefaultTheme())` then `ApplyTheme(<theme>)` for the default theme and each file of `themes/*.json`, read by a throwaway test (deleted) run on the commit before this change (themes read with `git show HEAD:themes/<name>.json`; for `default`, the old LOW pair `#585b70` / `#cdd6f4`) and on this one. The guards that hold the result are `TestEverySeverityBadgeReachesTextContrastOnEveryTheme`, `TestSeverityBadgeTextIsTheAppBackgroundOnEveryTheme`, `TestSeverityHighAndCriticalStayApart` (threshold 8), `TestEveryTextColourReachesItsContrastOnEveryTheme` (`ColorSeverityHigh` and `ColorSeverityLow`), `TestAThemeWithoutSeverityLowFallsBackOnDimAndBackground` and `TestTheGaugeTrackIsTheViewportBorderOnEveryTheme` |
| Thresholds | 4.5:1 for the badge text on the badge, and for the badge colour on the background when it is painted as text (WCAG 1.4.3); 3:1 for the badge colour on the selected row (a glyph, Rule 118); CIE76 delta E of at least 8 between CRITICAL and HIGH. No ratio is held for the rail |
| Retuning rule | HIGH: same hue and same HSL saturation, lightness moved by steps of 0.005 away from the surfaces, by the smallest step (`minimalToneFor`, `tune_test.go`). LOW: `severity_low` is `color_dim` (already held to 4.5:1). The text of every badge is `color_background`. Tokens: same hue, lightness and saturation moved to the nearest colour (smallest delta E) that holds every guard of `tokencolors_test.go` |
| Themes | the 13 shipped ones. `default` and `catppuccin-mocha` are the same palette |

## What changed

| Key | Themes | Moves |
|---|---|---|
| `severity_high` | latte, one-dark, solarized-dark, nord, solarized-light | raised to 4.5:1 for the badge text |
| `severity_low` | the eight dark themes (frappe, macchiato, mocha, dark-plus, nord, one-dark, solarized-dark, tokyo-night) and `default` | set to `color_dim` (github-dark already was; the three light themes already held) |
| `severity_low_fg` | all 13 | set to `color_background` |
| `GaugeTrackStyle` | all 13 | `ColorSeverityLow` becomes `ColorViewportBorder` |
| `token_cache_read` / `token_input` | latte, github-light, solarized-light / nord | moved off the new rail tone |
| fallbacks in `ApplyTheme` | a theme that omits the LOW keys | `ColorCmdLineBg` and `ColorText` become `ColorDim` and `ColorBackground` |

Before this change, 5 of 13 themes had a HIGH badge under 4.5:1 (latte 3.48, one-dark 2.95, solarized-dark 3.26,
nord 4.39, solarized-light 4.27), and 10 of 13 a LOW badge under it (from 1.21 on solarized-light to 4.17 on
macchiato). After it, none of the four badges is under 4.5:1 on any theme; the lowest text-on-badge ratios are nord HIGH
and dark-plus LOW, both 4.52.

## Ratios, before and after

Each cell is the value after the change when it did not move, and `before to after` when it did.

### HIGH badge

| Theme | badge colour | badge text | text on badge (need 4.5) | badge colour on background (need 4.5) | badge colour on selected row (need 3) | CRITICAL to HIGH delta E (need 8) |
|---|---|---|---|---|---|---|
| catppuccin-frappe | #ea999c | #303446 | 5.59 | 5.59 | 4.29 | 11.3 |
| catppuccin-latte | #e64553 to #d61d2d | #eff1f5 | 3.48 to 4.56 | 3.48 to 4.56 | 2.55 to 3.34 | 11.7 to 9.0 |
| catppuccin-macchiato | #ee99a0 | #24273a | 6.80 | 6.80 | 5.17 | 8.9 |
| catppuccin-mocha | #eba0ac | #1e1e2e | 7.93 | 7.93 | 6.08 | 14.1 |
| dark-plus | #f14c4c | #1e1e1e | 4.67 | 4.67 | 5.88 | 19.4 |
| default | #eba0ac | #1e1e2e | 7.93 | 7.93 | 6.08 | 14.1 |
| github-dark | #db6d28 | #0d1117 | 5.61 | 5.61 | 5.13 | 28.2 |
| github-light | #bc4c00 | #ffffff | 5.03 | 5.03 | 4.73 | 28.7 |
| nord | #d08770 to #d18a74 | #2e3440 | 4.39 to 4.52 | 4.39 to 4.52 | 3.54 to 3.64 | 16.3 to 15.4 |
| one-dark | #be5046 to #cf7d75 | #282c34 | 2.95 to 4.56 | 2.95 to 4.56 | 2.75 to 4.25 | 17.5 to 14.0 |
| solarized-dark | #cb4b16 to #e8652e | #002b36 | 3.26 to 4.53 | 3.26 to 4.53 | 2.82 to 3.92 | 29.1 to 27.6 |
| solarized-light | #cb4b16 to #c44815 | #fdf6e3 | 4.27 to 4.54 | 4.27 to 4.54 | 3.76 to 4.00 | 18.6 to 19.2 |
| tokyo-night | #ff9e64 | #1a1b26 | 8.40 | 8.40 | 8.86 | 40.9 |

### LOW badge

| Theme | badge colour | badge text | text on badge (need 4.5) | badge colour on background (need 4.5) | badge colour on selected row (need 3) |
|---|---|---|---|---|---|
| catppuccin-frappe | #626880 to #afb3c2 | #c6d0f5 to #303446 | 3.61 to 5.89 | 2.23 to 5.89 | 1.72 to 4.53 |
| catppuccin-latte | #5c5f77 | #4c4f69 to #eff1f5 | 1.28 to 5.53 | 5.53 | 4.05 |
| catppuccin-macchiato | #5b6078 to #a0a4b7 | #cad3f5 to #24273a | 4.17 to 5.95 | 2.38 to 5.95 | 1.81 to 4.53 |
| catppuccin-mocha | #585b70 to #979aad | #cdd6f4 to #1e1e2e | 4.62 to 5.89 | 2.46 to 5.89 | 1.88 to 4.52 |
| dark-plus | #666666 to #858585 | #cccccc to #1e1e1e | 3.58 to 4.52 | 2.90 to 4.52 | 3.66 to 5.69 |
| default | #585b70 to #979aad | #cdd6f4 to #1e1e2e | 4.62 to 5.89 | 2.46 to 5.89 | 1.88 to 4.52 |
| github-dark | #8b949e | #c9d1d9 to #0d1117 | 1.99 to 6.15 | 6.15 | 5.62 |
| github-light | #656d76 | #1f2328 to #ffffff | 3.01 to 5.25 | 5.25 | 4.93 |
| nord | #4c566a to #a6aebf | #d8dee9 to #2e3440 | 5.46 to 5.61 | 1.69 to 5.61 | 1.36 to 4.52 |
| one-dark | #5c6370 to #9299a5 | #abb2bf to #282c34 | 2.84 to 4.88 | 2.32 to 4.88 | 2.16 to 4.54 |
| solarized-dark | #586e75 to #869ca4 | #839496 to #002b36 | 1.70 to 5.22 | 2.79 to 5.22 | 2.42 to 4.52 |
| solarized-light | #586e75 | #657b83 to #fdf6e3 | 1.21 to 4.99 | 4.99 | 4.39 |
| tokyo-night | #565f89 to #7982ab | #c0caf5 to #1a1b26 | 3.83 to 4.55 | 2.76 to 4.55 | 2.91 to 4.80 |

### Gauge rail (empty cells)

| Theme | rail colour | rail on background | rail on selected row |
|---|---|---|---|
| catppuccin-frappe | #626880 to #777d97 | 2.23 to 3.03 | 1.72 to 2.33 |
| catppuccin-latte | #5c5f77 to #858a9e | 5.53 to 3.03 | 4.05 to 2.22 |
| catppuccin-macchiato | #5b6078 to #6e738d | 2.38 to 3.15 | 1.81 to 2.40 |
| catppuccin-mocha | #585b70 to #6c7086 | 2.46 to 3.36 | 1.88 to 2.57 |
| dark-plus | #666666 to #696969 | 2.90 to 3.04 | 3.66 to 3.83 |
| default | #585b70 to #6c7086 | 2.46 to 3.36 | 1.88 to 2.57 |
| github-dark | #8b949e to #56616e | 6.15 to 3.00 | 5.62 to 2.74 |
| github-light | #656d76 to #8597a9 | 5.25 to 3.00 | 4.93 to 2.82 |
| nord | #4c566a to #6f7d98 | 1.69 to 3.01 | 1.36 to 2.43 |
| one-dark | #5c6370 to #6b758d | 2.32 to 3.04 | 2.16 to 2.83 |
| solarized-dark | #586e75 to #5d757c | 2.79 to 3.07 | 2.42 to 2.66 |
| solarized-light | #586e75 to #819292 | 4.99 to 3.01 | 4.39 to 2.65 |
| tokyo-night | #565f89 to #5b6592 | 2.76 to 3.03 | 2.91 to 3.19 |

## Delta E of every key moved (CIE76, before to after)

| Theme | Key | Before | After | Delta E |
|---|---|---|---|---|
| catppuccin-latte | severity_high | #e64553 | #d61d2d | 15.4 |
| one-dark | severity_high | #be5046 | #cf7d75 | 20.5 |
| solarized-dark | severity_high | #cb4b16 | #e8652e | 9.5 |
| nord | severity_high | #d08770 | #d18a74 | 1.6 |
| solarized-light | severity_high | #cb4b16 | #c44815 | 2.6 |
| catppuccin-frappe | severity_low | #626880 | #afb3c2 | 29.4 |
| catppuccin-macchiato | severity_low | #5b6078 | #a0a4b7 | 26.8 |
| catppuccin-mocha, default | severity_low | #585b70 | #979aad | 24.9 |
| dark-plus | severity_low | #666666 | #858585 | 12.3 |
| nord | severity_low | #4c566a | #a6aebf | 34.7 |
| one-dark | severity_low | #5c6370 | #9299a5 | 21.3 |
| solarized-dark | severity_low | #586e75 | #869ca4 | 18.0 |
| tokyo-night | severity_low | #565f89 | #7982ab | 14.0 |
| catppuccin-latte | token_cache_read | #607670 | #5d746e | 1.0 |
| github-light | token_cache_read | #748db4 | #6888bb | 7.3 |
| nord | token_input | #7b91bf | #7f9bd4 | 7.0 |
| solarized-light | token_cache_read | #87836e | #878163 | 5.3 |

`severity_low_fg` moves to `color_background` on all 13 themes (delta E 45.5 to 86.6 from the old text tone): it is the
choice that flips the LOW badge from light-on-dark to dark-on-light (on the dark themes) and dark-on-light to
light-on-dark (on the light ones), which is the visible change of this part. The "default" theme moves its LOW pair
`#585b70` / `#cdd6f4` to `#979aad` / `#1e1e2e` (delta E 24.9 and 74.0). The `severity_low` of github-dark, github-light,
latte and solarized-light does not move.

## The gauge rail

The rail was `ColorSeverityLow`; it is now `ColorViewportBorder` on every theme. The table above ("Gauge rail") gives the
ratios. Read it both ways, because the rail does not improve everywhere:

- on the background, the rail goes from 1.69 to 3.0 or more on the nine dark themes that were under 3:1 (nord 1.69, frappe 2.23, one-dark 2.32, macchiato 2.38, mocha and default 2.46, tokyo-night 2.76, solarized-dark 2.79, dark-plus 2.90), and from 5.0 to 6.2 down to 3.0 on latte, github-light, github-dark and solarized-light: those four rails are **fainter** than before (the LOW tone was a text colour on them);
- on the selected row, the border is under 3:1 on 11 of 13 themes after the change (2.22 to 2.83), against 8 before (1.36 to 2.91). It is not held: a rail is not a carrier of information (the fill, the number and the glyph are), see D181.

## Token colours

`TestTokenColorsAreNotSeverityOrGaugeColours` (delta E of at least 20 from the severity colours, `ColorOK`, `ColorDim`
and the gauge track) failed on four colours once the rail became the border: latte `token_cache_read` `#607670` (19.5 from
`#858a9e`), github-light `token_cache_read` `#748db4` (12.7 from `#8597a9`), nord `token_input` `#7b91bf` (13.1 from
`#6f7d98`), solarized-light `token_cache_read` `#87836e` (15.2 from `#819292`). Each moved by the smallest delta E, hue
kept, to a colour that holds `TestTokenColorsAreDistinct` (20 from the other three), `TestTokenColorsContrastWithBackground`
(3:1 on the background and the selected row) and every other guard; the test is unchanged. Raising HIGH moved no token
(the guard holds for `token_cache_write` of solarized-light `#b8670a` against the new HIGH `#c44815`).

## Not done here

- INFO and UNKNOWN badges (D179 and D181 leave them), the `Yes` and success buttons, the error row (`ColorBlack` on
  `ColorError`: 3.05 on nord, 4.29 on solarized-light).
- The severity counts of the `:sec` inventory under the cursor: HIGH 3.3 (latte), 3.64 (nord), 4.0 (solarized-light),
  CRITICAL 3.5 to 3.98 on the same: the 3:1 glyph threshold of Rule 118, as before.
- The rail under the cursor (2.2 to 2.8:1 on 11 themes), by the decision of D181.
- No terminal was read by eye: the figures are computed, and the SGR before and after of the `:sec` inventory, the CVE
  findings tab, the containers table (gauges) and the dashboard were compared on default, nord, latte and solarized-light
  (the text of each is identical, only the colours moved).
