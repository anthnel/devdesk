package ociresources

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
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
			if !m.footer.IsSet() {
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

	m = feed(t, m, testutil.Key(keymap.Prune))

	if m.confirmModal != nil {
		t.Error("'p' opened the prune confirmation while the search box had focus")
	}
	if !strings.Contains(m.imageTable.FilterBar().SearchQuery(), keymap.Prune) {
		t.Errorf("'p' did not reach the search box; query = %q", m.imageTable.FilterBar().SearchQuery())
	}
}

// ── Images: scanning ─────────────────────────────────────────────────────────

// A scan in flight marks its row, so the user can tell a stale count from one
// being recomputed. The mark comes from the registry snapshot now, so this is
// the view's half: it reads the snapshot, and it caches the result.
func TestScanProgressMarksTheRow(t *testing.T) {
	m := scanning(t, loadedModel(t), "web:v3")

	if !m.scanningImage("web:v3") {
		t.Error("the image does not read as scanning")
	}

	m = withJobs(t, m, settledScanRun("web:v3"))
	m = feed(t, m, ImageScanFinishedMsg{
		ImageName: "web:v3",
		Entry:     cache.ImageScanEntry{Critical: 1, ScannedAt: at(3)},
	})

	if m.scanningImage("web:v3") {
		t.Error("the image still reads as scanning after it finished")
	}
	if got := m.scanCache["web:v3"]; got.Critical != 1 {
		t.Errorf("the cache entry = %+v, want the scan result", got)
	}
}

// The registry is one bookkeeping, so a scan started in another view marks the
// row here. The two would otherwise write the same cache entry — which is what
// the per-view map allowed.
func TestAScanStartedElsewhereMarksTheRow(t *testing.T) {
	fromSecurity := jobs.NewRun(jobs.KindScan, command.ViewSecurity, "default", "inventory", "web:v3")
	fromSecurity.Items[0].State = jobs.ItemRunning

	m := withJobs(t, loadedModel(t), fromSecurity)

	if !m.scanningImage("web:v3") {
		t.Error("a scan started from the security view is invisible here")
	}
	if !m.anyScanRunning() {
		t.Error("the view reports itself idle while a scan runs on one of its images")
	}
}

// A failed scan is distinct from an unscanned one: the row says the scan was
// tried and did not work, rather than silently staying blank.
func TestAFailedScanIsRemembered(t *testing.T) {
	m := scanning(t, loadedModel(t), "web:v3")
	m = withJobs(t, m, settledScanRun("web:v3"))
	m = feed(t, m, ImageScanFinishedMsg{ImageName: "web:v3", Err: errors.New("trivy: exit status 1")})

	if m.scanningImage("web:v3") {
		t.Error("a failed scan left the row reading as scanning")
	}
	if !m.failedScans["web:v3"] {
		t.Error("the failure was not recorded")
	}
	if _, cached := m.scanCache["web:v3"]; cached {
		t.Error("a failed scan wrote a cache entry")
	}
}

// Rule 126: A with the purge unchecked scans only what has never been scanned,
// so pressing it twice does not redo work. The assertion is on the run it asks
// the router to register — reading the command runs it, and what it registers is
// exactly the list.
func TestScanAllUnscannedSkipsWhatIsCached(t *testing.T) {
	m := loadedModel(t)

	_, cmd := scanAll(t, m, false)

	run := wantScanRun(t, cmd)
	if len(run.Items) == 0 {
		t.Error("'A' did not queue anything while images were unscanned")
	}
	for _, item := range run.Items {
		if item.State != jobs.ItemQueued {
			t.Errorf("%s starts as %q, want every target queued (D6)", item.Target, item.State)
		}
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

	m, cmd := scanAll(t, m, false)

	if _, started := startedScanRun(cmd); started {
		t.Error("'A' registered a batch with nothing left to scan")
	}
	if !m.footer.IsSet() {
		t.Error("'A' said nothing when there was nothing to do")
	}
}

// A batch already running must not be restarted on a second press.
func TestScanAllIgnoredWhileABatchRuns(t *testing.T) {
	// This test drains the Cmd, and the footer timer inside it really sleeps.
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := scanning(t, loadedModel(t), "web:v3")

	m, cmd := step(t, m, testutil.Key(keymap.ScanAll))

	// Refused before the question is even put: there is nothing to confirm.
	if m.scanAllModal != nil {
		t.Error("'A' opened its confirmation while a batch was already running")
	}
	if testutil.Msg(cmd) == nil {
		t.Error("the refusal set a footer message with no timer to clear it")
	}
}

// Rule 126: A with the purge checked clears the cache first, so nothing is
// skipped and no stale entry survives an interrupted run. It was ctrl+a, a key
// that differed from A by the modifier alone with nothing saying which one
// destroyed data (§3.26).
func TestScanAllPurgesTheCacheFirst(t *testing.T) {
	m := loadedModel(t)

	m, cmd := scanAll(t, m, true)

	if len(m.scanCache) != 0 {
		t.Errorf("the in-memory cache still holds %v", m.scanCache)
	}
	if run := wantScanRun(t, cmd); len(run.Items) == 0 {
		t.Error("the purging scan queued nothing")
	}
}

// The purge is a deliberate gesture: A alone asks, and declining does nothing.
func TestDecliningTheScanAllConfirmationDoesNothing(t *testing.T) {
	m := loadedModel(t)

	m, _ = step(t, m, testutil.Key(keymap.ScanAll))
	_, cmd := step(t, m, sharedcomponents.OptionConfirmModalNoMsg{})

	if _, started := startedScanRun(cmd); started {
		t.Error("declining still registered a scan")
	}
	if cmd != nil {
		t.Errorf("declining issued %T", testutil.Msg(cmd))
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
	m := scanning(t, loadedModel(t), "api:v1")

	for _, key := range []string{keymap.New, keymap.Delete, keymap.Scan} {
		next := feed(t, m, testutil.Key(key))
		if next.launchForm != nil || next.confirmModal != nil {
			t.Errorf("%q acted on an image being scanned", key)
		}
	}
}

// ── Images: destructive actions ──────────────────────────────────────────────

func TestDeleteAsksFirstAndNamesTheImage(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

	if m.confirmModal == nil {
		t.Fatal("ctrl+d removed an image without asking")
	}
	if view := m.confirmModal.View(); !strings.Contains(view, "api:v1") {
		t.Errorf("the confirmation does not name the image:\n%s", view)
	}
}

func TestPruneAsksFirst(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Prune))

	if m.confirmModal == nil {
		t.Fatal("'p' pruned without asking")
	}
}

