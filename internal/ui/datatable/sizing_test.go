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

// The one thing about Sizing that can be checked rather than reviewed: no
// column leaves it at its zero value.
//
// SizingUnset could have been given a meaning, and both candidates are worse
// than the absence. "Fixed, never dropped" changes nothing until a table is
// annotated — but then no column is droppable, the last resort decides
// everything, and the behaviour that existed disappears without being
// replaced. "Content, droppable" is closer to what the solver used to do, but
// it makes the identifying column of twenty tables removable by default. That
// is D12 under another name: a zero that speaks for what nobody declared.
//
// A source test rather than a runtime one, and that is forced: the columns live
// in fourteen different instantiations of a generic type, so there is no single
// table to walk — only the source says all of them. The precedent is
// internal/ui/keymap, which reads the switches for the same reason.

// sizingScannedTree is where the application's columns are declared. They are
// all under internal/ui; nothing outside the UI builds one.
const sizingScannedTree = ".."

func TestEveryColumnDeclaresItsSizing(t *testing.T) {
	var missing []string

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
				if declaresSizing(column) {
					continue
				}
				at := fset.Position(column.Pos())
				missing = append(missing, at.String()+" "+titleOf(column))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", sizingScannedTree, err)
	}

	if len(missing) > 0 {
		t.Errorf("%d column(s) declare no Sizing — every column states whether its width is exact (SizingFixed) or follows its content (SizingContent):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// columnLiterals returns the Column literals a composite literal contains: the
// literal itself when it is one, and its elements when it is a slice of them —
// the elements of `[]datatable.Column[T]{{…}, {…}}` carry no type of their own.
func columnLiterals(lit *ast.CompositeLit) []*ast.CompositeLit {
	if isColumnType(lit.Type) {
		return []*ast.CompositeLit{lit}
	}
	array, ok := lit.Type.(*ast.ArrayType)
	if !ok || !isColumnType(array.Elt) {
		return nil
	}
	var out []*ast.CompositeLit
	for _, elt := range lit.Elts {
		if element, ok := elt.(*ast.CompositeLit); ok {
			out = append(out, element)
		}
	}
	return out
}

// isColumnType recognises datatable.Column[T] — and Column[T] inside this
// package, though nothing here declares one outside a test.
func isColumnType(expr ast.Expr) bool {
	index, ok := expr.(*ast.IndexExpr)
	if !ok {
		return false
	}
	switch name := index.X.(type) {
	case *ast.SelectorExpr:
		return name.Sel.Name == "Column"
	case *ast.Ident:
		return name.Name == "Column"
	}
	return false
}

func declaresSizing(lit *ast.CompositeLit) bool {
	return fieldValue(lit, "Sizing") != nil
}

// titleOf names the column in the failure, so the report can be acted on
// without opening the file. A title built at runtime has none to give.
func titleOf(lit *ast.CompositeLit) string {
	title, ok := fieldValue(lit, "Title").(*ast.BasicLit)
	if !ok {
		return "(untitled)"
	}
	return title.Value
}

func fieldValue(lit *ast.CompositeLit, name string) ast.Expr {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == name {
			return kv.Value
		}
	}
	return nil
}
