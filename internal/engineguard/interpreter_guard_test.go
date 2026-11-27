package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// DEVDESK-816, OC-18: nothing the program starts is handed to an interpreter as a
// command line built at run time. `sh -c "command -v " + name` runs whatever
// `name` holds as shell code; the same word in an argv array is one element and
// stays data. For a container the rule is the one of the story: the shell of
// `T` is `<engine> exec -it <id> <shell>`, an array, with a validated ID.
//
// A static test in the vein of G2, G3, G8 and S9, which it reuses (importName,
// fileConsts, funcLabel, execCallee, isStringSlice, calleeName, argvCallees,
// wordShape, dynamicMarker, scanDestructiveTree, launchModuleRoot,
// launchGuardMinimum). It reads every non-test .go file of the module with
// go/parser, no type information, and reports, with file and line, a word list
// that holds an interpreter and, after it, a command-line flag followed by a word
// that is not a constant:
//
//	interpreter  sh, bash, zsh, dash, ash, ksh, fish, /bin/sh, /bin/bash, ...,
//	             cmd, cmd.exe, powershell, powershell.exe, pwsh (a constant word)
//	flag         -c (and -lc, -ic, ...), /c, /k, -Command, -EncodedCommand
//	dynamic      the word right after the flag is not a constant: a variable, a
//	             call, a concatenation with one (`"command -v "+name`,
//	             `fmt.Sprintf("docker exec -it %s sh", id)`)
//
// A "word list" is the one S9 reads: a []string literal, the arguments of an
// append after its first, the arguments of exec.Command(Context) from the binary
// on, and the arguments of the argv helpers G3 knows.
//
// Limits, stated rather than discovered: a constant script (`sh -c "true"`) is not
// reported, it holds no run-time value; a binary or a flag held in a variable, a
// list built word by word and a script written to a file are not seen; only the
// word right after the flag is judged, so `cmd /c start <url>` (the words that
// follow `/c` are one command line there) is not reported; a runner is not
// exempt: there is no home for this, only the register below.

// interpreterWords are the programs that read a command line.
var interpreterWords = []string{
	"sh", "bash", "zsh", "dash", "ash", "ksh", "fish",
	"/bin/sh", "/bin/bash", "/bin/zsh", "/bin/dash", "/usr/bin/sh", "/usr/bin/bash",
	"cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe",
}

// interpreterFlags are the options that make the next word the command line.
var interpreterFlags = []string{"/c", "/k", "-Command", "-command", "-EncodedCommand"}

// shellDashC matches -c and its combinations (-lc, -ic, -ec).
var shellDashC = regexp.MustCompile(`^-[a-z]*c$`)

const interpreterRule = "interpreter given a command line built at run time"

// interpreterException is a function that does so and why it may. Same rules as
// the other guards: keyed by file and function, Sites is the exact count, a debt
// register that does not grow for convenience; an entry whose code is gone, whose
// count differs or without a reason fails the test.
type interpreterException struct {
	File   string // slash path from the module root
	Func   string // see funcLabel
	Sites  int
	Reason string
}

// The register. VerifyEntrypoint (internal/docker/launch.go) is in it as a
// temporary debt: it runs `sh -c "command -v "+entrypoint` in a throwaway
// container with the entrypoint typed in the launch form. The entry goes when the
// check goes through an argv or the name is validated.
var interpreterExceptions = []interpreterException{
	{
		File:  "internal/docker/launch.go",
		Func:  "VerifyEntrypoint",
		Sites: 1,
		Reason: "temporary: the entrypoint typed in the launch form is spliced into `sh -c \"command -v \"+entrypoint`, run in a " +
			"throwaway `run --rm` container of the image the user is about to launch; to move to an argv or a validation of the " +
			"name (follow-up story). It does not reach the host's interpreter, and the user already chooses what that container runs.",
	},
	{
		File:  "internal/testutil/stepshell/stepshell.go",
		Func:  "Run",
		Sites: 1,
		Reason: "permanent: a test helper that runs a recorded remediation step through `sh -c` on purpose, to prove the " +
			"step's quoting (scanning.md); the command is a constant of the test, the helper is imported by _test.go files only.",
	},
}

// interpreterSite is one word list that gives an interpreter a command line built
// at run time.
type interpreterSite struct {
	File string
	Line int
	Func string
	What string
}

