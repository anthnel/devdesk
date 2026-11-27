package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// OC-10 (DEVDESK-808), guard G8, requirement S6: every pull goes through
// imagepull.Pull, so no path bypasses the signature check or trust.yaml.
//
// A static test in the vein of guards G2 and G3 (launch_guard_test.go and
// destructive_guard_test.go, whose helpers it reuses: importName, constString,
// fileConsts, funcLabel, execCallee, constWords, calleeName, isStringSlice,
// argvCallees, scanDestructiveTree, launchModuleRoot, launchGuardExcludedDirs,
// launchGuardMinimum): it reads every non-test .go file of the module with
// go/parser, no type information, and reports, with file and line, each place that
// either builds the argv of a pull or reaches the engine's pull function without
// going through internal/imagepull.
//
// imagepull builds no argv. It is handed internal/docker's PullImageContext as a
// function value (imagepull.Default, Deps.Pull), and that function is the only
// place that spells the verb. So the guard has two halves, and G8 holds only when
// both do:
//
//	argv       the verb is built nowhere but in docker.PullImageContext;
//	reference  docker.PullImageContext and docker.PullImage are named nowhere but
//	           in internal/imagepull (and the named exceptions below).
//
// What counts as a site (each is reported once):
//
//	argv literal   a []string composite literal in which any element folds to
//	               "pull" (constants are folded), so "image pull", "compose pull"
//	               and a global option before the verb are all seen, and so is a
//	               literal that starts with a variable (bin, "pull", ref).
//	argv call      a call whose callee is exec.Command or exec.CommandContext (the
//	               arguments after the binary), or one of the helpers that take an
//	               argv (argvCallees: Build, mutate, mutateContext, prune,
//	               dockerOutput, dockerCombined), with any argument that folds to
//	               "pull".
//	pull flag      the same places, for a word "--pull" or "--pull=<value>" other
//	               than never and false: `run --pull=always`, `build --pull`,
//	               `compose up --pull always`. "--pull=never" is the opposite of a
//	               pull and is how a launch stays out of this guard.
//	reference      <docker>.PullImage or <docker>.PullImageContext, where <docker>
//	               is the name internal/docker is imported under, called or taken as
//	               a value; and, inside internal/docker, a call to PullImage or
//	               PullImageContext by its bare name.
//
// internal/imagepull is the home of the reference half only. It is not exempt from
// the argv half: it has no reason to build one, and the day it does, that is an
// exception with a reason, not a silent pass.
//
// Test files are not scanned: a test that reaches a real engine is guard G1's, and
// a test that stubs the pull is not a path. The run-time half of the story, that a
// launch never pulls, is held by internal/docker/launch_pull_test.go.
//
// Limits, stated here and not hidden (G2 and G3 say the same of themselves):
//
//   - An argv assembled word by word (args = append(args, "pull")), a verb read
//     from a variable, a field or a function result, and a constant defined in
//     another file are not seen. Only a literal that spells the verb is.
//   - A verb in a shell line ("sh", "-c", "docker pull x"), a script, a Makefile
//     or the CI workflow is not seen, and neither is a function value
//     (start := exec.Command). A dot import of internal/docker is not seen.
//   - The check is on words, not on the program: a []string{"git", "pull"} would
//     be reported too, because nothing says it is not an engine argv. The real
//     module has none today, and an exception is the way out if one appears.
//   - Inside internal/docker only a call to PullImage or PullImageContext is a
//     reference; a function value taken there is not seen.
//   - An implicit pull is not a verb: `run`, `create`, `build` and `compose up`
//     fetch a missing image on their own unless told not to. A launch is held by
//     --pull=never at run time (OC-10, criterion 3). The scanners' own
//     `run --rm` (internal/scan/container_run.go) pulls DevDesk's tool images
//     without a signature check: a declared exception of the verified pull
//     (docs/architecture/network.md), and no part of this guard.
//   - imagepull.Pull itself, with scan.image_verification off, pulls the tag as it
//     always did: that is the user's setting, not a path around it.
//   - What kind pulls (OC-71) is out of scope here, as the story says, and so is
//     internal/nexusbench/skopeo.go, which runs `skopeo copy` from registry to
//     registry (a development tool, no engine involved).
//   - The two-word form "--pull", "never" is reported (the bare "--pull" is a
//     pull word, and the guard does not read the next word): a false positive,
//     accepted. A launch spells it --pull=never.

