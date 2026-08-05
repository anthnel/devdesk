package status

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/status/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewReadsRefreshSettingsFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Status.RefreshInterval = 45
	cfg.Status.AutoRefresh = false

	m := New(cfg)

	if m.refreshInterval != 45*time.Second {
		t.Errorf("refreshInterval = %v, want 45s", m.refreshInterval)
	}
	if !m.paused {
		t.Error("paused = false with AutoRefresh disabled; the view would refresh against the user's setting")
	}
	if !m.firstCheck {
		t.Error("firstCheck = false on a new model")
	}
	if m.activeTab != TabMonitors {
		t.Errorf("activeTab = %d on a new model, want %d (Monitors)", m.activeTab, TabMonitors)
	}
	if column, desc := m.monitorTable.SortState(); column != columnName || desc {
		t.Errorf("sort = (column %d, desc=%v) on a new model, want name ascending", column, desc)
	}
}

func TestNewAutoRefreshEnabledStartsUnpaused(t *testing.T) {
	cfg := config.Default()
	cfg.Status.AutoRefresh = true

	if New(cfg).paused {
		t.Error("paused = true with AutoRefresh enabled")
	}
}

// Init must schedule the first check itself: nothing else in the view triggers
// one before the first tick elapses.
func TestInitBatchesSpinnerTickAndCheck(t *testing.T) {
	if cmd := New(testConfig()).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so no initial check is ever run")
	}
}

// ── Check lifecycle ──────────────────────────────────────────────────────────

func TestCheckCompleteStoresResultsAndSchedulesTheNext(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...)
	m.checking = true
	timestamp := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	m = feed(t, m, CheckCompleteMsg{Components: checkResults(), Timestamp: timestamp})

	if m.checking {
		t.Error("checking = true after the results arrived")
	}
	if m.firstCheck {
		t.Error("firstCheck = true after the first results arrived")
	}
	if !m.lastCheck.Equal(timestamp) {
		t.Errorf("lastCheck = %v, want %v", m.lastCheck, timestamp)
	}
	if want := timestamp.Add(m.refreshInterval); !m.nextCheck.Equal(want) {
		t.Errorf("nextCheck = %v, want %v (lastCheck + interval)", m.nextCheck, want)
	}
	if len(m.components) != len(checkResults()) {
		t.Errorf("stored %d components, want %d", len(m.components), len(checkResults()))
	}
	if m.error != "" {
		t.Errorf("error = %q after a successful check", m.error)
	}
}

// A failed check must not wipe the last known good results — a blank table is a
// worse answer than a stale one plus an error.
func TestCheckCompleteWithErrorKeepsPreviousResults(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, CheckCompleteMsg{Err: errors.New("checker exploded"), Timestamp: time.Now()})

	if m.error != "checker exploded" {
		t.Errorf("error = %q, want the checker's message", m.error)
	}
	if len(m.components) != len(checkResults()) {
		t.Errorf("stored %d components after a failed check, want the previous %d", len(m.components), len(checkResults()))
	}
	if m.checking {
		t.Error("checking = true after a failed check, so the spinner never stops")
	}
}

func TestCheckCompleteClearsAPreviousError(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...)
	m = feed(t, m, CheckCompleteMsg{Err: errors.New("boom"), Timestamp: time.Now()})

	m = feed(t, m, CheckCompleteMsg{Components: checkResults(), Timestamp: time.Now()})

	if m.error != "" {
		t.Errorf("error = %q after a successful check followed a failure", m.error)
	}
}

// ── Ticks ────────────────────────────────────────────────────────────────────

func TestTickStartsACheckOnlyWhenDue(t *testing.T) {
	tests := []struct {
		name         string
		paused       bool
		checking     bool
		nextCheck    time.Time
		wantChecking bool
	}{
		{"due and idle", false, false, time.Now().Add(-time.Second), true},
		{"not due yet", false, false, time.Now().Add(time.Hour), false},
		{"paused", true, false, time.Now().Add(-time.Second), false},
		{"already checking", false, true, time.Now().Add(-time.Second), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			m.paused = tc.paused
			m.checking = tc.checking
			m.nextCheck = tc.nextCheck

			m, cmd := step(t, m, TickMsg(time.Now()))

			if m.checking != tc.wantChecking {
				t.Errorf("checking = %v, want %v", m.checking, tc.wantChecking)
			}
			if cmd == nil {
				t.Error("the tick loop stopped: no command returned")
			}
		})
	}
}

