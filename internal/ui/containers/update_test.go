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
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
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
		byName[row[columnName]] = row
	}

	// The metrics start after the status, name and image columns.
	const columnCPU = columnImage + 1
	if got := byName["web"][columnCPU]; got != "12.5%" {
		t.Errorf("web CPU cell = %q, want \"12.5%%\"", got)
	}
	if got := byName["web"][columnCPU+1]; got != "150M/8G" {
		t.Errorf("web memory cell = %q, want \"150M/8G\"", got)
	}
	for _, name := range []string{"api", "cache", "zombie"} {
		for col := columnCPU; col <= columnCPU+5; col++ {
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
	// cycle returns to the start. Two columns do not sort: the status glyph and
	// Ports.
	sortable := len(containerColumns()) - 2
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
		{"cpu descending", columnImage + 1, true, []string{"api", "web", "cache", "zombie"}},
		{"mem descending", columnImage + 2, true, []string{"web", "cache", "zombie", "api"}},
		{"net rx descending", columnImage + 3, true, []string{"web", "cache", "zombie", "api"}},
		{"net tx descending", columnImage + 4, true, []string{"web", "cache", "zombie", "api"}},
		{"block rx descending", columnImage + 5, true, []string{"web", "cache", "zombie", "api"}},
		{"block tx descending", columnImage + 6, true, []string{"web", "cache", "zombie", "api"}},
		// CreatedAt is compared as a string, so an unparseable value sorts
		// after every ISO timestamp rather than being treated as unknown.
		{"created ascending", columnImage + 7, false, []string{"cache", "api", "web", "zombie"}},
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

	if got := m.containerTable.Table().Columns()[columnName].Title; got != "Name ▲" {
		t.Errorf("Name header = %q, want the ascending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.containerTable.Table().Columns()[columnName].Title; got != "Name ▼" {
		t.Errorf("Name header = %q after reversing, want the descending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.containerTable.Table().Columns()[columnImage].Title; got != "Image ▲" {
		t.Errorf("Image header = %q, want the ascending arrow", got)
	}
	if got := m.containerTable.Table().Columns()[columnName].Title; got != "Name" {
		t.Errorf("Name header = %q once Image took over, want it bare", got)
	}
	// Ports is not sortable and must never gain an arrow.
	if got := m.containerTable.Table().Columns()[len(containerColumns())-1].Title; got != "Ports" {
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

	m = feed(t, m, testutil.Key("end"))
	if got := m.containerTable.Cursor(); got != 3 {
		t.Errorf("cursor = %d after end, want 3 (last row)", got)
	}
	m = feed(t, m, testutil.Key("home"))
	if got := m.containerTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d after home, want 0", got)
	}
	m = feed(t, m, testutil.Key("down"))
	if got := m.containerTable.Cursor(); got != 1 {
		t.Errorf("cursor = %d after down, want 1", got)
	}
	m = feed(t, m, testutil.Key("up"))
	if got := m.containerTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d after up, want 0", got)
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

	m, cmd := pressK(t, m, choiceStop)
	if cmd != nil {
		t.Error("K issued a stop for an exited container")
	}
	if got := m.actionLine(); got != "" {
		t.Errorf("actionLine = %q for a container that cannot be stopped", got)
	}

	m = feed(t, m, testutil.Key("down"), testutil.Key("down")) // web, running
	m, cmd = pressK(t, m, choiceStop)
	if cmd == nil {
		t.Error("K did not issue a stop for a running container")
	}
	if got := m.actionLine(); got != "Stopping web…" {
		t.Errorf("actionLine = %q, want \"Stopping web…\"", got)
	}
}

func TestRestartActsOnAnyContainer(t *testing.T) {
	m := loadedModel(t) // api, exited

	m, cmd := pressK(t, m, choiceRestart)

	if cmd == nil {
		t.Error("r did not issue a restart")
	}
	if got := m.actionLine(); got != "Restarting api…" {
		t.Errorf("actionLine = %q, want \"Restarting api…\"", got)
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

			want := tc.wantAction
			if want != "" {
				want += "…"
			}
			if got := m.actionLine(); got != want {
				t.Errorf("actionLine = %q, want %q", got, want)
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
		})
	}
}

func TestActionResultClearsPendingAndRefreshes(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	m, _ = pressK(t, m, choiceStop)
	if m.actionLine() == "" {
		t.Fatal("the stop was not marked as running")
	}

	m, cmd := step(t, m, ContainerActionMsg{Action: "stop", ID: webID, Name: "web"})

	if got := m.actionLine(); got != "" {
		t.Errorf("actionLine = %q after the action completed, want it cleared", got)
	}
	if cmd == nil {
		t.Error("a completed action did not refresh the list")
	}
}

// A failure has to lift the marker as surely as a success does. Clearing only
// on success leaves the row spinning for the life of the view — and hides the
// state the container still has, which is worse than saying nothing.
func TestActionFailureSurfacesAShortMessageAndClearsTheMarker(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	m, _ = pressK(t, m, choiceStop)

	m, cmd := step(t, m, ContainerActionMsg{
		Action: "stop", ID: webID, Name: "web", Err: errors.New("permission denied"),
	})

	if m.errorMsg == "" {
		t.Error("a failed action left errorMsg empty")
	}
	if strings.Contains(m.errorMsg, "permission denied") {
		t.Errorf("errorMsg = %q leaks the raw error; Rule 128 wants a short message plus a log", m.errorMsg)
	}
	if m.containerTable.IsBusy(webID) {
		t.Error("the busy marker survived a failed action: the row spins for good")
	}
	// Rule 128: the message carries its own three-second timer.
	if cmd == nil {
		t.Error("a footer message was set with no timer to clear it")
	}
}

// ── Confirmation modal ───────────────────────────────────────────────────────

func TestDeleteAsksForConfirmationNamingTheContainer(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key(keymap.Delete))

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

	m, cmd := step(t, m, testutil.Key(keymap.Prune))

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
		m := feed(t, loadedModel(t), testutil.Key(keymap.Prune))

		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

		if cmd == nil {
			t.Error("confirming a prune issued no command")
		}
		if !m.pruning {
			t.Error("confirming a prune did not mark the view as pruning")
		}
		if got := m.actionLine(); got != "Pruning containers…" {
			t.Errorf("actionLine = %q, want the prune label", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

		m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

		if cmd == nil {
			t.Error("confirming a delete issued no command")
		}
		if got := m.actionLine(); got != "Removing api…" {
			t.Errorf("actionLine = %q, want \"Removing api…\"", got)
		}
	})
}

func TestCancellingTheConfirmationDoesNothing(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

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
		m := feed(t, loadedModel(t), testutil.Key(keymap.Prune), sharedcomponents.ConfirmModalYesMsg{})
		if !m.pruning {
			t.Fatal("the prune was not marked as running")
		}

		m, cmd := step(t, m, ContainerPruneMsg{Output: "Total reclaimed space: 1.2GB"})

		if m.pruning {
			t.Error("the view still reads as pruning after the prune completed")
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
		// The failure path returns before the refresh, and the message is what
		// says it was taken. The command it does return is Rule 128's timer —
		// asserted, not executed: running it would sleep three seconds.
		if cmd == nil {
			t.Error("a footer message was set with no timer to clear it")
		}
	})
}

