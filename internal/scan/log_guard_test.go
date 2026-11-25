package scan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-775: the guard that keeps a name from the scanned repository out of
// the log as a raw word. DEVDESK-768 added logWord for that and Loggable() for
// a command; the calls it did not reach stayed raw, and nothing would have
// noticed the next one. Like the container guard (internal/netproxy,
// DEVDESK-583) it parses the production sources (go/ast, no type
// information) and names the file and line of each call that breaks the rule.
//
// The rule: every argument of a log.Print*, log.Fatal* or log.Panic* call in a
// non-test file of this package is one of
//
//   - a literal (and a concatenation of literals);
//   - an integer: the argument of a %d verb, or len(...);
//   - written by %q, which quotes whatever it is given;
//   - a call to logWord or to logErr (safeLogCalls), or the Loggable() of a
//     command, which quotes its words the same way.
//
// An argument that is none of them is a violation unless logGuardExceptions
// lists it, with its reason.
//
// What the guard cannot see, and which review has to:
//
//   - an import of "log" under an alias (l "log"), a dot import: the file is
//     not scanned, as nothing tells the guard that l.Printf logs;
//   - a call that does not go through the package-level functions: a logger
//     from log.New or log.Default(), fmt.Fprintf(log.Writer(), ...), log/slog,
//     a method value (p := log.Printf; p(...)), another package's logger;
//   - a verb with an explicit index or a star (%[1]s, %*d): from there the
//     arguments are all required to be safe on their own;
//   - a string passed to %d prints as %!d(string=...), raw: go vet's printf
//     check catches that, not this guard;
//   - safeLogCalls and Loggable are trusted by name: a function of that name
//     that does not quote is not noticed, and a new quoting helper (a
//     logWords for a list, say) has to be added to safeLogCalls on purpose;
//   - a value quoted before the call and passed as a variable: only the
//     arguments of the call are looked at, so `q := logWord(n); log.Print(q)`
//     is reported (use logWord at the call, or add an exception).

// safeLogCalls are the functions whose result is safe to print as a word.
var safeLogCalls = map[string]bool{
	"logWord": true, // a path or a name
	"logErr":  true, // an error whose text may hold one
}

// logGuardException is an argument allowed to stay as it is.
type logGuardException struct {
	File string // slash path from the module root
	Expr string // the argument as written
	Why  string
}

// logGuardExceptions is the whole list.
var logGuardExceptions = []logGuardException{}

// logViolation is one argument of one log call that is not safe.
type logViolation struct {
	File string
	Line int
	Expr string
}

func (v logViolation) String() string {
	return v.File + ":" + strconv.Itoa(v.Line) + ": " + v.Expr
}

func (e logGuardException) covers(v logViolation) bool {
	return e.File == v.File && e.Expr == v.Expr
}

// formatVerbs is the verb of each argument of a printf format, in order: 0 for
// an argument it cannot place (an index or a star was met, the verbs after it
// are not counted). "%%" takes no argument.
func formatVerbs(format string) []rune {
	var verbs []rune
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' {
			continue
		}
		i++
		for i < len(rs) && strings.ContainsRune("+-# 0123456789.", rs[i]) {
			i++
		}
		if i >= len(rs) || rs[i] == '*' || rs[i] == '[' {
			return verbs
		}
		if rs[i] != '%' {
			verbs = append(verbs, rs[i])
		}
	}
	return verbs
}

// isSafeLogArg reports whether arg, written by verb (0 when unknown, or for a
// Print or Println), cannot put a name of the repository in the log raw.
func isSafeLogArg(arg ast.Expr, verb rune) bool {
	if verb == 'd' || verb == 'q' {
		return true
	}
	switch a := arg.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return isSafeLogArg(a.X, 0)
	case *ast.BinaryExpr:
		return a.Op == token.ADD && isSafeLogArg(a.X, 0) && isSafeLogArg(a.Y, 0)
	case *ast.CallExpr:
		switch fn := a.Fun.(type) {
		case *ast.Ident:
			return safeLogCalls[fn.Name] || fn.Name == "len"
		case *ast.SelectorExpr:
			return fn.Sel.Name == "Loggable"
		}
	}
	return false
}

// isLogPrint reports whether call is log.Print*, log.Fatal* or log.Panic* and
// whether it takes a format.
func isLogPrint(call *ast.CallExpr) (isLog, formatted bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "log" {
		return false, false
	}
	for _, p := range []string{"Print", "Fatal", "Panic"} {
		if strings.HasPrefix(sel.Sel.Name, p) {
			return true, strings.HasSuffix(sel.Sel.Name, "f")
		}
	}
	return false, false
}

// importsLog reports whether the file imports "log" under its own name.
func importsLog(f *ast.File) bool {
	return slices.ContainsFunc(f.Imports, func(spec *ast.ImportSpec) bool {
		return spec.Path.Value == `"log"` && spec.Name == nil
	})
}