// The spinner only animates while a check is in flight; ticking it when idle
// would keep the render loop busy for nothing.
func TestSpinnerTickIsIgnoredWhenIdle(t *testing.T) {
	m := newTestModel(t)

	_, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		if _, batched := cmd().(tea.BatchMsg); !batched && cmd() != nil {
			t.Errorf("an idle model answered a spinner tick with %T", cmd())
		}
	}

	m.checking = true
	_, cmd = step(t, m, spinner.TickMsg{})
	if cmd == nil {
		t.Error("a checking model did not keep the spinner running")
	}
}

// ── Refresh controls ─────────────────────────────────────────────────────────

func TestCtrlRStartsACheckUnlessOneIsRunning(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	if !m.checking {
		t.Error("ctrl+r did not start a check")
	}
	if cmd == nil {
		t.Error("ctrl+r returned no command, so no check was issued")
	}

	m, cmd = step(t, m, testutil.Key("ctrl+r"))
	if cmd != nil {
		t.Error("ctrl+r issued a second check while one was already running")
	}
}

func TestSpaceTogglesPause(t *testing.T) {
	m := loadedModel(t)
	m.paused = true
	m.lastCheck = time.Now() // too recent to trigger an immediate check

	m = feed(t, m, testutil.Key(" "))
	if m.paused {
		t.Error("space did not resume")
	}
	if m.checking {
		t.Error("resuming started a check even though the last one was recent")
	}

	m = feed(t, m, testutil.Key(" "))
	if !m.paused {
		t.Error("space did not pause")
	}
}

// Resuming after the interval has already elapsed should check immediately
// rather than leave the table stale until the next tick.
func TestResumingAfterTheIntervalChecksImmediately(t *testing.T) {
	m := loadedModel(t)
	m.paused = true
	m.lastCheck = time.Now().Add(-2 * m.refreshInterval)

	m, cmd := step(t, m, testutil.Key(" "))

	if !m.checking {
		t.Error("resuming with a stale last check did not start one")
	}
	if cmd == nil {
		t.Error("resuming returned no command")
	}
}

func TestIntervalAdjustmentClamps(t *testing.T) {
	t.Run("plus adds five seconds and stops at five minutes", func(t *testing.T) {
		m := newTestModel(t)
		m.refreshInterval = 30 * time.Second

		m = feed(t, m, testutil.Key("+"))
		if m.refreshInterval != 35*time.Second {
			t.Errorf("refreshInterval = %v after +, want 35s", m.refreshInterval)
		}

		m.refreshInterval = 299 * time.Second
		m = feed(t, m, testutil.Key("+"), testutil.Key("+"))
		if m.refreshInterval != 300*time.Second {
			t.Errorf("refreshInterval = %v, want it clamped at 300s", m.refreshInterval)
		}
	})

	t.Run("minus subtracts five seconds and stops at five", func(t *testing.T) {
		m := newTestModel(t)
		m.refreshInterval = 30 * time.Second

		m = feed(t, m, testutil.Key("-"))
		if m.refreshInterval != 25*time.Second {
			t.Errorf("refreshInterval = %v after -, want 25s", m.refreshInterval)
		}

		m.refreshInterval = 6 * time.Second
		m = feed(t, m, testutil.Key("-"), testutil.Key("-"))
		if m.refreshInterval != 5*time.Second {
			t.Errorf("refreshInterval = %v, want it clamped at 5s", m.refreshInterval)
		}
	})
}

// ── Navigation ───────────────────────────────────────────────────────────────

func TestTabSwitchesBetweenMonitorsAndCertificates(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != TabCertificates {
		t.Errorf("activeTab = %d after tab, want %d (Certificates)", m.activeTab, TabCertificates)
	}
	if !m.sslTable.Table().Focused() || m.monitorTable.Table().Focused() {
		t.Error("focus did not follow the active tab")
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != TabMonitors {
		t.Errorf("activeTab = %d after a second tab, want %d (Monitors)", m.activeTab, TabMonitors)
	}
	if !m.monitorTable.Table().Focused() || m.sslTable.Table().Focused() {
		t.Error("focus did not return to the monitor table")
	}
}

func TestVerticalNavigationMovesTheActiveTableOnly(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("down"))
	if m.monitorTable.Cursor() != 1 {
		t.Errorf("monitor cursor = %d after down, want 1", m.monitorTable.Cursor())
	}
	if m.sslTable.Cursor() != 0 {
		t.Errorf("ssl cursor = %d, want it untouched while the monitor tab is active", m.sslTable.Cursor())
	}

	m = feed(t, m, testutil.Key("up"))
	if m.monitorTable.Cursor() != 0 {
		t.Errorf("monitor cursor = %d after up, want 0", m.monitorTable.Cursor())
	}
}

