package nettrace

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The trace sends its probes itself, over ICMP and UDP sockets. It must never
// start a child process (traceroute, ping) nor replace the process image, on
// any platform. The guard reads every .go file of the package whatever its
// build constraints (go/parser does not evaluate them), so a file that only
// builds on another OS is covered too.

const bannedImport = "os/exec"

// bannedCalls maps an import path to the functions it must not be called for.
var bannedCalls = map[string]map[string]bool{
	"os":                    {"StartProcess": true},
	"syscall":               {"ForkExec": true, "Exec": true},
	"golang.org/x/sys/unix": {"Exec": true},
}

// guardSource returns one message per offence in src ("name:line: ...").
func guardSource(fset *token.FileSet, name string, src any) ([]string, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	local := map[string]string{} // local package name -> import path
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == bannedImport {
			out = append(out, fmt.Sprintf("%s: import of %s", fset.Position(imp.Pos()), path))
		}
		if _, watched := bannedCalls[path]; !watched {
			continue
		}
		n := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			n = imp.Name.Name
		}
		local[n] = path
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := local[id.Name]; ok && bannedCalls[path][sel.Sel.Name] {
			out = append(out, fmt.Sprintf("%s: call to %s.%s", fset.Position(call.Pos()), id.Name, sel.Sel.Name))
		}
		return true
	})
	return out, nil
}

func TestGuardSourceFlagsOffences(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      int
	}{
		{"exec.go", "package x\nimport \"os/exec\"\nfunc f() { _ = exec.Command(\"traceroute\") }\n", 1},
		{"aliased_exec.go", "package x\nimport e \"os/exec\"\nvar _ = e.Command\n", 1},
		{"startprocess.go", "package x\nimport \"os\"\nfunc f() { _, _ = os.StartProcess(\"a\", nil, nil) }\n", 1},
		{"forkexec.go", "package x\nimport \"syscall\"\nfunc f() { _, _ = syscall.ForkExec(\"a\", nil, nil); _ = syscall.Exec(\"a\", nil, nil) }\n", 2},
		{"unix.go", "package x\nimport \"golang.org/x/sys/unix\"\nfunc f() { _ = unix.Exec(\"a\", nil, nil) }\n", 1},
		{"aliased_unix.go", "package x\nimport u \"golang.org/x/sys/unix\"\nfunc f() { _ = u.Exec(\"a\", nil, nil) }\n", 1},
		{"tagged.go", "//go:build windows\n\npackage x\nimport \"os/exec\"\nvar _ = exec.Command\n", 1},
		{"clean.go", "package x\nimport (\"os\"; \"syscall\")\n// os.StartProcess in a comment is not a call\nfunc f() { _ = os.Getpid(); _ = syscall.Getpid(); var s struct{ Exec func() }; s.Exec() }\n", 0},
	} {
		got, err := guardSource(token.NewFileSet(), c.name, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: got %d offences %v, want %d", c.name, len(got), got, c.want)
		}
	}
}

func TestPackageNeverStartsAProcess(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files++
		offences, err := guardSource(fset, e.Name(), src)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		for _, o := range offences {
			t.Error(o + ": the trace must not start or exec a process")
		}
	}
	if files == 0 {
		t.Error("the scan saw no Go file: the guard would pass vacuously")
	}
}
