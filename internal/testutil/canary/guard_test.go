package canary_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// OC-23, rule C2: the canary helper never enters the binary `dk`. Only _test.go
// files import internal/testutil/canary (it imports "testing"). Modelled on C1
// (internal/testutil/enginecontract/import_guard_test.go): every .go file of the
// module is read with go/parser, imports only, and a site is reported with file
// and line. It cannot see an import path built at run time or a copy of the files.

const (
	canaryPackagePath = "github.com/anthnel/devdesk/internal/testutil/canary"
	// c2Minimum is how many files the walk must read, so a walk that finds
	// nothing (a wrong root) cannot pass.
	c2Minimum = 200
)

var c2Excluded = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

type c2Site struct {
	File string
	Line int
}

func (s c2Site) String() string { return fmt.Sprintf("%s:%d", s.File, s.Line) }

// c2Sites returns the imports of the package in one source file. A _test.go file
// is never reported; no directory is exempt.
func c2Sites(rel string, src []byte) ([]c2Site, error) {
	if strings.HasSuffix(rel, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var out []c2Site
	for _, imp := range af.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == canaryPackagePath {
			out = append(out, c2Site{File: rel, Line: fset.Position(imp.Pos()).Line})
		}
	}
	return out, nil
}

func c2Walk(root string) ([]c2Site, int, error) {
	var all []c2Site
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || slices.Contains(c2Excluded, d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sites, err := c2Sites(filepath.ToSlash(rel), src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, sites...)
		return nil
	})
	return all, files, err
}

func TestOnlyTestsImportCanary(t *testing.T) {
	sites, files, err := c2Walk(moduleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if files < c2Minimum {
		t.Fatalf("the walk read %d .go files, want at least %d: wrong root?", files, c2Minimum)
	}
	for _, s := range sites {
		t.Errorf("C2: %s imports internal/testutil/canary from a non-test file: the canary helper must not reach "+
			"the dk binary (only _test.go files may)", s)
	}
}

// The detector on sources held in memory: a guard that never fires is worth nothing.
func TestC2DetectorOnSources(t *testing.T) {
	const imp = `import "github.com/anthnel/devdesk/internal/testutil/canary"`
	tests := []struct {
		name string
		file string
		src  string
		want []int
	}{
		{"a non-test file", "internal/docker/exec.go", "package docker\n\n" + imp + "\n", []int{3}},
		{"an aliased import", "internal/x/x.go", "package x\n\nimport c \"github.com/anthnel/devdesk/internal/testutil/canary\"\n", []int{3}},
		{"a blank import", "internal/x/x.go", "package x\n\nimport _ \"github.com/anthnel/devdesk/internal/testutil/canary\"\n", []int{3}},
		{"inside an import block", "cmd/dk/main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/anthnel/devdesk/internal/testutil/canary\"\n)\n", []int{5}},
		{"another helper of the same tree", "internal/testutil/fakecli/fakecli.go", "package fakecli\n\n" + imp + "\n", []int{3}},
		{"a _test.go file", "internal/docker/x_test.go", "package docker\n\n" + imp + "\n", nil},
		{"an external test file", "internal/docker/y_test.go", "package docker_test\n\n" + imp + "\n", nil},
		{"a package whose name starts the same", "internal/x/x.go",
			"package x\n\nimport \"github.com/anthnel/devdesk/internal/testutil/canary2\"\n", nil},
		{"no import", "internal/x/x.go", "package x\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := c2Sites(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			var lines []int
			for _, s := range sites {
				lines = append(lines, s.Line)
			}
			if !slices.Equal(lines, tt.want) {
				t.Errorf("lines = %v, want %v", lines, tt.want)
			}
		})
	}
}

func moduleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
