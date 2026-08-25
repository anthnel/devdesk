package netdiag

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The ports tab reads the socket table in this process (§3.43), so no test
// executes a command. These drive the sub-model through the parent's Update,
// which is how the app reaches it.

// portFixtures cover both protocols and both states, plus one entry with no PID
// (kernel sockets have none) and one with a hostname rather than an address.
func portFixtures() []ports.Socket {
	return []ports.Socket{
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
	for _, row := range m.portsModel.table.Table().Rows() {
		out = append(out, row[5]) // Process column
	}
	return out
}

// ── Data ─────────────────────────────────────────────────────────────────────

func TestPortsDataPopulatesTheTable(t *testing.T) {
	m := portsModel(t)

	if got := len(m.portsModel.table.Visible()); got != 4 {
		t.Errorf("%d entries after loading, want the four fixtures", got)
	}
	if got := portRows(m); len(got) != 4 {
		t.Errorf("the table holds %v, want four rows", got)
	}
	if m.portsModel.footer.IsSet() {
		t.Errorf("footer = %q after a successful fetch", m.portsModel.footer.Text())
	}
}

// TestAFailedFetchKeepsTheRowsAndDatesThem is the defect this replaces: the
// early return kept the table and the footer message expired after three
// seconds, leaving a table refreshing into failure with nothing on screen
// saying so. Dropping the rows would be the wrong fix — on a two-second tick a
// transient hiccup would flash the table empty, and the rows are not wrong,
// they are dated.
func TestAFailedFetchKeepsTheRowsAndDatesThem(t *testing.T) {
	m := portsModel(t)
	before := len(m.portsModel.table.Items())
	if before == 0 {
		t.Fatal("the fixture has no rows to keep")
	}

	m, cmd := step(t, m, portsDataMsg{err: errors.New("cannot connect to the Docker daemon")})

	if got := len(m.portsModel.table.Items()); got != before {
		t.Fatalf("the table holds %d rows after a failure, want the %d it had", got, before)
	}
	if !m.portsModel.stale {
		t.Fatal("the failure was not recorded as staleness")
	}
	// Rule 128: this is a state, not an event. A message would expire, and its
	// expiry is exactly what made a dead table look alive.
	if m.portsModel.footer.IsSet() {
		t.Errorf("footer = %q; staleness belongs to the status line, which has no timer",
			m.portsModel.footer.Text())
	}
	if cmd != nil {
		t.Error("a command was returned for a state that needs no timer")
	}

	status := m.portsModel.statusLine().Text
	if !strings.Contains(status, "could not be read") {
		t.Errorf("status = %q, does not say the socket table could not be read", status)
	}
	if !strings.Contains(status, "as of") {
		t.Errorf("status = %q, does not date the rows on screen", status)
	}
	if strings.Contains(status, "cannot connect") {
		t.Errorf("status = %q leaks the raw error; Rule 128 wants a short line plus a log", status)
	}
}

// TestStalenessOutranksThePause — a pause is what the user asked for, a socket
// table that will not answer is not, and only one of the two makes the rows lie.
func TestStalenessOutranksThePause(t *testing.T) {
	m := portsModel(t)
	m = feed(t, m, testutil.Key(" "))
	if !m.portsModel.paused {
		t.Fatal("space did not pause")
	}
	m = feed(t, m, portsDataMsg{err: errors.New("socket table refused")})

	if got := m.portsModel.statusLine().Text; !strings.Contains(got, "could not be read") {
		t.Fatalf("status = %q, want the staleness to win over the pause", got)
	}
}

// TestAFetchThatNeverSucceededSaysSoRatherThanDatingNothing — TimeAgo renders
// the zero time as an empty string, so "ports as of " would trail off.
func TestAFetchThatNeverSucceededSaysSoRatherThanDatingNothing(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"))
	m = feed(t, m, portsDataMsg{err: errors.New("socket table refused")})

	status := m.portsModel.statusLine().Text
	if strings.HasSuffix(status, "as of ") || strings.Contains(status, "as of") {
		t.Fatalf("status = %q dates rows that were never fetched", status)
	}
	if !strings.Contains(status, "could not be read") {
		t.Fatalf("status = %q", status)
	}
	// And the body must not claim the host has no open ports.
	if body := m.portsModel.view(); strings.Contains(body, "No active ports found") {
		t.Error("the empty state claims there are no ports when none could be read")
	}
}

// TestASuccessfulFetchClearsTheStaleness — the state has to end, or a recovered
// daemon still reads as dead.
func TestASuccessfulFetchClearsTheStaleness(t *testing.T) {
	m := portsModel(t)
	m = feed(t, m, portsDataMsg{err: errors.New("socket table refused")})
	if !m.portsModel.stale {
		t.Fatal("the failure was not recorded")
	}

	m = feed(t, m, portsDataMsg{ports: portFixtures()})
	if m.portsModel.stale {
		t.Fatal("a successful fetch left the staleness set")
	}
	if got := m.portsModel.statusLine().Text; got != "" {
		t.Errorf("status = %q after recovery, want empty", got)
	}
}

func TestThePortsFooterClearsOnItsOwnExpiry(t *testing.T) {
	m := portsModel(t)
	m.portsModel.footer.Error("something")

	m = feed(t, m, components.ClearFooterMsg{ID: m.portsModel.footer.ID()})

	if m.portsModel.footer.IsSet() {
		t.Errorf("the ports footer holds %q after its expiry", m.portsModel.footer.Text())
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

	if !m.portsModel.table.FilterBar().IsTokenActive(filterTokenPaused) {
		t.Error("pausing did not light the paused token")
	}
	// Pausing is a state, so it is derived rather than set as a message — one
	// would expire after three seconds while the tab is still paused.
	if m.portsModel.statusLine().Text == "" {
		t.Error("pausing said nothing in the footer")
	}

	m = feed(t, m, testutil.Key(" "))
	if m.portsModel.table.FilterBar().IsTokenActive(filterTokenPaused) {
		t.Error("resuming left the paused token lit")
	}
	if got := m.portsModel.statusLine().Text; got != "" {
		t.Errorf("footer = %q after resuming, want it cleared", got)
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
	if m.portsModel.table.FilterBar().InEditMode() {
		t.Fatal("enter did not confirm the search")
	}

	m = feed(t, m, testutil.Key("z"))

	if got := portRows(m); len(got) != 4 {
		t.Errorf("rows = %v after reset, want all four back", got)
	}
	if m.portsModel.paused {
		t.Error("reset left the poll paused")
	}
	if m.portsModel.table.FilterBar().SearchQuery() != "" {
		t.Errorf("reset left the search query %q", m.portsModel.table.FilterBar().SearchQuery())
	}
	for _, token := range []string{filterTokenTCP, filterTokenUDP, filterTokenListen, filterTokenEstab, filterTokenPaused} {
		if m.portsModel.table.FilterBar().IsTokenActive(token) {
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
	if m.portsModel.table.FilterBar().IsTokenActive(filterTokenNumeric) == before {
		t.Error("the numeric token did not follow the setting")
	}
	if cmd == nil {
		t.Error("toggling numeric addresses did not refetch")
	}
}

// ── Kill ─────────────────────────────────────────────────────────────────────

func TestKillRequiresAPID(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.Table().SetCursor(3) // the kernel socket, which has no PID

	m, cmd := step(t, m, testutil.Key(keymap.Kill))

	if m.portsModel.confirmModal != nil {
		t.Error("a PID-less entry was offered a confirmation it cannot act on")
	}
	if !m.portsModel.footer.IsSet() {
		t.Error("killing a PID-less entry said nothing")
	}
	if cmd == nil {
		t.Error("no command returned, so the footer message would never clear")
	}
}

func TestKillIssuesACommandForAnEntryWithAPID(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.Table().SetCursor(0) // sshd, pid 812

	m, _ = step(t, m, testutil.Key(keymap.Kill))

	// K asks first: this is a SIGKILL on a process of the host, not a container
	// to be brought back up (§3.26).
	if m.portsModel.confirmModal == nil {
		t.Fatal("K on a killable entry did not ask for confirmation")
	}
	if _, cmd := step(t, m, components.ConfirmModalYesMsg{}); cmd == nil {
		t.Error("confirming the kill issued no command")
	}
}

// Rule 130: K is greyed where there is nothing to signal, and pressing it
// anyway says why rather than returning in silence.
func TestKillIsGreyedWithoutASelection(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab")) // no data yet

	if !testutil.HasShortcut(m.GetShortcuts(), keymap.Kill) {
		t.Fatal("K disappeared from an empty table instead of being greyed")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
		t.Error("K is offered with nothing selected")
	}

	next, _ := step(t, m, testutil.Key(keymap.Kill))

	if next.portsModel.confirmModal != nil {
		t.Error("K opened the confirmation with nothing selected")
	}
	if got := next.portsModel.footer.Text(); !strings.Contains(got, reasonNoSocketRow) {
		t.Errorf("footer = %q, want it to carry %q", got, reasonNoSocketRow)
	}
}

// A socket the system declines to attribute carries no PID: List blanks a PID
// of zero rather than printing it, so the row would otherwise look killable.
func TestKillIsGreyedOnASocketWithNoPID(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"))
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{{
		Protocol: "tcp", LocalAddr: "0.0.0.0:445", State: "LISTEN",
	}}})

	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
		t.Error("K is offered on a socket the system attributed to no process")
	}

	next, _ := step(t, m, testutil.Key(keymap.Kill))

	if next.portsModel.confirmModal != nil {
		t.Error("K opened the confirmation for a socket with no PID")
	}
	if got := next.portsModel.footer.Text(); !strings.Contains(got, reasonNoPID) {
		t.Errorf("footer = %q, want it to carry %q", got, reasonNoPID)
	}
}

// A row the system did attribute stays killable, whatever the OS will make of
// the signal — that is the attempt's answer, not the header's.
func TestKillIsOfferedOnAnAttributedSocket(t *testing.T) {
	if !testutil.ShortcutEnabled(portsModel(t).GetShortcuts(), keymap.Kill) {
		t.Error("K is greyed on a socket with a PID")
	}
}

// DevDesk signals with the rights it has (§3.43), so the refusal the user meets
// most often is the operating system's. "Failed to kill PID N" made "access
// denied" and "already gone" the same sentence, and gave no reason to suspect
// the first.
func TestAKillRefusedBySystemSaysSo(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"permission", fmt.Errorf("killing process 812: %w", os.ErrPermission), "Refused by the system"},
		{"already gone", fmt.Errorf("killing process 812: %w", os.ErrProcessDone), "no longer running"},
		{"anything else", errors.New("operation not permitted"), "check logs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := step(t, portsModel(t), portsKillResultMsg{pid: "812", err: tt.err})

			got := m.portsModel.footer.Text()
			if !strings.Contains(got, tt.want) {
				t.Errorf("footer = %q, want it to carry %q", got, tt.want)
			}
			if !strings.Contains(got, "812") {
				t.Errorf("footer = %q, want it to name the PID", got)
			}
		})
	}
}

