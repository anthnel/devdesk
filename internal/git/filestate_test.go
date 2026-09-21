package git

import (
	"os"
	"path/filepath"
	"testing"
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
