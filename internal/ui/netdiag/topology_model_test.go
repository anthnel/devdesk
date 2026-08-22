package netdiag

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// topoFixture covers one of every shape the view has a branch for: an up
// interface with errors, a loopback, a default route and a connected one, a
// reachable neighbour and a failed one, and a firewall chain per policy.
func topoFixture() topoDataMsg {
	return topoDataMsg{
		interfaces: []InterfaceInfo{
			{Name: "eth0", State: "UP", Addresses: []string{"192.168.1.5/24"}, MTU: 1500, RxErrors: 3, TxErrors: 0},
			{Name: "lo", State: "LOOP", Addresses: []string{"127.0.0.1/8"}, MTU: 65536},
			{Name: "wlan0", State: "DOWN", MTU: 1500},
		},
		routes: []RouteInfo{
			{Destination: "default", Gateway: "192.168.1.1", Interface: "eth0"},
			{Destination: "192.168.1.0/24", Interface: "eth0"},
		},
		neighbours: []NeighbourInfo{
			{IP: "192.168.1.1", MAC: "aa:bb:cc:dd:ee:ff", Interface: "eth0", State: "REACHABLE"},
			{IP: "192.168.1.9", Interface: "eth0", State: "FAILED"},
		},
		firewall: []FirewallChain{
			{Name: "INPUT", Policy: "DROP", Rules: 12},
			{Name: "FORWARD", Policy: "ACCEPT", Rules: 0},
		},
		firewallSrc: "iptables",
	}
}

// topologyModel returns the parent model on the topology tab, with data loaded.
func topologyModel(t *testing.T) *Model {
	t.Helper()
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))
	return feed(t, m, topoFixture())
}

// ── Loading ──────────────────────────────────────────────────────────────────

func TestTopologyStartsLoading(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))

	if m.topologyModel.state != topoStateLoading {
		t.Errorf("state = %d before any data, want loading", m.topologyModel.state)
	}
	// The load is reported in the footer, never in the pane.
	if !strings.Contains(m.RenderFooter(120), "Loading network data") {
		t.Error("the topology tab does not report the first fetch in the footer")
	}
	if strings.Contains(m.View(), "Loading network data") {
		t.Error("the pane reports the load; it belongs in the footer alone")
	}
}

func TestTopologySpinnerRunsOnlyWhileLoading(t *testing.T) {
	loading := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))

	_, cmd := step(t, loading, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while the topology data was loading")
	}

	_, cmd = step(t, topologyModel(t), spinner.TickMsg{})
	if cmd != nil {
		t.Error("the spinner kept running after the data arrived")
	}
}

func TestTopologyDataEndsTheLoadingState(t *testing.T) {
	m := topologyModel(t)

	if m.topologyModel.state != topoStateReady {
		t.Errorf("state = %d after the data arrived, want ready", m.topologyModel.state)
	}
	if m.topologyModel.loadErr != "" {
		t.Errorf("loadErr = %q after a successful fetch", m.topologyModel.loadErr)
	}
}

func TestTopologyFetchFailureSurfacesAShortMessage(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))

	m = feed(t, m, topoDataMsg{err: errors.New("no such image")})

	if m.topologyModel.loadErr == "" {
		t.Error("a failed fetch left the error empty")
	}
	if strings.Contains(m.topologyModel.loadErr, "no such image") {
		t.Errorf("loadErr = %q leaks the raw error", m.topologyModel.loadErr)
	}
	// The tab still leaves the loading state, so the user is not stuck on a
	// spinner that will never resolve.
	if m.topologyModel.state != topoStateReady {
		t.Error("a failed fetch left the tab spinning forever")
	}
	if !strings.Contains(m.View(), "Failed to load network data") {
		t.Error("the failure is not shown in the viewport")
	}
}

// ── Rendering ────────────────────────────────────────────────────────────────

func TestTopologyRendersEverySection(t *testing.T) {
	out := topologyModel(t).View()

	for _, want := range []string{"eth0", "192.168.1.5/24", "192.168.1.1", "aa:bb:cc:dd:ee:ff", "INPUT", "iptables"} {
		if !strings.Contains(out, want) {
			t.Errorf("the topology view is missing %q", want)
		}
	}
}

func TestTopologyRendersAnEmptyFetch(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))

	m = feed(t, m, topoDataMsg{firewallSrc: "unavailable"})

	if out := m.View(); out == "" {
		t.Error("the topology view is blank for an empty result")
	}
}

// ── Navigation ───────────────────────────────────────────────────────────────

