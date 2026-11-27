package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// OC-05 (DEVDESK-802), guard G2: only the runners start the engine.
//
// A static test, modelled on the other source guards (the signal guard of
// internal/ports, the container guard of internal/netproxy, the host store guard
// of internal/credentials, the fake binary guard of internal/testutil/fakecli):
// it reads every non-test .go file of the module with go/parser, no type
// information, and reports, with file and line, each place that starts or
// prepares a start of the container engine, kind or kubectl outside the runners.
//
// It lives next to guard G1 (the run-time half of the same story, OC-03) because
// both answer the question "who may reach a real engine"; it is a test of the
// whole module, not of this package.
//
// What counts as a site (each is reported once):
//
//	named   exec.Command or exec.CommandContext whose binary argument is, once
//	        constants are folded, docker, podman, kind or kubectl: a string
//	        literal, a file-level constant, a concatenation of those, a path
//	        ending in one of the names ("/usr/bin/docker", "docker.exe"), or
//	        engine.Docker, engine.Podman, config.EngineDocker, config.EnginePodman.
//	binary  a read of engine.Shape.Binary: engine.Current().Binary, a variable
//	        assigned from engine.Current(), engine.ShapeFor(...) or
//	        engine.Resolve(...) within the same top-level declaration, or a
//	        parameter typed engine.Shape or *engine.Shape. The binary is the one
//	        thing a launch needs, so reading it is treated as preparing one.
//
// Test files are not scanned: a test that reaches a real engine is guard G1's
// (engineguard.Check), at run time.
//
// Limits, stated here and not hidden (the neighbouring guards say the same of
// themselves):
//
//   - A binary name built at run time is not seen: a variable, a field, a
//     parameter or a function result (exec.Command(tc.Name, ...) in
//     internal/scan/run.go and tool_source.go starts the engine this way, from
//     what containerRun.cmd returned; internal/ui/containers/update.go starts a
//     terminal emulator that runs the shell argv). A constant defined in another
//     file of the package is not folded either.
//   - Flow is followed only inside one top-level declaration, by name and
//     without scopes: a Shape returned from a function, stored in a field or
//     passed to a function of an untyped parameter is not followed, and a
//     variable that shadows a tracked one is read as the tracked one.
//   - An import is resolved by its path, so a renamed os/exec or engine import
//     is seen; a dot import of the engine package, a function value
//     (start := exec.Command), syscall.Exec, os.StartProcess, an exec.Cmd
//     literal with a Path, and cgo are not.
//   - An external binary is not seen at all: a shell line ("sh", "-c",
//     "docker ..."), a script, a Makefile or mise task, the CI workflow.
//   - exec.LookPath("docker") names the engine but starts nothing, so it is not
//     a site; engine.Shape.Binary read for the same look-up is (see Detect).
//   - A runner file is exempt as a whole: the guard does not check what it does
//     with the engine, only that nothing else does it.
//   - internal/nexusbench has its own kubectl runner and its own guard
//     (guard_test.go there); it starts kubectl through a path variable, so it is
//     not seen here.

const (
	enginePackage = "github.com/anthnel/devdesk/internal/engine"
	configPackage = "github.com/anthnel/devdesk/internal/config"

	// launchGuardMinimum is a floor on the files read: a walk that reads almost
	// nothing would pass for the wrong reason.
	launchGuardMinimum = 200

	kindNamed  = "named"
	kindBinary = "binary"
)

// launchBinaries are the programs whose start is the runners' business.
var launchBinaries = []string{"docker", "podman", "kind", "kubectl"}

// shapeSources are the engine functions whose result is a Shape (Resolve returns
// it first, then an error).
var shapeSources = []string{"Current", "ShapeFor", "Resolve"}

// engineNameConsts are the exported constants that spell an engine name, by
// package.
var engineNameConsts = map[string]map[string]string{
	"engine": {"Docker": "docker", "Podman": "podman"},
	"config": {"EngineDocker": "docker", "EnginePodman": "podman"},
}

// launchGuardExcludedDirs are skipped at any depth below the module root, like
// the other source guards, and so is any directory whose name starts with a dot.
var launchGuardExcludedDirs = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

