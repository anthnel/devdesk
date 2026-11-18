package netproxy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// DEVDESK-583: the guard that keeps every container launch of the scanners on
// containerRun.cmd (internal/scan/container_run.go), the one place that forwards
// the proxy variables into the container. Like the HTTP guard it parses the
// production sources of the whole module (go/ast, no type information) and
// reports the two ways code has found to build a `docker|podman run`:
//
//	C1  a []string literal whose first element is "run"
//	C2  a call with the literal "run" as argument 0 or 1 and at least one
//	    argument after it: exec.Command(bin, "run", img), dockerCombined("run", ...)
//
// A label such as cmdLabel("run") has no argument after "run" and is not
// reported. What the guard cannot see (a "run" assembled at run time, a
// "container run" spelling) is documented in docs/architecture/network.md.

const (
	ruleContainerSlice = "C1"
	ruleContainerCall  = "C2"

	// containerRunFile and containerRunFunc are the one site allowed to build a
	// run. It is hard-coded here and not an exception: it is the entry point.
	containerRunFile = "internal/scan/container_run.go"
	containerRunFunc = "containerRun.cmd"
)

var containerRules = []string{ruleContainerSlice, ruleContainerCall}

// containerExceptions is the whole list. Each entry is also named in the
// "Container scanners" section of docs/architecture/network.md
// (TestNetworkDocListsEveryContainerException).
var containerExceptions = []exception{
	{
		File: "internal/docker/launch.go", Func: "buildLaunchArgs", Rule: ruleContainerSlice,
		Why:   "the user's own container, configured in the launch form; not a scanner (outside DEVDESK-583)",
		Until: "permanent",
	},
	{
		File: "internal/docker/launch.go", Func: "VerifyEntrypoint", Rule: ruleContainerCall,
		Why:   "runs only `command -v`, nothing to forward",
		Until: "permanent",
	},
}

// isRunLiteral reports whether e is the string literal "run" (or `run`).
func isRunLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == "run"
}

// isStringSlice reports whether t is the type expression []string.
func isStringSlice(t ast.Expr) bool {
	at, ok := t.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	id, ok := at.Elt.(*ast.Ident)
	return ok && id.Name == "string"
}

// scanContainerSource parses src as the file name (a slash path from the module
// root) and returns its C1 and C2 violations ordered by line, then rule.
func scanContainerSource(name string, src []byte) ([]violation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []violation
	add := func(pos token.Pos, rule, fn string) {
		out = append(out, violation{File: name, Line: fset.Position(pos).Line, Rule: rule, Func: fn})
	}
	inspect := func(node ast.Node, fn string) {
		ast.Inspect(node, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if n.Type != nil && isStringSlice(n.Type) && len(n.Elts) > 0 && isRunLiteral(n.Elts[0]) {
					add(n.Pos(), ruleContainerSlice, fn)
				}
			case *ast.CallExpr:
				for i := 0; i < 2 && i+1 < len(n.Args); i++ {
					if isRunLiteral(n.Args[i]) {
						add(n.Pos(), ruleContainerCall, fn)
						break
					}
				}
			}
			return true
		})
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			inspect(d, funcName(d))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					inspect(sp, sp.Names[0].Name)
				case *ast.TypeSpec:
					inspect(sp, sp.Name.Name)
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b violation) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return out, nil
}

