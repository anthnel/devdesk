package git

import "strings"

// RepoStatus is what a working copy says about itself without touching the
// network.
//
// It was read in two places that did not quite agree: the workspaces view
// counted modified and untracked files and both divergence directions with its
// own `execGit`, while this package counted the divergence again in
// `divergence` for the sync. A second consumer — the MCP server (§3.38) — is
// what made a third copy the obvious mistake to avoid.
type RepoStatus struct {
	// Branch is the checked-out branch, or "HEAD" when it is detached.
	Branch string

	// Remote is `origin` exactly as it is stored, SCP-like syntax included.
	// Turning it into something a user reads, or a browser opens, is the
	// caller's business.
	Remote string

	Modified  int
	Untracked int

	// HasUpstream says whether the branch tracks anything. Without it, Ahead
	// and Behind at zero mean both "in step with the remote" and "there is no
	// remote to be in step with", which is exactly the pair a caller has to
	// tell apart before reporting either.
	HasUpstream bool

	// Ahead is the number of local commits the upstream does not have.
	Ahead int

	// Behind is the number of upstream commits the working copy does not have —
	// and it is **only as fresh as the last fetch** (D35). It is read from
	// `@{u}`, the local remote-tracking ref, which nothing in this function
	// moves: a repository forty commits behind reads 0 until something fetches.
	// Sync is what makes it true, because it fetches first and always.
	Behind int
}

// ReadStatus reads a working copy. It runs no network operation, so what it
// reports about the upstream is as old as the last fetch — see RepoStatus.Behind.
//
// A field that cannot be read is left at its zero value rather than failing the
// whole read: a repository with no upstream, or a fresh one with no commits, is
// an ordinary repository and not an error. Only a path that is not a repository
// comes back as one.
func ReadStatus(repoPath string) (RepoStatus, error) {
	var status RepoStatus

	// The existence check is `rev-parse --git-dir` rather than reading HEAD,
	// because a repository that has just been initialised has no commit yet and
	// every question about HEAD fails on it. Gating on one of those questions is
	// what made a fresh repository report no untracked files — it has nothing
	// but untracked files.
	if _, err := run(repoPath, "", "rev-parse", "--git-dir"); err != nil {
		return status, err
	}

	status.Branch = branchOf(repoPath)

	if remote, err := RemoteURL(repoPath); err == nil {
		status.Remote = remote
	}

	if out, err := run(repoPath, "", "status", "--porcelain"); err == nil {
		status.Modified, status.Untracked = countPorcelain(out)
	}

	// A branch with no upstream makes rev-list fail rather than answer zero,
	// which is what distinguishes the two.
	if behind, ahead, err := divergence(repoPath); err == nil {
		status.HasUpstream = true
		status.Behind, status.Ahead = behind, ahead
	}

	return status, nil
}

// branchOf names the checked-out branch, including one that has no commit yet.
//
// symbolic-ref answers for an unborn HEAD, which rev-parse does not; rev-parse
// answers for a detached HEAD, which symbolic-ref does not. Both are ordinary
// states of a working copy, so both are asked.
func branchOf(repoPath string) string {
	if out, err := run(repoPath, "", "symbolic-ref", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	if out, err := run(repoPath, "", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

// countPorcelain splits `git status --porcelain` into what is tracked and
// changed, and what git has never seen.
func countPorcelain(out string) (modified, untracked int) {
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		if line[:2] == "??" {
			untracked++
			continue
		}
		modified++
	}
	return modified, untracked
}