// launchRunner is a file that may start the engine.
type launchRunner struct {
	File string // slash path from the module root
	// Required is true for a runner that exists: it must still carry a site, or
	// the guard would be reading a file that no longer does the work. The kind
	// runner does not exist yet (its package comes with the kind stories), so its
	// rules are active and the file is optional.
	Required bool
}

var launchRunners = []launchRunner{
	{File: "internal/docker/exec.go", Required: true},
	{File: "internal/kind/runner.go", Required: false},
	{File: "internal/scan/container_run.go", Required: true},
}

// launchException is a function outside the runners that starts, or reads the
// binary to start, the engine, kind or kubectl, and why it may. It is keyed by
// file and function, not by line, so an unrelated edit does not move it; Sites
// is how many sites the function has, so a second launch added to the same
// function is not covered by the first one's reason.
//
// The list is a debt register: an entry is removed when the site moves behind a
// runner, and the guard fails on an entry whose code is gone. It does not grow
// for convenience; a new entry needs a reason a reviewer can disagree with.
type launchException struct {
	File   string // slash path from the module root
	Func   string // "name", "(Recv).name" or "(*Recv).name"; "<package level>" outside a function
	Sites  int
	Reason string
}

// There is no exception for the log pager (V, OC-84, DEVDESK-801): it reads
// `<engine> logs` from docker.LogsCmd, which is built by the docker runner, and
// starts only the user's pager. The shell (T) goes through docker.ShellCmd and
// docker.DetectShell, which also use the runner; neither is a site any more.
var launchExceptions = []launchException{
	{
		File:  "internal/scan/scanner.go",
		Func:  "imageIsLocal",
		Sites: 1,
		Reason: "temporary: starts `<engine> images -q <image>` itself, before a scan, because internal/scan does not import " +
			"internal/docker (the engine package exists for that reason). To move behind containerRun / the docker runner.",
	},
	{
		File:  "internal/scan/toolbox.go",
		Func:  "Detect",
		Sites: 1,
		Reason: "temporary: looks the engine binary up on PATH, then toolVersion asks it `version` (a one-off probe at startup " +
			"and on a context switch). To move behind the docker runner.",
	},
	{
		File:  "internal/app/configuration.go",
		Func:  "(*App).resolveContainerEngine",
		Sites: 1,
		Reason: "permanent: reads Shape.Binary for a log line only, nothing is started. Listed because the guard follows " +
			"engine.Resolve, whose result is the one a launch would read.",
	},
}

// launch is one site: a place that starts the engine, kind or kubectl, or reads
// the binary to do it.
type launch struct {
	File string // slash path from the module root
	Line int
	Func string
	Kind string // kindNamed or kindBinary
	What string
}

func (l launch) String() string {
	return fmt.Sprintf("%s:%d: %s (in %s)", l.File, l.Line, l.What, l.Func)
}

// ── Detector ─────────────────────────────────────────────────────────────────

// importName returns the name path is imported under in f: "" when it is not
// imported, "." for a dot import, "_" for a blank one.
func importName(f *ast.File, path, def string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return def
	}
	return ""
}

// constString folds e to a string when it is made of literals, constants of
// consts and concatenations of those. pkgConsts maps a local package name to the
// exported constants it is known to hold.
func constString(e ast.Expr, consts map[string]string, pkgConsts map[string]map[string]string) (string, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return constString(e.X, consts, pkgConsts)
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[e.Name]
		return s, ok
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			s, ok := pkgConsts[x.Name][e.Sel.Name]
			return s, ok
		}
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := constString(e.X, consts, pkgConsts)
		r, rok := constString(e.Y, consts, pkgConsts)
		return l + r, lok && rok
	}
	return "", false
}

