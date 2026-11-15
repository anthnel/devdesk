package netdiag

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-330 criterion 7, view level: the path trace reaches the container
// engine only through a fixed list of read functions, and no source file of the
// package names a verb that starts, creates or enters a container. The runtime
// half is TestPathEngineReadsStartNothing.
//
// What the guard sees: selectors on the docker and engine packages (aliases
// included, dot imports refused), string literals and constant concatenations
// and fmt.Sprint* calls over literals whose words include a forbidden verb
// (case-insensitive, per word: "container START abc" is caught, UI prose such
// as "Run again" is not), imports of
// os/exec, execabs, syscall, calls to os.StartProcess, and any internal
// package that is not on allowedInternalImports.
//
// What it does not see, by construction: a verb assembled from variables or
// read at run time (a config value, an environment variable, strings.Join over
// a slice, a rune loop), and a read function of the docker package that itself
// starts something (the allowed list is the contract; those functions are
// covered by internal/docker's own tests and by the runtime half).

const (
	dockerImportPath   = "github.com/anthnel/devdesk/internal/docker"
	engineImportPath   = "github.com/anthnel/devdesk/internal/engine"
	internalImportBase = "github.com/anthnel/devdesk/internal/"
)

// allowedDockerUses is every docker.<X> the path trace may name: the read
// functions and the types they return.
var allowedDockerUses = []string{
	"Container", "ContainerNetworking", "EndpointNetwork",
	"ListContainersContext", "EngineOperatingSystem", "InspectContainerNetworking",
	"IsContainerID", "NetworkBridge", "InspectNetworkBridge",
}

// allowedEngineUses is every engine.<X> the path trace may name.
var allowedEngineUses = []string{"Current"}

// allowedInternalImports is every internal package the netdiag sources import.
// A new one fails the guard so that it is looked at once: a package that can
// reach the engine or a process must not slip in through a helper.
//
// wslenv is the declared exception: it runs wslinfo to detect WSL2, which is
// a process start. Its own guard, internal/wslenv/source_guard_test.go, is the
// contract for what it may run.
var allowedInternalImports = []string{
	"command", "config", "docker", "engine", "forward", "jobs", "netcheck",
	"netiface", "nettrace", "ports", "shared", "wslenv",
	"ui/components", "ui/datatable", "ui/help", "ui/indicator", "ui/keymap",
	"ui/overlay", "ui/shortcut", "ui/theme",
}

// forbiddenImports start processes: the path trace has no business with them.
var forbiddenImports = []string{"os/exec", "golang.org/x/sys/execabs", "syscall"}

// forbiddenVerbs are engine arguments that change a container or open a shell
// in it. A string whose words include one of them has no business in this
// package.
var forbiddenVerbs = []string{
	"start", "exec", "run", "cp", "attach", "create", "restart", "unpause",
	"kill", "stop", "pause", "rm", "commit",
}

// engineWords are the words after which a verb is an engine command line
// ("container start abc", "docker exec -it"), as opposed to prose.
var engineWords = []string{"docker", "podman", "container", "engine"}

// verbIn returns the forbidden verb s names, ignoring case and surrounding
// punctuation: either s is that one word, or a word of s is one right after an
// engine word. UI prose ("Run again", "Pause forward", "Kill process") is not an
// engine command and is deliberately not flagged; a verb at the head of a
// multi-word string with no engine word before it is the guard's blind spot.
func verbIn(s string) (string, bool) {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToLower(strings.Trim(w, `.,:;!?()[]{}"'`+"`"))
	}
	if len(words) == 1 && slices.Contains(forbiddenVerbs, words[0]) {
		return words[0], true
	}
	for i := 1; i < len(words); i++ {
		if slices.Contains(forbiddenVerbs, words[i]) && slices.Contains(engineWords, words[i-1]) {
			return words[i], true
		}
	}
	return "", false
}

// constString folds a string literal, or a + of foldable operands, to its value.
func constString(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return constString(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := constString(x.X)
		r, rok := constString(x.Y)
		return l + r, lok && rok
	}
	return "", false
}

// sprintResult evaluates fmt.Sprint* when every argument is a literal, so a
// verb assembled by "%s%s", "sta", "rt" is seen. ok is false otherwise.
func sprintResult(name string, args []ast.Expr) (string, bool) {
	vals := make([]any, 0, len(args))
	for _, a := range args {
		if s, ok := constString(a); ok {
			vals = append(vals, s)
			continue
		}
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.INT {
			n, err := strconv.Atoi(lit.Value)
			if err != nil {
				return "", false
			}
			vals = append(vals, n)
			continue
		}
		return "", false
	}
	switch name {
	case "Sprintf":
		format, ok := vals[0].(string)
		if !ok {
			return "", false
		}
		return fmt.Sprintf(format, vals[1:]...), true
	case "Sprint":
		return fmt.Sprint(vals...), true
	case "Sprintln":
		return fmt.Sprintln(vals...), true
	}
	return "", false
}

