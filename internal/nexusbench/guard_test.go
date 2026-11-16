package nexusbench

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Source guards of DEVDESK-504 (CA7). They read the package with go/parser and
// fail naming file and line, like internal/ui/keymap/*_scan_test.go:
//
//   - a test that reaches the real cluster, the bench descriptor or the secrets
//     path calls requireBench(t) first, so `mise run test` can never act on
//     datomir;
//   - nothing but kubectl.go and secrets.go starts a process, so the bench has
//     one door to the cluster (kubectl.go) and one to git (secrets.go);
//   - the credentials of the REST client are attached in one place and no code
//     names the Authorization header, so no log or error can carry it.
//
// Every detector has a self-test that feeds it a faulty source and a correct
// one held in memory. A failure of a guard points at code that breaks the rule,
// not at a test to relax. Limits are stated at each guard.

// ---- parsing ----------------------------------------------------------------

type parsedSource struct {
	path string // slash-separated, relative to the package folder
	fset *token.FileSet
	file *ast.File
}

func parseSource(t *testing.T, path, src string) parsedSource {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return parsedSource{path: path, fset: fset, file: f}
}

func (p parsedSource) at(n ast.Node) string {
	return p.path + ":" + strconv.Itoa(p.fset.Position(n.Pos()).Line)
}

// packageSources parses the Go files of the package folder, and of the folders
// below it when recursive. Test files are kept or dropped as asked; testdata is
// never read.
func packageSources(t *testing.T, tests, recursive bool) []parsedSource {
	t.Helper()
	var out []parsedSource
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (!recursive || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") != tests {
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // a source file of this package, found by the walk above
		if err != nil {
			return err
		}
		out = append(out, parseSource(t, filepath.ToSlash(path), string(src)))
		return nil
	})
	if err != nil {
		t.Fatalf("read the package sources: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("the guard found no source file: it would pass on nothing")
	}
	return out
}

// ---- guard 1: requireBench ---------------------------------------------------

// benchGuardedCalls are the calls that reach the real bench: kubectl on PATH,
// the versioned descriptor that names the bench, the secrets path of the bench,
// and a REST client. A test (or helper) that makes one has to call
// requireBench(t) first, unless it is an exception below.
var benchGuardedCalls = []string{"NewExecRunner", "DescriptorPath", "DefaultSecretsPath", "newClient"}

// benchException is a function allowed to make a guarded call without
// requireBench, because what it does cannot reach the bench. check, when set,
// verifies the reason in the source of the function, so the exception cannot
// outlive it.
type benchException struct {
	reason string
	check  func(*ast.FuncDecl) string
}

// requireBenchExceptions are named, never matched by pattern. TestGuardExceptionsStillMatch
// fails when one stops matching a function that makes a guarded call.
var requireBenchExceptions = map[string]benchException{
	// DescriptorPath is only compared with a path; the file is not read.
	"TestDescriptorPath": {reason: "compares the path, reads no file"},
	// A pure path computation over a map standing in for the environment.
	"TestDefaultSecretsPath": {reason: "pure path computation", check: noRealEnvironment},
	// The client of this test is only inspected by reflection; it never sends.
	"TestNewClientSettings": {reason: "inspects the fields of a client that sends nothing"},
	// The four tests of NewExecRunner run against an empty PATH or a shell
	// script standing in for kubectl (installFakeKubectl).
	"TestNewExecRunnerKubectlMissing":         {reason: "PATH is an empty folder", check: isolatesPath},
	"TestExecRunnerInjectsTheContext":         {reason: "PATH leads to a fake kubectl script", check: isolatesPath},
	"TestExecRunnerKeepsBothStreamsOnFailure": {reason: "PATH leads to a fake kubectl script", check: isolatesPath},
	"TestExecRunnerHonorsACancelledContext":   {reason: "PATH leads to a fake kubectl script", check: isolatesPath},
}

// callName returns the name a call is made under: f(...) or nexusbench.f(...).
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok && x.Name == "nexusbench" {
			return fn.Sel.Name
		}
	}
	return ""
}

// isHTTPTestURL tells whether e reads the address of a test server: x.URL,
// x.URL() or one of them followed by a path.
func isHTTPTestURL(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "URL"
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "URL" && len(x.Args) == 0
	case *ast.BinaryExpr:
		return x.Op == token.ADD && isHTTPTestURL(x.X)
	case *ast.ParenExpr:
		return isHTTPTestURL(x.X)
	}
	return false
}

