package containers

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Construction and loading ─────────────────────────────────────────────────

func TestNewStartsLoading(t *testing.T) {
	m := New(config.Default())

	if !m.loading {
		t.Error("loading = false on a new model, so the spinner never shows before the first list arrives")
	}
	if m.showAll {
		t.Error("showAll = true on a new model, want active containers only")
	}
	if m.state != stateTable {
		t.Errorf("state = %d on a new model, want stateTable", m.state)
	}
}

func TestInitFetchesTheList(t *testing.T) {
	if cmd := New(config.Default()).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so the list is never fetched")
	}
}

func TestContainersListPopulatesTheTable(t *testing.T) {
	m := loadedModel(t)

	if m.loading {
		t.Error("loading = true after the list arrived")
	}
	if m.errorMsg != "" {
		t.Errorf("errorMsg = %q after a successful list", m.errorMsg)
	}
	if got := rowNames(tableRows(m)); len(got) != 4 {
		t.Errorf("table holds %v, want the four fixtures", got)
	}
}

func TestContainersListErrorSurfacesAndKeepsRows(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, ContainersListMsg{Err: errors.New("docker daemon is down")})

	if m.errorMsg == "" {
		t.Error("a failed list left errorMsg empty")
	}
	if strings.Contains(m.errorMsg, "daemon") {
		t.Errorf("errorMsg = %q leaks the raw error into the UI; Rule 128 wants a short message plus a log", m.errorMsg)
	}
	if len(tableRows(m)) != 4 {
		t.Error("a failed list wiped the table instead of keeping the last known state")
	}
	if m.loading {
		t.Error("loading = true after a failed list, so the spinner never stops")
	}
}

// ── Metrics ──────────────────────────────────────────────────────────────────

func TestMetricsMergeIntoTheMatchingContainer(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, ContainerMetricsMsg{Metrics: map[string]docker.Container{
		"aaaa111122223333": {
			CPUPercent: 42.5, MemUsage: "500MiB / 7.776GiB", MemPercent: 6.3,
			NetIO: "9kB / 9kB", NetRX: 9000, NetTX: 9000,
			BlockIO: "2MB / 1MB", BlockRX: 2_000_000, BlockTX: 1_000_000,
		},
	}})

	var web docker.Container
	for _, c := range m.containerTable.Items() {
		if c.Name == "web" {
			web = c
		}
	}
	if web.CPUPercent != 42.5 {
		t.Errorf("web CPU = %v after the metrics merge, want 42.5", web.CPUPercent)
	}
	if web.NetRX != 9000 || web.BlockTX != 1_000_000 {
		t.Errorf("web counters = rx %d / blk tx %d, want 9000 / 1000000", web.NetRX, web.BlockTX)
	}

	// Containers absent from the metrics map keep whatever they had.
	for _, c := range m.containerTable.Items() {
		if c.Name == "api" && c.CPUPercent != 99 {
			t.Errorf("api CPU = %v, want it untouched by a merge that did not mention it", c.CPUPercent)
		}
	}
}

func TestMetricsFailureIsIgnored(t *testing.T) {
	m := loadedModel(t)
	before := m.containerTable.Items()[0].CPUPercent

	m = feed(t, m,
		ContainerMetricsMsg{Err: errors.New("stats failed")},
		ContainerMetricsMsg{Metrics: nil},
	)

	if m.containerTable.Items()[0].CPUPercent != before {
		t.Error("a failed metrics fetch overwrote the previous values")
	}
	if m.errorMsg != "" {
		t.Errorf("errorMsg = %q; a metrics hiccup should not shout at the user", m.errorMsg)
	}
}

// ── Cell formatting ──────────────────────────────────────────────────────────

// Metrics belong to running containers only: a stopped one has no live CPU or
// memory, and showing its last values would be a lie.
func TestOnlyRunningContainersShowMetrics(t *testing.T) {
	m := loadedModel(t)

	byName := map[string]table.Row{}
	for _, row := range tableRows(m) {
		byName[row[0]] = row
	}

	if got := byName["web"][2]; got != "12.5%" {
		t.Errorf("web CPU cell = %q, want \"12.5%%\"", got)
	}
	if got := byName["web"][3]; got != "150M/8G" {
		t.Errorf("web memory cell = %q, want \"150M/8G\"", got)
	}
	for _, name := range []string{"api", "cache", "zombie"} {
		for _, col := range []int{2, 3, 4, 5, 6, 7} {
			if got := byName[name][col]; got != "-" {
				t.Errorf("%s column %d = %q, want \"-\" for a non-running container", name, col, got)
			}
		}
	}
}

func TestFormatNetBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0kB"},
		{1200, "1.2kB"},
		{1_000_000, "1.0MB"},
		{3_400_000, "3.4MB"},
		{1_000_000_000, "1.0GB"},
		{2_500_000_000, "2.5GB"},
	}

	for _, tc := range tests {
		if got := formatNetBytes(tc.in); got != tc.want {
			t.Errorf("formatNetBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatMemUsage(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"150MiB / 7.776GiB", "150M/8G"},
		{"512KiB / 1GiB", "512K/1G"},
		{"1.5GiB / 8GiB", "2G/8G"},
		{"800B / 1GiB", "800B/1G"},
		{"no slash here", "no slash here"},
		{"weird / units", "weird/units"},
		{"abcMiB / 1GiB", "abcMiB/1G"}, // unparseable number passes through
	}

	for _, tc := range tests {
		if got := formatMemUsage(tc.in); got != tc.want {
			t.Errorf("formatMemUsage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRelativeTimeFallsBackToTheRawValue(t *testing.T) {
	if got := relativeTime("not a timestamp"); got != "not a timestamp" {
		t.Errorf("relativeTime on an unparseable value = %q, want it passed through", got)
	}

	// Both Docker layouts must parse, with and without the zone name.
	for _, in := range []string{
		time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04:05 -0700 MST"),
		time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04:05 -0700"),
	} {
		if got := relativeTime(in); got == in || got == "" {
			t.Errorf("relativeTime(%q) = %q, want a relative label", in, got)
		}
	}
}

func TestStateIconIsDistinctPerState(t *testing.T) {
	seen := map[string]string{}
	for _, state := range []string{"running", "paused", "exited", "created", "restarting", "dead", "something-else"} {
		icon := stateIcon(state)
		if icon == "" {
			t.Errorf("stateIcon(%q) returned nothing", state)
		}
		seen[state] = icon
	}
	if seen["running"] == seen["exited"] || seen["running"] == seen["paused"] {
		t.Error("running, paused and exited must be distinguishable at a glance")
	}
	if seen["created"] != seen["restarting"] {
		t.Error("created and restarting deliberately share an icon")
	}
}

// Rule 122: a styled string in table.Row is truncated mid-escape by
// runewidth.Truncate and bleeds into every row below. The colour profile has to
// be forced, otherwise lipgloss strips the sequences and this passes whatever
// the code does.
func TestTableCellsCarryNoANSISequences(t *testing.T) {
	withTrueColor(t)
	m := loadedModel(t)

	for _, row := range tableRows(m) {
		for i, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("cell %d = %q contains an escape sequence", i, cell)
			}
		}
	}
}

// ── Filtering and sorting ────────────────────────────────────────────────────

func TestFilterMatchesNameImageAndState(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"web", []string{"web"}},
		{"redis", []string{"cache"}},
		{"exited", []string{"api"}},
		{"nomatch", nil},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key("/"))
			m = feed(t, m, testutil.Type(tc.query)...)

			got := rowNames(tableRows(m))
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

// The list used to open Z→A because New() left sortAsc at its zero value while
// the status view set it explicitly (D9). Both now agree, and the constructor
// says so rather than relying on a default.
func TestDefaultSortIsNameAscending(t *testing.T) {
	m := rawModel(t)

	if column, desc := m.containerTable.SortState(); column != columnName || desc {
		t.Errorf("default sort = (column %d, desc=%v), want name ascending", column, desc)
	}
	if got := rowNames(tableRows(m)); got[0] != "api" {
		t.Errorf("first row = %q, want api under an ascending default (full order %v)", got[0], got)
	}
}

func TestCycleSortWalksDirectionThenColumn(t *testing.T) {
	m := rawModel(t) // starts at (name, ascending)

	// One press from ascending reverses the same column.
	m = feed(t, m, testutil.Key("."))
	if column, desc := m.containerTable.SortState(); column != columnName || !desc {
		t.Errorf("sort = (column %d, desc=%v) after one press, want (name, desc)", column, desc)
	}

	// The next press flips direction back and advances the column.
	m = feed(t, m, testutil.Key("."))
	if column, desc := m.containerTable.SortState(); column != columnImage || desc {
		t.Errorf("sort = (column %d, desc=%v) after two presses, want (image, asc)", column, desc)
	}

	// Each sortable column is visited ascending then descending, so a full
	// cycle returns to the start. Every column but Ports sorts.
	sortable := len(containerColumns()) - 1
	for range sortable*2 - 2 {
		m = feed(t, m, testutil.Key("."))
	}
	if column, desc := m.containerTable.SortState(); column != columnName || desc {
		t.Errorf("sort = (column %d, desc=%v) after a full cycle, want the starting (name, asc)", column, desc)
	}
}

func TestEachColumnOrdersByItsOwnValue(t *testing.T) {
	tests := []struct {
		name   string
		column int
		desc   bool
		want   []string
	}{
		{"name ascending", columnName, false, []string{"api", "cache", "web", "zombie"}},
		{"name descending", columnName, true, []string{"zombie", "web", "cache", "api"}},
		{"image ascending", columnImage, false, []string{"zombie", "api", "web", "cache"}},
		{"cpu descending", 2, true, []string{"api", "web", "cache", "zombie"}},
		{"mem descending", 3, true, []string{"web", "cache", "zombie", "api"}},
		{"net rx descending", 4, true, []string{"web", "cache", "zombie", "api"}},
		{"net tx descending", 5, true, []string{"web", "cache", "zombie", "api"}},
		{"block rx descending", 6, true, []string{"web", "cache", "zombie", "api"}},
		{"block tx descending", 7, true, []string{"web", "cache", "zombie", "api"}},
		// CreatedAt is compared as a string, so an unparseable value sorts
		// after every ISO timestamp rather than being treated as unknown.
		{"created ascending", 8, false, []string{"cache", "api", "web", "zombie"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := orderUnder(tc.column, tc.desc)

			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("position %d = %q, want %q (full order %v)", i, got[i], want, got)
				}
			}
		})
	}
}

