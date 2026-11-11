package themes

import (
	"path/filepath"
	"testing"
)

func TestFSEmbedsEveryThemeFile(t *testing.T) {
	files, err := filepath.Glob("*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v, %d files", err, len(files))
	}
	for _, f := range files {
		data, err := FS.ReadFile(f)
		if err != nil || len(data) == 0 {
			t.Errorf("FS.ReadFile(%q): %v (len %d)", f, err, len(data))
		}
	}
	if _, err := FS.ReadFile("github-light.json"); err != nil {
		t.Errorf("github-light.json must be embedded: %v", err)
	}
}