const dockerPackage = "github.com/anthnel/devdesk/internal/docker"

const (
	pullKindArgv = "argv"
	pullKindRef  = "reference"
)

// pullHomes are the directories that may name the engine's pull function. They
// are not exempt from the argv half.
var pullHomes = []string{"internal/imagepull"}

// pullFuncs are the exported functions of internal/docker that pull.
var pullFuncs = []string{"PullImage", "PullImageContext"}

// pullSafeFlagValues are the values of --pull that do not pull.
var pullSafeFlagValues = []string{"never", "false"}

// pullMustSee are the sites the real module has today. The guard fails when it
// stops seeing one: it would be passing because the detector went blind, not
// because nothing pulls.
var pullMustSee = []pullSite{
	{File: "internal/docker/images.go", Kind: pullKindArgv},
	{File: "internal/imagepull/default.go", Kind: pullKindRef},
}

// pullException is a function outside the homes that builds a pull argv or
// names the engine's pull function, and why it may. Same rules as launchException
// and destructiveException: keyed by file and function, Sites is the exact count,
// a debt register that shrinks, an entry whose code is gone fails the test. A new
// one needs a reason a reviewer can disagree with.
type pullException struct {
	File   string // slash path from the module root
	Func   string // see funcLabel
	Sites  int
	Reason string
}

var pullExceptions = []pullException{
	{
		File:  "internal/docker/images.go",
		Func:  "PullImageContext",
		Sites: 1,
		Reason: "permanent: the one place that spells the verb. imagepull.Default wires it into Deps.Pull, and the " +
			"reference rule of this guard keeps every other package from naming it.",
	},
	{
		File:  "internal/docker/images.go",
		Func:  "PullImage",
		Sites: 1,
		Reason: "permanent: the context-free wrapper of PullImageContext. It has no caller in production code, so it is not " +
			"a path; delete it, and this entry, the day nothing needs it.",
	},
	{
		File:  "internal/ui/oci_resources/image_update.go",
		Func:  "<package level>",
		Sites: 1,
		Reason: "permanent: the test seam `pullImageContext = docker.PullImageContext`. It only feeds imagepull.Deps " +
			"(Model.pullDeps); TestNoPullBypassesTheSignatureCheck (pull_verify_test.go) holds that nothing in the package " +
			"calls it directly.",
	},
}

// pullSite is one place that builds a pull argv or names the pull function.
type pullSite struct {
	File string // slash path from the module root
	Line int
	Func string
	Kind string // pullKindArgv or pullKindRef
	Word string // "pull", "--pull=always", "docker.PullImageContext"
	What string
}

func (s pullSite) String() string {
	return fmt.Sprintf("%s:%d: %s (in %s)", s.File, s.Line, s.What, s.Func)
}

// ── Detector ─────────────────────────────────────────────────────────────────

// pullWord returns what w says about a pull: "pull" for the verb, the flag itself
// for --pull / --pull=<anything but never or false>, "" otherwise.
func pullWord(w string) string {
	switch {
	case w == "pull", w == "--pull":
		return w
	case strings.HasPrefix(w, "--pull="):
		if slices.Contains(pullSafeFlagValues, strings.TrimPrefix(w, "--pull=")) {
			return ""
		}
		return w
	}
	return ""
}

// firstPullWord folds each of exprs and returns the first that is a pull word.
func firstPullWord(exprs []ast.Expr, consts map[string]string) string {
	for _, e := range exprs {
		if s, ok := constString(e, consts, nil); ok {
			if w := pullWord(s); w != "" {
				return w
			}
		}
	}
	return ""
}