func TestDecliningAConfirmationActsOnNothing(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if cmd != nil {
		t.Errorf("declining issued %T", testutil.Msg(cmd))
	}
	if m.confirmModal != nil || m.pendingAction != "" {
		t.Errorf("declining left modal=%v action=%q", m.confirmModal, m.pendingAction)
	}
}

func TestConfirmingADeleteIssuesIt(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

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

	if !m.footer.IsSet() {
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
		{"delete a network", 1, keymap.Delete},
		{"prune networks", 1, keymap.Prune},
		{"delete a volume", 2, keymap.Delete},
		{"prune volumes", 2, keymap.Prune},
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

	if !m.footer.IsSet() {
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
		if m.footer.IsSet() {
			t.Errorf("%T reported %q on success", tt.ok, m.footer.Text())
		}
		if cmd == nil {
			t.Errorf("%T did not refetch", tt.ok)
		}

		failed := feed(t, loadedModel(t), tt.failed)
		if !failed.footer.IsSet() {
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

// The URL column shows the entry's *address* — the host with its repo prefix —
// because that is what tells one line from another once several proxies are
// declared on one instance, and it is exactly the head of the pull reference
// (§3.18). Without it a group's members are the same host repeated.
func TestTheRegistryTabShowsTheAddressAndNotTheBareHost(t *testing.T) {
	cfg := testConfig()
	cfg.Registry.Registries = []config.RegistryItem{
		{URL: "nexus.example.com", Alias: "dhi", Slug: "dhi", AuthMode: config.AuthAnonymous, RepoPrefix: "dhi-io-proxy"},
		{URL: "nexus.example.com", Alias: "quay", Slug: "quay", AuthMode: config.AuthAnonymous, RepoPrefix: "quay-io-proxy"},
		{URL: "docker.io", Alias: "hub", Slug: "hub", AuthMode: config.AuthAnonymous},
	}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	for range 3 {
		m = feed(t, m, testutil.Key("tab"))
	}

	want := []string{"nexus.example.com/dhi-io-proxy", "nexus.example.com/quay-io-proxy", "docker.io"}
	if got := cells(m.registryTable.Table().Rows(), 1); !equal(got, want) {
		t.Errorf("the registry table holds %v, want %v", got, want)
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
	if !failed.footer.IsSet() {
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

		if !m.footer.IsSet() {
			t.Errorf("%T reported nothing", msg)
			continue
		}
		if cmd == nil {
			t.Errorf("%T scheduled no clear timer", msg)
			continue
		}

		cleared := feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})
		if cleared.footer.IsSet() {
			t.Errorf("%T left %q after the timer", msg, cleared.footer.Text())
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

	confirming := feed(t, loadedModel(t), testutil.Key(keymap.Delete))
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

		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		for name, span := range map[string]int{
			"networks": m.networkTable.RenderedWidth(),
			"volumes":  m.volumeTable.RenderedWidth(),
		} {
			if want := width - 2; span != want {
				t.Errorf("%s at width %d: the line spans %d, want %d", name, width, span, want)
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
	_, cmd := step(t, loadedModel(t), ScanRequestMsg{ImageName: "web:v3"})

	run := wantScanRun(t, cmd)
	if len(run.Items) != 1 || run.Items[0].Target != "web:v3" {
		t.Errorf("run targets = %+v, want the requested image alone", run.Items)
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
	m = scanning(t, m, "web:v3")

	if _, cmd := step(t, m, ScanRequestMsg{ImageName: "web:v3"}); cmd != nil {
		t.Error("a duplicate request started a second scan")
	}
}
