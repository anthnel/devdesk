package components_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The footer used to be written eight times over, once per view, and that is
// how the errors ended up left-aligned everywhere while the notices were
// centred: nobody ever centred the error branch, in any of the eight.
//
// These two tests read the sources the way internal/ui/keymap's do. A
// convention nothing verifies is exactly what produced the divergence.

// uiRoots are the trees a view can live in.
var uiRoots = []string{"../../ui", "../../app"}

// forbiddenInFooter are the styles that mean "I am rendering my own footer
// message". They have legitimate uses elsewhere — status icons, table rows,
// panels — so the ban is scoped to the functions that render the footer.
var forbiddenInFooter = []string{
	"StatusErrorStyle",
	"StatusOKStyle",
	"StatusWarningStyle",
	"ColorHighlight",
}

func TestNoViewStylesItsOwnFooterMessage(t *testing.T) {
	forEachUIFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		// This package is the one implementation; it is allowed to style.
		if strings.Contains(filepath.ToSlash(path), "internal/ui/components/") {
			return
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !rendersFooter(fn.Name.Name) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				for _, banned := range forbiddenInFooter {
					if id.Name == banned {
						t.Errorf("%s: %s uses theme.%s to render a footer message.\n"+
							"The three levels live in components.FooterMessage — call m.footer.Info/Warn/Error "+
							"and render with m.footer.View(width, status).",
							fset.Position(id.Pos()), fn.Name.Name, banned)
					}
				}
				return true
			})
		}
	})
}

// rendersFooter reports whether a function's name says it draws the footer's
// message line. The three names cover every view: RenderFooter itself, and the
// two shapes the helper it delegates to has always taken.
func rendersFooter(name string) bool {
	switch {
	case strings.Contains(name, "RenderFooter"):
		return true
	case strings.Contains(name, "InfoLine"), strings.Contains(name, "InfoText"):
		return true
	default:
		return false
	}
}

// A table's load belongs to the footer, with a spinner, so the table itself
// stays on screen — a body that swaps itself for a spinner loses its header and
// its columns for the length of every refresh.
//
// theme.SpinnerMessage is what those bodies called. It survives for screens
// that are not a table at all: a form waiting on authentication, a progress
// screen, an operation with no rows behind it.
var viewsWithATableBody = []string{
	"containers/view.go",
	"oci_resources/view.go",
	"oci_resources/browser_view.go",
	"gitlab/explorer/view.go",
	"status/view.go",
	"viewer/view.go",
	"netdiag/topology_model.go",
}

// declaredSpinnerExceptions are the functions inside a guarded file that draw a
// spinner for something that is not a table load. They are written down as
// exceptions, the way keymap.DeclaredExceptions is, so the next audit does not
// read them as drift.
//
//	RegistryBrowser.viewStatus — a pull replaces the whole viewport for several
//	seconds and there is no table behind it. Moving it to the footer would leave
//	a blank screen with one line at the bottom, which says less, not more.
var declaredSpinnerExceptions = map[string]bool{
	"viewStatus": true,
}

func TestNoTableViewRendersALoadingBody(t *testing.T) {
	forEachUIFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		slash := filepath.ToSlash(path)
		guarded := false
		for _, suffix := range viewsWithATableBody {
			if strings.HasSuffix(slash, suffix) {
				guarded = true
				break
			}
		}
		if !guarded {
			return
		}

		inspect := func(fn string, body ast.Node) {
			if declaredSpinnerExceptions[fn] {
				return
			}
			ast.Inspect(body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "SpinnerMessage" {
					return true
				}
				t.Errorf("%s: %s renders a spinner in the body of a table view.\n"+
					"A load is reported in the footer: return components.Status{Text: …, Spinner: true} "+
					"from the view's status() and leave the table on screen.",
					fset.Position(sel.Pos()), fn)
				return true
			})
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			inspect(fn.Name.Name, fn.Body)
		}
	})
}

// forEachUIFile parses every non-test .go file under the UI trees.
func forEachUIFile(t *testing.T, visit func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()

	seen := map[string]bool{}
	for _, root := range uiRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if seen[abs] {
				return nil
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
