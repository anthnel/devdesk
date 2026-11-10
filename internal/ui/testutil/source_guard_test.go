package testutil

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-199: tests build key presses and pick a colour profile through the
// helpers in this package and internal/testutil/colorprofile, never by hand.
// A hand-built tea.KeyMsg{Type: tea.KeyRunes, Runes: ...} drifts from what the
// terminal really sends, and lipgloss.SetColorProfile / termenv leak a global
// across tests. This guard reads every *_test.go the way internal/ui/keymap's
// scans do, and names file:line for each offence.
//
// Using tea.KeyMsg as a TYPE (assertion, parameter, return) stays allowed.

const (
	bubbleteaPath = "charm.land/bubbletea/v2"
	termenvPath   = "github.com/muesli/termenv"
)

// bubbleteaV2Paths are the bubbletea v2 import paths; bubbleteaPath is v1.
var bubbleteaV2Paths = map[string]bool{
	"charm.land/bubbletea/v2":               true,
	"github.com/charmbracelet/bubbletea/v2": true,
}

// bannedKeyLiterals lists the key types that must not be built by hand, per
// bubbletea major version (false is v1, true is v2).
var bannedKeyLiterals = map[bool]map[string]bool{
	false: {"KeyMsg": true, "Key": true},
	true:  {"KeyPressMsg": true, "KeyReleaseMsg": true, "Key": true},
}

// allowedKeySelectors are Key-prefixed names that stay allowed as types.
var allowedKeySelectors = map[string]bool{
	"KeyMsg": true, "KeyPressMsg": true, "KeyReleaseMsg": true, "KeyboardEnhancementsMsg": true,
}

// fieldSelectorExceptions lists "<slash path suffix>:<selector>" pairs allowed
// to use .Runes / .Alt / .Paste. Empty on purpose: every entry needs a reason
// written next to it.
var fieldSelectorExceptions = map[string]string{}

var bannedFields = map[string]bool{"Runes": true, "Alt": true, "Paste": true}

// skippedDirs are module-relative slash paths the scan does not enter.
var skippedDirs = []string{"internal/ui/testutil", "internal/testutil/colorprofile"}

// guardSource returns one message per offence in src ("name:line: reason") and
// the number of bubbletea imports it saw (so the caller can tell a scan that
// looked at nothing).
func guardSource(fset *token.FileSet, name string, src any) ([]string, int, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, err
	}
	teaNames := map[string]bool{} // local name -> is v2
	teaImports := 0
	var out []string
	report := func(pos token.Pos, format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(pos), fmt.Sprintf(format, args...)))
	}
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		switch {
		case p == bubbleteaPath || bubbleteaV2Paths[p]:
			teaImports++
			n := "tea"
			if imp.Name != nil {
				n = imp.Name.Name
			}
			if n != "_" && n != "." {
				teaNames[n] = bubbleteaV2Paths[p]
			}
		case p == termenvPath:
			report(imp.Pos(), "imports %s; use internal/testutil/colorprofile", termenvPath)
		}
	}
	// isTea returns the local name of a bubbletea import and whether it is v2.
	isTea := func(e ast.Expr) (string, bool, bool) {
		id, ok := e.(*ast.Ident)
		if !ok {
			return "", false, false
		}
		v2, ok := teaNames[id.Name]
		return id.Name, v2, ok
	}
	slash := filepath.ToSlash(name)
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if sel, ok := x.Type.(*ast.SelectorExpr); ok {
				if pkg, v2, ok := isTea(sel.X); ok && bannedKeyLiterals[v2][sel.Sel.Name] {
					report(x.Pos(), "composite literal %s.%s; use testutil.Key/Keys/Type", pkg, sel.Sel.Name)
				}
			}
		case *ast.SelectorExpr:
			if pkg, _, ok := isTea(x.X); ok {
				if strings.HasPrefix(x.Sel.Name, "Key") && !allowedKeySelectors[x.Sel.Name] {
					report(x.Pos(), "%s.%s; use testutil.Key/Keys/Type", pkg, x.Sel.Name)
				}
			} else if id, ok := x.X.(*ast.Ident); ok && id.Name == "testutil" {
				// testutil.Paste is the sanctioned helper, a package-qualified
				// call and not a field of a key message: not an offence.
			} else if bannedFields[x.Sel.Name] {
				allowed := false
				for k := range fieldSelectorExceptions {
					if i := strings.LastIndex(k, ":"); i > 0 && strings.HasSuffix(slash, k[:i]) && k[i+1:] == x.Sel.Name {
						allowed = true
					}
				}
				if !allowed {
					report(x.Sel.Pos(), "selector .%s on a key message; use the testutil helpers", x.Sel.Name)
				}
			}
			if x.Sel.Name == "SetColorProfile" {
				report(x.Sel.Pos(), "SetColorProfile (call or reference); use internal/testutil/colorprofile")
			}
		}
		return true
	})
	return out, teaImports, nil
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

