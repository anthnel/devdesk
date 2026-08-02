package netdiag

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	dockerpkg "github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The ports tab polls `ss` inside a privileged container, so no test executes a
// command. These drive the sub-model through the parent's Update, which is how
// the app reaches it.

// portFixtures cover both protocols and both states, plus one entry with no PID
// (kernel sockets have none) and one with a hostname rather than an address.
func portFixtures() []dockerpkg.PortInfo {
	return []dockerpkg.PortInfo{
		{Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:22", PeerAddr: "0.0.0.0:*", PID: "812", Process: "sshd"},
		{Protocol: "tcp", State: "ESTAB", LocalAddr: "10.0.0.5:443", PeerAddr: "93.184.216.34:52344", PID: "1204", Process: "nginx"},
		{Protocol: "udp", State: "UNCONN", LocalAddr: "0.0.0.0:53", PeerAddr: "0.0.0.0:*", PID: "440", Process: "dnsmasq"},
		{Protocol: "udp", State: "ESTAB", LocalAddr: "localhost:123", PeerAddr: "ntp.example.com:123", PID: "", Process: "kernel"},
	}
}

// portsModel returns the parent model on the ports tab, with data loaded.
func portsModel(t *testing.T) *Model {
	t.Helper()
	m := feed(t, newTestModel(t), testutil.Key("tab"))
	return feed(t, m, portsDataMsg{ports: portFixtures()})
}

func portRows(m *Model) []string {
	var out []string
	for _, row := range m.portsModel.table.Rows() {
		out = append(out, row[5]) // Process column
	}
	return out
}

// ── Data ─────────────────────────────────────────────────────────────────────

func TestPortsDataPopulatesTheTable(t *testing.T) {
	m := portsModel(t)

	if got := len(m.portsModel.filtered); got != 4 {
		t.Errorf("%d entries after loading, want the four fixtures", got)
	}
	if got := portRows(m); len(got) != 4 {
		t.Errorf("the table holds %v, want four rows", got)
	}
	if m.portsModel.footerError != "" {
		t.Errorf("footerError = %q after a successful fetch", m.portsModel.footerError)
	}
}

func TestPortsFetchFailureSurfacesAShortMessage(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"))

	m, cmd := step(t, m, portsDataMsg{err: errors.New("permission denied")})

	if m.portsModel.footerError == "" {
		t.Error("a failed fetch left the footer empty")
	}
	if strings.Contains(m.portsModel.footerError, "permission denied") {
		t.Errorf("footerError = %q leaks the raw error; Rule 128 wants a short message plus a log", m.portsModel.footerError)
	}
	// Rule 128: a footer message must come with the timer that clears it.
	if cmd == nil {
		t.Error("no command returned, so the footer message would never clear")
	}
}

func TestPortsClearFooterEmptiesBoth(t *testing.T) {
	m := portsModel(t)
	m.portsModel.footerError = "something"
	m.portsModel.footerInfo = "something else"

	m = feed(t, m, portsClearFooterMsg{})

	if m.portsModel.footerError != "" || m.portsModel.footerInfo != "" {
		t.Error("the ports footer survived its clear message")
	}
}

// ── Polling ──────────────────────────────────────────────────────────────────

func TestTickRefetchesUnlessPaused(t *testing.T) {
	m := portsModel(t)

	_, cmd := step(t, m, portsTickMsg{})
	if cmd == nil {
		t.Error("the tick issued no command, so the table goes stale")
	}

	m = feed(t, m, testutil.Key(" "))
	if !m.portsModel.paused {
		t.Fatal("space did not pause the poll")
	}
	// Paused still reschedules the tick — it just does not fetch.
	_, cmd = step(t, m, portsTickMsg{})
	if cmd == nil {
		t.Error("pausing stopped the tick loop entirely, so resuming would never poll again")
	}
}

func TestPauseIsAdvertisedAsAToken(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key(" "))

	if !m.portsModel.filterBar.IsTokenActive(filterTokenPaused) {
		t.Error("pausing did not light the paused token")
	}
	if m.portsModel.footerInfo == "" {
		t.Error("pausing said nothing in the footer")
	}

	m = feed(t, m, testutil.Key(" "))
	if m.portsModel.filterBar.IsTokenActive(filterTokenPaused) {
		t.Error("resuming left the paused token lit")
	}
	if m.portsModel.footerInfo != "" {
		t.Errorf("footerInfo = %q after resuming, want it cleared", m.portsModel.footerInfo)
	}
}

// ── Filters ──────────────────────────────────────────────────────────────────

