package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The sync tests run against real repositories on local remotes. A fake would
// have to model exactly the thing under test — what git considers a
// fast-forward, and what it considers dirty — so it would only ever confirm the
// author's idea of those rules.

// gitIn runs a git command in dir and fails the test if it errors.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=devdesk", "-c", "user.email=devdesk@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// commitFile writes a file and commits it.
func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-m", "add "+name)
}

// clonePair builds an upstream repository and a working copy of it, and returns
// both paths. The working copy is a real clone, so it has the upstream tracking
// ref Sync reads.
func clonePair(t *testing.T) (upstream, working string) {
	t.Helper()
	requireGit(t)

	root := t.TempDir()
	upstream = filepath.Join(root, "upstream")
	working = filepath.Join(root, "working")

	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatalf("creating upstream: %v", err)
	}
	gitIn(t, upstream, "init", "--initial-branch=main")
	commitFile(t, upstream, "README.md", "# seed\n")

	if err := Clone(upstream, working, CloneOptions{}); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	return upstream, working
}

func TestSyncFastForwardsABehindRepository(t *testing.T) {
	upstream, working := clonePair(t)
	commitFile(t, upstream, "second.txt", "two\n")

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncUpdated {
		t.Errorf("Outcome = %v, want SyncUpdated (reason %q)", result.Outcome, result.Reason)
	}
	if result.Behind != 1 {
		t.Errorf("Behind = %d, want 1", result.Behind)
	}
	if _, err := os.Stat(filepath.Join(working, "second.txt")); err != nil {
		t.Errorf("the new commit's file is not in the working copy: %v", err)
	}
}

func TestSyncReportsARepositoryAlreadyAtItsUpstream(t *testing.T) {
	_, working := clonePair(t)

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncUpToDate {
		t.Errorf("Outcome = %v, want SyncUpToDate", result.Outcome)
	}
}

// Unpushed commits are not the pull direction's business. A repository with
// local work and nothing incoming is up to date as far as sync is concerned —
// rule 3: sync never pushes.
func TestSyncLeavesUnpushedCommitsAloneAndCallsItUpToDate(t *testing.T) {
	_, working := clonePair(t)
	commitFile(t, working, "local.txt", "mine\n")

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncUpToDate {
		t.Errorf("Outcome = %v, want SyncUpToDate (reason %q)", result.Outcome, result.Reason)
	}
	if result.Ahead != 1 {
		t.Errorf("Ahead = %d, want the local commit counted", result.Ahead)
	}
}

// Commits on both sides cannot fast-forward, and the alternatives — a merge
// commit, a rebase — are decisions about someone's unpublished work. Refuse.
func TestSyncRefusesADivergedBranch(t *testing.T) {
	upstream, working := clonePair(t)
	commitFile(t, upstream, "theirs.txt", "them\n")
	commitFile(t, working, "mine.txt", "me\n")

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncSkipped {
		t.Fatalf("Outcome = %v, want SyncSkipped", result.Outcome)
	}
	if !strings.Contains(result.Reason, "diverged") {
		t.Errorf("Reason = %q, want it to name the divergence", result.Reason)
	}
	if result.Behind != 1 || result.Ahead != 1 {
		t.Errorf("Behind/Ahead = %d/%d, want 1/1", result.Behind, result.Ahead)
	}
}

func TestSyncRefusesADirtyWorkingTree(t *testing.T) {
	upstream, working := clonePair(t)
	commitFile(t, upstream, "second.txt", "two\n")
	if err := os.WriteFile(filepath.Join(working, "README.md"), []byte("# edited\n"), 0o600); err != nil {
		t.Fatalf("dirtying the working copy: %v", err)
	}

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncSkipped {
		t.Fatalf("Outcome = %v, want SyncSkipped", result.Outcome)
	}
	if !strings.Contains(result.Reason, "uncommitted") {
		t.Errorf("Reason = %q, want it to name the uncommitted changes", result.Reason)
	}
	// The refusal must not have cost the user their edit.
	content, err := os.ReadFile(filepath.Join(working, "README.md"))
	if err != nil || string(content) != "# edited\n" {
		t.Errorf("the working copy's edit did not survive the sync: %q, %v", content, err)
	}
}

// The whole point of D35: the count DevDesk shows comes from the local tracking
// ref, and nothing moved it. A repository it declines must still come out of a
// sync knowing how far behind it is, or the refusal has taught the user nothing.
func TestARefusedSyncStillFetchedAndKnowsHowFarBehindItIs(t *testing.T) {
	upstream, working := clonePair(t)
	commitFile(t, upstream, "a.txt", "1\n")
	commitFile(t, upstream, "b.txt", "2\n")
	if err := os.WriteFile(filepath.Join(working, "README.md"), []byte("# edited\n"), 0o600); err != nil {
		t.Fatalf("dirtying the working copy: %v", err)
	}

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Behind != 2 {
		t.Errorf("Behind = %d, want 2 — the fetch runs whatever the tree looks like", result.Behind)
	}
}

func TestSyncSkipsARepositoryWithNoUpstream(t *testing.T) {
	requireGit(t)
	dir := seedRepo(t)

	result, err := Sync(dir, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncSkipped {
		t.Fatalf("Outcome = %v, want SyncSkipped", result.Outcome)
	}
	if !strings.Contains(result.Reason, "upstream") {
		t.Errorf("Reason = %q, want it to name the missing upstream", result.Reason)
	}
}

func TestSyncSkipsADetachedHead(t *testing.T) {
	_, working := clonePair(t)
	head := strings.TrimSpace(gitIn(t, working, "rev-parse", "HEAD"))
	gitIn(t, working, "checkout", "--detach", head)

	result, err := Sync(working, SyncOptions{})

	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Outcome != SyncSkipped {
		t.Fatalf("Outcome = %v, want SyncSkipped", result.Outcome)
	}
	if !strings.Contains(result.Reason, "detached") {
		t.Errorf("Reason = %q, want it to name the detached HEAD", result.Reason)
	}
}

// A remote that is gone is an error, not a skip: the difference is whether the
// repository is in the state its owner left it in, or whether DevDesk could not
// find out.
func TestSyncFailsWhenTheRemoteIsUnreachable(t *testing.T) {
	upstream, working := clonePair(t)
	if err := os.RemoveAll(upstream); err != nil {
		t.Fatalf("removing the upstream: %v", err)
	}

	_, err := Sync(working, SyncOptions{})

	if err == nil {
		t.Fatal("Sync() against a missing remote returned no error")
	}
	if strings.HasPrefix(err.Error(), "exit status") {
		t.Errorf("Sync() reported only an exit status: %v", err)
	}
}

// Sync's subprocesses run under the same environment as Clone's. A fetch is a
// network call, and left to itself it reaches the credential helper — the
// console-corrupting, browser-opening failure §3.16 shipped a fix for.
func TestSyncRunsGitNonInteractively(t *testing.T) {
	_, working := clonePair(t)

	// A repository whose remote needs credentials must fail rather than wait.
	// file:// with a path that does not exist is the cheapest stand-in for a
	// remote git cannot reach without asking.
	gitIn(t, working, "remote", "set-url", "origin", "https://127.0.0.1:1/private.git")

	done := make(chan error, 1)
	go func() {
		_, err := Sync(working, SyncOptions{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Sync() against an unreachable private remote returned no error")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Sync() blocked — something in the git environment is still prompting")
	}
}
