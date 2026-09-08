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
	// None of them: the default set is the empty selection, which is what keeps
	// the filter bar off the screen on arrival.
	for _, label := range stateTokens {
		if m.containerTable.IsTokenActive(label) {
			t.Errorf("%s is lit on a new model, so the view opens with a filter bar", label)
		}
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
	if m.footer.IsSet() {
		t.Errorf("footer = %q after a successful list", m.footer.Text())
	}
	if got := rowNames(tableRows(m)); len(got) != 4 {
		t.Errorf("table holds %v, want the four fixtures", got)
	}
}

func TestContainersListErrorSurfacesAndKeepsRows(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, ContainersListMsg{Err: errors.New("docker daemon is down")})

	if !m.footer.IsSet() {
		t.Error("a failed list said nothing")
	}
	if strings.Contains(m.footer.Text(), "daemon") {
		t.Errorf("footer = %q leaks the raw error into the UI; Rule 128 wants a short message plus a log", m.footer.Text())
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
	if m.footer.IsSet() {
		t.Errorf("footer = %q; a metrics hiccup should not shout at the user", m.footer.Text())
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

	// The metrics start after the status, name and image columns: CPU, its
	// gauge, Mem, its gauge, then the four I/O counters.
	const (
		columnCPU      = columnImage + 1
		columnCPUGauge = columnCPU + 1
		columnMem      = columnCPU + 2
		columnMemGauge = columnCPU + 3
		columnLastIO   = columnCPU + 7
	)
	if got := byName["web"][columnCPU]; got != "12.5%" {
		t.Errorf("web CPU cell = %q, want \"12.5%%\"", got)
	}
	if got := byName["web"][columnMem]; got != "150M/8G" {
		t.Errorf("web memory cell = %q, want \"150M/8G\"", got)
	}
	for _, name := range []string{"api", "cache", "zombie"} {
		for col := columnCPU; col <= columnLastIO; col++ {
			if got := byName[name][col]; got != "-" {
				t.Errorf("%s column %d = %q, want \"-\" for a non-running container", name, col, got)
			}
		}
	}
}

// The gauges draw the two percentages beside them, at the scale their header
// names: one core for the CPU, the container's own limit for the memory. A
// stopped container has neither, and renders the same "-" as every other
// metric column rather than an empty bar — an empty bar is a measurement.
func TestTheGaugesDrawTheMetricsBesideThem(t *testing.T) {
	m := rawModel(t)
	m = feed(t, m, ContainersListMsg{Containers: []docker.Container{
		{ID: "1", Name: "idle", State: "running", CPUPercent: 0.4, MemPercent: 0.2},
		{ID: "2", Name: "busy", State: "running", CPUPercent: 50, MemPercent: 95},
		// Two full cores: the bar saturates and only the number says so.
		{ID: "3", Name: "greedy", State: "running", CPUPercent: 200, MemPercent: 100},
		{ID: "4", Name: "stopped", State: "exited", CPUPercent: 80, MemPercent: 80},
	}})
	m = feed(t, m, testutil.Key("z")) // show every state, not just the running ones

	const (
		columnCPUGauge = columnImage + 2
		columnMemGauge = columnImage + 4
	)
	want := map[string][2]string{
		"idle":    {"[      ]", "[      ]"},
		"busy":    {"[⣿⣿⣿   ]", "[⣿⣿⣿⣿⣿⡇]"},
		"greedy":  {"[⣿⣿⣿⣿⣿⣿]", "[⣿⣿⣿⣿⣿⣿]"},
		"stopped": {"-", "-"},
	}
	for _, row := range tableRows(m) {
		expected, ok := want[row[columnName]]
		if !ok {
			t.Fatalf("unexpected row %q", row[columnName])
		}
		if got := row[columnCPUGauge]; got != expected[0] {
			t.Errorf("%s CPU gauge = %q, want %q", row[columnName], got, expected[0])
		}
		if got := row[columnMemGauge]; got != expected[1] {
			t.Errorf("%s memory gauge = %q, want %q", row[columnName], got, expected[1])
		}
	}
}

// A gauge is the first thing to go when the table runs out of room, whatever
// its position — it illustrates a number that stays behind (§3.71).
func TestTheGaugesAreTheFirstColumnsDropped(t *testing.T) {
	columns := containerColumns()

	for i, col := range columns {
		gauge := col.Title == "1 core" || col.Title == "Limit"
		if gauge && (!col.Optional || !col.DropFirst) {
			t.Errorf("column %d (%q) is a gauge but is not Optional+DropFirst", i, col.Title)
		}
		if !gauge && col.DropFirst {
			t.Errorf("column %d (%q) is not a gauge and should not be DropFirst", i, col.Title)
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
	// cycle returns to the start. Four columns do not sort: the status glyph,
	// Ports, and the two gauges — a bar sorts by the number it draws, and that
	// number's own column already offers it.
	sortable := len(containerColumns()) - 4
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
		{"mem descending", columnImage + 3, true, []string{"web", "cache", "zombie", "api"}},
		{"net rx descending", columnImage + 5, true, []string{"web", "cache", "zombie", "api"}},
		{"net tx descending", columnImage + 6, true, []string{"web", "cache", "zombie", "api"}},
		{"block rx descending", columnImage + 7, true, []string{"web", "cache", "zombie", "api"}},
		{"block tx descending", columnImage + 8, true, []string{"web", "cache", "zombie", "api"}},
		// CreatedAt is compared as a string, so an unparseable value sorts
		// after every ISO timestamp rather than being treated as unknown.
		{"created ascending", columnCreated, false, []string{"cache", "api", "web", "zombie"}},
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
	m = allStates(t, m) // every state, so the sorted order is the whole list

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
	if got := m.containerTable.Table().Columns()[columnPorts].Title; got != "Ports" {
		t.Errorf("Ports header = %q, want it bare", got)
	}
}

// The named indices are the one thing that goes wrong quietly when columns are
// reordered: the six metric columns sit between Image and these two, so an
// offset stays plausible while pointing at the wrong column.
func TestTheNamedColumnsAreWhereTheirNamesSay(t *testing.T) {
	cols := containerColumns()
	for _, tc := range []struct {
		index int
		title string
	}{
		{columnStatus, ""},
		{columnName, "Name"},
		{columnImage, "Image"},
		{columnPorts, "Ports"},
		{columnCreated, "Created"},
	} {
		if tc.index >= len(cols) {
			t.Fatalf("index %d is past the %d columns there are", tc.index, len(cols))
		}
		if got := cols[tc.index].Title; got != tc.title {
			t.Errorf("column %d is %q, want %q", tc.index, got, tc.title)
		}
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

	if !m.footer.IsSet() {
		t.Error("a failed action said nothing")
	}
	if strings.Contains(m.footer.Text(), "permission denied") {
		t.Errorf("footer = %q leaks the raw error; Rule 128 wants a short message plus a log", m.footer.Text())
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

		if !m.footer.IsSet() {
			t.Error("a failed prune said nothing")
		}
		if strings.Contains(m.footer.Text(), "daemon refused") {
			t.Errorf("footer = %q leaks the raw error", m.footer.Text())
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

// ── State filter ─────────────────────────────────────────────────────────────

// names reads the container names the table is actually showing, in order.
func names(m Model) []string {
	out := make([]string, 0, len(m.containerTable.Visible()))
	for _, c := range m.containerTable.Visible() {
		out = append(out, c.Name)
	}
	return out
}

// The default the whole change exists for: the list arrives whole, the view
// shows the running part of it, and there is no bar over it.
func TestTheViewOpensOnTheRunningContainersWithNoBar(t *testing.T) {
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})

	if got := names(m); len(got) != 1 || got[0] != "web" {
		t.Errorf("visible = %v, want only the running container", got)
	}
	if m.containerTable.FilterBar().IsVisible() {
		t.Error("the filter bar is on screen on arrival, so the view opens under a visible filter")
	}
	if len(m.containerTable.Items()) != len(containerFixtures()) {
		t.Error("the table does not hold every container, so a state cannot be filtered back in")
	}
}

// Each key stands for a group, and a lit token replaces the default rather than
// adding to it.
func TestEachStateKeyFiltersItsGroup(t *testing.T) {
	tests := []struct {
		key  string
		want []string
	}{
		{"r", []string{"web"}},           // running — the default, said out loud
		{"p", []string{"cache"}},         // paused
		{"s", []string{"api", "zombie"}}, // exited + dead
		{"t", nil},                       // no fixture is in transition
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			m := feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})
			m = feed(t, m, testutil.Key(tc.key))

			got := names(m)
			if len(got) != len(tc.want) {
				t.Fatalf("%s showed %v, want %v", tc.key, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("%s showed %v, want %v", tc.key, got, tc.want)
				}
			}
		})
	}
}

// A key that is on puts the bar up, which is the whole difference from the
// opening default: the moment the user has an opinion, it is named on screen.
func TestATokenPutsTheBarUp(t *testing.T) {
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})

	m = feed(t, m, testutil.Key("s"))
	if !m.containerTable.FilterBar().IsVisible() {
		t.Error("the bar stayed hidden with a state filter on, so nothing says the list is restricted")
	}

	m = feed(t, m, testutil.Key("s"))
	if m.containerTable.FilterBar().IsVisible() {
		t.Error("the bar survived turning the last filter off")
	}
}

