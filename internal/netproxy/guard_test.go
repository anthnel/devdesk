package netproxy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// DEVDESK-581: the guard that keeps every connection DevDesk opens on the
// proxy entry point. It parses the production sources of the module (go/ast, no
// type information) and reports the four ways code has found to leave the path
// that net/http and this package share:
//
//	R1  an http.Transport built by hand (a literal, &literal, new(...) or a var)
//	R2  a reference to http.DefaultTransport (cloning it keeps its Proxy field
//	    but invites the next line to replace it)
//	R3  an assignment to a Proxy field, or a Proxy: key in a composite literal,
//	    in a file that imports net/http
//	R4  a raw TLS dial: tls.Dial, tls.DialWithDialer, tls.Client, tls.Dialer{}
//
// A new connection goes through Dialer.DialTLS, NewTransport or a client with no
// Transport (which uses http.DefaultTransport and so the proxy environment).
// What the guard cannot see is documented in docs/architecture/network.md.

const (
	guardHTTPPath = "net/http"
	guardTLSPath  = "crypto/tls"

	// ruleDotImport is not one of R1 to R4 and cannot be excepted: a dot import
	// of net/http or crypto/tls hides every selector the rules look for.
	ruleDotImport = "dot-import"
)

// violation is one place where the source leaves the proxy entry point.
type violation struct {
	File string // slash path from the module root
	Line int
	Rule string // "R1".."R4" or ruleDotImport
	Func string // enclosing func ("Type.Method" or "Func") or package-level name
}

// String is the report line: file:line: rule (enclosing function).
func (v violation) String() string {
	fn := v.Func
	if fn == "" {
		fn = "file scope"
	}
	return fmt.Sprintf("%s:%d: %s (%s)", v.File, v.Line, v.Rule, fn)
}

// exception is a known violation, matched by file, enclosing function and rule,
// never by line number, so an edit above it does not break the list.
type exception struct {
	File  string // slash path from the module root
	Func  string // enclosing func ("Type.Method" or "Func") or package-level var
	Rule  string // "R1".."R4"
	Why   string
	Until string // "permanent" or the story that removes it
}

// proxyExceptions is the whole list. Each entry is also named in the proxy entry
// point section of docs/architecture/network.md (TestNetworkDocListsEveryProxyException).
var proxyExceptions = []exception{
	{
		File: "internal/nexusbench/nexusapi.go", Func: "newClient", Rule: "R2",
		Why:   "the bench is reached directly: a proxy would receive its credentials",
		Until: "permanent",
	},
	{
		File: "internal/nexusbench/nexusapi.go", Func: "newClient", Rule: "R3",
		Why:   "the bench is reached directly: a proxy would receive its credentials",
		Until: "permanent",
	},
	{
		File: "internal/nexusbench/probe.go", Func: "NewProber", Rule: "R2",
		Why:   "the bench probe goes without a proxy, like the bench client",
		Until: "permanent",
	},
	{
		File: "internal/nexusbench/probe.go", Func: "NewProber", Rule: "R3",
		Why:   "the bench probe goes without a proxy, like the bench client",
		Until: "permanent",
	},
	{
		File: "internal/netcheck/env.go", Func: "systemEnv.Head", Rule: "R1",
		Why:   "the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the Proxy row says when a proxy applies",
		Until: "permanent",
	},
	{
		File: "internal/netcheck/env.go", Func: "systemEnv.Handshake", Rule: "R4",
		Why:   "the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the TLS step runs on the same direct path as the TCP step",
		Until: "permanent",
	},
	{
		File: "internal/forward/proxy.go", Func: "routeTransport", Rule: "R1",
		Why:   "named routes follow the path of the TCP forwards to the same target, which is direct (decision Q1)",
		Until: "permanent",
	},
}

var guardRules = []string{"R1", "R2", "R3", "R4"}

// guardExcludedDirs are skipped at any depth below the module root.
var guardExcludedDirs = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

// guardOwnDir is the one directory allowed to clone http.DefaultTransport and to
// call tls.Client: this package.
const guardOwnDir = "internal/netproxy"

// scanner holds what one file's imports resolve to.
type scanner struct {
	fset      *token.FileSet
	file      string
	names     map[string]string // local package name -> import path, for the two watched packages
	importsHT bool              // the file imports net/http, under any name
	out       []violation
}

