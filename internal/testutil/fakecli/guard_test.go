package fakecli_test

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

// OC-02 (DEVDESK-797): the static guard that keeps the fake binary single and
// out of production. Modelled on the host store guard
// (internal/credentials/hoststore_guard_test.go): it reads every .go file of
// the module with go/parser, no type information, and reports two rules.
//
//	F1  an import of internal/testutil/fakecli from a non-test file (it imports
//	    "testing" and must not reach a production binary). No exception, like K3.
//	F2  a _test.go file that puts its own fake binary on PATH: it overrides PATH
//	    (Setenv("PATH", ...)) and either copies the test binary (os.Executable)
//	    or writes a script (a string literal starting with "#!"). Such a fake
//	    belongs to fakecli. The only exceptions are the files of legacyFakes,
//	    which must shrink and never grow.
//
// What the guard cannot see: a fake whose PATH is set through os/exec's
// Cmd.Env or through a helper in another package, an import path built at run
// time, a shim written without a "#!" literal or without os.Executable, and a
// fake that is not on PATH at all but reached through engine.Shape.Binary or a
// path in the configuration (internal/scan container tests and
// internal/ui/netdiag path_engine_reads_test.go were such fakes; they now use
// fakecli.InstallAt, but a new one would not be caught).

const (
	ruleFakeCLIInProd = "F1"
	ruleOwnFakeOnPath = "F2"

	fakeCLIPath      = "github.com/anthnel/devdesk/internal/testutil/fakecli"
	fakeCLIDir       = "internal/testutil/fakecli"
	fakeGuardMinimum = 50
)

// fakeGuardExcludedDirs are skipped at any depth below the module root, like the
// other source guards.
var fakeGuardExcludedDirs = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

// legacyFakes lists the test files that still carry their own fake binary on
// PATH. The list only shrinks: the guard fails when an entry no longer matches
// (migrate it and delete the line) and when a new file is added to the tree.
//
//   - Temporary, tracked for later stories:
//     nexusbench/helper_process_test.go is deferred (OC-02 follow-up): a
//     development tool outside dk (the story allows it), and its fakes need
//     what fakecli does not offer yet (a hang, a large stdout, the content of
//     --authfile and --policy files, the PID, TMPDIR, stdin).
//     nexusbench/kubectl_test.go (records argv and stdin) goes with it.
//     app/scan_tools_hermetic_test.go (witness scripts for trivy, gitleaks and
//     plumber) is followed up with OC-03b.
//   - Permanent, assumed: browser and clipboard stand in for programs that are
//     not engine tools (a browser opener, powershell).
var legacyFakes = map[string]string{
	"internal/nexusbench/helper_process_test.go": "temporary, OC-02 follow-up: hang, large stdout, authfile and policy capture, PID, TMPDIR, stdin",
	"internal/nexusbench/kubectl_test.go":        "temporary, OC-02 follow-up: kubectl script that records argv and stdin",
	"internal/app/scan_tools_hermetic_test.go":   "temporary, tracked with OC-03b: witness scripts for trivy, gitleaks and plumber",
	"internal/browser/browser_test.go":           "permanent: browser opener, not an engine tool",
	"internal/clipboard/clipboard_test.go":       "permanent: powershell stand-in, not an engine tool",
}

// fakeViolation is one place where the fake binary rules are broken.
type fakeViolation struct {
	File string // slash path from the module root
	Line int
	Rule string // "F1" or "F2"
}

// fakeAdvice names the rule broken and the way out.
var fakeAdvice = map[string]string{
	ruleFakeCLIInProd: "imports internal/testutil/fakecli from a non-test file; " +
		"it imports \"testing\" and belongs to _test.go files only (OC-02, DEVDESK-797)",
	ruleOwnFakeOnPath: "puts its own fake binary on PATH; install one with fakecli.Install(t, scenario, tools...) " +
		"instead (OC-02, DEVDESK-797)",
}

func (v fakeViolation) String() string {
	return fmt.Sprintf("%s:%d: %s %s", v.File, v.Line, v.Rule, fakeAdvice[v.Rule])
}

