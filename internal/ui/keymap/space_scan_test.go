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

const bubbleteaPath = "charm.land/bubbletea/v2"

// The space key is named " " by bubbletea v1 and "space" by v2, and nothing
// reports the change: a `case " "` just stops matching and every checkbox stops
// toggling. Every binding reads keymap.Space instead, and this guard refuses the
// literals (DEVDESK-200).
//
// Limit (stated, not hidden): only switch clauses and ==/!= are read.
// slices.Contains(keys, " "), map keys and strings.EqualFold are not seen.

func importsBubbletea(file *ast.File) bool {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == bubbleteaPath {
			return true
		}
	}
	return false
}

func isSpaceLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, err := strconv.Unquote(lit.Value)
	return err == nil && (v == " " || v == "space")
}

// literalSpaceComparisons returns "path:line" for every place in file that compares
// a key to the space literal (" " or "space") instead of keymap.Space: a case clause
// of any switch, or an operand of ==/!= in a file that imports bubbletea.
func literalSpaceComparisons(fset *token.FileSet, path string, file *ast.File) []string {
	var out []string
	flag := func(e ast.Expr) {
		out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(fset.Position(e.Pos()).Line))
	}
	tea := importsBubbletea(file)
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CaseClause:
			for _, e := range x.List {
				if isSpaceLiteral(e) {
					flag(e)
				}
			}
		case *ast.BinaryExpr:
			if tea && (x.Op == token.EQL || x.Op == token.NEQ) {
				for _, e := range []ast.Expr{x.X, x.Y} {
					if isSpaceLiteral(e) {
						flag(e)
					}
				}
			}
		}
		return true
	})
	return out
}

func TestLiteralSpaceComparisonsDetector(t *testing.T) {
	const head = "package p\nimport tea \"charm.land/bubbletea/v2\"\nvar _ tea.Msg\n"
	tests := []struct {
		name string
		src  string
		want []string // lines, in order
	}{
		{"case space literal", head + "func f(s string) {\n\tswitch s {\n\tcase \" \":\n\t}\n}\n", []string{"x.go:6"}},
		{"case named space", head + "func f(s string) {\n\tswitch s {\n\tcase \"space\":\n\t}\n}\n", []string{"x.go:6"}},
		{"enter and space", head + "func f(s string) {\n\tswitch s {\n\tcase \"enter\", \" \":\n\t}\n}\n", []string{"x.go:6"}},
		{"equality", head + "func f(s string) bool {\n\treturn s == \" \"\n}\n", []string{"x.go:5"}},
		{"inequality reversed", head + "func f(s string) bool {\n\treturn \"space\" != s\n}\n", []string{"x.go:5"}},
		{"equality without bubbletea is not read", "package p\nfunc f(s string) bool {\n\treturn s == \" \"\n}\n", nil},
		{"keymap constant", head + "func f(s string) {\n\tswitch s {\n\tcase keymap.Space:\n\t}\n}\n", nil},
		{"rune", head + "func f(r rune) bool {\n\treturn r == ' '\n}\n", nil},
		{"display label", head + "type K struct{ Key string }\nvar _ = K{Key: \"space\"}\n", nil},
		{"assignment", head + "var x = \" \"\n", nil},
		{"strings.Repeat", head + "var y = strings.Repeat(\" \", 3)\n", nil},
		{"other literals", head + "func f(s string) {\n\tswitch s {\n\tcase \"enter\", \"a b\":\n\t}\n}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := literalSpaceComparisons(fset, "x.go", file)
			if len(got) != len(tc.want) {
				t.Fatalf("findings = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("finding %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// walkProduction parses every non-test Go file of the scanned trees and hands it to
// visit. Skipped: this package (it declares the vocabulary) and internal/ui/testutil
// (it translates test input names such as "space" into messages and binds nothing;
// DEVDESK-199's source guard already owns it).
func walkProduction(t *testing.T, visit func(fset *token.FileSet, path string, file *ast.File)) {
	t.Helper()
	seen := map[string]bool{}
	for _, root := range scannedTrees {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			slash := filepath.ToSlash(path)
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
				strings.Contains(slash, "/ui/keymap/") || strings.Contains(slash, "/ui/testutil/") {
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
			visit(fset, path, file)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func TestNoKeyIsComparedToALiteralSpace(t *testing.T) {
	files, uses := 0, 0
	walkProduction(t, func(fset *token.FileSet, path string, file *ast.File) {
		files++
		for _, where := range literalSpaceComparisons(fset, path, file) {
			t.Errorf("%s: compares a key to a space literal; write keymap.Space — bubbletea v1 names the space \" \", v2 \"space\", and only the constant follows the migration (DEVDESK-200)", where)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Space" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "keymap" {
					uses++
				}
			}
			return true
		})
	})
	if files == 0 {
		t.Fatal("no production file scanned — the scan is broken, not the code")
	}
	if uses == 0 && !t.Failed() {
		t.Fatal("no keymap.Space selector seen — the scan is broken, not the code")
	}
}
