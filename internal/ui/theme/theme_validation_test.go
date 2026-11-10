package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// DEVDESK-201 step A2, H3: LoadTheme validates the hex values. A non-empty
// value that is not #rgb or #rrggbb refuses the whole theme, naming the key and
// the value; an empty optional value keeps today's fallback.
//
// These tests use only LoadTheme and DefaultTheme.

// isolateThemeHome points ThemeDir at a temp directory and returns it.
func isolateThemeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".devdesk", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeThemeWith writes a complete, valid theme (the default one) with the
// given JSON keys overridden, and returns its name.
func writeThemeWith(t *testing.T, dir string, overrides map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range overrides {
		m[k] = v
	}
	m["name"] = "under-test"
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "under-test.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return "under-test"
}

// LoadTheme refuses a theme holding an invalid hex value.
func TestLoadThemeRejectsAnInvalidHex(t *testing.T) {
	keys := []string{"color_ok", "color_background", "title_fg", "severity_critical_fg", "token_cache_read"}
	values := []string{"#ggg", "red", "12", "ffffff", "#12345"}
	for _, key := range keys {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				dir := isolateThemeHome(t)
				name := writeThemeWith(t, dir, map[string]string{key: value})

				got, err := LoadTheme(name)

				if err == nil {
					t.Fatalf("LoadTheme accepted %s = %q", key, value)
				}
				if got != nil {
					t.Errorf("LoadTheme returned a theme alongside the error: %+v", got)
				}
				if !strings.Contains(err.Error(), key) {
					t.Errorf("error %q does not name the key %q", err, key)
				}
				if !strings.Contains(err.Error(), `"`+value+`"`) {
					t.Errorf("error %q does not quote the value %q", err, value)
				}
			})
		}
	}
}

// Valid forms stay valid, upper case included: the validation must not be
// stricter than ContrastRatio, which accepts both lengths in either case.
func TestLoadThemeAcceptsShortAndUpperCaseHex(t *testing.T) {
	dir := isolateThemeHome(t)
	name := writeThemeWith(t, dir, map[string]string{"color_ok": "#0F0", "title_fg": "#ABCDEF"})
	if _, err := LoadTheme(name); err != nil {
		t.Fatalf("LoadTheme refused #0F0 / #ABCDEF: %v", err)
	}
}

// optionalThemeKeys returns the JSON key of every field tagged omitempty: the
// component colours that fall back to the semantic palette.
func optionalThemeKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	rt := reflect.TypeOf(Theme{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if name, opts, _ := strings.Cut(tag, ","); strings.Contains(opts, "omitempty") {
			keys = append(keys, name)
		}
	}
	if len(keys) < 30 {
		t.Fatalf("found only %d optional keys; the reflection no longer sees the omitempty tags", len(keys))
	}
	return keys
}

// An empty optional value keeps the fallback, not an error.
func TestLoadThemeKeepsEmptyOptionalKeys(t *testing.T) {
	dir := isolateThemeHome(t)
	empty := map[string]string{}
	for _, k := range optionalThemeKeys(t) {
		empty[k] = ""
	}
	name := writeThemeWith(t, dir, empty)

	th, err := LoadTheme(name)
	if err != nil {
		t.Fatalf("LoadTheme refused empty optional keys: %v", err)
	}
	if th.TitleFg != "" || th.TokenInput != "" {
		t.Errorf("empty optional values were rewritten: title_fg=%q token_input=%q", th.TitleFg, th.TokenInput)
	}
}

// A key left out of the file is the same as an empty one.
func TestLoadThemeKeepsAbsentOptionalKeys(t *testing.T) {
	dir := isolateThemeHome(t)
	data := `{"name":"minimal","color_ok":"#00ff00","color_error":"#ff0000","color_warn":"#ff8800",` +
		`"color_primary":"#0000ff","color_secondary":"#8888ff","color_border":"#cccccc","color_text":"#000000",` +
		`"color_dim":"#888888","color_highlight":"#ffff00","color_white":"#ffffff","color_black":"#000000"}`
	if err := os.WriteFile(filepath.Join(dir, "minimal.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTheme("minimal"); err != nil {
		t.Fatalf("LoadTheme refused a theme without optional keys: %v", err)
	}
}

// Guards H3 against the shipped files: none of them may be refused.
func TestEveryShippedThemeLoads(t *testing.T) {
	files, err := filepath.Glob("../../../themes/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no theme found under themes/*.json (err %v)", err)
	}
	dir := isolateThemeHome(t)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		t.Run(name, func(t *testing.T) {
			if _, err := LoadTheme(name); err != nil {
				t.Errorf("shipped theme %s refused: %v", name, err)
			}
		})
	}
}
