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

// forgeMarkers are what no view may write into a string.
//
// The names are the obvious half. The prefixes are the half that got through:
// the auth view carried `glpat-xxxxxxxxxxxxxxxxxxxx` as its token placeholder
// for the life of §3.6, and this test walked past it every time because
// `glpat-` names no platform. A token prefix is forge-specific without saying
// which forge, which is exactly the shape a guard on names cannot see.
//
// They are matched case-insensitively on a substring, so `ghp_` catches a
// placeholder and an error message alike.
var forgeMarkers = []string{
	"gitlab", "github",
	"glpat-", "ghp_", "github_pat_", "gho_", "ghs_",
}

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

	// `.gitlab-ci.yml` is a *filename*, not vocabulary. The file is called that
	// on disk whatever a context targets, so the icon table names it the way it
	// names `Dockerfile` and `Cargo.toml` — and a GitHub-targeted context that
	// happens to hold a GitLab pipeline file still wants the right glyph on it.
	// This is a distinction the guard exists to make the reader confront, not
	// one it exists to forbid.
	"internal/ui/fileicon/fileicon.go": "filenames that happen to carry a forge's name",

	// L'écran About nomme le dépôt de DevDesk lui-même. Ce n'est pas du
	// vocabulaire de forge : la valeur ne dépend d'aucun contexte et ne
	// changerait pas si l'utilisateur configurait GitLab — c'est une adresse,
	// au même titre que le chemin de `~/.devdesk`. La distinction est celle que
	// fait déjà l'entrée fileicon : ce que la garde cherche est un *mot* qui
	// devrait suivre la forge active.
	"internal/ui/about/model.go": "the address of this project's own repository",
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
			for _, marker := range forgeMarkers {
				if strings.Contains(lower, marker) {
					t.Errorf("%s: a string is specific to one forge (%q): %q\n"+
						"  Use forge.Vocabulary for the words and the token example,\n"+
						"  and internal/command for a command name.\n"+
						"  If it truly belongs here, declare it in `allowed` with the reason.",
						fset.Position(lit.Pos()), marker, value)
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
