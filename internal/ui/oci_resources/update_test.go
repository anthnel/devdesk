package ociresources

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Construction and loading ─────────────────────────────────────────────────

func TestNewOpensOnImagesSortedByName(t *testing.T) {
	m := New(testConfig())

	if m.activeTab != tabImages {
		t.Errorf("activeTab = %d on a new model, want the images tab", m.activeTab)
	}
	if column, desc := m.imageTable.SortState(); column != imageColumnName || desc {
		t.Errorf("sort = (column %d, desc=%v), want name ascending", column, desc)
	}
	if !m.loading || !m.loadingNets || !m.loadingVols {
		t.Error("a new model is not marked loading on every tab")
	}
	// The registries come from config rather than from Docker, so they are
	// present before any command runs.
	if len(m.registries) != 2 {
		t.Errorf("registries = %v, want the configured two", m.registries)
	}
}

func TestInitLoadsEveryTab(t *testing.T) {
	if cmd := New(testConfig()).Init(); cmd == nil {
		t.Fatal("Init() issued no commands")
	}
}

func TestListsFillTheirTables(t *testing.T) {
	m := loadedModel(t)

	if m.loading || m.loadingNets || m.loadingVols {
		t.Error("still loading after every list arrived")
	}
	if got := cells(m.imageTable.Table().Rows(), 1); len(got) != 4 {
		t.Errorf("the image table holds %v", got)
	}
	if got := cells(m.networkTable.Table().Rows(), 1); !equal(got, []string{"bridge", "devdesk", "overlay-prod"}) {
		t.Errorf("the network table holds %v", got)
	}
	if got := cells(m.volumeTable.Table().Rows(), 0); !equal(got, []string{"pgdata", "redis"}) {
		t.Errorf("the volume table holds %v", got)
	}
}

// A Docker failure has to clear the spinner as well as report, or the tab shows
// a spinner forever.
func TestListFailuresStopTheSpinner(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		busy func(Model) bool
	}{
		{"images", ImagesListMsg{Err: errors.New("docker daemon not running")}, func(m Model) bool { return m.loading }},
		{"networks", NetworksListMsg{Err: errors.New("docker daemon not running")}, func(m Model) bool { return m.loadingNets }},
		{"volumes", VolumesListMsg{Err: errors.New("docker daemon not running")}, func(m Model) bool { return m.loadingVols }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := feed(t, newTestModel(t), tt.msg)

			if tt.busy(m) {
				t.Error("still loading after the failure")
			}
			if m.errorMsg == "" {
				t.Error("the failure was not reported")
			}
		})
	}
}

// An untagged image is "<repository>" rather than "repository:<none>", which is
// also the key the scan cache is looked up under.
func TestUntaggedImagesDropTheTagSuffix(t *testing.T) {
	m := loadedModel(t)

	names := cells(m.imageTable.Table().Rows(), 1)
	for _, name := range names {
		if strings.Contains(name, "<none>") {
			t.Errorf("the table shows a raw <none> tag: %v", names)
		}
	}
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

func TestTabCyclesForwardAndBack(t *testing.T) {
	m := loadedModel(t)

	for _, want := range []ociTab{tabNetworks, tabVolumes, tabRegistries, tabImages} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("tab landed on %d, want %d", m.activeTab, want)
		}
	}

	m = feed(t, m, testutil.Key("shift+tab"))
	if m.activeTab != tabRegistries {
		t.Errorf("shift+tab landed on %d, want it to wrap backwards", m.activeTab)
	}
}