func TestGotoTopAndBottom(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("G"))
	last := len(m.monitorTable.Table().Rows()) - 1
	if m.monitorTable.Cursor() != last {
		t.Errorf("cursor = %d after G, want %d (last row)", m.monitorTable.Cursor(), last)
	}

	m = feed(t, m, testutil.Key("g"))
	if m.monitorTable.Cursor() != 0 {
		t.Errorf("cursor = %d after g, want 0", m.monitorTable.Cursor())
	}
}

func TestVimNavigationMatchesArrows(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("j"))
	if m.monitorTable.Cursor() != 1 {
		t.Errorf("cursor = %d after j, want 1", m.monitorTable.Cursor())
	}
	m = feed(t, m, testutil.Key("k"))
	if m.monitorTable.Cursor() != 0 {
		t.Errorf("cursor = %d after k, want 0", m.monitorTable.Cursor())
	}
}

// ── Sorting ──────────────────────────────────────────────────────────────────

// '.' cycles direction first, then column: each column is visited ascending and
// descending before moving on.
func TestCycleSortWalksDirectionThenColumn(t *testing.T) {
	m := loadedModel(t)

	// Name(0), Target(1), Type(3) and Response(4) sort; Status(2) does not.
	steps := []struct {
		wantColumn int
		wantDesc   bool
	}{
		{0, true},
		{1, false},
		{1, true},
		{3, false},
		{3, true},
		{4, false},
		{4, true},
		{0, false}, // wraps
	}

	for i, want := range steps {
		m = feed(t, m, testutil.Key("."))
		column, desc := m.monitorTable.SortState()
		if column != want.wantColumn || desc != want.wantDesc {
			t.Fatalf("step %d: sort = (column %d, desc=%v), want (column %d, desc=%v)", i+1, column, desc, want.wantColumn, want.wantDesc)
		}
	}
}

// The certificates tab declares no comparators, so `.` is inert there rather
// than silently reordering an expiry list nobody asked to reorder.
func TestTheCertificatesTabDoesNotSort(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("."))

	if column, desc := m.sslTable.SortState(); column != -1 || desc {
		t.Errorf("ssl sort = (column %d, desc=%v) after '.', want it untouched", column, desc)
	}
}

func TestEachColumnOrdersByItsOwnValue(t *testing.T) {
	monitors := []status.ComponentStatus{
		{Name: "web", Target: "z.example.com", Type: status.TypeHTTPS, ResponseTime: 300 * time.Millisecond},
		{Name: "api", Target: "a.example.com", Type: status.TypeHTTP, ResponseTime: 100 * time.Millisecond},
		{Name: "dns", Target: "m.example.com", Type: status.TypeDNS, ResponseTime: 200 * time.Millisecond},
	}

	tests := []struct {
		name   string
		column int
		desc   bool
		want   []string
	}{
		{"name ascending", 0, false, []string{"api", "dns", "web"}},
		{"name descending", 0, true, []string{"web", "dns", "api"}},
		{"target ascending", 1, false, []string{"api", "dns", "web"}},
		{"type ascending", 3, false, []string{"dns", "api", "web"}},
		{"response ascending", 4, false, []string{"api", "dns", "web"}},
		{"response descending", 4, true, []string{"web", "dns", "api"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dt := datatable.New(datatable.Config[status.ComponentStatus]{
				Columns:    monitorColumns(),
				SortColumn: tc.column,
			})
			if tc.desc {
				dt.CycleSort() // ascending → descending, same column
			}
			dt.SetItems(monitors)

			got := namesOf(dt.Visible())
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("position %d = %q, want %q (full order %v)", i, got[i], want, got)
				}
			}
		})
	}
}

