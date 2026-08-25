package netdiag

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/netiface"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// interfacesModel returns a model on the Interfaces tab, sized, holding items.
func interfacesModel(t *testing.T, items ...netiface.Interface) *Model {
	t.Helper()
	m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: 140, Height: 30})
	m.activeTab = tabInterfaces
	return feed(t, m, ifaceDataMsg{interfaces: items})
}

func counter(n uint64) *uint64 { return &n }

// TestACounterNobodyReadIsADashAndNeverAZero is D58 where the user meets it.
// The counters come from a source that fails on its own, and a zero written
// because nobody looked reads as an interface that has dropped nothing.
func TestACounterNobodyReadIsADashAndNeverAZero(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500,
		MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"192.168.1.21/24"}, IPv6: []string{"fe80::1c2d:3e4f:5a6b:7c8d/64"},
	})

	row := interfaceRowFor(t, m, "Ethernet 2")
	if strings.Contains(row, " 0 ") {
		t.Fatalf("an unread counter rendered as a zero: %q", row)
	}
	if !strings.Contains(row, "-") {
		t.Fatalf("an unread counter did not render as a dash: %q", row)
	}
}

// TestACounterThatWasReadShowsItsFigure is the other half: the dash has to mean
// something, so a counter that was read must never render as one.
func TestACounterThatWasReadShowsItsFigure(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Wi-Fi", State: netiface.StateUp, MTU: 1500,
		RxErrors: counter(0), TxErrors: counter(42),
	})

	row := interfaceRowFor(t, m, "Wi-Fi")
	if !strings.Contains(row, "42") {
		t.Fatalf("a read counter of 42 is not on the row: %q", row)
	}
}

// TestAnMTUThePlatformDoesNotReportIsWithheld covers the loopback under
// Windows, which reports -1 where ip reports 65536.
func TestAnMTUThePlatformDoesNotReportIsWithheld(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Loopback", State: netiface.StateLoop, MTU: -1,
		IPv4: []string{"127.0.0.1/8"},
	})

	if row := interfaceRowFor(t, m, "Loopback"); strings.Contains(row, "-1") {
		t.Fatalf("an MTU of -1 reached the screen: %q", row)
	}
}

// TestTheMacIsShownBecauseItIsFree pins the one field this tab gained over the
// Topology tab it replaced: net.Interfaces() hands the hardware address over
// for nothing, and it is what someone comes to this screen to copy.
func TestTheMacIsShownBecauseItIsFree(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500, MAC: "aa:bb:cc:dd:ee:ff",
	})

	if row := interfaceRowFor(t, m, "Ethernet 2"); !strings.Contains(row, "aa:bb:cc:dd:ee:ff") {
		t.Fatalf("the hardware address is not on the row: %q", row)
	}
}

// TestAFailedReadKeepsTheRowsAndDatesThem is the rule the Ports tab already
// follows: rows that could not be refreshed are not wrong, they are dated, and
// dropping them would blank the tab over a transient failure.
func TestAFailedReadKeepsTheRowsAndDatesThem(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500})
	m = feed(t, m, ifaceDataMsg{err: errRead})

	if !strings.Contains(m.View(), "Ethernet 2") {
		t.Fatal("a failed read emptied the table")
	}
	footer := m.RenderFooter(140)
	if !strings.Contains(footer, "could not be read") {
		t.Fatalf("the failure is not reported in the footer: %q", footer)
	}
	if !strings.Contains(footer, "as of") {
		t.Fatalf("the rows on screen are not dated: %q", footer)
	}
}

// TestAFailedFirstReadSaysSoWithoutDatingNothing covers the case where there
// are no rows to date. "as of" with nothing behind it would be worse than the
// plain statement.
func TestAFailedFirstReadSaysSoWithoutDatingNothing(t *testing.T) {
	m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: 140, Height: 30})
	m.activeTab = tabInterfaces
	m = feed(t, m, ifaceDataMsg{err: errRead})

	if footer := m.RenderFooter(140); strings.Contains(footer, "as of") {
		t.Fatalf("a first read that failed dated rows it never had: %q", footer)
	}
}

