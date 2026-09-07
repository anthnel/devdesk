package theme_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// scannedTrees are the trees where a premature render could sneak in: every
// `internal/`, plus the repo's `main`.
var scannedTrees = []string{
	filepath.Join("..", "..", ".."),
}

// TestNoPackageLevelVarRendersAString forbids a package-level variable from
// being initialized by a render.
//
// Two things break when that happens, and the second one was costly:
//
//  1. A package's initialization runs before `ApplyTheme`, so the string is
//     frozen on the default theme's colors and never follows any theme
//     change afterward. This is the trap already documented on
//     `ColorChartBg` in `ApplyTheme`.
//
//  2. More importantly: the first render triggers the `sync.Once` by which
//     lipgloss **permanently** memorizes the terminal's color profile
//     (`Renderer.ColorProfile`). The profile therefore ended up computed
//     during package init, i.e. before the line in `main()` that sets
//     `COLORTERM=truecolor` where WSL hasn't propagated it. The whole TUI
//     fell back to ANSI256, where each theme's background is quantized onto
//     the 256 palette: `#1e1e2e` (default, mocha) becomes black 232 and
//     `#24273a` (macchiato) becomes navy blue 17. The background didn't
//     "respect the theme" because it never received its exact color.
//
// The fix is always the same, and it's free: a function rather than a
// `var`, so that the first render happens inside `View()`.
func TestNoPackageLevelVarRendersAString(t *testing.T) {
	for _, decl := range packageLevelVarInits(t) {
		ast.Inspect(decl.value, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, isRender := renderingCall(call); isRender {
				t.Errorf("%s: la variable de paquet %q est initialisée par %s. "+
					"Un rendu à l'init fige la chaîne sur le thème par défaut et "+
					"verrouille le profil de couleur de lipgloss avant que main() "+
					"n'ait posé COLORTERM — remplacer la var par une fonction.",
					decl.where, decl.name, name)
			}
			return true
		})
	}
}

// renderingCall recognizes a call that produces a styled string, and thus
// resolves a color. Two forms are enough to cover them all:
// `<style>.Render(...)`, and any function of the theme package — its
// helpers (`Bg`, `BgLine`, `PadWithBg`, `EmptyLineBg`, `RenderCheckbox`…)
// all render. The package's *values* (`theme.ColorText`, `theme.IconOK`) are
// not calls and remain allowed.
func renderingCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if sel.Sel.Name == "Render" {
		return "un appel à .Render()", true
	}
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "theme" {
		return "theme." + sel.Sel.Name + "()", true
	}
	return "", false
}

type varInit struct {
	name  string
	where string
	value ast.Expr
}

// packageLevelVarInits returns every initialization expression of variables
// declared at package level. Declarations inside a function are out of
// scope: they run when the function is called, not at init.
func packageLevelVarInits(t *testing.T) []varInit {
	t.Helper()

	seen := map[string]bool{}
	var inits []varInit

	for _, tree := range scannedTrees {
		// The root is resolved to an absolute path before the walk:
		// `filepath.WalkDir` visits the starting entry itself, and the `..`
		// of a relative path would then get mistaken for a hidden directory
		// by the filter below.
		root, err := filepath.Abs(tree)
		if err != nil {
			t.Fatalf("abs %s: %v", tree, err)
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Not the dependencies, not git, not the build caches.
				name := d.Name()
				if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "bin") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			for _, d := range file.Decls {
				gen, ok := d.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, value := range vs.Values {
						name := "?"
						if i < len(vs.Names) {
							name = vs.Names[i].Name
						}
						inits = append(inits, varInit{
							name:  name,
							where: filepath.ToSlash(path) + ":" + strconv.Itoa(fset.Position(value.Pos()).Line),
							value: value,
						})
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(inits) == 0 {
		t.Fatal("aucune variable de paquet trouvée — le parcours des sources est cassé")
	}
	return inits
}
