package theme

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

const (
	tokenMinDistance = 20.0 // CIE76 delta E: told apart at a glance
	tokenMinContrast = 3.0  // WCAG non-text contrast
)

var tokenKeys = []string{"token_input", "token_output", "token_cache_write", "token_cache_read"}

// partialTheme returns the default theme with the given token keys replaced and
// every other token key removed, as a user-written theme file would be.
func partialTheme(t *testing.T, name string, set map[string]string) *Theme {
	t.Helper()
	raw, err := json.Marshal(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range tokenKeys {
		delete(m, k)
	}
	for k, v := range set {
		m[k] = v
	}
	raw, _ = json.Marshal(m)
	th := &Theme{}
	if err := json.Unmarshal(raw, th); err != nil {
		t.Fatal(err)
	}
	th.Name = name
	return th
}

// requireTokenRoles fails unless TokenRoles() is exactly the four expected
// roles, so a loop over them can never pass vacuously.
func requireTokenRoles(t *testing.T) []TokenRole {
	t.Helper()
	want := []TokenRole{TokenRoleInput, TokenRoleOutput, TokenRoleCacheWrite, TokenRoleCacheRead}
	roles := TokenRoles()
	if len(roles) != len(want) {
		t.Fatalf("TokenRoles() = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("TokenRoles() = %v, want %v", roles, want)
		}
	}
	return roles
}

func tokenColors(t *testing.T) map[TokenRole]lipgloss.Color {
	t.Helper()
	roles := requireTokenRoles(t)
	out := map[TokenRole]lipgloss.Color{}
	for _, r := range roles {
		out[r] = TokenColor(r)
	}
	return out
}

func TestTokenRolesAreInBarOrder(t *testing.T) {
	want := []TokenRole{TokenRoleInput, TokenRoleOutput, TokenRoleCacheWrite, TokenRoleCacheRead}
	got := TokenRoles()
	if len(got) != len(want) {
		t.Fatalf("TokenRoles() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("TokenRoles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTokenColorsAreDistinct(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		roles := requireTokenRoles(t)
		for i := range roles {
			for j := i + 1; j < len(roles); j++ {
				a, b := TokenColor(roles[i]), TokenColor(roles[j])
				d, err := ColorDistance(a, b)
				if err != nil {
					t.Fatalf("theme %s: %s %q vs %s %q: %v", name, roles[i], a, roles[j], b, err)
				}
				if d < tokenMinDistance {
					t.Errorf("theme %s: %s %s and %s %s are %.1f apart, want >= %.0f",
						name, roles[i], a, roles[j], b, d, tokenMinDistance)
				}
			}
		}
	})
}

func TestTokenColorsContrastWithBackground(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for role, c := range tokenColors(t) {
			for _, bg := range []struct {
				what string
				c    lipgloss.Color
			}{{"background", ColorBackground}, {"selected row", ColorTableLineSelected}} {
				r, err := ContrastRatio(c, bg.c)
				if err != nil {
					t.Fatalf("theme %s: %s %q on %s %q: %v", name, role, c, bg.what, bg.c, err)
				}
				if r < tokenMinContrast {
					t.Errorf("theme %s: %s %s on %s %s is %.2f:1, want >= %.1f:1",
						name, role, c, bg.what, bg.c, r, tokenMinContrast)
				}
			}
		}
	})
}

func TestTokenColorsAreNotSeverityOrGaugeColours(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		others := map[string]lipgloss.Color{
			"severity critical": ColorSeverityCritical,
			"severity high":     ColorSeverityHigh,
			"severity medium":   ColorSeverityMedium,
			"severity low":      ColorSeverityLow,
			"severity info":     ColorSeverityInfo,
			"gauge fill (OK)":   ColorOK,
			"dim":               ColorDim,
			"gauge track":       GaugeTrackStyle().GetForeground().(lipgloss.Color),
		}
		for role, c := range tokenColors(t) {
			for what, o := range others {
				d, err := ColorDistance(c, o)
				if err != nil {
					t.Fatalf("theme %s: %s %q vs %s %q: %v", name, role, c, what, o, err)
				}
				if d < tokenMinDistance {
					t.Errorf("theme %s: %s %s is %.1f from %s %s, want >= %.0f",
						name, role, c, d, what, o, tokenMinDistance)
				}
			}
		}
	})
}

func TestEveryShippedThemeSetsTheTokenKeys(t *testing.T) {
	files, err := filepath.Glob("../../../themes/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no theme found under themes/*.json (err %v)", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, k := range tokenKeys {
			v, _ := m[k].(string)
			if !strings.HasPrefix(v, "#") {
				t.Errorf("%s: key %q missing or not a hex colour (%q)", filepath.Base(f), k, v)
			}
		}
	}
	th := DefaultTheme()
	raw, _ := json.Marshal(th)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	for _, k := range tokenKeys {
		if v, _ := m[k].(string); !strings.HasPrefix(v, "#") {
			t.Errorf("DefaultTheme(): key %q missing or not a hex colour (%q)", k, v)
		}
	}
}

