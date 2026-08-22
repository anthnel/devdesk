// Package vocabtest holds one test, and it has a package of its own because it
// is about every view at once.
//
// It walks the sources under internal/ui and refuses a forge's name written
// into a string literal. That is the rule forge.Vocabulary exists to make
// checkable: a wording table nothing enforces drifts back one message at a
// time, and the messages that drift are the ones nobody reads until a GitHub
// context renders "GitLab not authenticated".
//
// A source scan is fragile, and that is accepted — it is the price of the
// control, and internal/ui/keymap already has this shape of test.
package vocabtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// uiRoot is the tree every view lives in.
const uiRoot = ".."

// forgeNames are what no view may write into a string.
var forgeNames = []string{"gitlab", "github"}

// allowed are the literals that legitimately carry a forge's name.
//
// Written out rather than pattern-matched, on the model of
// keymap.DeclaredExceptions(): an exception that explains itself in a line
// costs less than a rule with a hole in it, and the next reading of this test
// will not mistake one for a drift.
var allowed = map[string]string{
	// The two lookup tables. They are keyed on config's own constants, so the
	// literals here are the *case labels* of the switch that resolves them —
	// the one place a forge's name has to appear as text.
	"internal/ui/theme/icons.go": "the ForgeIcon table",
}

// importPath recognises a Go import, which is a string literal like any other
// and carries "github.com/" in almost every file.
func isImportPath(v string) bool {
	return strings.HasPrefix(v, "github.com/") || strings.HasPrefix(v, "gitlab.com/") ||
		strings.HasPrefix(v, "gopkg.in/") || strings.HasPrefix(v, "golang.org/")
}

func TestNoViewNamesAForge(t *testing.T) {
	fset := token.NewFileSet()

	err := filepath.WalkDir(uiRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("%s: %v", path, parseErr)
			return nil
		}

		rel := filepath.ToSlash(filepath.Join("internal/ui", strings.TrimPrefix(filepath.ToSlash(path), "../")))
		if why, ok := allowed[rel]; ok {
			t.Logf("%s: skipped — %s", rel, why)
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil || isImportPath(value) {
				return true
			}
			lower := strings.ToLower(value)
			for _, name := range forgeNames {
				if strings.Contains(lower, name) {
					t.Errorf("%s: a string names a forge: %q\n"+
						"  Use forge.Vocabulary for the words and internal/command for a command name.\n"+
						"  If it truly belongs here, declare it in `allowed` with the reason.",
						fset.Position(lit.Pos()), value)
					return false
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", uiRoot, err)
	}
}
