package app

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func TestTerminalBackgroundByProfile(t *testing.T) {
	bg := lipgloss.Color("#1e1e2e")

	t.Run("TrueColor keeps the exact colour", func(t *testing.T) {
		got := terminalBackground(colorprofile.TrueColor, bg)
		if got == nil || theme.Hex(got) != "#1e1e2e" {
			t.Fatalf("got %v, want #1e1e2e", got)
		}
	})

	t.Run("ANSI256 converts to the colour the painted cells get", func(t *testing.T) {
		got := terminalBackground(colorprofile.ANSI256, bg)
		want := colorprofile.ANSI256.Convert(bg)
		if got != want {
			t.Fatalf("got %#v, want %#v", got, want)
		}
		if _, ok := got.(ansi.IndexedColor); !ok {
			t.Fatalf("got %T, want ansi.IndexedColor", got)
		}
		if got == color.Color(bg) {
			t.Fatal("ANSI256 result equals the exact colour: a seam would show against painted cells")
		}
	})

	for _, tc := range []struct {
		name    string
		profile colorprofile.Profile
		bg      color.Color
	}{
		{"ANSI (16 colours)", colorprofile.ANSI, bg},
		{"ASCII (NO_COLOR)", colorprofile.ASCII, bg},
		{"NoTTY", colorprofile.NoTTY, bg},
		{"Unknown (zero value)", colorprofile.Unknown, bg},
		{"nil colour", colorprofile.TrueColor, nil},
		{"NoColor", colorprofile.TrueColor, lipgloss.NoColor{}},
		{"nil colour, ANSI256", colorprofile.ANSI256, nil},
	} {
		t.Run("nil for "+tc.name, func(t *testing.T) {
			if got := terminalBackground(tc.profile, tc.bg); got != nil {
				t.Fatalf("got %#v, want nil", got)
			}
		})
	}
}

// TestTerminalBackgroundIsComparableOnEveryShippedTheme: the renderer compares
// View.BackgroundColor with !=, so a non-comparable dynamic type would panic.
func TestTerminalBackgroundIsComparableOnEveryShippedTheme(t *testing.T) {
	files, err := filepath.Glob("../../themes/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no theme found under themes/*.json")
	}
	themes := map[string]*theme.Theme{"default": theme.DefaultTheme()}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var th theme.Theme
		if err := json.Unmarshal(data, &th); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		themes[strings.TrimSuffix(filepath.Base(f), ".json")] = &th
	}
	if want := len(files) + 1; len(themes) != want {
		t.Fatalf("got %d themes, want %d (default + %d files)", len(themes), want, len(files))
	}
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })

	for name, th := range themes {
		for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			t.Run(name+"/"+p.String(), func(t *testing.T) {
				theme.ApplyTheme(theme.DefaultTheme())
				theme.ApplyTheme(th)
				got := terminalBackground(p, theme.ColorBackground)
				if got == nil {
					t.Fatal("got nil, want a colour")
				}
				if !reflect.TypeOf(got).Comparable() {
					t.Fatalf("%T is not comparable", got)
				}
				_ = got == terminalBackground(p, theme.ColorBackground) // would panic if not comparable
			})
		}
	}
}

// bgOf returns the terminal background the router asks for, as a hex string or
// "<nil>" when it leaves the terminal alone.
func bgOf(a *App) string {
	c := a.View().BackgroundColor
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%#v", c)
}

func sendProfile(t *testing.T, a *App, p colorprofile.Profile) {
	t.Helper()
	a.Update(tea.ColorProfileMsg{Profile: p})
}

