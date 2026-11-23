package security

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-750 (step 7): the Misconfigurations tab shows the file of each
// finding, which is what tells two findings of one rule apart. The column is
// the table's own and is hidden on every other tab.

// fileColumnIndex is where findingColumns declares the File column.
func fileColumnIndex(t *testing.T) int {
	t.Helper()
	for i, c := range findingColumns() {
		if c.Title == "File" {
			return i
		}
	}
	t.Fatal("findingColumns declares no File column")
	return -1
}

// fileFixtures are three misconfigurations of one file apiece: a relative path,
// one under the container mount, and an absolute one under HOME that is longer
// than the column may be.
func fileFixtures(home string) []scan.Finding {
	deep := filepath.Join(home, "projects", "an-application", "deploy", "kubernetes", "overlays", "production", "manifest-file.yaml")
	mk := func(id, file string) scan.Finding {
		return scan.Finding{ID: id, Title: "A misconfiguration", Severity: scan.SeverityHigh,
			Source: scan.SourceTrivyMisconfig, File: file, IaCType: "kubernetes"}
	}
	return []scan.Finding{
		mk("KSV-0001", "Dockerfile"),
		mk("KSV-0003", "/scan/k8s/base/deployment.yaml"),
		mk("KSV-0009", deep),
	}
}

func fileModel(t *testing.T, width int, tab int) Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	result := &scan.Result{Target: detailsTarget, TargetType: scan.TargetDirectory, Findings: fileFixtures(home)}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(width, 30))
	m.switchTab(tab)
	return m
}

func headerLineOf(m Model) string {
	first, _, _ := strings.Cut(ansi.Strip(m.findingsTable.View()), "\n")
	return first
}

func TestTheFileColumnIsDeclaredLikeThePlanSays(t *testing.T) {
	at := fileColumnIndex(t)
	columns := findingColumns()
	c := columns[at]

	if at != len(columns)-1 {
		t.Errorf("File is column %d of %d: it is appended last so the other indices do not move", at, len(columns))
	}
	if c.Sizing != datatable.SizingContent || c.MinWidth != 16 || c.MaxWidth != 40 || !c.Optional || !c.TruncateHead {
		t.Errorf("File = %+v, want SizingContent, 16..40, Optional, TruncateHead", c)
	}
	if c.Less != nil || c.Search != nil {
		t.Error("File sorts or searches: the Title search already matches the file, and a second way to ask is noise")
	}
}

func TestTheFileColumnIsOnTheMisconfigurationTabOnly(t *testing.T) {
	for _, width := range []int{120, 175} {
		m := fileModel(t, width, TabMisconfig)
		if !strings.Contains(headerLineOf(m), "File") {
			t.Errorf("at %d the Misconfig header has no File: %q", width, headerLineOf(m))
		}
	}
	for _, tab := range []int{TabCVE, TabSecrets, TabLicense, TabCIScore} {
		m := fileModel(t, 175, tab)
		if strings.Contains(headerLineOf(m), "File") {
			t.Errorf("tab %d shows a File column: %q", tab, headerLineOf(m))
		}
	}
}

// The column follows the tab in both directions and survives a resize on a tab
// that hides it.
func TestTheFileColumnFollowsTheTab(t *testing.T) {
	m := fileModel(t, 175, TabMisconfig)

	m.switchTab(TabCVE)
	if strings.Contains(headerLineOf(m), "File") {
		t.Errorf("File stayed after leaving the tab: %q", headerLineOf(m))
	}
	m = feed(t, m, testutil.Resize(160, 30))
	if strings.Contains(headerLineOf(m), "File") {
		t.Errorf("a resize brought File back on the CVE tab: %q", headerLineOf(m))
	}
	m.switchTab(TabMisconfig)
	if !strings.Contains(headerLineOf(m), "File") {
		t.Errorf("File did not come back with the tab: %q", headerLineOf(m))
	}
}

func TestTheFileColumnKeepsTheLineInsideTheViewport(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		for _, tab := range []int{TabMisconfig, TabCVE} {
			m := fileModel(t, width, tab)
			if got, want := m.findingsTable.RenderedWidth(), width-2; got != want {
				t.Errorf("tab %d at %d: the line spans %d, want %d", tab, width, got, want)
			}
			for i, line := range strings.Split(m.findingsTable.View(), "\n") {
				if got := ansi.StringWidth(line); got > width-2 {
					t.Errorf("tab %d at %d: line %d spans %d cells", tab, width, i, got)
				}
			}
		}
	}
}

// Rule 122: plain text, measured before it is coloured. And the path a user
// reads is the one in the project, not the one inside the scanner's container.
func TestTheFileCellsArePlainAndShort(t *testing.T) {
	m := fileModel(t, 175, TabMisconfig)
	at := fileColumnIndex(t)

	cells := map[string]string{}
	for _, row := range m.findingsTable.Table().Rows() {
		if len(row) <= at {
			t.Fatalf("a row has %d cells, File is column %d", len(row), at)
		}
		if strings.Contains(row[at], "\x1b") {
			t.Errorf("the File cell of %s carries an escape sequence: %q", row[1], row[at])
		}
		cells[row[1]] = row[at]
	}

	if got := cells["KSV-0001"]; got != "Dockerfile" {
		t.Errorf("relative path = %q", got)
	}
	if got := cells["KSV-0003"]; got != "k8s/base/deployment.yaml" {
		t.Errorf("a path under the container mount = %q, want it without /scan/", got)
	}
	if got := cells["KSV-0009"]; !strings.HasPrefix(got, "~/projects/") {
		t.Errorf("a path under HOME = %q, want it folded to ~/", got)
	}
}

// A path longer than the column loses its start: the file name is what tells
// two findings apart.
func TestALongFilePathKeepsItsTail(t *testing.T) {
	m := fileModel(t, 175, TabMisconfig)

	var line string
	for _, l := range strings.Split(ansi.Strip(m.findingsTable.View()), "\n") {
		if strings.Contains(l, "KSV-0009") {
			line = l
		}
	}
	if !strings.Contains(line, "manifest-file.yaml") {
		t.Errorf("the tail of the path is cut: %q", line)
	}
	if strings.Contains(line, "an-application") {
		t.Errorf("the head of the path survived a 40 cell column: %q", line)
	}
}
