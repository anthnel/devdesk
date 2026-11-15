package netdiag

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-471 step 2 (red phase): the Path tab is gone. The network view has
// four tabs, and the trace only lives under Diagnostics. These tests name no
// removed symbol (no tabPath), so the package compiles before and after.

var fourTabs = []int{tabDiagnostics, tabPorts, tabInterfaces, tabForward}

func TestTabCyclesTheFourTabsBothWays(t *testing.T) {
	m := newTestModel(t)
	for _, want := range []int{tabPorts, tabInterfaces, tabForward, tabDiagnostics} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("tab: activeTab = %d, want %d", m.activeTab, want)
		}
	}
	for _, want := range []int{tabForward, tabInterfaces, tabPorts, tabDiagnostics} {
		m = feed(t, m, testutil.Key("shift+tab"))
		if m.activeTab != want {
			t.Fatalf("shift+tab: activeTab = %d, want %d", m.activeTab, want)
		}
	}
}

func TestTabCountIsFour(t *testing.T) {
	if tabCount != len(fourTabs) {
		t.Errorf("tabCount = %d, want %d", tabCount, len(fourTabs))
	}
	if !slices.Equal(fourTabs, []int{0, 1, 2, 3}) {
		t.Errorf("tab indices = %v, want 0..3 in order Diagnostics, Ports, Interfaces, Forward", fourTabs)
	}
}

func TestTabRowListsFourTabsAndNoPathTab(t *testing.T) {
	m := newTestModel(t)
	for _, tab := range fourTabs {
		m.activeTab = tab
		plain := ansi.Strip(m.RenderFooter(120))
		for _, want := range []string{"Diagnostics", "Ports", "Interfaces", "Forward"} {
			if !strings.Contains(plain, want) {
				t.Errorf("tab %d: footer lacks the %q label:\n%s", tab, want, plain)
			}
		}
		if strings.Contains(plain, "Path") {
			t.Errorf("tab %d: the footer still names a Path tab:\n%s", tab, plain)
		}
		if got, rendered := m.GetFooterHeight(), strings.Count(plain, "\n")+1; got != rendered {
			t.Errorf("tab %d: declared footer height %d, rendered %d lines", tab, got, rendered)
		}
	}
}

func TestHelpDescribesFourTabsAndTheTraceKeys(t *testing.T) {
	c := New(testConfig()).GetHelpContent()
	all := []string{c.Description}
	for _, kb := range c.KeyBindings {
		all = append(all, kb.Key, kb.Description)
	}
	for _, s := range c.Sections {
		all = append(all, s.Title, s.Body)
	}
	text := strings.ToLower(strings.Join(all, "\n"))
	for _, banned := range []string{"path tab", "five-tab", "(path)", "(path,"} {
		if strings.Contains(text, banned) {
			t.Errorf("the help still says %q", banned)
		}
	}
	if !strings.Contains(c.Description, "Four-tab") {
		t.Errorf("Description = %q, want a four-tab summary", c.Description)
	}
	var tabs string
	for _, kb := range c.KeyBindings {
		if strings.HasPrefix(kb.Key, "tab") {
			tabs = kb.Description
		}
	}
	if strings.Contains(tabs, "Path") || !strings.Contains(tabs, "Forward") {
		t.Errorf("the tab binding %q must list the four tabs, not Path", tabs)
	}
	for _, want := range []string{"enter (trace)", "ctrl+o (trace)", "esc (trace)"} {
		found := false
		for _, kb := range c.KeyBindings {
			found = found || kb.Key == want
		}
		if !found {
			t.Errorf("help lacks the binding %q", want)
		}
	}
}

// Regression guards: these already hold and must keep holding after the tab goes.

func TestTraceLevelFooterHeightIsThreeInEveryState(t *testing.T) {
	ready, _ := routeLevel(t, "example.com")
	running, _, _ := traceLevelModel(t, traceLevelHost)
	done, _, _ := traceLevelModel(t, traceLevelHost)
	done = traceEnded(t, traceHops(t, done, 2))
	picker, _ := routeLevel(t, "example.com")
	picker = feed(t, picker, testutil.Key("ctrl+o"))

	for name, m := range map[string]*Model{"ready": ready, "running": running, "done": done, "picker": picker} {
		if got := m.GetFooterHeight(); got != 3 {
			t.Errorf("%s: GetFooterHeight() = %d, want 3", name, got)
		}
		if got := strings.Count(ansi.Strip(m.RenderFooter(120)), "\n") + 1; got != 3 {
			t.Errorf("%s: footer is %d lines, want 3", name, got)
		}
	}
}

func TestTraceLevelDropsAPaste(t *testing.T) {
	ready, _ := routeLevel(t, "example.com")
	running, _, _ := traceLevelModel(t, traceLevelHost)
	for name, m := range map[string]*Model{"ready": ready, "running": running} {
		host, gen, state, form := m.pathModel.target.Host, m.pathModel.gen, m.pathModel.state, m.targetInput.Value()
		m = feed(t, m, testutil.Paste("evil.example"))
		pm := m.pathModel
		if pm.target.Host != host || pm.gen != gen || pm.state != state || m.targetInput.Value() != form || m.state != StateTrace {
			t.Errorf("%s: a paste changed the sub-level (target %q, gen %d, state %v, form %q)", name, pm.target.Host, pm.gen, pm.state, m.targetInput.Value())
		}
	}
}

func TestTabLeavesTheTraceLevelAndComesBack(t *testing.T) {
	m, _ := routeLevel(t, "example.com")
	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabPorts {
		t.Fatalf("activeTab = %d after tab, want Ports", m.activeTab)
	}
	m = feed(t, m, testutil.Key("shift+tab"))
	if m.activeTab != tabDiagnostics || m.state != StateTrace || m.pathModel.target.Host != "example.com" {
		t.Errorf("tab=%d state=%v target=%q, want the sub-level back as it was", m.activeTab, m.state, m.pathModel.target.Host)
	}
}
