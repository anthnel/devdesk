package credentials

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// safeHelper writes a credential helper that reads its input and keeps
// nothing, so a test can make git accept a backend without any real store.
func safeHelper(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "safe-helper.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("writing helper: %v", err)
	}
	return p
}

// hostileRepo creates a repository whose local configuration adds a helper
// that copies whatever git hands it into a file, and makes it the working
// directory. It returns the path of that file.
func hostileRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	leak := filepath.Join(dir, "leak.txt")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "config", "--local", "--add", "credential.helper", "!f() { cat >> " + leak + "; }; f"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(repo)
	return leak
}

func requireNoLeak(t *testing.T, leak string) {
	t.Helper()
	if data, err := os.ReadFile(leak); err == nil {
		t.Errorf("the repository's own helper ran and received:\n%s", data)
	}
}

// A repository DevDesk is started in must not choose or run its helper: its
// .git/config is not the user's configuration, and a helper there is a shell
// command of the repository's author (DEVDESK-745).
func TestGitIgnoresTheHelperOfTheWorkingDirectoryRepository(t *testing.T) {
	t.Run("the local helper alone is not a backend", func(t *testing.T) {
		hermeticGitWithHelper(t, "store")
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty"))
		leak := hostileRepo(t)

		if name, ok := gitHelperUsable(); ok {
			t.Errorf("gitHelperUsable() = %q, true: the repository's helper was selected", name)
		}
		requireNoLeak(t, leak)
	})

	t.Run("save and load do not run the local helper", func(t *testing.T) {
		hermeticGitWithHelper(t, safeHelper(t))
		leak := hostileRepo(t)

		if _, ok := gitHelperUsable(); !ok {
			t.Fatal("gitHelperUsable() = false with a safe global helper")
		}
		g := NewGitCredentialStorageWithContext("work")
		_ = g.Save("https://gitlab.example.com", "glpat-hostile-check")
		_, _ = g.Load("https://gitlab.example.com")
		_ = g.Delete("https://gitlab.example.com")
		requireNoLeak(t, leak)
	})
}

// The user's own GIT_CEILING_DIRECTORIES is kept, with DevDesk's appended.
func TestGitEnvironmentKeepsTheUserCeiling(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", "/user/ceiling")
	env := gitEnv("/tmp/devdesk-git-x")

	want := "GIT_CEILING_DIRECTORIES=/user/ceiling" + string(os.PathListSeparator) + "/tmp"
	found := false
	for _, kv := range env {
		if kv == want {
			found = true
		}
	}
	if !found {
		t.Errorf("environment does not carry %q", want)
	}
}

// git writes what it could not parse to stderr, and a helper may too: the
// error of a git credential call must never carry it, since it ends up in
// notes and logs. A token with a newline makes git echo its second half
// ("invalid credential line: ...").
func TestGitCredentialErrorsNeverCarryTheSecret(t *testing.T) {
	hermeticGitWithHelper(t, safeHelper(t))
	const secretPart = "SECRETPART-n4"

	err := NewGitCredentialStorageWithContext("work").Save("https://gitlab.example.com", "glpat-abc\n"+secretPart)
	if err == nil {
		t.Fatal("Save() of a token with a newline succeeded; the test no longer reaches the failure path")
	}
	if strings.Contains(err.Error(), secretPart) {
		t.Errorf("Save() error = %q, carries the secret", err)
	}
}
