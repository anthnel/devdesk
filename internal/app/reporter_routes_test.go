package app

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEveryJobReporterIsRoutedToTheRegistry scans the views for the messages
// that declare themselves a jobs.Reporter and checks the router has a case for
// each.
//
// Implementing the interface proves nothing on its own: Transition() is called
// by routeWork, and routeWork is called only from a case naming the message. A
// message without one is still delivered to its view, which is what hides the
// omission — the footer says the work finished while the registry keeps the run
// in progress. That is how a template's sync shipped, after the scan had been
// through the same mistake.
func TestEveryJobReporterIsRoutedToTheRegistry(t *testing.T) {
	appSrc, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	aliases := importAliases(t, "app.go")
	declaration := regexp.MustCompile(`(?m)^\s*_\s+jobs\.Reporter\s*=\s*(\w+)\{\}`)

	found := 0
	err = filepath.WalkDir(filepath.Join("..", "ui"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		matches := declaration.FindAllStringSubmatch(string(src), -1)
		if len(matches) == 0 {
			return nil
		}
		// ../ui/forge/explorer/jobs.go → internal/ui/forge/explorer
		pkg := "github.com/anthnel/devdesk/internal/" + strings.TrimPrefix(filepath.ToSlash(filepath.Dir(p)), "../")
		alias, ok := aliases[pkg]
		if !ok {
			t.Errorf("%s declares a jobs.Reporter but app.go does not import %s", p, pkg)
			return nil
		}
		for _, m := range matches {
			found++
			route := regexp.MustCompile(`(?m)^\s*case\b[^\n]*\b` + regexp.QuoteMeta(alias+"."+m[1]) + `\b`)
			if !route.Match(appSrc) {
				t.Errorf("%s: %s.%s is a jobs.Reporter with no case in app.go — its transition never reaches the registry and the run stays in progress", p, alias, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("found no jobs.Reporter declarations: the scan is looking in the wrong place")
	}
}

// importAliases maps an import path to the name the file refers to it by.
func importAliases(t *testing.T, file string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[p] = name
	}
	return out
}