// fileConsts collects the string constants declared at file level, in order.
func fileConsts(f *ast.File, pkgConsts map[string]map[string]string) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					break
				}
				if s, ok := constString(vs.Values[i], out, pkgConsts); ok {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

// namesALaunchBinary reports whether name, a program a call is about to start,
// is the engine, kind or kubectl: the base of a path, without ".exe", any case.
func namesALaunchBinary(name string) bool {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	return slices.Contains(launchBinaries, name)
}

// funcLabel names the top-level declaration d for an exception: "name",
// "(Recv).name" or "(*Recv).name", and "<package level>" for a var or const.
func funcLabel(d ast.Decl) string {
	fn, ok := d.(*ast.FuncDecl)
	if !ok {
		return "<package level>"
	}
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return fn.Name.Name
	}
	typ, star := fn.Recv.List[0].Type, ""
	if s, ok := typ.(*ast.StarExpr); ok {
		typ, star = s.X, "*"
	}
	if idx, ok := typ.(*ast.IndexExpr); ok { // generic receiver
		typ = idx.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return "(" + star + id.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}

// isEngineCall reports whether e is a call engine.<one of names>(...).
func isEngineCall(e ast.Expr, engineName string, names []string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == engineName && slices.Contains(names, sel.Sel.Name)
}

// shapeIdents collects, inside one top-level declaration, the names that hold an
// engine.Shape: assigned from the engine functions of shapeSources, or declared
// as a parameter of type engine.Shape or *engine.Shape.
func shapeIdents(decl ast.Decl, engineName string) map[string]bool {
	out := map[string]bool{}
	if engineName == "" || engineName == "." || engineName == "_" {
		return out
	}
	bind := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			switch {
			case len(rhs) == len(lhs) && isEngineCall(rhs[i], engineName, shapeSources):
				out[id.Name] = true
			case len(rhs) == 1 && i == 0 && isEngineCall(rhs[0], engineName, shapeSources): // shape, err := engine.Resolve(...)
				out[id.Name] = true
			}
		}
	}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			bind(n.Lhs, n.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(n.Names))
			for i, id := range n.Names {
				lhs[i] = id
			}
			bind(lhs, n.Values)
		case *ast.FuncType:
			for _, field := range n.Params.List {
				typ := field.Type
				if s, ok := typ.(*ast.StarExpr); ok {
					typ = s.X
				}
				sel, ok := typ.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Shape" {
					continue
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == engineName {
					for _, name := range field.Names {
						out[name.Name] = true
					}
				}
			}
		}
		return true
	})
	return out
}

// scanLaunches parses src as the file name (a slash path from the module root)
// and returns its sites ordered by line. A _test.go file has none: see the
// header.
func scanLaunches(name string, src []byte) ([]launch, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, "_test.go") {
		return nil, nil
	}

	execName := importName(f, "os/exec", "exec")
	engineName := importName(f, enginePackage, "engine")
	pkgConsts := map[string]map[string]string{}
	if engineName != "" && engineName != "." && engineName != "_" {
		pkgConsts[engineName] = engineNameConsts["engine"]
	}
	if cfg := importName(f, configPackage, "config"); cfg != "" && cfg != "." && cfg != "_" {
		pkgConsts[cfg] = engineNameConsts["config"]
	}
	consts := fileConsts(f, pkgConsts)

	var out []launch
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		shapes := shapeIdents(decl, engineName)
		add := func(pos token.Pos, kind, what string) {
			out = append(out, launch{File: name, Line: fset.Position(pos).Line, Func: label, Kind: kind, What: what})
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if n.Sel.Name == "Binary" && isShapeExpr(n.X, engineName, shapes) {
					add(n.Pos(), kindBinary, "reads engine.Shape.Binary")
				}
			case *ast.CallExpr:
				fn, argIdx := execCallee(n.Fun, execName)
				if fn == "" || argIdx >= len(n.Args) {
					return true
				}
				if s, ok := constString(n.Args[argIdx], consts, pkgConsts); ok && namesALaunchBinary(s) {
					add(n.Pos(), kindNamed, fmt.Sprintf("exec.%s(%q, ...)", fn, s))
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// isShapeExpr reports whether e is an engine.Shape: engine.Current(), a call of
// the other shape sources, or a variable tracked by shapeIdents.
func isShapeExpr(e ast.Expr, engineName string, shapes map[string]bool) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return isShapeExpr(e.X, engineName, shapes)
	case *ast.Ident:
		return shapes[e.Name]
	case *ast.CallExpr:
		return engineName != "" && isEngineCall(e, engineName, shapeSources)
	}
	return false
}

