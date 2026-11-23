package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-751 (step 7): ctrl+o applies the built-in fix of the finding shown in
// the :sec detail. It is the same act as the Misconfigurations tab's ctrl+o (the
// same write, the same confirmation that defaults to No), so it is declared on
// its own surface, security/details: IsException matches by key only, and
// without its own entry the next survey could not tell the uses of ctrl+o apart.
// Y and X are already in the vocabulary (Copy, Exclude) and need no exception;
// O is not bound in the detail, enter opens the viewer, which has it.

func TestTheDetailsApplyFixKeyIsADeclaredException(t *testing.T) {
	for _, e := range DeclaredExceptions() {
		if e.Key != "ctrl+o" || e.Surface != "security/details" {
			continue
		}
		if e.Why == "" {
			t.Fatal("the ctrl+o exception on security/details has no reason")
		}
		for _, want := range []string{"confirmation", "No"} {
			if !strings.Contains(e.Why, want) {
				t.Errorf("the reason does not say %q: the key never acts on its own, it opens a confirmation that defaults to No: %q", want, e.Why)
			}
		}
		return
	}
	t.Errorf("no declared exception for ctrl+o on security/details; declared: %v", exceptionKeys())
}

// The three ctrl+o entries are told apart by their surface, so a surface may not
// be declared twice for one key.
func TestNoExceptionIsDeclaredTwiceOnOneSurface(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range DeclaredExceptions() {
		id := e.Key + " on " + e.Surface
		if seen[id] {
			t.Errorf("%s is declared twice", id)
		}
		seen[id] = true
		if e.Why == "" {
			t.Errorf("%s has no reason", id)
		}
	}
}

// The declaration is only worth something while the code uses it: the key
// handler of the detail binds exactly the keys the header advertises, ctrl+o
// among them, and binds no O. The scan reads the source because this package
// cannot import the view (the view imports this one).
func TestTheDetailsHandlerBindsTheAdvertisedKeys(t *testing.T) {
	keys := caseKeysOf(t, "../security/details.go", "handleDetailsState")
	for _, want := range []string{"esc", Web, "enter", "ctrl+o", Copy, Exclude} {
		if !slices.Contains(keys, want) {
			t.Errorf("handleDetailsState does not bind %q (binds %v)", want, keys)
		}
	}
	if slices.Contains(keys, IDE) {
		t.Errorf("handleDetailsState binds %q: enter opens the viewer, which has it (decision Q3)", IDE)
	}
}

func TestTheDetailsExceptionIsWrittenDownWhereTheRulesLive(t *testing.T) {
	for _, path := range []string{
		"../../../.claude/rules/tui-layout.md",
		"../../../docs/architecture/app-shell.md",
	} {
		doc, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(doc), "security/details") {
			t.Errorf("%s does not mention the ctrl+o exception on security/details: the survey would take it for drift", path)
		}
	}
}

// caseKeysOf returns the keys the `case` clauses of function fn in path bind:
// string literals, keymap.X constants, and package-level string constants of the
// file's directory (writeRemediationKey is "ctrl+o").
func caseKeysOf(t *testing.T, path, fn string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	local := packageStringConsts(t, path)
	vocabulary := keymapConstants(t)

	var keys []string
	found := false
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn || fd.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range cc.List {
				switch e := expr.(type) {
				case *ast.BasicLit:
					if v, err := strconv.Unquote(e.Value); err == nil && e.Kind == token.STRING {
						keys = append(keys, v)
					}
				case *ast.SelectorExpr:
					if pkg, ok := e.X.(*ast.Ident); ok && pkg.Name == "keymap" {
						if v, ok := vocabulary[e.Sel.Name]; ok {
							keys = append(keys, v)
						}
					}
				case *ast.Ident:
					if v, ok := local[e.Name]; ok {
						keys = append(keys, v)
					}
				}
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s declares no %s", path, fn)
	}
	return keys
}

// packageStringConsts reads the string constants declared at the top level of
// every non-test file next to path.
func packageStringConsts(t *testing.T, path string) map[string]string {
	t.Helper()
	dir := path[:strings.LastIndex(path, "/")]
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), dir+"/"+name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							out[id.Name] = v
						}
					}
				}
			}
		}
	}
	return out
}