// Cumulative, like the security view's severities: `r`+`p` asks for running or
// paused, which no single-value switch can express.
func TestStateFiltersAreCumulative(t *testing.T) {
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})
	m = feed(t, m, testutil.Key("r"), testutil.Key("p"))

	got := names(m)
	if len(got) != 2 || got[0] != "cache" || got[1] != "web" {
		t.Errorf("visible = %v, want the running and the paused container", got)
	}
}

// "Everything" is the four together. There is no `all` token, because it would
// be a fifth state to select alongside four real ones.
func TestTheFourStatesTogetherShowEveryContainer(t *testing.T) {
	m := rawModel(t)

	if len(m.containerTable.Visible()) != len(containerFixtures()) {
		t.Errorf("visible = %v with every state on, want every container", names(m))
	}
}

// The reset and the arrival state are one screen, not two. This is the whole
// correction: with the default lit as a token, `z` unlit it and landed
// somewhere the view never opens on.
func TestResetLandsWhereTheViewOpens(t *testing.T) {
	opened := feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})
	reset := feed(t, rawModel(t), testutil.Key("z"))

	if got, want := names(reset), names(opened); len(got) != len(want) {
		t.Fatalf("z showed %v, want the opening state %v", got, want)
	}
	if reset.containerTable.FilterBar().IsVisible() {
		t.Error("the bar survived z, so the reset is not where the view opens")
	}
	for _, label := range stateTokens {
		if reset.containerTable.IsTokenActive(label) {
			t.Errorf("%s survived z", label)
		}
	}
}

