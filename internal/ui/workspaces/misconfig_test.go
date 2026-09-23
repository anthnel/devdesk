package workspaces

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// misconfigConfig is a context that looks for misconfigurations.
func misconfigConfig() *config.Config {
	cfg := testConfig()
	cfg.Scan.Categories.Misconfig.Enabled = true
	return cfg
}

func misconfigModel(t *testing.T, cfg *config.Config, entries []Entry, scans map[string]cache.WorkspaceScanEntry) Model {
	t.Helper()
	m := feed(t, New(cfg, nil), tea.WindowSizeMsg{Width: 200, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entries})
	if scans != nil {
		m = feed(t, m, ScanCacheLoadedMsg{Cache: scans})
	}
	return m
}

func misconfigCells(m Model) map[string]string {
	cells := map[string]string{}
	for _, r := range m.table.Visible() {
		cells[r.Entry.Name] = r.Misconfig.Text
	}
	return cells
}

// The column exists only when the category is on: off, it would be a dash on
// every row for the life of the view.
func TestTheMisconfigColumnFollowsTheSetting(t *testing.T) {
	if hasColumn(misconfigModel(t, testConfig(), entryFixtures(), nil), "CFG") {
		t.Error("the CFG column is there with the misconfig category off")
	}
	if !hasColumn(misconfigModel(t, misconfigConfig(), entryFixtures(), nil), "CFG") {
		t.Error("the CFG column is missing with the misconfig category on")
	}
}

// A repository that was scanned with the category off has been read for CVEs
// and not for misconfigurations: the cell must say so rather than print a zero
// nobody measured.
func TestAScanThatDidNotLookForMisconfigurationsShowsADash(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk", ScannedAt: time.Now(),
			Critical: 4, Misconfig: nil,
		},
		"/tmp/workspaces/clean-repo": {
			RepoPath: "/tmp/workspaces/clean-repo", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{},
		},
	}
	cells := misconfigCells(misconfigModel(t, misconfigConfig(), entryFixtures(), scans))

	if got := cells["devdesk"]; got != "-" {
		t.Errorf("a scan with no misconfiguration stage shows %q, want a dash", got)
	}
	if got := cells["clean-repo"]; got != "0" {
		t.Errorf("a stage that looked and found nothing shows %q, want a zero", got)
	}
	// A file has nothing to scan at all, which is not the same as a repository
	// waiting for a scan.
	if got := cells["notes.md"]; got != "" {
		t.Errorf("a file shows %q, want nothing", got)
	}
}

// The "?" is what keeps a chart nobody rendered apart from a chart with nothing
// in it — and it matters most on a zero.
func TestAnUnrenderedChartMakesTheCountPartial(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{Count: 0, Unrendered: 2},
		},
		"/tmp/workspaces/clean-repo": {
			RepoPath: "/tmp/workspaces/clean-repo", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{Count: 12, Worst: scan.SeverityHigh, Unrendered: 1},
		},
	}
	cells := misconfigCells(misconfigModel(t, misconfigConfig(), entryFixtures(), scans))

	if got := cells["devdesk"]; got != "0?" {
		t.Errorf("a repository whose charts were not rendered shows %q, want %q", got, "0?")
	}
	if got := cells["clean-repo"]; got != "12?" {
		t.Errorf("a partial count shows %q, want %q", got, "12?")
	}
}

// Counts add up, so a directory carries a real total — unlike the CI grade,
// where the worst of three letters is the grade of nothing.
func TestADirectorySumsTheMisconfigurationsUnderIt(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/clients/a": {
			RepoPath: "/tmp/workspaces/clients/a", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{Count: 3, Worst: scan.SeverityMedium},
		},
		"/tmp/workspaces/clients/b": {
			RepoPath: "/tmp/workspaces/clients/b", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{Count: 5, Worst: scan.SeverityCritical},
		},
	}
	m := misconfigModel(t, misconfigConfig(), entryFixtures(), scans)

	for _, r := range m.table.Visible() {
		if r.Entry.Name != "clients" {
			continue
		}
		if r.Misconfig.Text != "8" {
			t.Errorf("the directory shows %q, want the sum %q", r.Misconfig.Text, "8")
		}
		if r.Misconfig.Worst != string(scan.SeverityCritical) {
			t.Errorf("the directory's worst severity = %q, want CRITICAL", r.Misconfig.Worst)
		}
		if r.Misconfig.State != theme.MisconfigFound {
			t.Errorf("state = %v, want found", r.Misconfig.State)
		}
		return
	}
	t.Fatal("the clients directory is missing from the table")
}