// The modal owns the keyboard while it is open.
func TestConfirmationCapturesKeys(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))
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

	m.loading = true
	_, cmd = step(t, m, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while the list was loading")
	}
}

// ── Logs and inspect open the viewer ─────────────────────────────────────────
//
// The pane these replace — its viewport, wrap, scrolling, reload, follow,
// timestamps and pager — is the document viewer's now. The tests that pinned
// that behaviour moved with it, to internal/ui/viewer and internal/viewer;
// what is left to check here is that the right source leaves this view.

func TestLogsAsksForTheViewer(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, testutil.Key(keymap.Logs))

	source := openRequestSource(t, cmd)
	logs, ok := source.(logsSource)
	if !ok {
		t.Fatalf("l asked for a %T, want a logsSource", source)
	}
	if logs.Container != "api" {
		t.Errorf("Container = %q, want the selected container", logs.Container)
	}
	if logs.Kind() != viewerpkg.KindLog {
		t.Errorf("Kind = %q, want log — the producer declares it, it is never sniffed", logs.Kind())
	}
}

func TestInspectAsksForTheViewer(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, testutil.Key("enter"))

	source := openRequestSource(t, cmd)
	inspect, ok := source.(inspectSource)
	if !ok {
		t.Fatalf("i asked for a %T, want an inspectSource", source)
	}
	if inspect.Kind() != viewerpkg.KindJSON {
		t.Errorf("Kind = %q, want json", inspect.Kind())
	}
}

