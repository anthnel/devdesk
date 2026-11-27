package engineguard_test

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
	"sort"
	"strings"
	"sync"
	"testing"
)

// OC-06 (DEVDESK-803), guard G3, requirements S1 and S2: the destructive verbs
// stay in their package.
//
// A static test in the vein of guard G2 (launch_guard_test.go, which it reuses:
// importName, constString, fileConsts, funcLabel, execCallee, namesALaunchBinary,
// launchModuleRoot, launchGuardExcludedDirs, launchGuardMinimum): it reads every
// non-test .go file of the module with go/parser, no type information, and
// reports, with file and line, each place that builds the argv of a destructive
// subcommand outside internal/docker and internal/kind. Inside those two packages
// the other half of the story applies, at run time: every function that builds
// such an argv from an identifier validates it first and builds nothing for a
// malformed one (internal/docker/destructive_ids_test.go).
//
// The destructive subcommands:
//
//	kill, rm, stop, rmi, prune      alone ("docker rm ID")
//	<noun> <verb>                   container, image, volume, network, system,
//	                                builder, pod, machine followed by kill, rm,
//	                                remove, stop or prune ("volume rm", "network rm",
//	                                "system prune"); "system reset" too
//	delete cluster, delete clusters kind
//
// What counts as a site (each is reported once):
//
//	literal   a []string composite literal whose words start with one of those
//	          (or whose first word names docker, podman, kind or kubectl and the
//	          words after it do), once constants are folded.
//	call      a call whose callee is exec.Command or exec.CommandContext (the
//	          words after the binary), or one of the helpers that take an argv
//	          (argvCallees: Build, mutate, mutateContext, prune, dockerOutput,
//	          dockerCombined), with a run of string arguments that starts with one
//	          of those, at any position.
//
// Test files are not scanned: a test that reaches a real engine is guard G1's.
//
// Limits, stated here and not hidden (G2 says the same of itself):
//
//   - An argv assembled word by word (args = append(args, "rm")), a verb read from
//     a variable, a field or a function result, and a constant defined in another
//     file are not seen. Only a literal that spells the verb is.
//   - A verb in a shell line ("sh", "-c", "docker rm x"), a script or a Makefile
//     is not seen, and neither is a function value (start := exec.Command).
//   - The check is on words, not on the program: a []string{"rm", path} that is
//     the argv of /bin/rm is reported too, because nothing says it is not an
//     engine argv. That is the price of not resolving types, and it errs on the
//     side of the guard.
//   - kubectl delete is not a verb of this guard (it is G2's launch, and
//     internal/nexusbench has its own runner and guard); neither are restart,
//     pause, unpause nor `compose down`.
//   - Calling docker.RemoveContainer(id) from another package is not a site:
//     that is the intended way in, and the function validates id. No exported
//     function of internal/docker takes an argv (TestNoExportedDockerFunctionTakesAnArgv),
//     which is what makes "the verbs are only built in the package" enough.
//   - A package home is exempt as a whole, file by file: the guard does not check
//     what it builds, only that nothing else does (the run-time invariant test
//     does).

// destructiveHomes are the directories whose files may build a destructive argv.
var destructiveHomes = []string{"internal/docker", "internal/kind"}

// destructiveAlone are the subcommands that are destructive as the first word.
var destructiveAlone = []string{"kill", "rm", "stop", "rmi", "prune"}

// destructiveNouns are the command groups whose destructive verbs are listed in
// destructiveGroupVerbs.
var destructiveNouns = []string{"container", "image", "volume", "network", "system", "builder", "pod", "machine"}

// destructiveGroupVerbs follow a destructiveNouns word.
var destructiveGroupVerbs = []string{"kill", "rm", "remove", "stop", "prune", "reset"}

// argvCallees are the functions, by name, whose string arguments are an argv.
// They are internal/docker's own, so outside it a hit means a copy of them.
var argvCallees = []string{"Build", "mutate", "mutateContext", "prune", "dockerOutput", "dockerCombined"}