// guardedCalls returns the calls under n that reach the bench. A newClient call
// whose URL is the address of a test server does not.
func guardedCalls(n ast.Node) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if !slices.Contains(benchGuardedCalls, name) {
			return true
		}
		if name == "newClient" && len(call.Args) > 0 && isHTTPTestURL(call.Args[0]) {
			return true
		}
		out = append(out, call)
		return true
	})
	return out
}

// firstRequireBench returns the position of the first requireBench call under n.
func firstRequireBench(n ast.Node) (token.Pos, bool) {
	pos, found := token.NoPos, false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callName(call) == "requireBench" && (!found || call.Pos() < pos) {
			pos, found = call.Pos(), true
		}
		return true
	})
	return pos, found
}

// isolatesPath tells whether the function replaces PATH: installFakeKubectl(t)
// or t.Setenv("PATH", ...).
func isolatesPath(fn *ast.FuncDecl) string {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "installFakeKubectl" {
			found = true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Setenv" && len(call.Args) > 0 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == `"PATH"` {
				found = true
			}
		}
		return true
	})
	if !found {
		return "it neither calls installFakeKubectl nor sets PATH, so NewExecRunner may find the real kubectl"
	}
	return ""
}

// noRealEnvironment tells whether DefaultSecretsPath is given something other
// than os.Getenv, so it never reads the environment of the machine.
func noRealEnvironment(fn *ast.FuncDecl) string {
	problem := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || callName(call) != "DefaultSecretsPath" || len(call.Args) < 3 {
			return true
		}
		if sel, ok := call.Args[2].(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && sel.Sel.Name == "Getenv" {
				problem = "DefaultSecretsPath reads the real environment (os.Getenv)"
			}
		}
		return true
	})
	return problem
}

// benchScan is what the requireBench guard found in one file.
type benchScan struct {
	findings []string
	// excepted are the exceptions this file used: a function that makes a
	// guarded call without requireBench and is named in the exceptions.
	excepted []string
}

// scanRequireBench checks every function of src that makes a guarded call.
func scanRequireBench(src parsedSource, exceptions map[string]benchException) benchScan {
	var res benchScan
	for _, decl := range src.file.Decls {
		calls := guardedCalls(decl)
		if len(calls) == 0 {
			continue
		}
		// Only a plain function can be an exception: a method or a variable
		// that shares the name of one does not inherit it.
		name, fn := "", (*ast.FuncDecl)(nil)
		if f, ok := decl.(*ast.FuncDecl); ok && f.Recv == nil {
			name, fn = f.Name.Name, f
		}
		first := calls[0]
		if guard, ok := firstRequireBench(decl); ok && guard < first.Pos() {
			continue
		}
		what := callName(first) + " reaches the bench"
		ex, excepted := exceptions[name]
		switch {
		case fn == nil || !excepted:
			where := "outside a plain function"
			if fn != nil {
				where = "in " + name
			}
			res.findings = append(res.findings, src.at(first)+": "+what+" "+where+" without a requireBench(t) call before it")
		default:
			if problem := ex.run(fn); problem != "" {
				res.findings = append(res.findings, src.at(first)+": exception for "+name+" no longer holds: "+problem)
			}
			res.excepted = append(res.excepted, name)
		}
	}
	return res
}

func (e benchException) run(fn *ast.FuncDecl) string {
	if e.check == nil {
		return ""
	}
	return e.check(fn)
}

