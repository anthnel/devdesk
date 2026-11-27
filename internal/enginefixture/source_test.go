package enginefixture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), step 13: where the source tree is, for the importer (the
// embedded copy is read-only), on the model of nexusfixture.SourceDir.
//
//	func SourceDir() (string, error)          // <repo>/internal/enginefixture/fixtures
//	func VersionsSourceFile() (string, error) // <repo>/tools/engine-vms/versions.env
//
// Both come from runtime.Caller. Under -trimpath that path is module-relative, so
// each then answers an empty path and an error that names the source tree: neither
// ever answers with a path that is not there.

func TestSourceDirIsTheFixturesDirectoryOfTheSourceTree(t *testing.T) {
	dir, err := enginefixture.SourceDir()
	if err != nil {
		t.Fatalf("SourceDir: %v", err)
	}
	want, err := filepath.Abs("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	if dir != want {
		t.Errorf("SourceDir() = %q, want %q (the directory this package's tests run in)", dir, want)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), "internal/enginefixture/fixtures") {
		t.Errorf("SourceDir() = %q, want a path ending in internal/enginefixture/fixtures", dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("SourceDir() = %q is not an existing directory: %v", dir, err)
	}
}

func TestVersionsSourceFileIsTheVersionsFileOfTheRepository(t *testing.T) {
	file, err := enginefixture.VersionsSourceFile()
	if err != nil {
		t.Fatalf("VersionsSourceFile: %v", err)
	}
	if !filepath.IsAbs(file) || !strings.HasSuffix(filepath.ToSlash(file), "tools/engine-vms/versions.env") {
		t.Errorf("VersionsSourceFile() = %q, want an absolute path ending in tools/engine-vms/versions.env", file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the file VersionsSourceFile() names cannot be read: %v", err)
	}
	if _, err := enginefixture.ParseVersions(data); err != nil {
		t.Errorf("the file VersionsSourceFile() names is not a versions file: %v", err)
	}
}

// The contract of both functions, which have no argument to vary: an existing
// absolute path, or an empty one with an error that says the source tree cannot be
// located. Run with -trimpath to see the second branch.
func TestTheSourceLocatorsNeverAnswerAPathThatIsNotThere(t *testing.T) {
	for name, locate := range map[string]func() (string, error){
		"SourceDir":          enginefixture.SourceDir,
		"VersionsSourceFile": enginefixture.VersionsSourceFile,
	} {
		t.Run(name, func(t *testing.T) {
			p, err := locate()
			if err != nil {
				if p != "" {
					t.Errorf("%s() = %q with error %v, want an empty path with an error", name, p, err)
				}
				if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "source tree") {
					t.Errorf("error %q must say the source tree cannot be located", err)
				}
				return
			}
			if !filepath.IsAbs(p) {
				t.Errorf("%s() = %q with no error: not an absolute path", name, p)
			}
			if _, statErr := os.Stat(p); statErr != nil {
				t.Errorf("%s() = %q with no error: it does not exist (%v)", name, p, statErr)
			}
		})
	}
}