// Sorting reorders what is shown, never the list handed in: m.components is the
// source of truth for the config lookup.
func TestSortingLeavesTheSourceListAlone(t *testing.T) {
	monitors := []status.ComponentStatus{
		{Name: "web"}, {Name: "api"}, {Name: "dns"},
	}
	dt := datatable.New(datatable.Config[status.ComponentStatus]{
		Columns:    monitorColumns(),
		SortColumn: columnName,
	})

	dt.SetItems(monitors)

	if monitors[0].Name != "web" || monitors[1].Name != "api" || monitors[2].Name != "dns" {
		t.Errorf("the sort reordered the caller's slice: %v", namesOf(monitors))
	}
	if got := namesOf(dt.Visible()); got[0] != "api" {
		t.Errorf("Visible() = %v, want it sorted", got)
	}
}

func namesOf(components []status.ComponentStatus) []string {
	names := make([]string, 0, len(components))
	for _, c := range components {
		names = append(names, c.Name)
	}
	return names
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

func TestCtrlNOpensAnEmptyForm(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("ctrl+n"))

	if m.componentForm == nil {
		t.Fatal("ctrl+n did not open the component form")
	}
	if got := m.componentForm.GetTitle(); got != "Add New Monitor" {
		t.Errorf("form title = %q, want the creation title", got)
	}
}

func TestEditOpensTheFormOnTheSelectedMonitor(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("down")) // second row, sorted by name ascending

	m = feed(t, m, testutil.Key("e"))

	if m.componentForm == nil {
		t.Fatal("e did not open the component form")
	}
	if got := m.componentForm.GetTitle(); got != "Edit Monitor" {
		t.Errorf("form title = %q, want the edit title", got)
	}
	// Sorted by name: api, dns-primary, web. Row 1 is dns-primary.
	if got := m.config.Status.Components[m.selectedIdx].Name; got != "dns-primary" {
		t.Errorf("editing %q, want the monitor under the cursor (dns-primary)", got)
	}
}

func TestEditAndDeleteAreInertWithoutData(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...) // no check results yet

	m = feed(t, m, testutil.Key("e"))
	if m.componentForm != nil {
		t.Error("e opened a form with no monitors loaded")
	}

	m = feed(t, m, testutil.Key("ctrl+d"))
	if m.confirmModal != nil {
		t.Error("ctrl+d opened a confirmation with no monitors loaded")
	}
}

func TestCtrlDOpensAConfirmationNamingTheMonitor(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("ctrl+d"))

	if m.confirmModal == nil {
		t.Fatal("ctrl+d did not open the confirmation modal")
	}
	// Sorted by name, the first row is api.
	if got := m.config.Status.Components[m.selectedIdx].Name; got != "api" {
		t.Errorf("selected %q for deletion, want the monitor under the cursor (api)", got)
	}
}

func TestSelectedIndexResolvesThroughTheSortOrder(t *testing.T) {
	m := loadedModel(t)

	// Config order is web, api, dns-primary, cert; sorted display order is
	// api, dns-primary, web. Cursor 2 must resolve to web's config index (0).
	m.monitorTable.SetCursor(2)
	if got := m.getSelectedComponentIndex(); got != 0 {
		t.Errorf("getSelectedComponentIndex() = %d for the third sorted row, want 0 (web)", got)
	}

	m = feed(t, m, testutil.Key(".")) // name descending: web is first
	m.monitorTable.SetCursor(0)
	if got := m.getSelectedComponentIndex(); got != 0 {
		t.Errorf("getSelectedComponentIndex() = %d for the first descending row, want 0 (web)", got)
	}
}

func TestSelectedIndexOnTheCertificateTab(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("tab"))

	// The single SSL entry sits at config index 3.
	if got := m.getSelectedComponentIndex(); got != 3 {
		t.Errorf("getSelectedComponentIndex() = %d on the SSL tab, want 3 (cert)", got)
	}
}

// The table and m.components can disagree — the rows are rendered from an
// earlier check — so the cursor may point past the current results.
func TestSelectedIndexOutOfRangeReturnsMinusOne(t *testing.T) {
	m := loadedModel(t)
	m.monitorTable.SetCursor(2)
	// The rows shrink to one and the cursor is clamped onto it, so the lookup
	// answers for the row on screen rather than for an index nothing holds.
	m = feed(t, m, CheckCompleteMsg{Components: checkResults()[:1], Timestamp: time.Now()})

	if got := m.getSelectedComponentIndex(); got != 0 {
		t.Errorf("getSelectedComponentIndex() = %d after the results shrank, want 0 (the only row left)", got)
	}
}

