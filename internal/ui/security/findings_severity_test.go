package security

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-770 part B (D181): a badge colour is never the only carrier of the
// level (WCAG 1.4.1). The Severity cell spells the level out in full, so the
// HIGH and LOW badges stay readable by their word even where their colours sit
// close to a neighbour. This is a guard: it holds today and fails the day a
// column shortens or drops the word.

var severityWords = []scan.SeverityLevel{
	scan.SeverityCritical, scan.SeverityHigh, scan.SeverityMedium, scan.SeverityLow,
}

func TestTheSeverityCellSpellsTheLevelOut(t *testing.T) {
	col := findingColumns()[0]
	if col.Title != "Severity" {
		t.Fatalf("column 0 is %q, want Severity", col.Title)
	}
	for _, level := range severityWords {
		if got, want := col.Cell(scan.Finding{Severity: level}), string(level); got != want {
			t.Errorf("Severity cell for %s = %q, want %q", level, got, want)
		}
	}
	for _, want := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		if got := col.Cell(scan.Finding{Severity: scan.SeverityLevel(want)}); got != want {
			t.Errorf("Severity cell for %s = %q, want the word itself", want, got)
		}
	}
	if col.Sizing != datatable.SizingFixed {
		t.Errorf("Severity column Sizing = %v, want SizingFixed: the word must never be cut", col.Sizing)
	}
	if col.MinWidth < len("CRITICAL") {
		t.Errorf("Severity column MinWidth = %d, want >= %d (len of CRITICAL)", col.MinWidth, len("CRITICAL"))
	}
	if col.Optional {
		t.Error("Severity column is Optional: it would be dropped on a narrow terminal and the level would vanish")
	}
}

// The word has to survive the rendering at 80 columns, on a row that is not
// under the cursor and on the one that is (a severity row repaints whole).
func TestTheSeverityWordIsRenderedOnEveryRowAt80Columns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var findings []scan.Finding
	for i, level := range severityWords {
		findings = append(findings, scan.Finding{
			ID: "CVE-2026-000" + string(rune('1'+i)), Title: "A vulnerable package", PkgName: "libexample",
			Severity: level, Source: scan.SourceTrivy,
		})
	}
	result := &scan.Result{Target: detailsTarget, TargetType: scan.TargetDirectory, Findings: findings}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(80, 30))
	m.switchTab(TabCVE)

	visible := m.findingsTable.Visible()
	if len(visible) != len(severityWords) {
		t.Fatalf("the CVE tab lists %d findings, want %d", len(visible), len(severityWords))
	}
	for cursor := range visible {
		m.findingsTable.SetCursor(cursor)
		lines := strings.Split(ansi.Strip(m.findingsTable.View()), "\n")
		for row, f := range visible {
			var line string
			for _, l := range lines {
				if strings.Contains(l, f.ID) {
					line = l
				}
			}
			state := "unselected"
			if row == cursor {
				state = "selected"
			}
			if line == "" {
				t.Errorf("cursor %d: the %s row of %s is not rendered at 80 columns", cursor, state, f.ID)
				continue
			}
			if !strings.Contains(line, string(f.Severity)) {
				t.Errorf("cursor %d: the %s row of %s does not carry the word %s: %q", cursor, state, f.ID, f.Severity, line)
			}
		}
	}
}