// destructiveMustSee are the verbs internal/docker builds today. The guard fails
// when it stops seeing one in the package: it would be passing because the
// detector went blind, not because nothing is built. kill and kind delete are
// not listed: nothing builds them yet.
var destructiveMustSee = []string{
	"stop", "rm", "rmi", "volume rm", "network rm",
	"container prune", "image prune", "volume prune", "network prune",
}

// destructiveException is a function outside the homes that builds a destructive
// argv, and why it may. Same rules as launchException: keyed by file and
// function, Sites is the exact count, a debt register that shrinks, an entry
// whose code is gone fails. There is none today, and a new one needs a reason a
// reviewer can disagree with.
type destructiveException struct {
	File   string // slash path from the module root
	Func   string // see funcLabel
	Sites  int
	Reason string
}

var destructiveExceptions []destructiveException

// destructiveSite is one place that builds a destructive argv.
type destructiveSite struct {
	File string // slash path from the module root
	Line int
	Func string
	Verb string // "rm", "volume rm", "delete cluster"...
	What string
}

func (s destructiveSite) String() string {
	return fmt.Sprintf("%s:%d: %s (in %s)", s.File, s.Line, s.What, s.Func)
}

// ── Detector ─────────────────────────────────────────────────────────────────

func contains(list []string, s string) bool { return slices.Contains(list, s) }

// destructiveVerbAt returns the destructive subcommand that words starts with,
// "" when it does not start with one.
func destructiveVerbAt(words []string) string {
	switch {
	case len(words) == 0:
		return ""
	case contains(destructiveAlone, words[0]):
		return words[0]
	case len(words) >= 2 && contains(destructiveNouns, words[0]) && contains(destructiveGroupVerbs, words[1]):
		return words[0] + " " + words[1]
	case len(words) >= 2 && words[0] == "delete" && (words[1] == "cluster" || words[1] == "clusters"):
		return "delete " + words[1]
	}
	return ""
}

// constWords folds the leading run of constant string expressions of exprs.
func constWords(exprs []ast.Expr, consts map[string]string) []string {
	var out []string
	for _, e := range exprs {
		s, ok := constString(e, consts, nil)
		if !ok {
			break
		}
		out = append(out, s)
	}
	return out
}

// isStringSlice reports whether t is the type []string.
func isStringSlice(t ast.Expr) bool {
	at, ok := t.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	id, ok := at.Elt.(*ast.Ident)
	return ok && id.Name == "string"
}

// calleeName is the name a call goes by: "Command" for exec.Command, "mutate"
// for mutate(...).
func calleeName(fun ast.Expr) string {
	switch fun := fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.Ident:
		return fun.Name
	}
	return ""
}