// staleExceptions returns the exceptions that no function used.
func staleExceptions(exceptions map[string]benchException, used []string) []string {
	var stale []string
	for name := range exceptions {
		if !slices.Contains(used, name) {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	return stale
}

// Limit (stated, not hidden): the guard reads calls by name in the test files
// of this folder. A guarded call hidden behind a function value, or made from
// the tests of cmd/nexus-bench (which run on fakes, with their own package),
// is not seen.
func scanAllRequireBench(t *testing.T) (findings, used []string) {
	t.Helper()
	for _, src := range packageSources(t, true, false) {
		res := scanRequireBench(src, requireBenchExceptions)
		findings = append(findings, res.findings...)
		used = append(used, res.excepted...)
	}
	return findings, used
}

func TestGuardBenchTestsCallRequireBench(t *testing.T) {
	findings, _ := scanAllRequireBench(t)
	for _, f := range findings {
		t.Error(f)
	}
}

// An exception that matches nothing (the test was renamed, deleted, or now
// calls requireBench) is a hole in waiting: it has to go.
func TestGuardExceptionsStillMatch(t *testing.T) {
	_, used := scanAllRequireBench(t)
	for _, name := range staleExceptions(requireBenchExceptions, used) {
		t.Errorf("requireBenchExceptions lists %s, but no function of that name makes a guarded call without requireBench: remove the entry", name)
	}
}

func TestGuardRequireBenchDetectorSelfTest(t *testing.T) {
	exceptions := map[string]benchException{
		"TestPure":       {reason: "pure"},
		"TestFakePath":   {reason: "fake kubectl", check: isolatesPath},
		"TestFakeSecret": {reason: "fake env", check: noRealEnvironment},
		"TestGone":       {reason: "no such function"},
		"TestNowGuarded": {reason: "calls requireBench now"},
	}
	tests := []struct {
		name         string
		src          string
		wantFindings int
		wantExcepted []string
	}{
		{"kubectl without the guard", `package p
func TestA(t *testing.T) { r, _ := NewExecRunner("c"); _ = r }`, 1, nil},
		{"descriptor without the guard", `package p
func TestA(t *testing.T) { _ = DescriptorPath() }`, 1, nil},
		{"secrets path without the guard", `package p
func TestA(t *testing.T) { _, _ = DefaultSecretsPath("c", "n", os.Getenv) }`, 1, nil},
		{"qualified call without the guard", `package p
func TestA(t *testing.T) { _ = nexusbench.DescriptorPath() }`, 1, nil},
		{"literal URL without the guard", `package p
func TestA(t *testing.T) { _ = newClient("http://127.0.0.1:30481", "", "") }`, 1, nil},
		{"guard after the call", `package p
func TestA(t *testing.T) { _ = DescriptorPath(); requireBench(t) }`, 1, nil},
		{"guard in the wrong function", `package p
func TestA(t *testing.T) { requireBench(t) }
func TestB(t *testing.T) { _ = DescriptorPath() }`, 1, nil},
		{"call in a package variable", `package p
var path = DescriptorPath()`, 1, nil},
		{"call in a closure, no guard", `package p
func TestA(t *testing.T) { t.Run("x", func(t *testing.T) { _ = DescriptorPath() }) }`, 1, nil},
		{"guard first", `package p
func TestA(t *testing.T) { requireBench(t); r, _ := NewExecRunner("c"); _ = r }`, 0, nil},
		{"guard in the outer test, call in a closure", `package p
func TestA(t *testing.T) { requireBench(t); t.Run("x", func(t *testing.T) { _ = DescriptorPath() }) }`, 0, nil},
		{"client on an httptest URL (field)", `package p
func TestA(t *testing.T) { _ = newClient(srv.URL, "a", "b") }`, 0, nil},
		{"client on an httptest URL (method)", `package p
func TestA(t *testing.T) { _ = newClient(fake.URL(), "a", "b") }`, 0, nil},
		{"client on an httptest URL plus a path", `package p
func TestA(t *testing.T) { _ = newClient(srv.URL+"/x", "a", "b") }`, 0, nil},
		{"no guarded call at all", `package p
func TestA(t *testing.T) { _ = os.Getenv("X") }`, 0, nil},
		{"named exception", `package p
func TestPure(t *testing.T) { _ = DescriptorPath() }`, 0, []string{"TestPure"}},
		{"exception that holds: fake PATH", `package p
func TestFakePath(t *testing.T) { installFakeKubectl(t); r, _ := NewExecRunner("c"); _ = r }`, 0, []string{"TestFakePath"}},
		{"exception that holds: PATH set", `package p
func TestFakePath(t *testing.T) { t.Setenv("PATH", d); r, _ := NewExecRunner("c"); _ = r }`, 0, []string{"TestFakePath"}},
		{"exception that no longer holds: real PATH", `package p
func TestFakePath(t *testing.T) { r, _ := NewExecRunner("c"); _ = r }`, 1, []string{"TestFakePath"}},
		{"exception that holds: fake environment", `package p
func TestFakeSecret(t *testing.T) { _, _ = DefaultSecretsPath("c", "n", fakeEnv(m)) }`, 0, []string{"TestFakeSecret"}},
		{"exception that no longer holds: real environment", `package p
func TestFakeSecret(t *testing.T) { _, _ = DefaultSecretsPath("c", "n", os.Getenv) }`, 1, []string{"TestFakeSecret"}},
		{"a method does not inherit the exception of a function of the same name", `package p
func (b benchEnv) TestPure() { _ = DescriptorPath() }`, 1, nil},
		{"an exception does not cover a variable", `package p
var TestPure = DescriptorPath()`, 1, nil},
		{"a guarded function listed as an exception is stale", `package p
func TestNowGuarded(t *testing.T) { requireBench(t); _ = DescriptorPath() }`, 0, nil},
	}
	var used []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := scanRequireBench(parseSource(t, "x_test.go", tt.src), exceptions)
			if len(res.findings) != tt.wantFindings {
				t.Errorf("findings = %q, want %d", res.findings, tt.wantFindings)
			}
			if !slices.Equal(res.excepted, tt.wantExcepted) {
				t.Errorf("exceptions used = %q, want %q", res.excepted, tt.wantExcepted)
			}
			used = append(used, res.excepted...)
		})
	}
	if got, want := staleExceptions(exceptions, used), []string{"TestGone", "TestNowGuarded"}; !slices.Equal(got, want) {
		t.Errorf("stale exceptions = %q, want %q", got, want)
	}
	if got := staleExceptions(map[string]benchException{"TestPure": {}}, []string{"TestPure"}); len(got) != 0 {
		t.Errorf("stale exceptions of a used one = %q, want none", got)
	}
}