// Only the active tab's table takes the keyboard, or arrow keys would move two
// cursors at once.
func TestOnlyTheActiveTablesIsFocused(t *testing.T) {
	m := loadedModel(t)
	if !m.imageTable.Table().Focused() {
		t.Error("the image table is not focused on the images tab")
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.imageTable.Table().Focused() {
		t.Error("the image table kept focus after switching away")
	}
	if !m.networkTable.Table().Focused() {
		t.Error("the network table did not take focus")
	}
}

// ── Images: sorting and filtering ────────────────────────────────────────────

func TestSortCyclesDirectionThenColumn(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("."))
	if column, desc := m.imageTable.SortState(); column != imageColumnName || !desc {
		t.Errorf("after one '.', sort = (column %d, desc=%v), want the same column reversed", column, desc)
	}

	m = feed(t, m, testutil.Key("."))
	if column, desc := m.imageTable.SortState(); column != imageColumnName+1 || desc {
		t.Errorf("after two '.', sort = (column %d, desc=%v), want Disk Usage ascending", column, desc)
	}
}

func TestSortReordersTheImages(t *testing.T) {
	m := loadedModel(t)

	if got := cells(m.imageTable.Table().Rows(), 1); !equal(got, []string{"api:v1", "cache:v2", "orphan", "web:v3"}) {
		t.Errorf("names ascending = %v", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := cells(m.imageTable.Table().Rows(), 1); !equal(got, []string{"web:v3", "orphan", "cache:v2", "api:v1"}) {
		t.Errorf("names descending = %v", got)
	}
}

func TestFilterNarrowsTheImages(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("ca")...)

	if got := cells(m.imageTable.Table().Rows(), 1); !equal(got, []string{"cache:v2"}) {
		t.Errorf("rows while filtering on \"ca\" = %v", got)
	}
}

// The Name column shows the alias substituted in, so the filter has to match
// it: searching for what is on screen is the first thing anyone tries. The raw
// repository stays searchable, which is all the filter matched before.
func TestTheFilterMatchesBothTheAliasAndTheRawName(t *testing.T) {
	m := feed(t, newTestModel(t), ImagesListMsg{Images: []docker.Image{
		{ID: "eee5555", Repository: "registry.example.com/team/svc", Tag: "v9"},
	}})

	shown := cells(m.imageTable.Table().Rows(), 1)
	if !equal(shown, []string{"prod/team/svc:v9"}) {
		t.Fatalf("the Name column = %v, want the configured alias substituted in", shown)
	}

	for _, query := range []string{"prod", "registry.example.com", "svc", "v9"} {
		filtered := feed(t, feed(t, m, testutil.Key("/")), testutil.Type(query)...)
		if got := cells(filtered.imageTable.Table().Rows(), 1); len(got) != 1 {
			t.Errorf("filtering on %q left %v, want the one image", query, got)
		}
	}
}

// While the search box has focus it owns every key, so view shortcuts must not
// fire from inside it.
func TestSearchModeSwallowsViewShortcuts(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))

	m = feed(t, m, testutil.Key("p"))

	if m.confirmModal != nil {
		t.Error("'p' opened the prune confirmation while the search box had focus")
	}
	if !strings.Contains(m.imageTable.FilterBar().SearchQuery(), "p") {
		t.Errorf("'p' did not reach the search box; query = %q", m.imageTable.FilterBar().SearchQuery())
	}
}

// ── Images: scanning ─────────────────────────────────────────────────────────

// A scan in flight marks its row, so the user can tell a stale count from one
// being recomputed.
func TestScanProgressMarksTheRow(t *testing.T) {
	m := feed(t, loadedModel(t), ImageScanStartingMsg{ImageName: "web:v3"})

	if !m.scanningImages["web:v3"] {
		t.Error("the image is not marked as scanning")
	}

	m = feed(t, m, ImageScanFinishedMsg{
		ImageName: "web:v3",
		Entry:     cache.ImageScanEntry{Critical: 1, ScannedAt: at(3)},
	})

	if m.scanningImages["web:v3"] {
		t.Error("the image is still marked as scanning after it finished")
	}
	if got := m.scanCache["web:v3"]; got.Critical != 1 {
		t.Errorf("the cache entry = %+v, want the scan result", got)
	}
}