func TestRouterSetsTheTerminalBackgroundFromTheProfile(t *testing.T) {
	theme.ApplyTheme(theme.DefaultTheme())
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })

	wantTrue := fmt.Sprintf("%#v", theme.ColorBackground)
	wantIdx := fmt.Sprintf("%#v", colorprofile.ANSI256.Convert(theme.ColorBackground))

	cases := []struct {
		name    string
		profile colorprofile.Profile
		want    string
	}{
		{"TrueColor", colorprofile.TrueColor, wantTrue},
		{"ANSI256", colorprofile.ANSI256, wantIdx},
		{"ANSI", colorprofile.ANSI, "<nil>"},
		{"ASCII", colorprofile.ASCII, "<nil>"},
		{"NoTTY", colorprofile.NoTTY, "<nil>"},
	}

	for _, withOverlay := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if withOverlay {
				name += "/palette open"
			}
			t.Run(name, func(t *testing.T) {
				a := routerAt(t, &fakeView{body: "hello"}, 80, 24)
				if withOverlay {
					press(t, a, "ctrl+p")
				}
				if got := bgOf(a); got != "<nil>" {
					t.Fatalf("before any ColorProfileMsg: got %s, want nil", got)
				}
				before := a.View().Content

				sendProfile(t, a, tc.profile)

				v := a.View()
				if got := bgOf(a); got != tc.want {
					t.Errorf("got %s, want %s", got, tc.want)
				}
				if v.Content != a.Render() {
					t.Error("View().Content differs from Render()")
				}
				if v.Content != before {
					t.Error("a ColorProfileMsg changed the rendered content")
				}
				if !v.AltScreen {
					t.Error("AltScreen lost")
				}
			})
		}
	}
}

func TestRouterSetsTheTerminalBackgroundWithAnOverlayOpen(t *testing.T) {
	theme.ApplyTheme(theme.DefaultTheme())
	a := routerAt(t, &fakeView{}, 80, 24)
	sendProfile(t, a, colorprofile.TrueColor)
	a.handleKeyMsg(testutil.Key("?"))
	if !a.showHelp {
		t.Fatal("help did not open")
	}
	if got, want := bgOf(a), fmt.Sprintf("%#v", theme.ColorBackground); got != want {
		t.Errorf("help overlay open: got %s, want %s", got, want)
	}
	if v := a.View(); v.Content != a.Render() {
		t.Error("View().Content differs from Render() with the overlay open")
	}
}

func TestRouterFollowsTheLatestProfile(t *testing.T) {
	theme.ApplyTheme(theme.DefaultTheme())
	a := routerAt(t, &fakeView{body: "hello"}, 80, 24)
	sendProfile(t, a, colorprofile.TrueColor)
	if bgOf(a) == "<nil>" {
		t.Fatal("TrueColor: want a colour")
	}
	sendProfile(t, a, colorprofile.ASCII)
	if got := bgOf(a); got != "<nil>" {
		t.Errorf("TrueColor then ASCII: got %s, want nil", got)
	}
	sendProfile(t, a, colorprofile.TrueColor)
	if bgOf(a) == "<nil>" {
		t.Error("ASCII then TrueColor: want a colour again")
	}
}

func TestColorProfileMsgDoesNotReachTheActiveView(t *testing.T) {
	fv := &fakeView{body: "hello"}
	a := routerAt(t, fv, 80, 24)
	sendProfile(t, a, colorprofile.TrueColor)
	for _, m := range fv.received {
		if _, ok := m.(tea.ColorProfileMsg); ok {
			t.Error("the router forwarded a ColorProfileMsg to the active view")
		}
	}
}