func (s interpreterSite) String() string {
	return fmt.Sprintf("%s:%d: %s: %s (in %s)", s.File, s.Line, interpreterRule, s.What, s.Func)
}

// judgeInterpreter returns the problems of one word list, as `what` texts.
func judgeInterpreter(words []ast.Expr, consts map[string]string) []string {
	var out []string
	interpreter := ""
	for i, w := range words {
		s, ok := constString(w, consts, nil)
		if !ok {
			continue
		}
		switch {
		case interpreter == "" && slices.Contains(interpreterWords, s):
			interpreter = s
		case interpreter != "" && i+1 < len(words) &&
			(slices.Contains(interpreterFlags, s) || (shellDashC.MatchString(s) && !strings.HasPrefix(s, "--"))):
			if shape, dynamic := wordShape(words[i+1], consts); dynamic {
				out = append(out, fmt.Sprintf("%s %s followed by %s", interpreter, s, strings.ReplaceAll(shape, dynamicMarker, "<dynamic>")))
			}
		}
	}
	return out
}

// scanInterpreter parses src as the file name (a slash path from the module root)
// and returns its sites ordered by line. A _test.go file has none.
func scanInterpreter(name string, src []byte) ([]interpreterSite, error) {
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

	var out []interpreterSite
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		report := func(pos token.Pos, words []ast.Expr) {
			for _, what := range judgeInterpreter(words, consts) {
				out = append(out, interpreterSite{File: name, Line: fset.Position(pos).Line, Func: label, What: what})
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if isStringSlice(n.Type) {
					report(n.Pos(), n.Elts)
				}
			case *ast.CallExpr:
				callee := calleeName(n.Fun)
				fn, binIdx := execCallee(n.Fun, execName)
				start := -1
				switch {
				case fn != "":
					start = binIdx // the binary is a word here: it is the interpreter
				case callee == "append":
					start = 1
				case contains(argvCallees, callee):
					start = 0
				}
				if start >= 0 && start < len(n.Args) {
					report(n.Pos(), n.Args[start:])
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// judgeInterpreterSites turns the sites found into the problems to report: a site
// without an exception, an exception whose count is not the number of sites, an
// exception whose code is gone or without a reason.
func judgeInterpreterSites(found []interpreterSite, exceptions []interpreterException) []string {
	var problems []string
	byFunc := map[string][]interpreterSite{}
	for _, s := range found {
		byFunc[s.File+"\x00"+s.Func] = append(byFunc[s.File+"\x00"+s.Func], s)
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := byFunc[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why that command line is safe", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s has no site left; delete it from interpreterExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf("exception %s %s covers %d site(s) but the function has %d", e.File, e.Func, e.Sites, len(sites)))
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
				"%s: a value inside a command line is code to the interpreter (`x; rm -rf ~`, `$(...)`); pass it as one "+
					"argv element instead, validated (docker.IsContainerID and its siblings), or put an exception with a reason "+
					"in interpreterExceptions (OC-18, DEVDESK-816)", s))
		}
	}
	return problems
}

// ── The module ───────────────────────────────────────────────────────────────

var (
	interpreterScanOnce  sync.Once
	interpreterScanSites []interpreterSite
	interpreterScanFiles int
	interpreterScanErr   error
)

func scanInterpreterRepo(t *testing.T) []interpreterSite {
	t.Helper()
	interpreterScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			interpreterScanErr = err
			return
		}
		interpreterScanSites, interpreterScanFiles, interpreterScanErr = scanDestructiveTree(root, scanInterpreter)
	})
	if interpreterScanErr != nil {
		t.Fatalf("scanning the module for command lines: %v", interpreterScanErr)
	}
	if interpreterScanFiles < launchGuardMinimum {
		t.Fatalf("the interpreter guard read %d files, want the whole module (at least %d)", interpreterScanFiles, launchGuardMinimum)
	}
	return interpreterScanSites
}

// On the real module: no interpreter is given a command line built at run time
// outside the named exceptions, and an exception whose code is gone fails. Today
// it names internal/docker/launch.go (VerifyEntrypoint).
func TestNoInterpreterIsGivenAConstructedCommandLine(t *testing.T) {
	for _, p := range judgeInterpreterSites(scanInterpreterRepo(t), interpreterExceptions) {
		t.Error(p)
	}
}

