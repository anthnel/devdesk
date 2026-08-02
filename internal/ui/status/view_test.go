package status

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Header and metadata ──────────────────────────────────────────────────────

func TestGetTitleShowsTheFormAsABreadcrumb(t *testing.T) {
	m := loadedModel(t)

	if got := m.GetTitle(); !strings.Contains(got, "Status Monitor") {
		t.Errorf("GetTitle() = %q, want it to name the view", got)
	}

	m = feed(t, m, testutil.Key("ctrl+n"))
	got := m.GetTitle()
	if !strings.Contains(got, "Status Monitor") || !strings.Contains(got, "Add New Monitor") {
		t.Errorf("GetTitle() = %q with the form open, want the view and the form title", got)
	}
}

func TestGetHeaderInfoReportsContextAndInterval(t *testing.T) {
	m := newTestModel(t)
	m.refreshInterval = 45 * time.Second

	info := m.GetHeaderInfo("work")

	if len(info) != 2 {
		t.Fatalf("GetHeaderInfo() returned %d entries, want 2", len(info))
	}
	if info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("first entry = %q/%q, want Context/work", info[0].Key, info[0].Value)
	}
	if info[1].Value != "45s" {
		t.Errorf("refresh value = %q, want \"45s\"", info[1].Value)
	}
}

func TestGetIconIsEmptyBecauseTheTitleCarriesIt(t *testing.T) {
	if got := newTestModel(t).GetIcon(); got != "" {
		t.Errorf("GetIcon() = %q, want empty", got)
	}
}

// Rule 130: shortcuts must reflect the current state, not a static list.
func TestGetShortcutsSwitchesWithTheForm(t *testing.T) {
	m := loadedModel(t)

	main := m.GetShortcuts()
	if len(main) < 5 {
		t.Fatalf("the main view exposes %d shortcuts, want the full set", len(main))
	}
	if !hasShortcut(main, "ctrl+n") || !hasShortcut(main, "ctrl+d") {
		t.Error("the main view does not expose the CRUD shortcuts")
	}

	m = feed(t, m, testutil.Key("ctrl+n"))
	form := m.GetShortcuts()
	if hasShortcut(form, "ctrl+n") {
		t.Error("the form state still offers ctrl+n, which does nothing there")
	}
	if !hasShortcut(form, "esc") || !hasShortcut(form, "enter") {
		t.Error("the form state does not offer cancel and submit")
	}
}

// Rule 137: descriptions are imperative and capitalised.
func TestShortcutDescriptionsAreCapitalisedImperatives(t *testing.T) {
	m := loadedModel(t)

	for _, s := range append(m.GetShortcuts(), feed(t, m, testutil.Key("ctrl+n")).GetShortcuts()...) {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return true
		}
	}
	return false
}

// ── View states ──────────────────────────────────────────────────────────────

func TestViewRendersTheFormWhenOpen(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+n"))

	out := m.View()

	if !strings.Contains(out, "Save Monitor") {
		t.Error("View() does not render the form while it is open")
	}
}

func TestViewRendersTheConfirmationWhenOpen(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	out := m.View()

	if !strings.Contains(out, "Delete Monitor") {
		t.Error("View() does not render the confirmation while it is open")
	}
	if !strings.Contains(out, "api") {
		t.Error("the confirmation does not name the monitor being deleted")
	}
}

func TestViewRendersTheError(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{Err: errors.New("checker exploded"), Timestamp: time.Now()})

	if out := m.View(); !strings.Contains(out, "checker exploded") {
		t.Error("View() does not surface the check error")
	}
}

func TestViewRendersTheEmptyStateOnlyAfterTheFirstCheck(t *testing.T) {
	m := newTestModel(t)

	// Before any result the view shows the table rather than claiming there is
	// nothing configured.
	if strings.Contains(m.View(), "No components configured") {
		t.Error("View() claimed there are no monitors before the first check completed")
	}

	m = feed(t, m, CheckCompleteMsg{Components: nil, Timestamp: time.Now()})
	if !strings.Contains(m.View(), "No components configured") {
		t.Error("View() does not show the empty state after a check returned nothing")
	}
}

func TestViewShowsTheSpinnerWhileTheFirstCheckRuns(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{Components: nil, Timestamp: time.Now()})
	m.checking = true

	if !strings.Contains(m.View(), "Checking components") {
		t.Error("View() does not report an in-flight check on an empty list")
	}
}

func TestViewRendersTheActiveTable(t *testing.T) {
	m := loadedModel(t)

	monitors := m.View()
	if !strings.Contains(monitors, "api.example.com") {
		t.Error("the monitor table is not rendered on the Monitors tab")
	}

	m = feed(t, m, testutil.Key("tab"))
	certificates := m.View()
	if !strings.Contains(certificates, "Let's Encrypt") {
		t.Error("the SSL table is not rendered on the Certificates tab")
	}
}

