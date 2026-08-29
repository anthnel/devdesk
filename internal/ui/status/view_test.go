package status

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Header and metadata ──────────────────────────────────────────────────────

func TestGetTitleShowsTheFormAsABreadcrumb(t *testing.T) {
	m := loadedModel(t)

	if got := m.GetTitle(); !strings.Contains(got, "Status Monitor") {
		t.Errorf("GetTitle() = %q, want it to name the view", got)
	}

	m = feed(t, m, testutil.Key(keymap.New))
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
	if !hasShortcut(main, keymap.New) || !hasShortcut(main, keymap.Delete) {
		t.Error("the main view does not expose the CRUD shortcuts")
	}

	m = feed(t, m, testutil.Key(keymap.New))
	form := m.GetShortcuts()
	if hasShortcut(form, keymap.New) {
		t.Error("the form state still offers ctrl+n, which does nothing there")
	}
	if !hasShortcut(form, "esc") || !hasShortcut(form, "enter") {
		t.Error("the form state does not offer cancel and submit")
	}
}

// Rule 137: descriptions are imperative and capitalised.
func TestShortcutDescriptionsAreCapitalisedImperatives(t *testing.T) {
	m := loadedModel(t)

	for _, s := range append(m.GetShortcuts(), feed(t, m, testutil.Key(keymap.New)).GetShortcuts()...) {
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
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))

	out := m.View()

	if !strings.Contains(out, "Save Monitor") {
		t.Error("View() does not render the form while it is open")
	}
}

func TestViewRendersTheConfirmationWhenOpen(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

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

// The check is reported in the footer, never in the body: the invitation to add
// a monitor would otherwise be replaced by it on every refresh.
func TestTheCheckIsReportedInTheFooterWhileItRuns(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{Components: nil, Timestamp: time.Now()})
	m.checking = true

	if !strings.Contains(m.RenderFooter(160), "Checking components") {
		t.Error("the footer does not report an in-flight check on an empty list")
	}
	if strings.Contains(m.View(), "Checking components") {
		t.Error("the body reports the check; it belongs in the footer alone")
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
		{"form open", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.New)) }},
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
	withForm := feed(t, m, testutil.Key(keymap.New))
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

	m = feed(t, m, testutil.Key(keymap.New))

	if m.componentForm != nil {
		t.Error("ctrl+n opened the form while the search input had focus")
	}
}

// ── Table contents ───────────────────────────────────────────────────────────

func TestUpdateTableSplitsMonitorsFromCertificates(t *testing.T) {
	m := loadedModel(t)

	monitors := rowNames(m.monitorTable.Table().Rows())
	if len(monitors) != 3 {
		t.Errorf("monitor table holds %v, want the three non-SSL entries", monitors)
	}
	for _, name := range monitors {
		if name == "cert" {
			t.Error("the SSL entry leaked into the monitor table")
		}
	}

	if certificates := rowNames(m.sslTable.Table().Rows()); len(certificates) != 1 || certificates[0] != "cert" {
		t.Errorf("ssl table holds %v, want only cert", certificates)
	}
}

