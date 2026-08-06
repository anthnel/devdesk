package security

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The scan itself runs Trivy and Gitleaks in containers, and the browser and
// cache commands reach the desktop and the disk. No test executes one: results,
// progress updates and dependency checks are fed in as messages, and the
// assertions are on model state.
//
// These tests drive Update() and View() only — model.go is 1977 lines, the
// largest file in the project and due to be split, so assertions on its
// internals would pin the current layout.
//
// The form persists every toggle with config.Save, so HOME is redirected for
// the whole package: a test must not rewrite the developer's ~/.devdesk.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	home, err := os.MkdirTemp("", "devdesk-security-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Scan.EnableVuln = true
	cfg.Scan.EnableSecret = true
	cfg.Scan.TrivyImage = "aquasec/trivy:latest"
	cfg.Scan.GitleaksImage = "zricethezav/gitleaks:latest"
	return cfg
}

// findingFixtures cover one finding per tab plus a second CVE at a different
// severity, so the tab and severity filters can be told apart.
func findingFixtures() []scan.Finding {
	return []scan.Finding{
		{
			ID: "CVE-2026-0001", Title: "Critical flaw in libfoo", Severity: scan.SeverityCritical,
			Source: "trivy", PkgName: "libfoo", Version: "1.0.0", FixedIn: "1.0.1",
			References: []string{"https://nvd.example/CVE-2026-0001"},
		},
		{
			ID: "CVE-2026-0002", Title: "Medium flaw in libbar", Severity: scan.SeverityMedium,
			Source: "trivy", PkgName: "libbar", Version: "2.0.0",
		},
		{
			ID: "aws-access-token", Title: "AWS key committed", Severity: scan.SeverityHigh,
			Source: "gitleaks", File: "config/prod.env", Line: 12,
			Match: "AKIA...", Fingerprint: "config/prod.env:aws-access-token:12",
		},
		{
			ID: "GPL-3.0", Title: "Copyleft licence on a dependency", Severity: scan.SeverityLow,
			Source: "trivy-license", File: "go.mod",
		},
		{
			ID: "DS002", Title: "Container runs as root", Severity: scan.SeverityHigh,
			Source: "trivy-misconfig", File: "Dockerfile",
		},
	}
}

func resultFixture() *scan.Result {
	return &scan.Result{
		Target:     "/tmp/repo",
		TargetType: scan.TargetDirectory,
		StartTime:  time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		EndTime:    time.Date(2026, 8, 1, 10, 0, 30, 0, time.UTC),
		Duration:   30 * time.Second,
		Counts:     scan.SeverityCounts{Critical: 1, High: 1, Medium: 1, Low: 1},
		Findings:   findingFixtures(),
	}
}

// newTestModel returns a laid-out model on the empty form.
//
// The form is asked for rather than assumed: New() opens on the inventory, and
// a view rooted there returns to it from the results and from a failed scan.
// The form's own tests are about the form, so they say so — and they go with it
// in phase 3.
func newTestModel(t *testing.T) Model {
	t.Helper()
	m := New(testConfig())
	m.state = StateInput
	m.homeState = StateInput
	return feed(t, m, tea.WindowSizeMsg{Width: 160, Height: 30})
}

// inventoryModel returns a laid-out model on the inventory, holding targets.
func inventoryModel(t *testing.T, targets ...scanTarget) Model {
	t.Helper()
	m := feed(t, New(testConfig()), tea.WindowSizeMsg{Width: 160, Height: 30})
	return feed(t, m, InventoryLoadedMsg{Targets: targets})
}

// inventoryFixtures cover both kinds and both ends of the CRITICAL order.
func inventoryFixtures() []scanTarget {
	return []scanTarget{
		{
			Kind: kindImage, Name: "nexus/api:1.4", Scanned: true,
			Counts:    scan.SeverityCounts{Critical: 3, High: 11, Medium: 4, Low: 1},
			ScannedAt: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC),
		},
		{
			Kind: kindRepo, Name: "/home/dev/workspaces/devdesk", Scanned: true,
			Counts:    scan.SeverityCounts{Critical: 0, High: 2},
			ScannedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		},
	}
}

// scannedModel returns a model showing results for the fixture scan.
func scannedModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.targetPath = "/tmp/repo"
	return feed(t, m, ScanCompleteMsg{Result: resultFixture(), Gen: m.scanGen})
}

// detailsModel returns a model in the details view for the first CVE.
func detailsModel(t *testing.T) Model {
	t.Helper()
	return feed(t, scannedModel(t), testutil.Key("enter"))
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
		t.Fatalf("Update() returned %T, want security.Model", next)
	}
	return updated, cmd
}

// rowIDs returns the ID cell of every table row.
func rowIDs(rows []table.Row) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row[1])
	}
	return ids
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