// scanDestructive parses src as the file name (a slash path from the module root)
// and returns its sites ordered by line. A _test.go file has none.
func scanDestructive(name string, src []byte) ([]destructiveSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, "_test.go") {
		return nil, nil
	}

	execName := importName(f, "os/exec", "exec")
	consts := fileConsts(f, nil)

	var out []destructiveSite
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		add := func(pos token.Pos, verb, what string) {
			out = append(out, destructiveSite{File: name, Line: fset.Position(pos).Line, Func: label, Verb: verb, What: what})
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if !isStringSlice(n.Type) {
					return true
				}
				words := constWords(n.Elts, consts)
				if verb := destructiveVerbAt(words); verb != "" {
					add(n.Pos(), verb, fmt.Sprintf("argv literal %q", words))
				} else if len(words) > 1 && namesALaunchBinary(words[0]) {
					if verb := destructiveVerbAt(words[1:]); verb != "" {
						add(n.Pos(), verb, fmt.Sprintf("argv literal %q", words))
					}
				}
			case *ast.CallExpr:
				fn, binIdx := execCallee(n.Fun, execName)
				callee := calleeName(n.Fun)
				var starts []int
				switch {
				case fn != "":
					starts = []int{binIdx + 1}
				case contains(argvCallees, callee):
					for i := range n.Args {
						starts = append(starts, i)
					}
				}
				for _, i := range starts {
					if i >= len(n.Args) {
						continue
					}
					words := constWords(n.Args[i:], consts)
					if verb := destructiveVerbAt(words); verb != "" {
						add(n.Pos(), verb, fmt.Sprintf("%s(... %q ...)", callee, words))
						break
					}
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// ── Judgement ────────────────────────────────────────────────────────────────

func inDestructiveHome(file string) bool { return contains(destructiveHomes, path.Dir(file)) }

// judgeDestructive turns the sites found into the problems to report: a site
// outside the homes and without an exception, an exception whose count is not the
// number of sites, an exception whose code is gone or that is not well formed,
// and a verb that internal/docker is known to build and that the detector no
// longer sees there.
func judgeDestructive(found []destructiveSite, exceptions []destructiveException, mustSee []string) []string {
	var problems []string
	seenInHome := map[string]bool{}
	byFunc := map[string][]destructiveSite{}
	for _, s := range found {
		if inDestructiveHome(s.File) {
			if path.Dir(s.File) == "internal/docker" {
				seenInHome[s.Verb] = true
			}
			continue
		}
		byFunc[s.File+"\x00"+s.Func] = append(byFunc[s.File+"\x00"+s.Func], s)
	}

	for _, verb := range mustSee {
		if !seenInHome[verb] {
			problems = append(problems, fmt.Sprintf(
				"internal/docker builds %q today but the guard no longer sees it there: the detector went blind "+
					"or the verb moved; update destructiveMustSee (OC-06, DEVDESK-803)", verb))
		}
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := byFunc[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why it may build a destructive argv", e.File, e.Func))
		case inDestructiveHome(e.File):
			problems = append(problems, fmt.Sprintf("exception %s %s is in internal/docker or internal/kind, which need none: delete it", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s has no site left; delete it from destructiveExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf(
				"exception %s %s covers %d site(s) but the function has %d: %s",
				e.File, e.Func, e.Sites, len(sites), joinDestructive(sites)))
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
		for _, s := range byFunc[k] {
			problems = append(problems, fmt.Sprintf(
				"%s: builds the argv of a destructive subcommand (%s) outside internal/docker and internal/kind; "+
					"call the validating function of internal/docker instead (OC-06, DEVDESK-803, S1/S2)", s, s.Verb))
		}
	}
	return problems
}

func joinDestructive(sites []destructiveSite) string {
	parts := make([]string, len(sites))
	for i, s := range sites {
		parts[i] = fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return strings.Join(parts, ", ")
}

// ── The module ───────────────────────────────────────────────────────────────

// scanDestructiveTree scans every .go file under root with scan and returns the
// results and the number of files read. The walk is the one of G2.
func scanDestructiveTree[T any](root string, scan func(name string, src []byte) ([]T, error)) ([]T, int, error) {
	var all []T
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
		found, err := scan(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, found...)
		return nil
	})
	return all, files, err
}

var (
	destructiveScanOnce  sync.Once
	destructiveScanSites []destructiveSite
	destructiveScanFiles int
	destructiveScanErr   error
)

// scanDestructiveRepo scans the module that holds this package, once per test
// process.
func scanDestructiveRepo(t *testing.T) []destructiveSite {
	t.Helper()
	destructiveScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			destructiveScanErr = err
			return
		}
		destructiveScanSites, destructiveScanFiles, destructiveScanErr = scanDestructiveTree(root, scanDestructive)
	})
	if destructiveScanErr != nil {
		t.Fatalf("scanning the module for destructive verbs: %v", destructiveScanErr)
	}
	if destructiveScanFiles < launchGuardMinimum {
		t.Fatalf("the destructive verbs guard read %d files, want the whole module (at least %d)", destructiveScanFiles, launchGuardMinimum)
	}
	return destructiveScanSites
}

// Criterion 1 on the real module: the destructive subcommands are built only in
// internal/docker and internal/kind, and an exception whose code is gone fails.
func TestDestructiveVerbsStayInTheirPackage(t *testing.T) {
	for _, p := range judgeDestructive(scanDestructiveRepo(t), destructiveExceptions, destructiveMustSee) {
		t.Error(p)
	}
}

// ── Closing the other door ───────────────────────────────────────────────────

