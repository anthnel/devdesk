package workspaces

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
)

// secretsFound writes the "a secret was found" verdict. Le verdict est un
// pointeur parce qu'il en a trois : nil dit que personne n'a cherché.
func secretsFound() *bool { v := true; return &v }

// Loading entries walks the filesystem and shells out to git; scanning runs
// Trivy and Gitleaks in containers. No test executes a command Update returns:
// entries and scan results are fed in as messages instead.
//
// These tests drive Update() and View() only — model.go is 1299 lines and due
// to be split, and assertions on its internals would pin the current layout.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.App.WorkspacesDir = "/tmp/workspaces"
	cfg.App.IDECommand = "code"
	return cfg
}

// entryFixtures cover the shapes the view branches on: a git repo with a dirty
// tree, a clean git repo, a plain directory holding nested repos, a directory
// holding none, and a file.
func entryFixtures() []Entry {
	modTime := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	return []Entry{
		{
			Name: "devdesk", Path: "/tmp/workspaces/devdesk", IsDir: true, ModTime: modTime,
			IsGitRepo: true, GitBranch: "main", GitRemote: "anthnel/devdesk",
			GitRemoteURL: "https://github.com/anthnel/devdesk",
			GitModified:  2, GitUntracked: 1, GitUnpushed: 3, GitUnpulled: 0,
			ProjectType: "Go",
		},
		{
			Name: "clean-repo", Path: "/tmp/workspaces/clean-repo", IsDir: true, ModTime: modTime,
			IsGitRepo: true, GitBranch: "main", GitRemote: "anthnel/clean",
			GitRemoteURL: "https://github.com/anthnel/clean",
			ProjectType:  "Node",
		},
		{
			Name: "clients", Path: "/tmp/workspaces/clients", IsDir: true, ModTime: modTime,
			SubRepoPaths: []string{"/tmp/workspaces/clients/a", "/tmp/workspaces/clients/b"},
		},
		{
			Name: "empty-dir", Path: "/tmp/workspaces/empty-dir", IsDir: true, ModTime: modTime,
		},
		{
			Name: "notes.md", Path: "/tmp/workspaces/notes.md", IsDir: false, ModTime: modTime,
		},
	}
}

// newTestModel returns a laid-out model with no entries yet.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// loadedModel returns a model that has absorbed the fixture entries.
func loadedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, newTestModel(t), EntriesLoadedMsg{Entries: entryFixtures()})
}

// scannedModel returns a loaded model with a cached scan result for the first
// repo, which is what unlocks the details view.
func scannedModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	return feed(t, m, ScanCacheLoadedMsg{Cache: map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk",
			Critical: 1, High: 2, Medium: 3, Low: 4, Sensitive: secretsFound(),
			ScannedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		},
	}})
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want workspaces.Model", next)
	}
	return updated, cmd
}

// rowNames returns the Name cell of every table row.
func rowNames(rows []table.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[0])
	}
	return names
}
