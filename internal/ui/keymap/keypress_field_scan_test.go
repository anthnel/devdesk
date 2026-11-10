package keymap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// DEVDESK-279: bubbletea v2 gives tea.KeyPressMsg (and tea.Key) the fields
// Code, Text and Mod. Production code reads a key through String() only
// (DEVDESK-200), so those fields are refused too. legacy_key_scan_test.go covers
// the v1-only fields; this file covers the v2 ones.
//
// The scanner is syntactic like its siblings (no go/types). A read X.Code,
// X.Text or X.Mod is flagged when X is a key value, within one top-level
// declaration. X is a key value when it is:
//   - an identifier declared with type tea.KeyPressMsg / tea.Key as a parameter,
//     result, receiver or var (a struct field is not a declaration it sees);
//   - an identifier assigned from a key value: a type assertion to one of those
//     types, a composite literal, a conversion, a call m.Key() on a key value
//     (KeyPressMsg.Key() returns a tea.Key), or another key name (k := msg).
//     Assignments are followed to a fixed point, so chains (a := msg;
//     b := a.Key()) resolve whatever their order;
//   - the binding of a type switch, but only inside the one clause whose single
//     type is a key type (`switch msg := msg.(type)` leaves msg an ordinary name
//     in the other clauses);
//   - or a type assertion / conversion / composite literal / .Key() call used
//     directly.
//
// Not covered (each one escapes the guard):
//   - a struct that embeds or holds a tea.KeyPressMsg / tea.Key (x.Code through
//     the embedding, or s.key.Code through a field);
//   - a type alias or defined type of a key type (type K = tea.Key);
//   - a key obtained from a function result other than .Key() (f().Code), and
//     from a range or channel receive;
//   - a name reused for another type in the same declaration outside a type
//     switch clause: tracking is by name, so that other use is a false positive.
//
// Other types with Code/Text/Mod (project structs, textinput) are not flagged.

var keyValueTypes = map[string]bool{"KeyPressMsg": true, "Key": true}

// isKeyType reports whether e names tea.KeyPressMsg or tea.Key (optionally behind a pointer).
func isKeyType(e ast.Expr, teaName string) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == teaName && keyValueTypes[sel.Sel.Name]
}

// scopedName is a key name valid only between two positions (a type switch clause).
type scopedName struct {
	name     string
	from, to token.Pos
}

// keyTracker knows which identifiers of one declaration hold a key value.
type keyTracker struct {
	tea    string
	names  map[string]bool
	scoped []scopedName
}

// isKey reports whether the identifier, at its position, names a key value.
func (t *keyTracker) isKey(id *ast.Ident) bool {
	if t.names[id.Name] {
		return true
	}
	for _, s := range t.scoped {
		if s.name == id.Name && id.Pos() >= s.from && id.Pos() < s.to {
			return true
		}
	}
	return false
}

// valued reports whether e is an expression that evidently has a key type:
// m.(tea.KeyPressMsg), tea.KeyPressMsg{...}, tea.Key(x), a tracked name, or
// .Key() called on one of those.
func (t *keyTracker) valued(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return t.valued(v.X)
	case *ast.Ident:
		return t.isKey(v)
	case *ast.TypeAssertExpr:
		return v.Type != nil && isKeyType(v.Type, t.tea)
	case *ast.CompositeLit:
		return v.Type != nil && isKeyType(v.Type, t.tea)
	case *ast.CallExpr:
		if isKeyType(v.Fun, t.tea) {
			return true
		}
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Key" && len(v.Args) == 0 {
			return t.valued(sel.X)
		}
	}
	return false
}

// mark records a key name and reports whether it was new.
func (t *keyTracker) mark(name string) bool {
	if name == "_" || t.names[name] {
		return false
	}
	t.names[name] = true
	return true
}