// Sorting reorders what is shown, never the list the view was handed — the
// caller's slice and Items() both stay in arrival order.
func TestSortingLeavesTheSourceListAlone(t *testing.T) {
	input := containerFixtures()

	m := feed(t, newTestModel(t), ContainersListMsg{Containers: input})

	if input[0].Name != "web" {
		t.Errorf("the sort reordered the caller's slice: first entry is now %q", input[0].Name)
	}
	if got := m.containerTable.Items()[0].Name; got != "web" {
		t.Errorf("Items()[0] = %q, want the list in the order it arrived", got)
	}
	if got := m.containerTable.Visible()[0].Name; got != "api" {
		t.Errorf("Visible()[0] = %q, want the sorted order", got)
	}
}

func TestSortIndicatorFollowsTheActiveColumn(t *testing.T) {
	m := loadedModel(t) // name ascending

	if got := m.containerTable.Table().Columns()[0].Title; got != "Name ▲" {
		t.Errorf("Name header = %q, want the ascending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.containerTable.Table().Columns()[0].Title; got != "Name ▼" {
		t.Errorf("Name header = %q after reversing, want the descending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.containerTable.Table().Columns()[1].Title; got != "Image ▲" {
		t.Errorf("Image header = %q, want the ascending arrow", got)
	}
	if got := m.containerTable.Table().Columns()[0].Title; got != "Name" {
		t.Errorf("Name header = %q once Image took over, want it bare", got)
	}
	// Ports is not sortable and must never gain an arrow.
	if got := m.containerTable.Table().Columns()[9].Title; got != "Ports" {
		t.Errorf("Ports header = %q, want it bare", got)
	}
}

// ── Selection ────────────────────────────────────────────────────────────────

// The cursor indexes the sorted, filtered view — not m.containers — so every
// action resolves through the same path.
func TestSelectionResolvesThroughSortAndFilter(t *testing.T) {
	m := loadedModel(t) // sorted by name: api, cache, web, zombie

	if got := m.getSelectedContainer(); got == nil || got.Name != "api" {
		t.Fatalf("first selection = %v, want api", got)
	}

	m = feed(t, m, testutil.Key("down"), testutil.Key("down"))
	if got := m.getSelectedContainer(); got == nil || got.Name != "web" {
		t.Fatalf("selection after two downs = %v, want web", got)
	}

	// No SetCursor here: the cursor sat on row 2 and the filter leaves one row.
	// Clamping it is the table's job now, and it is what the view never did.
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	if got := m.getSelectedContainer(); got == nil || got.Name != "cache" {
		t.Fatalf("selection under a filter = %v, want cache", got)
	}
}

func TestSelectionIsNilWhenTheListIsEmpty(t *testing.T) {
	m := newTestModel(t)

	if got := m.getSelectedContainer(); got != nil {
		t.Errorf("getSelectedContainer() = %v with no containers, want nil", got)
	}
}

func TestNavigationKeys(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("G"))
	if got := m.containerTable.Cursor(); got != 3 {
		t.Errorf("cursor = %d after G, want 3 (last row)", got)
	}
	m = feed(t, m, testutil.Key("g"))
	if got := m.containerTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d after g, want 0", got)
	}
	m = feed(t, m, testutil.Key("j"))
	if got := m.containerTable.Cursor(); got != 1 {
		t.Errorf("cursor = %d after j, want 1", got)
	}
	m = feed(t, m, testutil.Key("k"))
	if got := m.containerTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d after k, want 0", got)
	}
}