func TestGuardNoHandBuiltKeysOrColorProfile(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	var violations []string
	teaSeen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			for _, s := range skippedDirs {
				if rel == s {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		v, n, err := guardSource(fset, rel, mustRead(t, path))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		teaSeen += n
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if teaSeen == 0 {
		t.Error("the scan saw no bubbletea import: the guard would pass vacuously (migration or wrong root?)")
	}
	sort.Strings(violations)
	for _, v := range violations {
		t.Errorf("%s", v)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGuardDetectorSelfTest(t *testing.T) {
	const bad = `package x

import (
	tea "charm.land/bubbletea/v2"
	bt "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"
)

func f(m tea.Msg) {
	_ = tea.KeyMsg{Type: tea.KeyEnter}
	_ = bt.Key{}
	_ = bt.KeyCtrlC
	k := m.(tea.KeyMsg)
	_ = k.Runes
	_ = k.Alt
	_ = k.Paste
	lipgloss.SetColorProfile(termenv.Ascii)
	lipgloss.DefaultRenderer().SetColorProfile(termenv.Ascii)
	r.SetColorProfile(termenv.Ascii)
}
`
	const badV2 = `package x

import (
	tea "charm.land/bubbletea/v2"
	bt "github.com/charmbracelet/bubbletea/v2"
)

func f() {
	_ = tea.KeyPressMsg{}
	_ = tea.KeyReleaseMsg{}
	_ = bt.Key{}
	_ = bt.KeyEnter
}
`
	const good = `package x

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"x/testutil"
)

type other struct{ KeyEnter int }

func g(k tea.KeyPressMsg) tea.KeyMsg { return nil }

func f(m tea.Msg, o other) tea.KeyMsg {
	k, _ := m.(tea.KeyMsg)
	_ = o.KeyEnter
	_ = testutil.Paste("x")
	_ = lipgloss.NewStyle()
	return k
}

`
	got, n, err := guardSource(token.NewFileSet(), "bad_test.go", bad)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("bubbletea imports counted = %d, want 2", n)
	}
	want := []string{
		"imports " + termenvPath, "composite literal tea.KeyMsg", "tea.KeyEnter",
		"composite literal bt.Key", "bt.KeyCtrlC", ".Runes", ".Alt", ".Paste", "SetColorProfile",
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if strings.Contains(g, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("detector missed %q; got %q", w, got)
		}
	}
	if c := strings.Count(strings.Join(got, "\n"), "SetColorProfile"); c != 3 {
		t.Errorf("SetColorProfile offences = %d, want 3; got %q", c, got)
	}
	got, n, err = guardSource(token.NewFileSet(), "bad_v2_test.go", badV2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("v2 bubbletea imports counted = %d, want 2", n)
	}
	for _, w := range []string{"composite literal tea.KeyPressMsg", "composite literal tea.KeyReleaseMsg", "composite literal bt.Key", "bt.KeyEnter"} {
		found := false
		for _, g := range got {
			if strings.Contains(g, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("v2 detector missed %q; got %q", w, got)
		}
	}
	got, n, err = guardSource(token.NewFileSet(), "good_test.go", good)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("good: bubbletea imports counted = %d, want 1", n)
	}
	if len(got) != 0 {
		t.Errorf("false positives: %q", got)
	}
}