// A monitor that is checked but absent from the config has no index to return;
// silently answering 0 would edit or delete an unrelated entry.
func TestSelectedIndexReturnsMinusOneWhenTheConfigHasNoMatch(t *testing.T) {
	m := loadedModel(t)
	m.config.Status.Components = []config.ComponentConfig{
		{Name: "unrelated", Type: "https", Target: "other.example.com"},
	}

	if got := m.getSelectedComponentIndex(); got != -1 {
		t.Errorf("getSelectedComponentIndex() = %d with no matching config entry, want -1", got)
	}
}

func TestFormSubmitAppendsANewMonitor(t *testing.T) {
	m := loadedModel(t)
	before := len(m.config.Status.Components)

	m, cmd := step(t, m, components.ComponentFormSubmitMsg{
		Component: config.ComponentConfig{Name: "new", Type: "https", Target: "new.example.com"},
	})

	if m.componentForm != nil {
		t.Error("the form stayed open after submission")
	}
	if len(m.config.Status.Components) != before+1 {
		t.Fatalf("config holds %d components after a creation, want %d", len(m.config.Status.Components), before+1)
	}
	if got := m.config.Status.Components[before].Name; got != "new" {
		t.Errorf("appended %q, want \"new\"", got)
	}
	if cmd == nil {
		t.Error("submission returned no command, so the config is never written")
	}
}

func TestFormSubmitReplacesTheEditedMonitor(t *testing.T) {
	m := loadedModel(t)
	original := m.config.Status.Components[1] // api
	before := len(m.config.Status.Components)

	m, cmd := step(t, m, components.ComponentFormSubmitMsg{
		Component: config.ComponentConfig{Name: "api-renamed", Type: "http", Target: "api.example.com"},
		Original:  &original,
	})

	if len(m.config.Status.Components) != before {
		t.Errorf("config holds %d components after an edit, want %d", len(m.config.Status.Components), before)
	}
	if got := m.config.Status.Components[1].Name; got != "api-renamed" {
		t.Errorf("component 1 = %q after the edit, want \"api-renamed\"", got)
	}
	if cmd == nil {
		t.Error("the edit returned no command, so the config is never written")
	}
}

// An Original that no longer matches anything must not silently append a
// duplicate: the entry it referred to is gone.
func TestFormSubmitWithAnUnknownOriginalChangesNothing(t *testing.T) {
	m := loadedModel(t)
	before := len(m.config.Status.Components)
	stale := config.ComponentConfig{Name: "ghost", Type: "https", Target: "gone.example.com"}

	m, _ = step(t, m, components.ComponentFormSubmitMsg{
		Component: config.ComponentConfig{Name: "whatever", Type: "https", Target: "x"},
		Original:  &stale,
	})

	if len(m.config.Status.Components) != before {
		t.Errorf("config holds %d components, want %d unchanged", len(m.config.Status.Components), before)
	}
	for _, c := range m.config.Status.Components {
		if c.Name == "whatever" {
			t.Error("a stale edit was appended as a new monitor")
		}
	}
}

func TestConfirmDeleteRemovesTheSelectedMonitor(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("ctrl+d")) // selects api (config index 1)
	before := len(m.config.Status.Components)

	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

	if m.confirmModal != nil {
		t.Error("the confirmation stayed open after answering yes")
	}
	if len(m.config.Status.Components) != before-1 {
		t.Fatalf("config holds %d components after a delete, want %d", len(m.config.Status.Components), before-1)
	}
	for _, c := range m.config.Status.Components {
		if c.Name == "api" {
			t.Error("the deleted monitor is still in the config")
		}
	}
	if cmd == nil {
		t.Error("the delete returned no command, so the config is never written")
	}
}

func TestCancellingDeleteKeepsEverything(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("ctrl+d"))
	before := len(m.config.Status.Components)

	m = feed(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if m.confirmModal != nil {
		t.Error("the confirmation stayed open after answering no")
	}
	if len(m.config.Status.Components) != before {
		t.Errorf("config holds %d components after cancelling, want %d", len(m.config.Status.Components), before)
	}
}