// TestTheTableSurvivesARefresh keeps the load out of the body (Rule 139): the
// header and the columns must not disappear while ctrl+r runs.
func TestTheTableSurvivesARefresh(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500})
	m = feed(t, m, testutil.Key("ctrl+r"))

	view := m.View()
	if !strings.Contains(view, "Interface") {
		t.Fatalf("the table header vanished during a refresh: %q", view)
	}
	if !strings.Contains(view, "Ethernet 2") {
		t.Fatal("the rows vanished during a refresh")
	}
	if !strings.Contains(m.RenderFooter(140), "Reading network interfaces") {
		t.Error("the refresh is not reported in the footer")
	}
}

// TestTheHeaderCountsWhatIsUp checks the one header line this tab spends.
func TestTheHeaderCountsWhatIsUp(t *testing.T) {
	m := interfacesModel(t,
		netiface.Interface{Name: "Ethernet 2", State: netiface.StateUp},
		netiface.Interface{Name: "Wi-Fi", State: netiface.StateDown},
		netiface.Interface{Name: "Loopback", State: netiface.StateLoop},
	)

	var got string
	for _, info := range m.GetHeaderInfo("work") {
		if info.Key == "Interfaces" {
			got = info.Value
		}
	}
	if got != "1 up / 3 total" {
		t.Fatalf("header count = %q, want \"1 up / 3 total\"", got)
	}
}

// TestASearchOnTheInterfacesTabClaimsTheKeyboard is Rule 111's other half: a
// ":" typed into the query must be a character, not the command line. The
// Topology tab this replaced had no input at all, so InEditMode returned a
// constant false — which would have been wrong the moment the table gained one.
func TestASearchOnTheInterfacesTabClaimsTheKeyboard(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{Name: "Ethernet 2", State: netiface.StateUp})
	if m.InEditMode() {
		t.Fatal("the tab claims the keyboard with no search open")
	}

	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("an open search does not claim the keyboard")
	}
}

// TestTheFilterBarIsBudgetedInTheFooterHeight keeps the bar from overlapping
// the table: the router takes GetFooterHeight off the viewport, so a bar that
// renders without being counted is drawn over content (Rule 136).
func TestTheFilterBarIsBudgetedInTheFooterHeight(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{Name: "Ethernet 2", State: netiface.StateUp})
	closed := m.GetFooterHeight()

	m = feed(t, m, testutil.Key("/"))
	if open := m.GetFooterHeight(); open <= closed {
		t.Fatalf("footer height stayed %d with the filter bar open, want more than %d", open, closed)
	}
	if !strings.Contains(m.RenderFooter(140), "/") {
		t.Error("the filter bar is not rendered in the footer")
	}
}

// errRead stands in for a platform call that failed.
var errRead = readError("reading the network interfaces: access denied")

type readError string

func (e readError) Error() string { return string(e) }

// interfaceRowFor returns the rendered line carrying name, so an assertion is
// made against what is on screen rather than against the model behind it.
func interfaceRowFor(t *testing.T, m *Model, name string) string {
	t.Helper()
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no row for %q in:\n%s", name, m.View())
	return ""
}

// A read in flight greys the table's keys rather than removing them: the tab is
// the same screen either side of a refresh, and a column that empties and
// refills on every ctrl+r is the flicker Rule 130 is about.
func TestALoadGreysTheInterfaceKeysInsteadOfRemovingThem(t *testing.T) {
	m := interfacesModel(t)
	settled := testutil.ShortcutKeys(m.GetShortcuts())

	m.interfacesModel.loading = true

	loading := testutil.ShortcutKeys(m.GetShortcuts())
	if strings.Join(loading, " ") != strings.Join(settled, " ") {
		t.Errorf("a load advertises %v, want the same keys as a settled tab %v", loading, settled)
	}
	for _, key := range []string{"/", "."} {
		if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%q is offered while the interfaces are being read", key)
		}
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), "ctrl+r") {
		t.Error("ctrl+r is greyed while loading; asking again is what still applies")
	}
}

// ── The two address columns ──────────────────────────────────────────────────

// The families get a column each, so a row is read down one notation rather
// than across a mixed list.
func TestTheAddressesAreSplitByFamily(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500,
		IPv4: []string{"192.168.1.21/24"},
		IPv6: []string{"fe80::1c2d:3e4f:5a6b:7c8d/64"},
	})

	header := interfaceRowFor(t, m, "IPv4")
	if !strings.Contains(header, "IPv6") {
		t.Errorf("the header carries IPv4 but not IPv6: %q", header)
	}

	row := interfaceRowFor(t, m, "Ethernet 2")
	v4 := strings.Index(row, "192.168.1.21")
	v6 := strings.Index(row, "fe80:")
	if v4 < 0 || v6 < 0 {
		t.Fatalf("an address is missing from the row: %q", row)
	}
	if v4 > v6 {
		t.Errorf("IPv6 is rendered before IPv4: %q", row)
	}
}