// scanLogSource parses src as the file name (a slash path from the module root)
// and returns the arguments of its log calls that are not safe, in source
// order, and how many log calls it holds.
func scanLogSource(name string, src []byte) ([]logViolation, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, err
	}
	if !importsLog(f) {
		return nil, 0, nil
	}
	var out []logViolation
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		isLog, formatted := isLogPrint(call)
		if !isLog {
			return true
		}
		calls++
		args := call.Args
		var verbs []rune
		if formatted && len(args) > 0 {
			lit, ok := args[0].(*ast.BasicLit)
			format, err := "", error(nil)
			if ok && lit.Kind == token.STRING {
				format, err = strconv.Unquote(lit.Value)
			}
			if !ok || lit.Kind != token.STRING || err != nil {
				out = append(out, logViolation{File: name, Line: fset.Position(args[0].Pos()).Line, Expr: types.ExprString(args[0])})
			}
			verbs, args = formatVerbs(format), args[1:]
		}
		for i, arg := range args {
			var verb rune
			if i < len(verbs) {
				verb = verbs[i]
			}
			if !isSafeLogArg(arg, verb) {
				out = append(out, logViolation{File: name, Line: fset.Position(arg.Pos()).Line, Expr: types.ExprString(arg)})
			}
		}
		return true
	})
	return out, calls, nil
}

// scanLogPackage scans the non-test .go files of dir (the package under test,
// as go test runs in it) and names them as internal/scan/<file>.
func scanLogPackage(dir string) (vs []logViolation, calls, files int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(dir + "/" + name) //nolint:gosec // a source file of this package
		if err != nil {
			return nil, 0, 0, err
		}
		v, n, err := scanLogSource("internal/scan/"+name, src)
		if err != nil {
			return nil, 0, 0, err
		}
		files++
		calls += n
		vs = append(vs, v...)
	}
	return vs, calls, files, nil
}

// No log call of this package prints a raw word.
func TestLogGuardEveryLoggedWordIsQuotedIfItCanHoldARepositoryName(t *testing.T) {
	vs, calls, files, err := scanLogPackage(".")
	if err != nil {
		t.Fatal(err)
	}
	// A walk that reads almost nothing would pass for the wrong reason.
	if files < 20 || calls < 20 {
		t.Fatalf("the guard read %d files and %d log calls, want the whole package", files, calls)
	}
	for _, v := range vs {
		if slices.ContainsFunc(logGuardExceptions, func(e logGuardException) bool { return e.covers(v) }) {
			continue
		}
		t.Errorf("%s: printed raw to the log; a name of the scanned repository can hold a newline, U+2028, U+202E or a byte that is not UTF-8. Wrap it in logWord (a path or a name) or logErr (an error), or add a reasoned exception to logGuardExceptions", v)
	}
}

// An exception that matches nothing is stale: the guard would pass with
// something to guard no more.
func TestLogGuardEveryExceptionStillMatches(t *testing.T) {
	vs, _, _, err := scanLogPackage(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[logGuardException]bool{}
	for _, e := range logGuardExceptions {
		key := logGuardException{File: e.File, Expr: e.Expr}
		if seen[key] {
			t.Errorf("exception %s %q is listed twice", e.File, e.Expr)
		}
		seen[key] = true
		if strings.TrimSpace(e.Why) == "" {
			t.Errorf("exception %s %q needs a reason", e.File, e.Expr)
		}
		if !slices.ContainsFunc(vs, e.covers) {
			t.Errorf("stale exception: %s %q matches no unsafe argument any more; remove it from logGuardExceptions", e.File, e.Expr)
		}
	}
}

// The error helper the story names. The tests of log_names_test.go reach it
// through the log, since a test that called it would not compile before it
// exists; this one reads the source for its declaration:
//
//	func logErr(err error) string
//
// It returns err.Error() unchanged when that text is valid UTF-8 and prints,
// the quoted text (strconv.Quote, as logWord) otherwise, and "<nil>" for a nil
// error. If the helper gets another name, change it here, in safeLogCalls, and
// in log_names_test.go's comment: those are the three places that name it.
func TestLogErrIsDeclared(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "logErr" {
				if got := types.ExprString(fn.Type); got != "func(err error) string" {
					t.Errorf("%s declares logErr as %s, want func(err error) string", name, got)
				}
				return
			}
		}
	}
	t.Error("internal/scan declares no func logErr(err error) string: the error text of a log call holds the paths the system was given, which logWord does not reach")
}

type logGuardCase struct {
	name string
	src  string
	want []string
}

