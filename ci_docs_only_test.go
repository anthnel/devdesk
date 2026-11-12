package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docsOnlyScript returns the absolute path of the CI classifier script. The
// test fails (it does not skip) when the script is missing: that is the point
// of the guard.
func docsOnlyScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not in PATH")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(wd, ".github", "scripts", "docs-only.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("docs-only script missing: %v", err)
	}
	return script
}

// runDocsOnly runs the script and returns stdout and the exit code.
func runDocsOnly(t *testing.T, script, dir, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running script: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

func nul(paths ...string) string {
	return strings.Join(paths, "\x00") + "\x00"
}

func TestDocsOnlyClassify(t *testing.T) {
	script := docsOnlyScript(t)
	tests := []struct {
		name  string
		stdin string
		want  string
	}{
		{"docs markdown", nul("docs/backlog.md"), "true"},
		{"docs image", nul("docs/ux/a.png"), "true"},
		{"root README", nul("README.md"), "true"},
		{"claude rules md", nul(".claude/rules/tui.md"), "true"},
		{"nested README", nul("internal/x/README.md"), "true"},
		{"space in docs path", nul("docs/a b.txt"), "true"},
		{"newline in docs path", nul("docs/a\nb.txt"), "true"},
		{"accent under docs", nul("docs/é.png"), "true"},
		{"accent in md", nul("internal/é.md"), "true"},
		{"last entry without NUL", "docs/backlog.md", "true"},
		{"several docs without trailing NUL", "docs/a.md\x00README.md", "true"},
		{"empty input", "", "false"},
		{"only a NUL", "\x00", "false"},
		{"go file", nul("main.go"), "false"},
		{"mixed docs and code", nul("docs/backlog.md", "main.go"), "false"},
		{"code without NUL", "main.go", "false"},
		{"CHANGELOG alone", nul("CHANGELOG.md"), "false"},
		{"CHANGELOG and manifest", nul("CHANGELOG.md", ".release-please-manifest.json"), "false"},
		{"uppercase extension", nul("README.MD"), "false"},
		{"markdown extension", nul("a.markdown"), "false"},
		{"mydocs prefix", nul("mydocs/a.txt"), "false"},
		{"nested docs dir", nul("x/docs/a.txt"), "false"},
		{"md under testdata", nul("internal/agentusage/testdata/README.md"), "false"},
		{"file under docs/testdata", nul("docs/testdata/a.png"), "false"},
		{"app-shell doc", nul("docs/architecture/app-shell.md"), "false"},
		{"docs-site", nul("docs-site/mkdocs.yml"), "false"},
		{"the script itself", nul(".github/scripts/docs-only.sh"), "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runDocsOnly(t, script, t.TempDir(), tt.stdin, "classify")
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if out != tt.want+"\n" && out != tt.want {
				t.Fatalf("stdout = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestDocsOnlyUnknownArgument(t *testing.T) {
	script := docsOnlyScript(t)
	for _, args := range [][]string{{"bogus"}, {}} {
		out, code := runDocsOnly(t, script, t.TempDir(), nul("docs/a.md"), args...)
		if code != 2 {
			t.Errorf("args %v: exit code = %d, want 2", args, code)
		}
		if out != "" {
			t.Errorf("args %v: stdout = %q, want empty", args, out)
		}
	}
}

// git runs git in dir with a hermetic configuration.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=Test", "-c", "user.email=test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repo with one base commit on main.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, "internal/a.go", "package internal\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestDocsOnlyDiff(t *testing.T) {
	script := docsOnlyScript(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	// merged builds a PR-like merge commit: a branch with the given edit,
	// merged into main with --no-ff (a second main commit makes it a true merge).
	merged := func(t *testing.T, edit func(dir string)) string {
		dir := newRepo(t)
		gitIn(t, dir, "checkout", "-q", "-b", "feature")
		edit(dir)
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", "feature")
		gitIn(t, dir, "checkout", "-q", "main")
		gitIn(t, dir, "merge", "-q", "--no-ff", "-m", "merge", "feature")
		return dir
	}
	tests := []struct {
		name string
		repo func(t *testing.T) string
		want string
	}{
		{"docs-only branch", func(t *testing.T) string {
			return merged(t, func(d string) {
				writeFile(t, d, "docs/new.md", "x\n")
				writeFile(t, d, "docs/img.png", "x\n")
			})
		}, "true"},
		{"mixed branch", func(t *testing.T) string {
			return merged(t, func(d string) {
				writeFile(t, d, "docs/new.md", "x\n")
				writeFile(t, d, "main.go", "package main\n// changed\n")
			})
		}, "false"},
		{"rename from code to docs", func(t *testing.T) string {
			return merged(t, func(d string) {
				// internal/a.go is committed on the base by newRepo; git mv
				// does not create the destination directory.
				if err := os.MkdirAll(filepath.Join(d, "docs"), 0o755); err != nil {
					t.Fatal(err)
				}
				gitIn(t, d, "mv", "internal/a.go", "docs/a.go")
			})
		}, "false"},
		{"HEAD without second parent", func(t *testing.T) string {
			d := newRepo(t)
			writeFile(t, d, "docs/new.md", "x\n")
			gitIn(t, d, "add", "-A")
			gitIn(t, d, "commit", "-q", "-m", "docs")
			return d
		}, "false"},
		{"single commit repo", newRepo, "false"},
		{"not a git directory", func(t *testing.T) string { return t.TempDir() }, "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.repo(t)
			out, code := runDocsOnly(t, script, dir, "", "diff")
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if out != tt.want+"\n" && out != tt.want {
				t.Fatalf("stdout = %q, want %q", out, tt.want)
			}
		})
	}
}