func TestProtocolAndStateFiltersAreOredWithinAndAndedAcross(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want []string
	}{
		{"no filter", nil, []string{"sshd", "nginx", "dnsmasq", "kernel"}},
		{"tcp only", []string{"t"}, []string{"sshd", "nginx"}},
		{"udp only", []string{"u"}, []string{"dnsmasq", "kernel"}},
		{"tcp or udp is everything", []string{"t", "u"}, []string{"sshd", "nginx", "dnsmasq", "kernel"}},
		{"listen only", []string{"l"}, []string{"sshd"}},
		{"estab only", []string{"e"}, []string{"nginx", "kernel"}},
		{"tcp and estab", []string{"t", "e"}, []string{"nginx"}},
		{"udp and listen matches nothing", []string{"u", "l"}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := portsModel(t)
			for _, key := range tc.keys {
				m = feed(t, m, testutil.Key(key))
			}

			got := portRows(m)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("row %d = %q, want %q (full set %v)", i, got[i], want, got)
				}
			}
		})
	}
}

func TestTogglingAFilterTwiceRestoresEverything(t *testing.T) {
	m := portsModel(t)

	m = feed(t, m, testutil.Key("t"), testutil.Key("t"))

	if got := portRows(m); len(got) != 4 {
		t.Errorf("rows = %v after toggling tcp off again, want all four", got)
	}
}

func TestSearchMatchesEveryColumn(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"nginx", []string{"nginx"}},           // process
		{"812", []string{"sshd"}},              // pid
		{"0.0.0.0:53", []string{"dnsmasq"}},    // local address
		{"ntp.example", []string{"kernel"}},    // peer address
		{"udp", []string{"dnsmasq", "kernel"}}, // protocol
		{"listen", []string{"sshd"}},           // state
		{"nomatch", nil},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			m := feed(t, portsModel(t), testutil.Key("/"))
			m = feed(t, m, testutil.Type(tc.query)...)

			got := portRows(m)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("row %d = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestSearchAndTokensCombine(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("t")) // tcp only
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("estab")...)

	if got := portRows(m); len(got) != 1 || got[0] != "nginx" {
		t.Errorf("rows = %v, want only nginx (tcp AND matching the search)", got)
	}
}

// z is the escape hatch: it has to clear every filter, the search and the pause.
func TestResetClearsEveryFilter(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("t"), testutil.Key("l"), testutil.Key(" "))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("sshd")...)
	// enter confirms the query and gives the keyboard back to the view; without
	// it, z would just be another character in the search box.
	m = feed(t, m, testutil.Key("enter"))
	if m.portsModel.filterBar.InEditMode() {
		t.Fatal("enter did not confirm the search")
	}

	m = feed(t, m, testutil.Key("z"))

	if got := portRows(m); len(got) != 4 {
		t.Errorf("rows = %v after reset, want all four back", got)
	}
	if m.portsModel.paused {
		t.Error("reset left the poll paused")
	}
	if m.portsModel.filterBar.SearchQuery() != "" {
		t.Errorf("reset left the search query %q", m.portsModel.filterBar.SearchQuery())
	}
	for _, token := range []string{filterTokenTCP, filterTokenUDP, filterTokenListen, filterTokenEstab, filterTokenPaused} {
		if m.portsModel.filterBar.IsTokenActive(token) {
			t.Errorf("reset left the %q token active", token)
		}
	}
}

// Toggling numeric addresses changes what `ss` is asked for, so it must refetch
// rather than re-render the data it already has.
func TestNumericToggleRefetches(t *testing.T) {
	m := portsModel(t)
	before := m.portsModel.numericAddrs

	m, cmd := step(t, m, testutil.Key("n"))

	if m.portsModel.numericAddrs == before {
		t.Error("n did not toggle numeric addresses")
	}
	if m.portsModel.filterBar.IsTokenActive(filterTokenNumeric) == before {
		t.Error("the numeric token did not follow the setting")
	}
	if cmd == nil {
		t.Error("toggling numeric addresses did not refetch")
	}
}

// ── Kill ─────────────────────────────────────────────────────────────────────

func TestKillRequiresAPID(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.SetCursor(3) // the kernel socket, which has no PID

	m, cmd := step(t, m, testutil.Key("ctrl+k"))

	if m.portsModel.footerInfo == "" {
		t.Error("killing a PID-less entry said nothing")
	}
	if cmd == nil {
		t.Error("no command returned, so the footer message would never clear")
	}
}

func TestKillIssuesACommandForAnEntryWithAPID(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.SetCursor(0) // sshd, pid 812

	_, cmd := step(t, m, testutil.Key("ctrl+k"))

	if cmd == nil {
		t.Error("ctrl+k on a killable entry issued no command")
	}
}

