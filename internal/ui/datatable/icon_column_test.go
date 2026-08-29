package datatable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A first column carrying a glyph and nothing else is written one way in the
// whole application: no title, and IconColumnWidth cells. The workspaces list
// is where the shape was settled and the containers and inventory tables now
// match it.
//
// What this catches is drift in the width, which is the half that is invisible
// on one screen: three tables each declaring their own 2 or 3 look right alone
// and differ side by side — the containers table had 3, so its glyph sat one
// cell further from the name than the same glyph two views away.
//
// What it cannot catch is the other half — an icon glued into a text cell, the
// way the inventory used to render `IconDocker + " " + name` inside Target.
// Nothing in the source distinguishes that from a name that happens to start
// with a glyph, so it stays a review question. The rule is in Rule 125.
//
// A source test rather than a runtime one, for the reason
// TestEveryColumnDeclaresItsSizing is one: the columns live in a dozen
// instantiations of a generic type, and only the source names all of them.
func TestAnIconColumnIsUntitledAndTwoCellsWide(t *testing.T) {
	var wrong []string

	err := filepath.WalkDir(sizingScannedTree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, column := range columnLiterals(lit) {
				if titleOf(column) != `""` {
					continue
				}
				if namesIconColumnWidth(fieldValue(column, "MinWidth")) {
					continue
				}
				wrong = append(wrong, fset.Position(column.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", sizingScannedTree, err)
	}

	if len(wrong) > 0 {
		t.Errorf("%d untitled column(s) declare a width of their own — a glyph column is datatable.IconColumnWidth cells, everywhere:\n  %s",
			len(wrong), strings.Join(wrong, "\n  "))
	}
}

// namesIconColumnWidth accepts the constant by either spelling: qualified from
// a view, bare from inside this package.
func namesIconColumnWidth(expr ast.Expr) bool {
	switch name := expr.(type) {
	case *ast.SelectorExpr:
		return name.Sel.Name == "IconColumnWidth"
	case *ast.Ident:
		return name.Name == "IconColumnWidth"
	}
	return false
}