// z clears the search too, which is the half a state key cannot reach.
func TestClearingFiltersDropsTheSearch(t *testing.T) {
	m := feed(t, rawModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	m = feed(t, m, testutil.Key("enter")) // confirm, or `z` is a character
	if len(m.containerTable.Visible()) != 1 {
		t.Fatalf("the search did not narrow the table: %v", names(m))
	}

	m = feed(t, m, testutil.Key("z"))

	if q := m.containerTable.FilterBar().SearchQuery(); q != "" {
		t.Errorf("search query = %q after z, want it cleared", q)
	}
	if got := names(m); len(got) != 1 || got[0] != "web" {
		t.Errorf("visible = %v after z, want the running default", got)
	}
}

// A state that no token names must not be a container that disappears.
func TestAnUnknownStateIsStopped(t *testing.T) {
	if got := stateToken("something-docker-invented"); got != filterTokenStopped {
		t.Errorf("stateToken(unknown) = %q, want %q", got, filterTokenStopped)
	}
	for _, state := range []string{"running", "paused", "exited", "dead", "created", "restarting", "removing"} {
		if stateToken(state) == "" {
			t.Errorf("state %q maps to no token, so such a container can be hidden by every filter", state)
		}
	}
}

// The filter is local: no refetch, so a state comes back in the frame it is
// pressed rather than after a round trip to the daemon.
func TestTogglingAStateIssuesNoCommand(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, testutil.Key("s"))

	if cmd != nil {
		t.Error("toggling a state issued a command — the list already holds every container")
	}
}

// ── Refresh ──────────────────────────────────────────────────────────────────

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

// The pager command carries no quote, and that is what the Windows branch got
// wrong for its whole life.
//
// Go's exec.Command escapes an argument's inner quotes as `\"` when it builds a
// Windows command line, and cmd.exe does not understand that escaping: it reads
// the backslashes as part of the path. `more "%TEMP%\devdesk-logs.txt"` became
// `C:\C:\Users\...\devdesk-logs.txt\`, cmd refused it, and `V` came straight
// back to DevDesk with an exit status nobody rendered.
//
// The assertion is on the string rather than on the platform, because the
// hazard is not platform-specific: a quote inside a shell script handed to
// exec.Command is the trap, and the Unix branch has no more business carrying
// one.
func TestThePagerCommandCarriesNoQuote(t *testing.T) {
	cmd := logsSource{ID: "abc123", Container: "api"}.PagerCmd()

	for _, arg := range cmd.Args {
		if strings.ContainsAny(arg, `"'`) {
			t.Errorf("pager argument %q carries a quote; exec.Command escapes it and cmd.exe does not understand that", arg)
		}
	}
}

