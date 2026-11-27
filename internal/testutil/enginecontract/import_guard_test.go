package enginecontract_test

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

// OC-09a1, rule C1: the contract helpers never enter the binary `dk`. Only
// _test.go files import internal/testutil/enginecontract (it imports "testing",
// internal/engineguard and internal/enginefixture, none of which a production
// binary wants it to drag along). Modelled on E1
// (internal/enginefixture/import_guard_test.go) and F1 (fakecli).
//
// The guard reads every .go file of the module with go/parser, no type
// information, and names file and line. It cannot see an import path built at run
// time or a copy of the files.

const (
	contractPackagePath = "github.com/anthnel/devdesk/internal/testutil/enginecontract"
	// c1Minimum is how many files the walk must read, so a walk that finds
	// nothing (a wrong root) cannot pass.
	c1Minimum = 200
)

// c1Excluded directories are skipped at any depth below the root.
var c1Excluded = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

type c1Site struct {
	File string
	Line int
}

func (s c1Site) String() string { return fmt.Sprintf("%s:%d", s.File, s.Line) }

// c1Sites returns the imports of the package in one source file, by the path of
// the file relative to the module root (slash separated). A _test.go file is
// never reported. No directory is exempt: a package that needs the helpers in
// production has misplaced them.
func c1Sites(rel string, src []byte) ([]c1Site, error) {
	if strings.HasSuffix(rel, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var out []c1Site
	for _, imp := range af.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == contractPackagePath {
			out = append(out, c1Site{File: rel, Line: fset.Position(imp.Pos()).Line})
		}
	}
	return out, nil
}

func c1Walk(root string) ([]c1Site, int, error) {
	var all []c1Site
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
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || slices.Contains(c1Excluded, d.Name())) {
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
		sites, err := c1Sites(filepath.ToSlash(rel), src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, sites...)
		return nil
	})
	return all, files, err
}

func TestOnlyTestsImportEngineContract(t *testing.T) {
	sites, files, err := c1Walk(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if files < c1Minimum {
		t.Fatalf("the walk read %d .go files, want at least %d: wrong root?", files, c1Minimum)
	}
	for _, s := range sites {
		t.Errorf("C1: %s imports internal/testutil/enginecontract from a non-test file: the contract helpers must not reach "+
			"the dk binary (only _test.go files may)", s)
	}
}

// The detector on sources held in memory: a guard that never fires is worth nothing.
func TestC1DetectorOnSources(t *testing.T) {
	const imp = `import "github.com/anthnel/devdesk/internal/testutil/enginecontract"`
	tests := []struct {
		name string
		file string
		src  string
		want []int // lines of the offending imports
	}{
		{"a non-test file elsewhere", "internal/docker/exec.go", "package docker\n\n" + imp + "\n", []int{3}},
		{"an aliased import", "internal/x/x.go", "package x\n\nimport ec \"github.com/anthnel/devdesk/internal/testutil/enginecontract\"\n", []int{3}},
		{"a blank import", "internal/x/x.go", "package x\n\nimport _ \"github.com/anthnel/devdesk/internal/testutil/enginecontract\"\n", []int{3}},
		{"inside an import block", "cmd/dk/main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/anthnel/devdesk/internal/testutil/enginecontract\"\n)\n", []int{5}},
		{"the root package", "main.go", "package main\n\n" + imp + "\n", []int{3}},
		{"another package of the same tree", "internal/testutil/fakecli/fakecli.go", "package fakecli\n\n" + imp + "\n", []int{3}},
		{"a _test.go file", "internal/docker/contract_support_test.go", "package docker\n\n" + imp + "\n", nil},
		{"an external test file", "internal/docker/x_test.go", "package docker_test\n\n" + imp + "\n", nil},
		{"another package whose name starts the same", "internal/x/x.go",
			"package x\n\nimport \"github.com/anthnel/devdesk/internal/testutil/enginecontract2\"\n", nil},
		{"no import at all", "internal/x/x.go", "package x\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := c1Sites(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("c1Sites: %v", err)
			}
			var lines []int
			for _, s := range sites {
				lines = append(lines, s.Line)
				if s.File != tt.file {
					t.Errorf("site names %q, want %q", s.File, tt.file)
				}
			}
			if !slices.Equal(lines, tt.want) {
				t.Errorf("lines = %v, want %v", lines, tt.want)
			}
		})
	}
}

// repoRoot finds the module root from the working directory (the package dir).
func repoRoot(t testing.TB) string {
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
