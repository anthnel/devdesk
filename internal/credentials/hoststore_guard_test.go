package credentials

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// DEVDESK-610: the static guard that keeps the host secret store behind the
// HostStore seam (hoststore.go). The runtime guard (refuseUnderTest) only sees
// the code paths a test executes; this one reads every .go file of the module
// (go/parser, no type information) and reports three ways the isolation can be
// bypassed or leak into production:
//
//	K1  an import of github.com/zalando/go-keyring outside internal/credentials/hoststore.go
//	    and the _test.go files of internal/credentials. A test elsewhere that talks to
//	    go-keyring (mock or not) bypasses the seam and the fake.
//	K2  a reference to credentials.SwapHostStoreForTest in a non-test file, except
//	    internal/testutil/fakekeyring and internal/credentials itself. The seam is for
//	    tests; production code must never replace the host store.
//	K3  an import of internal/testutil/fakekeyring from a non-test file (it imports
//	    "testing" and must not reach a production binary). No exception.
//
// What the guard cannot see (a shadowed package name, an import path built at
// run time, a test that shells out to a binary that reaches the keyring) is
// listed in docs/architecture/configuration.md.

const (
	ruleKeyringImport = "K1"
	ruleSwapInProd    = "K2"
	ruleFakeInProd    = "K3"

	goKeyringPath    = "github.com/zalando/go-keyring"
	credentialsPath  = "github.com/anthnel/devdesk/internal/credentials"
	fakeKeyringPath  = "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
	swapSeamName     = "SwapHostStoreForTest"
	credentialsDir   = "internal/credentials"
	hostStoreFile    = "internal/credentials/hoststore.go"
	fakeKeyringDir   = "internal/testutil/fakekeyring"
	hostGuardMinimum = 50
)

var hostStoreRules = []string{ruleKeyringImport, ruleSwapInProd, ruleFakeInProd}

// hostStoreExcludedDirs are skipped at any depth below the module root, like the
// other source guards.
var hostStoreExcludedDirs = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

// hostViolation is one place where the host secret store is reached or the seam
// is used outside what the rules allow.
type hostViolation struct {
	File string // slash path from the module root
	Line int
	Rule string // "K1".."K3"
}

// String is the report line: file:line: rule, then what to do.
func (v hostViolation) String() string {
	return fmt.Sprintf("%s:%d: %s %s", v.File, v.Line, v.Rule, hostStoreAdvice[v.Rule])
}

// hostStoreAdvice names the rule broken and the way out, so a failure needs no
// trip to the documentation.
var hostStoreAdvice = map[string]string{
	ruleKeyringImport: "imports github.com/zalando/go-keyring outside internal/credentials; " +
		"isolate with fakekeyring.Install() or fakekeyring.Fresh(t) and go through credentials (DEVDESK-610)",
	ruleSwapInProd: "calls credentials.SwapHostStoreForTest from a non-test file; " +
		"the seam is for _test.go files and internal/testutil/fakekeyring only (DEVDESK-610)",
	ruleFakeInProd: "imports internal/testutil/fakekeyring from a non-test file; " +
		"it imports \"testing\" and belongs to _test.go files only (DEVDESK-610)",
}

// importLocalName is the identifier an import binds in the file: its alias, or
// the last element of its path. "_" and "." are returned as is.
func importLocalName(spec *ast.ImportSpec, importPath string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	return path.Base(importPath)
}