func TestTokenColorFallsBackWhenTheKeyIsMissing(t *testing.T) {
	withTheme(t, partialTheme(t, "no-token-keys", nil))
	want := map[TokenRole]lipgloss.Color{
		TokenRoleInput:      ColorSecondary,
		TokenRoleOutput:     ColorHighlight,
		TokenRoleCacheWrite: ColorPrimary,
		TokenRoleCacheRead:  ColorText,
	}
	for role, w := range want {
		if got := TokenColor(role); got != w {
			t.Errorf("TokenColor(%s) = %q, want fallback %q", role, got, w)
		}
	}
}

func TestTokenColorOneKeyOverridesOnlyThatRole(t *testing.T) {
	withTheme(t, partialTheme(t, "one-key", map[string]string{"token_output": "#123456"}))
	if got := TokenColor(TokenRoleOutput); got != "#123456" {
		t.Errorf("TokenColor(output) = %q, want #123456", got)
	}
	if got := TokenColor(TokenRoleInput); got != ColorSecondary {
		t.Errorf("TokenColor(input) = %q, want fallback ColorSecondary %q", got, ColorSecondary)
	}
}

func TestTokenColorFollowsAThemeChange(t *testing.T) {
	withTheme(t, partialTheme(t, "a", map[string]string{"token_input": "#111111"}))
	if got := TokenColor(TokenRoleInput); got != "#111111" {
		t.Fatalf("TokenColor(input) = %q, want #111111", got)
	}
	ApplyTheme(partialTheme(t, "b", map[string]string{"token_input": "#222222"}))
	if got := TokenColor(TokenRoleInput); got != "#222222" {
		t.Errorf("TokenColor(input) = %q after a theme change, want #222222", got)
	}
}

func TestTokenColorOfAnUnknownRoleIsText(t *testing.T) {
	for _, r := range []TokenRole{"", "bogus"} {
		if got := TokenColor(r); got != ColorText || got == "" {
			t.Errorf("TokenColor(%q) = %q, want ColorText %q", r, got, ColorText)
		}
	}
}

func TestTokenStyleIsForegroundOnly(t *testing.T) {
	for _, r := range requireTokenRoles(t) {
		s := TokenStyle(r)
		if got := s.GetForeground(); got != TokenColor(r) {
			t.Errorf("TokenStyle(%s) foreground = %v, want %q", r, got, TokenColor(r))
		}
		if _, noBg := s.GetBackground().(lipgloss.NoColor); !noBg {
			t.Errorf("TokenStyle(%s) sets a background %v; datatable supplies it (Rule 122)", r, s.GetBackground())
		}
	}
	if got := TokenStyle("bogus").GetForeground(); got != ColorText {
		t.Errorf("TokenStyle(unknown) foreground = %v, want ColorText", got)
	}
}

func TestColorDistance(t *testing.T) {
	d, err := ColorDistance("#000000", "#ffffff")
	if err != nil || math.Abs(d-100) > 0.5 {
		t.Errorf("black/white = %.2f, %v; want ~100", d, err)
	}
	if d, err := ColorDistance("#89b4fa", "#89b4fa"); err != nil || d != 0 {
		t.Errorf("identical = %v, %v; want 0", d, err)
	}
	ab, _ := ColorDistance("#89b4fa", "#f9e2af")
	ba, _ := ColorDistance("#f9e2af", "#89b4fa")
	if ab == 0 || math.Abs(ab-ba) > 1e-9 {
		t.Errorf("not symmetric or zero: %v vs %v", ab, ba)
	}
	if d, _ := ColorDistance("#abc", "#aabbcc"); d != 0 {
		t.Errorf("#abc vs #aabbcc = %v, want 0 (same as ContrastRatio's short form)", d)
	}
	for _, bad := range []lipgloss.Color{"red", "", "#12345", "#gggggg", "5"} {
		if _, err := ColorDistance(bad, "#000000"); err == nil {
			t.Errorf("ColorDistance(%q, ...) accepted a non-hex colour", bad)
		}
	}
}

// Rule 119: the token roles bring no hex of their own; hex lives in colors.go
// (palette) and manager.go/theme files.
func TestTokenColorsFileHasNoHardcodedHex(t *testing.T) {
	data, err := os.ReadFile("tokencolors.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `lipgloss.Color("#`) {
		t.Error(`tokencolors.go contains lipgloss.Color("#...") (Rule 119)`)
	}
}