func TestTopologyScrolling(t *testing.T) {
	m := topologyModel(t)

	m = feed(t, m, testutil.Key("G"))
	atBottom := m.topologyModel.viewport.YOffset

	m = feed(t, m, testutil.Key("g"))
	if m.topologyModel.viewport.YOffset != 0 {
		t.Errorf("YOffset = %d after g, want the top", m.topologyModel.viewport.YOffset)
	}

	m = feed(t, m, testutil.Key("down"))
	if atBottom > 0 && m.topologyModel.viewport.YOffset != 1 {
		t.Errorf("YOffset = %d after down, want 1", m.topologyModel.viewport.YOffset)
	}

	m = feed(t, m, testutil.Key("pgdown"))
	if atBottom > 0 && m.topologyModel.viewport.YOffset <= 1 {
		t.Error("pgdown did not scroll")
	}
}

// Keys must not act on a viewport that has no content yet.
func TestTopologyKeysAreInertWhileLoading(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if cmd != nil {
		t.Error("ctrl+r triggered a second fetch while the first was still running")
	}
	if m.topologyModel.state != topoStateLoading {
		t.Error("a key changed the state while loading")
	}
}

func TestTopologyRefreshReturnsToLoading(t *testing.T) {
	m := topologyModel(t)
	m.topologyModel.loadErr = "stale failure"

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if m.topologyModel.state != topoStateLoading {
		t.Errorf("state = %d after ctrl+r, want loading", m.topologyModel.state)
	}
	if m.topologyModel.loadErr != "" {
		t.Error("the previous failure survived the refresh")
	}
	if cmd == nil {
		t.Error("ctrl+r issued no fetch")
	}
}

func TestTopologyResizeRebuildsTheViewport(t *testing.T) {
	m := feed(t, topologyModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.topologyModel.viewport.Width != 98 {
		t.Errorf("viewport width = %d, want the terminal minus the borders", m.topologyModel.viewport.Width)
	}
	if !strings.Contains(m.View(), "eth0") {
		t.Error("the content was lost on resize")
	}
}

func TestSubModelsDeclareNoEditMode(t *testing.T) {
	m := topologyModel(t)

	if m.topologyModel.InEditMode() {
		t.Error("the topology tab claims an edit mode; it has no text inputs")
	}
	if m.InEditMode() {
		t.Error("the parent reports edit mode on the topology tab")
	}
}

func TestTopologyResizeHasAFloor(t *testing.T) {
	m := feed(t, topologyModel(t), tea.WindowSizeMsg{Width: 5, Height: 1})

	if m.topologyModel.viewport.Width < 20 || m.topologyModel.viewport.Height < 3 {
		t.Errorf("viewport = %dx%d on a tiny terminal, want the floors of 20x3",
			m.topologyModel.viewport.Width, m.topologyModel.viewport.Height)
	}
}

// TestAFailedRefreshDatesTheSectionsItKeeps — the error banner was already
// persistent, so it was visible; what was missing is that the sections under it
// are from an earlier load. An error line above data the reader takes for
// current is the same defect the ports table had, one screen over.
func TestAFailedRefreshDatesTheSectionsItKeeps(t *testing.T) {
	m := topologyModel(t)
	before := len(m.topologyModel.interfaces)
	if before == 0 {
		t.Fatal("the fixture loaded no interfaces")
	}

	m = feed(t, m, topoDataMsg{err: errors.New("cannot connect to the Docker daemon")})

	if got := len(m.topologyModel.interfaces); got != before {
		t.Fatalf("the pane holds %d interfaces after a failed refresh, want %d", got, before)
	}
	if !strings.Contains(m.topologyModel.loadErr, "Refresh failed") {
		t.Errorf("banner = %q, does not say the refresh is what failed", m.topologyModel.loadErr)
	}
	// theme.TimeAgo (Rule 127) says "now" under a minute and "5 min ago" past
	// it, so the banner is phrased to read with either.
	if !strings.Contains(m.topologyModel.loadErr, "last loaded:") {
		t.Errorf("banner = %q, does not date the sections below it", m.topologyModel.loadErr)
	}
}

// TestAFirstLoadThatFailsIsAPlainFailure — with nothing on screen there is
// nothing to date, and "showing the load from" would be a lie.
func TestAFirstLoadThatFailsIsAPlainFailure(t *testing.T) {
	m := newTestModel(t)
	m.activeTab = tabTopology
	m = feed(t, m, topoDataMsg{err: errors.New("daemon down")})

	if strings.Contains(m.topologyModel.loadErr, "Refresh failed") {
		t.Errorf("banner = %q claims to show an earlier load that never happened",
			m.topologyModel.loadErr)
	}
	if m.topologyModel.loadErr == "" {
		t.Error("a failed first load said nothing")
	}
}