// One repository nobody read makes the whole total partial: the directory
// cannot be more certain than what is unknown about its children.
func TestADirectoryWithAnUnreadRepositoryIsPartial(t *testing.T) {
	scans := map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/clients/a": {
			RepoPath: "/tmp/workspaces/clients/a", ScannedAt: time.Now(),
			Misconfig: &scan.MisconfigSummary{Count: 3, Worst: scan.SeverityMedium},
		},
	}
	cells := misconfigCells(misconfigModel(t, misconfigConfig(), entryFixtures(), scans))

	if got := cells["clients"]; got != "3?" {
		t.Errorf("a directory with one repository unscanned shows %q, want %q", got, "3?")
	}
}

// Rule 122: the cell is measured before it is styled, so it carries no escape
// sequence — the colour goes through Style.
func TestTheMisconfigCellIsPlainAndItsColourComesFromStyle(t *testing.T) {
	var col = misconfigColumn()
	row := workspaceRow{Misconfig: misconfigCell{
		Text: "7", State: theme.MisconfigFound, Worst: string(scan.SeverityCritical),
	}}

	if cell := col.Cell(row); cell != "7" {
		t.Errorf("Cell = %q, want the bare count", cell)
	}
	want := theme.SeverityTextStyle(string(scan.SeverityCritical))
	if got := col.Style(row); got.GetForeground() != want.GetForeground() {
		t.Error("Style does not carry the worst severity's colour")
	}
}

// Rule 116 holds with the extra column, at the widths the view is used at.
func TestTheLineStillSpansTheViewportWithTheMisconfigColumn(t *testing.T) {
	cfg := misconfigConfig()
	cfg.Scan.Categories.CI.Enabled = true
	for _, width := range []int{80, 100, 140, 200} {
		m := feed(t, New(cfg, nil), tea.WindowSizeMsg{Width: width, Height: 30})
		m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})

		if got, want := m.table.RenderedWidth(), width-2; got != want {
			t.Errorf("width %d: rendered span = %d, want %d", width, got, want)
		}
	}
}

// The two verdict columns keep one reading order across the views that show
// them: the severity counters, then the misconfiguration count, then the grade.
func TestTheMisconfigColumnSitsBetweenTheCountersAndTheGrade(t *testing.T) {
	cfg := misconfigConfig()
	cfg.Scan.Categories.CI.Enabled = true
	cfg.Forge.Type = config.ForgeGitHub
	cfg.Forge.URL = "https://github.com"

	m := misconfigModel(t, cfg, entryFixtures(), nil)
	titles := []string{}
	for _, c := range m.table.Table().Columns() {
		titles = append(titles, c.Title)
	}
	idx := func(want string) int {
		for i, title := range titles {
			if title == want {
				return i
			}
		}
		t.Fatalf("column %q is missing from %v", want, titles)
		return -1
	}
	if idx("L") >= idx("CFG") || idx("CFG") >= idx("CI") || idx("CI") >= idx("Scanned") {
		t.Errorf("column order is %v, want L before CFG before CI before Scanned", titles)
	}
}

// With the grade off, the count still lands before the last two columns rather
// than after them: the splice inserts what is on, not a fixed pair.
func TestTheMisconfigColumnKeepsItsPlaceWithoutTheGrade(t *testing.T) {
	m := misconfigModel(t, misconfigConfig(), entryFixtures(), nil)

	titles := []string{}
	for _, c := range m.table.Table().Columns() {
		titles = append(titles, c.Title)
	}
	if len(titles) < 3 {
		t.Fatalf("too few columns: %v", titles)
	}
	if got := titles[len(titles)-3:]; got[0] != "CFG" || got[1] != "Scanned" || got[2] != "Modified" {
		t.Errorf("the last three columns are %v, want CFG Scanned Modified", got)
	}
}