// A machine with IPv4 only has no IPv6 address — a fact about it, not a reading
// that failed — so the cell says so rather than going blank, as the MAC column
// already does for a loopback.
func TestAFamilyWithNoAddressIsADashAndNotABlank(t *testing.T) {
	m := interfacesModel(t, netiface.Interface{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500,
		IPv4: []string{"192.168.1.21/24"},
	})

	row := interfaceRowFor(t, m, "Ethernet 2")
	if !strings.Contains(row, "192.168.1.21") {
		t.Fatalf("the IPv4 address is missing: %q", row)
	}
	// The IPv4 cell is filled, so any dash after it is the IPv6 one.
	if tail := row[strings.Index(row, "192.168.1.21"):]; !strings.Contains(tail, "-") {
		t.Errorf("the empty IPv6 cell rendered blank instead of a dash: %q", row)
	}
}

// The filter reaches both columns: someone looking for an interface by address
// does not know, or care, which family they are typing.
func TestTheFilterMatchesEitherFamily(t *testing.T) {
	items := []netiface.Interface{
		{Name: "Ethernet 2", State: netiface.StateUp, IPv4: []string{"192.168.1.21/24"}},
		{Name: "Wi-Fi", State: netiface.StateUp, IPv6: []string{"fe80::dead:beef/64"}},
	}

	for _, tt := range []struct{ query, want string }{
		{"192.168", "Ethernet 2"},
		{"dead:beef", "Wi-Fi"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			m := interfacesModel(t, items...)
			m = feed(t, m, testutil.Key("/"))
			for _, r := range tt.query {
				m = feed(t, m, testutil.Key(string(r)))
			}

			visible := m.interfacesModel.table.Visible()
			if len(visible) != 1 || visible[0].Name != tt.want {
				t.Errorf("%q matched %v, want just %s", tt.query, visible, tt.want)
			}
		})
	}
}

// Both address columns are flexible, so a shortfall is levelled between them
// rather than emptying one: `shrink` always takes from the widest flexible
// column, and with the flex on IPv6 alone it fell to zero width — header
// included — from about 100 columns down, which is an ordinary terminal.
//
// 100 is the width this pins because it is the narrowest ordinary one. Below
// about 88 the six fixed columns take everything and both address columns go;
// that cliff is the table's, not this change's — the single Addresses column
// it replaced had the same one — and it is written down rather than pretended
// away.
func TestBothAddressColumnsSurviveAnOrdinaryTerminal(t *testing.T) {
	m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 24})
	m.activeTab = tabInterfaces
	m = feed(t, m, ifaceDataMsg{interfaces: []netiface.Interface{{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500,
		MAC:  "aa:bb:cc:dd:ee:ff",
		IPv4: []string{"192.168.1.21/24"},
		IPv6: []string{"fe80::1c2d:3e4f:5a6b:7c8d/64"},
	}}})

	row := interfaceRowFor(t, m, "Ethernet 2")
	for _, want := range []string{"192.1", "fe80:"} {
		if !strings.Contains(row, want) {
			t.Errorf("%q is not on the row at 100 columns — its column was squeezed out: %s", want, row)
		}
	}
}

// Rule 116: eight columns must still fit an 80-column terminal — truncated is
// the acceptable outcome, overflowing is not.
func TestTheInterfacesLayoutFitsANarrowTerminal(t *testing.T) {
	m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: 80, Height: 24})
	m.activeTab = tabInterfaces
	m = feed(t, m, ifaceDataMsg{interfaces: []netiface.Interface{{
		Name: "Ethernet 2", State: netiface.StateUp, MTU: 1500,
		MAC:  "aa:bb:cc:dd:ee:ff",
		IPv4: []string{"192.168.1.21/24"},
		IPv6: []string{"fe80::1c2d:3e4f:5a6b:7c8d/64"},
	}}})

	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Errorf("line %d is %d cells wide, want at most 80:\n%s", i, got, line)
		}
	}
}
