package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
)

// The alias is a display substitution and nothing else: the cache key is what
// enter, S and A resolve, and what AddToGitleaksIgnore writes into. These tests
// hold the two apart.

const (
	nexusURL   = "nexus.example.com/docker-hosted"
	aliasedRef = nexusURL + "/agent-base:1.0"
)

// aliasedConfig declares one registry with an alias and one without, so the
// substitution and its absence are both exercised.
func aliasedConfig() *config.Config {
	cfg := testConfig()
	cfg.Registry.Registries = []config.RegistryItem{
		{URL: nexusURL, Alias: "nx"},
		{URL: "registry.example.com"},
	}
	return cfg
}

func aliasedModel(t *testing.T, targets ...scanTarget) Model {
	t.Helper()
	m := feed(t, New(aliasedConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})
	return feed(t, m, InventoryLoadedMsg{Targets: targets})
}

// errNoStoredResult stands in for the read error the loader reports when a
// result file is gone.
var errNoStoredResult = errors.New("no such file")

func imageTarget(name string) scanTarget {
	return scanTarget{
		Kind: kindImage, Name: name, Scanned: true,
		Counts:    scan.SeverityCounts{Critical: 1},
		ScannedAt: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC),
	}
}

// targetCells returns the Target cell of every rendered row.
func targetCells(t *testing.T, m Model) []string {
	t.Helper()
	idx := columnIndex(t, m.inventory.Table().Columns(), "Target")
	cells := make([]string, 0, len(m.inventory.Table().Rows()))
	for _, row := range m.inventory.Table().Rows() {
		cells = append(cells, row[idx])
	}
	return cells
}

func TestAnImageRowShowsItsRegistryAlias(t *testing.T) {
	m := aliasedModel(t, imageTarget(aliasedRef))

	cells := targetCells(t, m)
	if len(cells) != 1 {
		t.Fatalf("the inventory holds %v, want one row", cells)
	}
	if !strings.Contains(cells[0], "nx/agent-base:1.0") {
		t.Errorf("Target cell = %q, want the registry prefix folded to its alias", cells[0])
	}
	if strings.Contains(cells[0], nexusURL) {
		t.Errorf("Target cell = %q, still carries the full registry URL", cells[0])
	}
}

// The row is what the user reads; the key is what every command resolves. An
// alias that reached the key would send a rescan and a result load to an entry
// that does not exist.
func TestTheCacheKeyIsNeverAliased(t *testing.T) {
	m := aliasedModel(t, imageTarget(aliasedRef))

	target, ok := m.inventory.Selected()
	if !ok {
		t.Fatal("no row selected")
	}
	if target.Name != aliasedRef {
		t.Errorf("Name = %q, want the cache key untouched", target.Name)
	}
}

func TestAnImageWithNoConfiguredAliasKeepsItsFullName(t *testing.T) {
	m := aliasedModel(t, imageTarget("registry.example.com/api:2"))

	if cells := targetCells(t, m); !strings.Contains(cells[0], "registry.example.com/api:2") {
		t.Errorf("Target cell = %q, want the full reference when no alias is declared", cells[0])
	}
}

// A repository path is folded to "~", never to a registry alias — and an
// absolute path may legitimately begin with a configured registry URL.
func TestARepositoryTargetIsUntouchedByAliases(t *testing.T) {
	path := "/srv/" + nexusURL + "/checkout"
	m := aliasedModel(t, scanTarget{Kind: kindRepo, Name: path, Scanned: true})

	cells := targetCells(t, m)
	if !strings.Contains(cells[0], path) {
		t.Errorf("Target cell = %q, want the repository path left alone", cells[0])
	}
	target, _ := m.inventory.Selected()
	if target.Display != "" {
		t.Errorf("Display = %q on a repository, want nothing to substitute", target.Display)
	}
}

// A column that shows one name while the search wants the other reads as a bug,
// which is why the images tab searches both. So does this one.
func TestTheTargetColumnSearchesBothNames(t *testing.T) {
	m := aliasedModel(t, imageTarget(aliasedRef))
	target, _ := m.inventory.Selected()

	idx := columnIndex(t, m.inventory.Table().Columns(), "Target")
	search := inventoryColumns(false)[idx].Search
	if search == nil {
		t.Fatal("the Target column declares no Search, so '/' matches nothing")
	}

	haystack := search(target)
	for _, want := range []string{aliasedRef, "nx/agent-base:1.0"} {
		if !strings.Contains(haystack, want) {
			t.Errorf("Search() = %q, does not match %q", haystack, want)
		}
	}
}

// The title reads the folded label; AddToGitleaksIgnore reads the key. Folding
// one field for both would name a directory that does not exist.
func TestTheTitleShowsTheAliasAndTheKeyStaysWhole(t *testing.T) {
	result := &scan.Result{Target: aliasedRef, TargetType: scan.TargetImage}
	m := NewWithPreloadedResult(aliasedConfig(), nil, result)

	if !strings.Contains(m.GetTitle(), "nx/agent-base:1.0") {
		t.Errorf("GetTitle() = %q, want the alias", m.GetTitle())
	}
	if m.targetPath != aliasedRef {
		t.Errorf("targetPath = %q, want the untouched target — it is the path written to", m.targetPath)
	}
}

// A repository result folds its home directory in the title, as the rows do.
func TestARepositoryResultTitleFoldsTheHomeDirectory(t *testing.T) {
	result := &scan.Result{Target: "/srv/ws/devdesk", TargetType: scan.TargetDirectory}
	m := NewWithPreloadedResult(aliasedConfig(), nil, result)

	if !strings.Contains(m.GetTitle(), "/srv/ws/devdesk") {
		t.Errorf("GetTitle() = %q, want the repository path", m.GetTitle())
	}
}

// The footer names a target too, and it truncates to the width (Rule 128) — the
// registry prefix is exactly what would be spent there.
func TestAFooterMessageNamesTheFoldedTarget(t *testing.T) {
	m := aliasedModel(t, imageTarget(aliasedRef))

	m, _ = step(t, m, InventoryResultLoadedMsg{Name: aliasedRef, Err: errNoStoredResult})

	if footer := m.RenderFooter(160); !strings.Contains(footer, "nx/agent-base:1.0") {
		t.Errorf("the footer reads %q, want the folded name", footer)
	}
}