// keyNames collects the identifiers of one declaration that hold a key value.
func keyNames(decl ast.Decl, teaName string) *keyTracker {
	t := &keyTracker{tea: teaName, names: map[string]bool{}}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if isKeyType(f.Type, teaName) {
				for _, n := range f.Names {
					t.mark(n.Name)
				}
			}
		}
	}
	// Declared types and type switch clauses do not depend on other names.
	ast.Inspect(decl, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			addFields(v.Recv)
			addFields(v.Type.Params)
			addFields(v.Type.Results)
		case *ast.FuncLit:
			addFields(v.Type.Params)
			addFields(v.Type.Results)
		case *ast.ValueSpec:
			if v.Type != nil && isKeyType(v.Type, teaName) {
				for _, id := range v.Names {
					t.mark(id.Name)
				}
			}
		case *ast.TypeSwitchStmt:
			as, ok := v.Assign.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 {
				break
			}
			id, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				break
			}
			for _, c := range v.Body.List {
				cc := c.(*ast.CaseClause)
				if len(cc.List) == 1 && isKeyType(cc.List[0], teaName) {
					t.scoped = append(t.scoped, scopedName{id.Name, cc.Pos(), cc.End()})
				}
			}
		}
		return true
	})
	// Assignments from a key value, to a fixed point: k := msg.Key() needs msg,
	// and a later alias needs k.
	for changed := true; changed; {
		changed = false
		ast.Inspect(decl, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.ValueSpec:
				for i, val := range v.Values {
					if i < len(v.Names) && t.valued(val) && t.mark(v.Names[i].Name) {
						changed = true
					}
				}
			case *ast.AssignStmt:
				// x := m.(tea.KeyPressMsg) and x, ok := m.(tea.KeyPressMsg)
				if len(v.Rhs) == 1 && len(v.Lhs) > 0 && t.valued(v.Rhs[0]) {
					if id, ok := v.Lhs[0].(*ast.Ident); ok && t.mark(id.Name) {
						changed = true
					}
				}
				if len(v.Lhs) == len(v.Rhs) {
					for i, r := range v.Rhs {
						if id, ok := v.Lhs[i].(*ast.Ident); ok && t.valued(r) && t.mark(id.Name) {
							changed = true
						}
					}
				}
			}
			return true
		})
	}
	return t
}

// keyPressFieldReads returns "path:line field" for every read of a v2 key field.
func keyPressFieldReads(fset *token.FileSet, path string, file *ast.File, teaName string) []string {
	if teaName == "" {
		return nil
	}
	var out []string
	for _, decl := range file.Decls {
		tr := keyNames(decl, teaName)
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Code", "Text", "Mod":
			default:
				return true
			}
			if tr.valued(sel.X) {
				out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(fset.Position(sel.Sel.Pos()).Line)+" ."+sel.Sel.Name)
			}
			return true
		})
	}
	return out
}

