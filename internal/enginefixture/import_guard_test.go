package enginefixture_test

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

// OC-08a, rule E1: the recorded fixtures never enter the binary `dk`. Only
// _test.go files and internal/testutil/fakecli (itself reachable from _test.go
// files only, rule F1) import internal/enginefixture.
//
// The guard reads every .go file of the module with go/parser, no type
// information, like G2 (internal/engineguard/launch_guard_test.go) and F1.
//
// What it cannot see: an import path built at run time, a go:embed of the
// fixtures directory from another package, and a copy of the files. It fails
// naming file and line, never the fixture.

const (
	engineFixturePath = "github.com/anthnel/devdesk/internal/enginefixture"
	// e1Minimum is how many files the walk must read, so a walk that finds
	// nothing (a wrong root) cannot pass.
	e1Minimum = 200
)

// e1Allowed are the directories whose non-test files may import the package.
var e1Allowed = []string{"internal/enginefixture", "internal/testutil/fakecli"}

// e1Excluded directories are skipped at any depth below the root.
var e1Excluded = []string{".worktrees", ".git", "vendor", "testdata", "docs-site", "node_modules"}

// e1Site is one offending import.
type e1Site struct {
	File string
	Line int
}

func (s e1Site) String() string { return fmt.Sprintf("%s:%d", s.File, s.Line) }

// e1Sites returns the imports of enginefixture in one source file, judged by the
// path of the file relative to the module root (slash separated). A _test.go
// file and an allowed directory are never reported.
func e1Sites(rel string, src []byte) ([]e1Site, error) {
	if strings.HasSuffix(rel, "_test.go") {
		return nil, nil
	}
	for _, dir := range e1Allowed {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return nil, nil
		}
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var out []e1Site
	for _, imp := range af.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == engineFixturePath {
			out = append(out, e1Site{File: rel, Line: fset.Position(imp.Pos()).Line})
		}
	}
	return out, nil
}

// e1Root finds the module root from the working directory (the package dir).
func e1Root(t testing.TB) string {
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

// e1Walk reads every .go file under root and returns the offending imports and
// the number of files read.
func e1Walk(root string) ([]e1Site, int, error) {
	var all []e1Site
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
			if slices.Contains(e1Excluded, d.Name()) && rel != "." {
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
		sites, err := e1Sites(filepath.ToSlash(rel), src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, sites...)
		return nil
	})
	return all, files, err
}

func TestOnlyTestsImportEngineFixture(t *testing.T) {
	sites, files, err := e1Walk(e1Root(t))
	if err != nil {
		t.Fatal(err)
	}
	if files < e1Minimum {
		t.Fatalf("the walk read %d .go files, want at least %d: wrong root?", files, e1Minimum)
	}
	for _, s := range sites {
		t.Errorf("E1: %s imports internal/enginefixture from a non-test file: recorded fixtures must not reach the dk binary "+
			"(only _test.go files and internal/testutil/fakecli may)", s)
	}
}

// The detector itself, on sources held in memory: a guard that never fires is
// worth nothing.
func TestE1DetectorOnSources(t *testing.T) {
	const imp = `import "github.com/anthnel/devdesk/internal/enginefixture"`
	tests := []struct {
		name string
		file string
		src  string
		want []int // lines of the offending imports
	}{
		{"a non-test file elsewhere", "internal/docker/replay.go", "package docker\n\n" + imp + "\n", []int{3}},
		{"an aliased import", "internal/x/x.go", "package x\n\nimport ef \"github.com/anthnel/devdesk/internal/enginefixture\"\n", []int{3}},
		{"a blank import", "internal/x/x.go", "package x\n\nimport _ \"github.com/anthnel/devdesk/internal/enginefixture\"\n", []int{3}},
		{"inside an import block", "cmd/dk/main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/anthnel/devdesk/internal/enginefixture\"\n)\n", []int{5}},
		{"the root package", "main.go", "package main\n\n" + imp + "\n", []int{3}},
		{"a _test.go file", "internal/docker/replay_test.go", "package docker\n\n" + imp + "\n", nil},
		{"fakecli, which imports testing itself", "internal/testutil/fakecli/fixtures.go", "package fakecli\n\n" + imp + "\n", nil},
		{"enginefixture itself", "internal/enginefixture/load.go", "package enginefixture\n\n" + imp + "\n", nil},
		{"another package whose name starts the same", "internal/x/x.go",
			"package x\n\nimport \"github.com/anthnel/devdesk/internal/enginefixture2\"\n", nil},
		{"a sibling directory of fakecli", "internal/testutil/fakecli2/x.go", "package x\n\n" + imp + "\n", []int{3}},
		{"no import at all", "internal/x/x.go", "package x\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := e1Sites(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("e1Sites: %v", err)
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