// exportedArgvTakers returns the exported functions of the package files (name to
// source) that take a variadic string parameter, the shape of an argv. Methods of
// an unexported type are not reachable from another package, so they are not
// counted.
func exportedArgvTakers(files map[string][]byte) ([]string, error) {
	var out []string
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, files[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Type.Params == nil {
				continue
			}
			if fn.Recv != nil && !exportedReceiver(fn.Recv) {
				continue
			}
			for _, field := range fn.Type.Params.List {
				el, ok := field.Type.(*ast.Ellipsis)
				if !ok {
					continue
				}
				if id, ok := el.Elt.(*ast.Ident); ok && id.Name == "string" {
					out = append(out, funcLabel(decl))
				}
			}
		}
	}
	return out, nil
}

func exportedReceiver(recv *ast.FieldList) bool {
	if len(recv.List) != 1 {
		return false
	}
	typ := recv.List[0].Type
	if s, ok := typ.(*ast.StarExpr); ok {
		typ = s.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && id.IsExported()
}

// Criterion 1, the other half: no function of internal/docker that another
// package can call takes an argv. Without that, "built only in the package" would
// not be enough, because a caller elsewhere could hand it {"rm", x}.
func TestNoExportedDockerFunctionTakesAnArgv(t *testing.T) {
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "docker")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files["internal/docker/"+e.Name()] = src
	}
	if len(files) < 10 {
		t.Fatalf("read %d files of internal/docker, want the package", len(files))
	}
	got, err := exportedArgvTakers(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range got {
		t.Errorf("internal/docker exports %s, which takes a variadic string (an argv): a caller elsewhere could build a destructive command through it", fn)
	}
}

// ── The detector on sources ──────────────────────────────────────────────────
//
// Criterion 4: the guard is only worth something if it sees a violation. Each
// rule is run on sources that break it and on sources that look like a breach and
// are not.

type destructiveCase struct {
	name string
	file string
	src  string
	want []string // "line verb"
}

func TestDestructiveDetectorOnSources(t *testing.T) {
	const other = "internal/ui/other/x.go"
	tests := []destructiveCase{
		// criterion 4: a destructive call added elsewhere
		{"exec.Command docker rm", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("docker", "rm", id) }
`, []string{"3 rm"}},
		{"exec.Command podman stop", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("podman", "stop", id) }
`, []string{"3 stop"}},
		{"exec.Command docker kill", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("docker", "kill", id) }
`, []string{"3 kill"}},
		{"exec.Command docker rmi", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("docker", "rmi", "-f", id) }
`, []string{"3 rmi"}},
		{"exec.Command docker prune", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "system", "prune", "-f") }
`, []string{"3 system prune"}},
		{"volume rm", other, `package other
import "os/exec"
func f(n string) { _ = exec.Command("docker", "volume", "rm", n) }
`, []string{"3 volume rm"}},
		{"network rm", other, `package other
import "os/exec"
func f(n string) { _ = exec.Command("docker", "network", "rm", n) }
`, []string{"3 network rm"}},
		{"container rm and its alias remove", other, `package other