func TestKeyPressFieldReadsDetector(t *testing.T) {
	const head = "package p\nimport tea \"charm.land/bubbletea/v2\"\n"
	tests := []struct {
		name string
		src  string
		want int
	}{
		// Faulty reads: the guard must see them.
		{"code on a parameter", head + "func f(msg tea.KeyPressMsg) bool { return msg.Code == 'a' }\n", 1},
		{"text on a parameter", head + "func f(msg tea.KeyPressMsg) string { return msg.Text }\n", 1},
		{"mod on a parameter", head + "func f(msg tea.KeyPressMsg) bool { return msg.Mod != 0 }\n", 1},
		{"tea.Key parameter", head + "func f(k tea.Key) rune { return k.Code }\n", 1},
		{"pointer parameter", head + "func f(k *tea.Key) rune { return k.Code }\n", 1},
		{"receiver", head + "type m struct{}\nfunc (m) f(k tea.KeyPressMsg) string { return k.Text }\n", 1},
		{"type switch binding", head + "func f(m tea.Msg) rune {\n\tswitch k := m.(type) {\n\tcase tea.KeyPressMsg:\n\t\treturn k.Code\n\t}\n\treturn 0\n}\n", 1},
		{"assertion binding", head + "func f(m tea.Msg) string {\n\tk, ok := m.(tea.KeyPressMsg)\n\tif !ok {\n\t\treturn \"\"\n\t}\n\treturn k.Text\n}\n", 1},
		{"direct assertion", head + "func f(m tea.Msg) rune { return m.(tea.KeyPressMsg).Code }\n", 1},
		{"var declaration", head + "func f() rune {\n\tvar k tea.KeyPressMsg\n\treturn k.Code\n}\n", 1},
		{"composite literal binding", head + "func f() string {\n\tk := tea.KeyPressMsg{}\n\treturn k.Text\n}\n", 1},
		{"closure parameter", head + "var g = func(k tea.KeyPressMsg) string { return k.Text }\n", 1},
		{"aliased import", "package p\nimport bt \"charm.land/bubbletea/v2\"\nfunc f(k bt.KeyPressMsg) rune { return k.Code }\n", 1},
		{"two fields", head + "func f(k tea.KeyPressMsg) bool { return k.Code == 'a' && k.Mod == 0 }\n", 2},
		{"Key() chained", head + "func f(msg tea.KeyPressMsg) rune { return msg.Key().Code }\n", 1},
		{"Key() bound to a name", head + "func f(msg tea.KeyPressMsg) rune {\n\tk := msg.Key()\n\treturn k.Code\n}\n", 1},
		{"alias of a key name", head + "func f(msg tea.KeyPressMsg) string {\n\tk := msg\n\treturn k.Text\n}\n", 1},
		{"alias chain through Key()", head + "func f(msg tea.KeyPressMsg) tea.KeyMod {\n\ta := msg\n\tb := a.Key()\n\treturn b.Mod\n}\n", 1},
		{"Key() on a direct assertion", head + "func f(m tea.Msg) string { return m.(tea.KeyPressMsg).Key().Text }\n", 1},
		{"read in the key clause of a type switch", head + "type P struct{ Text string }\nfunc f(msg tea.Msg) rune {\n\tswitch msg := msg.(type) {\n\tcase tea.KeyPressMsg:\n\t\treturn msg.Code\n\tcase P:\n\t\treturn 0\n\t}\n\treturn 0\n}\n", 1},
		{"key clause and other clause both read", head + "type P struct{ Text string }\nfunc f(msg tea.Msg) string {\n\tswitch msg := msg.(type) {\n\tcase P:\n\t\treturn msg.Text\n\tcase tea.KeyPressMsg:\n\t\treturn msg.Text\n\t}\n\treturn \"\"\n}\n", 1},
		// Allowed or unrelated: no finding.
		{"type switch: other clause reads Text", head + "type P struct{ Text string }\nfunc f(msg tea.Msg) string {\n\tswitch msg := msg.(type) {\n\tcase tea.KeyPressMsg:\n\t\treturn msg.String()\n\tcase P:\n\t\treturn msg.Text\n\t}\n\treturn \"\"\n}\n", 0},
		{"Key() on a project struct", head + "type C struct{}\nfunc (C) Key() C { return C{} }\nfunc f(c C) int { _ = c.Key().Code; return 0 }\n", 0},
		{"String", head + "func f(k tea.KeyPressMsg) string { return k.String() }\n", 0},
		{"project struct", head + "type C struct{ Code int; Text string; Mod int }\nfunc f(c C) int { return c.Code + c.Mod + len(c.Text) }\n", 0},
		{"project struct beside a key", head + "type C struct{ Code int }\nfunc f(c C, k tea.KeyPressMsg) int { _ = k.String(); return c.Code }\n", 0},
		{"key literal field name", head + "var _ = tea.KeyPressMsg{Code: 'a', Text: \"a\"}\n", 0},
		{"paste message", head + "func f(p tea.PasteMsg) string { return p.Content }\n", 0},
		{"no bubbletea import", "package p\nfunc f(k struct{ Code int }) int { return k.Code }\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := keyPressFieldReads(fset, "x.go", file, teaImportName(file))
			if len(got) != tc.want {
				t.Errorf("findings = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestNoProductionCodeReadsKeyPressFields(t *testing.T) {
	files := 0
	walkProduction(t, func(fset *token.FileSet, path string, file *ast.File) {
		files++
		for _, where := range keyPressFieldReads(fset, path, file, teaImportName(file)) {
			t.Errorf("%s: reads a field of tea.KeyPressMsg/tea.Key; match msg.String() instead (DEVDESK-200, DEVDESK-279)", where)
		}
	})
	if files == 0 {
		t.Fatal("no production file scanned — the scan is broken, not the code")
	}
}