// scanPulls parses src as the file name (a slash path from the module root) and
// returns its sites ordered by line. A _test.go file has none.
func scanPulls(name string, src []byte) ([]pullSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, "_test.go") {
		return nil, nil
	}

	execName := importName(f, "os/exec", "exec")
	dockerName := importName(f, dockerPackage, "docker")
	inDocker := path.Dir(name) == "internal/docker"
	consts := fileConsts(f, nil)

	var out []pullSite
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		add := func(pos token.Pos, kind, word, what string) {
			out = append(out, pullSite{File: name, Line: fset.Position(pos).Line, Func: label, Kind: kind, Word: word, What: what})
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if !isStringSlice(n.Type) {
					return true
				}
				if w := firstPullWord(n.Elts, consts); w != "" {
					add(n.Pos(), pullKindArgv, w, fmt.Sprintf("argv literal with %q", w))
				}
			case *ast.CallExpr:
				fn, binIdx := execCallee(n.Fun, execName)
				callee := calleeName(n.Fun)
				start := -1
				switch {
				case fn != "":
					start = binIdx + 1
				case contains(argvCallees, callee):
					start = 0
				}
				if start >= 0 && start < len(n.Args) {
					if w := firstPullWord(n.Args[start:], consts); w != "" {
						add(n.Pos(), pullKindArgv, w, fmt.Sprintf("%s(... %q ...)", callee, w))
					}
				}
				if id, ok := n.Fun.(*ast.Ident); ok && inDocker && contains(pullFuncs, id.Name) {
					add(n.Pos(), pullKindRef, id.Name, fmt.Sprintf("call to %s inside internal/docker", id.Name))
				}
			case *ast.SelectorExpr:
				x, ok := n.X.(*ast.Ident)
				if ok && dockerName != "" && dockerName != "." && x.Name == dockerName && contains(pullFuncs, n.Sel.Name) {
					word := dockerName + "." + n.Sel.Name
					add(n.Pos(), pullKindRef, word, "reference to "+word)
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// ── Judgement ────────────────────────────────────────────────────────────────

func atPullHome(s pullSite) bool {
	return s.Kind == pullKindRef && slices.Contains(pullHomes, path.Dir(s.File))
}

// judgePulls turns the sites found into the problems to report: a site outside
// the homes and without an exception, an exception whose count is not the number
// of sites, an exception whose code is gone or that is not well formed, and a
// site the real module is known to have and that the detector no longer sees.
func judgePulls(found []pullSite, exceptions []pullException, mustSee []pullSite) []string {
	var problems []string
	seen := map[string]bool{}
	byFunc := map[string][]pullSite{}
	for _, s := range found {
		seen[s.File+"\x00"+s.Kind] = true
		if atPullHome(s) {
			continue
		}
		byFunc[s.File+"\x00"+s.Func] = append(byFunc[s.File+"\x00"+s.Func], s)
	}

	for _, m := range mustSee {
		if !seen[m.File+"\x00"+m.Kind] {
			problems = append(problems, fmt.Sprintf(
				"%s has a pull %s today but the guard no longer sees it there: the detector went blind "+
					"or the code moved; update pullMustSee (OC-10, DEVDESK-808)", m.File, m.Kind))
		}
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := byFunc[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why it may pull outside imagepull.Pull", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s has no site left; delete it from pullExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf(
				"exception %s %s covers %d site(s) but the function has %d: %s",
				e.File, e.Func, e.Sites, len(sites), joinPulls(sites)))
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
			switch s.Kind {
			case pullKindArgv:
				problems = append(problems, fmt.Sprintf(
					"%s: builds a pull argv (%s) outside docker.PullImageContext; every pull goes through imagepull.Pull, "+
						"so the signature check and trust.yaml cannot be bypassed (OC-10, DEVDESK-808, S6)", s, s.Word))
			default:
				problems = append(problems, fmt.Sprintf(
					"%s: names %s outside internal/imagepull; pull through imagepull.Pull, which verifies first "+
						"(OC-10, DEVDESK-808, S6)", s, s.Word))
			}
		}
	}
	return problems
}

func joinPulls(sites []pullSite) string {
	parts := make([]string, len(sites))
	for i, s := range sites {
		parts[i] = fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return strings.Join(parts, ", ")
}

// ── The module ───────────────────────────────────────────────────────────────

var (
	pullScanOnce  sync.Once
	pullScanSites []pullSite
	pullScanFiles int
	pullScanErr   error
)

// scanPullRepo scans the module that holds this package, once per test process.
func scanPullRepo(t *testing.T) []pullSite {
	t.Helper()
	pullScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			pullScanErr = err
			return
		}
		pullScanSites, pullScanFiles, pullScanErr = scanDestructiveTree(root, scanPulls)
	})
	if pullScanErr != nil {
		t.Fatalf("scanning the module for pulls: %v", pullScanErr)
	}
	if pullScanFiles < launchGuardMinimum {
		t.Fatalf("the pull guard read %d files, want the whole module (at least %d)", pullScanFiles, launchGuardMinimum)
	}
	return pullScanSites
}