import "os/exec"
func f(n string) {
	_ = exec.Command("docker", "container", "rm", n)
	_ = exec.Command("docker", "container", "remove", n)
}
`, []string{"4 container rm", "5 container remove"}},
		{"image prune", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "image", "prune", "-f") }
`, []string{"3 image prune"}},
		{"kind delete cluster", other, `package other
import "os/exec"
func f(n string) { _ = exec.Command("kind", "delete", "cluster", "--name", n) }
`, []string{"3 delete cluster"}},
		{"kind delete clusters", other, `package other
import "os/exec"
func f() { _ = exec.Command("kind", "delete", "clusters", "--all") }
`, []string{"3 delete clusters"}},
		{"CommandContext", other, `package other
import ("context"; "os/exec")
func f(ctx context.Context, id string) { _ = exec.CommandContext(ctx, "docker", "rm", id) }
`, []string{"3 rm"}},
		{"a binary built at run time still has its words read", other, `package other
import "os/exec"
func f(bin, id string) { _ = exec.Command(bin, "rm", "-f", id) }
`, []string{"3 rm"}},
		{"the same call through a renamed os/exec", other, `package other
import run "os/exec"
func f(id string) { _ = run.Command("docker", "stop", id) }
`, []string{"3 stop"}},

		// an argv literal, however it is run afterwards
		{"an argv literal", other, `package other
func f(id string) []string { return []string{"rm", "-f", id} }
`, []string{"2 rm"}},
		{"an argv literal that starts with the binary", other, `package other
func f(id string) []string { return []string{"docker", "rmi", id} }
`, []string{"2 rmi"}},
		{"a grouped argv literal", other, `package other
var argv = []string{"volume", "rm", "pgdata"}
`, []string{"2 volume rm"}},
		{"a verb held in a file-level constant", other, `package other
const verb = "prune"
func f() []string { return []string{verb, "-f"} }
`, []string{"3 prune"}},
		{"a concatenated verb", other, `package other
func f() []string { return []string{"r" + "m", "x"} }
`, []string{"2 rm"}},

		// the helpers of internal/docker, copied elsewhere
		{"a mutate-like helper, verb after the label", other, `package other
func f(id string) error { return mutate("docker stop", "stop", id) }
`, []string{"2 stop"}},
		{"runner.Build", other, `package other
func f(id string) { _ = runner.Build("kill", id) }
`, []string{"2 kill"}},
		{"dockerOutput", other, `package other
func f(n string) { _, _ = dockerOutput("network", "rm", n) }
`, []string{"2 network rm"}},
		{"a prune helper", other, `package other
func f() { _, _ = prune("docker volume prune", "volume", "prune", "-f") }
`, []string{"2 volume prune"}},
		{"two sites are two reports, in line order", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "stop", "a") }
func g() { _ = exec.Command("docker", "rm", "b") }
`, []string{"3 stop", "4 rm"}},

		// not a breach
		{"a read-only subcommand", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "ps", "-a"); _ = exec.Command("docker", "volume", "ls") }
`, nil},
		{"restart, pause and create are not destructive verbs of this guard", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("docker", "restart", id); _ = exec.Command("docker", "network", "create", id) }
`, nil},
		{"kubectl delete is another guard's", other, `package other
import "os/exec"
func f() { _ = exec.Command("kubectl", "delete", "namespace", "bench") }
`, nil},
		{"the verb is a bare word in a message", other, `package other
import "fmt"
func f() error { return fmt.Errorf("rm failed") }
`, nil},
		{"a verb that is not the first word of an unknown call", other, `package other
func f(s string) bool { return strings.HasPrefix(s, "rm") || strings.Contains("stop", s) }
`, nil},
		{"a noun alone", other, `package other
func f() []string { return []string{"volume", "ls"} }
`, nil},
		{"a key binding named like a verb", other, `package other
var keys = map[string]bool{"stop": true, "kill": true}
`, nil},
		{"the binary position is not a verb", other, `package other
import "os/exec"
func f() { _ = exec.Command("rm") }
`, nil},
		{"a test file is not scanned", "internal/ui/other/x_test.go", `package other
import "os/exec"
func f() { _ = exec.Command("docker", "rm", "x") }
`, nil},
		{"internal/docker is read like any file: judgement is not the detector's", "internal/docker/containers.go", `package docker
func StopContainer(id string) error { return mutate("docker stop", "stop", id) }
`, []string{"2 stop"}},

		// the known limits, pinned so that lifting one is a visible change
		{"limit: an argv assembled word by word", other, `package other
func f(id string) []string { args := []string{"docker"}; args = append(args, "rm", id); return args }
`, nil},
		{"limit: a verb read from a variable", other, `package other
import "os/exec"
func f(verb, id string) { _ = exec.Command("docker", verb, id) }
`, nil},
		{"limit: a shell line", other, `package other
import "os/exec"
func f(id string) { _ = exec.Command("sh", "-c", "docker rm "+id) }
`, nil},
		{"limit: a function value", other, `package other
import "os/exec"
var start = exec.Command
func f(id string) { _ = start("docker", "rm", id) }
`, nil},
		{"limit: a constant from another file", other, `package other
func f(id string) []string { return []string{verbRemove, id} }
`, nil},
		{"limit: an argv of /bin/rm is reported (words, not programs)", other, `package other