// scanFakeSource parses src as the file name (a slash path from the module
// root) and returns its violations ordered by line, then rule.
func scanFakeSource(name string, src []byte) ([]fakeViolation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	isTest := strings.HasSuffix(name, "_test.go")

	var out []fakeViolation
	add := func(pos token.Pos, rule string) {
		out = append(out, fakeViolation{File: name, Line: fset.Position(pos).Line, Rule: rule})
	}

	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if p == fakeCLIPath && !isTest {
			add(spec.Pos(), ruleFakeCLIInProd)
		}
	}

	if isTest && path.Dir(name) != fakeCLIDir {
		var pathOverride bool
		var firstBody token.Pos
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "Setenv" && len(n.Args) > 0 && isStringLit(n.Args[0], "PATH") {
					pathOverride = true
				}
				if sel.Sel.Name == "Executable" && len(n.Args) == 0 {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" && !firstBody.IsValid() {
						firstBody = n.Pos()
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil && strings.HasPrefix(s, "#!") && !firstBody.IsValid() {
						firstBody = n.Pos()
					}
				}
			}
			return true
		})
		if pathOverride && firstBody.IsValid() {
			add(firstBody, ruleOwnFakeOnPath)
		}
	}

	slices.SortStableFunc(out, func(a, b fakeViolation) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return out, nil
}

// isStringLit tells whether e is the string literal want.
func isStringLit(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == want
}

// scanFakeTree scans every .go file under root and returns the violations and
// the number of files read.
func scanFakeTree(root string) ([]fakeViolation, int, error) {
	var all []fakeViolation
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
			if rel != "." && slices.Contains(fakeGuardExcludedDirs, d.Name()) {
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
		vs, err := scanFakeSource(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, vs...)
		return nil
	})
	return all, files, err
}

// fakeModuleRoot walks up from dir to the directory holding go.mod.
func fakeModuleRoot(dir string) (string, error) {
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
	fakeGuardOnce       sync.Once
	fakeGuardViolations []fakeViolation
	fakeGuardFiles      int
	fakeGuardErr        error
)

// scanFakeRepo scans the module that holds this package, once per test process.
func scanFakeRepo(t *testing.T) []fakeViolation {
	t.Helper()
	fakeGuardOnce.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			fakeGuardErr = err
			return
		}
		root, err := fakeModuleRoot(wd)
		if err != nil {
			fakeGuardErr = err
			return
		}
		fakeGuardViolations, fakeGuardFiles, fakeGuardErr = scanFakeTree(root)
	})
	if fakeGuardErr != nil {
		t.Fatalf("scanning the module for fake binaries: %v", fakeGuardErr)
	}
	// A walk that reads almost nothing would pass for the wrong reason.
	if fakeGuardFiles < fakeGuardMinimum {
		t.Fatalf("the fake binary guard read %d files, want the whole module", fakeGuardFiles)
	}
	return fakeGuardViolations
}

// Criterion 4 on the real module: nothing outside a _test.go file imports
// fakecli. Criterion 3 on the real module: no test file but the files of
// legacyFakes puts a fake of its own on PATH; oci_resources/faketool_test.go and
// scan/dependencies_test.go, named by the story, are not in that list
// (nexusbench/helper_process_test.go is, deferred).
func TestFakeCLIGuardWalk(t *testing.T) {
	for _, v := range scanFakeRepo(t) {
		if v.Rule == ruleOwnFakeOnPath {
			if _, ok := legacyFakes[v.File]; ok {
				continue
			}
		}
		t.Error(v.String())
	}
}

// An exception that no longer matches is dead weight: the list only shrinks.
func TestLegacyFakesAreStillFakes(t *testing.T) {
	flagged := map[string]bool{}
	for _, v := range scanFakeRepo(t) {
		if v.Rule == ruleOwnFakeOnPath {
			flagged[v.File] = true
		}
	}
	for file := range legacyFakes {
		if !flagged[file] {
			t.Errorf("%s is listed in legacyFakes but no longer carries its own fake binary: delete the line", file)
		}
	}
}

// fakeGuardCase is one synthetic source and the violations expected, each as
// "line rule".
type fakeGuardCase struct {
	name string
	file string
	src  string
	want []string
}