// A stopped or dead container is styled as an error row; the style has to
// follow the cursor rather than being set once. bubbles keeps its styles
// unexported, so this reads them back out of the rendered table.
func TestSelectionStyleFollowsTheContainerState(t *testing.T) {
	withTrueColor(t)
	errorSelection := ansiPrefix(theme.TableStylesForState("error").Selected.Render("x"))
	if errorSelection == "" {
		t.Fatal("the error selection style renders no escape sequence; the colour profile is not forced")
	}

	m := loadedModel(t) // cursor on api, which is exited
	if !strings.Contains(m.containerTable.View(), errorSelection) {
		t.Error("an exited container is not selected in the error colour")
	}

	m = feed(t, m, testutil.Key("down")) // cache, paused
	if strings.Contains(m.containerTable.View(), errorSelection) {
		t.Error("the selection stayed on the error colour after moving to a paused container")
	}

	m = feed(t, m, testutil.Key("up")) // back to api
	if !strings.Contains(m.containerTable.View(), errorSelection) {
		t.Error("the selection did not return to the error colour on an exited container")
	}
}

// ── Lifecycle actions ────────────────────────────────────────────────────────

func TestStopOnlyActsOnRunningContainers(t *testing.T) {
	m := loadedModel(t) // api, exited

	m, cmd := step(t, m, testutil.Key("K"))
	if cmd != nil {
		t.Error("K issued a stop for an exited container")
	}
	if m.pendingAction != "" {
		t.Errorf("pendingAction = %q for a container that cannot be stopped", m.pendingAction)
	}

	m = feed(t, m, testutil.Key("down"), testutil.Key("down")) // web, running
	m, cmd = step(t, m, testutil.Key("K"))
	if cmd == nil {
		t.Error("K did not issue a stop for a running container")
	}
	if m.pendingAction != "Stopping web" {
		t.Errorf("pendingAction = %q, want \"Stopping web\"", m.pendingAction)
	}
}

func TestRestartActsOnAnyContainer(t *testing.T) {
	m := loadedModel(t) // api, exited

	m, cmd := step(t, m, testutil.Key("r"))

	if cmd == nil {
		t.Error("r did not issue a restart")
	}
	if m.pendingAction != "Restarting api" {
		t.Errorf("pendingAction = %q, want \"Restarting api\"", m.pendingAction)
	}
}

