package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
)

func TestWorkspacesListReportsEveryRepositoryUnderTheRoot(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "work")
	makeRepo(t, filepath.Join(root, "alpha"))
	makeRepo(t, filepath.Join(root, "team", "beta"))
	mkdirs(t, filepath.Join(root, "notes"))

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var out workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &out)

	if len(out.Repositories) != 2 {
		t.Fatalf("listed %d repositories (%+v), want the two that are repositories", len(out.Repositories), out.Repositories)
	}
	if out.Repositories[0].Name != "alpha" || out.Repositories[1].Name != "beta" {
		t.Errorf("listed %s and %s, want alpha and beta sorted by path", out.Repositories[0].Name, out.Repositories[1].Name)
	}
	if out.WorkspacesDir != root {
		t.Errorf("workspaces_dir = %q, want the configured root", out.WorkspacesDir)
	}
}

// It lists repositories at whatever depth, where the view lists one level of
// directories: an agent asking what is checked out wants the repositories, not a
// tree it would then walk a call at a time.
func TestANestedRepositoryIsListedAndNotDescendedInto(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "work")
	outer := filepath.Join(root, "outer")
	makeRepo(t, outer)
	// A repository inside a repository is a submodule or a vendored copy, and
	// neither is a row of its own.
	makeRepo(t, filepath.Join(outer, "vendor", "inner"))

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var out workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &out)

	if len(out.Repositories) != 1 || out.Repositories[0].Name != "outer" {
		t.Errorf("listed %+v, want only the outer repository", out.Repositories)
	}
}

// The same rule the view follows: without it the walk descends into .venv and
// .terraform and reports whatever they vendor as the user's own work.
func TestAHiddenDirectoryIsSkippedUnlessTheContextAsksForIt(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "work")
	makeRepo(t, filepath.Join(root, "visible"))
	makeRepo(t, filepath.Join(root, ".cache", "vendored"))

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var hidden workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &hidden)
	if len(hidden.Repositories) != 1 {
		t.Errorf("listed %+v, want only the visible repository", hidden.Repositories)
	}

	env.Config.App.ShowHiddenFiles = true
	var shown workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &shown)
	if len(shown.Repositories) != 2 {
		t.Errorf("listed %+v with show_hidden_files on, want both", shown.Repositories)
	}
}

func TestARepositoryCarriesItsGitStateAndItsScan(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "work")
	repo := filepath.Join(root, "alpha")
	makeRepo(t, repo)
	writeFile(t, filepath.Join(repo, "go.mod"), "module alpha\n")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "x\n")
	storeWorkspaceEntry(t, "work", repo, cache.WorkspaceScanEntry{Critical: 4, High: 2})

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var out workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &out)

	if len(out.Repositories) != 1 {
		t.Fatalf("listed %+v, want the one repository", out.Repositories)
	}
	got := out.Repositories[0]
	// README.md, go.mod and untracked.txt: `git init` adds nothing, so every
	// file the fixture wrote is untracked.
	if got.Untracked != 3 {
		t.Errorf("untracked = %d, want the three files nothing has added", got.Untracked)
	}
	if got.HasUpstream {
		t.Error("has_upstream = true for a repository with no remote")
	}
	if !got.Scanned || got.Critical != 4 || got.High != 2 {
		t.Errorf("scan state = %+v, want the cached 4 critical / 2 high", got)
	}
}

// D35 through a protocol. The count is read from the local tracking ref and
// nothing here fetches, so the staleness is a field rather than a line of
// documentation — that is the only form an agent reads.
func TestTheBehindCountIsDeclaredStale(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "work")
	makeRepo(t, filepath.Join(root, "alpha"))

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var out workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &out)

	if len(out.Repositories) != 1 {
		t.Fatalf("listed %+v", out.Repositories)
	}
	if !out.Repositories[0].BehindIsStale {
		t.Error("behind_is_stale = false — this server never fetches, so it can never be current")
	}
}

// A context whose workspaces_dir does not exist yet answers with an empty list,
// and creates nothing on the way.
func TestAMissingWorkspacesDirIsAnEmptyList(t *testing.T) {
	home := fakeCacheHome(t)
	root := filepath.Join(home, "nowhere")

	env := testEnv(nil)
	env.Config.App.WorkspacesDir = root

	var out workspacesListOut
	callTool(t, connect(t, env), "workspaces_list", nil, &out)

	if len(out.Repositories) != 0 {
		t.Errorf("listed %+v, want none", out.Repositories)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Error("the listing created the workspaces directory")
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

func makeRepo(t *testing.T, path string) {
	t.Helper()
	mkdirs(t, path)
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in %s: %v: %s", path, err, out)
	}
	writeFile(t, filepath.Join(path, "README.md"), "# repo\n")
}

func mkdirs(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