// execCallee returns "Command" or "CommandContext" when fun is that function of
// os/exec, and the index of the argument that names the binary.
func execCallee(fun ast.Expr, execName string) (string, int) {
	switch fun := fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok && execName != "" && x.Name == execName {
			return commandIndex(fun.Sel.Name)
		}
	case *ast.Ident:
		if execName == "." {
			return commandIndex(fun.Name)
		}
	}
	return "", 0
}

func commandIndex(name string) (string, int) {
	switch name {
	case "Command":
		return name, 0
	case "CommandContext":
		return name, 1
	}
	return "", 0
}

// ── Judgement ────────────────────────────────────────────────────────────────

// judgeLaunches turns the sites found into the problems to report: a site
// outside every runner and without an exception, an exception whose count is not
// the number of sites, an exception whose code is gone, a runner that no longer
// does what it is the runner for, and an exception that is not well formed.
func judgeLaunches(found []launch, runners []launchRunner, exceptions []launchException) []string {
	var problems []string
	runnerSeen := map[string]bool{}
	byFunc := map[string][]launch{}
	for _, l := range found {
		if slices.ContainsFunc(runners, func(r launchRunner) bool { return r.File == l.File }) {
			runnerSeen[l.File] = true
			continue
		}
		byFunc[l.File+"\x00"+l.Func] = append(byFunc[l.File+"\x00"+l.Func], l)
	}

	for _, r := range runners {
		if r.Required && !runnerSeen[r.File] {
			problems = append(problems, fmt.Sprintf(
				"%s is a runner but carries no site: the file moved or the guard no longer sees it; "+
					"update launchRunners (OC-05, DEVDESK-802)", r.File))
		}
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := byFunc[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why it may start the engine", e.File, e.Func))
		case slices.ContainsFunc(runners, func(r launchRunner) bool { return r.File == e.File }):
			problems = append(problems, fmt.Sprintf("exception %s %s is in a runner, which needs none: delete it", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s has no site left; delete it from launchExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf(
				"exception %s %s covers %d site(s) but the function has %d: %s",
				e.File, e.Func, e.Sites, len(sites), joinSites(sites)))
		}
	}

	keys := make([]string, 0, len(byFunc))
	for k := range byFunc {
		if !covered[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, l := range byFunc[k] {
			problems = append(problems, fmt.Sprintf(
				"%s: starts or prepares the engine, kind or kubectl outside the runners; go through the runner "+
					"(internal/docker/exec.go, internal/kind/runner.go, internal/scan/container_run.go) (OC-05, DEVDESK-802)", l))
		}
	}
	return problems
}

func joinSites(sites []launch) string {
	parts := make([]string, len(sites))
	for i, s := range sites {
		parts[i] = fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return strings.Join(parts, ", ")
}

// ── The module ───────────────────────────────────────────────────────────────

// launchModuleRoot finds the directory holding go.mod, from where this source
// file lives when the build kept its path, else from the working directory.
func launchModuleRoot() (string, error) {
	var starts []string
	if _, file, _, ok := runtime.Caller(0); ok && filepath.IsAbs(file) {
		starts = append(starts, filepath.Dir(file))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, dir := range starts {
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("no go.mod above %v", starts)
}

// scanLaunchTree scans every .go file under root and returns the sites, ordered
// by file then line, and the number of files read.
func scanLaunchTree(root string) ([]launch, int, error) {
	var all []launch
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
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || slices.Contains(launchGuardExcludedDirs, d.Name())) {
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
		sites, err := scanLaunches(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, sites...)
		return nil
	})
	return all, files, err
}

var (
	launchScanOnce  sync.Once
	launchScanSites []launch
	launchScanFiles int
	launchScanErr   error
)

// scanLaunchRepo scans the module that holds this package, once per test process.
func scanLaunchRepo(t *testing.T) []launch {
	t.Helper()
	launchScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			launchScanErr = err
			return
		}
		launchScanSites, launchScanFiles, launchScanErr = scanLaunchTree(root)
	})
	if launchScanErr != nil {
		t.Fatalf("scanning the module for engine launches: %v", launchScanErr)
	}
	if launchScanFiles < launchGuardMinimum {
		t.Fatalf("the engine launch guard read %d files, want the whole module (at least %d)", launchScanFiles, launchGuardMinimum)
	}
	return launchScanSites
}

