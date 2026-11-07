package config

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLog redirects the standard logger for the test and returns the buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func isolateMotionHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestMotionTable(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		kept      string
		effective string
		static    bool
		valid     bool
		warn      bool
	}{
		{"absent", "", "", MotionBlink, false, true, false},
		{"blink", "blink", "blink", MotionBlink, false, true, false},
		{"static", "static", "static", MotionStatic, true, true, false},
		{"flash", "flash", "flash", MotionBlink, false, false, true},
		{"Static is not static", "Static", "Static", MotionBlink, false, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateMotionHome(t)
			buf := captureLog(t)
			cfg := Default()
			cfg.App.Motion = tc.in
			if err := applyDefaults(cfg); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}
			if cfg.App.Motion != tc.kept {
				t.Errorf("Motion = %q, want %q (kept as read)", cfg.App.Motion, tc.kept)
			}
			if got := cfg.App.EffectiveMotion(); got != tc.effective {
				t.Errorf("EffectiveMotion = %q, want %q", got, tc.effective)
			}
			if got := cfg.App.StaticMotion(); got != tc.static {
				t.Errorf("StaticMotion = %v, want %v", got, tc.static)
			}
			if got := cfg.App.MotionValid(); got != tc.valid {
				t.Errorf("MotionValid = %v, want %v", got, tc.valid)
			}
			want := `WARN [config] app.motion "` + tc.in + `" is not blink or static, blinking`
			if has := strings.Contains(buf.String(), want); has != tc.warn {
				t.Errorf("log contains WARN = %v, want %v; log: %q", has, tc.warn, buf.String())
			}
			if !tc.warn && strings.Contains(buf.String(), "app.motion") {
				t.Errorf("unexpected app.motion log: %q", buf.String())
			}
		})
	}
}

func TestMotionConstantsAndSet(t *testing.T) {
	if MotionBlink != "blink" || MotionStatic != "static" || DefaultMotion != MotionBlink {
		t.Fatalf("constants: %q %q %q", MotionBlink, MotionStatic, DefaultMotion)
	}
	got := Motions()
	if len(got) != 2 || got[0] != MotionBlink || got[1] != MotionStatic {
		t.Errorf("Motions() = %v, want [blink static]", got)
	}
}

func TestMotionDefaultDoesNotSetItself(t *testing.T) {
	isolateMotionHome(t)
	cfg := Default()
	if cfg.App.StaticMotion() {
		t.Error("Default() must blink")
	}
	if cfg.App.Motion != "" {
		t.Errorf("Default().App.Motion = %q, want empty (absent stays absent)", cfg.App.Motion)
	}
}

// roundTrip writes a context file, loads it, saves it and returns the loaded
// config, the reloaded one and the saved file's text.
func roundTrip(t *testing.T, yamlBody string) (loaded, reloaded *Config, saved string) {
	t.Helper()
	isolateMotionHome(t)
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	path, err := GetContextPath("motiontest")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadContext("motiontest")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if err := SaveContext(loaded, "motiontest"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err = LoadContext("motiontest")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return loaded, reloaded, string(data)
}

func TestMotionInvalidValueSurvivesLoadAndSave(t *testing.T) {
	tests := []struct{ body, want string }{
		{"app:\n  motion: flash\n", "flash"},
		{"app:\n  motion: 3\n", "3"},
		{"app:\n  motion: Static\n", "Static"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			buf := captureLog(t)
			loaded, reloaded, _ := roundTrip(t, tc.body)
			if loaded.App.Motion != tc.want || reloaded.App.Motion != tc.want {
				t.Errorf("Motion loaded=%q reloaded=%q, want %q kept", loaded.App.Motion, reloaded.App.Motion, tc.want)
			}
			if loaded.App.MotionValid() || loaded.App.StaticMotion() {
				t.Error("invalid value must be invalid and blink")
			}
			if !strings.Contains(buf.String(), `WARN [config] app.motion "`+tc.want+`" is not blink or static, blinking`) {
				t.Errorf("missing WARN, log: %q", buf.String())
			}
		})
	}
}

func TestMotionValidValueRoundTrips(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  motion: static\n")
	if !loaded.App.StaticMotion() || !reloaded.App.StaticMotion() {
		t.Error("static must survive load, save and reload")
	}
	if !strings.Contains(saved, "motion: static") {
		t.Errorf("saved file lost motion: static:\n%s", saved)
	}
}

func TestMotionAbsentKeyStaysAbsentOnSave(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  gauge_glyph: block\n")
	if loaded.App.Motion != "" || reloaded.App.Motion != "" {
		t.Errorf("Motion = %q / %q, want empty", loaded.App.Motion, reloaded.App.Motion)
	}
	if strings.Contains(saved, "motion") {
		t.Errorf("saved file gained a motion key:\n%s", saved)
	}
	if loaded.App.StaticMotion() {
		t.Error("absent must blink")
	}
}

// Only string literals are spotted: a name built at run time would slip through.
// TestMotionHasNoEnvironmentOverride enforces that app.motion has no
// environment variable (criterion 6): no os.Getenv/LookupEnv on a name
// containing MOTION anywhere in the module.
func TestMotionHasNoEnvironmentOverride(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "motion_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
				return true
			}
			for _, a := range call.Args {
				if lit, ok := a.(*ast.BasicLit); ok && strings.Contains(strings.ToUpper(lit.Value), "MOTION") {
					t.Errorf("%s: environment override for motion: %s", fset.Position(lit.Pos()), lit.Value)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
