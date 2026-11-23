package security

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-751 (step 6, AC5): DevDesk copies the command under Run and never runs
// it. The command is text the scanner wrote about a package: handing it to a
// shell would turn a finding into code execution. The guard reads the sources of
// this package and fails, naming the file and the function, when:
//
//	R1  a file that imports os/exec (or the exec wrapper) also reads the command;
//	R2  one function both reads the command and starts a process;
//	R3  a function reachable from the copy path (copyFixCommand, runCommand, the Run
//	    line) starts a process.
//
// "Reads the command" is: the identifier runCommand, reproCommand or copyCommand
// (DEVDESK-752: the Repro line is the same kind of text), a selector .FixCommand,
// a selector .Command on anything but exec (fixGroup.Command and
// remediation.Fix.Command, DEVDESK-754), or a FixCommandCopiedMsg. "Starts a process" is a call on exec, teaexec or syscall,
// os.StartProcess, or tea.Exec / tea.ExecProcess.
//
// Limits, stated: it follows calls by name inside this package, so a call through
// a function value or the writeClipboard seam is not followed (the clipboard
// package pipes the text to the clipboard tool's stdin, never as an argument),
// and a process started from another package is out of its sight.

// copyPathRoots are where the copied command lives. They must exist: a rename
// would otherwise make R3 pass on nothing.
var copyPathRoots = []string{"copyFixCommand", "runCommand", "detailsRun", "reproCommand", "copyCommand", "detailsRepro", "copyGroupCommand"}

type sourceFile struct {
	name string
	file *ast.File
}

func parseSources(t *testing.T, sources map[string]string) []sourceFile {
	t.Helper()
	fset := token.NewFileSet()
	var out []sourceFile
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		out = append(out, sourceFile{name, f})
	}
	return out
}

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(b)
	}
	return sources
}

// readsCommand reports whether a node mentions the command.
func readsCommand(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.Ident:
			if v.Name == "runCommand" || v.Name == "reproCommand" || v.Name == "copyCommand" || v.Name == "FixCommandCopiedMsg" {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "FixCommand" {
				found = true
			}
			// .Command is the group's command; exec.Command and teaexec.Command
			// build a process and are what startsProcess looks for.
			if pkg, isPkg := v.X.(*ast.Ident); v.Sel.Name == "Command" && (!isPkg || (pkg.Name != "exec" && pkg.Name != "teaexec")) {
				found = true
			}
		}
		return !found
	})
	return found
}

// startsProcess reports whether a node calls something that starts a process.
func startsProcess(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return !found
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return !found
		}
		switch {
		case pkg.Name == "exec" || pkg.Name == "teaexec":
			found = true
		case pkg.Name == "syscall" && (sel.Sel.Name == "Exec" || sel.Sel.Name == "ForkExec"):
			found = true
		case pkg.Name == "os" && sel.Sel.Name == "StartProcess":
			found = true
		case pkg.Name == "tea" && (sel.Sel.Name == "Exec" || sel.Sel.Name == "ExecProcess"):
			found = true
		}
		return !found
	})
	return found
}

func importsExec(f *ast.File) bool {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err == nil && (path == "os/exec" || strings.HasSuffix(path, "/teaexec")) {
			return true
		}
	}
	return false
}

// neverRunViolations is the guard itself, over parsed sources.
func neverRunViolations(files []sourceFile, roots []string) []string {
	var out []string
	funcs := map[string][]*ast.FuncDecl{}
	owner := map[*ast.FuncDecl]string{}
	for _, sf := range files {
		reads := false
		for _, decl := range sf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
			owner[fn] = sf.name
			read := readsCommand(fn.Body)
			reads = reads || read
			if read && startsProcess(fn.Body) {
				out = append(out, sf.name+": "+fn.Name.Name+" reads the command and starts a process (R2)")
			}
		}
		if reads && importsExec(sf.file) {
			out = append(out, sf.name+" reads the command and imports os/exec or the exec wrapper (R1)")
		}
	}

	seen := map[string]bool{}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		for _, fn := range funcs[name] {
			if startsProcess(fn.Body) {
				out = append(out, owner[fn]+": "+name+" is on the copy path and starts a process (R3)")
			}
			ast.Inspect(fn.Body, func(x ast.Node) bool {
				switch c := x.(type) {
				case *ast.CallExpr:
					switch f := c.Fun.(type) {
					case *ast.Ident:
						queue = append(queue, f.Name)
					case *ast.SelectorExpr:
						queue = append(queue, f.Sel.Name)
					}
				}
				return true
			})
		}
	}
	return out
}

