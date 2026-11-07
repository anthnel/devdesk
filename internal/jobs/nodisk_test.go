package jobs

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTheRegistryNeverTouchesTheDisk guards criterion 5 of DEVDESK-138: the
// failure memory lives in this process only. The non-test files of the package
// must not import anything that reads or writes files or the config/caches.
func TestTheRegistryNeverTouchesTheDisk(t *testing.T) {
	forbidden := map[string]bool{
		"os":        true,
		"io/ioutil": true,
		"github.com/anthnel/devdesk/internal/cache":  true,
		"github.com/anthnel/devdesk/internal/config": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if forbidden[path] {
				t.Errorf("%s imports %q: the failure memory must never be persisted (DEVDESK-138)", file, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test file scanned: the guard would pass on nothing")
	}
}
