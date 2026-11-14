package netdiag

import (
	"os"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-328 step 6: on the Diagnostics results, H (keymap.Diagnose) on the
// "Local route" row hands the results' target to the Path tab. Nothing is
// resolved or sent: the user presses enter on Path.

// The reason text is the plan's; it is spelled out here so that the test
// reads the same literal the user would.
const wantNotARouteRow = "Select the Local route row to trace its path"

// routeResults returns a model with finished results over host, engine faked,
// the cursor on the first row.
func routeResults(t *testing.T, host string) (*Model, *fakeEngine) {
	t.Helper()
	m := typeInto(t, newTestModel(t), host)
	fe := &fakeEngine{}
	m.pathModel.engine = fe.engine()
	m = feed(t, m, testutil.Key("enter"))
	route := check(netcheck.CheckRoute, netcheck.OK, "eth0 via 192.168.1.1")
	route.Title = "Local route"
	m = deliver(t, m, route,
		check(netcheck.CheckResolve, netcheck.OK, host+" resolves"),
		check(netcheck.CheckTCP, netcheck.Fail, "Port 443 does not accept connections"),
	)
	if m.state != StateResults {
		t.Fatalf("state = %v, want StateResults", m.state)
	}
	return m, fe
}

// selectCheck moves the results cursor onto the row of id.
func selectCheck(t *testing.T, m *Model, id netcheck.CheckID) *Model {
	t.Helper()
	for range 20 {
		if c, ok := m.checksTable.Selected(); ok && c.ID == id {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("no row for check %q in the results", id)
	return m
}

func TestHOnTheRouteRowOpensPathPrefilledWithoutTracing(t *testing.T) {
	m, fe := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)

	m, _ = step(t, m, testutil.Key(keymap.Diagnose))

	if m.activeTab != tabPath {
		t.Fatalf("activeTab = %d, want tabPath (%d)", m.activeTab, tabPath)
	}
	if got := m.pathModel.input.Value(); got != "example.com" {
		t.Errorf("Path field = %q, want the diagnostics host", got)
	}
	if m.pathModel.state != pathEditing || !m.pathModel.InEditMode() {
		t.Errorf("Path state = %v, want editing with the field focused", m.pathModel.state)
	}
	if m.pathModel.running() {
		t.Error("a trace is running; H must only prefill")
	}
	if fe.resolveCalls != 0 || fe.traceCalls != 0 {
		t.Errorf("engine called (resolve %d, trace %d); nothing may be resolved or sent", fe.resolveCalls, fe.traceCalls)
	}
}

func TestHKeepsTheHostOnlyNotTheDiagnosticsPort(t *testing.T) {
	m := typeInto(t, newTestModel(t), "example.com")
	m.portInput.SetValue("8443")
	m.pathModel.engine = (&fakeEngine{}).engine()
	m = feed(t, m, testutil.Key("enter"))
	route := check(netcheck.CheckRoute, netcheck.OK, "eth0")
	route.Title = "Local route"
	m = deliver(t, m, route)
	m = selectCheck(t, m, netcheck.CheckRoute)

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if got := m.pathModel.input.Value(); got != "example.com" {
		t.Errorf("Path field = %q, want the host alone (the Path port is not the Diagnostics port)", got)
	}
}

func TestHPrefillClearsAPreviousPathOutcome(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.pathModel.state = pathDone
	m.pathModel.errText = "old failure"
	m.pathModel.input.SetValue("old.example")

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	pm := m.pathModel
	if pm.state != pathEditing || pm.errText != "" || len(pm.hops) != 0 {
		t.Errorf("state=%v errText=%q hops=%d, want editing with the old outcome cleared", pm.state, pm.errText, len(pm.hops))
	}
	if pm.input.Value() != "example.com" {
		t.Errorf("field = %q, want example.com", pm.input.Value())
	}
}

func TestHOffTheRouteRowWarnsWithTheReasonAndStays(t *testing.T) {
	m, fe := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckTCP)

	m, _ = step(t, m, testutil.Key(keymap.Diagnose))

	if m.activeTab != tabDiagnostics || m.state != StateResults {
		t.Errorf("tab=%d state=%v, want unchanged Diagnostics results", m.activeTab, m.state)
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != wantNotARouteRow {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), wantNotARouteRow)
	}
	if m.pathModel.input.Value() != "" || fe.traceCalls != 0 || fe.resolveCalls != 0 {
		t.Error("the Path tab was touched by a refused H")
	}
}