// scanSource parses src as the file name (a slash path from the module root) and
// returns its violations ordered by line, then rule.
func scanSource(name string, src []byte) ([]violation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	s := &scanner{fset: fset, file: name, names: map[string]string{}}
	s.resolveImports(f)

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			s.inspect(d, funcName(d))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					s.inspect(sp, sp.Names[0].Name)
				case *ast.TypeSpec:
					s.inspect(sp, sp.Name.Name)
				}
			}
		}
	}
	slices.SortStableFunc(s.out, func(a, b violation) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return s.out, nil
}

// resolveImports records the local name of net/http and crypto/tls, alias
// included, and reports a dot import of either.
func (s *scanner) resolveImports(f *ast.File) {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || (p != guardHTTPPath && p != guardTLSPath) {
			continue
		}
		if p == guardHTTPPath {
			s.importsHT = true
		}
		local := path.Base(p)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		switch local {
		case "_":
			continue
		case ".":
			s.add(imp.Pos(), ruleDotImport, "import")
			continue
		}
		s.names[local] = p
	}
}

func (s *scanner) add(pos token.Pos, rule, fn string) {
	s.out = append(s.out, violation{File: s.file, Line: s.fset.Position(pos).Line, Rule: rule, Func: fn})
}

// isSel reports whether e is pkg.Name for the imported package at pkgPath, under
// whatever local name the file gave it; with no names, any selector of it.
func (s *scanner) isSel(e ast.Expr, pkgPath string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || s.names[id.Name] != pkgPath {
		return false
	}
	return len(names) == 0 || slices.Contains(names, sel.Sel.Name)
}

// valueOfType reports a type expression that builds a transport or a dialer by
// value: http.Transport (R1) or tls.Dialer (R4).
func (s *scanner) valueOfType(typ ast.Expr, pos token.Pos, fn string) {
	switch {
	case s.isSel(typ, guardHTTPPath, "Transport"):
		s.add(pos, "R1", fn)
	case s.isSel(typ, guardTLSPath, "Dialer"):
		s.add(pos, "R4", fn)
	}
}

// inspect walks one top-level declaration; closures belong to the declaration
// that holds them.
func (s *scanner) inspect(node ast.Node, fn string) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if n.Type != nil {
				s.valueOfType(n.Type, n.Pos(), fn)
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "new" && len(n.Args) == 1 {
				s.valueOfType(n.Args[0], n.Pos(), fn)
			}
		case *ast.ValueSpec:
			if n.Type != nil {
				s.valueOfType(n.Type, n.Pos(), fn)
			}
		case *ast.SelectorExpr:
			if s.isSel(n, guardHTTPPath, "DefaultTransport") {
				s.add(n.Pos(), "R2", fn)
			}
			if s.isSel(n, guardTLSPath, "Dial", "DialWithDialer", "Client") {
				s.add(n.Pos(), "R4", fn)
			}
		case *ast.AssignStmt:
			if s.importsHT && n.Tok == token.ASSIGN {
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Proxy" {
						s.add(n.Pos(), "R3", fn)
					}
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok && s.importsHT && id.Name == "Proxy" {
				s.add(n.Pos(), "R3", fn)
			}
		}
		return true
	})
}

// funcName is "Func" or "Type.Method", without the pointer or the type
// parameters of the receiver.
func funcName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	return receiverName(d.Recv.List[0].Type) + "." + d.Name.Name
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.ParenExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// findModuleRoot goes up from dir to the first directory that holds a go.mod.
// Inside a worktree that is the worktree's own go.mod, not the main checkout's.
func findModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// scanTree scans every non-test .go file under root. The excluded directories
// are matched on the names of the entries below root, never on root's own
// absolute path: a checkout that lives under .worktrees is still scanned.
// It also returns the number of files read.
func scanTree(root string) ([]violation, int, error) {
	var all []violation
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (slices.Contains(guardExcludedDirs, d.Name()) || rel == guardOwnDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		vs, err := scanSource(rel, src)
		if err != nil {
			return err
		}
		files++
		all = append(all, vs...)
		return nil
	})
	return all, files, err
}