// The platform error never reaches the screen: Windows returns it in the
// machine's own language, which is the route stage's rule (§3.44).
func TestTheOperatingSystemsOwnWordsNeverReachTheFooter(t *testing.T) {
	m, _ := step(t, portsModel(t), portsKillResultMsg{
		pid: "812",
		err: fmt.Errorf("killing process 812: %w", errors.New("Zugriff verweigert")),
	})

	if strings.Contains(m.portsModel.footer.Text(), "Zugriff") {
		t.Errorf("the platform error leaked into the footer: %q", m.portsModel.footer.Text())
	}
}

func TestKillResultReportsBothOutcomes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m, cmd := step(t, portsModel(t), portsKillResultMsg{pid: "812"})

		if !strings.Contains(m.portsModel.footer.Text(), "812") {
			t.Errorf("footer = %q, want it to name the terminated process", m.portsModel.footer.Text())
		}
		if cmd == nil {
			t.Error("no clear timer after the kill result")
		}
	})

}

// ── Navigation and rendering ─────────────────────────────────────────────────

func TestPortsNavigation(t *testing.T) {
	m := portsModel(t)

	m = feed(t, m, testutil.Key("end"))
	if got := m.portsModel.table.Cursor(); got != 3 {
		t.Errorf("cursor = %d after G, want the last row", got)
	}
	m = feed(t, m, testutil.Key("home"))
	if got := m.portsModel.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after home, want the top", got)
	}
	m = feed(t, m, testutil.Key("down"))
	if got := m.portsModel.table.Cursor(); got != 1 {
		t.Errorf("cursor = %d after down, want 1", got)
	}
	m = feed(t, m, testutil.Key("up"))
	if got := m.portsModel.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after up, want 0", got)
	}
	m = feed(t, m, testutil.Key("pgdown"))
	if m.portsModel.table.Cursor() == 0 {
		t.Error("pgdown did not move the cursor")
	}
}

