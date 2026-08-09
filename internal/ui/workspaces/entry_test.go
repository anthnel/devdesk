package workspaces

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// These are the only commands this package's tests execute: they touch nothing
// but a temporary directory. The scan and external-tool commands still shell
// out to Docker and the desktop, and are left alone.

// ── Create and delete ────────────────────────────────────────────────────────

func TestCreateWorkspaceMakesTheDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.App.WorkspacesDir = root
	m := New(cfg, nil)

	msg := m.createWorkspace("new-project")().(WorkspaceCreatedMsg)

	if msg.Error != nil {
		t.Fatalf("createWorkspace returned an error: %v", msg.Error)
	}
	want := filepath.Join(root, "new-project")
	if msg.Path != want {
		t.Errorf("created %q, want %q", msg.Path, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("the directory was not created: err=%v", err)
	}
}

// Creating while browsing a subdirectory puts the new entry there, not at the
// root — otherwise the user's new folder appears somewhere they are not.
func TestCreateWorkspaceUsesTheBrowsedDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "clients")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("creating the nested directory: %v", err)
	}
	cfg := config.Default()
	cfg.App.WorkspacesDir = root
	m := New(cfg, nil)
	m.currentPath = nested

	msg := m.createWorkspace("acme")().(WorkspaceCreatedMsg)

	if msg.Error != nil {
		t.Fatalf("createWorkspace returned an error: %v", msg.Error)
	}
	if want := filepath.Join(nested, "acme"); msg.Path != want {
		t.Errorf("created %q, want it under the browsed directory %q", msg.Path, want)
	}
}

// The workspaces root may not exist yet on a first run.
func TestCreateWorkspaceCreatesTheRootIfMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-yet")
	cfg := config.Default()
	cfg.App.WorkspacesDir = root
	m := New(cfg, nil)

	msg := m.createWorkspace("first")().(WorkspaceCreatedMsg)

	if msg.Error != nil {
		t.Fatalf("createWorkspace returned an error: %v", msg.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "first")); err != nil {
		t.Errorf("the workspace was not created under a missing root: %v", err)
	}
}

func TestDeleteEntryRemovesRecursively(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "doomed")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatalf("creating the tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "nested", "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	msg := New(config.Default(), nil).deleteEntry(target)().(EntryDeletedMsg)

	if msg.Error != nil {
		t.Fatalf("deleteEntry returned an error: %v", msg.Error)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("the directory survived the delete: err=%v", err)
	}
}

// ── Rename ───────────────────────────────────────────────────────────────────

func TestRenameEntryMovesWithinItsParent(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "before")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}

	msg := New(config.Default(), nil).renameEntry(old, "after")().(EntryRenamedMsg)

	if msg.Error != nil {
		t.Fatalf("renameEntry returned an error: %v", msg.Error)
	}
	want := filepath.Join(root, "after")
	if msg.NewPath != want {
		t.Errorf("renamed to %q, want %q", msg.NewPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the renamed directory does not exist: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the original directory still exists")
	}
}

func TestRenameEntryReportsFailure(t *testing.T) {
	root := t.TempDir()

	msg := New(config.Default(), nil).renameEntry(filepath.Join(root, "missing"), "after")().(EntryRenamedMsg)

	if msg.Error == nil {
		t.Error("renaming a missing entry reported success")
	}
}

// ── Entry enrichment ─────────────────────────────────────────────────────────

// enrichEntry runs the real git binary against a throwaway repository — the
// same approach internal/gitlab takes for Clone. A stub would only prove the
// stub works.
func TestEnrichEntryReadsGitMetadata(t *testing.T) {
	git := gitOrSkip(t)
	repo := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "gitconfig"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("remote", "add", "origin", "git@gitlab.com:group/project.git")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	run("add", "go.mod")
	run("commit", "-m", "initial")
	// One tracked modification and one untracked file.
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module y\n"), 0o600); err != nil {
		t.Fatalf("modifying go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing the untracked file: %v", err)
	}

	entry := Entry{Name: filepath.Base(repo), Path: repo, IsDir: true}
	enrichEntry(&entry)

	if !entry.IsGitRepo {
		t.Fatal("the repository was not detected as one")
	}
	if entry.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want main", entry.GitBranch)
	}
	if entry.GitRemote != "group/project" {
		t.Errorf("GitRemote = %q, want the path without the host", entry.GitRemote)
	}
	if entry.GitRemoteURL != "https://gitlab.com/group/project" {
		t.Errorf("GitRemoteURL = %q, want a browsable HTTPS URL", entry.GitRemoteURL)
	}
	if entry.ProjectType != "Go" {
		t.Errorf("ProjectType = %q, want Go", entry.ProjectType)
	}
	if entry.GitModified != 1 {
		t.Errorf("GitModified = %d, want 1", entry.GitModified)
	}
	if entry.GitUntracked != 1 {
		t.Errorf("GitUntracked = %d, want 1", entry.GitUntracked)
	}
	// A repo is never walked for nested repos — it is one itself.
	if entry.SubRepoPaths != nil {
		t.Errorf("SubRepoPaths = %v on a git repo, want none", entry.SubRepoPaths)
	}
}