// A failed scan is distinct from an unscanned one: the row says the scan was
// tried and did not work, rather than silently staying blank.
func TestAFailedScanIsRemembered(t *testing.T) {
	m := feed(t, loadedModel(t),
		ImageScanStartingMsg{ImageName: "web:v3"},
		ImageScanFinishedMsg{ImageName: "web:v3", Err: errors.New("trivy: exit status 1")},
	)

	if m.scanningImages["web:v3"] {
		t.Error("a failed scan left the row marked as scanning")
	}
	if !m.failedScans["web:v3"] {
		t.Error("the failure was not recorded")
	}
	if _, cached := m.scanCache["web:v3"]; cached {
		t.Error("a failed scan wrote a cache entry")
	}
}

// Rule 126: 'A' scans only what has never been scanned, so pressing it twice
// does not redo work. The command it returns runs Trivy, so the assertion is on
// the flag it sets — and on the branch where there is nothing left to do.
func TestScanAllUnscannedSkipsWhatIsCached(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("A"))

	if !m.scanning {
		t.Error("'A' did not start a batch scan while two images were unscanned")
	}
	if cmd == nil {
		t.Error("'A' issued no command")
	}
}

// With every image cached there is nothing to do, and saying so beats starting
// a scan that immediately finishes.
func TestScanAllUnscannedWithNothingLeft(t *testing.T) {
	m := loadedModel(t)
	m.scanCache = map[string]cache.ImageScanEntry{
		"api:v1": {ScannedAt: at(1)}, "cache:v2": {ScannedAt: at(2)},
		"web:v3": {ScannedAt: at(3)}, "orphan": {ScannedAt: at(4)},
	}

	m, _ = step(t, m, testutil.Key("A"))

	if m.scanning {
		t.Error("'A' started a batch scan with nothing left to scan")
	}
	if m.errorMsg == "" {
		t.Error("'A' said nothing when there was nothing to do")
	}
}

// A batch already running must not be restarted on a second press.
func TestScanAllIgnoredWhileABatchRuns(t *testing.T) {
	m := loadedModel(t)
	m.scanning = true

	_, cmd := step(t, m, testutil.Key("A"))

	if cmd != nil {
		t.Errorf("'A' issued %T while a batch was already running", testutil.Msg(cmd))
	}
}

// Rule 126: ctrl+a purges the cache first, so nothing is skipped and no stale
// entry survives an interrupted run.
func TestScanAllPurgesTheCacheFirst(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+a"))

	if len(m.scanCache) != 0 {
		t.Errorf("the in-memory cache still holds %v", m.scanCache)
	}
	if cmd == nil {
		t.Error("ctrl+a issued no commands")
	}
}

// Enter opens the cached details, which only the router can do — it owns the
// security view.
func TestEnterAsksForTheCachedDetails(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, testutil.Key("enter"))

	msg, ok := testutil.MsgOf[ScanDetailsRequestMsg](cmd)
	if !ok {
		t.Fatalf("enter produced %T, want a details request", testutil.Msg(cmd))
	}
	if msg.ImageName != "api:v1" {
		t.Errorf("enter asked for %q, want the highlighted row", msg.ImageName)
	}
}

// An image being scanned has no stable state to act on, so the destructive and
// configuring keys are inert until it finishes.
func TestKeysAreInertWhileTheSelectedImageScans(t *testing.T) {
	m := feed(t, loadedModel(t), ImageScanStartingMsg{ImageName: "api:v1"})

	for _, key := range []string{"ctrl+e", "ctrl+d", "ctrl+s"} {
		next := feed(t, m, testutil.Key(key))
		if next.launchForm != nil || next.confirmModal != nil {
			t.Errorf("%q acted on an image being scanned", key)
		}
	}
}

// ── Images: destructive actions ──────────────────────────────────────────────

