package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strings"
	"testing"
)

// OC-22 (DEVDESK-820), requirements S2 and C2, the static half: what DevDesk
// destroys is what the user previewed, one identifier at a time, never forced.
//
// It extends guard G3 (destructive_guard_test.go, whose detector and helpers it
// reuses) with two rules on the argv that internal/docker itself builds, where G3
// is deliberately silent ("a package home is exempt as a whole"):
//
//	F  no destructive verb carries a force or collateral option: -f, --force, -v,
//	   --volumes, -a, --all, --depend, --filter (and the glued forms -fv, -vf).
//	   `rm -f` removes a running container, `rm -v` its anonymous volumes with it,
//	   `rmi -f` an image a container uses. `stop` keeps `--time` if it ever has one:
//	   that is a delay, not a force.
//	P  no prune. `container prune`, `image prune`, `volume prune` (and `system prune`,
//	   `builder prune`...) remove whatever the engine finds when the command starts,
//	   which is not what the user previewed. The one exception today is network
//	   prune, outside this story (pruneExceptions, with its reason); an entry is
//	   deleted the day its code is.
//
// Same limits as G3, stated and not hidden: an option appended word by word
// (args = append(args, "-f")), a verb or an option read from a variable or a
// function result, and an argv in another package are not seen. The run-time half
// covers the first: internal/docker/destructive_ids_test.go pins the whole argv of
// every destructive function (exactly the verb and the identifier), and
// removal_report_test.go the batch.

// forceOptions are the words a destructive argv may not carry.
var forceOptions = []string{"-f", "--force", "-v", "--volumes", "-a", "--all", "--depend", "--filter", "-fv", "-vf"}

// forceMustSee are the destructive verbs internal/docker builds without prune.
// The guard fails when it stops seeing one: it would pass because the detector went
// blind, not because nothing is built.
var forceMustSee = []string{"stop", "rm", "rmi", "volume rm", "network rm"}

// pruneException is a prune argv that internal/docker still builds, and why.
// Same rules as destructiveException: keyed by file and function, Sites exact, a
// reason, and an entry whose code is gone fails.
type pruneException struct {
	File   string // slash path from the module root
	Func   string // see funcLabel
	Sites  int
	Reason string
}

var pruneExceptions = []pruneException{
	{
		File: "internal/docker/networks.go", Func: "PruneNetworks", Sites: 1,
		Reason: "network prune is outside OC-22 (DEVDESK-820), which covers containers, images and volumes: " +
			"the Networks tab has no preview yet (DEVDESK-949). Delete this entry, and the function, when it gets one",
	},
}

// argvSite is one destructive argv found in the source, with its constant words.
type argvSite struct {
	File  string
	Line  int
	Func  string
	Verb  string   // "rm", "volume rm", "image prune"...
	Words []string // the whole run of constant words, the verb and the options
}

func (s argvSite) String() string {
	return fmt.Sprintf("%s:%d: %q (in %s)", s.File, s.Line, s.Words, s.Func)
}