// Criteria 1 to 3 on the real module: the engine, kind and kubectl are started
// only from the runners and from the named exceptions, and an exception whose
// code is gone fails the test.
func TestOnlyTheRunnersStartTheEngine(t *testing.T) {
	for _, p := range judgeLaunches(scanLaunchRepo(t), launchRunners, launchExceptions) {
		t.Error(p)
	}
}

// Criterion 2: the log pager (V) is not an exception since OC-84.
func TestThePagerIsNotAnException(t *testing.T) {
	for _, e := range launchExceptions {
		file := strings.ToLower(e.File)
		text := strings.ToLower(e.Func + " " + e.Reason)
		if strings.HasSuffix(file, "ui/viewer/update.go") || strings.HasSuffix(file, "ui/containers/sources.go") ||
			strings.Contains(text, "pager") {
			t.Errorf("exception %s %s concerns the log pager: OC-84 (DEVDESK-801) removed it, there is none to give", e.File, e.Func)
		}
	}
}

// ── The detector on sources ──────────────────────────────────────────────────
//
// The guard is only worth something if it sees a violation: each rule is run on
// sources that break it, and on sources that look like a breach and are not.

// siteCase is one source and the sites expected, each as "line kind".
type siteCase struct {
	name string
	file string
	src  string
	want []string
}

