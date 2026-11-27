package viewer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// OC-84 (DEVDESK-801, requirement S1), criterion 1: the pager path assembles no
// command string and names no shell. It scans the three files that make up the
// path (the viewer's `V` handler, the Pageable contract, and the container log
// source) for a string literal that is a shell, a shell's "run this string"
// flag, or a pipeline fragment.
//
// This is a guard local to the pager path, not the repository-wide one planned
// as G2 (OC-05): no such guard exists yet, so there is no exception list to
// adapt. When G2 lands, this one can go.
//
// Limits: it reads literals, so a shell name built by concatenation or read from
// a variable is missed; and it covers these three files, not a fourth that a
// later change might add to the path.

var pagerPathFiles = []string{
	"update.go",
	"../../viewer/source.go",
	"../containers/sources.go",
}

var shellWords = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "cmd": true, "cmd.exe": true,
	"powershell": true, "pwsh": true, "-c": true, "/c": true, "/bin/sh": true, "/bin/bash": true,
}

// shellFragments are substrings no argv element of the pager path may carry.
var shellFragments = []string{"${", "$(", "2>&1", " | ", "&&"}

// pagerPathOffences returns "file:line: literal" for every shell word or
// fragment among the string literals of src.
func pagerPathOffences(fset *token.FileSet, name string, src any) ([]string, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		bad := shellWords[strings.ToLower(s)]
		for _, frag := range shellFragments {
			if strings.Contains(s, frag) {
				bad = true
			}
		}
		if bad {
			out = append(out, fmt.Sprintf("%s: %q", fset.Position(lit.Pos()), s))
		}
		return true
	})
	return out, nil
}

func TestPagerPathOffencesFlagsAShell(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"sh -c", `package x
import "os/exec"
func f() { _ = exec.Command("sh", "-c", "x") }`, 2},
		{"cmd /c", `package x
import "os/exec"
func f() { _ = exec.Command("cmd", "/c", "x") }`, 2},
		{"a pipeline string", `package x
import "fmt"
func f(b string) string { return fmt.Sprintf("%s logs 2>&1 | ${PAGER:-less} -R", b) }`, 1},
		{"an argv of words", `package x
import "os/exec"
func f(b string) { _ = exec.Command(b, "logs", "--tail", "500") }`, 0},
		{"a shell in a comment", "package x\n// exec.Command(\"sh\", \"-c\", x)\nfunc f() {}\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pagerPathOffences(token.NewFileSet(), "x.go", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d offences %v, want %d", len(got), got, tt.want)
			}
		})
	}
}

func TestThePagerPathNamesNoShell(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range pagerPathFiles {
		offences, err := pagerPathOffences(fset, name, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, o := range offences {
			t.Errorf("%s: the pager path hands a command string to a shell; build an argv and pipe it (teaexec.Pipe)", o)
		}
	}
}