func TestEnrichEntryOnAPlainDirectoryFindsNestedRepos(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inner", ".git"), 0o755); err != nil {
		t.Fatalf("creating the nested repo: %v", err)
	}

	entry := Entry{Name: "clients", Path: root, IsDir: true}
	enrichEntry(&entry)

	if entry.IsGitRepo {
		t.Error("a plain directory was reported as a git repo")
	}
	if len(entry.SubRepoPaths) != 1 {
		t.Errorf("SubRepoPaths = %v, want the one nested repo", entry.SubRepoPaths)
	}
}

func TestEnrichEntryOnANonRepoLeavesGitFieldsEmpty(t *testing.T) {
	entry := Entry{Name: "empty", Path: t.TempDir(), IsDir: true}

	enrichEntry(&entry)

	if entry.IsGitRepo || entry.GitBranch != "" || entry.GitRemote != "" {
		t.Errorf("git fields were populated for a non-repo: %+v", entry)
	}
}

func gitOrSkip(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	return git
}

// ── Loading ──────────────────────────────────────────────────────────────────

// loadEntries reads the real directory, so it is driven against a temp tree.
func TestLoadEntriesReadsTheWorkspacesDirectory(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("creating %q: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	cfg := config.Default()
	cfg.App.WorkspacesDir = root
	m := New(cfg, nil)

	msg := m.loadEntries()()
	loaded, ok := msg.(EntriesLoadedMsg)
	if !ok {
		t.Fatalf("loadEntries returned %T, want EntriesLoadedMsg", msg)
	}

	names := map[string]bool{}
	for _, e := range loaded.Entries {
		names[e.Name] = true
	}
	for _, want := range []string{"alpha", "beta", "notes.md"} {
		if !names[want] {
			t.Errorf("%q is missing from the loaded entries %v", want, names)
		}
	}
}

// A missing workspaces root is a first run, not an error: it is created and
// comes back empty.
func TestLoadEntriesCreatesTheRootOnFirstRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")
	cfg := config.Default()
	cfg.App.WorkspacesDir = root
	m := New(cfg, nil)

	msg := m.loadEntries()()

	loaded, ok := msg.(EntriesLoadedMsg)
	if !ok {
		t.Fatalf("loadEntries returned %T for a missing root, want it created", msg)
	}
	if len(loaded.Entries) != 0 {
		t.Errorf("the new root came back with %d entries, want none", len(loaded.Entries))
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the root was not created: %v", err)
	}
}

// A missing directory the user drilled into *is* an error — it was there a
// moment ago, so something removed it underneath them.
func TestLoadEntriesReportsAMissingBrowsedDirectory(t *testing.T) {
	cfg := config.Default()
	cfg.App.WorkspacesDir = t.TempDir()
	m := New(cfg, nil)
	m.currentPath = filepath.Join(cfg.App.WorkspacesDir, "vanished")

	msg := m.loadEntries()()

	if _, ok := msg.(LoadErrorMsg); !ok {
		t.Errorf("loadEntries returned %T for a vanished subdirectory, want LoadErrorMsg", msg)
	}
}

// Hidden entries are skipped: .git and friends are noise in a workspace list.
func TestLoadEntriesSkipsHiddenEntries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"visible", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("creating %q: %v", name, err)
		}
	}
	cfg := config.Default()
	cfg.App.WorkspacesDir = root

	loaded := New(cfg, nil).loadEntries()().(EntriesLoadedMsg)

	for _, e := range loaded.Entries {
		if e.Name == ".hidden" {
			t.Error("a hidden directory was listed")
		}
	}
	if len(loaded.Entries) != 1 {
		t.Errorf("loaded %d entries, want just the visible one", len(loaded.Entries))
	}
}

// The whole loop: load a real directory, feed the result back, and check the
// table shows it.
func TestLoadedEntriesReachTheTable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	cfg := config.Default()
	cfg.App.WorkspacesDir = root

	m := feed(t, New(cfg, nil), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, m.loadEntries()())

	if got := rowNames(m.table.Table().Rows()); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("the table holds %v, want the one directory", got)
	}
}

func TestRefreshReloads(t *testing.T) {
	_, cmd := step(t, loadedModel(t), testutil.Key("ctrl+r"))

	if cmd == nil {
		t.Error("ctrl+r issued no reload")
	}
}
