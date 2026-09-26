package workspaces

import (
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/fuzzy"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// fuzzyWalkResult is what a whole-tree directory walk found for fuzzy-find,
// and how many directories it could not read — same shape as subRepoScan,
// for the same reason (§1.3 D59): a silently incomplete result set is worse
// than a slow one.
type fuzzyWalkResult struct {
	Dirs    []string
	Skipped int
}

// FuzzyPathsLoadedMsg carries the result of the whole-tree walk behind the
// fuzzy-find prompt. The walk itself runs in a Cmd; only Update touches the
// model with what it found (Rule 110).
type FuzzyPathsLoadedMsg struct {
	Candidates []fuzzy.Candidate
	Skipped    int
}

// walkWorkspaceDirsCmd walks the whole workspaces root once, when the prompt
// opens — not on every keystroke.
func (m Model) walkWorkspaceDirsCmd() tea.Cmd {
	root := m.getExpandedWorkspacesDir()
	showHidden := m.config.App.ShowHiddenFiles

	return func() tea.Msg {
		result := collectDirs(root, showHidden)
		candidates := make([]fuzzy.Candidate, 0, len(result.Dirs))
		for _, dir := range result.Dirs {
			rel, err := filepath.Rel(root, dir)
			if err != nil || rel == "." {
				// The root itself: nothing to jump to that Home does not
				// already give.
				continue
			}
			candidates = append(candidates, dirCandidate(dir, filepath.ToSlash(rel)))
		}
		return FuzzyPathsLoadedMsg{Candidates: candidates, Skipped: result.Skipped}
	}
}

// dirCandidate is one directory the prompt can match: the absolute path is
// what jumping there needs, the forward-slash path relative to the workspaces
// root is what the query is matched against and what the results show.
func dirCandidate(abs, rel string) fuzzy.Candidate {
	return fuzzy.Candidate{Key: abs, Label: rel, Icon: theme.IconDirectory, Role: theme.IconRoleDirectory}
}

// collectDirs walks basePath and returns every directory under it, at any
// depth — never descending into a repository's own tree, mirroring
// detectSubRepos's boundary: a repository is a valid fuzzy-find target, but
// its vendor or node_modules is not. It shares
// isHidden/leadsToDir/mayFollow/holdsRepo with detectSubRepos — those are
// what make the walk safe against cycles and Windows junctions, and
// reimplementing them here would risk the two walks disagreeing over time.
func collectDirs(basePath string, showHidden bool) fuzzyWalkResult {
	var found fuzzyWalkResult
	var crossed []os.FileInfo
	walkAllDirs(basePath, showHidden, &crossed, &found)
	return found
}

// walkAllDirs is the recursive helper for collectDirs.
func walkAllDirs(dir string, showHidden bool, crossed *[]os.FileInfo, found *fuzzyWalkResult) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("ERROR [workspaces] read %s: %v", dir, err)
		found.Skipped++
		return
	}

	found.Dirs = append(found.Dirs, dir)

	if holdsRepo(entries) {
		// A repository is a valid target to land on, but its own tree is not
		// walked — same boundary as detectSubRepos, same reason.
		return
	}

	for _, e := range entries {
		if isHidden(e.Name(), showHidden) {
			continue
		}
		child := filepath.Join(dir, e.Name())

		if e.IsDir() {
			walkAllDirs(child, showHidden, crossed, found)
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
		walkAllDirs(child, showHidden, crossed, found)
	}
}