func TestDeleteAsksFirstAndNamesTheImage(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	if m.confirmModal == nil {
		t.Fatal("ctrl+d removed an image without asking")
	}
	if view := m.confirmModal.View(); !strings.Contains(view, "api:v1") {
		t.Errorf("the confirmation does not name the image:\n%s", view)
	}
}

func TestPruneAsksFirst(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("p"))

	if m.confirmModal == nil {
		t.Fatal("'p' pruned without asking")
	}
}

func TestDecliningAConfirmationActsOnNothing(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if cmd != nil {
		t.Errorf("declining issued %T", testutil.Msg(cmd))
	}
	if m.confirmModal != nil || m.pendingAction != "" {
		t.Errorf("declining left modal=%v action=%q", m.confirmModal, m.pendingAction)
	}
}

func TestConfirmingADeleteIssuesIt(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

	if cmd == nil {
		t.Error("confirming a delete issued no command")
	}
	if m.confirmModal != nil {
		t.Error("the confirmation stayed open after being answered")
	}
}

// A refused removal is the common case — the image is in use — so it has to be
// reported rather than silently leaving the row in place.
func TestImageActionFailureIsReported(t *testing.T) {
	m := feed(t, loadedModel(t), ImageActionMsg{
		Action: "remove", ID: "aaa1111",
		Err: errors.New("image is being used by running container"),
	})

	if m.errorMsg == "" {
		t.Error("a failed removal reported nothing")
	}
}

// ── Networks and volumes ─────────────────────────────────────────────────────

func TestNetworkAndVolumeActionsAskFirst(t *testing.T) {
	tests := []struct {
		name string
		tab  int
		key  string
	}{
		{"delete a network", 1, "ctrl+d"},
		{"prune networks", 1, "p"},
		{"delete a volume", 2, "ctrl+d"},
		{"prune volumes", 2, "p"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)
			for range tt.tab {
				m = feed(t, m, testutil.Key("tab"))
			}

			m = feed(t, m, testutil.Key(tt.key))

			if m.confirmModal == nil {
				t.Errorf("%q acted without asking", tt.key)
			}
		})
	}
}

// Docker refuses to remove the networks it created, which is the common failure
// here.
func TestNetworkActionFailureIsReported(t *testing.T) {
	m := feed(t, loadedModel(t), NetworkActionMsg{
		Action: "remove", Err: errors.New("bridge is a pre-defined network and cannot be removed"),
	})

	if m.errorMsg == "" {
		t.Error("a failed network action reported nothing")
	}
}

// A successful prune refetches rather than reporting: the reclaimed space is
// visible in the list that comes back, and a message would just be noise.
// A failure has to say so, since nothing visible changes.
func TestPruneReportsOnlyFailures(t *testing.T) {
	tests := []struct {
		ok     tea.Msg
		failed tea.Msg
	}{
		{PruneCompleteMsg{Output: "Total reclaimed space: 1.2GB"}, PruneCompleteMsg{Err: errors.New("no")}},
		{NetworkPruneCompleteMsg{Output: "Deleted Networks: devdesk"}, NetworkPruneCompleteMsg{Err: errors.New("no")}},
		{VolumePruneCompleteMsg{Output: "Total reclaimed space: 300MB"}, VolumePruneCompleteMsg{Err: errors.New("no")}},
	}

	for _, tt := range tests {
		m, cmd := step(t, loadedModel(t), tt.ok)
		if m.errorMsg != "" {
			t.Errorf("%T reported %q on success", tt.ok, m.errorMsg)
		}
		if cmd == nil {
			t.Errorf("%T did not refetch", tt.ok)
		}

		failed := feed(t, loadedModel(t), tt.failed)
		if failed.errorMsg == "" {
			t.Errorf("%T reported nothing", tt.failed)
		}
	}
}

// ── Registries ───────────────────────────────────────────────────────────────