// scanContainerTree scans every non-test .go file under root, with the same
// exclusions as the HTTP guard (guardExcludedDirs, matched on the names below
// root) but internal/netproxy included: nothing there launches a container
// either. It also returns the number of files read.
func scanContainerTree(root string) ([]violation, int, error) {
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
			if rel != "." && slices.Contains(guardExcludedDirs, d.Name()) {
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
		vs, err := scanContainerSource(rel, src)
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
	containerOnce       sync.Once
	containerViolations []violation
	containerFiles      int
	containerErr        error
)

// scanContainerRepo scans the module that holds this package, once per test
// process, and returns its root.
func scanContainerRepo(t *testing.T) (string, []violation) {
	t.Helper()
	root, _ := scanRepo(t)
	containerOnce.Do(func() {
		containerViolations, containerFiles, containerErr = scanContainerTree(root)
	})
	if containerErr != nil {
		t.Fatalf("scanning the module for container launches: %v", containerErr)
	}
	// A walk that reads almost nothing would pass for the wrong reason.
	if containerFiles < 50 {
		t.Fatalf("the container guard read %d files under %s, want the whole module", containerFiles, root)
	}
	return root, containerViolations
}

// isContainerEntryPoint reports whether v is in the one function allowed to
// build a run.
func isContainerEntryPoint(v violation) bool {
	return v.File == containerRunFile && v.Func == containerRunFunc
}

// No scanner launches a container but through containerRun.cmd, which forwards
// the proxy variables; the listed exceptions are not scanners.
func TestContainerGuardNoLaunchBypassesTheProxyForward(t *testing.T) {
	_, vs := scanContainerRepo(t)
	for _, v := range vs {
		if isContainerEntryPoint(v) {
			continue
		}
		if slices.ContainsFunc(containerExceptions, func(e exception) bool { return e.covers(v) }) {
			continue
		}
		t.Errorf("%s: builds a container run by hand; build it with containerRun in %s so the proxy variables are forwarded (containerRun.cmd), or add a reasoned exception to containerExceptions and to docs/architecture/network.md", v, containerRunFile)
	}
}

// An exception that matches nothing is stale, and so is an entry point that no
// longer builds the run: the guard would pass with nothing left to guard.
func TestContainerGuardEveryExceptionStillMatches(t *testing.T) {
	_, vs := scanContainerRepo(t)
	if !slices.ContainsFunc(vs, isContainerEntryPoint) {
		t.Errorf("%s no longer builds a run in %s; the entry point moved, update containerRunFile/containerRunFunc and the doc", containerRunFunc, containerRunFile)
	}
	seen := map[exception]bool{}
	for _, e := range containerExceptions {
		key := exception{File: e.File, Func: e.Func, Rule: e.Rule}
		if seen[key] {
			t.Errorf("exception %s %s %s is listed twice", e.File, e.Func, e.Rule)
		}
		seen[key] = true
		if !slices.Contains(containerRules, e.Rule) {
			t.Errorf("exception %s %s: rule %q is not one of %v", e.File, e.Func, e.Rule, containerRules)
		}
		if strings.TrimSpace(e.Why) == "" || strings.TrimSpace(e.Until) == "" {
			t.Errorf("exception %s %s %s needs a reason and an end (\"permanent\" or a story)", e.File, e.Func, e.Rule)
		}
		if !slices.ContainsFunc(vs, e.covers) {
			t.Errorf("stale exception: %s %s %s matches no container launch any more; remove it from containerExceptions and docs/architecture/network.md", e.File, e.Func, e.Rule)
		}
	}
}

func TestContainerGuardDetectors(t *testing.T) {
	cases := []guardCase{
		{
			name: "a literal slice starting with run",
			src: `package p
func a() []string { return []string{"run", "--rm"} }
`,
			want: []string{"x.go:2: C1 (a)"},
		},
		{
			name: "a one-element slice with run",
			src: `package p
func a(x []string) []string { return append([]string{"run"}, x...) }
`,
			want: []string{"x.go:2: C1 (a)"},
		},
		{
			name: "a slice with run at package scope",
			src: `package p
var base = []string{"run", "-d"}
`,
			want: []string{"x.go:2: C1 (base)"},
		},
		{
			name: "a slice with run in a closure belongs to its declaration",
			src: `package p
type T struct{}
func (t *T) m() func() []string {
	return func() []string { return []string{"run", "x"} }
}
`,
			want: []string{"x.go:4: C1 (T.m)"},
		},
		{
			name: "run at argument 1 of a call",
			src: `package p
import "os/exec"
func a(b, i string) { _ = exec.Command(b, "run", i) }
`,
			want: []string{"x.go:3: C2 (a)"},
		},
		{
			name: "run at argument 0 of a call",
			src: `package p
func f(a ...string) {}
func a() { f("run", "--rm") }
`,
			want: []string{"x.go:3: C2 (a)"},
		},
		{
			name: "run followed by a spread",
			src: `package p
func f(a string, b ...string) {}
func a(x []string) { f("run", x...) }
`,
			want: []string{"x.go:3: C2 (a)"},
		},
		{
			name: "a label with run alone",
			src: `package p
func cmdLabel(s string) string { return s }
func a() string { return cmdLabel("run") }
`,
		},
		{
			name: "run last, with nothing after it",
			src: `package p
func f(a, b string) {}
func a() { f("x", "run") }
`,
		},
		{
			name: "run at argument 2 is not a launch",
			src: `package p
func f(a, b, c, d string) {}
func a() { f("x", "y", "run", "z") }
`,
		},
		{
			name: "run after another element in a slice",
			src: `package p
func a() []string { return []string{"--rm", "run"} }
`,
		},
		{
			name: "a slice of another type",
			src: `package p
func a() []any { return []any{"run", "x"} }
`,
		},
		{
			name: "a constant is not a launch",
			src: `package p
const s = "run"
var t = "run"
`,
		},
		{
			name: "a longer word is not run",
			src: `package p
func f(a, b, c string) {}
func a() { f("x", "runner", "y"); _ = []string{"running"} }
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, err := scanContainerSource("x.go", []byte(tc.src))
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
		for _, r := range containerRules {
			if !slices.ContainsFunc(cases, func(tc guardCase) bool {
				return slices.ContainsFunc(tc.want, func(w string) bool { return strings.Contains(w, ": "+r+" (") })
			}) {
				t.Errorf("no detector case triggers %s", r)
			}
		}
	})
}

// The walk: the excluded directory names apply below the root only (a root under
// .worktrees is still scanned), test files are skipped, and, unlike the HTTP
// guard, internal/netproxy is read.
func TestContainerGuardWalk(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".worktrees", "feat", "wt")
	bad := "package p\nfunc a() []string { return []string{\"run\"} }\n"
	files := map[string]string{
		"go.mod":                     "module example.com/m\n",
		"main.go":                    bad,
		"pkg/deep/ok.go":             bad,
		"pkg/clean.go":               "package p\n",
		"pkg/thing_test.go":          bad,
		"internal/netproxy/own.go":   bad,
		".worktrees/other/w.go":      bad,
		"pkg/.worktrees/nested/w.go": bad,
		".git/hooks/h.go":            bad,
		"vendor/v/v.go":              bad,
		"pkg/testdata/t.go":          bad,
		"docs-site/node/d.go":        bad,
		"web/node_modules/n/n.go":    bad,
		"README.md":                  "not go",
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
	vs, n, err := scanContainerTree(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.String())
	}
	want := []string{
		"internal/netproxy/own.go:2: C1 (a)",
		"main.go:2: C1 (a)",
		"pkg/deep/ok.go:2: C1 (a)",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("scanContainerTree reported\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	if n != 4 {
		t.Errorf("scanContainerTree read %d files, want 4 (main.go, pkg/clean.go, pkg/deep/ok.go, internal/netproxy/own.go)", n)
	}
}

// containerDocSection is the part of the document under the heading that names
// the container scanners, up to the next heading of the same or a higher level.
func containerDocSection(md string) (string, bool) {
	heading := regexp.MustCompile(`^(#+)\s`)
	var out []string
	level := 0
	for _, line := range strings.Split(md, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			switch {
			case level == 0 && strings.Contains(strings.ToLower(line), "container scanners"):
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

func TestContainerGuardDocSection(t *testing.T) {
	md := "# Network\n\n## Other\nnot this\n\n## Container scanners\nin `a.go`\n### Sub\nin sub\n## Next\nnot this\n"
	got, ok := containerDocSection(md)
	if !ok || !strings.Contains(got, "a.go") || !strings.Contains(got, "in sub") ||
		strings.Contains(got, "not this") {
		t.Errorf("containerDocSection = %q, %v", got, ok)
	}
	if _, ok := containerDocSection("# Network\n## Other\n"); ok {
		t.Error("a document without the heading reported a section")
	}
}

// The exceptions the guard allows are the exceptions the doc lists.
func TestNetworkDocListsEveryContainerException(t *testing.T) {
	root, _ := scanContainerRepo(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "network.md"))
	if err != nil {
		t.Fatal(err)
	}
	section, ok := containerDocSection(string(raw))
	if !ok {
		t.Fatal("docs/architecture/network.md has no section whose heading names the container scanners")
	}
	for _, e := range containerExceptions {
		if !strings.Contains(section, e.File) {
			t.Errorf("the container scanners section does not name the file %s (exception %s %s)", e.File, e.Func, e.Rule)
		}
		if !strings.Contains(section, e.Func) {
			t.Errorf("the container scanners section does not name %s (exception in %s, %s)", e.Func, e.File, e.Rule)
		}
	}
}