func TestSpaceTogglesPauseAccordingToState(t *testing.T) {
	tests := []struct {
		name       string
		downs      int
		wantAction string
	}{
		{"exited container is inert", 0, ""},
		{"paused container resumes", 1, "Resuming cache"},
		{"running container pauses", 2, "Pausing web"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			for i := 0; i < tc.downs; i++ {
				m = feed(t, m, testutil.Key("down"))
			}

			m, cmd := step(t, m, testutil.Key(" "))

			if m.pendingAction != tc.wantAction {
				t.Errorf("pendingAction = %q, want %q", m.pendingAction, tc.wantAction)
			}
			if (cmd != nil) != (tc.wantAction != "") {
				t.Errorf("command issued = %v, want %v", cmd != nil, tc.wantAction != "")
			}
		})
	}
}

func TestActionsAreInertWithoutASelection(t *testing.T) {
	for _, key := range []string{"K", "r", " ", "ctrl+d", "l", "i"} {
		t.Run(key, func(t *testing.T) {
			m := newTestModel(t) // no containers

			m, cmd := step(t, m, testutil.Key(key))

			if cmd != nil {
				t.Errorf("%q issued a command with nothing selected", key)
			}
			if m.confirmModal != nil {
				t.Errorf("%q opened a confirmation with nothing selected", key)
			}
			if m.state != stateTable {
				t.Errorf("%q left the table view with nothing selected", key)
			}
		})
	}
}

func TestActionResultClearsPendingAndRefreshes(t *testing.T) {
	m := loadedModel(t)
	m.pendingAction = "Stopping web"

	m, cmd := step(t, m, ContainerActionMsg{Action: "stop", ID: "web"})

	if m.pendingAction != "" {
		t.Errorf("pendingAction = %q after the action completed, want it cleared", m.pendingAction)
	}
	if cmd == nil {
		t.Error("a completed action did not refresh the list")
	}
}

func TestActionFailureSurfacesAShortMessage(t *testing.T) {
	m := loadedModel(t)
	m.pendingAction = "Stopping web"

	m, cmd := step(t, m, ContainerActionMsg{Action: "stop", ID: "web", Err: errors.New("permission denied")})

	if m.errorMsg == "" {
		t.Error("a failed action left errorMsg empty")
	}
	if strings.Contains(m.errorMsg, "permission denied") {
		t.Errorf("errorMsg = %q leaks the raw error; Rule 128 wants a short message plus a log", m.errorMsg)
	}
	if cmd != nil {
		t.Error("a failed action still refreshed the list")
	}
	if m.pendingAction != "" {
		t.Errorf("pendingAction = %q after a failure, want it cleared", m.pendingAction)
	}
}

// ── Confirmation modal ───────────────────────────────────────────────────────

func TestDeleteAsksForConfirmationNamingTheContainer(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+d"))

	if m.confirmModal == nil {
		t.Fatal("ctrl+d did not open the confirmation")
	}
	if cmd != nil {
		t.Error("ctrl+d removed the container before it was confirmed")
	}
	if m.pendingAction != "confirm-delete" {
		t.Errorf("pendingAction = %q, want \"confirm-delete\"", m.pendingAction)
	}
	if !strings.Contains(m.confirmModal.View(), "api") {
		t.Error("the confirmation does not name the container")
	}
}

func TestPruneAsksForConfirmation(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("p"))

	if m.confirmModal == nil {
		t.Fatal("p did not open the confirmation")
	}
	if cmd != nil {
		t.Error("p pruned before the user confirmed")
	}
	if m.pendingAction != "confirm-prune" {
		t.Errorf("pendingAction = %q, want \"confirm-prune\"", m.pendingAction)
	}
}

// The same modal serves delete and prune, so the pending action is what routes
// the answer — confirming a prune must not delete the selected container.
func TestConfirmationRoutesByPendingAction(t *testing.T) {
	t.Run("prune", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key("p"))

		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

		if cmd == nil {
			t.Error("confirming a prune issued no command")
		}
		if m.pendingAction != "Pruning containers..." {
			t.Errorf("pendingAction = %q, want the prune label", m.pendingAction)
		}
	})

	t.Run("delete", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

		if cmd == nil {
			t.Error("confirming a delete issued no command")
		}
		if m.pendingAction != "Removing api" {
			t.Errorf("pendingAction = %q, want \"Removing api\"", m.pendingAction)
		}
	})
}