func TestLaunchDetectorOnSources(t *testing.T) {
	const other = "internal/ui/other/x.go"
	tests := []siteCase{
		// criterion 4: a new exec.Command("docker", ...) elsewhere
		{"docker literal", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "ps") }
`, []string{"3 named"}},
		{"podman literal", other, `package other
import "os/exec"
func f() { _ = exec.Command("podman", "ps") }
`, []string{"3 named"}},
		// criterion 5: kind and kubectl are active although internal/kind does not exist
		{"kind literal", other, `package other
import "os/exec"
func f() { _ = exec.Command("kind", "create", "cluster") }
`, []string{"3 named"}},
		{"kubectl literal", other, `package other
import "os/exec"
func f() { _ = exec.Command("kubectl", "get", "pods") }
`, []string{"3 named"}},
		{"CommandContext names the binary in its second argument", other, `package other
import ("context"; "os/exec")
func f(ctx context.Context) { _ = exec.CommandContext(ctx, "kubectl", "get") }
`, []string{"3 named"}},
		{"CommandContext does not read its context as the binary", other, `package other
import ("context"; "os/exec")
func f() { _ = exec.CommandContext(context.Background(), "git", "status") }
`, nil},

		// how a name is spelled
		{"a path to the binary", other, `package other
import "os/exec"
func f() { _ = exec.Command("/usr/local/bin/docker", "ps") }
`, []string{"3 named"}},
		{"a Windows path with its extension", other, `package other
import "os/exec"
func f() { _ = exec.Command(` + "`C:\\Program Files\\Docker\\docker.EXE`" + `, "ps") }
`, []string{"3 named"}},
		{"a file-level constant", other, `package other
import "os/exec"
const engineName = "podman"
func f() { _ = exec.Command(engineName, "ps") }
`, []string{"4 named"}},
		{"a concatenation of literals", other, `package other
import "os/exec"
const prefix = "kube"
func f() { _ = exec.Command(prefix + "ctl") }
`, []string{"4 named"}},
		{"engine.Docker", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/engine")
func f() { _ = exec.Command(engine.Docker, "ps") }
`, []string{"3 named"}},
		{"config.EnginePodman", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/config")
func f() { _ = exec.Command(config.EnginePodman, "ps") }
`, []string{"3 named"}},
		{"a renamed os/exec import", other, `package other
import run "os/exec"
func f() { _ = run.Command("docker", "ps") }
`, []string{"3 named"}},
		{"a dot import of os/exec", other, `package other
import . "os/exec"
func f() { _ = Command("docker", "ps") }
`, []string{"3 named"}},
		{"two sites are two reports, in line order", other, `package other
import "os/exec"
func f() { _ = exec.Command("kind", "get", "clusters") }
func g() { _ = exec.Command("docker", "ps") }
`, []string{"3 named", "4 named"}},

		// the binary of a Shape
		{"engine.Current().Binary", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/engine")
func f() { _ = exec.Command(engine.Current().Binary, "ps") }
`, []string{"3 binary"}},
		{"engine.Current().Binary alone, to look it up", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/engine")
func f() { _, _ = exec.LookPath(engine.Current().Binary) }
`, []string{"3 binary"}},
		{"a variable assigned from engine.Current()", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/engine")
func f() {
	eng := engine.Current()
	_, _ = exec.LookPath(eng.Binary)
}
`, []string{"5 binary"}},
		{"a variable declared with var from engine.ShapeFor", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
func f() string {
	var s = engine.ShapeFor("podman")
	return s.Binary
}
`, []string{"5 binary"}},
		{"the Shape of engine.Resolve", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
func f(p string) string {
	shape, err := engine.Resolve(p)
	if err != nil { return "" }
	return shape.Binary
}
`, []string{"6 binary"}},
		{"a parameter typed engine.Shape", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
func f(s engine.Shape) string { return s.Binary }
`, []string{"3 binary"}},
		{"a pointer parameter typed *engine.Shape", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
func f(s *engine.Shape) string { return s.Binary }
`, []string{"3 binary"}},
		{"a renamed engine import", other, `package other
import eng "github.com/anthnel/devdesk/internal/engine"
func f() string { return eng.Current().Binary }
`, []string{"3 binary"}},
		{"a method, reported under its receiver", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
type T struct{}
func (t *T) run() string { return engine.Current().Binary }
`, []string{"4 binary"}},
		{"a call that names the engine and reads its binary is reported twice, once per rule", other, `package other
import ("os/exec"; "github.com/anthnel/devdesk/internal/engine")
func f() { _ = exec.Command("docker", engine.Current().Binary) }
`, []string{"3 named", "3 binary"}},

		// not a breach
		{"git", other, `package other
import "os/exec"
func f() { _ = exec.Command("git", "status") }
`, nil},
		{"a name that merely starts like an engine", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker-credential-pass", "get") }
`, nil},
		{"LookPath names the engine and starts nothing", other, `package other
import "os/exec"
func f() { _, _ = exec.LookPath("docker") }
`, nil},
		{"the word docker in a message", other, `package other
import "fmt"
func f() error { return fmt.Errorf("docker not found") }
`, nil},
		{"a Binary field of something that is not a Shape", other, `package other
type tool struct{ Binary string }
func f(t tool) string { return t.Binary }
`, nil},
		{"engine.Current().Name is not the binary", other, `package other
import "github.com/anthnel/devdesk/internal/engine"
func f() string { return engine.Current().Name }
`, nil},
		{"a method called Command on another type", other, `package other
import "os/exec"
type cli struct{}
func (cli) Command(string) {}
func f(c cli) { c.Command("docker"); _ = exec.ErrNotFound }
`, nil},
		{"no os/exec import: another package called exec", other, `package other
func f() { _ = exec.Command("docker") }
`, nil},
		{"a test file is not scanned", "internal/docker/x_test.go", `package docker
import "os/exec"
func f() { _ = exec.Command("docker") }
`, nil},

		// the known limits, pinned so that lifting one is a visible change
		{"limit: a name built at run time", other, `package other
import "os/exec"
func f(name string) { _ = exec.Command(name, "ps") }
`, nil},
		{"limit: a name read from a struct", other, `package other
import "os/exec"
type toolCmd struct{ Name string }
func f(tc toolCmd) { _ = exec.Command(tc.Name) }
`, nil},
		{"limit: a function value", other, `package other
import "os/exec"
var start = exec.Command
func f() { _ = start("docker") }
`, nil},
		{"limit: an external binary through a shell", other, `package other
import "os/exec"
func f() { _ = exec.Command("sh", "-c", "docker ps") }
`, nil},
		{"limit: a Shape that crossed a function result", other, `package other
import "os/exec"
type shape struct{ Binary string }
func current() shape { return shape{} }
func f() { _ = exec.Command(current().Binary) }
`, nil},
		{"limit: a constant from another file of the package", other, `package other
import "os/exec"
func f() { _ = exec.Command(engineBinary) }
`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := scanLaunches(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("parsing the case: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%d %s", s.Line, s.Kind))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}