// scanArgvWords is scanDestructive keeping the words of each site. A _test.go file
// has none.
func scanArgvWords(name string, src []byte) ([]argvSite, error) {
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

	var out []argvSite
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		add := func(pos token.Pos, verb string, words []string) {
			out = append(out, argvSite{File: name, Line: fset.Position(pos).Line, Func: label, Verb: verb, Words: words})
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if !isStringSlice(n.Type) {
					return true
				}
				words := constWords(n.Elts, consts)
				if verb := destructiveVerbAnywhere(words); verb != "" {
					add(n.Pos(), verb, words)
				}
			case *ast.CallExpr:
				fn, binIdx := execCallee(n.Fun, execName)
				var starts []int
				switch {
				case fn != "":
					starts = []int{binIdx + 1}
				case contains(argvCallees, calleeName(n.Fun)):
					for i := range n.Args {
						starts = append(starts, i)
					}
				}
				for _, i := range starts {
					if i >= len(n.Args) {
						continue
					}
					words := constWords(n.Args[i:], consts)
					if verb := destructiveVerbAnywhere(words); verb != "" {
						add(n.Pos(), verb, words)
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

// judgeForceAndPrune returns the problems to report. Only the sites of
// internal/docker and internal/kind (the homes of the destructive verbs) are
// judged: anywhere else G3 already refuses the site altogether.
func judgeForceAndPrune(found []argvSite, exceptions []pruneException, mustSee []string) []string {
	var problems []string
	seen := map[string]bool{}
	prunes := map[string][]argvSite{}

	for _, s := range found {
		if !inDestructiveHome(s.File) {
			continue
		}
		seen[s.Verb] = true
		if strings.HasSuffix(s.Verb, "prune") {
			prunes[s.File+"\x00"+s.Func] = append(prunes[s.File+"\x00"+s.Func], s)
			continue
		}
		for _, w := range s.Words {
			if slices.Contains(forceOptions, w) {
				problems = append(problems, fmt.Sprintf(
					"%s: the argv of %q carries %q: a destructive verb takes the identifier and nothing else, "+
						"never a force or collateral option (OC-22, DEVDESK-820, C2)", s, s.Verb, w))
				break
			}
		}
	}

	for _, verb := range mustSee {
		if !seen[verb] {
			problems = append(problems, fmt.Sprintf(
				"internal/docker builds %q today but the force guard no longer sees it there: the detector went blind "+
					"or the verb moved; update forceMustSee (OC-22, DEVDESK-820)", verb))
		}
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := prunes[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("prune exception %s %s has no reason: say why it may still prune", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the prune exception still exists but its code is gone: %s %s has no prune site left; delete it from pruneExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf(
				"prune exception %s %s covers %d site(s) but the function has %d", e.File, e.Func, e.Sites, len(sites)))
		}
	}

	keys := make([]string, 0, len(prunes))
	for k := range prunes {
		if !covered[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, s := range prunes[k] {
			problems = append(problems, fmt.Sprintf(
				"%s: builds a prune (%s). A prune removes whatever the engine finds when it runs, not what the user "+
					"previewed: list the items, freeze the list, and remove each by identifier with Remove* "+
					"(OC-22, DEVDESK-820, S2/C2)", s, s.Verb))
		}
	}
	return problems
}

// scanArgvWordsRepo scans the module once per test process, like scanDestructiveRepo.
func scanArgvWordsRepo(t *testing.T) []argvSite {
	t.Helper()
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	sites, files, err := scanDestructiveTree(root, scanArgvWords)
	if err != nil {
		t.Fatalf("scanning the module for destructive argv words: %v", err)
	}
	if files < launchGuardMinimum {
		t.Fatalf("the OC-22 guard read %d files, want the whole module (at least %d)", files, launchGuardMinimum)
	}
	return sites
}

// OC-22 criteria 1 and 5 on the real module.
func TestNoDestructiveArgvForcesOrPrunes(t *testing.T) {
	for _, p := range judgeForceAndPrune(scanArgvWordsRepo(t), pruneExceptions, forceMustSee) {
		t.Error(p)
	}
}

// ── The detector on sources ──────────────────────────────────────────────────
//
// The guard is only worth something if it sees a violation: each rule runs on
// sources that break it, and on sources that look like a breach and are not.

func TestForceAndPruneDetectorOnSources(t *testing.T) {
	const docker = "internal/docker/x.go"
	tests := []struct {
		name       string
		file, src  string
		exceptions []pruneException
		want       []string // substrings, one per expected problem, in order
	}{
		{"rm -f", docker, `package docker
func f(id string) { _ = []string{"rm", "-f", id} }
`, nil, []string{`carries "-f"`}},
		{"rmi --force", docker, `package docker
func f(id string) error { return mutate("rmi", "rmi", "--force", id) }
`, nil, []string{`carries "--force"`}},
		{"volume rm -v", docker, `package docker
func f(n string) error { return mutate("x", "volume", "rm", "-v", n) }
`, nil, []string{`carries "-v"`}},
		{"rm --volumes", docker, `package docker
func f(id string) { _ = []string{"rm", "--volumes", id} }
`, nil, []string{`carries "--volumes"`}},
		{"rm --depend", docker, `package docker
func f(id string) { _ = []string{"rm", "--depend", id} }
`, nil, []string{`carries "--depend"`}},
		{"network rm -f", docker, `package docker
func f(n string) error { return mutate("x", "network", "rm", "-f", n) }
`, nil, []string{`carries "-f"`}},
		{"a force behind a global option", docker, `package docker
func f(id string) { _ = []string{"--context", "x", "rm", "-f", id} }
`, nil, []string{`carries "-f"`}},
		{"rm with the identifier only", docker, `package docker
func f(id string) { _ = []string{"rm", id} }
`, nil, nil},
		{"stop with a delay is not a force", docker, `package docker
func f(id string) { _ = []string{"stop", "--time", "5", id} }
`, nil, nil},
		{"ps -a is not destructive", docker, `package docker
func f() { _ = []string{"ps", "-a", "--format", "x"} }
`, nil, nil},
		{"run -v is not a destructive verb", docker, `package docker
func f(v string) { _ = []string{"run", "-v", v, "img"} }
`, nil, nil},
		{"image prune", docker, `package docker
func f() (string, error) { return prune("docker image prune", "image", "prune", "-f") }
`, nil, []string{"builds a prune (image prune)"}},
		{"volume prune", docker, `package docker
func f() { _ = []string{"volume", "prune", "-f"} }
`, nil, []string{"builds a prune (volume prune)"}},
		{"container prune", docker, `package docker
func f() { _ = []string{"container", "prune"} }
`, nil, []string{"builds a prune (container prune)"}},
		{"system prune", docker, `package docker
func f() { _ = []string{"system", "prune", "-a"} }
`, nil, []string{"builds a prune (system prune)"}},
		{"a bare prune", docker, `package docker
func f() { _ = []string{"prune"} }
`, nil, []string{"builds a prune (prune)"}},
		{"a prune with an exception", docker, `package docker
func PruneNetworks() (string, error) { return prune("x", "network", "prune", "-f") }
`, []pruneException{{File: docker, Func: "PruneNetworks", Sites: 1, Reason: "kept on purpose"}}, nil},
		{"an exception does not cover a second prune", docker, `package docker
func PruneNetworks() {
	_ = []string{"network", "prune", "-f"}
	_ = []string{"volume", "prune", "-f"}
}
`, []pruneException{{File: docker, Func: "PruneNetworks", Sites: 1, Reason: "kept on purpose"}}, []string{"covers 1 site(s) but the function has 2"}},
		{"an exception whose code is gone", docker, `package docker
func f(id string) { _ = []string{"rm", id} }
`, []pruneException{{File: docker, Func: "PruneNetworks", Sites: 1, Reason: "kept on purpose"}}, []string{"its code is gone"}},
		{"an exception with no reason", docker, `package docker
func PruneNetworks() { _ = []string{"network", "prune", "-f"} }
`, []pruneException{{File: docker, Func: "PruneNetworks", Sites: 1, Reason: " "}}, []string{"has no reason"}},
		// Elsewhere the site is G3's, which refuses it whole: this guard says nothing.
		{"outside the homes is G3's", "internal/ui/other/x.go", `package other
func f(id string) { _ = []string{"rm", "-f", id} }
`, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := scanArgvWords(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}

			got := judgeForceAndPrune(sites, tt.exceptions, nil)

			if len(got) != len(tt.want) {
				t.Fatalf("problems = %q, want %d matching %q", got, len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], want)
				}
			}
		})
	}
}

// A test file is never scanned: the contract tests remove the containers of their
// own run with `rm -f` (cleanupNow), and that is the contract's business.
func TestForceAndPruneDetectorIgnoresTestFiles(t *testing.T) {
	sites, err := scanArgvWords("internal/docker/x_test.go", []byte(`package docker
func f(id string) { _ = []string{"rm", "-f", id}; _ = []string{"volume", "prune", "-f"} }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("a _test.go file reported %v", sites)
	}
}

// The blindness check: when a verb internal/docker builds is not seen, the guard
// says so rather than passing.
func TestForceGuardFailsWhenItNoLongerSeesAVerb(t *testing.T) {
	got := judgeForceAndPrune(nil, nil, []string{"rm"})
	if len(got) != 1 || !strings.Contains(got[0], `builds "rm" today`) {
		t.Errorf("problems = %q, want the blindness report for rm", got)
	}
}
