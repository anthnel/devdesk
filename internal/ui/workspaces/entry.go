package workspaces

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthnel/devdesk/internal/git"
)

// isHidden decides what the workspaces view leaves out.
//
// One rule, consulted by the listing and by the nested-repo walk both: what the
// view shows is what S and F act on, and two rules for that question would let
// a repository be a visible row and an invisible target at once.
//
// The consequence of showHidden is worth stating rather than discovering: the
// walk will then descend into .venv, .terraform and .cache, so S on a directory
// reaches whatever they vendor. What bounds the walk is not a depth limit — see
// walkSubRepos — but the fact that it stops at every repository it finds.
func isHidden(name string, showHidden bool) bool {
	return !showHidden && strings.HasPrefix(name, ".")
}

// enrichEntry populates git and project type metadata for a directory entry
func enrichEntry(entry *Entry, showHidden bool) {
	detectGitStatus(entry)
	if entry.IsGitRepo {
		return
	}
	// For non-git directories, find every nested git repo, however deep.
	found := detectSubRepos(entry.Path, showHidden)
	entry.SubRepoPaths = found.Repos
	entry.SubRepoSkipped = found.Skipped
}

// detectGitStatus populates git-related fields on the entry.
//
// The reading itself is git.ReadStatus: this file used to run the four commands
// with its own execGit while internal/git counted the divergence again for the
// sync, and the MCP server (§3.38) needed the same answers a third time. What
// stays here is the part that is a *display* decision — turning `origin` into
// something a user reads and something a browser opens — because that is what
// the view owns and the domain package deliberately does not.
//
// GitUnpulled is as fresh as the last fetch, and nothing here fetches (D35).
func detectGitStatus(entry *Entry) {
	if !isRepo(entry.Path) {
		return
	}
	entry.IsGitRepo = true

	status, err := git.ReadStatus(entry.Path)
	if err != nil {
		return
	}

	entry.GitBranch = status.Branch
	entry.GitModified = status.Modified
	entry.GitUntracked = status.Untracked
	entry.GitUnpushed = status.Ahead
	entry.GitUnpulled = status.Behind

	if status.Remote != "" {
		entry.GitRemote = extractRemotePath(status.Remote)
		entry.GitRemoteURL = normalizeRemoteURL(status.Remote)
	}
}

// extractRemotePath strips the server from a git remote URL and returns only the path.
// Examples:
//
//	"https://gitlab.com/group/project.git" → "group/project"
//	"git@gitlab.com:group/project.git"     → "group/project"
func extractRemotePath(rawURL string) string {
	// SCP-like syntax: git@host:path — colon must not be followed by "//"
	if idx := strings.Index(rawURL, ":"); idx != -1 && !strings.Contains(rawURL[:idx], "/") {
		after := rawURL[idx+1:]
		if !strings.HasPrefix(after, "//") {
			return strings.TrimSuffix(after, ".git")
		}
	}
	// URL syntax: scheme://host/path
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		rest := rawURL[idx+3:]
		if slash := strings.Index(rest, "/"); slash != -1 {
			return strings.TrimSuffix(rest[slash+1:], ".git")
		}
	}
	return rawURL
}

// normalizeRemoteURL delegates to internal/git, which owns the rule since
// §3.42 needed the same host comparison from a domain package.
func normalizeRemoteURL(rawURL string) string {
	return git.NormalizeRemoteURL(rawURL)
}

// subRepoScan is what a walk found, and what it could not look at.
//
// Skipped is the second half, and it is the one that decides whether a gap is
// visible: a scan that silently leaves work out is the whole of D59, and the
// footer had no number for it. A directory refused by the filesystem is not an
// error the user can act on from here, but "12 repositories, 3 directories I
// could not read" is a different sentence from "12 repositories".
type subRepoScan struct {
	Repos   []string
	Skipped int
}

// detectSubRepos returns the git repositories under basePath, at any depth,
// and how many directories the walk could not read.
//
// It used to stop at three levels, and that literal was D59: a repository at
// `monorepos/client/2026/api` was invisible to S, F and A while the directory
// holding it browsed normally, and nothing on screen said a limit had been
// applied. A scan that silently leaves work out is worse than a slow one, so
// there is no limit any more.
//
// What bounds the walk instead, and why it is enough:
//
//   - It stops at every repository it finds, so a repository's own node_modules
//     or vendor tree is never entered. That is the prune that matters, and it
//     was always the one doing the work.
//   - It refuses to enter a link pointing back up the path it came by, and to
//     enter the same tree through a second link. That is the cycle guard the
//     depth limit used to provide by accident, and it is what makes following
//     links safe at all.
//
// Measured before removing the limit, on this machine: over ~/projects the
// unbounded walk was as fast or faster than the bounded one (the repositories
// are shallow, so both stop at the same places), and over the Go module cache —
// tens of thousands of directories with no repository anywhere to prune it — it
// took 283 ms against 37 ms. That is the worst case, it runs in a Cmd rather
// than in Update, and it is the price of not losing repositories.
func detectSubRepos(basePath string, showHidden bool) subRepoScan {
	var found subRepoScan
	var crossed []os.FileInfo
	walkSubRepos(basePath, showHidden, &crossed, &found)
	return found
}