// TestOnlyTheRouterSetsTheTerminalBackground: the terminal's background is
// terminal state, so like AltScreen only the router (internal/app/view.go)
// sets it. Any other assignment or composite-literal field named
// BackgroundColor in non-test code is reported with file:line.
func TestOnlyTheRouterSetsTheTerminalBackground(t *testing.T) {
	const owner = "internal/app/view.go"
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var offences []string
	ownerSets := false

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		report := func(pos token.Pos) {
			if rel == owner {
				ownerSets = true
				return
			}
			offences = append(offences, fset.Position(pos).String())
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "BackgroundColor" {
						report(sel.Pos())
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && id.Name == "BackgroundColor" {
					report(id.Pos())
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offences {
		t.Errorf("%s sets BackgroundColor; only %s may (the router owns terminal state)", o, owner)
	}
	if !ownerSets {
		t.Errorf("%s never assigns BackgroundColor: the guard would pass vacuously", owner)
	}
}

// writeTheme writes a valid theme named name under the isolated HOME's theme
// directory, with the given background.
func writeTheme(t *testing.T, name, background string) {
	t.Helper()
	dir, err := theme.ThemeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	th := theme.DefaultTheme()
	th.Name = name
	th.ColorBackground = background
	data, err := json.Marshal(th)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func bgHex(a *App) string {
	c := a.View().BackgroundColor
	if c == nil {
		return "<nil>"
	}
	return theme.Hex(c)
}

func saveTheme(a *App, name string) {
	cfg := testConfig()
	cfg.App.Theme = name
	a.Update(configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})
}

func resetThemeAfter(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		theme.ApplyTheme(theme.DefaultTheme())
		theme.CurrentThemeName = "default"
	})
	theme.ApplyTheme(theme.DefaultTheme())
	theme.CurrentThemeName = "default"
}

// DEVDESK-209 step 3: the terminal background follows the theme.
func TestTheTerminalBackgroundFollowsTheTheme(t *testing.T) {
	a := router(t, &fakeView{body: "hello"}) // isolates HOME
	resetThemeAfter(t)
	writeTheme(t, "light", "#ffffff")
	sendProfile(t, a, colorprofile.TrueColor)
	if got := bgHex(a); got != "#1e1e2e" {
		t.Fatalf("default theme: got %s, want #1e1e2e", got)
	}

	saveTheme(a, "light")
	if got := bgHex(a); got != "#ffffff" {
		t.Errorf("after switching to the light theme: got %s, want #ffffff", got)
	}

	saveTheme(a, "default")
	if got := bgHex(a); got != "#1e1e2e" {
		t.Errorf("after switching back to the default theme: got %s, want #1e1e2e", got)
	}
}

func TestARefusedThemeKeepsThePreviousTerminalBackground(t *testing.T) {
	a := router(t, &fakeView{body: "hello"})
	resetThemeAfter(t)
	writeTheme(t, "light", "#ffffff")
	writeTheme(t, "broken", "#ggg")
	sendProfile(t, a, colorprofile.TrueColor)

	saveTheme(a, "broken")
	if got := bgHex(a); got != "#1e1e2e" {
		t.Errorf("refused theme from default: got %s, want #1e1e2e kept", got)
	}

	saveTheme(a, "light")
	saveTheme(a, "broken")
	if got := bgHex(a); got != "#ffffff" {
		t.Errorf("refused theme from light: got %s, want #ffffff kept", got)
	}
}

func TestAThemeSwitchOnANonTrueColorProfile(t *testing.T) {
	a := router(t, &fakeView{body: "hello"})
	resetThemeAfter(t)
	writeTheme(t, "light", "#ffffff")

	sendProfile(t, a, colorprofile.ASCII)
	saveTheme(a, "light")
	if got := bgHex(a); got != "<nil>" {
		t.Errorf("ASCII: got %s, want nil", got)
	}

	sendProfile(t, a, colorprofile.ANSI256)
	// theme.Hex does not read an indexed colour, so compare real RGBA values.
	got := a.View().BackgroundColor
	if got == nil {
		t.Fatal("ANSI256 after the switch: background is nil")
	}
	wr, wg, wb, wa := colorprofile.ANSI256.Convert(theme.ColorBackground).RGBA()
	gr, gg, gb, ga := got.RGBA()
	if gr != wr || gg != wg || gb != wb || ga != wa {
		t.Errorf("ANSI256 after the switch: got %v %v %v %v, want %v %v %v %v", gr, gg, gb, ga, wr, wg, wb, wa)
	}
}

func themeSaved(cfg *config.Config) tea.Msg {
	return configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true}
}