// ── Footer (Rule 124) ────────────────────────────────────────────────────────

func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"loaded", loadedModel},
		{"empty", func(t *testing.T) Model { return newTestModel(t) }},
		{"form open", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("ctrl+n")) }},
		{"searching", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("/")) }},
		{"error", func(t *testing.T) Model {
			return feed(t, newTestModel(t), CheckCompleteMsg{Err: errors.New("boom"), Timestamp: time.Now()})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)

			want := m.GetFooterHeight()
			got := strings.Count(m.RenderFooter(120), "\n") + 1
			if got != want {
				t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
			}
		})
	}
}

// The tab bar counts rows, so it has to be read after the tables are populated.
func TestFooterTabsCountRows(t *testing.T) {
	m := loadedModel(t)

	footer := m.RenderFooter(120)

	if !strings.Contains(footer, "Service Monitors (3)") {
		t.Errorf("footer does not report three monitors:\n%s", footer)
	}
	if !strings.Contains(footer, "SSL Certificates (1)") {
		t.Errorf("footer does not report one certificate:\n%s", footer)
	}
}

func TestFilterBarVisibilityFollowsTheViewState(t *testing.T) {
	m := loadedModel(t)
	if m.FilterBarVisible() {
		t.Error("the filter bar is visible before any search")
	}

	searching := feed(t, m, testutil.Key("/"))
	if !searching.FilterBarVisible() {
		t.Error("the filter bar is hidden while searching")
	}

	// An overlay covers the table, so its filter bar has nothing to filter.
	withForm := feed(t, m, testutil.Key("ctrl+n"))
	withForm.filterBar = searching.filterBar
	if withForm.FilterBarVisible() {
		t.Error("the filter bar stayed visible under the form")
	}

	empty := newTestModel(t)
	empty.filterBar = searching.filterBar
	if empty.FilterBarVisible() {
		t.Error("the filter bar is visible with nothing to filter")
	}
}

// While the search input holds focus every key belongs to it — including the
// ones that would otherwise open a form.
func TestSearchModeSwallowsViewShortcuts(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))

	m = feed(t, m, testutil.Key("ctrl+n"))

	if m.componentForm != nil {
		t.Error("ctrl+n opened the form while the search input had focus")
	}
}

// ── Table contents ───────────────────────────────────────────────────────────

func TestUpdateTableSplitsMonitorsFromCertificates(t *testing.T) {
	m := loadedModel(t)

	monitors := rowNames(m.monitorTable.Rows())
	if len(monitors) != 3 {
		t.Errorf("monitor table holds %v, want the three non-SSL entries", monitors)
	}
	for _, name := range monitors {
		if name == "cert" {
			t.Error("the SSL entry leaked into the monitor table")
		}
	}

	if certificates := rowNames(m.sslTable.Rows()); len(certificates) != 1 || certificates[0] != "cert" {
		t.Errorf("ssl table holds %v, want only cert", certificates)
	}
}

func TestUpdateTableFormatsMonitorCells(t *testing.T) {
	m := loadedModel(t)

	rows := m.monitorTable.Rows()
	byName := map[string][]string{}
	for _, row := range rows {
		byName[row[0]] = row
	}

	// Sorted by name: api, dns-primary, web.
	if got := byName["web"][4]; got != "120ms" {
		t.Errorf("web response cell = %q, want \"120ms\"", got)
	}
	if got := byName["api"][4]; got != "-" {
		t.Errorf("api response cell = %q, want \"-\" for an unmeasured check", got)
	}
	if got := byName["api"][3]; got != "http" {
		t.Errorf("api type cell = %q, want \"http\"", got)
	}
}

// Rule 122: cells are plain text, so a truncated ANSI sequence cannot bleed
// into the rows below.
func TestTableCellsCarryNoANSISequences(t *testing.T) {
	m := loadedModel(t)

	for label, rows := range map[string][][]string{
		"monitor": toStrings(m.monitorTable.Rows()),
		"ssl":     toStrings(m.sslTable.Rows()),
	} {
		for _, row := range rows {
			for i, cell := range row {
				if strings.Contains(cell, "\x1b[") {
					t.Errorf("%s table cell %d = %q contains an escape sequence", label, i, cell)
				}
			}
		}
	}
}

