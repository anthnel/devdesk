package theme_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Rule 119: colours are named in the theme, and a view that wants one asks the
// theme for it. These guards read the sources the way
// components/footer_guard_test.go does, with the same two roots.

// guardRoots are the trees a view can live in.
var guardRoots = []string{"../../ui", "../../app"}

// hexColor matches a string literal that is a whole colour, "#rgb" to "#rrggbbaa".
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// shortHexColor is the 3 and 4 digit form. It is also what "#bad" or "#fade"
// look like in prose, so it only counts as a colour when it is an argument of
// a colour constructor (see colorCallee).
var shortHexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4})$`)

// colorCallee reports whether a called function's name says it builds a colour.
func colorCallee(name string) bool {
	return strings.Contains(name, "Color") || strings.Contains(name, "Hex") || strings.Contains(name, "RGB")
}

// colorsFile is the only file that may spell a colour wholesale: the palette.
const colorsFile = "/ui/theme/colors.go"

// defaultThemeFile and defaultThemeFunc name the one function outside the
// palette that may spell colours: the embedded default theme.
const (
	defaultThemeFile = "/ui/theme/manager.go"
	defaultThemeFunc = "DefaultTheme"
)

// underlineEntry is the one function allowed to touch lipgloss's underline API.
const (
	underlineFile = "/ui/theme/styles.go"
	underlineFunc = "StyleInvalidField"
)

// guardFinding is one violation, already worded for the failure message.
type guardFinding struct {
	pos token.Position
	msg string
}

func TestGuardNoHexColorOutsideColorsGo(t *testing.T) {
	forEachGuardedFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, f := range findHexColors(path, file, fset) {
			t.Errorf("%s: %s", f.pos, f.msg)
		}
	})
}

func TestGuardInvalidFieldUnderlineHasOneEntryPoint(t *testing.T) {
	forEachGuardedFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, f := range findUnderlineUses(path, file, fset) {
			t.Errorf("%s: %s", f.pos, f.msg)
		}
	})
}

// TestGuardDetectors proves the two scanners fire on a faulty fragment and
// stay quiet on the sanctioned one.
func TestGuardDetectors(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		scan func(string, *ast.File, *token.FileSet) []guardFinding
		want int
	}{
		{"hex literal in a view", "internal/ui/foo/view.go",
			"package foo\nvar c = lipgloss.Color(\"#ff0000\")\n", findHexColors, 1},
		{"short hex literal in a view", "internal/ui/foo/view.go",
			"package foo\nvar c = lipgloss.Color(\"#f00\")\n", findHexColors, 1},
		{"bare short hex-like string", "internal/ui/foo/view.go",
			"package foo\nvar c = \"#bad\"\n", findHexColors, 0},
		{"color.RGBA composite in a view", "internal/ui/foo/view.go",
			"package foo\nvar c = color.RGBA{R: 1, G: 2, B: 3, A: 255}\n", findHexColors, 1},
		{"color.RGBA64 composite in a view", "internal/ui/foo/view.go",
			"package foo\nvar c = color.RGBA64{R: 1}\n", findHexColors, 1},
		{"color.RGBA composite in colors.go", "internal/ui/theme/colors.go",
			"package theme\nvar c = color.RGBA{R: 1, A: 255}\n", findHexColors, 0},
		{"RGBA type assertion is not a composite", "internal/ui/foo/view.go",
			"package foo\nfunc f(c any) { _, _ = c.(color.RGBA) }\n", findHexColors, 0},
		{"hex in DefaultTheme", "internal/ui/theme/manager.go",
			"package theme\nfunc DefaultTheme() { _ = \"#a6e3a1\" }\n", findHexColors, 0},
		{"hex elsewhere in manager.go", "internal/ui/theme/manager.go",
			"package theme\nfunc Other() { _ = \"#a6e3a1\" }\n", findHexColors, 1},
		{"hex literal in colors.go", "internal/ui/theme/colors.go",
			"package theme\nvar c = \"#ff0000\"\n", findHexColors, 0},
		{"non-colour string", "internal/ui/foo/view.go",
			"package foo\nvar c = \"#heading\"\n", findHexColors, 0},
		{"underline in a view", "internal/ui/foo/view.go",
			"package foo\nfunc f() { x.UnderlineStyle(1) }\n", findUnderlineUses, 1},
		{"plain Underline in a view", "internal/ui/foo/view.go",
			"package foo\nfunc f() { lipgloss.NewStyle().Underline(true) }\n", findUnderlineUses, 1},
		{"underline in another theme function", "internal/ui/theme/styles.go",
			"package theme\nfunc Other() { x.UnderlineColor(c) }\n", findUnderlineUses, 1},
		{"underline in StyleInvalidField", "internal/ui/theme/styles.go",
			"package theme\nfunc StyleInvalidField() { x.UnderlineStyle(1).UnderlineColor(c).UnderlineSpaces(false) }\n",
			findUnderlineUses, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.path, tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := tc.scan(tc.path, file, fset); len(got) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func findHexColors(path string, file *ast.File, fset *token.FileSet) []guardFinding {
	slash := filepath.ToSlash(path)
	if strings.HasSuffix(slash, colorsFile) {
		return nil
	}
	var out []guardFinding
	add := func(pos token.Pos, what string) {
		out = append(out, guardFinding{fset.Position(pos),
			what + " outside theme/colors.go (Rule 119): add it to the palette and use the theme name"})
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == defaultThemeFunc && strings.HasSuffix(slash, defaultThemeFile) {
			continue
		}
		// Literals that are an argument of a colour constructor.
		colorArgs := map[*ast.BasicLit]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if colorCallee(name) {
				for _, arg := range call.Args {
					if lit, ok := arg.(*ast.BasicLit); ok {
						colorArgs[lit] = true
					}
				}
			}
			return true
		})
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(n.Value)
				if err != nil || !hexColor.MatchString(s) {
					return true
				}
				if shortHexColor.MatchString(s) && !colorArgs[n] {
					return true
				}
				add(n.Pos(), "colour literal "+n.Value)
			case *ast.CompositeLit:
				if sel, ok := n.Type.(*ast.SelectorExpr); ok && (sel.Sel.Name == "RGBA" || sel.Sel.Name == "RGBA64") {
					add(n.Pos(), "colour composite "+sel.Sel.Name+"{...}")
				}
			}
			return true
		})
	}
	return out
}

// findUnderlineUses reports every selector of the underline API that is not
// inside theme.StyleInvalidField.
func findUnderlineUses(path string, file *ast.File, fset *token.FileSet) []guardFinding {
	slash := filepath.ToSlash(path)
	var out []guardFinding
	report := func(sel *ast.SelectorExpr) {
		out = append(out, guardFinding{fset.Position(sel.Sel.Pos()),
			sel.Sel.Name + " used outside theme.StyleInvalidField: call theme.StyleInvalidField instead"})
	}
	for _, decl := range file.Decls {
		inEntry := false
		if fn, ok := decl.(*ast.FuncDecl); ok {
			inEntry = fn.Name.Name == underlineFunc && strings.HasSuffix(slash, underlineFile)
		}
		if inEntry {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Underline") ||
				ok && strings.HasPrefix(sel.Sel.Name, "GetUnderline") {
				report(sel)
			}
			return true
		})
	}
	return out
}

func forEachGuardedFile(t *testing.T, visit func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	seen := map[string]bool{}
	for _, root := range guardRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil || seen[abs] {
				return err
			}
			seen[abs] = true
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			visit(path, file, fset)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no sources were scanned; the guard would pass on an empty set")
	}
}