func TestLogGuardDetectors(t *testing.T) {
	cases := []logGuardCase{
		{
			name: "a string printed raw",
			src: `package p
import "log"
func a(path string) { log.Printf("read %s", path) }
`,
			want: []string{"x.go:3: path"},
		},
		{
			name: "a field printed raw",
			src: `package p
import "log"
func a(s struct{ mount string }) { log.Printf("mount %s", s.mount) }
`,
			want: []string{"x.go:3: s.mount"},
		},
		{
			name: "an error printed with %v",
			src: `package p
import "log"
func a(err error) { log.Printf("failed: %v", err) }
`,
			want: []string{"x.go:3: err"},
		},
		{
			name: "logWord and logErr are safe",
			src: `package p
import "log"
func a(path string, err error) { log.Printf("read %s: %v", logWord(path), logErr(err)) }
`,
		},
		{
			name: "the Loggable of a command is safe, whatever it is called on",
			src: `package p
import "log"
func a(cmd c) { log.Printf("Running: %s", cmd.Loggable()); log.Printf("Running: %s", build(cmd).Loggable()) }
`,
		},
		{
			name: "%q quotes whatever it is given",
			src: `package p
import ("log"; "strings")
func a(out []byte) { log.Printf("worktree %q", strings.TrimSpace(string(out))) }
`,
		},
		{
			name: "an integer by %d, or len",
			src: `package p
import "log"
func a(r struct{ n int }, xs []string) { log.Printf("%d of %v", r.n, len(xs)) }
`,
		},
		{
			name: "literals",
			src: `package p
import "log"
func a() { log.Printf("%s %v", "x", 3); log.Println("a", "b"+"c", 1) }
`,
		},
		{
			name: "Println and Print with a variable",
			src: `package p
import "log"
func a(n string) {
	log.Println("read", n)
	log.Print(n)
}
`,
			want: []string{"x.go:4: n", "x.go:5: n"},
		},
		{
			name: "Fatal and Panic are logged too",
			src: `package p
import "log"
func a(n string) {
	log.Fatalf("%s", n)
	log.Panicln(n)
}
`,
			want: []string{"x.go:4: n", "x.go:5: n"},
		},
		{
			name: "a format that is not a literal",
			src: `package p
import "log"
func a(f string) { log.Printf(f) }
`,
			want: []string{"x.go:3: f"},
		},
		{
			name: "a call that is not a quoting helper",
			src: `package p
import ("log"; "strings")
func a(xs []string) { log.Printf("not rendered: %s", strings.Join(xs, ", ")) }
`,
			want: []string{"x.go:3: strings.Join(xs, \", \")"},
		},
		{
			name: "a quoted join is safe",
			src: `package p
import ("log"; "strings")
func a(xs []string) { log.Printf("not rendered: %s", logWord(strings.Join(xs, ", "))) }
`,
		},
		{
			name: "%% takes no argument",
			src: `package p
import "log"
func a(path string, n int) { log.Printf("100%% %d %s", n, path) }
`,
			want: []string{"x.go:3: path"},
		},
		{
			name: "flags and widths do not move the verbs",
			src: `package p
import "log"
func a(path string, n int) { log.Printf("%-8s|%05d", path, n) }
`,
			want: []string{"x.go:3: path"},
		},
		{
			name: "an argument past the verbs",
			src: `package p
import "log"
func a(path string) { log.Printf("done", path) }
`,
			want: []string{"x.go:3: path"},
		},
		{
			name: "an explicit index: the arguments stand on their own",
			src: `package p
import "log"
func a(n int, path string) { log.Printf("%[2]s %[1]d", n, path) }
`,
			want: []string{"x.go:3: n", "x.go:3: path"},
		},
		{
			name: "a call in a closure and at package level",
			src: `package p
import "log"
var hook = func(p string) { log.Printf("%s", p) }
func a() func(string) { return func(p string) { log.Print(p) } }
`,
			want: []string{"x.go:3: p", "x.go:4: p"},
		},
		{
			name: "a file that does not import log: a local named log is not the package",
			src: `package p
func a(log interface{ Printf(string, ...any) }, p string) { log.Printf("%s", p) }
`,
		},
		// The limits, written down so they are known and not discovered: each of
		// these logs a raw word and the guard does not see it.
		{
			name: "limit: an aliased import is not scanned",
			src: `package p
import l "log"
func a(p string) { l.Printf("%s", p) }
`,
		},
		{
			name: "limit: a logger from log.New is not scanned",
			src: `package p
import ("log"; "os")
func a(p string) { lg := log.New(os.Stderr, "", 0); lg.Printf("%s", p) }
`,
		},
		{
			name: "limit: a method value is not scanned",
			src: `package p
import "log"
func a(p string) { print := log.Printf; print("%s", p) }
`,
		},
		{
			name: "limit: a name that is not a quoting helper but is called logWord",
			src: `package p
import "log"
func logWord(s string) string { return s }
func a(p string) { log.Printf("%s", logWord(p)) }
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, _, err := scanLogSource("x.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("the synthetic source does not parse: %v", err)
			}
			got := make([]string, len(vs))
			for i, v := range vs {
				got[i] = v.String()
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("report =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

// Each file the story names is parsed and holds log calls the guard counts, so
// a guard gone blind (an empty report) cannot pass for a clean package.
func TestLogGuardSeesTheCallsOfTheStory(t *testing.T) {
	for _, file := range []string{"kubeconform_render.go", "kubeconform.go", "buildcontext.go", "seccomp_origin.go", "signature.go", "scanner.go"} {
		src, err := os.ReadFile(file) //nolint:gosec // a source file of this package
		if err != nil {
			t.Fatal(err)
		}
		_, calls, err := scanLogSource("internal/scan/"+file, src)
		if err != nil {
			t.Fatal(err)
		}
		if calls == 0 {
			t.Errorf("%s: the guard sees no log call in it", file)
		}
	}
}
