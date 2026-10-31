package keymap

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

// typeNamed reports whether expr names the given type, qualified or not.
func typeNamed(expr ast.Expr, name string) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name == name
	case *ast.Ident:
		return e.Name == name
	}
	return false
}

// isShortcutList reports whether lit is a []shortcut.Shortcut or
// shortcut.Shortcuts literal, whose elements may omit their type.
func isShortcutList(lit *ast.CompositeLit) bool {
	if arr, ok := lit.Type.(*ast.ArrayType); ok {
		return typeNamed(arr.Elt, "Shortcut")
	}
	return lit.Type != nil && typeNamed(lit.Type, "Shortcuts")
}

// isCommandModeKey reports whether expr is "ctrl+p" or the CommandMode constant.
func isCommandModeKey(expr ast.Expr) bool {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s, err := strconv.Unquote(lit.Value)
		return err == nil && s == "ctrl+p"
	}
	return typeNamed(expr, "CommandMode")
}

// TestNoViewListsTheCommandModeKeyInItsShortcuts: the header shows
// "ctrl+p Commands" permanently (internal/app/app_header.go), so a view that
// also returns it from GetShortcuts() prints the hint twice (DEVDESK-124).
// The shell (internal/app) supplies the hint and is not scanned; help
// content (help.KeyBinding) is a different type and stays allowed.
func TestNoViewListsTheCommandModeKeyInItsShortcuts(t *testing.T) {
	root := filepath.Join("..", "..", "ui")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		report := func(lit *ast.CompositeLit) {
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if ok && typeNamed(kv.Key, "Key") && isCommandModeKey(kv.Value) {
					t.Errorf("%s: a view lists ctrl+p in its shortcuts; the header already "+
						"shows \"ctrl+p Commands\" (DEVDESK-124)", fset.Position(kv.Pos()))
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			switch {
			case lit.Type != nil && typeNamed(lit.Type, "Shortcut"):
				report(lit)
			case isShortcutList(lit):
				for _, el := range lit.Elts {
					if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
						report(inner)
					}
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