// Criteria 1 on the real module: no pull argv outside docker.PullImageContext, no
// reference to it outside internal/imagepull and the named exceptions, and an
// exception whose code is gone fails.
func TestEveryPullGoesThroughImagepull(t *testing.T) {
	for _, p := range judgePulls(scanPullRepo(t), pullExceptions, pullMustSee) {
		t.Error(p)
	}
}

// Criterion 2 on the real module: a pull added anywhere else, here as a source
// held in memory next to the module's own sites, makes the guard fail and names
// where. Without it the module is clean, so the failure is the source's.
func TestAPullAddedElsewhereFailsTheGuardOnTheRealModule(t *testing.T) {
	module := scanPullRepo(t)
	if got := judgePulls(module, pullExceptions, pullMustSee); len(got) != 0 {
		t.Skipf("the module itself is not clean, TestEveryPullGoesThroughImagepull says why: %v", got)
	}
	added := map[string]string{
		"internal/ui/containers/pull.go": `package containers
import "os/exec"
func pull(ref string) error { return exec.Command("docker", "pull", ref).Run() }
`,
		"internal/mcp/pull.go": `package mcp
import "github.com/anthnel/devdesk/internal/docker"
func pull(ref string) error { return docker.PullImageContext(nil, ref) }
`,
		"internal/docker/more.go": `package docker
import "context"
func Refresh(ref string) error { return mutate("docker pull", "image", "pull", ref) }
`,
	}
	for file, src := range added {
		t.Run(file, func(t *testing.T) {
			sites, err := scanPulls(file, []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) == 0 {
				t.Fatal("the detector saw nothing in the added source")
			}
			got := judgePulls(append(slices.Clone(module), sites...), pullExceptions, pullMustSee)
			if len(got) == 0 {
				t.Fatalf("a pull added in %s did not fail the guard", file)
			}
			if !strings.Contains(strings.Join(got, "\n"), file+":3") {
				t.Errorf("problems = %q, want one that names %s:3", got, file)
			}
		})
	}
}

// The module's exceptions are well formed, whatever the sources hold: a typo in a
// path would otherwise read as "no site, no problem".
func TestPullExceptionsAreWellFormed(t *testing.T) {
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range pullExceptions {
		key := e.File + " " + e.Func
		if seen[key] {
			t.Errorf("exception %s is listed twice", key)
		}
		seen[key] = true
		if strings.TrimSpace(e.Reason) == "" || e.Sites < 1 {
			t.Errorf("exception %s needs a reason and at least one site", key)
		}
		if slices.Contains(pullHomes, path.Dir(e.File)) {
			t.Errorf("exception %s is in a home of the reference rule, which needs none", key)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.File))); err != nil {
			t.Errorf("exception %s names a file that cannot be read: %v", key, err)
		}
	}
	for _, home := range pullHomes {
		if !strings.HasPrefix(home, "internal/") || strings.HasSuffix(home, "/") {
			t.Errorf("home %q must be a slash path from the module root, without a trailing slash", home)
		}
	}
}

// ── The detector on sources ──────────────────────────────────────────────────
//
// Criterion 2: the guard is only worth something if it sees a violation. Each
// rule is run on sources that break it and on sources that look like a breach and
// are not.

type pullCase struct {
	name string
	file string
	src  string
	want []string // "line kind word"
}