func TestUpdateTableFormatsMonitorCells(t *testing.T) {
	m := loadedModel(t)

	rows := m.monitorTable.Table().Rows()
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
// into the rows below. The colour profile has to be forced — lipgloss detects
// no TTY under `go test`, falls back to the Ascii profile and strips every
// sequence, which would make this pass whatever the code does.
func TestTableCellsCarryNoANSISequences(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	m := loadedModel(t)

	for label, rows := range map[string][][]string{
		"monitor": toStrings(m.monitorTable.Table().Rows()),
		"ssl":     toStrings(m.sslTable.Table().Rows()),
	} {
		for _, row := range rows {
			for i, cell := range row {
				if strings.Contains(cell, "\x1b") {
					t.Errorf("%s table cell %d = %q contains an escape sequence", label, i, cell)
				}
			}
		}
	}
}

// sslCell reads a cell by the title of its column. Un indice écrit ici serait
// une seconde déclaration de l'ordre des colonnes, et c'est celle qui pourrit
// en silence : le réordonnancement de §3.54 aurait fait passer ces tests en
// comparant les mauvaises cellules si les noms n'y étaient pas.
func sslCell(t *testing.T, row []string, title string) string {
	t.Helper()
	for i, col := range sslColumns() {
		if col.Title == title {
			if i >= len(row) {
				t.Fatalf("column %q is declared at %d but the row has %d cells", title, i, len(row))
			}
			return row[i]
		}
	}
	t.Fatalf("no column titled %q on the Certificates tab", title)
	return ""
}

func TestUpdateTableFormatsCertificateCells(t *testing.T) {
	m := loadedModel(t)

	row := m.sslTable.Table().Rows()[0]
	for _, c := range []struct{ title, want string }{
		{"Days Left", "42"},
		{"Expires", "2026-12-01 10:30"},
		{"Issuer", "Let's Encrypt"},
	} {
		if got := sslCell(t, row, c.title); got != c.want {
			t.Errorf("%s cell = %q, want %q", c.title, got, c.want)
		}
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

	row := m.sslTable.Table().Rows()[0]
	for _, title := range []string{"Issuer", "Days Left", "Expires"} {
		if got := sslCell(t, row, title); got != "-" {
			t.Errorf("%s cell = %q, want %q for a certificate with no details", title, got, "-")
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

	if got := m.monitorTable.Table().Rows()[0][3]; got != "unknown" {
		t.Errorf("type cell = %q for a component with no type, want \"unknown\"", got)
	}
}

// An unrecognised status must still render its own name rather than an empty
// cell, so an unexpected checker result stays visible.
//
// **Le certificat n'en est plus : sa cellule ne lit plus StatusType du tout.**
// Elle lit `SSLDaysLeft`, dont l'absence veut dire « rien n'a pu être lu » quel
// que soit le statut porté à côté — et c'est ce qu'elle doit dire ici, plutôt
// que de recopier un mot que la colonne d'à côté n'explique pas.
func TestUnknownStatusFallsBackToItsName(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, CheckCompleteMsg{
		Components: []status.ComponentStatus{
			{Name: "odd", Type: status.TypeHTTP, Target: "example.com", Status: status.StatusType("PENDING")},
			{Name: "odd-ssl", Type: status.TypeSSL, Target: "example.com", Status: status.StatusType("PENDING")},
		},
		Timestamp: time.Now(),
	})

	if got := m.monitorTable.Table().Rows()[0][2]; got != "PENDING" {
		t.Errorf("status cell = %q, want the raw status name", got)
	}
	if got, want := sslCell(t, m.sslTable.Table().Rows()[0], "Status"), theme.CertStateIcon(string(status.CertError)); got != want {
		t.Errorf("ssl status cell = %q, want the unread glyph %q", got, want)
	}
}

// La colonne sépare désormais les quatre états d'un certificat, et c'est le
// point : `ERROR` recouvrait aussi bien un certificat périmé qu'un qui expire
// dans six jours, donc l'icône était la même pour les deux (D64).
func TestFormatSSLStatusCoversEveryState(t *testing.T) {
	days := func(n int) *int { return &n }
	cases := []struct {
		name string
		comp status.ComponentStatus
		want status.CertState
	}{
		{"valide", status.ComponentStatus{Status: status.StatusOK, SSLDaysLeft: days(200)}, status.CertValid},
		{"à renouveler", status.ComponentStatus{Status: status.StatusError, SSLDaysLeft: days(6)}, status.CertToRenew},
		{"périmé", status.ComponentStatus{Status: status.StatusError, SSLDaysLeft: days(-1)}, status.CertExpired},
		{"illisible", status.ComponentStatus{Status: status.StatusDown}, status.CertError},
	}

	seen := map[string]bool{}
	for _, c := range cases {
		got := formatSSLStatus(c.comp)
		if got == "" {
			t.Errorf("formatSSLStatus(%s) returned an empty cell", c.name)
		}
		if want := theme.CertStateIcon(string(c.want)); got != want {
			t.Errorf("formatSSLStatus(%s) = %q, want the %q glyph %q", c.name, got, c.want, want)
		}
		seen[got] = true
	}
	if len(seen) != len(cases) {
		t.Errorf("formatSSLStatus produced %d distinct icons for %d states", len(seen), len(cases))
	}

	// Rule 122 : la cellule est mesurée, donc elle ne porte pas sa couleur —
	// c'est certStatusStyle qui la donne, et elle doit séparer ce que l'icône
	// vient de séparer.
	if certStatusStyle(cases[1].comp).GetForeground() == certStatusStyle(cases[2].comp).GetForeground() {
		t.Error("a certificate to renew and an expired one read the same colour")
	}
}

// The header carries the sort indicator, so it has to track both the column and
// the direction.
func TestSortIndicatorFollowsTheActiveColumn(t *testing.T) {
	m := loadedModel(t)

	if got := m.monitorTable.Table().Columns()[0].Title; got != "Name ▲" {
		t.Errorf("Name header = %q, want the ascending arrow", got)
	}

	m = feed(t, m, testutil.Key(".")) // name descending
	if got := m.monitorTable.Table().Columns()[0].Title; got != "Name ▼" {
		t.Errorf("Name header = %q after reversing, want the descending arrow", got)
	}

	m = feed(t, m, testutil.Key(".")) // target ascending
	if got := m.monitorTable.Table().Columns()[0].Title; got != "Name" {
		t.Errorf("Name header = %q once Target became the sort column, want it bare", got)
	}
	if got := m.monitorTable.Table().Columns()[1].Title; got != "Target ▲" {
		t.Errorf("Target header = %q, want the ascending arrow", got)
	}
	// Status is not sortable and must never gain an arrow.
	if got := m.monitorTable.Table().Columns()[2].Title; got != "Status" {
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
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 116, on both tables and not only the visible one: switching tabs must
// not have to wait for a resize to get its widths right. The eleven ratios this
// replaced rounded down independently and handed the drift to the last column.
func TestBothTablesFitTheWidth(t *testing.T) {
	for _, width := range []int{60, 90, 120, 200} {
		m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: width, Height: 30})

		tables := []struct {
			name    string
			columns []table.Column
			span    int
		}{
			{"monitors", m.monitorTable.Table().Columns(), m.monitorTable.RenderedWidth()},
			{"certificates", m.sslTable.Table().Columns(), m.sslTable.RenderedWidth()},
		}
		for _, tc := range tables {
			for _, col := range tc.columns {
				if col.Width < 0 {
					t.Errorf("%s at width %d: column %q is %d wide", tc.name, width, col.Title, col.Width)
				}
			}
			// RenderedWidth rather than the declared columns plus two cells
			// each: a column dropped for want of room renders nothing and hands
			// its padding back, so that arithmetic asks for less than the line
			// spans (D61).
			if want := width - 2; tc.span != want {
				t.Errorf("%s at width %d: the line spans %d, want %d", tc.name, width, tc.span, want)
			}
		}
	}
}