// A refresh must not throw the user back to the top of a long list.
func TestDataRefreshPreservesTheCursor(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("end"))
	cursor := m.portsModel.table.Cursor()

	m = feed(t, m, portsDataMsg{ports: portFixtures()})

	if got := m.portsModel.table.Cursor(); got != cursor {
		t.Errorf("cursor = %d after a refresh, want it preserved at %d", got, cursor)
	}
}

// Changing a filter does reset the cursor, since the rows underneath it moved.
func TestFilterChangeResetsTheCursor(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key("end"))

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

	empty := feed(t, loading, portsDataMsg{ports: []ports.Socket{}})
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

	for _, row := range m.portsModel.table.Table().Rows() {
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
	for _, col := range m.portsModel.table.Table().Columns() {
		total += col.Width
	}
	if want := 160 - 2 - 6*2; total != want {
		t.Errorf("columns total %d, want %d so the selected row reaches the border", total, want)
	}
}

// ── The datatable migration (§2 step 3) ──────────────────────────────────────

// The reason the tableReady / lastTableWidth / lastTableHeight trio existed: the
// table refreshes every two seconds, and rebuilding it from scratch each time
// threw away where the user was looking. SetItems is what replaced it, so this
// is the invariant that has to hold without the workaround.
func TestScrollSurvivesTheTwoSecondRefresh(t *testing.T) {
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, testutil.Key("down"), testutil.Key("down"))

	before := m.portsModel.table.Cursor()
	if before != 2 {
		t.Fatalf("cursor = %d after two downs, want 2", before)
	}
	selected, _ := m.portsModel.table.Selected()

	// A refresh returning the same entries, as a quiet system would.
	m = feed(t, m, portsDataMsg{ports: portFixtures()})

	if got := m.portsModel.table.Cursor(); got != before {
		t.Errorf("cursor = %d after a refresh, want it left at %d", got, before)
	}
	after, ok := m.portsModel.table.Selected()
	if !ok || after.Process != selected.Process {
		t.Errorf("the selection moved from %q to %+v across a refresh", selected.Process, after)
	}
}