func TestHIsAlwaysAnnouncedOnResultsAndGreyedOffTheRouteRow(t *testing.T) {
	m, _ := routeResults(t, "example.com")

	m = selectCheck(t, m, netcheck.CheckRoute)
	onRoute := m.GetShortcuts()
	if !testutil.HasShortcut(onRoute, keymap.Diagnose) || !testutil.ShortcutEnabled(onRoute, keymap.Diagnose) {
		t.Errorf("route row: H missing or greyed: %v", testutil.ShortcutKeys(onRoute))
	}

	m = selectCheck(t, m, netcheck.CheckTCP)
	offRoute := m.GetShortcuts()
	if !testutil.HasShortcut(offRoute, keymap.Diagnose) || !testutil.ShortcutDisabled(offRoute, keymap.Diagnose) {
		t.Errorf("TCP row: H must stay announced and be greyed: %v", testutil.ShortcutKeys(offRoute))
	}

	// Rule 130: the key set does not depend on the row.
	if a, b := strings.Join(testutil.ShortcutKeys(onRoute), ","), strings.Join(testutil.ShortcutKeys(offRoute), ","); a != b {
		t.Errorf("results key set changed with the row: %s -> %s", a, b)
	}
}

func TestHDoesNotCollideWithAnotherResultsShortcut(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	seen := map[string]int{}
	for _, k := range testutil.ShortcutKeys(m.GetShortcuts()) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("key %q announced %d times on the results", k, n)
		}
	}
	if seen[keymap.Diagnose] != 1 {
		t.Errorf("H announced %d times, want once", seen[keymap.Diagnose])
	}
}

func TestHWhileAPathTraceRunsSwitchesTabAndLeavesTheTraceAlone(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.activeTab = tabPath
	m = feed(t, m, testutil.Type("1.1.1.1")...)
	m = feed(t, m, testutil.Key("enter"))
	if !m.pathModel.running() {
		t.Fatal("setup: no path trace running")
	}
	m.activeTab = tabDiagnostics

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if m.activeTab != tabPath {
		t.Errorf("activeTab = %d, want tabPath", m.activeTab)
	}
	if !m.pathModel.running() || m.pathModel.input.Value() != "1.1.1.1" {
		t.Errorf("running=%v field=%q, want the trace and its field untouched", m.pathModel.running(), m.pathModel.input.Value())
	}
	const want = "A path trace is already running — esc cancels it"
	if m.pathModel.footer.Level() != components.LevelWarning || m.pathModel.footer.Text() != want {
		t.Errorf("Path footer = {%v %q}, want Warn %q", m.pathModel.footer.Level(), m.pathModel.footer.Text(), want)
	}
}

func TestHIsInertInTheFormAndWhileRunning(t *testing.T) {
	m := typeInto(t, newTestModel(t), "")
	m = feed(t, m, testutil.Key(keymap.Diagnose))
	if m.activeTab != tabDiagnostics || m.targetInput.Value() != "H" {
		t.Errorf("form: tab=%d field=%q, want H typed into the field and no switch", m.activeTab, m.targetInput.Value())
	}

	r := runningModel(t, "example.com")
	r = feed(t, r, testutil.Key(keymap.Diagnose))
	if r.activeTab != tabDiagnostics || r.state != StateRunning || r.pathModel.input.Value() != "" {
		t.Errorf("running: tab=%d state=%v path=%q, want H inert", r.activeTab, r.state, r.pathModel.input.Value())
	}
}

func TestHOnDetailsIsInert(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key("enter"))
	m = feed(t, m, testutil.Key(keymap.Diagnose))
	if m.activeTab != tabDiagnostics || m.state != StateDetails {
		t.Errorf("details: tab=%d state=%v, want H inert", m.activeTab, m.state)
	}
}

func TestHelpMentionsHOnTheResults(t *testing.T) {
	var found bool
	for _, kb := range New(testConfig()).GetHelpContent().KeyBindings {
		if strings.HasPrefix(kb.Key, keymap.Diagnose) && strings.Contains(kb.Key, "results") {
			found = strings.Contains(strings.ToLower(kb.Description), "path") &&
				strings.Contains(kb.Description, "Local route")
		}
	}
	if !found {
		t.Error(`help lacks a binding "H (results)" whose description names the Path trace and the Local route row`)
	}
}

// The announcement and the handler must be the same token, or the keymap guard
// (TestEveryAnnouncedActionIsBoundInItsPackage) has nothing to compare.
func TestHIsAnnouncedAndHandledThroughKeymapDiagnose(t *testing.T) {
	for _, f := range []string{"header.go", "update.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "keymap.Diagnose") {
			t.Errorf("%s never mentions keymap.Diagnose", f)
		}
	}
}