func TestPullDetectorOnSources(t *testing.T) {
	const other = "internal/ui/other/x.go"
	tests := []pullCase{
		// criterion 2: a pull added elsewhere
		{"exec.Command docker pull", other, `package other
import "os/exec"
func f(ref string) { _ = exec.Command("docker", "pull", ref) }
`, []string{"3 argv pull"}},
		{"exec.Command podman pull", other, `package other
import "os/exec"
func f(ref string) { _ = exec.Command("podman", "pull", ref) }
`, []string{"3 argv pull"}},
		{"image pull", other, `package other
import "os/exec"
func f(ref string) { _ = exec.Command("docker", "image", "pull", ref) }
`, []string{"3 argv pull"}},
		{"CommandContext", other, `package other
import ("context"; "os/exec")
func f(ctx context.Context, ref string) { _ = exec.CommandContext(ctx, "docker", "pull", ref) }
`, []string{"3 argv pull"}},
		{"a binary built at run time still has its words read", other, `package other
import "os/exec"
func f(bin, ref string) { _ = exec.Command(bin, "pull", ref) }
`, []string{"3 argv pull"}},
		{"the same call through a renamed os/exec", other, `package other
import run "os/exec"
func f(ref string) { _ = run.Command("docker", "pull", ref) }
`, []string{"3 argv pull"}},
		{"pull first in a literal", other, `package other
func f(ref string) []string { return []string{"pull", ref} }
`, []string{"2 argv pull"}},
		{"pull after the binary in a literal", other, `package other
func f(ref string) []string { return []string{"docker", "pull", ref} }
`, []string{"2 argv pull"}},
		{"pull after a global option", other, `package other
func f(ref string) []string { return []string{"--context", "remote", "pull", ref} }
`, []string{"2 argv pull"}},
		{"pull after a variable, which ends the run G3 reads", other, `package other
func f(host, ref string) []string { return []string{"--host", host, "pull", ref} }
`, []string{"2 argv pull"}},
		{"compose pull", other, `package other
func f() []string { return []string{"compose", "-p", "x", "pull"} }
`, []string{"2 argv pull"}},
		{"a constant folded into the verb", other, `package other
const verb = "pull"
func f(ref string) []string { return []string{"image", verb, ref} }
`, []string{"3 argv pull"}},
		{"a concatenation folded into the verb", other, `package other
func f(ref string) []string { return []string{"pu" + "ll", ref} }
`, []string{"2 argv pull"}},
		{"through the helpers that take an argv", other, `package other
func f(ctx context.Context, ref string) {
	mutate("docker pull", "pull", ref)
	mutateContext(ctx, "docker pull", "pull", ref)
	dockerOutput("pull", ref)
	dockerCombined("image", "pull", ref)
	Build("pull", ref)
	prune("docker pull", "image", "pull")
}
`, []string{"3 argv pull", "4 argv pull", "5 argv pull", "6 argv pull", "7 argv pull", "8 argv pull"}},
		{"a method called Build", other, `package other
func f(r runner, ref string) { _ = r.Build("pull", ref) }
`, []string{"2 argv pull"}},
		{"a toolCmd that names the verb in its Args", other, `package other
func f(ref string) toolCmd { return toolCmd{Name: "helm", Args: []string{"pull", ref}} }
`, []string{"2 argv pull"}},

		// the flag form of an implicit pull
		{"run --pull=always", other, `package other
func f(img string) []string { return []string{"run", "--pull=always", img} }
`, []string{"2 argv --pull=always"}},
		{"build --pull", other, `package other
import "os/exec"
func f() { _ = exec.Command("docker", "build", "--pull", ".") }
`, []string{"3 argv --pull"}},
		{"--pull=missing and --pull=newer", other, `package other
var a = []string{"run", "--pull=missing", "x"}
var b = []string{"run", "--pull=newer", "x"}
`, []string{"2 argv --pull=missing", "3 argv --pull=newer"}},
		{"--pull=never is the opposite of a pull", other, `package other
func f(img string) []string { return []string{"run", "--pull=never", img} }
`, nil},
		{"--pull=false is not a pull either", other, `package other
func f(img string) []string { return []string{"build", "--pull=false", img} }
`, nil},
		{"the constant the launch uses", other, `package other
const noImplicitPull = "--pull=never"
func f(img string) []string { return []string{"run", noImplicitPull, img} }
`, nil},

		// references to the engine's pull function
		{"docker.PullImageContext called", other, `package other
import "github.com/anthnel/devdesk/internal/docker"
func f(ref string) error { return docker.PullImageContext(nil, ref) }
`, []string{"3 reference docker.PullImageContext"}},
		{"docker.PullImage called", other, `package other
import "github.com/anthnel/devdesk/internal/docker"
func f(ref string) error { return docker.PullImage(ref) }
`, []string{"3 reference docker.PullImage"}},
		{"docker.PullImageContext taken as a value", other, `package other
import "github.com/anthnel/devdesk/internal/docker"
var pull = docker.PullImageContext
`, []string{"3 reference docker.PullImageContext"}},
		{"a renamed import of internal/docker", other, `package other
import eng "github.com/anthnel/devdesk/internal/docker"
func f(ref string) error { return eng.PullImage(ref) }
`, []string{"3 reference eng.PullImage"}},
		{"the home of the reference rule is read like any file: judgement is not the detector's", "internal/imagepull/default.go", `package imagepull
import "github.com/anthnel/devdesk/internal/docker"
func Default() Deps { return Deps{Pull: docker.PullImageContext} }
`, []string{"3 reference docker.PullImageContext"}},
		{"a call by its bare name inside internal/docker", "internal/docker/more.go", `package docker
func Refresh(ref string) error { return PullImageContext(nil, ref) }
`, []string{"2 reference PullImageContext"}},
		{"the declaration itself is not a reference", "internal/docker/images.go", `package docker
func PullImage(ref string) error { return nil }
func PullImageContext(ctx context.Context, ref string) error { return nil }
`, nil},
		{"internal/docker builds the verb: read like any file", "internal/docker/images.go", `package docker
func PullImageContext(ctx context.Context, ref string) error {
	return mutateContext(ctx, cmdLabel("pull"), "pull", ref)
}
`, []string{"3 argv pull"}},

		// what looks like a pull and is not
		{"a call named PullImage in another package value", other, `package other
func f(c client, ref string) error { return c.PullImage(ref) }
`, nil},
		{"another package that has a PullImage", other, `package other
import "github.com/anthnel/devdesk/internal/imagepull"
func f() { _ = imagepull.PullImage }
`, nil},
		{"imagepull.Pull is the way in", other, `package other
import "github.com/anthnel/devdesk/internal/imagepull"
func f(ref string, d imagepull.Deps) { _, _ = imagepull.Pull(nil, ref, d) }
`, nil},
		{"a word that contains the verb", other, `package other
func f() []string { return []string{"pulled", "pull-request", "pullimage", "Pull", "pull "} }
`, nil},
		{"a message that names it", other, `package other
import "fmt"
func f() error { return fmt.Errorf("pull") }
`, nil},
		{"the job kind", other, `package other
type Kind string
const KindPull Kind = "pull"
var kinds = []Kind{KindPull}
`, nil},
		{"a map index", other, `package other
func f(perms map[string]bool) bool { return perms["pull"] }
`, nil},
		{"a footer label", other, `package other
func f() string { return cmdLabel("pull") }
`, nil},
		{"the binary position is not a verb", other, `package other
import "os/exec"
func f() { _ = exec.Command("pull") }
`, nil},
		{"a test file is not scanned", "internal/ui/other/x_test.go", `package other
import "os/exec"
func f() { _ = exec.Command("docker", "pull", "x") }
`, nil},

		// the known limits, pinned so that lifting one is a visible change
		{"limit: an argv assembled word by word", other, `package other
func f(ref string) []string { args := []string{"docker"}; args = append(args, "pull", ref); return args }
`, nil},
		{"limit: a verb read from a variable", other, `package other
import "os/exec"
func f(verb, ref string) { _ = exec.Command("docker", verb, ref) }
`, nil},
		{"limit: a shell line", other, `package other
import "os/exec"
func f(ref string) { _ = exec.Command("sh", "-c", "docker pull "+ref) }
`, nil},
		{"limit: a function value", other, `package other
import "os/exec"
var start = exec.Command
func f(ref string) { _ = start("docker", "pull", ref) }
`, nil},
		{"limit: a constant from another file", other, `package other
func f(ref string) []string { return []string{verbPull, ref} }
`, nil},
		{"limit: a dot import of internal/docker", other, `package other
import . "github.com/anthnel/devdesk/internal/docker"
func f(ref string) error { return PullImage(ref) }
`, nil},
		{"limit: a function value inside internal/docker", "internal/docker/more.go", `package docker
var p = PullImageContext
`, nil},
		{"limit: an implicit pull by run, create or build with no flag", other, `package other
func f(img string) []string { return []string{"run", "--rm", img} }
`, nil},
		{"limit: git pull is reported (words, not programs)", other, `package other
func f() []string { return []string{"git", "pull"} }
`, []string{"2 argv pull"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := scanPulls(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("parsing the case: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%d %s %s", s.Line, s.Kind, s.Word))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v", got, tt.want)
			}
		})
	}
}

