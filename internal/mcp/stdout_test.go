package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In stdio, stdout *is* the protocol channel. A stray write anywhere on the
// `dk mcp` path corrupts the frame, and what the client reports is a JSON parse
// error that names nothing — the failure is as far from its cause as it gets.
//
// So nothing in this package writes to stdout, and this test is what says so.
// It is the shape of TestNoViewStylesItsOwnFooterMessage: it parses the sources
// and fails naming file, line and what it found.
//
// main.go is deliberately *not* covered here. It writes to stdout on the TUI
// path and must go on doing so; what matters is that the mcp branch returns
// before any of it, which TestTheMCPBranchIsTakenBeforeAnythingPrints checks
// from the other end.
func TestNothingInThisPackageWritesToStdout(t *testing.T) {
	fset := token.NewFileSet()

	banned := map[string]string{
		"fmt.Print":   "writes to stdout",
		"fmt.Printf":  "writes to stdout",
		"fmt.Println": "writes to stdout",
		"os.Stdout":   "is the protocol channel",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/mcp: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				pkg, ok := node.X.(*ast.Ident)
				if !ok {
					return true
				}
				if why, bad := banned[pkg.Name+"."+node.Sel.Name]; bad {
					t.Errorf("%s:%d: %s.%s %s. Diagnostics go to stderr; stdout carries the MCP frames.",
						name, fset.Position(node.Pos()).Line, pkg.Name, node.Sel.Name, why)
				}
			case *ast.CallExpr:
				// The print builtins go to stderr, so they would not corrupt a
				// frame — but they are debug scaffolding, and scaffolding is how
				// a write to stdout gets added next to them later.
				fn, ok := node.Fun.(*ast.Ident)
				if ok && (fn.Name == "println" || fn.Name == "print") {
					t.Errorf("%s:%d: builtin %s — debug scaffolding in a server whose output is a protocol.",
						name, fset.Position(node.Pos()).Line, fn.Name)
				}
			}
			return true
		})
	}
}

// The mcp branch has to be taken before main() loads a theme, opens a log file
// or prints a warning — every one of those writes to stdout on the TUI path.
// Reading the source is the only way to check an ordering that has no return
// value; running main() is not an option.
func TestTheMCPBranchIsTakenBeforeAnythingPrints(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	body := string(src)
	branch := strings.Index(body, "runMCP")
	if branch < 0 {
		t.Fatal("main.go never mentions runMCP — the subcommand is not wired")
	}

	firstPrint := -1
	for _, call := range []string{"fmt.Print", "tea.LogToFile"} {
		if i := strings.Index(body, call); i >= 0 && (firstPrint < 0 || i < firstPrint) {
			firstPrint = i
		}
	}
	if firstPrint >= 0 && firstPrint < branch {
		t.Errorf("main.go writes to stdout before it dispatches to runMCP — the first MCP frame would arrive behind a warning")
	}
}