func TestCancellingTheConfirmationDoesNothing(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if m.confirmModal != nil {
		t.Error("the confirmation stayed open after answering no")
	}
	if m.pendingAction != "" {
		t.Errorf("pendingAction = %q after cancelling, want it cleared", m.pendingAction)
	}
	if cmd != nil {
		t.Error("cancelling still issued a command")
	}
}

func TestPruneResultHandling(t *testing.T) {
	t.Run("success refreshes", func(t *testing.T) {
		m := loadedModel(t)
		m.pendingAction = "Pruning containers..."

		m, cmd := step(t, m, ContainerPruneMsg{Output: "Total reclaimed space: 1.2GB"})

		if m.pendingAction != "" {
			t.Errorf("pendingAction = %q after the prune completed", m.pendingAction)
		}
		if cmd == nil {
			t.Error("a completed prune did not refresh the list")
		}
	})

	t.Run("failure surfaces a short message", func(t *testing.T) {
		m := loadedModel(t)

		m, cmd := step(t, m, ContainerPruneMsg{Err: errors.New("daemon refused")})

		if m.errorMsg == "" {
			t.Error("a failed prune left errorMsg empty")
		}
		if strings.Contains(m.errorMsg, "daemon refused") {
			t.Errorf("errorMsg = %q leaks the raw error", m.errorMsg)
		}
		if cmd != nil {
			t.Error("a failed prune still refreshed the list")
		}
	})
}

// The modal owns the keyboard while it is open.
func TestConfirmationCapturesKeys(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))
	cursor := m.containerTable.Cursor()

	m = feed(t, m, testutil.Key("down"))

	if m.containerTable.Cursor() != cursor {
		t.Error("a keystroke reached the table while the confirmation was open")
	}
}

// ── Refresh ──────────────────────────────────────────────────────────────────

func TestToggleAllRefetches(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("a"))

	if !m.showAll {
		t.Error("a did not switch to all containers")
	}
	if !m.loading {
		t.Error("loading = false after switching scope, so the spinner never shows")
	}
	if cmd == nil {
		t.Error("switching scope did not refetch")
	}

	m, _ = step(t, m, testutil.Key("a"))
	if m.showAll {
		t.Error("a did not switch back to active containers")
	}
}

func TestRefreshTickFetchesListAndMetrics(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, RefreshTickMsg(time.Now()))

	if cmd == nil {
		t.Fatal("the refresh tick issued no command, so the view goes stale")
	}
}

func TestCtrlRRefreshes(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if !m.loading {
		t.Error("loading = false after ctrl+r")
	}
	if cmd == nil {
		t.Error("ctrl+r issued no command")
	}
}

func TestSpinnerTicksOnlyWhileLoading(t *testing.T) {
	m := loadedModel(t) // loading is false once the list arrived

	_, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		t.Error("an idle model kept the spinner running")
	}

	m.logsLoading = true
	_, cmd = step(t, m, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while the logs were loading")
	}
}

// ── Logs viewport ────────────────────────────────────────────────────────────

func TestLogsOpensTheViewportForTheSelectedContainer(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("l"))

	if m.state != stateLogs {
		t.Fatal("l did not switch to the logs view")
	}
	if m.logsContainerName != "api" {
		t.Errorf("logsContainerName = %q, want the selected container", m.logsContainerName)
	}
	if !m.logsLoading {
		t.Error("logsLoading = false right after opening the view")
	}
	if cmd == nil {
		t.Error("opening the logs view did not fetch anything")
	}
}

func TestLogsContentIsStrippedAndNormalised(t *testing.T) {
	m := logsModel(t, "\x1b[31mred\x1b[0m line\r\nsecond\rthird\n")

	if strings.Contains(m.logsRawContent, "\x1b") {
		t.Errorf("log content kept escape sequences: %q", m.logsRawContent)
	}
	if strings.Contains(m.logsRawContent, "\r") {
		t.Errorf("log content kept carriage returns: %q", m.logsRawContent)
	}
	if !strings.Contains(m.logsRawContent, "red line") {
		t.Errorf("stripping mangled the text: %q", m.logsRawContent)
	}
	if m.logsLoading {
		t.Error("logsLoading = true after the content arrived")
	}
}

