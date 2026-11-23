package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStateOfATrackedCleanFile(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine\n")
	commit(t, repo)

	got, err := StateOf(filepath.Join(repo, "Dockerfile"))
	if err != nil || got != FileClean {
		t.Errorf("StateOf = %v, %v; want FileClean", got, err)
	}
}

func TestStateOfATrackedFileWithUncommittedChanges(t *testing.T) {
	repo := initRepo(t)
	path := filepath.Join(repo, "Dockerfile")
	write(t, path, "FROM alpine\n")
	commit(t, repo)
	write(t, path, "FROM alpine:3.18\n")

	got, err := StateOf(path)
	if err != nil || got != FileModified {
		t.Errorf("StateOf = %v, %v; want FileModified", got, err)
	}
}

func TestStateOfAFileGitHasNeverSeen(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "README"), "x\n")
	commit(t, repo)
	path := filepath.Join(repo, "Dockerfile")
	write(t, path, "FROM alpine\n")

	got, err := StateOf(path)
	if err != nil || got != FileUntracked {
		t.Errorf("StateOf = %v, %v; want FileUntracked", got, err)
	}
}

// An ignored file is in the working copy and out of git's history: no safety net.
func TestStateOfAnIgnoredFileIsUntracked(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, ".gitignore"), "Dockerfile\n")
	commit(t, repo)
	path := filepath.Join(repo, "Dockerfile")
	write(t, path, "FROM alpine\n")

	if got, err := StateOf(path); err != nil || got != FileUntracked {
		t.Errorf("StateOf = %v, %v; want FileUntracked", got, err)
	}
}

func TestStateOfAFileOutsideAnyRepository(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	write(t, path, "FROM alpine\n")

	got, err := StateOf(path)
	if err != nil || got != FileOutsideRepo {
		t.Errorf("StateOf = %v, %v; want FileOutsideRepo", got, err)
	}
}

func TestStateOfAFileInASubdirectory(t *testing.T) {
	repo := initRepo(t)
	sub := filepath.Join(repo, "svc", "api")
	if err := mkdir(sub); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "Dockerfile")
	write(t, path, "FROM alpine\n")
	commit(t, repo)

	if got, err := StateOf(path); err != nil || got != FileClean {
		t.Errorf("StateOf = %v, %v; want FileClean", got, err)
	}
}

func mkdir(path string) error { return os.MkdirAll(path, 0o755) }

// trapRepo is a committed repository whose own configuration would run a script
// the moment git reads it: a hostile checkout the user only meant to look at.
// The script writes marker; the tests assert that it does not.
func trapRepo(t *testing.T) (repo, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the trap is a POSIX shell script")
	}
	repo = initRepo(t)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine\n")
	commit(t, repo)

	marker = filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "trap.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "core.fsmonitor", script)
	hooks := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "post-index-change"), []byte("#!/bin/sh\necho ran > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return repo, marker
}

// The scanned repository is not trusted: opening a finding's detail must not run
// what its .git/config names (M1).
func TestStateOfDoesNotRunTheFsmonitorOfTheRepository(t *testing.T) {
	repo, marker := trapRepo(t)
	// Make the index stale so that git has a reason to consult the monitor.
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine:3.18\n")

	if got, err := StateOf(filepath.Join(repo, "Dockerfile")); err != nil || got != FileModified {
		t.Fatalf("StateOf = %v, %v; want FileModified", got, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("core.fsmonitor of the scanned repository ran")
	}
}

// A read-only command is hardened: an index write fires post-index-change, a
// hook of the repository.
func TestRunReadOnlyDoesNotRunTheHooksOfTheRepository(t *testing.T) {
	repo, marker := trapRepo(t)
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine:3.18\n")

	// It exits 1 on a modified file; only its side effects matter here.
	_, _ = runReadOnly(context.Background(), repo, "update-index", "--refresh")
	if _, err := os.Stat(marker); err == nil {
		t.Error("a hook of the repository ran")
	}
}

// The hardening is for the read-only paths only. The same command through run,
// which Sync, pull and fetch use, still fires the hook: this also proves the
// trap above can fire at all.
func TestRunKeepsTheHooksOfTheRepository(t *testing.T) {
	repo, marker := trapRepo(t)
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine:3.18\n")

	_, _ = run(repo, "", "update-index", "--refresh")
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("run disabled the hooks of the repository: %v", err)
	}
}

// A git that does not answer is abandoned instead of holding the detail open.
func TestStateOfGivesUpWhenGitDoesNotAnswerInTime(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "Dockerfile"), "FROM alpine\n")
	commit(t, repo)
	old := stateTimeout
	stateTimeout = time.Nanosecond
	t.Cleanup(func() { stateTimeout = old })

	if _, err := StateOf(filepath.Join(repo, "Dockerfile")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("StateOf error = %v; want context.DeadlineExceeded", err)
	}
}