// walkSubRepos is the recursive helper for detectSubRepos.
func walkSubRepos(dir string, showHidden bool, crossed *[]os.FileInfo, found *subRepoScan) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Swallowed until now: a directory refused in the middle of a tree
		// contributed nothing and said nothing, which is the same silence the
		// depth limit was (§1.3 D59).
		log.Printf("ERROR [workspaces] read %s: %v", dir, err)
		found.Skipped++
		return
	}

	// Read out of the listing rather than asked for with os.Stat, and the
	// difference is not small. Recognising a bare repository needs three names
	// where `.git` needed one, and a failed stat apiece is what that costs on
	// Windows: measured over the Go module cache — the worst case, tens of
	// thousands of directories with no repository anywhere to prune the walk —
	// the stat version took 334 ms against the old walk's 75 ms. The listing is
	// already being fetched, so the same question costs nothing here, and the
	// walk now runs it in 65 ms while finding strictly more. The price is one
	// ReadDir on a repository's own root, and repositories are where it stops.
	if holdsRepo(entries) {
		found.Repos = append(found.Repos, dir)
		return // don't recurse into nested repos
	}
	for _, e := range entries {
		if isHidden(e.Name(), showHidden) {
			continue
		}
		child := filepath.Join(dir, e.Name())

		// A real directory cannot close a cycle on its own, so it is entered
		// without asking. Only a link can, and only a link pays for the check.
		if e.IsDir() {
			walkSubRepos(child, showHidden, crossed, found)
			continue
		}
		if !leadsToDir(e, child) {
			continue
		}
		follow, err := mayFollow(child, crossed)
		if err != nil {
			log.Printf("ERROR [workspaces] resolve %s: %v", child, err)
			found.Skipped++
			continue
		}
		if !follow {
			continue
		}
		walkSubRepos(child, showHidden, crossed, found)
	}
}

// mayFollow decides whether a link may be entered, and records it when it may.
//
// Identity is os.SameFile rather than a resolved path, and that is not a
// preference: filepath.EvalSymlinks does **not** resolve a Windows junction —
// it hands back the link's own path — so a resolved-path guard sees two
// different names for one directory and never fires. Measured here on a
// junction: Readlink answers, EvalSymlinks does not, SameFile does. And a
// junction is exactly what the defect was reported against.
//
// Two questions, and they are different:
//
//   - **Pointing back up the path we came by.** Entering it walks the same tree
//     again, and again. Every directory the walk crossed is a prefix of the
//     link's own path, so the chain costs nothing to reconstruct — and the
//     climb goes to the filesystem root rather than to the base, because a link
//     above the base drags the base back in with it.
//   - **Already entered through another link.** That is not a loop but a
//     duplicate: the same repositories under a second name, and a second scan
//     of each.
//
// Only a link pays for any of this. A tree with none in it makes no extra call.
func mayFollow(link string, crossed *[]os.FileInfo) (bool, error) {
	target, err := os.Stat(link)
	if err != nil {
		return false, err
	}

	for dir := filepath.Dir(link); ; {
		if info, err := os.Stat(dir); err == nil && os.SameFile(info, target) {
			return false, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for _, seen := range *crossed {
		if os.SameFile(seen, target) {
			return false, nil
		}
	}
	*crossed = append(*crossed, target)
	return true, nil
}

// leadsToDir reports whether a listing entry leads to a directory, following a
// symbolic link or a Windows junction to answer.
//
// DirEntry.IsDir() reports on the link itself, so it said false for a junction
// pointing at a directory full of repositories: everything behind it was
// invisible to S, F and A, and the row rendered as a file — neither browsable
// nor scannable, with nothing saying why (§1.3 D59, cause 2).
//
//	entry a         IsDir=true   type=d---------  statIsDir=true
//	entry linked    IsDir=false  type=?---------  statIsDir=true   ← ignored
//
// The test is "not a plain file" rather than "is a symlink" on purpose: Go has
// reported a Windows junction as ModeSymlink and as ModeIrregular depending on
// the version, and os.Stat answers the same either way. A regular file costs no
// syscall, which is what keeps this affordable on a tree with no links in it.
func leadsToDir(e os.DirEntry, path string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type().IsRegular() {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isRepo reports whether dir is a git repository, from its own listing.
//
// One rule, one implementation: walkSubRepos asks holdsRepo directly because it
// has the listing in hand, and this is the entry point for a single directory —
// the row detectGitStatus is enriching. Its one ReadDir is nothing beside the
// four git subprocesses ReadStatus runs immediately after.
func isRepo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return holdsRepo(entries)
}

// holdsRepo is the rule: what a directory has to contain to be a repository.
//
// `.git` catches a working tree and a worktree alike — it is a directory in the
// first and a file in the second, and neither is asked about. What it misses is
// a *bare* repository, whose HEAD, objects and refs sit at the root: that one
// holds history, so it is a scan target like any other, and leaving it out was
// the same silent omission as the depth limit (§1.3 D59).
//
// All three are required for the bare case. A directory that merely has an
// `objects` in it is not a repository, and calling it one would send a scan
// somewhere there is nothing to read.
func holdsRepo(entries []os.DirEntry) bool {
	var head, objects, refs bool
	for _, e := range entries {
		switch e.Name() {
		case ".git":
			return true
		case "HEAD":
			head = true
		case "objects":
			objects = true
		case "refs":
			refs = true
		}
	}
	return head && objects && refs
}
