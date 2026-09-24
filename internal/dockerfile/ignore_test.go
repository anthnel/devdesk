package dockerfile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExcluded(t *testing.T) {
	for _, tt := range []struct {
		name, ignore, path string
		excluded, certain  bool
	}{
		{"no file says nothing", "", ".env", false, true},
		{"a plain name", ".env\n", ".env", true, true},
		{"a leading slash is dropped", "/.env\n", ".env", true, true},
		{"patterns are anchored at the root", ".env\n", "api/.env", false, true},
		{"** reaches any depth", "**/.env\n", "api/v1/.env", true, true},
		{"* stays in one segment", "*.pem\n", "certs/tls.pem", false, true},
		{"a directory takes its contents", "certs\n", "certs/tls.pem", true, true},
		{"the last match wins", "*.pem\n!tls.pem\n", "tls.pem", false, true},
		{"a negation can be overridden again", "!tls.pem\n*.pem\n", "tls.pem", true, true},
		{"comments and blanks are skipped", "# .env\n\n   \n", ".env", false, true},
		{"CRLF lines", ".env\r\n", ".env", true, true},
		{"a byte-order mark", "\xEF\xBB\xBF.env\n", ".env", true, true},
		{"an unreadable pattern makes nothing certain", "[\n", ".env", false, false},
		{"so does ** inside a segment", "a**b\n", ".env", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ex, certain := ParseIgnore([]byte(tt.ignore)).Excluded(tt.path)
			if ex != tt.excluded || certain != tt.certain {
				t.Errorf("Excluded(%q) = %v, %v; want %v, %v", tt.path, ex, certain, tt.excluded, tt.certain)
			}
		})
	}
}

// A directory is left out only if nothing brings part of it back: that is
// what separates "the history is not in the image" from "some of it is".
func TestExcludesTree(t *testing.T) {
	for _, tt := range []struct {
		name, ignore      string
		excluded, certain bool
	}{
		{"not mentioned", "node_modules\n", false, true},
		{"excluded", ".git\n", true, true},
		{"with a trailing slash", ".git/\n", true, true},
		{"anywhere", "**/.git\n", true, true},
		{"by a wildcard", ".*\n", true, true},
		{"part of it brought back", ".git\n!.git/config\n", false, false},
		{"brought back by a pattern that may reach it", ".git\n!**/config\n", false, false},
		{"a negation elsewhere does not matter", ".git\n!src/keep\n", true, true},
		{"a negation the last exclusion overrides", ".git\n!.git/config\n.git\n", true, true},
		{"re-included whole", ".git\n!.git\n", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ex, certain := ParseIgnore([]byte(tt.ignore)).ExcludesTree(".git")
			if ex != tt.excluded || certain != tt.certain {
				t.Errorf("ExcludesTree(.git) = %v, %v; want %v, %v", ex, certain, tt.excluded, tt.certain)
			}
		})
	}
}

// BuildKit reads the Dockerfile's own ignore file, the legacy builder the
// context's; both are returned when both exist, since which one applies is not
// in the files.
func TestIgnoreFiles(t *testing.T) {
	dir := t.TempDir()
	if got := IgnoreFiles(dir, "Dockerfile"); len(got) != 0 {
		t.Errorf("empty directory: %v", got)
	}
	for _, name := range []string{".dockerignore", "Dockerfile.dockerignore"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := IgnoreFiles(dir, "Dockerfile"), []string{"Dockerfile.dockerignore", ".dockerignore"}; !slices.Equal(got, want) {
		t.Errorf("IgnoreFiles = %v, want %v", got, want)
	}
	if got, want := IgnoreFiles(dir, "Dockerfile.dev"), []string{".dockerignore"}; !slices.Equal(got, want) {
		t.Errorf("another Dockerfile's ignore file applied: %v, want %v", got, want)
	}
}