func TestLogsFailureShowsAMessageInTheViewport(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("l"))

	m = feed(t, m, ContainerLogsLoadedMsg{Err: errors.New("no such container")})

	if m.logsLoading {
		t.Error("logsLoading = true after a failed fetch")
	}
	if !strings.Contains(m.logsRawContent, "Failed to load logs") {
		t.Errorf("logsRawContent = %q, want a short failure message", m.logsRawContent)
	}
	if strings.Contains(m.logsRawContent, "no such container") {
		t.Error("the raw Docker error leaked into the viewport")
	}
}

func TestEscLeavesTheLogsView(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			m := logsModel(t, "line")

			m = feed(t, m, testutil.Key(key))

			if m.state != stateTable {
				t.Errorf("%q did not return to the table", key)
			}
		})
	}
}

func TestLogsWrapToggleRebuildsTheViewport(t *testing.T) {
	long := strings.Repeat("x", 500)
	m := logsModel(t, long)

	m = feed(t, m, testutil.Key("w"))
	if !m.logsWrapEnabled {
		t.Fatal("w did not enable wrapping")
	}
	// The raw content is untouched; only the viewport is rebuilt.
	if m.logsRawContent != long {
		t.Error("enabling wrap modified the raw log content")
	}

	m = feed(t, m, testutil.Key("w"))
	if m.logsWrapEnabled {
		t.Error("w did not disable wrapping")
	}
}

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
		want    string
	}{
		{"short lines pass through", "ab\ncd", 10, "ab\ncd"},
		{"exact width is not split", "abcde", 5, "abcde"},
		{"long line splits", "abcdefg", 3, "abc\ndef\ng"},
		{"zero width is a no-op", "abcdefg", 0, "abcdefg"},
		{"negative width is a no-op", "abcdefg", -1, "abcdefg"},
		{"empty content", "", 5, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrapLines(tc.content, tc.width); got != tc.want {
				t.Errorf("wrapLines(%q, %d) = %q, want %q", tc.content, tc.width, got, tc.want)
			}
		})
	}
}

// Multibyte content must wrap on runes, not bytes, or the split lands mid-glyph.
func TestWrapLinesCountsRunes(t *testing.T) {
	got := wrapLines("ééééé", 2)

	if got != "éé\néé\né" {
		t.Errorf("wrapLines on multibyte content = %q, want it split every two runes", got)
	}
}

func TestTimestampsToggleRefetches(t *testing.T) {
	m := logsModel(t, "line")

	m, cmd := step(t, m, testutil.Key("t"))

	if !m.logsTimestamps {
		t.Error("t did not enable timestamps")
	}
	if !m.logsLoading {
		t.Error("logsLoading = false after toggling timestamps, so the spinner never shows")
	}
	if cmd == nil {
		t.Error("toggling timestamps did not refetch the logs")
	}
}

func TestLogsReloadRefetches(t *testing.T) {
	m := logsModel(t, "line")

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if !m.logsLoading {
		t.Error("logsLoading = false after ctrl+r in the logs view")
	}
	if cmd == nil {
		t.Error("ctrl+r in the logs view issued no fetch")
	}
}

func TestLogsScrollKeys(t *testing.T) {
	m := logsModel(t, strings.Repeat("line\n", 200))

	m = feed(t, m, testutil.Key("g"))
	if m.logsViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after g, want the top", m.logsViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("down"))
	if m.logsViewport.YOffset != 1 {
		t.Errorf("YOffset = %d after down, want 1", m.logsViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("up"))
	if m.logsViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after up, want 0", m.logsViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("pgdown"))
	if m.logsViewport.YOffset == 0 {
		t.Error("pgdown did not scroll")
	}

	m = feed(t, m, testutil.Key("G"))
	atBottom := m.logsViewport.YOffset
	m = feed(t, m, testutil.Key("pgup"))
	if m.logsViewport.YOffset >= atBottom {
		t.Error("pgup did not scroll back up")
	}
}

// The pager and the follow stream both suspend the TUI; returning from either
// must reload rather than leave the stale buffer on screen.
func TestReturningFromAPagerReloadsTheLogs(t *testing.T) {
	m := logsModel(t, "line")

	m, cmd := step(t, m, PagerExitMsg{})

	if !m.logsLoading {
		t.Error("logsLoading = false after returning from the pager")
	}
	if cmd == nil {
		t.Error("returning from the pager did not refetch")
	}
}

func TestReturningFromAPagerInTheTableRefreshes(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, PagerExitMsg{})

	if cmd == nil {
		t.Error("returning to the table did not restart the refresh loop")
	}
}

