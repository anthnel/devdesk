package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bubbletea v2 removes KeyMsg.Runes, .Alt, .Paste and the tea.KeyType constants.
// Production code reads the key by its name (String()) only, so the migration
// does not have to rewrite it (DEVDESK-200). internal/ui/testutil is the one place
// that builds keys and is out of scope.

// legacyKeyReads returns "path:line" for every use, in a production file, of a
// v1-only key field or key type constant: X.Runes, X.Alt, X.Paste, and tea.Key<Name>
// other than tea.KeyMsg and tea.Key. tea.KeyType is flagged: it disappears in v2.
// teaName is the local name of the bubbletea import; a file without it is skipped.
func legacyKeyReads(fset *token.FileSet, path string, file *ast.File, teaName string) []string {
	if teaName == "" {
		return nil
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		legacy := name == "Runes" || name == "Alt" || name == "Paste"
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == teaName &&
			strings.HasPrefix(name, "Key") && name != "KeyMsg" && name != "Key" {
			legacy = true
		}
		if legacy {
			out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(fset.Position(sel.Sel.Pos()).Line))
		}
		return true
	})
	return out
}

// teaImportName returns the local name of the bubbletea import, "" if absent.
func teaImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != bubbleteaPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "tea"
	}
	return ""
}

func TestLegacyKeyReadsDetector(t *testing.T) {
	const head = "package p\nimport tea \"github.com/charmbracelet/bubbletea\"\n"
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"runes", head + "func f(msg tea.KeyMsg) []rune { return msg.Runes }\n", 1},
		{"alt", head + "func f(msg tea.KeyMsg) bool { return msg.Alt }\n", 1},
		{"paste", head + "func f(msg tea.KeyMsg) bool { return msg.Paste }\n", 1},
		{"type constant in case", head + "func f(msg tea.KeyMsg) {\n\tswitch msg.Type {\n\tcase tea.KeyEsc:\n\t}\n}\n", 1},
		{"composite literal", head + "var _ = tea.KeyMsg{Type: tea.KeyLeft}\n", 1},
		{"key type", head + "func f(t tea.KeyType) {}\n", 1},
		{"type as value", head + "func f(msg tea.KeyMsg) { _ = msg.Type }\n", 0},
		{"KeyMsg as a type", head + "func f(msg tea.KeyMsg) {}\n", 0},
		{"String", head + "func f(msg tea.KeyMsg) string { return msg.String() }\n", 0},
		{"non-key Type field", head + "type C struct{ Type string }\nfunc f(c C) bool { return c.Type == \"ssl\" }\n", 0},
		{"no bubbletea import", "package p\nfunc f(x struct{ Alt bool }) bool { return x.Alt }\n", 0},
		{"aliased import", "package p\nimport bt \"github.com/charmbracelet/bubbletea\"\nvar _ = bt.KeyEsc\n", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := legacyKeyReads(fset, "x.go", file, teaImportName(file))
			if len(got) != tc.want {
				t.Errorf("findings = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestNoProductionCodeReadsLegacyKeyFields(t *testing.T) {
	files := 0
	walkProduction(t, func(fset *token.FileSet, path string, file *ast.File) {
		files++
		for _, where := range legacyKeyReads(fset, path, file, teaImportName(file)) {
			t.Errorf("%s: reads a bubbletea v1-only key field or tea.Key* constant; switch on msg.String() — v2 removes them (DEVDESK-200)", where)
		}
	})
	if files == 0 {
		t.Fatal("no production file scanned — the scan is broken, not the code")
	}
}
