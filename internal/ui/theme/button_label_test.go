package theme

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DEVDESK-774 part B (D183, parts A and B). The label of the unfocused danger and success
// buttons (ColorError and ColorOK on button_inactive_bg) is 1.55:1 to 4.89:1. The
// correction is two theme keys, button_danger_fg and button_success_fg, used by
// the unfocused branches of ButtonStyle only; the button background
// (button_inactive_bg) is shared with the primary and secondary buttons and does
// not move on any theme, the focused text and the global color_error and
// color_ok do not change either.
//
// These tests read the two keys by their JSON names, through the theme loader
// path (json.Unmarshal into Theme, then ApplyTheme), and read the colours the
// button really paints through ButtonStyle, so they do not depend on how the
// fields are named.

const (
	dangerFgKey  = "button_danger_fg"
	successFgKey = "button_success_fg"
)

// inactiveBgBefore774B is button_inactive_bg of every shipped theme as the files
// stood before part B (themes/*.json read on 2026-11-25, default being
// catppuccin-mocha). The correction may not move any of them.
var inactiveBgBefore774B = map[string]string{
	"catppuccin-frappe":    "#626880",
	"catppuccin-latte":     "#acb0be",
	"catppuccin-macchiato": "#5b6078",
	"catppuccin-mocha":     "#585b70",
	"dark-plus":            "#666666",
	"default":              "#585b70",
	"github-dark":          "#30363d",
	"github-light":         "#d0d7de",
	"nord":                 "#4c566a",
	"one-dark":             "#5c6370",
	"solarized-dark":       "#586e75",
	"solarized-light":      "#93a1a1",
	"tokyo-night":          "#414868",
}

// unfocusedLabelBefore774B is the ratio of the unfocused danger and success
// label on the button background, before part B (ContrastRatio after
// ApplyTheme(DefaultTheme()) then ApplyTheme(<theme>), measured on 2026-11-25 on
// the tree with part A and without part B), rounded to two decimals.
type labelRatios struct{ danger, success float64 }

var unfocusedLabelBefore774B = map[string]labelRatios{
	"catppuccin-frappe":    {2.08, 3.18},
	"catppuccin-latte":     {2.51, 2.40},
	"catppuccin-macchiato": {2.51, 3.86},
	"catppuccin-mocha":     {2.88, 4.49},
	"dark-plus":            {1.55, 2.32},
	"default":              {2.88, 4.49},
	"github-dark":          {3.64, 4.80},
	"github-light":         {3.69, 3.50},
	"nord":                 {2.68, 3.62},
	"one-dark":             {1.96, 3.00},
	"solarized-dark":       {1.62, 1.68},
	"solarized-light":      {1.82, 1.84},
	"tokyo-night":          {3.38, 4.89},
}

// buttonVariants are the variants ButtonStyle knows besides "disabled", and the
// empty one that falls in its default branch.
var buttonVariants = []string{"primary", "secondary", "danger", "success", ""}

// The background of an unfocused button is button_inactive_bg for every variant,
// and part B must not touch it: neither the value on any theme nor the style.
// The focused backgrounds and texts stay what part A left (D183).
func TestTheButtonBackgroundsDoNotMoveInPartB(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		want, ok := inactiveBgBefore774B[name]
		if !ok {
			t.Fatalf("theme %s has no entry in inactiveBgBefore774B", name)
		}
		if got := Hex(ColorButtonInactiveBg); got != want {
			t.Errorf("theme %s: button_inactive_bg is %s, was %s before part B: the label moves, not the background", name, got, want)
		}
		for _, v := range buttonVariants {
			if got := ButtonStyle(false, v).GetBackground(); !sameColor(got, ColorButtonInactiveBg) {
				t.Errorf("theme %s: unfocused %q button background is %s, want ColorButtonInactiveBg %s", name, v, Hex(got), Hex(ColorButtonInactiveBg))
			}
		}
		focused := []struct {
			variant string
			fg, bg  color.Color
		}{
			{"danger", ColorButtonFg, ColorError},
			{"success", ColorBlack, ColorOK},
			{"primary", ColorButtonFg, ColorButtonBg},
			{"", ColorButtonFg, ColorButtonBg},
		}
		for _, f := range focused {
			s := ButtonStyle(true, f.variant)
			if !sameColor(s.GetBackground(), f.bg) || !sameColor(s.GetForeground(), f.fg) {
				t.Errorf("theme %s: focused %q button is %s on %s, want %s on %s", name, f.variant,
					Hex(s.GetForeground()), Hex(s.GetBackground()), Hex(f.fg), Hex(f.bg))
			}
		}
	})
	t.Run("every shipped theme has a measure", func(t *testing.T) {
		for name := range shippedThemes(t) {
			if _, ok := inactiveBgBefore774B[name]; !ok {
				t.Errorf("theme %s has no entry in inactiveBgBefore774B", name)
			}
			if _, ok := unfocusedLabelBefore774B[name]; !ok {
				t.Errorf("theme %s has no entry in unfocusedLabelBefore774B", name)
			}
		}
	})
}

