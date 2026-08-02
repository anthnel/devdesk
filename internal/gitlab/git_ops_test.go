package gitlab

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireGit skips the test when no git binary is available.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// seedRepo creates a git repository holding a single committed file and returns
// its path. Identity and signing are forced on the command line so the test does
// not depend on the machine's git configuration.
func seedRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating source dir: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# seed\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=devdesk", "-c", "user.email=devdesk@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "seed")

	return dir
}

func TestCloneCopiesRepositoryContents(t *testing.T) {
	requireGit(t)
	source := seedRepo(t)
	target := filepath.Join(t.TempDir(), "clone")

	if err := Clone(source, target); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("cloned repo is missing README.md: %v", err)
	}
	if !DirExists(filepath.Join(target, ".git")) {
		t.Error("cloned repo has no .git directory")
	}
}

func TestCloneFailsOnMissingSource(t *testing.T) {
	requireGit(t)
	source := filepath.Join(t.TempDir(), "does-not-exist")
	target := filepath.Join(t.TempDir(), "clone")

	if err := Clone(source, target); err == nil {
		t.Error("Clone() from a missing source returned no error")
	}
}

func TestCloneFailsWhenTargetIsNotEmpty(t *testing.T) {
	requireGit(t)
	source := seedRepo(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing existing file: %v", err)
	}

	if err := Clone(source, target); err == nil {
		t.Error("Clone() into a non-empty directory returned no error")
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"existing directory", dir, true},
		{"nested missing path", filepath.Join(dir, "nope", "deeper"), false},
		{"a file is not a directory", file, false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DirExists(tt.path); got != tt.want {
				t.Errorf("DirExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