var (
	repoOnce       sync.Once
	repoRoot       string
	repoViolations []violation
	repoFiles      int
	repoErr        error
)

// scanRepo scans the module that holds this package, once per test process.
func scanRepo(t *testing.T) (string, []violation) {
	t.Helper()
	repoOnce.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			repoErr = err
			return
		}
		if repoRoot, repoErr = findModuleRoot(wd); repoErr != nil {
			return
		}
		repoViolations, repoFiles, repoErr = scanTree(repoRoot)
	})
	if repoErr != nil {
		t.Fatalf("scanning the module: %v", repoErr)
	}
	// A walk that reads almost nothing would pass for the wrong reason.
	if repoFiles < 50 {
		t.Fatalf("the guard read %d files under %s, want the whole module", repoFiles, repoRoot)
	}
	return repoRoot, repoViolations
}

func (e exception) covers(v violation) bool {
	return e.File == v.File && e.Func == v.Func && e.Rule == v.Rule
}

// Criterion 5: no production code builds its own path to the network outside
// the proxy entry point, but for the listed exceptions.
func TestNoHTTPClientBypassesTheProxyEntryPoint(t *testing.T) {
	_, vs := scanRepo(t)
	for _, v := range vs {
		if !slices.ContainsFunc(proxyExceptions, func(e exception) bool { return e.covers(v) }) {
			t.Errorf("%s: bypasses internal/netproxy; use netproxy.NewTransport, Dialer.DialTLS or a client without a Transport, or add a reasoned exception to proxyExceptions and to docs/architecture/network.md", v)
		}
	}
}

// An exception that matches nothing is stale: the code moved or was fixed, and
// the list (and the doc) must follow.
func TestEveryProxyExceptionStillMatches(t *testing.T) {
	_, vs := scanRepo(t)
	seen := map[exception]bool{}
	for _, e := range proxyExceptions {
		key := exception{File: e.File, Func: e.Func, Rule: e.Rule}
		if seen[key] {
			t.Errorf("exception %s %s %s is listed twice", e.File, e.Func, e.Rule)
		}
		seen[key] = true
		if !slices.Contains(guardRules, e.Rule) {
			t.Errorf("exception %s %s: rule %q is not one of %v", e.File, e.Func, e.Rule, guardRules)
		}
		if strings.TrimSpace(e.Why) == "" || strings.TrimSpace(e.Until) == "" {
			t.Errorf("exception %s %s %s needs a reason and an end (\"permanent\" or a story)", e.File, e.Func, e.Rule)
		}
		if !slices.ContainsFunc(vs, e.covers) {
			t.Errorf("stale exception: %s %s %s matches no violation any more; remove it from proxyExceptions and docs/architecture/network.md", e.File, e.Func, e.Rule)
		}
	}
}

// guardCase is one synthetic source and the exact report the guard must give.
type guardCase struct {
	name string
	src  string
	want []string
}