// Each rule is exercised on synthetic sources: a violation is reported with its
// rule, and the allowed uses are not. A rule dropped from the detector would
// fail here while the real-module test stays green.
func TestFakeCLIGuardDetectors(t *testing.T) {
	cases := []fakeGuardCase{
		// F1
		{
			name: "F1: a production file imports fakecli",
			file: "internal/other/x.go",
			src: `package other
import "github.com/anthnel/devdesk/internal/testutil/fakecli"
var _ = fakecli.Install
`,
			want: []string{"2 F1"},
		},
		{
			name: "F1: an aliased import in cmd",
			file: "cmd/tool/main.go",
			src: `package main
import fc "github.com/anthnel/devdesk/internal/testutil/fakecli"
func main() { _ = fc.Install }
`,
			want: []string{"2 F1"},
		},
		{
			name: "F1: a blank import",
			file: "internal/other/x.go",
			src: `package other
import _ "github.com/anthnel/devdesk/internal/testutil/fakecli"
`,
			want: []string{"2 F1"},
		},
		{
			name: "F1: a grouped import is found at its own line",
			file: "internal/other/x.go",
			src: `package other
import (
	"os"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)
var _ = os.Args
var _ = fakecli.Install
`,
			want: []string{"5 F1"},
		},
		{
			name: "F1: a test file may import fakecli",
			file: "internal/docker/x_test.go",
			src: `package docker
import "github.com/anthnel/devdesk/internal/testutil/fakecli"
var _ = fakecli.Install
`,
		},
		{
			name: "F1: a package that only looks alike is not fakecli",
			file: "internal/other/x.go",
			src: `package other
import "github.com/anthnel/devdesk/internal/testutil/fakecli/other"
`,
		},
		// F2
		{
			name: "F2: a test copies the test binary under a tool name and sets PATH",
			file: "internal/scan/x_test.go",
			src: `package scan
import ("os";"testing")
func install(t *testing.T) {
	self, _ := os.Executable()
	_ = self
	t.Setenv("PATH", t.TempDir())
}
`,
			want: []string{"4 F2"},
		},
		{
			name: "F2: a test writes a shell script and sets PATH",
			file: "internal/engine/x_test.go",
			src: `package engine
import "testing"
func install(t *testing.T) {
	_ = []byte("#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", t.TempDir())
}
`,
			want: []string{"4 F2"},
		},
		{
			name: "F2: a raw string shebang assembled with another string",
			file: "internal/engine/x_test.go",
			src: "package engine\nimport \"testing\"\nfunc install(t *testing.T, body string) {\n" +
				"\t_ = []byte(`#!/bin/sh\n` + body)\n\tt.Setenv(\"PATH\", t.TempDir())\n}\n",
			want: []string{"4 F2"},
		},
		{
			name: "F2: os.Setenv counts as a PATH override too",
			file: "internal/engine/x_test.go",
			src: `package engine
import "os"
func install() {
	_ = "#!/bin/sh"
	os.Setenv("PATH", "/x")
}
`,
			want: []string{"4 F2"},
		},
		{
			name: "F2: a shell script without a PATH override is not a fake on PATH",
			file: "internal/git/x_test.go",
			src: `package git
func body() string { return "#!/bin/sh\nexit 0\n" }
`,
		},
		{
			name: "F2: a PATH override without a fake body is allowed",
			file: "internal/ui/x_test.go",
			src: `package ui
import "testing"
func empty(t *testing.T) { t.Setenv("PATH", t.TempDir()) }
`,
		},
		{
			name: "F2: another variable is not PATH",
			file: "internal/ui/x_test.go",
			src: `package ui
import ("os";"testing")
func install(t *testing.T) {
	_, _ = os.Executable()
	t.Setenv("HOME", t.TempDir())
}
`,
		},
		{
			name: "F2: a production file is not a test file",
			file: "internal/ui/x.go",
			src: `package ui
import "os"
func install() {
	_, _ = os.Executable()
	os.Setenv("PATH", "/x")
}
`,
		},
		{
			name: "F2: fakecli itself is exempt",
			file: "internal/testutil/fakecli/x_test.go",
			src: `package fakecli_test
import ("os";"testing")
func install(t *testing.T) {
	_, _ = os.Executable()
	t.Setenv("PATH", t.TempDir())
}
`,
		},
		{
			name: "F2: a sibling directory of fakecli is not exempt",
			file: "internal/testutil/fakecliextra/x_test.go",
			src: `package fakecliextra
import ("os";"testing")
func install(t *testing.T) {
	_, _ = os.Executable()
	t.Setenv("PATH", t.TempDir())
}
`,
			want: []string{"4 F2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, err := scanFakeSource(tc.file, []byte(tc.src))
			if err != nil {
				t.Fatalf("scanFakeSource: %v", err)
			}
			var got []string
			for _, v := range vs {
				got = append(got, fmt.Sprintf("%d %s", v.Line, v.Rule))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

// A violation names the file and the line, and what to do about it.
func TestFakeViolationNamesFileAndLine(t *testing.T) {
	vs, err := scanFakeSource("internal/other/x.go", []byte(`package other
import "github.com/anthnel/devdesk/internal/testutil/fakecli"
`))
	if err != nil || len(vs) != 1 {
		t.Fatalf("scan = %v, %v", vs, err)
	}
	msg := vs[0].String()
	for _, want := range []string{"internal/other/x.go:2:", "F1", "fakecli", "DEVDESK-797"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}