// ---- guard 2: one door to the cluster, one to git ------------------------------

// execAllowed are the only non-test files that start a process: kubectl.go runs
// kubectl, secrets.go runs `git check-ignore` before it accepts a secrets path.
var execAllowed = []string{"kubectl.go", "secrets.go"}

// execStarts returns "file:line: exec.Command" for every use of Command or
// CommandContext of os/exec in src, whatever the import is called, and for a dot
// import of the package (which hides the calls).
//
// Limit (stated, not hidden): syscall.Exec, os.StartProcess and a process
// started by another package are not seen; the package imports none of them.
func execStarts(src parsedSource) []string {
	local := ""
	var out []string
	for _, imp := range src.file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "os/exec" {
			continue
		}
		local = "exec"
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if local == "." {
			out = append(out, src.at(imp)+": dot import of os/exec")
		}
	}
	if local == "" || local == "." || local == "_" {
		return out
	}
	ast.Inspect(src.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
			out = append(out, src.at(sel)+": exec."+sel.Sel.Name)
		}
		return true
	})
	return out
}

func TestGuardExecCommandOnlyInKubectlAndSecrets(t *testing.T) {
	seen := map[string]bool{}
	for _, src := range packageSources(t, false, true) {
		starts := execStarts(src)
		if len(starts) > 0 {
			seen[src.path] = true
		}
		if slices.Contains(execAllowed, src.path) {
			continue
		}
		for _, s := range starts {
			t.Errorf("%s: only %v may start a process", s, execAllowed)
		}
	}
	for _, name := range execAllowed {
		if !seen[name] {
			t.Errorf("%s is allowed to start a process but does not: drop it from execAllowed", name)
		}
	}
}

