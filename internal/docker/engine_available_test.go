package docker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OC-09a1 step 1 (DEVDESK-806): the production function that reports a missing
// engine binary is engineAvailable(), not requireEngine(). The name requireEngine
// belongs to the helper every contract test starts with, requireEngine(t), which
// lives in a _test.go file of this package (guard G5, internal/engineguard); the
// two would not compile side by side. The rename changes no behaviour: the tests
// of the message (TestTheMissingBinaryIsNamedAfterTheConfiguredEngine and
// TestRequireDockerReportsMissingBinary) call engineAvailable() and expect what
// they always did.
//
// A static test, because a regression here is a compile error in the test files
// of a later story, far from its cause: it reads the non-test files of this
// package with go/parser.

func TestTheProductionCodeNamesItsEngineCheckEngineAvailable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := false
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			switch fn.Name.Name {
			case "requireEngine":
				t.Errorf("%s declares requireEngine: the name is the contract tests' requireEngine(t); "+
					"the production check is engineAvailable() (OC-09a1, step 1)", name)
			case "engineAvailable":
				declared = true
				if fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
					t.Errorf("%s: engineAvailable must be func() error", name)
				}
			}
		}
	}
	if !declared {
		t.Error("no non-test file declares engineAvailable() error")
	}
}