// The label of an unfocused Yes button may only gain contrast on the button.
func TestTheUnfocusedDangerAndSuccessLabelsDoNotLoseContrast(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		before, ok := unfocusedLabelBefore774B[name]
		if !ok {
			t.Fatalf("theme %s has no entry in unfocusedLabelBefore774B", name)
		}
		for _, c := range []struct {
			variant string
			floor   float64
		}{{"danger", before.danger}, {"success", before.success}} {
			s := ButtonStyle(false, c.variant)
			ratio, err := ContrastRatio(s.GetForeground(), s.GetBackground())
			if err != nil {
				t.Errorf("theme %s: unfocused %s button: %v", name, c.variant, err)
				continue
			}
			if ratio < c.floor-roundingSlack {
				t.Errorf("theme %s: unfocused %s button label %s on %s is %.2f:1, it was %.2f:1 before part B and must not drop",
					name, c.variant, Hex(s.GetForeground()), Hex(s.GetBackground()), ratio, c.floor)
			}
		}
	})
}

// minYesNoGap is the smallest CIE76 distance between the label of an unfocused
// "No" button (button_inactive_fg) and the new danger or success label. Both sit
// on the same background and a light pastel red is the cheapest way to 4.5:1 on a
// dark theme: the bar keeps the Yes label from collapsing onto the No label. The
// architect measured the tightest proposal (dark-plus danger, #f7dfdf) at 8.8,
// and the labels are told apart by their words as well.
const minYesNoGap = 8.0

func TestTheUnfocusedYesLabelsStayApartFromTheNoLabel(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		no := ButtonStyle(false, "primary").GetForeground()
		for _, v := range []string{"danger", "success"} {
			yes := ButtonStyle(false, v).GetForeground()
			d, err := ColorDistance(yes, no)
			if err != nil {
				t.Errorf("theme %s: %s: %v", name, v, err)
				continue
			}
			if d < minYesNoGap {
				t.Errorf("theme %s: the unfocused %s label %s and the No label %s (button_inactive_fg) are delta E %.1f apart, want >= %.1f",
					name, v, Hex(yes), Hex(no), d, minYesNoGap)
			}
		}
	})
}

// themeFileKeys reads themes/<file>.json as a bare key/value map.
func themeFileKeys(t *testing.T, file string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "themes", file+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return m
}

// themeFromKeys turns a key/value map back into a Theme the way LoadTheme does
// (JSON into the struct), so a key is read by its JSON name and nothing else.
func themeFromKeys(t *testing.T, m map[string]any) *Theme {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var th Theme
	if err := json.Unmarshal(data, &th); err != nil {
		t.Fatal(err)
	}
	return &th
}

func shippedThemeFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "themes", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no theme file found: %v", err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".json"))
	}
	return names
}

