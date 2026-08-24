package netdiag

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
		MAC: "aa:bb:cc:dd:ee:ff", Addresses: []string{"192.168.1.21/24"},
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
		Addresses: []string{"127.0.0.1/8"},
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