// engineViolations parses one Go source and returns one line per forbidden use
// (see the head comment for what that covers).
func engineViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	allowed := map[string][]string{} // local package name -> allowed selectors
	var osName, fmtName string
	var out []string
	report := func(pos token.Pos, what string) {
		out = append(out, filepath.Base(filename)+":"+strconv.Itoa(fset.Position(pos).Line)+": "+what)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		local := filepath.Base(path)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if local == "." {
			report(imp.Pos(), "dot import of "+path+" hides what is used")
		}
		switch path {
		case dockerImportPath:
			allowed[local] = allowedDockerUses
		case engineImportPath:
			allowed[local] = allowedEngineUses
		case "os":
			osName = local
		case "fmt":
			fmtName = local
		}
		if slices.Contains(forbiddenImports, path) {
			report(imp.Pos(), "imports "+path)
		}
		if rest, ok := strings.CutPrefix(path, internalImportBase); ok && !slices.Contains(allowedInternalImports, rest) {
			report(imp.Pos(), "imports internal/"+rest+", which is not on the netdiag import list")
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			checkSelector(x, allowed, osName, report)
		case *ast.BinaryExpr:
			if s, ok := constString(x); ok {
				checkVerb(x.Pos(), s, "constant string", report)
				return false // the folded value covers every operand
			}
		case *ast.BasicLit:
			if s, ok := constString(x); ok {
				checkVerb(x.Pos(), s, "string literal "+x.Value, report)
			}
		case *ast.CallExpr:
			checkSprint(x, fmtName, report)
		}
		return true
	})
	return out, nil
}

func checkSelector(x *ast.SelectorExpr, allowed map[string][]string, osName string, report func(token.Pos, string)) {
	id, ok := x.X.(*ast.Ident)
	if !ok {
		return
	}
	if list, isPkg := allowed[id.Name]; isPkg && !slices.Contains(list, x.Sel.Name) {
		report(x.Pos(), id.Name+"."+x.Sel.Name+" is not on the read-only list")
	}
	if osName != "" && id.Name == osName && x.Sel.Name == "StartProcess" {
		report(x.Pos(), "os.StartProcess starts a process")
	}
}

func checkVerb(pos token.Pos, s, what string, report func(token.Pos, string)) {
	if v, bad := verbIn(s); bad {
		report(pos, what+" "+strconv.Quote(s)+" holds the engine verb "+strconv.Quote(v)+" that changes a container")
	}
}

// checkSprint reports a fmt.Sprint* over literals whose result holds a verb the
// format string alone does not (those are already reported as literals).
func checkSprint(call *ast.CallExpr, fmtName string, report func(token.Pos, string)) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fmtName == "" || len(call.Args) == 0 {
		return
	}
	if id, isIdent := sel.X.(*ast.Ident); !isIdent || id.Name != fmtName {
		return
	}
	res, ok := sprintResult(sel.Sel.Name, call.Args)
	if !ok {
		return
	}
	if first, isConst := constString(call.Args[0]); isConst {
		if _, already := verbIn(first); already {
			return
		}
	}
	checkVerb(call.Pos(), res, "fmt."+sel.Sel.Name+" result", report)
}

func TestPathTabCallsOnlyReadOnlyEngineFunctions(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source file found (err %v)", err)
	}
	scanned := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := engineViolations(file, raw)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, v := range got {
			t.Errorf("%s", v)
		}
		scanned++
	}
	if scanned < 10 {
		t.Fatalf("scanned %d files: the guard would pass on nothing", scanned)
	}
}