// Only the log source can be followed, paged or re-fetched with timestamps.
// That is what the three single-method interfaces are for: the viewer offers
// `t`, `ctrl+f` and `e` for this document and for no other.
func TestOnlyTheLogSourceCarriesTheOptionalCapabilities(t *testing.T) {
	logs := logsSource{ID: "abc123", Container: "api"}
	inspect := inspectSource{ID: "abc123", Container: "api"}

	if _, ok := viewerpkg.Source(logs).(viewerpkg.Timestamped); !ok {
		t.Error("logsSource is not Timestamped, so `t` never appears")
	}
	if _, ok := viewerpkg.Source(logs).(viewerpkg.Followable); !ok {
		t.Error("logsSource is not Followable, so `ctrl+f` never appears")
	}
	if _, ok := viewerpkg.Source(logs).(viewerpkg.Pageable); !ok {
		t.Error("logsSource is not Pageable, so `e` never appears")
	}

	if _, ok := viewerpkg.Source(inspect).(viewerpkg.Followable); ok {
		t.Error("inspectSource claims to be followable; there is nothing to follow")
	}
	if _, ok := viewerpkg.Source(inspect).(viewerpkg.Pageable); ok {
		t.Error("inspectSource claims a pager; every inspect pager path was deleted")
	}
}

// WithTimestamps returns a new source rather than mutating the one a command may
// already hold (Rule 110).
func TestWithTimestampsLeavesTheOriginalAlone(t *testing.T) {
	original := logsSource{ID: "abc123", Container: "api"}

	updated := original.WithTimestamps(true)

	if original.Timestamps {
		t.Error("WithTimestamps mutated the receiver")
	}
	if !updated.(logsSource).Timestamps {
		t.Error("WithTimestamps did not set the flag on the copy")
	}
}

// PagerExitMsg now only ever comes back from the shell, so it has one branch.
func TestReturningFromAPagerInTheTableRefreshes(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, PagerExitMsg{})

	if cmd == nil {
		t.Error("returning to the table did not restart the refresh loop")
	}
}

func TestInspectRejectsAMalformedContainerID(t *testing.T) {
	broken := containerFixtures()
	for i := range broken {
		broken[i].ID = "not-a-hex-id"
	}
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: broken})

	m, cmd := step(t, m, testutil.Key("enter"))

	if m.errorMsg != "Cannot inspect — invalid container ID" {
		t.Errorf("errorMsg = %q, want inspect to have been refused", m.errorMsg)
	}
	if cmd == nil {
		t.Error("a footer message was set with no timer to clear it")
	}
}

// ── Shell in a new window ────────────────────────────────────────────────────

// Only the early return is exercised: the success path calls detectShell, which
// shells out to `docker exec` synchronously.
func TestShellInNewWindowIsInertOnANonRunningContainer(t *testing.T) {
	m := loadedModel(t) // api, exited

	m, cmd := step(t, m, testutil.Key(keymap.Terminal))

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
		if want := width - 2 - len(containerColumns())*2; total != want {
			t.Errorf("at width %d the columns total %d, want %d so the selected row reaches the border", width, total, want)
		}
	}
}

func TestUnhandledKeysAreInert(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("z"))

	if cmd != nil {
		t.Errorf("an unbound key produced %T", testutil.Msg(cmd))
	}
	if m.confirmModal != nil {
		t.Error("an unbound key changed the view state")
	}
}

// ── A row says what is happening to it (§3.22) ───────────────────────────────