func TestRegistryTabShowsTheConfiguredRegistries(t *testing.T) {
	m := loadedModel(t)
	for range 3 {
		m = feed(t, m, testutil.Key("tab"))
	}

	if got := cells(m.registryTable.Table().Rows(), 1); !equal(got, []string{"registry.example.com", "docker.io"}) {
		t.Errorf("the registry table holds %v", got)
	}
}

// The login column is the point of the tab: it says whether a docker pull would
// work right now.
func TestLoginStatusReachesTheTable(t *testing.T) {
	m := loadedModel(t)
	for range 3 {
		m = feed(t, m, testutil.Key("tab"))
	}

	m = feed(t, m, RegistryLoginStatusMsg{Status: map[string]bool{"registry.example.com": true}})

	if !m.registryLoginStatus["registry.example.com"] {
		t.Error("the login status was not kept")
	}
	if m.registryLoginStatus["docker.io"] {
		t.Error("an unlisted registry was marked logged in")
	}
}

// A login updates the status optimistically and then re-checks against disk,
// because a credential helper can accept the login and still not persist it.
// A failure marks the registry logged out rather than leaving the old state.
func TestRegistryLoginUpdatesTheStatus(t *testing.T) {
	m, cmd := step(t, loadedModel(t), RegistryLoginCompleteMsg{RegistryURL: "registry.example.com"})

	if !m.registryLoginStatus["registry.example.com"] {
		t.Error("a successful login did not mark the registry logged in")
	}
	if cmd == nil {
		t.Error("a successful login did not re-check the status from disk")
	}

	failed := feed(t, m, RegistryLoginCompleteMsg{
		RegistryURL: "registry.example.com", Err: errors.New("unauthorized"),
	})
	if failed.registryLoginStatus["registry.example.com"] {
		t.Error("a failed login left the registry marked logged in")
	}
	if failed.errorMsg == "" {
		t.Error("a failed login reported nothing")
	}
}

// ── Footer messages ──────────────────────────────────────────────────────────

// Rule 128: footer messages expire after three seconds.
func TestFooterMessagesExpire(t *testing.T) {
	tests := []tea.Msg{
		ImagesListMsg{Err: errors.New("docker daemon not running")},
		NetworksListMsg{Err: errors.New("docker daemon not running")},
		VolumesListMsg{Err: errors.New("docker daemon not running")},
		NetworkActionMsg{Action: "remove", Err: errors.New("pre-defined network")},
		PruneCompleteMsg{Err: errors.New("no such image")},
	}

	for _, msg := range tests {
		m, cmd := step(t, loadedModel(t), msg)

		if m.errorMsg == "" {
			t.Errorf("%T reported nothing", msg)
			continue
		}
		if cmd == nil {
			t.Errorf("%T scheduled no clear timer", msg)
			continue
		}

		cleared := feed(t, m, clearInfoMsgMsg{})
		if cleared.infoMsg != "" || cleared.errorMsg != "" {
			t.Errorf("%T left info=%q error=%q after the timer", msg, cleared.infoMsg, cleared.errorMsg)
		}
	}
}

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode tells the router to leave esc alone. Every overlay that answers
// esc itself has to claim it.
func TestInEditModeCoversEveryOverlay(t *testing.T) {
	if loadedModel(t).InEditMode() {
		t.Error("InEditMode() is true on the plain table")
	}

	searching := feed(t, loadedModel(t), testutil.Key("/"))
	if !searching.InEditMode() {
		t.Error("InEditMode() is false while the search box has focus")
	}

	confirming := feed(t, loadedModel(t), testutil.Key("ctrl+d"))
	if !confirming.InEditMode() {
		t.Error("InEditMode() is false with a confirmation open")
	}
}

// ── The datatable migration (§2 step 2) ──────────────────────────────────────

