package workspaces

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ciConfig is a context that grades: the setting on, and a forge the fixtures'
// remotes belong to.
func ciConfig() *config.Config {
	cfg := testConfig()
	cfg.Scan.EnableCIScore = true
	cfg.Forge.Type = config.ForgeGitHub
	cfg.Forge.URL = "https://github.com"
	return cfg
}

func ciModel(t *testing.T, cfg *config.Config, entries []Entry, scans map[string]cache.WorkspaceScanEntry) Model {
	t.Helper()
	m := feed(t, New(cfg, nil), tea.WindowSizeMsg{Width: 200, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entries})
	if scans != nil {
		m = feed(t, m, ScanCacheLoadedMsg{Cache: scans})
	}
	return m
}

func hasColumn(m Model, title string) bool {
	for _, c := range m.table.Table().Columns() {
		if c.Title == title {
			return true
		}
	}
	return false
}

// The column exists only when the setting is on: off, it would be four cells of
// nothing on every row for the life of the view.
func TestTheCIColumnFollowsTheSetting(t *testing.T) {
	if hasColumn(ciModel(t, testConfig(), entryFixtures(), nil), "CI") {
		t.Error("the CI column is there with enable_ci_score off")
	}
	if !hasColumn(ciModel(t, ciConfig(), entryFixtures(), nil), "CI") {
		t.Error("the CI column is missing with enable_ci_score on")
	}
}

// A repository, a directory and a file each answer differently, and the two
// absences are not the same absence.
func TestWhatEachKindOfRowShowsForCI(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk", ScannedAt: time.Now(),
			CIScore: strptr("B"),
		},
	}
	m := ciModel(t, ciConfig(), entryFixtures(), scans)

	cells := map[string]string{}
	for _, r := range m.table.Visible() {
		cells[r.Entry.Name] = theme.CIScoreCell(r.CI.State, r.CI.Score)
	}

	if got := cells["devdesk"]; got != "B" {
		t.Errorf("a graded repository shows %q, want its letter", got)
	}
	// Scanned, gradeable, and no letter in the cache: the run was withheld.
	if got := cells["clean-repo"]; got != "-" {
		t.Errorf("an unscanned repository shows %q, want a dash", got)
	}
	// A directory is not graded: letters do not add up the way counts do.
	if got := cells["clients"]; got != "" {
		t.Errorf("a directory shows %q, want nothing", got)
	}
	if got := cells["notes.md"]; got != "" {
		t.Errorf("a file shows %q, want nothing", got)
	}
}

// The rule of §3.42, seen from the column: a repository of another forge is not
// "not yet graded", it cannot be graded from here — and the cell says so by
// being empty rather than a dash.
func TestARepositoryOfAnotherForgeShowsNothingRatherThanADash(t *testing.T) {
	entries := []Entry{{
		Name: "customer", Path: "/tmp/workspaces/customer", IsDir: true, IsGitRepo: true,
		GitBranch: "main", GitRemote: "acme/thing",
		GitRemoteURL: "https://git.customer.example/acme/thing",
	}}

	m := ciModel(t, ciConfig(), entries, nil)

	row := m.table.Visible()[0]
	if row.CI.State != theme.CIScoreUnavailable {
		t.Errorf("state = %v, want unavailable", row.CI.State)
	}
	if cell := theme.CIScoreCell(row.CI.State, row.CI.Score); cell != "" {
		t.Errorf("cell = %q, want nothing — a dash would read as \"not yet\"", cell)
	}
}

// A scanned repository whose run was withheld shows `?`, which is neither a
// grade nor an absence of scanning.
func TestAWithheldRunShowsAQuestionMark(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk", ScannedAt: time.Now(),
			CIScore: nil, // graded nothing: the run could not conclude
		},
	}
	m := ciModel(t, ciConfig(), entryFixtures(), scans)

	for _, r := range m.table.Visible() {
		if r.Entry.Name != "devdesk" {
			continue
		}
		if cell := theme.CIScoreCell(r.CI.State, r.CI.Score); cell != "?" {
			t.Errorf("a withheld run shows %q, want a question mark", cell)
		}
	}
}

// Rule 116 holds with the extra column, at the widths the view is used at.
func TestTheLineStillSpansTheViewportWithTheCIColumn(t *testing.T) {
	for _, width := range []int{80, 100, 140, 200} {
		m := feed(t, New(ciConfig(), nil), tea.WindowSizeMsg{Width: width, Height: 30})
		m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})

		if got, want := m.table.RenderedWidth(), width-2; got != want {
			t.Errorf("width %d: rendered span = %d, want %d", width, got, want)
		}
	}
}

func strptr(s string) *string { return &s }