func TestGuardExecDetectorSelfTest(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"Command", `package p
import "os/exec"
func f() { _ = exec.Command("kubectl") }`, 1},
		{"CommandContext", `package p
import "os/exec"
func f(ctx context.Context) { _ = exec.CommandContext(ctx, "git") }`, 1},
		{"renamed import", `package p
import run "os/exec"
func f() { _ = run.Command("kubectl") }`, 1},
		{"function value", `package p
import "os/exec"
var start = exec.Command`, 1},
		{"dot import", `package p
import . "os/exec"
func f() { _ = Command("kubectl") }`, 1},
		{"two calls", `package p
import "os/exec"
func f() { _ = exec.Command("a"); _ = exec.Command("b") }`, 2},
		{"LookPath only", `package p
import "os/exec"
func f() { _, _ = exec.LookPath("kubectl") }`, 0},
		{"error type only", `package p
import "os/exec"
func f(err error) { var e *exec.ExitError; _ = errors.As(err, &e) }`, 0},
		{"another package called exec", `package p
func f() { _ = exec.Command("x") }`, 0},
		{"a method named Command", `package p
import "os/exec"
func f(c cli) { _ = c.Command("x") }`, 0},
		{"no import", `package p
func f() {}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execStarts(parseSource(t, "render.go", tt.src)); len(got) != tt.want {
				t.Errorf("execStarts = %q, want %d", got, tt.want)
			}
		})
	}
}

// ---- guard 3: no Authorization header in a log or an error --------------------

// credentialUse is one place a source names or attaches the credentials of a
// request.
type credentialUse struct {
	kind  string // "authorization", "basic-auth" or "http dump"
	where string
}

// The word Authorization may appear only in the files that talk HTTP to Nexus,
// and credentials are attached by the client alone.
var (
	authorizationAllowed = []string{"nexusapi.go", "probe.go"}
	basicAuthAllowed     = []string{"nexusapi.go"}
)

// credentialUses returns every identifier or string that contains
// "authorization" (any case), every call of a SetBasicAuth method, and every
// import of net/http/httputil (whose dumps print the headers). Comments are not
// read: they may explain why nothing is logged.
//
// Limit (stated, not hidden): a header named by concatenation, or printed with
// %v of a whole http.Header, is not seen; the client never keeps the request or
// its headers in an error (APIError and transportError hold the operation, the
// status and the cause only).
func credentialUses(src parsedSource) []credentialUse {
	var out []credentialUse
	for _, imp := range src.file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == "net/http/httputil" {
			out = append(out, credentialUse{"http dump", src.at(imp)})
		}
	}
	ast.Inspect(src.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING && strings.Contains(strings.ToLower(x.Value), "authorization") {
				out = append(out, credentialUse{"authorization", src.at(x)})
			}
		case *ast.Ident:
			if strings.Contains(strings.ToLower(x.Name), "authorization") {
				out = append(out, credentialUse{"authorization", src.at(x)})
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "SetBasicAuth" {
				out = append(out, credentialUse{"basic-auth", src.at(x)})
			}
		}
		return true
	})
	return out
}

func TestGuardNoAuthorizationHeaderOutsideTheClient(t *testing.T) {
	for _, src := range packageSources(t, false, true) {
		for _, use := range credentialUses(src) {
			switch {
			case use.kind == "authorization" && slices.Contains(authorizationAllowed, src.path):
			case use.kind == "basic-auth" && slices.Contains(basicAuthAllowed, src.path):
			default:
				t.Errorf("%s: %s outside %v: the Authorization header must not reach a log or an error", use.where, use.kind, authorizationAllowed)
			}
		}
	}
}

func TestGuardCredentialDetectorSelfTest(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"header set by name", `package p
func f(r *http.Request) { r.Header.Set("Authorization", "x") }`, []string{"authorization"}},
		{"header read in lower case", `package p
func f(r *http.Request) string { return r.Header.Get("authorization") }`, []string{"authorization"}},
		{"header logged", `package p
func f(r *http.Request) { log.Printf("sent %s", r.Header.Get("Authorization")) }`, []string{"authorization"}},
		{"identifier", `package p
const authorizationHeader = "Authorization"`, []string{"authorization", "authorization"}},
		{"basic auth", `package p
func f(r *http.Request) { r.SetBasicAuth("u", "p") }`, []string{"basic-auth"}},
		{"request dump", `package p
import "net/http/httputil"
func f(r *http.Request) { b, _ := httputil.DumpRequestOut(r, true); _ = b }`, []string{"http dump"}},
		{"comment only", `package p
// the client never logs the Authorization header
func f() {}`, nil},
		{"plain code", `package p
func f(r *http.Request) { r.Header.Set("Content-Type", "application/json") }`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var kinds []string
			for _, u := range credentialUses(parseSource(t, "render.go", tt.src)) {
				kinds = append(kinds, u.kind)
			}
			if !slices.Equal(kinds, tt.want) {
				t.Errorf("uses = %q, want %q", kinds, tt.want)
			}
		})
	}
}