// The detector sees each shape, and only the bad one.
func TestTheInterpreterDetectorOnSources(t *testing.T) {
	const pkg = "package p\nimport (\n\t\"fmt\"\n\t\"os/exec\"\n)\nvar _ = fmt.Sprintf\nvar _ = exec.Command\n"
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"exec sh -c with a variable", `func f(c string) { _ = exec.Command("sh", "-c", c) }`, 1},
		{"exec bash -c with a concatenation", `func f(id string) { _ = exec.Command("bash", "-c", "docker exec "+id) }`, 1},
		{"exec CommandContext", `func f(ctx context.Context, c string) { _ = exec.CommandContext(ctx, "sh", "-c", c) }`, 1},
		{"a Sprintf script", `func f(id string) { _ = exec.Command("/bin/sh", "-c", fmt.Sprintf("docker exec -it %s sh", id)) }`, 1},
		{"a word list", `func f(e string) []string { return []string{"--entrypoint", "/bin/sh", "img", "-c", "command -v " + e} }`, 1},
		{"an argv helper", `func f(image, e string) { dockerCombined("run", "--entrypoint", "/bin/sh", image, "-c", "command -v "+e) }`, 1},
		{"combined flag", `func f(c string) { _ = exec.Command("bash", "-lc", c) }`, 1},
		{"powershell", `func f(c string) { _ = exec.Command("powershell", "-NoProfile", "-Command", c) }`, 1},
		{"cmd /c with the command line", `func f(c string) { _ = exec.Command("cmd", "/c", c) }`, 1},
		{"append", `func f(c string) []string { a := []string{"sh"}; return append(a, "-c", c) }`, 0}, // limit: the interpreter is in another list
		{"a constant script", `func f() { _ = exec.Command("sh", "-c", "true") }`, 0},
		{"an argv array, the good form", `func f(id string) { _ = exec.Command("docker", "exec", "-it", id, "/bin/sh") }`, 0},
		{"the shell is the last element, not an interpreter call", `func f(id string) []string { return []string{"exec", "-it", id, "/bin/sh"} }`, 0},
		{"git -c is not an interpreter", `func f(v string) { _ = exec.Command("git", "-c", "core.x="+v, "status") }`, 0},
		{"a shell with a script file", `func f(p string) { _ = exec.Command("sh", p) }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sites, err := scanInterpreter("internal/x/x.go", []byte(pkg+tc.body+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != tc.want {
				t.Errorf("sites = %v, want %d", sites, tc.want)
			}
		})
	}
}

// A command line added to the real module fails the guard, with its file and line.
func TestACommandLineAddedToTheModuleFailsTheGuard(t *testing.T) {
	module := scanInterpreterRepo(t)
	file := "internal/docker/added.go"
	src := "package docker\nimport \"os/exec\"\nfunc shellIn(id string) *exec.Cmd {\n\treturn exec.Command(\"sh\", \"-c\", \"docker exec -it \"+id+\" sh\")\n}\n"
	sites, err := scanInterpreter(file, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := judgeInterpreterSites(append(slices.Clone(module), sites...), interpreterExceptions)
	if !strings.Contains(strings.Join(got, "\n"), file+":4") {
		t.Errorf("problems = %q, want one that names %s:4", got, file)
	}
}

// The register is held to its own rules: a reason, a count, and code that is
// still there.
func TestTheInterpreterExceptionsAreHeldToTheirRules(t *testing.T) {
	site := interpreterSite{File: "internal/x/x.go", Func: "f", Line: 4, What: "sh -c followed by <dynamic>"}
	for _, tc := range []struct {
		name  string
		found []interpreterSite
		ex    []interpreterException
		want  string
	}{
		{"no reason", []interpreterSite{site}, []interpreterException{{File: site.File, Func: "f", Sites: 1}}, "has no reason"},
		{"code gone", nil, []interpreterException{{File: site.File, Func: "f", Sites: 1, Reason: "r"}}, "its code is gone"},
		{"count differs", []interpreterSite{site, site}, []interpreterException{{File: site.File, Func: "f", Sites: 1, Reason: "r"}}, "covers 1 site(s) but the function has 2"},
		{"no exception", []interpreterSite{site}, nil, "code to the interpreter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(judgeInterpreterSites(tc.found, tc.ex), "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want one with %q", got, tc.want)
			}
		})
	}
}