func TestKillIsInertWithoutASelection(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab")) // no data yet

	_, cmd := step(t, m, testutil.Key("ctrl+k"))

	if cmd != nil {
		t.Error("ctrl+k issued a command with nothing selected")
	}
}

func TestKillResultReportsBothOutcomes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m, cmd := step(t, portsModel(t), portsKillResultMsg{pid: "812"})

		if !strings.Contains(m.portsModel.footerInfo, "812") {
			t.Errorf("footerInfo = %q, want it to name the terminated process", m.portsModel.footerInfo)
		}
		if cmd == nil {
			t.Error("no clear timer after the kill result")
		}
	})

	t.Run("failure", func(t *testing.T) {
		m, _ := step(t, portsModel(t), portsKillResultMsg{pid: "812", err: errors.New("operation not permitted")})

		if !strings.Contains(m.portsModel.footerError, "812") {
			t.Errorf("footerError = %q, want it to name the PID", m.portsModel.footerError)
		}
		if strings.Contains(m.portsModel.footerError, "not permitted") {
			t.Error("the raw error leaked into the footer")
		}
	})
}

// ── Navigation and rendering ─────────────────────────────────────────────────

func TestPortsNavigation(t *testing.T) {
	m := portsModel(t)

	m = feed(t, m, testutil.Key("G"))
	if got := m.portsModel.table.Cursor(); got != 3 {
		t.Errorf("cursor = %d after G, want the last row", got)
	}
	m = feed(t, m, testutil.Key("g"))
	if got := m.portsModel.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after g, want the top", got)
	}
	m = feed(t, m, testutil.Key("j"))
	if got := m.portsModel.table.Cursor(); got != 1 {
		t.Errorf("cursor = %d after j, want 1", got)
	}
	m = feed(t, m, testutil.Key("k"))
	if got := m.portsModel.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after k, want 0", got)
	}
	m = feed(t, m, testutil.Key("pgdown"))
	if m.portsModel.table.Cursor() == 0 {
		t.Error("pgdown did not move the cursor")
	}
}

// A refresh must not throw the user back to the top of a long list.
func TestDataRefreshPreservesTheCursor(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("G"))
	cursor := m.portsModel.table.Cursor()

	m = feed(t, m, portsDataMsg{ports: portFixtures()})

	if got := m.portsModel.table.Cursor(); got != cursor {
		t.Errorf("cursor = %d after a refresh, want it preserved at %d", got, cursor)
	}
}

// Changing a filter does reset the cursor, since the rows underneath it moved.
func TestFilterChangeResetsTheCursor(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("G"))

	m = feed(t, m, testutil.Key("t"))

	if got := m.portsModel.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after changing a filter, want the top", got)
	}
}

func TestPortsViewStates(t *testing.T) {
	loading := feed(t, newTestModel(t), testutil.Key("tab"))
	if !strings.Contains(loading.View(), "Loading ports") {
		t.Error("the ports tab does not report the first fetch")
	}

	empty := feed(t, loading, portsDataMsg{ports: []dockerpkg.PortInfo{}})
	if !strings.Contains(empty.View(), "No active ports found") {
		t.Error("the ports tab does not report an empty result")
	}

	loaded := portsModel(t)
	if !strings.Contains(loaded.View(), "sshd") {
		t.Error("the ports table is not rendered once data arrives")
	}
}

// With a filter active the table stays rendered even when it matches nothing,
// so the filter bar keeps its place (Rule 136).
func TestPortsViewKeepsTheTableWhenAFilterMatchesNothing(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("nomatch")...)

	if strings.Contains(m.View(), "No active ports found") {
		t.Error("the empty state replaced the table while a filter was active")
	}
}

func TestPortsInEditModePropagatesToTheParent(t *testing.T) {
	m := portsModel(t)
	if m.InEditMode() {
		t.Error("InEditMode() is true on the ports tab before any search")
	}
	if m.FilterBarVisible() {
		t.Error("the filter bar is visible before any search")
	}

	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Error("InEditMode() is false while the ports search has focus; ':' would be stolen")
	}
	if !m.FilterBarVisible() {
		t.Error("the filter bar is hidden while searching")
	}
}

// Rule 122: no escape sequences in cell values.
func TestPortsCellsCarryNoANSISequences(t *testing.T) {
	m := portsModel(t)

	for _, row := range m.portsModel.table.Rows() {
		for i, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("cell %d = %q contains an escape sequence", i, cell)
			}
		}
	}
}

func TestPortsResizeFillsTheViewportWidth(t *testing.T) {
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})

	total := 0
	for _, col := range m.portsModel.table.Columns() {
		total += col.Width
	}
	if want := 160 - 2 - 6*2; total != want {
		t.Errorf("columns total %d, want %d so the selected row reaches the border", total, want)
	}
}
