package workspaces

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isHidden decides what the workspaces view leaves out.
//
// One rule, consulted by the listing and by the nested-repo walk both: what the
// view shows is what S and F act on, and two rules for that question would let
// a repository be a visible row and an invisible target at once.
//
// The consequence of showHidden is worth stating rather than discovering: the
// walk will then descend into .venv, .terraform and .cache, so S on a directory
// reaches whatever they vendor. The depth limit is what bounds it.
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
	// For non-git directories, find nested git repos (depth ≤ 3)
	entry.SubRepoPaths = detectSubRepoPaths(entry.Path, 3, showHidden)
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

// detectGitStatus populates git-related fields on the entry
func detectGitStatus(entry *Entry) {
	gitDir := filepath.Join(entry.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return
	}
	entry.IsGitRepo = true

	// Get current branch
	if out, err := execGit(entry.Path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		entry.GitBranch = strings.TrimSpace(out)
	}

	// Get remote URL: store display path and full normalized URL
	if out, err := execGit(entry.Path, "remote", "get-url", "origin"); err == nil {
		rawURL := strings.TrimSpace(out)
		entry.GitRemote = extractRemotePath(rawURL)
		entry.GitRemoteURL = normalizeRemoteURL(rawURL)
	}

	// Get modified/untracked counts from porcelain status
	if out, err := execGit(entry.Path, "status", "--porcelain"); err == nil {
		for line := range strings.SplitSeq(out, "\n") {
			if len(line) < 2 {
				continue
			}
			xy := line[:2]
			if xy == "??" {
				entry.GitUntracked++
			} else {
				entry.GitModified++
			}
		}
	}

	// Get unpushed commits count
	if out, err := execGit(entry.Path, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &entry.GitUnpushed)
	}

	// Get unpulled commits count
	if out, err := execGit(entry.Path, "rev-list", "--count", "HEAD..@{u}"); err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &entry.GitUnpulled)
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

// execGit runs a git command in the given directory and returns stdout
func execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// detectSubRepoPaths walks a directory up to maxDepth and returns paths of git repos found.
func detectSubRepoPaths(basePath string, maxDepth int, showHidden bool) []string {
	var repos []string
	walkSubRepos(basePath, 0, maxDepth, showHidden, &repos)
	return repos
}

// walkSubRepos is the recursive helper for detectSubRepoPaths
func walkSubRepos(dir string, depth, maxDepth int, showHidden bool, repos *[]string) {
	if depth > maxDepth {
		return
	}
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
		walkSubRepos(filepath.Join(dir, e.Name()), depth+1, maxDepth, showHidden, repos)
	}
}