func TestConfirmDeleteWithAStaleIndexIsInert(t *testing.T) {
	m := loadedModel(t)
	m.selectedIdx = 99
	before := len(m.config.Status.Components)

	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

	if len(m.config.Status.Components) != before {
		t.Errorf("config holds %d components, want %d unchanged", len(m.config.Status.Components), before)
	}
	if cmd != nil {
		t.Error("a delete was issued for an out-of-range index")
	}
}

// ── Save and delete results ──────────────────────────────────────────────────

func TestSaveAndDeleteErrorsSurfaceInTheView(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"save failed", ComponentSavedMsg{Error: errors.New("disk full")}},
		{"delete failed", ComponentDeletedMsg{Error: errors.New("disk full")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)

			m, cmd := step(t, m, tc.msg)

			if m.error != "disk full" {
				t.Errorf("error = %q, want the write failure surfaced", m.error)
			}
			if cmd != nil {
				t.Error("a failed write still triggered a reload")
			}
			if m.checking {
				t.Error("a failed write started a check")
			}
		})
	}
}

// A successful write reloads the config from disk, so the model picks up
// whatever was actually persisted rather than trusting its in-memory copy.
func TestSuccessfulSaveReloadsAndChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := loadedModel(t)
	m.config.Status.Components = append(m.config.Status.Components, config.ComponentConfig{
		Name: "persisted", Type: "https", Target: "persisted.example.com",
	})
	if err := config.Save(m.config); err != nil {
		t.Fatalf("seeding the config file: %v", err)
	}
	m.config.Status.Components = nil // prove the reload is what repopulates it

	m, cmd := step(t, m, ComponentSavedMsg{Success: true})

	if cmd == nil {
		t.Error("a successful save did not start a fresh check")
	}
	if !m.checking {
		t.Error("checking = false after a successful save")
	}
	var found bool
	for _, c := range m.config.Status.Components {
		if c.Name == "persisted" {
			found = true
		}
	}
	if !found {
		t.Errorf("the reload did not pick up the persisted monitor; got %d components", len(m.config.Status.Components))
	}
}

func TestFailedReloadSurfacesTheError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// A missing file falls back to defaults, so the failure has to come from a
	// file that exists and cannot be parsed.
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("app: [not-a-mapping"), 0o600); err != nil {
		t.Fatalf("writing the broken config: %v", err)
	}

	m := loadedModel(t)

	m, cmd := step(t, m, ComponentDeletedMsg{Success: true})

	if m.error == "" {
		t.Error("a failed reload left the error empty")
	}
	if cmd != nil {
		t.Error("a failed reload still started a check")
	}
}

// ── Filter bar (Rule 136) ────────────────────────────────────────────────────

func TestSlashActivatesSearchAndCapturesKeys(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("/"))
	if cmd == nil {
		t.Error("/ returned no command, so the input never takes focus")
	}
	if !m.filterBar.InEditMode() {
		t.Fatal("/ did not put the filter bar in edit mode")
	}
	if !m.InEditMode() {
		t.Error("InEditMode() is false while the filter bar has focus; the router would steal ':'")
	}

	// While searching, ordinary keys must reach the input rather than the view.
	before := m.activeTab
	m = feed(t, m, testutil.Type("api")...)
	if m.activeTab != before {
		t.Error("a keystroke leaked to the view while the filter bar was focused")
	}
	if m.filterBar.SearchQuery() != "api" {
		t.Errorf("search query = %q, want \"api\"", m.filterBar.SearchQuery())
	}
}

func TestSearchFiltersBothTables(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("api")...)

	if got := rowNames(m.monitorTable.Table().Rows()); len(got) != 1 || got[0] != "api" {
		t.Errorf("monitor rows = %v, want only api", got)
	}
	if got := rowNames(m.sslTable.Table().Rows()); len(got) != 0 {
		t.Errorf("ssl rows = %v, want none to match \"api\"", got)
	}
}

func TestSearchMatchesTargetAndType(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"api.example", []string{"api"}},                       // target
		{"dns", []string{"dns-primary"}},                       // type and name
		{"example.com", []string{"api", "dns-primary", "web"}}, // shared target substring
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			m := loadedModel(t)
			m = feed(t, m, testutil.Key("/"))
			m = feed(t, m, testutil.Type(tc.query)...)

			got := rowNames(m.monitorTable.Table().Rows())
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