// A source that does not parse is an error, not a silent pass.
func TestLaunchDetectorReportsASourceThatDoesNotParse(t *testing.T) {
	if _, err := scanLaunches("internal/x/x.go", []byte("package x\nfunc (")); err == nil {
		t.Error("scanLaunches accepted a source that does not parse")
	}
}

// The func label an exception is keyed by.
func TestLaunchDetectorLabelsTheEnclosingFunction(t *testing.T) {
	src := `package p
import "github.com/anthnel/devdesk/internal/engine"
type T struct{}
var x = engine.Current().Binary
func top() string { return engine.Current().Binary }
func (T) value() string { return engine.Current().Binary }
func (*T) pointer() string { return engine.Current().Binary }
`
	sites, err := scanLaunches("internal/p/p.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sites {
		got = append(got, s.Func)
	}
	want := []string{"<package level>", "top", "(T).value", "(*T).pointer"}
	if !slices.Equal(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

// ── The judgement on in-memory modules ───────────────────────────────────────

// judgeSources runs the detector and the judgement over a module given as
// sources, the way TestOnlyTheRunnersStartTheEngine does over the repository.
func judgeSources(t *testing.T, files map[string]string, runners []launchRunner, exceptions []launchException) []string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var found []launch
	for _, name := range names {
		sites, err := scanLaunches(name, []byte(files[name]))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found = append(found, sites...)
	}
	return judgeLaunches(found, runners, exceptions)
}

func TestLaunchJudgement(t *testing.T) {
	const dockerCall = "package p\nimport \"os/exec\"\nfunc run() { _ = exec.Command(\"docker\", \"ps\") }\n"
	const kindCall = "package p\nimport \"os/exec\"\nfunc run() { _ = exec.Command(\"kind\", \"get\", \"clusters\") }\n"
	const kubectlCall = "package p\nimport \"os/exec\"\nfunc run() { _ = exec.Command(\"kubectl\", \"get\", \"pods\") }\n"
	const noCall = "package p\nfunc run() {}\n"
	runners := []launchRunner{
		{File: "internal/docker/exec.go", Required: true},
		{File: "internal/kind/runner.go", Required: false},
	}
	exc := func(file, fn string, sites int) launchException {
		return launchException{File: file, Func: fn, Sites: sites, Reason: "because"}
	}

	tests := []struct {
		name       string
		files      map[string]string
		exceptions []launchException
		// wantAll are the fragments each of which some problem must contain;
		// wantNone, when set, requires there to be no problem at all.
		wantAll  []string
		wantNone bool
	}{
		{
			name:     "a runner may start the engine",
			files:    map[string]string{"internal/docker/exec.go": dockerCall},
			wantNone: true,
		},
		{
			// criterion 4: the report cites the file and the line
			name: "a new exec.Command(docker) elsewhere fails with its file and line",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": dockerCall,
			},
			wantAll: []string{"internal/ui/other/open.go:3"},
		},
		{
			// criterion 5: no internal/kind yet, the rules are already on
			name: "kind is refused outside its runner although its package does not exist",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": kindCall,
			},
			wantAll: []string{"internal/ui/other/open.go:3"},
		},
		{
			name: "kubectl is refused outside the runners",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": kubectlCall,
			},
			wantAll: []string{"internal/ui/other/open.go:3"},
		},
		{
			name: "the kind runner may start kind and kubectl once it exists",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/kind/runner.go":   kindCall + strings.SplitN(kubectlCall, "\n", 3)[2],
				"internal/kind/cluster.go":  noCall,
				"internal/ui/other/open.go": noCall,
			},
			wantNone: true,
		},
		{
			name: "the kind package outside runner.go is not a runner",
			files: map[string]string{
				"internal/docker/exec.go":  dockerCall,
				"internal/kind/cluster.go": kindCall,
			},
			wantAll: []string{"internal/kind/cluster.go:3"},
		},
		{
			name: "a named exception lets its function through",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": dockerCall,
			},
			exceptions: []launchException{exc("internal/ui/other/open.go", "run", 1)},
			wantNone:   true,
		},
		{
			name: "an exception covers its function, not the file",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": dockerCall + "func second() { _ = exec.Command(\"kind\") }\n",
			},
			exceptions: []launchException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"internal/ui/other/open.go:4"},
		},
		{
			name: "a second site in an excepted function is not covered by the first one's reason",
			files: map[string]string{
				"internal/docker/exec.go": dockerCall,
				"internal/ui/other/open.go": "package p\nimport \"os/exec\"\n" +
					"func run() { _ = exec.Command(\"docker\", \"ps\"); _ = exec.Command(\"docker\", \"rm\") }\n",
			},
			exceptions: []launchException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"covers 1 site(s) but the function has 2", "internal/ui/other/open.go:3"},
		},
		{
			// criterion 3
			name: "an exception whose code is gone fails and is named",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": noCall,
			},
			exceptions: []launchException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"the exception still exists", "internal/ui/other/open.go run"},
		},
		{
			name: "an exception for a file that is gone fails too",
			files: map[string]string{
				"internal/docker/exec.go": dockerCall,
			},
			exceptions: []launchException{exc("internal/ui/gone/gone.go", "run", 1)},
			wantAll:    []string{"the exception still exists", "internal/ui/gone/gone.go run"},
		},
		{
			name: "an exception without a reason fails",
			files: map[string]string{
				"internal/docker/exec.go":   dockerCall,
				"internal/ui/other/open.go": dockerCall,
			},
			exceptions: []launchException{{File: "internal/ui/other/open.go", Func: "run", Sites: 1, Reason: "  "}},
			wantAll:    []string{"has no reason"},
		},
		{
			name: "an exception for a runner is dead weight",
			files: map[string]string{
				"internal/docker/exec.go": dockerCall,
			},
			exceptions: []launchException{exc("internal/docker/exec.go", "run", 1)},
			wantAll:    []string{"is in a runner"},
		},
		{
			name: "a required runner that carries no site fails",
			files: map[string]string{
				"internal/docker/exec.go": noCall,
			},
			wantAll: []string{"internal/docker/exec.go is a runner but carries no site"},
		},
		{
			name: "an optional runner may be missing",
			files: map[string]string{
				"internal/docker/exec.go": dockerCall,
			},
			wantNone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := judgeSources(t, tt.files, runners, tt.exceptions)
			if tt.wantNone {
				if len(problems) != 0 {
					t.Errorf("problems = %q, want none", problems)
				}
				return
			}
			for _, want := range tt.wantAll {
				if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
					t.Errorf("no problem contains %q; problems = %q", want, problems)
				}
			}
		})
	}
}

// The runners and exceptions of the module are well formed, whatever the
// sources hold: a typo in a path would otherwise read as "no site, no problem".
func TestLaunchRunnersAndExceptionsAreWellFormed(t *testing.T) {
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range launchRunners {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(r.File)))
		if r.Required && err != nil {
			t.Errorf("runner %s is required and cannot be read: %v", r.File, err)
		}
	}
	seen := map[string]bool{}
	for _, e := range launchExceptions {
		key := e.File + " " + e.Func
		if seen[key] {
			t.Errorf("exception %s is listed twice", key)
		}
		seen[key] = true
		if strings.TrimSpace(e.Reason) == "" || e.Sites < 1 {
			t.Errorf("exception %s needs a reason and at least one site", key)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.File))); err != nil {
			t.Errorf("exception %s names a file that cannot be read: %v", key, err)
		}
	}
}