func f(p string) []string { return []string{"rm", "-rf", p} }
`, []string{"2 rm"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := scanDestructive(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("parsing the case: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%d %s", s.Line, s.Verb))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}

// A source that does not parse is an error, not a silent pass.
func TestDestructiveDetectorReportsASourceThatDoesNotParse(t *testing.T) {
	if _, err := scanDestructive("internal/x/x.go", []byte("package x\nfunc (")); err == nil {
		t.Error("scanDestructive accepted a source that does not parse")
	}
}

// The func label an exception is keyed by is the one of G2.
func TestDestructiveDetectorLabelsTheEnclosingFunction(t *testing.T) {
	src := `package p
type T struct{}
var x = []string{"rm", "a"}
func top() []string { return []string{"stop", "a"} }
func (T) value() []string { return []string{"kill", "a"} }
func (*T) pointer() []string { return []string{"rmi", "a"} }
`
	sites, err := scanDestructive("internal/p/p.go", []byte(src))
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

// judgeDestructiveSources runs the detector and the judgement over a module given
// as sources, the way TestDestructiveVerbsStayInTheirPackage does over the
// repository.
func judgeDestructiveSources(t *testing.T, files map[string]string, exceptions []destructiveException, mustSee []string) []string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var found []destructiveSite
	for _, name := range names {
		sites, err := scanDestructive(name, []byte(files[name]))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found = append(found, sites...)
	}
	return judgeDestructive(found, exceptions, mustSee)
}

func TestDestructiveJudgement(t *testing.T) {
	const stopCall = "package p\nimport \"os/exec\"\nfunc run(id string) { _ = exec.Command(\"docker\", \"stop\", id) }\n"
	const rmCall = "package p\nimport \"os/exec\"\nfunc run(id string) { _ = exec.Command(\"docker\", \"rm\", id) }\n"
	const kindDelete = "package p\nimport \"os/exec\"\nfunc run(n string) { _ = exec.Command(\"kind\", \"delete\", \"cluster\", \"--name\", n) }\n"
	const noCall = "package p\nfunc run() {}\n"
	exc := func(file, fn string, sites int) destructiveException {
		return destructiveException{File: file, Func: fn, Sites: sites, Reason: "because"}
	}

	tests := []struct {
		name       string
		files      map[string]string
		exceptions []destructiveException
		mustSee    []string
		// wantAll are the fragments each of which some problem must contain;
		// wantNone, when set, requires there to be no problem at all.
		wantAll  []string
		wantNone bool
	}{
		{
			name:     "internal/docker may build them",
			files:    map[string]string{"internal/docker/containers.go": stopCall},
			wantNone: true,
		},
		{
			name:     "internal/kind may build them, in any of its files",
			files:    map[string]string{"internal/kind/cluster.go": kindDelete, "internal/docker/containers.go": stopCall},
			wantNone: true,
		},
		{
			// criterion 4: the report cites file and line
			name: "a destructive call added elsewhere fails with its file and line",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     rmCall,
			},
			wantAll: []string{"internal/ui/other/open.go:3", "rm"},
		},
		{
			name: "kind delete is refused outside internal/kind although the package does not exist",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     kindDelete,
			},
			wantAll: []string{"internal/ui/other/open.go:3", "delete cluster"},
		},
		{
			name: "a subpackage of internal/docker is not internal/docker",
			files: map[string]string{
				"internal/docker/containers.go":   stopCall,
				"internal/docker/engine/other.go": rmCall,
			},
			wantAll: []string{"internal/docker/engine/other.go:3"},
		},
		{
			name: "a sibling package with a similar name is not a home",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/dockerish/x.go":       rmCall,
			},
			wantAll: []string{"internal/dockerish/x.go:3"},
		},
		{
			name: "a named exception lets its function through",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     rmCall,
			},
			exceptions: []destructiveException{exc("internal/ui/other/open.go", "run", 1)},
			wantNone:   true,
		},
		{
			name: "an exception covers its function, not the file",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     rmCall + "func second() { _ = exec.Command(\"docker\", \"kill\", \"x\") }\n",
			},
			exceptions: []destructiveException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"internal/ui/other/open.go:4"},
		},
		{
			name: "a second site in an excepted function is not covered by the first one's reason",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go": "package p\nimport \"os/exec\"\n" +
					"func run() { _ = exec.Command(\"docker\", \"rm\", \"a\"); _ = exec.Command(\"docker\", \"stop\", \"b\") }\n",
			},
			exceptions: []destructiveException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"covers 1 site(s) but the function has 2", "internal/ui/other/open.go:3"},
		},
		{
			name: "an exception whose code is gone fails and is named",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     noCall,
			},
			exceptions: []destructiveException{exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"the exception still exists", "internal/ui/other/open.go run"},
		},
		{
			name:       "an exception for a file that is gone fails too",
			files:      map[string]string{"internal/docker/containers.go": stopCall},
			exceptions: []destructiveException{exc("internal/ui/gone/gone.go", "run", 1)},
			wantAll:    []string{"the exception still exists", "internal/ui/gone/gone.go run"},
		},
		{
			name: "an exception without a reason fails",
			files: map[string]string{
				"internal/docker/containers.go": stopCall,
				"internal/ui/other/open.go":     rmCall,
			},
			exceptions: []destructiveException{{File: "internal/ui/other/open.go", Func: "run", Sites: 1, Reason: "  "}},
			wantAll:    []string{"has no reason"},
		},
		{
			name:       "an exception for a home is dead weight",
			files:      map[string]string{"internal/docker/containers.go": stopCall},
			exceptions: []destructiveException{exc("internal/docker/containers.go", "run", 1)},
			wantAll:    []string{"need none"},
		},
		{
			name:    "a verb internal/docker is known to build but the guard no longer sees fails",
			files:   map[string]string{"internal/docker/containers.go": noCall},
			mustSee: []string{"stop"},
			wantAll: []string{`internal/docker builds "stop" today`},
		},
		{
			name:     "a must-see verb seen in internal/docker passes",
			files:    map[string]string{"internal/docker/containers.go": stopCall},
			mustSee:  []string{"stop"},
			wantNone: true,
		},
		{
			name:    "a must-see verb seen only in internal/kind does not count for internal/docker",
			files:   map[string]string{"internal/kind/cluster.go": "package p\nfunc run() []string { return []string{\"stop\", \"x\"} }\n"},
			mustSee: []string{"stop"},
			wantAll: []string{`internal/docker builds "stop" today`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := judgeDestructiveSources(t, tt.files, tt.exceptions, tt.mustSee)
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

// The exported-argv check on sources.
func TestExportedArgvTakers(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"an exported function with a variadic string", "package docker\nfunc Run(args ...string) {}\n", []string{"Run"}},
		{"an unexported helper", "package docker\nfunc run(args ...string) {}\n", nil},
		{"a method of an unexported type", "package docker\ntype cli struct{}\nfunc (cli) Build(args ...string) {}\n", nil},
		{"a method of an exported type", "package docker\ntype Cli struct{}\nfunc (*Cli) Build(args ...string) {}\n", []string{"(*Cli).Build"}},
		{"a slice of IDs is not an argv", "package docker\nfunc Digests(ids []string) {}\n", nil},
		{"a variadic of another type", "package docker\nfunc Pick(n ...int) {}\n", nil},
		{"a test file is not read", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"internal/docker/x.go": []byte(tt.src)}
			if tt.src == "" {
				files = map[string][]byte{"internal/docker/x_test.go": []byte("package docker\nfunc Run(args ...string) {}\n")}
			}
			got, err := exportedArgvTakers(files)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("exportedArgvTakers = %v, want %v", got, tt.want)
			}
		})
	}
}

// The module's exceptions are well formed, whatever the sources hold: a typo in a
// path would otherwise read as "no site, no problem".
func TestDestructiveExceptionsAreWellFormed(t *testing.T) {
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range destructiveExceptions {
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
	for _, home := range destructiveHomes {
		if !strings.HasPrefix(home, "internal/") || strings.HasSuffix(home, "/") {
			t.Errorf("home %q must be a slash path from the module root, without a trailing slash", home)
		}
	}
}
