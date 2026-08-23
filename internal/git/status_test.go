package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A repository that has just been initialised has no commit, so every question
// about HEAD fails on it — and it is the state in which a repository has nothing
// *but* untracked files. Gating the read on one of those questions reported it
// as having none, which is the opposite of true.
func TestARepositoryWithNoCommitStillReportsItsFiles(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "README.md"), "# hello\n")

	status, err := ReadStatus(repo)
	if err != nil {
		t.Fatalf("ReadStatus on a fresh repository: %v", err)
	}

	if status.Untracked != 1 {
		t.Errorf("untracked = %d, want the one file that was written", status.Untracked)
	}
	if status.Branch == "" {
		t.Error("branch is empty — symbolic-ref answers for an unborn HEAD even though rev-parse does not")
	}
	if status.HasUpstream {
		t.Error("has_upstream = true for a repository with no remote")
	}
}

// Ahead and Behind at zero mean both "in step" and "there is nothing to be in
// step with", so HasUpstream is what tells a caller which it is looking at.
func TestABranchWithNoUpstreamSaysSo(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "a\n")
	commit(t, repo)

	status, err := ReadStatus(repo)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}

	if status.HasUpstream {
		t.Error("has_upstream = true with no remote configured")
	}
	if status.Modified != 0 || status.Untracked != 0 {
		t.Errorf("a committed tree reports %d modified / %d untracked, want none",
			status.Modified, status.Untracked)
	}
}

func TestModifiedAndUntrackedAreCountedApart(t *testing.T) {
	repo := initRepo(t)
	write(t, filepath.Join(repo, "tracked.txt"), "one\n")
	commit(t, repo)
	write(t, filepath.Join(repo, "tracked.txt"), "two\n")
	write(t, filepath.Join(repo, "new.txt"), "new\n")

	status, err := ReadStatus(repo)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}

	if status.Modified != 1 {
		t.Errorf("modified = %d, want the one tracked file that changed", status.Modified)
	}
	if status.Untracked != 1 {
		t.Errorf("untracked = %d, want the one file git has never seen", status.Untracked)
	}
}

func TestAPathThatIsNotARepositoryIsAnError(t *testing.T) {
	if _, err := ReadStatus(t.TempDir()); err == nil {
		t.Error("ReadStatus accepted a directory that is not a repository")
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	return dir
}

func commit(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "commit")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