// The other half: a refresh that returns fewer entries — a process exited —
// must not leave the cursor pointing past the end.
func TestARefreshWithFewerPortsClampsTheCursor(t *testing.T) {
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, testutil.Key("end")) // last row

	m = feed(t, m, portsDataMsg{ports: portFixtures()[:2]})

	entry, ok := m.portsModel.table.Selected()
	if !ok {
		t.Fatal("the cursor was left past the end of a shorter list")
	}
	if entry.Process != "nginx" {
		t.Errorf("selected %q, want the last entry that is left", entry.Process)
	}
}

// Rule 116 at a width the old arithmetic got wrong: it clamped the content
// width at 30 and floored the last column at 10, so the columns summed to more
// than the space they had.
func TestPortsColumnsHoldTheWidthInvariantWhenNarrow(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: width, Height: 40})

		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		if got, want := m.portsModel.table.RenderedWidth(), width-2; got != want {
			t.Errorf("at width %d the line spans %d, want %d", width, got, want)
		}
	}
}

// ctrl+k acts on the highlighted row. The kill used to index a separately-held
// filtered slice, which is the coupling that can silently kill the wrong PID.
func TestKillActsOnTheHighlightedRow(t *testing.T) {
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, testutil.Key("t")) // tcp only, which reorders what is visible
	m = feed(t, m, testutil.Key("down"))

	entry, ok := m.portsModel.table.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}
	if got := portRows(m)[m.portsModel.table.Cursor()]; got != entry.Process {
		t.Errorf("the highlighted row shows %q while ctrl+k would act on %q", got, entry.Process)
	}
}

