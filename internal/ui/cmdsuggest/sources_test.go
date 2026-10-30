package cmdsuggest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPackageSourcesHonorTheThemeRules fails naming file and line when the
// package styles itself outside the theme (Rules 115, 117, 119) or uses the
// dim colour for information.
func TestPackageSourcesHonorTheThemeRules(t *testing.T) {
	forbidden := []string{`lipgloss.Color("#`, "JoinHorizontal", "JoinVertical", "ColorDim", "DimStyle"}
	inlineHelp := regexp.MustCompile(`\[[^\]\s]+\] [a-z]+`)
	files, _ := filepath.Glob("*.go")
	if len(files) == 0 {
		t.Fatal("no sources found")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, bad := range forbidden {
				if strings.Contains(line, bad) {
					t.Errorf("%s:%d uses %s", f, i+1, bad)
				}
			}
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if inlineHelp.MatchString(line) && strings.Contains(line, `"`) {
				t.Errorf("%s:%d looks like a [key] action help text", f, i+1)
			}
		}
	}
}
