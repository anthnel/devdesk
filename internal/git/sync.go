package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Sync brings one repository up to date with its upstream (§3.17).
//
// It is the counterpart of Clone: the explorer creates what is missing, this
// reconciles what exists. Three rules shape it, and each is a refusal rather
// than a cleverness.
//
//  1. **It fetches first, always, whatever the working tree looks like.** The
//     "unpulled" count DevDesk shows comes from `@{u}`, the *local*
//     remote-tracking ref, which nothing moved until now — so it read 0 on a
//     repository forty commits behind (D35). A sync that decided from that
//     number would be reconciling against an answer it had not checked.
//
//  2. **It only fast-forwards.** No merge commit, no rebase, no stash. A
//     divergence is a decision about someone's unpublished work, and a tool
//     that guesses at it can destroy hours in a keystroke the user cannot undo.
//
//  3. **It never pushes.** Sync is the pull direction. Publishing is a separate
//     intent with separate failure modes, and nothing about "reconcile what
//     exists" implies it.
//
// A repository it declines is still better off than before: the fetch happened,
// so its counts are true and the row finally says how far behind it really is.
func Sync(repoPath string, opts SyncOptions) (SyncResult, error) {
	if _, err := run(repoPath, opts.Token, "fetch", "--quiet", "--prune"); err != nil {
		return SyncResult{}, err
	}

	// Order matters below: a repository can be several of these at once, and
	// the first answer is the one that explains why nothing moved.
	if _, err := run(repoPath, "", "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return skipped("detached HEAD"), nil
	}
	upstream, err := run(repoPath, "", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return skipped("no upstream branch"), nil
	}

	behind, ahead, err := divergence(repoPath)
	if err != nil {
		return SyncResult{}, err
	}

	result := SyncResult{Behind: behind, Ahead: ahead, Upstream: strings.TrimSpace(upstream)}

	// Nothing to pull. Local commits waiting to be pushed do not change that:
	// sync is the pull direction, and rule 3 leaves them alone.
	if behind == 0 {
		result.Outcome = SyncUpToDate
		return result, nil
	}
	if ahead > 0 {
		result.Outcome = SyncSkipped
		result.Reason = fmt.Sprintf("diverged — %s ahead", plural(ahead, "commit"))
		return result, nil
	}
	if dirty, err := isDirty(repoPath); err != nil {
		return SyncResult{}, err
	} else if dirty {
		result.Outcome = SyncSkipped
		result.Reason = "uncommitted changes"
		return result, nil
	}

	if _, err := run(repoPath, "", "merge", "--ff-only", "--quiet", "@{u}"); err != nil {
		return SyncResult{}, err
	}
	result.Outcome = SyncUpdated
	return result, nil
}

// RemoteURL returns a repository's `origin` URL, in whatever form it is stored.
//
// The caller needs it to decide which credential, if any, may be offered to
// this repository — a question this package deliberately does not answer.
func RemoteURL(repoPath string) (string, error) {
	out, err := run(repoPath, "", "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SyncOptions is what a sync needs beyond the path.
type SyncOptions struct {
	// Token authenticates the fetch over HTTPS. It must be a credential for the
	// repository's own remote host: the caller decides, because this package
	// cannot know which forge a working copy on disk came from. Empty means the
	// fetch is expected to succeed without one.
	Token string
}

// SyncOutcome is what a sync did to one repository.
type SyncOutcome int

const (
	// SyncUpToDate: the fetch found nothing to pull.
	SyncUpToDate SyncOutcome = iota
	// SyncUpdated: the branch was fast-forwarded.
	SyncUpdated
	// SyncSkipped: fetched, but not merged. Reason says why, and it is never a
	// failure — the repository is in the state its owner left it in.
	SyncSkipped
)

// SyncResult is what one repository's sync established.
type SyncResult struct {
	Outcome SyncOutcome
	// Behind and Ahead are counted after the fetch, so unlike the workspaces
	// listing's own columns they are current.
	Behind   int
	Ahead    int
	Upstream string
	Reason   string
}

func skipped(reason string) SyncResult {
	return SyncResult{Outcome: SyncSkipped, Reason: reason}
}

// divergence counts what separates HEAD from its upstream, in both directions.
func divergence(repoPath string) (behind, ahead int, err error) {
	out, err := run(repoPath, "", "rev-list", "--count", "--left-right", "@{u}...HEAD")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unreadable rev-list output: %q", strings.TrimSpace(out))
	}
	if behind, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("unreadable behind count: %w", err)
	}
	if ahead, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("unreadable ahead count: %w", err)
	}
	return behind, ahead, nil
}

// isDirty reports whether the working tree has anything uncommitted, untracked
// files included: a fast-forward can refuse to overwrite one, and a refusal is
// better reported before the merge than as git's error after it.
func isDirty(repoPath string) (bool, error) {
	out, err := run(repoPath, "", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// run executes one git command in a repository and returns its stdout.
//
// It carries the same non-interactive environment as Clone. A `git fetch` is a
// network call like any other, and left to itself it reaches the credential
// helper — which on Windows writes to the console over the rendered frame and
// then waits on a browser. That was §3.16's observed failure, and nothing about
// it was specific to cloning.
func run(repoPath, token string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv(token)

	var stdout, stderr strings.Builder
	cmd.Stdin = nil
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if reason := lastLine(stderr.String()); reason != "" {
			return "", fmt.Errorf("%s", reason)
		}
		return "", err
	}
	return stdout.String(), nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