func TestTheCopiedCommandIsNeverRun(t *testing.T) {
	files := parseSources(t, packageSources(t))
	declared := map[string]bool{}
	for _, sf := range files {
		for _, decl := range sf.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				declared[fn.Name.Name] = true
			}
		}
	}
	for _, root := range copyPathRoots {
		if !declared[root] {
			t.Errorf("%s is not declared in a non-test file: the guard has nothing to follow, rename it in copyPathRoots", root)
		}
	}
	for _, v := range neverRunViolations(files, copyPathRoots) {
		t.Error(v)
	}
}

// The guard must be able to fail: each shape it forbids is fed to it.
func TestTheNeverRunGuardCatchesWhatItForbids(t *testing.T) {
	const header = "package security\nimport (\n\"os/exec\"\n)\n"
	tests := []struct {
		name    string
		src     string
		roots   []string
		wantHit string
	}{
		{"a command from the finding handed to exec", header + `
func a(f Finding) { exec.Command("sh", "-c", f.FixCommand).Run() }`, nil, "(R2)"},
		{"the copied command handed to exec", header + `
func a(f Finding) { cmd, _ := runCommand(f); exec.Command(cmd).Run() }`, nil, "(R2)"},
		{"a file that imports exec and reads the command elsewhere", header + `
func a(f Finding) string { return f.FixCommand }
func b() { exec.Command("ls").Run() }`, nil, "(R1)"},
		{"the copy path calls a helper that starts a process", `package security
import "os/exec"
func copyFixCommand() { helper() }
func helper() { exec.Command("ls").Run() }`, []string{"copyFixCommand"}, "(R3)"},
		{"the copy path hands the command to the terminal", `package security
func copyFixCommand(f Finding) { tea.ExecProcess(nil, nil) ; _ = f.FixCommand }`, []string{"copyFixCommand"}, "(R2)"},
		{"the group's command handed to exec", header + `
func a(g fixGroup) { exec.Command("sh", "-c", g.Command).Run() }`, nil, "(R2)"},
		{"a remediation fix's command handed to the terminal", `package security
func a(fx remediation.Fix) { tea.ExecProcess(nil, nil) ; _ = fx.Command }`, nil, "(R2)"},
		{"a file that imports exec and reads the group's command elsewhere", header + `
func a(g fixGroup) string { return g.Command }
func b() { exec.Command("ls").Run() }`, nil, "(R1)"},
		{"the group copy path starts a process", `package security
import "os/exec"
func copyGroupCommand() { exec.Command("ls").Run() }`, []string{"copyGroupCommand"}, "(R3)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := neverRunViolations(parseSources(t, map[string]string{"x.go": tc.src}), tc.roots)
			if !slices.ContainsFunc(got, func(v string) bool { return strings.Contains(v, tc.wantHit) }) {
				t.Errorf("violations = %v, want one marked %s", got, tc.wantHit)
			}
		})
	}

	benign := `package security
func copyFixCommand(f Finding) { go writeClipboard(f.FixCommand) }
func other() { exec.Command("git", "status").Run() }`
	if got := neverRunViolations(parseSources(t, map[string]string{"x.go": benign}), []string{"copyFixCommand"}); len(got) != 0 {
		t.Errorf("a copy that starts nothing is reported: %v", got)
	}
}