// A query spanning two columns matched the joined haystack before the
// migration, and has to keep matching.
func TestAQuerySpanningColumnsStillMatches(t *testing.T) {
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, testutil.Key("/"))

	m = feed(t, m, testutil.Type("tcp listen")...)

	if got := len(m.portsModel.table.Visible()); got != 1 {
		t.Errorf("%d entries match \"tcp listen\", want the one — the joined search is gone", got)
	}
}

// ── A kill says it is running (§3.22) ────────────────────────────────────────

// A kill is not the instant a signal sounds like — the process has to go, and
// the table has to re-read to notice. The row used to look exactly as it had.
func TestAKillSpinsTheSocketState(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{}) // sshd, PID 812

	if !m.portsModel.table.IsBusy("812") {
		t.Fatal("the kill was not marked on the row")
	}
	view := m.portsModel.table.View()
	line := ""
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "sshd") {
			line = l
		}
	}
	if strings.Contains(line, "LISTEN") {
		t.Errorf("the row still shows the state the signal is about to change:\n%s", line)
	}
	if !strings.Contains(line, "sshd") {
		t.Errorf("the row lost the process it names:\n%s", line)
	}
}

// A PID is not a socket: killing a process takes every socket it holds, so all
// of its rows spin together — which is what actually happens.
func TestAKillSpinsEveryRowOfThatProcess(t *testing.T) {
	shared := append(portFixtures(),
		ports.Socket{Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:2222", PID: "812", Process: "sshd"},
	)
	m := feed(t, newTestModel(t), testutil.Key("tab"))
	m = feed(t, m, portsDataMsg{ports: shared})

	m = feed(t, m, testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	spinning := 0
	for _, line := range strings.Split(m.portsModel.table.View(), "\n") {
		if strings.Contains(line, "sshd") && !strings.Contains(line, "LISTEN") {
			spinning++
		}
	}
	if spinning != 2 {
		t.Errorf("%d of the process's rows spin, want both", spinning)
	}
}

// On every outcome: a kill that failed has to let the socket state show again
// rather than go on turning.
func TestAFailedKillLiftsTheMarker(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	m = feed(t, m, portsKillResultMsg{pid: "812", err: errors.New("operation not permitted")})

	if m.portsModel.table.IsBusy("812") {
		t.Error("the marker survived a failed kill: the row spins for good")
	}
	if !m.portsModel.footer.IsSet() {
		t.Error("a failed kill reported nothing")
	}
}

func TestASecondKillOfTheSamePIDIsRefused(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	m = feed(t, m, testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	if !strings.Contains(m.portsModel.footer.Text(), "812") {
		t.Errorf("footer = %q, want it to say the kill is already running", m.portsModel.footer.Text())
	}
}

// The tick stops itself once nothing is left running, rather than turning a
// frame nobody is looking at for the life of the view.
func TestTheKillSpinnerTickStopsWhenTheKillLands(t *testing.T) {
	m := feed(t, portsModel(t), testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	_, cmd := m.portsModel.handleSpinnerTick()
	if cmd == nil {
		t.Error("the tick stopped while a kill was still running")
	}

	m = feed(t, m, portsKillResultMsg{pid: "812"})
	if _, cmd := m.portsModel.handleSpinnerTick(); cmd != nil {
		t.Error("the tick went on being scheduled with nothing running")
	}
}
