package teaexec

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-290: every hand-over of the terminal to a child goes through
// teaexec.Process or teaexec.Command, which work around a Bubble Tea renderer
// ticker race. Calling tea.ExecProcess, tea.Exec or tea.Suspend directly
// brings the frozen screen back, so this guard names file:line for each call
// outside internal/ui/teaexec. Comments that mention them are not calls.

const bubbleteaPath = "charm.land/bubbletea/v2"

var bannedCalls = map[string]bool{"ExecProcess": true, "Exec": true, "Suspend": true}

// guardSource returns one message per banned call in src ("name:line: ...").
func guardSource(fset *token.FileSet, name string, src any) ([]string, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	teaNames := map[string]bool{}
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p != bubbleteaPath {
			continue
		}
		n := "tea"
		if imp.Name != nil {
			n = imp.Name.Name
		}
		teaNames[n] = true
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !bannedCalls[sel.Sel.Name] {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && teaNames[id.Name] {
			out = append(out, fmt.Sprintf("%s: %s.%s; use teaexec.Process or teaexec.Command", fset.Position(call.Pos()), id.Name, sel.Sel.Name))
		}
		return true
	})
	return out, nil
}

func TestGuardSourceFlagsDirectCalls(t *testing.T) {
	const bad = `package x
import tea "charm.land/bubbletea/v2"
// tea.ExecProcess in a comment is not a call
func f() { _ = tea.ExecProcess(nil, nil); _ = tea.Exec(nil, nil); _ = tea.Suspend() }
`
	const aliased = `package x
import bt "charm.land/bubbletea/v2"
func f() { _ = bt.ExecProcess(nil, nil) }
`
	const other = `package x
import "os/exec"
func f() { _ = exec.Command("x"); var tea struct{ Exec func() }; tea.Exec() }
`
	for _, c := range []struct {
		name, src string
		want      int
	}{{"bad.go", bad, 3}, {"alias.go", aliased, 1}, {"other.go", other, 0}} {
		got, err := guardSource(token.NewFileSet(), c.name, c.src)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != c.want {
			t.Errorf("%s: got %d offences %v, want %d", c.name, len(got), got, c.want)
		}
	}
}

func TestNoDirectTeaExecOutsideTeaexec(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("go.mod not found above the working directory")
		}
		root = parent
	}
	fset := token.NewFileSet()
	var violations []string
	files := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if rel == "internal/ui/teaexec" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		v, err := guardSource(fset, rel, src)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Error("the scan saw no Go file: the guard would pass vacuously")
	}
	for _, v := range violations {
		t.Error(v)
	}
}