// The detector itself: it fails on each kind of forbidden use, an alias or a
// concatenation does not hide one, and clean code passes.
func TestEngineViolationsDetectsForbiddenUses(t *testing.T) {
	const head = "package netdiag\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"github.com/anthnel/devdesk/internal/docker\"\n\teng \"github.com/anthnel/devdesk/internal/engine\"\n)\nvar _ = fmt.Sprint\nvar _ = os.Getenv\n"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"clean", "var _ = docker.ListContainersContext\nvar _ docker.Container\nvar _ = docker.IsContainerID\nvar _ = eng.Current", 0},
		{"docker function outside the list", "var _ = docker.StartContainer", 1},
		{"docker exec", "var _ = docker.ExecInContainer", 1},
		{"engine function outside the list", "var _ = eng.Run", 1},
		{"verb as an argument", "var _ = []string{\"container\", \"start\", \"abc\"}", 1},
		{"exec verb", "var _ = []string{\"exec\", \"-it\"}", 1},
		{"run verb", "var _ = \"run\"", 1},
		{"cp verb", "var _ = \"cp\"", 1},
		{"attach verb", "var _ = \"attach\"", 1},
		{"constant", "const verb = \"start\"", 1},
		{"concatenation", "var _ = \"sta\" + \"rt\"", 1},
		{"concatenation in parentheses", "var _ = (\"ki\" + \"l\") + \"l\"", 1},
		{"upper case", "var _ = \"START\"", 1},
		{"mixed case", "var _ = \"Restart\"", 1},
		{"multi-word string", "var _ = \"container start abc\"", 1},
		{"multi-word with punctuation", "var _ = \"docker exec, then stop\"", 1},
		{"sprintf assembling a verb", "var _ = fmt.Sprintf(\"%s%s\", \"sta\", \"rt\")", 1},
		{"sprintf assembling an engine command", "var _ = fmt.Sprintf(\"container %s%s\", \"sta\", \"rt\")", 1},
		{"sprintf with a verb argument", "var _ = fmt.Sprintf(\"container %s\", \"x\") + \"stop\"", 1},
		{"os.StartProcess", "var _ = os.StartProcess", 1},
		{"read verbs are fine", "var _ = []string{\"ps\", \"info\", \"container\", \"inspect\"}", 0},
		{"prose with a verb is not a command", "var _ = []string{\"Run again\", \"Pause forward\", \"Kill process\", \"Stop forwarding %s to %s?\"}", 0},
		{"words that only contain a verb", "var _ = \"The column starts on this machine; a running container, restarts nothing here\"", 0},
		{"clean sprintf", "var _ = fmt.Sprintf(\"%s hops to %s\", \"3\", \"host\")", 0},
		{"sprintf over variables", "func f(a, b string) string { return fmt.Sprintf(\"%s%s\", a, b) }", 0},
		{"os.Getenv", "var _ = os.Getenv(\"HOME\")", 0},
		{"non-string constant arithmetic", "var _ = 1 + 2", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engineViolations("synthetic.go", head+tc.body+"\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Errorf("violations = %v, want %d", got, tc.want)
			}
		})
	}
	importCases := []struct {
		name string
		src  string
		want int
	}{
		{"os/exec", "package netdiag\nimport \"os/exec\"\nvar _ = exec.Command\n", 1},
		{"execabs", "package netdiag\nimport \"golang.org/x/sys/execabs\"\nvar _ = execabs.Command\n", 1},
		{"syscall", "package netdiag\nimport \"syscall\"\nvar _ = syscall.Getpid\n", 1},
		{"aliased docker import", "package netdiag\nimport d \"github.com/anthnel/devdesk/internal/docker\"\nvar _ = d.StartContainer\n", 1},
		{"aliased docker import, allowed use", "package netdiag\nimport d \"github.com/anthnel/devdesk/internal/docker\"\nvar _ = d.IsContainerID\n", 0},
		{"dot import of docker", "package netdiag\nimport . \"github.com/anthnel/devdesk/internal/docker\"\nvar _ = StartContainer\n", 1},
		{"dot import of engine", "package netdiag\nimport . \"github.com/anthnel/devdesk/internal/engine\"\nvar _ = Run\n", 1},
		{"internal package off the list", "package netdiag\nimport \"github.com/anthnel/devdesk/internal/oci\"\nvar _ = oci.X\n", 1},
		{"internal package on the list", "package netdiag\nimport \"github.com/anthnel/devdesk/internal/ui/theme\"\nvar _ = theme.X\n", 0},
		{"third-party package", "package netdiag\nimport \"github.com/charmbracelet/x/ansi\"\nvar _ = ansi.Strip\n", 0},
	}
	for _, tc := range importCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engineViolations("synthetic.go", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Errorf("violations = %v, want %d", got, tc.want)
			}
		})
	}
}

// DEVDESK-333: wslenv is on the import list.
func TestWslenvIsADeclaredInternalImport(t *testing.T) {
	got, err := engineViolations("synthetic.go", "package netdiag\nimport \"github.com/anthnel/devdesk/internal/wslenv\"\nvar _ = wslenv.Detect\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("violations = %v, want none", got)
	}
}