// A source that does not parse is an error, not a silent pass.
func TestPullDetectorReportsASourceThatDoesNotParse(t *testing.T) {
	if _, err := scanPulls("internal/x/x.go", []byte("package x\nfunc (")); err == nil {
		t.Error("scanPulls accepted a source that does not parse")
	}
}

// The func label an exception is keyed by is the one of G2 and G3.
func TestPullDetectorLabelsTheEnclosingFunction(t *testing.T) {
	src := `package p
type T struct{}
var x = []string{"pull", "a"}
func top() []string { return []string{"pull", "b"} }
func (T) value() []string { return []string{"pull", "c"} }
func (*T) pointer() []string { return []string{"pull", "d"} }
`
	sites, err := scanPulls("internal/p/p.go", []byte(src))
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

// judgePullSources runs the detector and the judgement over a module given as
// sources, the way TestEveryPullGoesThroughImagepull does over the repository.
func judgePullSources(t *testing.T, files map[string]string, exceptions []pullException, mustSee []pullSite) []string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var found []pullSite
	for _, name := range names {
		sites, err := scanPulls(name, []byte(files[name]))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found = append(found, sites...)
	}
	return judgePulls(found, exceptions, mustSee)
}

func TestPullJudgement(t *testing.T) {
	const dockerPull = "package docker\nfunc PullImageContext(ctx context.Context, ref string) error {\n\treturn mutateContext(ctx, \"l\", \"pull\", ref)\n}\n"
	const wiring = "package imagepull\nimport \"github.com/anthnel/devdesk/internal/docker\"\nfunc Default() Deps { return Deps{Pull: docker.PullImageContext} }\n"
	const execPull = "package p\nimport \"os/exec\"\nfunc run(ref string) { _ = exec.Command(\"docker\", \"pull\", ref) }\n"
	const callPull = "package p\nimport \"github.com/anthnel/devdesk/internal/docker\"\nfunc run(ref string) error { return docker.PullImage(ref) }\n"
	const noCall = "package p\nfunc run() {}\n"
	exc := func(file, fn string, sites int) pullException {
		return pullException{File: file, Func: fn, Sites: sites, Reason: "because"}
	}
	dockerExc := exc("internal/docker/images.go", "PullImageContext", 1)

	tests := []struct {
		name       string
		files      map[string]string
		exceptions []pullException
		mustSee    []pullSite
		// wantAll are the fragments each of which some problem must contain;
		// wantNone, when set, requires there to be no problem at all.
		wantAll  []string
		wantNone bool
	}{
		{
			name: "the engine's pull, its one exception and the wiring in imagepull are clean",
			files: map[string]string{
				"internal/docker/images.go":     dockerPull,
				"internal/imagepull/default.go": wiring,
			},
			exceptions: []pullException{dockerExc},
			wantNone:   true,
		},
		{
			name: "the argv of the pull in internal/docker needs its exception",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
			},
			wantAll: []string{"internal/docker/images.go:3", "builds a pull argv"},
		},
		{
			// criterion 2: the report cites file and line
			name: "a pull argv added elsewhere fails with its file and line",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": execPull,
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/ui/other/open.go:3", "pull"},
		},
		{
			name: "internal/imagepull is not exempt from the argv half",
			files: map[string]string{
				"internal/docker/images.go":   dockerPull,
				"internal/imagepull/other.go": strings.ReplaceAll(execPull, "package p", "package imagepull"),
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/imagepull/other.go:3", "builds a pull argv"},
		},
		{
			name: "internal/imagepull may name the engine's pull function, in any of its files",
			files: map[string]string{
				"internal/docker/images.go":     dockerPull,
				"internal/imagepull/default.go": wiring,
				"internal/imagepull/more.go":    strings.ReplaceAll(callPull, "package p", "package imagepull"),
			},
			exceptions: []pullException{dockerExc},
			wantNone:   true,
		},
		{
			name: "a reference to the engine's pull function elsewhere fails",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/mcp/pull.go":      callPull,
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/mcp/pull.go:3", "outside internal/imagepull", "docker.PullImage"},
		},
		{
			name: "a subpackage of internal/imagepull is not internal/imagepull",
			files: map[string]string{
				"internal/docker/images.go":         dockerPull,
				"internal/imagepull/inner/other.go": callPull,
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/imagepull/inner/other.go:3"},
		},
		{
			name: "a sibling package with a similar name is not a home",
			files: map[string]string{
				"internal/docker/images.go":  dockerPull,
				"internal/imagepullx/foo.go": callPull,
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/imagepullx/foo.go:3"},
		},
		{
			name: "a named exception lets its function through",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": callPull,
			},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/open.go", "run", 1)},
			wantNone:   true,
		},
		{
			name: "an exception covers its function, not the file",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": callPull + "func second() { _ = docker.PullImageContext }\n",
			},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"internal/ui/other/open.go:4"},
		},
		{
			name: "a second site in an excepted function is not covered by the first one's reason",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": "package p\nimport \"os/exec\"\n" +
					"func run() { _ = exec.Command(\"docker\", \"pull\", \"a\"); _ = exec.Command(\"docker\", \"pull\", \"b\") }\n",
			},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"covers 1 site(s) but the function has 2", "internal/ui/other/open.go:3"},
		},
		{
			name: "an exception whose code is gone fails and is named",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": noCall,
			},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"the exception still exists but its code is gone", "internal/ui/other/open.go run"},
		},
		{
			name:       "an exception for a file that no longer exists fails",
			files:      map[string]string{"internal/docker/images.go": dockerPull},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/gone.go", "run", 1)},
			wantAll:    []string{"its code is gone", "internal/ui/other/gone.go run"},
		},
		{
			name: "an exception renamed away from its function fails twice: the old name and the new site",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": strings.ReplaceAll(callPull, "func run", "func renamed"),
			},
			exceptions: []pullException{dockerExc, exc("internal/ui/other/open.go", "run", 1)},
			wantAll:    []string{"its code is gone", "internal/ui/other/open.go:3"},
		},
		{
			name: "an exception without a reason fails",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": callPull,
			},
			exceptions: []pullException{dockerExc, {File: "internal/ui/other/open.go", Func: "run", Sites: 1, Reason: "  "}},
			wantAll:    []string{"has no reason"},
		},
		{
			name:       "a verb the module is known to build and the detector no longer sees fails",
			files:      map[string]string{"internal/docker/images.go": "package docker\nfunc PullImageContext() {}\n"},
			exceptions: nil,
			mustSee:    []pullSite{{File: "internal/docker/images.go", Kind: pullKindArgv}},
			wantAll:    []string{"internal/docker/images.go has a pull argv today", "update pullMustSee"},
		},
		{
			name:    "the wiring in imagepull that the detector no longer sees fails",
			files:   map[string]string{"internal/imagepull/default.go": "package imagepull\nfunc Default() {}\n"},
			mustSee: []pullSite{{File: "internal/imagepull/default.go", Kind: pullKindRef}},
			wantAll: []string{"internal/imagepull/default.go has a pull reference today"},
		},
		{
			name: "the must-see file is satisfied by its kind",
			files: map[string]string{
				"internal/docker/images.go":     dockerPull,
				"internal/imagepull/default.go": wiring,
			},
			exceptions: []pullException{dockerExc},
			mustSee:    pullMustSee,
			wantNone:   true,
		},
		{
			name: "a flag that pulls fails like the verb",
			files: map[string]string{
				"internal/docker/images.go": dockerPull,
				"internal/ui/other/open.go": "package p\nfunc run() []string { return []string{\"run\", \"--pull=always\", \"x\"} }\n",
			},
			exceptions: []pullException{dockerExc},
			wantAll:    []string{"internal/ui/other/open.go:2", "--pull=always"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := judgePullSources(t, tt.files, tt.exceptions, tt.mustSee)
			if tt.wantNone {
				if len(got) != 0 {
					t.Errorf("problems = %q, want none", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("no problem, want one containing %q", tt.wantAll)
			}
			joined := strings.Join(got, "\n")
			for _, frag := range tt.wantAll {
				if !strings.Contains(joined, frag) {
					t.Errorf("problems = %q, want one containing %q", got, frag)
				}
			}
		})
	}
}