// A theme file that carries neither key (every file written before part B, and
// every user theme) paints the unfocused label in ColorError and ColorOK, as
// before: applyColor falls back on them. Characterisation, green before part B.
func TestAThemeWithoutTheButtonLabelKeysFallsBackOnErrorAndOK(t *testing.T) {
	for _, file := range shippedThemeFiles(t) {
		t.Run(file, func(t *testing.T) {
			keys := themeFileKeys(t, file)
			delete(keys, dangerFgKey)
			delete(keys, successFgKey)
			ApplyTheme(DefaultTheme())
			withTheme(t, themeFromKeys(t, keys))
			if got := ButtonStyle(false, "danger").GetForeground(); !sameColor(got, ColorError) {
				t.Errorf("theme %s without %s: unfocused danger label is %s, want ColorError %s", file, dangerFgKey, Hex(got), Hex(ColorError))
			}
			if got := ButtonStyle(false, "success").GetForeground(); !sameColor(got, ColorOK) {
				t.Errorf("theme %s without %s: unfocused success label is %s, want ColorOK %s", file, successFgKey, Hex(got), Hex(ColorOK))
			}
		})
	}
}

// A theme that writes the keys sees them on the unfocused button and nowhere
// else: not on the focused buttons, not on the primary one, and not on
// ColorError and ColorOK themselves.
func TestTheButtonLabelKeysPaintTheUnfocusedDangerAndSuccessLabels(t *testing.T) {
	const danger, success = "#123456", "#fedcba"
	for _, file := range shippedThemeFiles(t) {
		t.Run(file, func(t *testing.T) {
			plain := themeFileKeys(t, file)
			delete(plain, dangerFgKey)
			delete(plain, successFgKey)
			ApplyTheme(DefaultTheme())
			withTheme(t, themeFromKeys(t, plain))
			errBefore, okBefore := Hex(ColorError), Hex(ColorOK)
			type painted struct{ fg, bg string }
			read := func(focused bool, v string) painted {
				s := ButtonStyle(focused, v)
				return painted{Hex(s.GetForeground()), Hex(s.GetBackground())}
			}
			others := map[string]painted{
				"focused danger":    read(true, "danger"),
				"focused success":   read(true, "success"),
				"unfocused primary": read(false, "primary"),
			}

			withKeys := themeFileKeys(t, file)
			withKeys[dangerFgKey], withKeys[successFgKey] = danger, success
			ApplyTheme(DefaultTheme())
			withTheme(t, themeFromKeys(t, withKeys))

			if got := Hex(ButtonStyle(false, "danger").GetForeground()); got != danger {
				t.Errorf("theme %s: %s is %s, the unfocused danger label is %s", file, dangerFgKey, danger, got)
			}
			if got := Hex(ButtonStyle(false, "success").GetForeground()); got != success {
				t.Errorf("theme %s: %s is %s, the unfocused success label is %s", file, successFgKey, success, got)
			}
			for _, v := range []string{"danger", "success"} {
				if got, want := Hex(ButtonStyle(false, v).GetBackground()), Hex(ColorButtonInactiveBg); got != want {
					t.Errorf("theme %s: unfocused %s background is %s, want button_inactive_bg %s", file, v, got, want)
				}
			}
			if Hex(ColorError) != errBefore || Hex(ColorOK) != okBefore {
				t.Errorf("theme %s: the keys moved ColorError or ColorOK (%s, %s to %s, %s)", file, errBefore, okBefore, Hex(ColorError), Hex(ColorOK))
			}
			for what, want := range others {
				var got painted
				switch what {
				case "focused danger":
					got = read(true, "danger")
				case "focused success":
					got = read(true, "success")
				default:
					got = read(false, "primary")
				}
				if got != want {
					t.Errorf("theme %s: the %s button changed with the keys: %v to %v", file, what, want, got)
				}
			}
		})
	}
}

// The embedded default theme carries a value for both keys (omitempty drops an
// empty field from the marshalled form, so absence here means not set).
func TestTheDefaultThemeDefinesTheButtonLabelKeys(t *testing.T) {
	data, err := json.Marshal(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{dangerFgKey, successFgKey} {
		if v, _ := m[k].(string); v == "" {
			t.Errorf("DefaultTheme() does not define %s", k)
		}
	}
}