func TestUpdateTableFormatsCertificateCells(t *testing.T) {
	m := loadedModel(t)

	row := m.sslTable.Rows()[0]
	if row[3] != "42" {
		t.Errorf("days-left cell = %q, want \"42\"", row[3])
	}
	if row[4] != "2026-12-01 10:30" {
		t.Errorf("expiry cell = %q, want the formatted date", row[4])
	}
	if row[5] != "Let's Encrypt" {
		t.Errorf("issuer cell = %q, want the issuer", row[5])
	}
}

// Missing SSL details render as "-" rather than a zero date or a nil deref.
func TestCertificateCellsFallBackWhenDetailsAreMissing(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{
		Components: []status.ComponentStatus{
			{Name: "bare", Type: status.TypeSSL, Target: "bare.example.com", Status: status.StatusError},
		},
		Timestamp: time.Now(),
	})

	row := m.sslTable.Rows()[0]
	for i, cell := range map[int]string{3: "-", 4: "-", 5: "-"} {
		if row[i] != cell {
			t.Errorf("cell %d = %q, want %q for a certificate with no details", i, row[i], cell)
		}
	}
}

func TestUnknownComponentTypeRendersAsUnknown(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{
		Components: []status.ComponentStatus{
			{Name: "mystery", Target: "example.com", Status: status.StatusOK},
		},
		Timestamp: time.Now(),
	})

	if got := m.monitorTable.Rows()[0][3]; got != "unknown" {
		t.Errorf("type cell = %q for a component with no type, want \"unknown\"", got)
	}
}

// An unrecognised status must still render its own name rather than an empty
// cell, so an unexpected checker result stays visible.
func TestUnknownStatusFallsBackToItsName(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{
		Components: []status.ComponentStatus{
			{Name: "odd", Type: status.TypeHTTP, Target: "example.com", Status: status.StatusType("PENDING")},
			{Name: "odd-ssl", Type: status.TypeSSL, Target: "example.com", Status: status.StatusType("PENDING")},
		},
		Timestamp: time.Now(),
	})

	if got := m.monitorTable.Rows()[0][2]; got != "PENDING" {
		t.Errorf("status cell = %q, want the raw status name", got)
	}
	if got := m.sslTable.Rows()[0][2]; got != "PENDING" {
		t.Errorf("ssl status cell = %q, want the raw status name", got)
	}
}

func TestFormatSSLStatusCoversEveryState(t *testing.T) {
	seen := map[string]bool{}
	for _, state := range []status.StatusType{status.StatusOK, status.StatusWarning, status.StatusError, status.StatusDown} {
		got := formatSSLStatus(status.ComponentStatus{Status: state})
		if got == "" {
			t.Errorf("formatSSLStatus(%q) returned an empty cell", state)
		}
		seen[got] = true
	}
	// OK, WARNING and ERROR/DOWN must be distinguishable; ERROR and DOWN share
	// an icon deliberately.
	if len(seen) != 3 {
		t.Errorf("formatSSLStatus produced %d distinct icons, want 3", len(seen))
	}
}

// The header carries the sort indicator, so it has to track both the column and
// the direction.
func TestSortIndicatorFollowsTheActiveColumn(t *testing.T) {
	m := loadedModel(t)

	if got := m.monitorTable.Columns()[0].Title; got != "Name ▲" {
		t.Errorf("Name header = %q, want the ascending arrow", got)
	}

	m = feed(t, m, testutil.Key(".")) // name descending
	if got := m.monitorTable.Columns()[0].Title; got != "Name ▼" {
		t.Errorf("Name header = %q after reversing, want the descending arrow", got)
	}

	m = feed(t, m, testutil.Key(".")) // target ascending
	if got := m.monitorTable.Columns()[0].Title; got != "Name" {
		t.Errorf("Name header = %q once Target became the sort column, want it bare", got)
	}
	if got := m.monitorTable.Columns()[1].Title; got != "Target ▲" {
		t.Errorf("Target header = %q, want the ascending arrow", got)
	}
	// Status is not sortable and must never gain an arrow.
	if got := m.monitorTable.Columns()[2].Title; got != "Status" {
		t.Errorf("Status header = %q, want it bare", got)
	}
}

func toStrings(rows []table.Row) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := newTestModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 {
		t.Error("the help content lists no key bindings")
	}
	if len(content.Sections) == 0 {
		t.Error("the help content has no sections")
	}
}

// Every shortcut the header advertises should be explained in the help.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m := loadedModel(t)
	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		// Entries list aliases as "↑/k"; record the whole label too, so "/"
		// itself is not split into nothing.
		documented[kb.Key] = true
		for _, key := range strings.Split(kb.Key, "/") {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	for _, s := range m.GetShortcuts() {
		switch s.Key {
		case "±", "?": // rendered differently in the help ("+/-" and its own entry)
			continue
		}
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}