// The temp file went with the quotes, because the reason given for it was not
// true: `more` reads a pipe perfectly well — `dir | more` is its canonical use.
// Both branches are one shape now, a pipe into a pager.
func TestThePagerPipesRatherThanWritingAFile(t *testing.T) {
	cmd := logsSource{ID: "abc123", Container: "api"}.PagerCmd()
	script := cmd.Args[len(cmd.Args)-1]

	if !strings.Contains(script, "|") {
		t.Errorf("pager script %q does not pipe", script)
	}
	// `2>&1` is a redirect and stays; what must not come back is a redirect to a
	// file, which is what needed the quoting that broke the whole thing.
	if strings.Contains(strings.ToUpper(script), "TEMP") || strings.Contains(script, ".txt") {
		t.Errorf("pager script %q still writes a temp file", script)
	}
	if !strings.Contains(script, "abc123") {
		t.Errorf("pager script %q does not name the container", script)
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

	if m.footer.Text() != "Cannot inspect — invalid container ID" {
		t.Errorf("footer = %q, want inspect to have been refused", m.footer.Text())
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
	if m.footer.IsSet() {
		t.Errorf("footer = %q; an inert key should say nothing", m.footer.Text())
	}
}

func TestShellWindowFailureSurfaces(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, ShellWindowOpenedMsg{Err: errors.New("no terminal emulator")})

	if !m.footer.IsSet() {
		t.Error("a failed terminal launch said nothing")
	}
	if strings.Contains(m.footer.Text(), "emulator") {
		t.Errorf("footer = %q leaks the raw error", m.footer.Text())
	}
}

func TestShellWindowSuccessSaysNothing(t *testing.T) {
	m := feed(t, loadedModel(t), ShellWindowOpenedMsg{})

	if m.footer.IsSet() {
		t.Errorf("footer = %q after a successful launch", m.footer.Text())
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

		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		if got, want := m.containerTable.RenderedWidth(), width-2; got != want {
			t.Errorf("at width %d the line spans %d, want %d so the selected row reaches the border", width, got, want)
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
	if m.footer.Text() != busyMessage {
		t.Errorf("footer = %q, want the busy message", m.footer.Text())
	}
	if cmd == nil {
		t.Error("a footer message was set with no timer to clear it")
	}
}

// The spinner on the row only turns while a tick is being scheduled, and Init's
// tick stops as soon as the list has arrived — a loaded, idle view schedules
// nothing (TestSpinnerTicksOnlyWhileLoading). So an action begun after that sat
// on frame zero for the whole ten second grace period of a `docker stop`, which
// is exactly the freeze the spinner exists to disprove.
//
// The command is stubbed rather than real: what is asserted is what startAction
// batches alongside it, and testutil.Msgs runs everything it is handed — the
// real one shells out to docker.
func TestAnActionRestartsTheSpinner(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down")) // web, running
	if _, cmd := step(t, m, spinner.TickMsg{}); cmd != nil {
		t.Fatal("the idle model was still ticking, so this test would prove nothing")
	}

	c := docker.Container{ID: webID, Name: "web"}
	_, cmd := m.startAction(&c, "Stopping", func() tea.Msg { return nil })

	if _, ok := testutil.MsgOf[spinner.TickMsg](cmd); !ok {
		t.Error("the action scheduled no tick, so its row spins on a frozen frame")
	}
}

// And once running, the tick has to go on being scheduled: the list is loaded
// and on screen the whole time a container stops, so the view's own loading
// flag says nothing about it.
func TestTheSpinnerKeepsTickingWhileAnActionRuns(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("down"), testutil.Key("down"))
	m, _ = pressK(t, m, choiceStop)

	_, cmd := step(t, m, spinner.TickMsg{})

	if cmd == nil {
		t.Error("the spinner stopped being scheduled while an action was running")
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
	m.footer.Error("Something went wrong")

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