// Rule 116: the column widths sum to exactly the space available, so the
// selected row reaches the right viewport border. The volumes table used to
// clamp its last column at 20 *after* the remainder was computed, which
// overflowed on any terminal narrow enough — the component's solver is what
// removes the whole class.
func TestTheResourceTablesHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120, 180, 240} {
		m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: width, Height: 30})

		for name, cols := range map[string][]table.Column{
			"networks": m.networkTable.Table().Columns(),
			"volumes":  m.volumeTable.Table().Columns(),
		} {
			total := 0
			for _, c := range cols {
				total += c.Width
			}
			// viewport borders (2) + bubbles/table's per-cell padding (2 each)
			want := width - 2 - len(cols)*2
			if total != want {
				t.Errorf("%s at width %d: the columns sum to %d, want %d", name, width, total, want)
			}
		}
	}
}

// The cursor is resolved against the slice the rows were built from, so the
// action cannot land on a different object than the one highlighted.
func TestTheSelectedResourceIsTheHighlightedRow(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab")) // Networks

	m = feed(t, m, testutil.Key("down"))

	net := m.getSelectedNetwork()
	if net == nil {
		t.Fatal("nothing selected on a filled table")
	}
	if got := m.networkTable.Table().Rows()[m.networkTable.Cursor()][1]; got != net.Name {
		t.Errorf("the highlighted row shows %q while the action would take %q", got, net.Name)
	}
}

// bubbles/table leaves the cursor where it was when rows are replaced, so a
// refresh that returns fewer networks used to strand it past the end.
func TestARefreshWithFewerResourcesClampsTheCursor(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab"))
	m = feed(t, m, testutil.Key("G")) // last row

	m = feed(t, m, NetworksListMsg{Networks: networkFixtures()[:1]})

	net := m.getSelectedNetwork()
	if net == nil {
		t.Fatal("the cursor was left pointing past the end of the list")
	}
	if net.Name != "bridge" {
		t.Errorf("selected %q, want the one network that is left", net.Name)
	}
}

// Neither tab searches today, so `/` must stay inert rather than opening a bar
// this view's footer does not render for them.
func TestSlashDoesNothingOnTheResourceTabs(t *testing.T) {
	for _, tab := range []int{1, 2} { // Networks, Volumes
		m := loadedModel(t)
		for range tab {
			m = feed(t, m, testutil.Key("tab"))
		}

		m = feed(t, m, testutil.Key("/"))

		if m.InEditMode() {
			t.Errorf("tab %d: '/' opened a search on a table with nothing to search", tab)
		}
	}
}

// ── A scan asked for from outside ────────────────────────────────────────────

// The router sends this when a cached scan's stored result has gone missing:
// the row is still in the list, and the scan that replaces it belongs here, next
// to the cache it writes. The name is both the cache key and the scan target.
func TestAScanRequestRescansTheNamedImage(t *testing.T) {
	next, cmd := step(t, loadedModel(t), ScanRequestMsg{ImageName: "web:v3"})

	if !next.scanning {
		t.Error("the view does not report a scan running")
	}
	starting, ok := testutil.MsgOf[ImageScanStartingMsg](cmd)
	if !ok {
		t.Fatal("the request started no scan")
	}
	if starting.ImageName != "web:v3" {
		t.Errorf("scanning %q, want the requested image", starting.ImageName)
	}
}

func TestAnEmptyImageScanRequestDoesNothing(t *testing.T) {
	if _, cmd := step(t, loadedModel(t), ScanRequestMsg{}); cmd != nil {
		t.Error("an empty request started a scan")
	}
}

// A second request for an image already being scanned is dropped rather than
// queued, as ctrl+s on the row is.
func TestAScanRequestForARunningImageScanIsRefused(t *testing.T) {
	m := loadedModel(t)
	m.scanningImages["web:v3"] = true

	if _, cmd := step(t, m, ScanRequestMsg{ImageName: "web:v3"}); cmd != nil {
		t.Error("a duplicate request started a second scan")
	}
}