// scanHostStoreSource parses src as the file name (a slash path from the module
// root) and returns its violations ordered by line, then rule.
func scanHostStoreSource(name string, src []byte) ([]hostViolation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	isTest := strings.HasSuffix(name, "_test.go")
	dir := path.Dir(name)

	var out []hostViolation
	add := func(pos token.Pos, rule string) {
		out = append(out, hostViolation{File: name, Line: fset.Position(pos).Line, Rule: rule})
	}

	credName := ""   // local name of internal/credentials ("" = not imported)
	credDot := false // internal/credentials is dot-imported
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		switch p {
		case goKeyringPath:
			if name != hostStoreFile && (!isTest || dir != credentialsDir) {
				add(spec.Pos(), ruleKeyringImport)
			}
		case fakeKeyringPath:
			if !isTest {
				add(spec.Pos(), ruleFakeInProd)
			}
		case credentialsPath:
			credName = importLocalName(spec, p)
			credDot = credName == "."
		}
	}

	swapAllowed := isTest || dir == fakeKeyringDir || dir == credentialsDir
	if credName != "" && !swapAllowed {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := n.X.(*ast.Ident); ok && !credDot && id.Name == credName && n.Sel.Name == swapSeamName {
					add(n.Sel.Pos(), ruleSwapInProd)
				}
			case *ast.Ident:
				if credDot && n.Name == swapSeamName {
					add(n.Pos(), ruleSwapInProd)
				}
			}
			return true
		})
	}

	slices.SortStableFunc(out, func(a, b hostViolation) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return out, nil
}

// scanHostStoreTree scans every .go file under root (test files included: K1
// applies to them), skipping hostStoreExcludedDirs matched on the names below
// root. It also returns the number of files read.
func scanHostStoreTree(root string) ([]hostViolation, int, error) {
	var all []hostViolation
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
			if rel != "." && slices.Contains(hostStoreExcludedDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		vs, err := scanHostStoreSource(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, vs...)
		return nil
	})
	return all, files, err
}

// hostStoreModuleRoot walks up from dir to the directory holding go.mod.
func hostStoreModuleRoot(dir string) (string, error) {
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

var (
	hostGuardOnce       sync.Once
	hostGuardRoot       string
	hostGuardViolations []hostViolation
	hostGuardFiles      int
	hostGuardErr        error
)

// scanHostStoreRepo scans the module that holds this package, once per test
// process.
func scanHostStoreRepo(t *testing.T) (string, []hostViolation) {
	t.Helper()
	hostGuardOnce.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			hostGuardErr = err
			return
		}
		if hostGuardRoot, hostGuardErr = hostStoreModuleRoot(wd); hostGuardErr != nil {
			return
		}
		hostGuardViolations, hostGuardFiles, hostGuardErr = scanHostStoreTree(hostGuardRoot)
	})
	if hostGuardErr != nil {
		t.Fatalf("scanning the module for host store use: %v", hostGuardErr)
	}
	// A walk that reads almost nothing would pass for the wrong reason.
	if hostGuardFiles < hostGuardMinimum {
		t.Fatalf("the host store guard read %d files under %s, want the whole module", hostGuardFiles, hostGuardRoot)
	}
	return hostGuardRoot, hostGuardViolations
}

// The real module follows K1 to K3: no test outside internal/credentials talks
// to go-keyring, nothing in production replaces the store or imports the fake.
func TestHostStoreGuardWalk(t *testing.T) {
	_, vs := scanHostStoreRepo(t)
	for _, v := range vs {
		t.Error(v.String())
	}
}

// hostGuardCase is one synthetic source and the violations expected, each as
// "line rule".
type hostGuardCase struct {
	name string
	file string
	src  string
	want []string
}