// The external pager builds a shell command from the container ID, so a
// malformed ID must be rejected before it reaches a shell.
func TestExternalPagerRejectsAMalformedContainerID(t *testing.T) {
	m := logsModel(t, "line")
	m.logsContainerID = "abc; rm -rf /"

	m, cmd := step(t, m, testutil.Key("e"))

	if cmd != nil {
		t.Error("the pager ran with a malformed container ID")
	}
	if m.errorMsg == "" {
		t.Error("rejecting the ID left errorMsg empty")
	}
}

func TestInspectRejectsAMalformedContainerID(t *testing.T) {
	broken := containerFixtures()
	for i := range broken {
		broken[i].ID = "not-a-hex-id"
	}
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: broken})

	m, cmd := step(t, m, testutil.Key("i"))

	if cmd != nil {
		t.Error("inspect ran with a malformed container ID")
	}
	if m.errorMsg == "" {
		t.Error("rejecting the ID left errorMsg empty")
	}
}

// ── Shell in a new window ────────────────────────────────────────────────────

// Only the early return is exercised: the success path calls detectShell, which
// shells out to `docker exec` synchronously.
func TestShellInNewWindowIsInertOnANonRunningContainer(t *testing.T) {
	m := loadedModel(t) // api, exited

	m, cmd := step(t, m, testutil.Key("S"))

	if cmd != nil {
		t.Error("S launched a shell for an exited container")
	}
	if m.errorMsg != "" {
		t.Errorf("errorMsg = %q; an inert key should say nothing", m.errorMsg)
	}
}

func TestShellWindowFailureSurfaces(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, ShellWindowOpenedMsg{Err: errors.New("no terminal emulator")})

	if m.errorMsg == "" {
		t.Error("a failed terminal launch left errorMsg empty")
	}
	if strings.Contains(m.errorMsg, "emulator") {
		t.Errorf("errorMsg = %q leaks the raw error", m.errorMsg)
	}
}

func TestShellWindowSuccessSaysNothing(t *testing.T) {
	m := feed(t, loadedModel(t), ShellWindowOpenedMsg{})

	if m.errorMsg != "" {
		t.Errorf("errorMsg = %q after a successful launch", m.errorMsg)
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 116 at every width, not just the roomy one. Ten columns asking for 120
// characters do not fit a 100-wide terminal, and the old percentages rounded
// down ten times over — the shortfall is shared out now, and the sum has to
// land on the nose either way.
func TestResizeFillsTheViewportWidth(t *testing.T) {
	for _, width := range []int{60, 100, 140, 200} {
		m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: width, Height: 40})

		total := 0
		for _, col := range m.containerTable.Table().Columns() {
			total += col.Width
		}
		// terminal minus viewport borders minus two columns of padding per cell
		if want := width - 2 - 10*2; total != want {
			t.Errorf("at width %d the columns total %d, want %d so the selected row reaches the border", width, total, want)
		}
	}
}

func TestResizeReflowsWrappedLogs(t *testing.T) {
	m := logsModel(t, strings.Repeat("x", 300))
	m = feed(t, m, testutil.Key("w")) // wrap on

	// Narrowing must re-split the content, not leave lines longer than the
	// viewport.
	m = feed(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})

	if m.logsViewport.Width != 40 {
		t.Errorf("logs viewport width = %d after the resize, want 40", m.logsViewport.Width)
	}
}

func TestUnhandledKeysAreInert(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("z"))

	if cmd != nil {
		t.Errorf("an unbound key produced %T", testutil.Msg(cmd))
	}
	if m.confirmModal != nil || m.state != stateTable {
		t.Error("an unbound key changed the view state")
	}
}