// ── Key routing ──────────────────────────────────────────────────────────────

// The form and the modal own the keyboard while they are open, otherwise typing
// a name would also drive the table underneath.
func TestOpenOverlaysCaptureKeys(t *testing.T) {
	t.Run("form", func(t *testing.T) {
		m := loadedModel(t)
		m = feed(t, m, testutil.Key("ctrl+n"))
		cursor := m.monitorTable.Cursor()

		m = feed(t, m, testutil.Key("down"))

		if m.monitorTable.Cursor() != cursor {
			t.Error("a keystroke reached the table while the form was open")
		}
		if m.componentForm == nil {
			t.Fatal("the form closed on a navigation key")
		}
	})

	t.Run("confirmation", func(t *testing.T) {
		m := loadedModel(t)
		m = feed(t, m, testutil.Key("ctrl+d"))
		cursor := m.monitorTable.Cursor()

		m = feed(t, m, testutil.Key("down"))

		if m.monitorTable.Cursor() != cursor {
			t.Error("a keystroke reached the table while the confirmation was open")
		}
	})
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m := loadedModel(t)

			_, cmd := step(t, m, testutil.Key(key))
			if cmd == nil {
				t.Fatalf("%q returned no command, want tea.Quit", key)
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("%q produced %T, want tea.QuitMsg", key, cmd())
			}
		})
	}
}

func TestUnhandledKeysAreInert(t *testing.T) {
	m := loadedModel(t)
	before := m

	m, cmd := step(t, m, testutil.Key("z"))

	if cmd != nil {
		t.Errorf("an unbound key produced %T", testutil.Msg(cmd))
	}
	if m.activeTab != before.activeTab || m.paused != before.paused || m.componentForm != nil {
		t.Error("an unbound key changed the view state")
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

func TestResizeFillsTheViewportWidth(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Rule 116: content width is the terminal minus the viewport borders, and
	// each cell adds two columns of padding.
	assertColumnsFill(t, "monitor", m.monitorTable.Table().Columns(), 120-2-5*2)
	assertColumnsFill(t, "ssl", m.sslTable.Table().Columns(), 120-2-6*2)
}

func assertColumnsFill(t *testing.T, label string, columns []table.Column, available int) {
	t.Helper()
	total := 0
	for _, col := range columns {
		total += col.Width
	}
	if total != available {
		t.Errorf("%s columns total %d, want %d so the selected row reaches the border", label, total, available)
	}
}

// A terminal briefly reports a zero height while resizing. resize() clamps its
// own computation to 1; without that clamp bubbles is handed a negative height
// and reports one back.
func TestResizeNeverHandsTheTableANegativeHeight(t *testing.T) {
	m := newTestModel(t)

	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 0})

	if m.monitorTable.Table().Height() < 0 {
		t.Errorf("monitor table height = %d on a zero-height terminal, want it non-negative", m.monitorTable.Table().Height())
	}
	if m.sslTable.Table().Height() < 0 {
		t.Errorf("ssl table height = %d on a zero-height terminal, want it non-negative", m.sslTable.Table().Height())
	}
}

// ── D25: the cursor and the rows disagree under a filter ─────────────────────

// The monitor rows are sorted and filtered; getSelectedComponentIndex sorts and
// does not filter. Under a filter the cursor into the rows becomes an index
// into a longer list, so `e` edits and `ctrl+d` deletes a monitor the user is
// not looking at. Same family as D24 in workspaces.
func TestAFilteredSelectionEditsTheRowTheUserSees(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("dns")...)
	m = feed(t, m, testutil.Key("enter")) // confirm the filter, hand the keys back

	if got := rowNames(m.monitorTable.Table().Rows()); len(got) != 1 || got[0] != "dns-primary" {
		t.Fatalf("rows under the filter = %v, want just dns-primary", got)
	}

	m = feed(t, m, testutil.Key("ctrl+d"))
	if m.confirmModal == nil {
		t.Fatal("ctrl+d did not open the delete confirmation")
	}
	if !strings.Contains(m.confirmModal.View(), "dns-primary") {
		t.Error("the confirmation does not name dns-primary — the cursor was resolved against the unfiltered list")
	}
}
