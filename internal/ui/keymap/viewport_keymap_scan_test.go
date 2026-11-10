package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// DEVDESK-201 D2. bubbles/viewport v2 scrolls horizontally on Left/Right/h/l by
// default. Rule 135 gives <-/-> to drill-down and value cycling, and a bare
// letter is an action. A viewport that is handed keys must therefore replace the
// default KeyMap with one that has no Left/Right.
//
// The guard: in a production package, every `X.Update(` whose receiver's last
// selector name matches (?i)viewport|vp must be accompanied, in the same package,
// by an assignment `X.KeyMap = ...` for the same X.
//
// Limits (stated, not hidden):
//   - It keys on the receiver's name. A viewport stored under a name that does
//     not contain "viewport" (or is not "vp") escapes it, and so does a KeyMap
//     set through a helper that returns the model.
//   - It accepts any assignment to X.KeyMap, including X.KeyMap =
//     viewport.DefaultKeyMap(), which is exactly the default it exists to
//     forbid. It proves a KeyMap is set, not that Left/Right are unbound; the
//     latter is TestArrowOnlyScrollBindsNoHorizontalKey in internal/app.
// The real code today has one such site (internal/app/help_overlay.go) and is
// compliant, so TestEveryViewportFedKeysSetsAKeyMap is a non-regression guard;
// TestViewportKeyMapDetector is what proves the guard can fire.

var viewportName = regexp.MustCompile(`(?i)viewport|^vp$`)

// lastName returns the last identifier of x (a.b.c -> "c"), "" if x is not a
// chain of selectors/identifiers.
func lastName(x ast.Expr) string {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// viewportKeyFindings returns "path:line" for each X.Update( on a viewport-named
// X in files (one package) with no X.KeyMap = assignment anywhere in files.
func viewportKeyFindings(fset *token.FileSet, files map[string]*ast.File) []string {
	withKeyMap := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, l := range as.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "KeyMap" {
						withKeyMap[lastName(sel.X)] = true
					}
				}
			}
			return true
		})
	}
	var out []string
	for path, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Update" {
				return true
			}
			name := lastName(sel.X)
			if name != "" && viewportName.MatchString(name) && !withKeyMap[name] {
				out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(fset.Position(sel.Sel.Pos()).Line))
			}
			return true
		})
	}
	return out
}

func TestViewportKeyMapDetector(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"fed keys, default KeyMap", "package p\nfunc f(m M, msg any) { m.detailsViewport, _ = m.detailsViewport.Update(msg) }\n", 1},
		{"fed keys, plain variable vp", "package p\nfunc f(msg any) { vp, _ = vp.Update(msg) }\n", 1},
		{"fed keys, KeyMap set", "package p\nfunc f(a A, msg any) {\n\ta.helpViewport.KeyMap = k()\n\ta.helpViewport, _ = a.helpViewport.Update(msg)\n}\n", 0},
		{"KeyMap set for another viewport only", "package p\nfunc f(a A, msg any) {\n\ta.helpViewport.KeyMap = k()\n\ta.textViewport, _ = a.textViewport.Update(msg)\n}\n", 1},
		{"not a viewport", "package p\nfunc f(a A, msg any) { a.spinner, _ = a.spinner.Update(msg) }\n", 0},
		{"viewport never fed keys", "package p\nfunc f(a A) { a.textViewport.SetContent(\"x\") }\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := viewportKeyFindings(fset, map[string]*ast.File{"x.go": file})
			if len(got) != tc.want {
				t.Errorf("findings = %v, want %d", got, tc.want)
			}
		})
	}
}

// scanProduction parses every production file into one FileSet, grouped by
// package directory.
func scanProduction(t *testing.T) (*token.FileSet, map[string]map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	byDir := map[string]map[string]*ast.File{}
	files := 0
	walkProduction(t, func(_ *token.FileSet, path string, _ *ast.File) {
		// walkProduction parses with its own FileSet; reparse into ours so
		// positions are comparable and files group by package directory.
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files++
		dir := filepath.Dir(path)
		if byDir[dir] == nil {
			byDir[dir] = map[string]*ast.File{}
		}
		byDir[dir][path] = f
	})
	if files == 0 {
		t.Fatal("no production file scanned — the scan is broken, not the code")
	}
	return fset, byDir
}

func TestEveryViewportFedKeysSetsAKeyMap(t *testing.T) {
	fset, byDir := scanProduction(t)
	for _, pkg := range byDir {
		for _, where := range viewportKeyFindings(fset, pkg) {
			t.Errorf("%s: a viewport is fed keys but its package never sets <viewport>.KeyMap; the default scrolls on Left/Right/h/l (Rule 135) — assign a KeyMap without them (DEVDESK-201)", where)
		}
	}
}

// The guard above is green when it finds nothing, so a rename of the help
// overlay's viewport (or a change of the scanned trees) would silence it. The
// one site known to feed keys to a viewport must still be seen.
func TestViewportGuardStillSeesTheHelpOverlay(t *testing.T) {
	fset, byDir := scanProduction(t)
	found := false
	for dir, pkg := range byDir {
		if filepath.Base(dir) != "app" {
			continue
		}
		for path, f := range pkg {
			if filepath.Base(path) != "help_overlay.go" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Update" &&
					lastName(sel.X) == "helpViewport" {
					found = true
				}
				return true
			})
			if !found {
				t.Errorf("%s no longer calls helpViewport.Update; the viewport guard has lost its only known site (renamed? moved?)", fset.Position(f.Pos()).Filename)
			}
		}
	}
	if !found {
		t.Error("internal/app/help_overlay.go not scanned or has no helpViewport.Update call: the viewport guard cannot be trusted")
	}
}