func TestGuardDetectors(t *testing.T) {
	cases := []guardCase{
		{
			name: "R1 literal, address of a literal, new and var",
			src: `package p
import "net/http"
func a() { _ = http.Transport{} }
func b() { _ = &http.Transport{MaxIdleConns: 1} }
func c() { _ = new(http.Transport) }
var d http.Transport
`,
			want: []string{
				"x.go:3: R1 (a)",
				"x.go:4: R1 (b)",
				"x.go:5: R1 (c)",
				"x.go:6: R1 (d)",
			},
		},
		{
			name: "R2 a reference to DefaultTransport, in a function and in a package var",
			src: `package p
import "net/http"
func a() *http.Transport { return http.DefaultTransport.(*http.Transport).Clone() }
var shared = http.DefaultTransport
`,
			want: []string{
				"x.go:3: R2 (a)",
				"x.go:4: R2 (shared)",
			},
		},
		{
			name: "R3 an assignment and a key",
			src: `package p
import "net/http"
func a(t *http.Client, f func()) {
	t.Proxy = nil
}
func b(f func()) { _ = struct{ Proxy func() }{Proxy: f} }
`,
			want: []string{
				"x.go:4: R3 (a)",
				"x.go:6: R3 (b)",
			},
		},
		{
			name: "R1 and R3 on the same literal",
			src: `package p
import "net/http"
func a(f func(*http.Request)) { _ = &http.Transport{Proxy: nil} }
`,
			want: []string{
				"x.go:3: R1 (a)",
				"x.go:3: R3 (a)",
			},
		},
		{
			name: "R4 raw TLS dials",
			src: `package p
import "crypto/tls"
func a() { _, _ = tls.Dial("tcp", "h:443", nil) }
func b() { _, _ = tls.DialWithDialer(nil, "tcp", "h:443", nil) }
func c() { _ = tls.Client(nil, nil) }
func d() { _ = tls.Dialer{} }
func e() { _ = &tls.Dialer{Config: nil} }
func f() { _ = new(tls.Dialer) }
func g() { dial := tls.Dial; _ = dial }
`,
			want: []string{
				"x.go:3: R4 (a)",
				"x.go:4: R4 (b)",
				"x.go:5: R4 (c)",
				"x.go:6: R4 (d)",
				"x.go:7: R4 (e)",
				"x.go:8: R4 (f)",
				"x.go:9: R4 (g)",
			},
		},
		{
			name: "aliased imports are resolved",
			src: `package p
import (
	nethttp "net/http"
	ctls "crypto/tls"
)
func a() { _ = &nethttp.Transport{} }
func b() { _ = nethttp.DefaultTransport }
func c(t *nethttp.Transport) { t.Proxy = nil }
func d() { _, _ = ctls.Dial("tcp", "h:443", nil) }
func e() { _ = ctls.Dialer{} }
`,
			want: []string{
				"x.go:6: R1 (a)",
				"x.go:7: R2 (b)",
				"x.go:8: R3 (c)",
				"x.go:9: R4 (d)",
				"x.go:10: R4 (e)",
			},
		},
		{
			name: "a dot import is itself a violation",
			src: `package p
import (
	. "net/http"
	. "crypto/tls"
)
func a() { _ = Transport{} }
`,
			want: []string{
				"x.go:3: dot-import (import)",
				"x.go:4: dot-import (import)",
			},
		},
		{
			name: "enclosing names: method, generic method, closure",
			src: `package p
import "net/http"
type Client struct{}
type Set[T any] struct{}
func (c *Client) Do() { _ = &http.Transport{} }
func (s *Set[T]) Add() { _ = &http.Transport{} }
func run() {
	go func() { _ = &http.Transport{} }()
}
`,
			want: []string{
				"x.go:5: R1 (Client.Do)",
				"x.go:6: R1 (Set.Add)",
				"x.go:8: R1 (run)",
			},
		},
		{
			name: "a client without a Transport is the compliant way",
			src: `package p
import (
	"crypto/tls"
	"net/http"
	"time"
)
var shared = &http.Client{Timeout: 5 * time.Second}
func a() (*http.Client, *tls.Config) {
	return &http.Client{Timeout: time.Second, CheckRedirect: nil}, &tls.Config{MinVersion: tls.VersionTLS12}
}
func b(t *http.Transport) *http.Transport { return t }
func c(conn *tls.Conn) { _ = conn }
`,
		},
		{
			name: "another field whose name only contains Proxy",
			src: `package p
import "net/http"
type cfg struct{ unblockProxy bool; ProxyURL string }
func a(c *cfg) *http.Client {
	c.unblockProxy = true
	c.ProxyURL = "x"
	_ = cfg{unblockProxy: true, ProxyURL: "y"}
	return &http.Client{}
}
`,
		},
		{
			name: "a Proxy field in a file that does not import net/http",
			src: `package p
type cfg struct{ Proxy string }
func a(c *cfg) { c.Proxy = "x"; _ = cfg{Proxy: "y"} }
`,
		},
		{
			name: "another package named http or tls is not net/http",
			src: `package p
import (
	"example.com/other/http"
	"example.com/other/tls"
)
func a() {
	_ = http.Transport{}
	_ = http.DefaultTransport
	_, _ = tls.Dial("tcp", "h", nil)
}
`,
		},
		{
			name: "a blank import is not a reference",
			src: `package p
import _ "net/http"
func a() {}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, err := scanSource("x.go", []byte(tc.src))
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

	// Every rule has at least one case that triggers it, so a rule cannot be
	// dropped from the detector while the table stays green.
	t.Run("every rule is exercised", func(t *testing.T) {
		seen := map[string]bool{}
		for _, tc := range cases {
			for _, w := range tc.want {
				for _, r := range append(slices.Clone(guardRules), ruleDotImport) {
					if strings.Contains(w, ": "+r+" (") {
						seen[r] = true
					}
				}
			}
		}
		for _, r := range append(slices.Clone(guardRules), ruleDotImport) {
			if !seen[r] {
				t.Errorf("no detector case triggers %s", r)
			}
		}
	})
}

// The walk: the module root is found by go.mod, the excluded directory names
// apply below the root only (a root under .worktrees is still scanned), and
// internal/netproxy and the test files are never read.
func TestGuardWalkRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".worktrees", "feat", "wt")
	bad := "package p\nimport \"net/http\"\nfunc a() { _ = &http.Transport{} }\n"
	files := map[string]string{
		"go.mod":                       "module example.com/m\n",
		"main.go":                      bad,
		"pkg/deep/ok.go":               bad,
		"pkg/clean.go":                 "package p\n",
		"pkg/thing_test.go":            bad,
		"internal/netproxy/own.go":     bad,
		"internal/other/netproxy/x.go": bad,
		".worktrees/other/w.go":        bad,
		"pkg/.worktrees/nested/w.go":   bad,
		".git/hooks/h.go":              bad,
		"vendor/v/v.go":                bad,
		"pkg/testdata/t.go":            bad,
		"docs-site/node/d.go":          bad,
		"web/node_modules/n/n.go":      bad,
		"README.md":                    "not go",
	}
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	found, err := findModuleRoot(filepath.Join(root, "pkg", "deep"))
	if err != nil || found != root {
		t.Fatalf("findModuleRoot = %q, %v; want %q", found, err, root)
	}

	vs, n, err := scanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.String())
	}
	want := []string{
		"internal/other/netproxy/x.go:3: R1 (a)",
		"main.go:3: R1 (a)",
		"pkg/deep/ok.go:3: R1 (a)",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("scanTree reported\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	if n != 4 {
		t.Errorf("scanTree read %d files, want 4 (main.go, pkg/clean.go, pkg/deep/ok.go, internal/other/netproxy/x.go)", n)
	}
}

// proxyDocSection is the part of the document under the heading that names the
// proxy entry point, up to the next heading of the same or a higher level.
func proxyDocSection(md string) (string, bool) {
	heading := regexp.MustCompile(`^(#+)\s`)
	var out []string
	level := 0
	for _, line := range strings.Split(md, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			switch {
			case level == 0 && strings.Contains(strings.ToLower(line), "proxy entry point"):
				level = len(m[1])
			case level != 0 && len(m[1]) <= level:
				return strings.Join(out, "\n"), true
			}
		}
		if level != 0 {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), level != 0
}

func TestGuardDocSection(t *testing.T) {
	md := "# Network\n\n## Other\nnot this\n\n## The proxy entry point\nin `a.go`\n### Sub\nin sub\n## Next\nnot this\n"
	got, ok := proxyDocSection(md)
	if !ok || !strings.Contains(got, "a.go") || !strings.Contains(got, "in sub") ||
		strings.Contains(got, "not this") {
		t.Errorf("proxyDocSection = %q, %v", got, ok)
	}
	if _, ok := proxyDocSection("# Network\n## Other\n"); ok {
		t.Error("a document without the heading reported a section")
	}
}

// Criterion 6: the exceptions the guard allows are the exceptions the doc lists.
func TestNetworkDocListsEveryProxyException(t *testing.T) {
	root, _ := scanRepo(t)
	doc := filepath.Join(root, "docs", "architecture", "network.md")
	raw, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	section, ok := proxyDocSection(string(raw))
	if !ok {
		t.Fatal("docs/architecture/network.md has no section whose heading names the proxy entry point")
	}
	for _, e := range proxyExceptions {
		if !strings.Contains(section, e.File) {
			t.Errorf("the proxy entry point section does not name the file %s (exception %s %s)", e.File, e.Func, e.Rule)
		}
		if !strings.Contains(section, e.Func) {
			t.Errorf("the proxy entry point section does not name %s (exception in %s, %s)", e.Func, e.File, e.Rule)
		}
	}
}
