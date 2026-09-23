package configuration

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// sized builds the view at a size, on a tab.
func sized(t *testing.T, width, height int, tab string) Model {
	t.Helper()
	m := New(config.Default(), MCPFacts{})
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	for i, s := range m.sections {
		if s.Title == tab {
			m.activeTab = i
			m.focusedField = m.settleFocus(0, 1)
			m.bindInput()
			return m
		}
	}
	t.Fatalf("no tab %q", tab)
	return m
}

// ↓ reads down the left column, then carries on at the top of the right one:
// the focus order is the flat list, and the columns only lay it out (§3.86).
func TestDownCrossesFromTheLeftColumnToTheRight(t *testing.T) {
	m := sized(t, 130, 60, toolsTab)
	if m.columns() != 2 {
		t.Fatalf("columns = %d at width 130, want 2", m.columns())
	}
	l := m.layout()
	fields := m.fields()

	crossed := false
	for i := 1; i < len(fields); i++ {
		if l.rows[i] < l.rows[i-1] {
			crossed = true
			if fields[i].Group == fields[i-1].Group {
				t.Errorf("the columns split inside the %q group", fields[i].Group)
			}
		}
	}
	if !crossed {
		t.Error("no field sits higher than the one before it: there is no second column")
	}
}

// Below the width two columns need, the tab falls back to one.
func TestANarrowTerminalGetsOneColumn(t *testing.T) {
	if m := sized(t, 2*columnMinWidth+columnGap-1, 60, toolsTab); m.columns() != 1 {
		t.Errorf("columns = %d below the minimum width, want 1", m.columns())
	}
}

// No rendered line is wider than the view, on any tab, in one column or two.
func TestNoLineIsWiderThanTheView(t *testing.T) {
	for _, width := range []int{80, 130, 200} {
		for _, tab := range []string{"app", "scan", toolsTab, "mcp"} {
			m := sized(t, width, 200, tab)
			for i, line := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%s at %d: line %d is %d wide: %q", tab, width, i, w, ansi.Strip(line))
				}
			}
		}
	}
}

// A tab taller than the viewport scrolls, and the focused field is always on
// screen — the bottom of the scan tab used to be cut off, with nothing to reach it.
func TestTheFocusedFieldIsAlwaysOnScreen(t *testing.T) {
	m := sized(t, 80, 12, toolsTab)
	if len(m.layout().lines) <= 12 {
		t.Fatal("the tools tab fits in 12 lines at width 80; the test exercises nothing")
	}
	seen := map[int]bool{}
	for range len(m.fields()) + 2 {
		view := m.View()
		if n := strings.Count(view, "\n") + 1; n > 12 {
			t.Fatalf("the view is %d lines tall, want at most 12", n)
		}
		if !strings.Contains(view, "") && m.current().focusable() {
			t.Fatalf("field %d (%q) is focused and not on screen:\n%s", m.focusedField, m.current().Label, ansi.Strip(view))
		}
		seen[m.focusedField] = true
		m = feed(t, m, testutil.Key("down"))
	}
	if len(seen) < 3 {
		t.Errorf("↓ visited %d fields", len(seen))
	}
}

// A tool's heading says whether this context needs it and whether it is there;
// one nobody ticked is "not used", whatever the machine holds.
func TestAToolHeadingSaysItsState(t *testing.T) {
	m := sized(t, 130, 60, toolsTab)
	if got := ansi.Strip(m.toolState(config.ToolTrivy)); got != "…" {
		t.Errorf("before the detection: %q, want …", got)
	}

	m = feed(t, m, shared.ScanToolsMsg{Report: &scan.Report{Tools: map[scan.ToolID]scan.ToolStatus{
		scan.ToolTrivy:   {Available: true, Source: scan.ToolSourceContainer, Version: "docker:Version: 0.71.2"},
		scan.ToolPlumber: {Available: true, Source: scan.ToolSourceBinary},
	}}})

	for tool, want := range map[string]string{
		config.ToolTrivy:    "required · image · 0.71.2",
		config.ToolGitleaks: "required · missing",
		config.ToolPlumber:  "not used",
	} {
		if got := ansi.Strip(m.toolState(tool)); !strings.HasSuffix(got, want) {
			t.Errorf("%s heading = %q, want it to end in %q", tool, got, want)
		}
	}
	// A box ticked on the scan tab changes the heading at once, no detection.
	m.config.Scan.Categories.CI.Enabled = true
	if got := ansi.Strip(m.toolState(config.ToolPlumber)); !strings.Contains(got, "required") {
		t.Errorf("plumber after CI was ticked: %q", got)
	}
}

// ctrl+r asks the router for a detection on the tools tab, and does nothing on
// another (Rule 111: refresh, and nothing else).
func TestCtrlRAsksForADetectionOnTheToolsTab(t *testing.T) {
	m := sized(t, 130, 60, toolsTab)
	_, cmd := m.Update(testutil.Key("ctrl+r"))
	if cmd == nil {
		t.Fatal("ctrl+r on the tools tab issued nothing")
	}
	if _, ok := cmd().(shared.ScanToolsDetectRequestMsg); !ok {
		t.Error("ctrl+r on the tools tab did not ask for a detection")
	}

	if _, cmd := sized(t, 130, 60, "scan").Update(testutil.Key("ctrl+r")); cmd != nil {
		t.Error("ctrl+r outside the tools tab did something")
	}
}

// A tool's extra arguments are typed as one line and stored as a list; a flag
// DevDesk sets itself is refused by name, and the old value stays (Rule 128).
func TestArgsAreStoredAsAListAndReservedFlagsRefused(t *testing.T) {
	m := focusOn(t, newModel(t), "Trivy › Args")
	m.input.SetValue(`--skip-dirs vendor --label "a b"`)
	m = feed(t, m, testutil.Key("esc"))

	want := []string{"--skip-dirs", "vendor", "--label", "a b"}
	if got := m.config.Scan.Tools.Trivy.Args; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %q, want %q", got, want)
	}

	m.input.SetValue("--format table")
	m = feed(t, m, testutil.Key("esc"))

	if got := m.config.Scan.Tools.Trivy.Args; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("a refused value replaced the old one: %q", got)
	}
	if !strings.Contains(m.footer.Text(), "--format") {
		t.Errorf("footer = %q, want it to name the refused flag", m.footer.Text())
	}
}
