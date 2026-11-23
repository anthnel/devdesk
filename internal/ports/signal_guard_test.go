package ports

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unreachablePID is above the default Linux pid_max (4194304), so even a guard
// that failed to stop a signal would have no process to reach.
const unreachablePID = 4194305

// recoverPanic runs f and returns what it panicked with, or nil.
func recoverPanic(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

// ── Run-time guard ───────────────────────────────────────────────────────────

// A test binary that did not ask for a real signal must not send one: the
// panic is what makes a test that forgot to substitute procOS fail loudly
// instead of killing something on the developer's machine.
func TestARealSignalInATestBinaryPanics(t *testing.T) {
	if realSignalsAllowed.Load() {
		t.Fatal("real signals are allowed at the start of a test; a previous test leaked allowRealSignals")
	}
	got := recoverPanic(func() { _ = realOS{}.kill(unreachablePID, IdentityForTest(1)) })
	if got == nil {
		t.Fatal("realOS.kill did not panic in a test binary that did not allow real signals")
	}
	msg := fmt.Sprint(got)
	for _, want := range []string{"DEVDESK-708", strconv.Itoa(unreachablePID)} {
		if !strings.Contains(msg, want) {
			t.Errorf("panic message %q does not contain %q", msg, want)
		}
	}
}

// Kill reaches the signal through the default procOS: with no substitution, a
// test calling Kill must hit the guard, not the machine.
func TestKillWithoutASubstitutedProcOSHitsTheGuard(t *testing.T) {
	if _, ok := procOS.(realOS); !ok {
		t.Fatalf("procOS defaults to %T, want realOS", procOS)
	}
	got := recoverPanic(func() { _ = Kill(strconv.Itoa(unreachablePID), IdentityForTest(1)) })
	if got == nil {
		t.Fatal("Kill reached a real signal in a test binary without allowRealSignals")
	}
	if !strings.Contains(fmt.Sprint(got), "DEVDESK-708") {
		t.Errorf("panic = %v, want a message naming DEVDESK-708", got)
	}
}

// allowRealSignals is scoped to the test that calls it.
func TestAllowRealSignalsIsScopedToTheTest(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		allowRealSignals(t)
		if !realSignalsAllowed.Load() {
			t.Error("allowRealSignals did not allow real signals")
		}
	})
	if realSignalsAllowed.Load() {
		t.Error("real signals are still allowed after the test that asked for them ended")
	}
}

// ── Static guard ─────────────────────────────────────────────────────────────

// signalCalls are the names whose call is a way to end or signal a process.
// Matched on the selector name, whatever the receiver: p.Kill(), p.Signal(),
// os.FindProcess, syscall.Kill, unix.Kill, windows.TerminateProcess.
var signalCalls = map[string]bool{
	"FindProcess":      true,
	"Kill":             true,
	"Signal":           true,
	"TerminateProcess": true,
}

// TestOnlyRealOSSendsASignal: (realOS).kill is the one place of the package
// that signals a process, and its first statement is the run-time guard.
//
// Limits: an import alias is not resolved (the selector name is what is read),
// and another package or an external binary is not seen; the Ports tab reaches
// this package only through Kill.
func TestOnlyRealOSSendsASignal(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fset := token.NewFileSet()
	foundRealOSKill := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if isRealOSKill(fn) {
				foundRealOSKill = true
				checkGuardIsFirst(t, fset, fn)
				continue
			}
			reportSignalCalls(t, fset, fn)
		}
	}
	if !foundRealOSKill {
		t.Error("no method (realOS).kill in the package: the single point that sends a signal is missing")
	}
}

// isRealOSKill reports whether fn is the method `func (realOS) kill(...)`.
func isRealOSKill(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != "kill" {
		return false
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && id.Name == "realOS"
}

// checkGuardIsFirst requires that the first statement of fn is a call to
// refuseRealSignalUnderTest.
func checkGuardIsFirst(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()
	if len(fn.Body.List) == 0 {
		t.Errorf("%s: (realOS).kill is empty", fset.Position(fn.Pos()))
		return
	}
	stmt, ok := fn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		t.Errorf("%s: the first statement of (realOS).kill must be the call to refuseRealSignalUnderTest", fset.Position(fn.Body.List[0].Pos()))
		return
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		t.Errorf("%s: the first statement of (realOS).kill must be the call to refuseRealSignalUnderTest", fset.Position(stmt.Pos()))
		return
	}
	if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "refuseRealSignalUnderTest" {
		t.Errorf("%s: the first statement of (realOS).kill must be the call to refuseRealSignalUnderTest", fset.Position(stmt.Pos()))
	}
}

// reportSignalCalls fails for every call of a signalling function in fn.
func reportSignalCalls(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && signalCalls[sel.Sel.Name] {
			t.Errorf("%s: %s sends or prepares a signal; only (realOS).kill may (DEVDESK-708)",
				fset.Position(call.Pos()), sel.Sel.Name)
		}
		return true
	})
}

// The guard is only worth something if it sees a violation: run its detection
// on a source that breaks the rule.
func TestTheSignalScanSeesAViolation(t *testing.T) {
	src := `package p
import "os"
func Stop(pid int) error {
	p, _ := os.FindProcess(pid)
	return p.Kill()
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "violation.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && signalCalls[sel.Sel.Name] {
				hits = append(hits, sel.Sel.Name)
			}
		}
		return true
	})
	if len(hits) != 2 {
		t.Errorf("the scan found %v in a source with FindProcess and Kill, want both", hits)
	}
}