// `docker stop` takes the ten second grace period by default. Nothing on screen
// used to say an action was running at all — the row was identical to one where
// nothing was happening, which is indistinguishable from a freeze.
func TestAStoppingContainerShowsASpinnerInPlaceOfItsState(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	if !strings.Contains(renderedRowFor(t, m, "web"), stateIcon("running")) {
		t.Fatal("the row does not show the running glyph before the action")
	}

	m, _ = pressK(t, m, choiceStop)

	// The override is applied when the row is drawn, not when it is built: the
	// stored cells would otherwise go stale every time the spinner advances.
	if strings.Contains(renderedRowFor(t, m, "web"), stateIcon("running")) {
		t.Error("the stopping container still shows its running glyph")
	}
	// And no other row is touched.
	if !strings.Contains(renderedRowFor(t, m, "cache"), stateIcon("paused")) {
		t.Error("an untouched row lost its own state glyph")
	}
}

// The refusal is not cosmetic: a second command against a container already
// stopping fails with "no such container", so the user is told an action failed
// when the first one in fact worked.
func TestASecondActionOnTheSameContainerIsRefused(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	m, _ = pressK(t, m, choiceStop)

	m, cmd := pressK(t, m, choiceStop)

	// The refusal is what the message proves; the command it returns is Rule
	// 128's timer, asserted rather than executed.
	if m.errorMsg != busyMessage {
		t.Errorf("errorMsg = %q, want the busy message", m.errorMsg)
	}
	if cmd == nil {
		t.Error("a footer message was set with no timer to clear it")
	}
}

// The cursor is deliberately not locked. A scan already runs with a spinner in
// the cell while the user keeps navigating, and locking the cursor would look
// like the freeze this exists to remove.
func TestTheCursorStillMovesWhileAnActionRuns(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	m, _ = pressK(t, m, choiceStop)

	m = feed(t, m, testutil.Key("up"))

	selected, ok := m.containerTable.Selected()
	if !ok || selected.Name == "web" {
		t.Error("the cursor was locked to the container the action is running on")
	}
	// And the marker stayed with the object rather than following the cursor.
	if !m.containerTable.IsBusy(webID) {
		t.Error("moving the cursor cleared the busy marker")
	}
}

// The list is replaced every refresh tick while an action runs. Keying the
// marker on the container ID rather than on a flag in the row is what survives.
func TestTheMarkerSurvivesARefresh(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down"))
	m, _ = pressK(t, m, choiceStop)

	m = feed(t, m, ContainersListMsg{Containers: containerFixtures()})

	if !m.containerTable.IsBusy(webID) {
		t.Error("a periodic refresh dropped the busy marker")
	}
}

// The glyph says something is happening; the footer says what. It is rendered
// from the current state rather than set as a footer message, because a footer
// message expires after three seconds and `docker stop` outlives that by seven.
func TestTheFooterNamesTheRunningAction(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down"))
	m, _ = pressK(t, m, choiceStop)

	if !strings.Contains(m.RenderFooter(120), "Stopping web") {
		t.Errorf("the footer does not name the action:\n%s", m.RenderFooter(120))
	}

	m = feed(t, m, ContainerActionMsg{Action: "stop", ID: webID, Name: "web"})
	if strings.Contains(m.RenderFooter(120), "Stopping web") {
		t.Error("the footer still names an action that finished")
	}
}

// An error the user has not read yet matters more than the progress of what is
// still running, so it takes the line.
func TestAnErrorTakesTheFooterAheadOfTheRunningAction(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down"))
	m, _ = pressK(t, m, choiceStop)
	m.errorMsg = "Something went wrong"

	footer := m.RenderFooter(120)

	if !strings.Contains(footer, "Something went wrong") {
		t.Errorf("the error was hidden by the action line:\n%s", footer)
	}
	if strings.Contains(footer, "Stopping web") {
		t.Errorf("both lines were rendered at once:\n%s", footer)
	}
}

// renderedRowFor returns the drawn line for a container, which is the only
// place the busy override is visible — it is applied at render time so the
// stored cells do not go stale as the spinner advances.
func renderedRowFor(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, line := range strings.Split(m.containerTable.View(), "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no rendered row for %q", name)
	return ""
}