// Each rule is exercised on synthetic sources: a violation is reported with its
// rule, and the allowed uses are not. A rule dropped from the detector would
// fail here while the real-module test stays green.
func TestHostStoreGuardDetectors(t *testing.T) {
	cases := []hostGuardCase{
		// K1
		{
			name: "K1: a test outside credentials imports go-keyring",
			file: "internal/app/x_test.go",
			src: `package app
import "github.com/zalando/go-keyring"
var _ = keyring.MockInit
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: a production file outside credentials imports go-keyring",
			file: "internal/other/x.go",
			src: `package other
import "github.com/zalando/go-keyring"
var _ = keyring.Get
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: an aliased import",
			file: "internal/app/x_test.go",
			src: `package app
import kr "github.com/zalando/go-keyring"
var _ = kr.MockInit
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: a blank import",
			file: "cmd/tool/main.go",
			src: `package main
import _ "github.com/zalando/go-keyring"
func main() {}
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: a dot import",
			file: "internal/ui/setup/x_test.go",
			src: `package setup
import . "github.com/zalando/go-keyring"
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: a grouped import is found at its own line",
			file: "internal/app/x_test.go",
			src: `package app
import (
	"testing"

	"github.com/zalando/go-keyring"
)
var _ testing.TB
var _ = keyring.MockInit
`,
			want: []string{"5 K1"},
		},
		{
			name: "K1: a subpackage of credentials is not credentials",
			file: "internal/credentials/sub/x_test.go",
			src: `package sub
import "github.com/zalando/go-keyring"
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1: a non-test file of credentials other than hoststore.go",
			file: "internal/credentials/keyring.go",
			src: `package credentials
import "github.com/zalando/go-keyring"
`,
			want: []string{"2 K1"},
		},
		{
			name: "K1 allowed: hoststore.go",
			file: "internal/credentials/hoststore.go",
			src: `package credentials
import "github.com/zalando/go-keyring"
var _ = keyring.Get
`,
		},
		{
			name: "K1 allowed: a _test.go of credentials",
			file: "internal/credentials/keyring_test.go",
			src: `package credentials
import "github.com/zalando/go-keyring"
var _ = keyring.MockInit
`,
		},
		{
			name: "K1 allowed: another keyring package is not go-keyring",
			file: "internal/app/x_test.go",
			src: `package app
import "github.com/example/go-keyring-extra"
`,
		},
		// K2
		{
			name: "K2: a production file calls the seam",
			file: "internal/app/router.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/credentials"
func f() { credentials.SwapHostStoreForTest(nil) }
`,
			want: []string{"3 K2"},
		},
		{
			name: "K2: the seam under an alias",
			file: "internal/app/router.go",
			src: `package app
import cred "github.com/anthnel/devdesk/internal/credentials"
var swap = cred.SwapHostStoreForTest
`,
			want: []string{"3 K2"},
		},
		{
			name: "K2: the seam through a dot import",
			file: "internal/app/router.go",
			src: `package app
import . "github.com/anthnel/devdesk/internal/credentials"
func f() { SwapHostStoreForTest(nil) }
`,
			want: []string{"3 K2"},
		},
		{
			name: "K2: the seam in main",
			file: "main.go",
			src: `package main
import "github.com/anthnel/devdesk/internal/credentials"
func main() { credentials.SwapHostStoreForTest(nil) }
`,
			want: []string{"3 K2"},
		},
		{
			name: "K2 allowed: a _test.go calls the seam",
			file: "internal/app/harness_test.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/credentials"
func f() { credentials.SwapHostStoreForTest(nil) }
`,
		},
		{
			name: "K2 allowed: fakekeyring itself",
			file: "internal/testutil/fakekeyring/fakekeyring.go",
			src: `package fakekeyring
import "github.com/anthnel/devdesk/internal/credentials"
func f() { credentials.SwapHostStoreForTest(nil) }
`,
		},
		{
			name: "K2 allowed: another function of credentials",
			file: "internal/app/router.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/credentials"
func f() { credentials.Select("x", "auto") }
`,
		},
		{
			name: "K2 allowed: the same name on an unrelated package",
			file: "internal/app/router.go",
			src: `package app
import "example.com/other"
func f() { other.SwapHostStoreForTest(nil) }
`,
		},
		{
			name: "K2 allowed: the name without importing credentials",
			file: "internal/app/router.go",
			src: `package app
func SwapHostStoreForTest() {}
func f() { SwapHostStoreForTest() }
`,
		},
		// K3
		{
			name: "K3: a production file imports fakekeyring",
			file: "internal/app/router.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
var _ = fakekeyring.Install
`,
			want: []string{"2 K3"},
		},
		{
			name: "K3: an aliased import in cmd",
			file: "cmd/tool/main.go",
			src: `package main
import fk "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
func main() { fk.Install() }
`,
			want: []string{"2 K3"},
		},
		{
			name: "K3: a blank import",
			file: "main.go",
			src: `package main
import _ "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
func main() {}
`,
			want: []string{"2 K3"},
		},
		{
			name: "K3: another testutil package from production is not K3",
			file: "internal/app/router.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/testutil/loopback"
`,
		},
		{
			name: "K3 allowed: a _test.go imports fakekeyring",
			file: "internal/app/harness_test.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
var _ = fakekeyring.Install
`,
		},
		{
			name: "K3 allowed: an external test package of credentials",
			file: "internal/credentials/x_test.go",
			src: `package credentials_test
import "github.com/anthnel/devdesk/internal/testutil/fakekeyring"
var _ = fakekeyring.Install
`,
		},
		// Several rules at once, ordered by line.
		{
			name: "K1 and K2 and K3 in one production file",
			file: "internal/app/router.go",
			src: `package app
import (
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/testutil/fakekeyring"
	"github.com/zalando/go-keyring"
)
func f() { credentials.SwapHostStoreForTest(nil); _ = fakekeyring.Install; _ = keyring.Get }
`,
			want: []string{"4 K3", "5 K1", "7 K2"},
		},
		{
			name: "clean file",
			file: "internal/app/router.go",
			src: `package app
import "github.com/anthnel/devdesk/internal/credentials"
func f() { credentials.Select("x", "auto") }
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, err := scanHostStoreSource(tc.file, []byte(tc.src))
			if err != nil {
				t.Fatalf("the synthetic source does not parse: %v", err)
			}
			got := make([]string, len(vs))
			for i, v := range vs {
				got[i] = fmt.Sprintf("%d %s", v.Line, v.Rule)
				if v.File != tc.file {
					t.Errorf("violation names %q, want %q", v.File, tc.file)
				}
				if msg := v.String(); !strings.Contains(msg, tc.file+":") ||
					!strings.Contains(msg, v.Rule) || !strings.Contains(msg, "DEVDESK-610") {
					t.Errorf("report %q must name the file, the rule and DEVDESK-610", msg)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("report = %v, want %v", got, tc.want)
			}
		})
	}

	// Every rule has at least one case that triggers it, so a rule cannot be
	// dropped from the detector while the table stays green.
	t.Run("every rule is exercised", func(t *testing.T) {
		for _, r := range hostStoreRules {
			if !slices.ContainsFunc(cases, func(tc hostGuardCase) bool {
				return slices.ContainsFunc(tc.want, func(w string) bool { return strings.HasSuffix(w, " "+r) })
			}) {
				t.Errorf("no detector case triggers %s", r)
			}
			if hostStoreAdvice[r] == "" {
				t.Errorf("rule %s has no advice text", r)
			}
		}
	})
}

// The walk: the excluded directory names apply below the root only (a root
// under .worktrees is still scanned), test files are read (K1 covers them), and
// a file that does not parse fails the scan instead of passing unread.
func TestHostStoreGuardTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".worktrees", "feat", "wt")
	bad := "package p\nimport \"github.com/zalando/go-keyring\"\n"
	files := map[string]string{
		"go.mod":                         "module example.com/m\n",
		"main.go":                        bad,
		"pkg/deep/ok.go":                 bad,
		"pkg/clean.go":                   "package p\n",
		"pkg/thing_test.go":              bad,
		"internal/credentials/own.go":    bad,
		"internal/credentials/a_test.go": bad,
		".worktrees/other/w.go":          bad,
		"pkg/.worktrees/nested/w.go":     bad,
		".git/hooks/h.go":                bad,
		"vendor/v/v.go":                  bad,
		"pkg/testdata/t.go":              bad,
		"docs-site/node/d.go":            bad,
		"web/node_modules/n/n.go":        bad,
		"README.md":                      "not go",
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
	vs, n, err := scanHostStoreTree(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.File+":"+strconv.Itoa(v.Line)+" "+v.Rule)
	}
	slices.Sort(got)
	want := []string{
		"internal/credentials/own.go:2 K1",
		"main.go:2 K1",
		"pkg/deep/ok.go:2 K1",
		"pkg/thing_test.go:2 K1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("scanHostStoreTree reported\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// main.go, pkg/deep/ok.go, pkg/clean.go, pkg/thing_test.go, credentials/own.go, credentials/a_test.go
	if n != 6 {
		t.Errorf("scanHostStoreTree read %d files, want 6", n)
	}

	broken := filepath.Join(root, "pkg", "broken.go")
	if err := os.WriteFile(broken, []byte("package p\nfunc ("), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scanHostStoreTree(root); err == nil || !strings.Contains(err.Error(), "pkg/broken.go") {
		t.Errorf("a file that does not parse gave %v, want an error naming pkg/broken.go", err)
	}
}

// docRequirement is one documentation file and the strings it must carry.
type docRequirement struct {
	File string   // slash path from the module root
	Want []string // each must appear verbatim in the file
}

// hostStoreDocRequirements is what the documentation of DEVDESK-610 must say:
// the section title, the isolation entry points, the seam and the guard.
var hostStoreDocRequirements = []docRequirement{
	{
		File: ".claude/CLAUDE.md",
		Want: []string{
			"**Tests and the host secret store (DEVDESK-610).**",
			"fakekeyring.Install()",
			"fakekeyring.Fresh(t)",
			"TestHostStoreGuard",
		},
	},
	{
		File: "docs/architecture/configuration.md",
		Want: []string{
			"### Testing code that reaches the secret store",
			"HostStore",
			"SwapHostStoreForTest",
			"FreshUnreachable",
			"K1",
			"K2",
			"K3",
			"t.Parallel",
		},
	},
}

// missingDocStrings returns one message per required string absent from text,
// each naming the file and the string.
func missingDocStrings(file, text string, want []string) []string {
	var out []string
	for _, w := range want {
		if !strings.Contains(text, w) {
			out = append(out, fmt.Sprintf("%s does not contain %q (DEVDESK-610 documentation)", file, w))
		}
	}
	return out
}

// The documentation keeps the trap, the isolation and the guard: a section that
// is renamed or trimmed fails here, naming the file and the missing string.
func TestHostStoreGuardDocSection(t *testing.T) {
	root := docModuleRoot(t)
	for _, req := range hostStoreDocRequirements {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.File)))
		if err != nil {
			t.Errorf("%s: %v", req.File, err)
			continue
		}
		for _, msg := range missingDocStrings(req.File, string(b), req.Want) {
			t.Error(msg)
		}
	}
}

// The check itself bites: every required string, removed in turn from the real
// text, is reported with the file and the string, and nothing else is.
func TestHostStoreGuardDocSectionBites(t *testing.T) {
	root := docModuleRoot(t)
	for _, req := range hostStoreDocRequirements {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.File)))
		if err != nil {
			t.Fatalf("%s: %v", req.File, err)
		}
		text := string(b)
		for _, w := range req.Want {
			// Strip every occurrence from the copy; the real file is untouched.
			cut := strings.ReplaceAll(text, w, "")
			got := missingDocStrings(req.File, cut, req.Want)
			// A shorter string may be a substring of another one (K1 in a longer
			// token, HostStore in SwapHostStoreForTest): only require that w is
			// reported when nothing else of the list still contains it.
			stillThere := false
			for _, o := range req.Want {
				if o != w && strings.Contains(o, w) {
					stillThere = true
				}
			}
			if stillThere {
				continue
			}
			want := fmt.Sprintf("%s does not contain %q", req.File, w)
			if !slices.ContainsFunc(got, func(m string) bool { return strings.HasPrefix(m, want) }) {
				t.Errorf("removing %q from %s was not reported; got %v", w, req.File, got)
			}
		}
	}
	if got := missingDocStrings("x.md", "", []string{"a"}); len(got) != 1 || !strings.Contains(got[0], `x.md`) || !strings.Contains(got[0], `"a"`) {
		t.Errorf("the message must name the file and the string, got %v", got)
	}
	if got := missingDocStrings("x.md", "a b", []string{"a", "b"}); len(got) != 0 {
		t.Errorf("a complete text was reported: %v", got)
	}
}

// docModuleRoot is the module root, found from the working directory.
func docModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := hostStoreModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
