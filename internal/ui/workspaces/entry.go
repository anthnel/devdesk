package workspaces

import (
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
	entry.ProjectType = detectProjectType(entry.Path)
	detectGitStatus(entry)
	if entry.IsGitRepo {
		return
	}
	// For non-git directories, find every nested git repo, however deep.
	entry.SubRepoPaths = detectSubRepoPaths(entry.Path, showHidden)
}

// detectProjectType detects the project type by looking for signature files
func detectProjectType(path string) string {
	signatures := []struct {
		file     string
		projType string
	}{
		{"go.mod", "Go"},
		{"Cargo.toml", "Rust"},
		{"package.json", "Node"},
		{"pyproject.toml", "Python"},
		{"requirements.txt", "Python"},
		{"pom.xml", "Java"},
		{"build.gradle", "Java"},
		{"Gemfile", "Ruby"},
		{"composer.json", "PHP"},
		{"mix.exs", "Elixir"},
		{"Makefile", "Make"},
		{"Dockerfile", "Docker"},
	}

	for _, sig := range signatures {
		if _, err := os.Stat(filepath.Join(path, sig.file)); err == nil {
			return sig.projType
		}
	}
	return ""
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
	gitDir := filepath.Join(entry.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
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

// normalizeRemoteURL converts any git remote URL to a plain HTTPS URL suitable for a browser.
// Examples:
//
//	"git@gitlab.com:group/project.git"            → "https://gitlab.com/group/project"
//	"https://token@gitlab.com/group/project.git"  → "https://gitlab.com/group/project"
func normalizeRemoteURL(rawURL string) string {
	// SCP-like syntax: git@host:path
	if idx := strings.Index(rawURL, ":"); idx != -1 && !strings.Contains(rawURL[:idx], "/") {
		after := rawURL[idx+1:]
		if !strings.HasPrefix(after, "//") {
			host := rawURL[:idx]
			if at := strings.Index(host, "@"); at != -1 {
				host = host[at+1:]
			}
			return "https://" + host + "/" + strings.TrimSuffix(after, ".git")
		}
	}
	// URL syntax: strip credentials and .git suffix
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		scheme := rawURL[:idx]
		rest := rawURL[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			if slash := strings.Index(rest, "/"); slash == -1 || at < slash {
				rest = rest[at+1:]
			}
		}
		return scheme + "://" + strings.TrimSuffix(rest, ".git")
	}
	return rawURL
}

// detectSubRepoPaths returns the git repositories under basePath, at any depth.
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
//   - It does not follow symbolic links (walkSubRepos filters on DirEntry.IsDir,
//     which reports on the link itself), so the walk cannot cycle. That is also
//     the other half of D59, still open: a repository behind a junction is not
//     found. Removing the depth limit does not make that worse, but whoever
//     fixes it will need a cycle guard that the depth limit used to provide by
//     accident.
//
// Measured before removing it, on this machine: over ~/projects the unbounded
// walk was as fast or faster than the bounded one (the repositories are shallow,
// so both stop at the same places), and over the Go module cache — tens of
// thousands of directories with no repository anywhere to prune it — it took
// 283 ms against 37 ms. That is the worst case, it runs in a Cmd rather than in
// Update, and it is the price of not losing repositories.
func detectSubRepoPaths(basePath string, showHidden bool) []string {
	var repos []string
	walkSubRepos(basePath, showHidden, &repos)
	return repos
}

// walkSubRepos is the recursive helper for detectSubRepoPaths
func walkSubRepos(dir string, showHidden bool, repos *[]string) {
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		*repos = append(*repos, dir)
		return // don't recurse into nested repos
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || isHidden(e.Name(), showHidden) {
			continue
		}
		walkSubRepos(filepath.Join(dir, e.Name()), showHidden, repos)
	}
}
