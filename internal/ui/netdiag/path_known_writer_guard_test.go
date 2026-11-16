package netdiag

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-473: PathModel.known is the Diagnostics route reading the trace may
// reuse. The argument that it is never stale rests on one fact: the field is
// assigned in exactly one place, Model.openTrace, the only entry into the trace
// level, which rebuilds it from the results on screen on every call. A second
// writer (ready, clearRun, a reset) could leave a reading that outlives its
// results, or make one reading serve two hosts.
//
// What the guard sees, in the non-test sources of the package: an assignment,
// an increment or a range clause whose target goes through a field named
// "known" (known, known.lookups, known.lookups[i]); the address of such a
// field (&pm.known), which hands out a writable alias; a "known:" key in a
// composite literal; and a store through a dereferenced pointer (*pm = ...),
// which copies every field of a PathModel, known included.
//
// The only exception is knownWriterException, Model.openTrace. A new one has to
// be added to that list, with its reason, in the same change.
//
// What it does not see, by construction: the type of the receiver (it matches
// the field name, and PathModel is the only type of the package that has one),
// a value copied out of a PathModel and written back through a method value or
// reflect, and a write from another package (the field is unexported).

// knownWriterException is the one function allowed to write the field: the
// method name and the receiver type name.
var knownWriterException = struct{ recv, name string }{"Model", "openTrace"}

// knownFieldName is the PathModel field the guard protects.
const knownFieldName = "known"

// knownWriteViolations parses one Go source and returns one line per write of
// the known field outside the exception.
func knownWriteViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	report := func(pos token.Pos, what string) {
		out = append(out, filepath.Base(filename)+":"+strconv.Itoa(fset.Position(pos).Line)+": "+what)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || isKnownWriterException(fn) {
			continue
		}
		scanKnownWrites(fn.Body, report)
	}
	// Package-level declarations (var x = PathModel{known: ...}) are not inside
	// a function body.
	for _, decl := range f.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			scanKnownWrites(gen, report)
		}
	}
	return out, nil
}

// isKnownWriterException reports whether fn is the one allowed writer.
func isKnownWriterException(fn *ast.FuncDecl) bool {
	if fn.Name.Name != knownWriterException.name || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == knownWriterException.recv
}

// scanKnownWrites reports every write of the known field under root.
func scanKnownWrites(root ast.Node, report func(token.Pos, string)) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				checkKnownTarget(lhs, report)
			}
		case *ast.IncDecStmt:
			checkKnownTarget(x.X, report)
		case *ast.RangeStmt:
			if x.Key != nil {
				checkKnownTarget(x.Key, report)
			}
			if x.Value != nil {
				checkKnownTarget(x.Value, report)
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && goesThroughKnown(x.X) {
				report(x.Pos(), "takes the address of the "+knownFieldName+" field, which is a writable alias")
			}
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && id.Name == knownFieldName {
				report(x.Pos(), "sets the "+knownFieldName+" field in a composite literal")
			}
		}
		return true
	})
}

// checkKnownTarget reports an assignment target that writes the known field,
// or the whole struct it belongs to through a pointer.
func checkKnownTarget(lhs ast.Expr, report func(token.Pos, string)) {
	if goesThroughKnown(lhs) {
		report(lhs.Pos(), "writes the "+knownFieldName+" field")
		return
	}
	if star, ok := lhs.(*ast.StarExpr); ok {
		report(star.Pos(), "stores through a pointer, which overwrites every field of the struct, "+knownFieldName+" included")
	}
}

// goesThroughKnown reports whether e is a selector chain, possibly indexed or
// parenthesised, that passes through a field named known: pm.known,
// pm.known.lookups, pm.known.lookups[0].
func goesThroughKnown(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == knownFieldName {
				return true
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return false
		}
	}
}

func TestOnlyOpenTraceWritesTheKnownRoutes(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source file found (err %v)", err)
	}
	scanned := 0
	sawException := false
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := knownWriteViolations(file, raw)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, v := range got {
			t.Errorf("%s: only Model.openTrace may write PathModel.known (DEVDESK-473)", v)
		}
		if strings.Contains(string(raw), "m.pathModel.known = ") {
			sawException = true
		}
		scanned++
	}
	if scanned < 10 {
		t.Fatalf("scanned %d files: the guard would pass on nothing", scanned)
	}
	if !sawException {
		t.Fatal("the allowed writer was not found: the guard may be looking at the wrong thing")
	}
}

// The detector itself: it fails on each way of writing the field, and passes on
// reads and on the one allowed writer.
func TestKnownWriteViolationsDetectsWrites(t *testing.T) {
	const head = "package netdiag\n"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"openTrace is the exception", "func (m *Model) openTrace() { m.pathModel.known = knownRoutesOf(\"\", m.results) }", 0},
		{"openTrace on a value receiver is the exception", "func (m Model) openTrace() { m.pathModel.known = x }", 0},
		{"assignment in ready", "func (pm *PathModel) ready() { pm.known = pathKnownRoutes{} }", 1},
		{"assignment in clearRun", "func (pm *PathModel) clearRun() { pm.known = pm.known }", 1},
		{"short-name assignment", "func f(pm *PathModel) { pm.known, x = a, b }", 1},
		{"assignment from another Model method", "func (m *Model) other() { m.pathModel.known = pathKnownRoutes{} }", 1},
		{"an openTrace of another type is not the exception", "func (pm *PathModel) openTrace() { pm.known = x }", 1},
		{"a function named openTrace is not the exception", "func openTrace(pm *PathModel) { pm.known = x }", 1},
		{"field of the field", "func f(pm *PathModel) { pm.known.host = \"x\" }", 1},
		{"element of the lookups", "func f(pm *PathModel) { pm.known.lookups[0] = l }", 1},
		{"parenthesised", "func f(pm *PathModel) { (pm.known) = x }", 1},
		{"op-assign", "func f(pm *PathModel) { pm.known.n += 1 }", 1},
		{"increment", "func f(pm *PathModel) { pm.known.n++ }", 1},
		{"range target", "func f(pm *PathModel) { for pm.known.n = range 3 {} }", 1},
		{"address of the field", "func f(pm *PathModel) { p := &pm.known; _ = p }", 1},
		{"address of a lookup", "func f(pm *PathModel) { p := &pm.known.lookups[0]; _ = p }", 1},
		{"composite literal key", "func f() *PathModel { return &PathModel{known: pathKnownRoutes{}} }", 1},
		{"package-level literal", "var pm = PathModel{known: pathKnownRoutes{}}", 1},
		{"store through a pointer", "func f(pm, o *PathModel) { *pm = *o }", 1},
		{"store through a pointer to a field", "func f(m *Model, o PathModel) { *m.pathModel = o }", 1},
		{"read of the field", "func f(pm *PathModel) { x := pm.known; _ = x }", 0},
		{"read through a method", "func f(pm *PathModel) { pm.known.find(\"h\", a) }", 0},
		{"read as an argument", "func f(pm *PathModel) { g(pm.known.routeFirst(\"h\", r)) }", 0},
		{"assigning a local called known", "func f() { known := 1; known = 2; _ = known }", 0},
		{"another field assigned", "func f(pm *PathModel) { pm.target = t; pm.source = s }", 0},
		{"a constructor without the field", "func f() *PathModel { return &PathModel{state: pathReady} }", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := knownWriteViolations("synthetic.go", head+tc.body+"\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Errorf("violations = %v, want %d", got, tc.want)
			}
		})
	}
}
