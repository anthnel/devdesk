package dashboard

import (
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/forge"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The dashboard's Cmds run status checks, hit the GitLab API, shell out to
// docker and du, and open a browser. None of them is executed here: the tests
// feed the result messages directly and assert on the model and its rendering.

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Forge.URL = "https://gitlab.example.com"
	cfg.App.WorkspacesDir = "~/workspaces"
	cfg.Status.RefreshInterval = 30
	return cfg
}

func newTestModel(t *testing.T) (Model, *shared.State) {
	t.Helper()
	state := &shared.State{ServiceStatus: shared.ServiceStatusUnknown}
	m := New(testConfig(), state)
	return feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}), state
}

// authenticatedModel returns a model whose shared state carries a GitLab
// session, which is what unlocks the m and i shortcuts.
func authenticatedModel(t *testing.T) (Model, *shared.State) {
	t.Helper()
	m, state := newTestModel(t)
	state.IsAuthenticated = true
	state.CurrentUser = forge.User{ID: "7", Username: "anthoni"}
	// The deep links are the forge's now, so a session means a backend too.
	state.Forge = gitlabforge.NewWithClient(nil, "https://gitlab.example.com")
	return m, state
}

// loadedModel returns a model that has absorbed one of every result message.
func loadedModel(t *testing.T) (Model, *shared.State) {
	t.Helper()
	m, state := authenticatedModel(t)
	m = feed(t, m,
		StatusCheckMsg{Result: status.MonitorResult{Components: componentFixtures(), Timestamp: time.Now()}},
		ForgeStatsMsg{Stats: forge.DashboardStats{AssignedChangeRequests: forge.Count(3), ReviewChangeRequests: forge.Count(2), AssignedIssues: forge.Count(5), Repositories: forge.Count(12), Namespaces: forge.Count(4)}},
		DockerStatsMsg{Stats: shared.DockerStats{Available: true, Running: 2, Stopped: 1, Paused: 1}},
		OCIStatsMsg{Stats: shared.OCIStats{Available: true, ImagesCount: 8, ImagesSize: "1.2GB", ContainersCount: 4, ContainersSize: "300MB", VolumesCount: 2, VolumesSize: "50MB", NetworksCount: 3}},
		WorkspaceStatsMsg{Count: 6},
		DiskUsageMsg{Workspaces: metrics.DiskUsage{Path: "~/workspaces", Free: 210 << 30, Used: 290 << 30, Total: 500 << 30, UsedPercent: 58, OK: true}},
		WorkspaceSizeMsg{Size: metrics.TreeSize{Path: "~/workspaces", Bytes: 12 << 30, OK: true}},
		HostSampleMsg{Sample: metrics.HostSample{CPUPercent: 9.2, MemPercent: 92, MemUsed: 31 << 30, MemTotal: 33 << 30, OK: true}},
		DockerMetricsMsg{Aggregate: docker.Aggregate{Available: true, Running: 2, CPUPercent: 3.5, MemPercent: 12}},
		ToolsDetectedMsg{Tools: toolFixtures()},
	)
	return m, state
}

// componentFixtures mixes services and certificates, and every status, so the
// section counters have something to disagree about.
func componentFixtures() []status.ComponentStatus {
	return []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK},
		{Name: "api", Type: status.TypeHTTP, Status: status.StatusDown},
		{Name: "dns", Type: status.TypeDNS, Status: status.StatusWarning},
		{Name: "cert-ok", Type: status.TypeSSL, Status: status.StatusOK, SSLDaysLeft: intPtr(200)},
		{Name: "cert-expired", Type: status.TypeSSL, Status: status.StatusError, SSLDaysLeft: intPtr(-3)},
	}
}

func intPtr(n int) *int { return &n }

func toolFixtures() []shared.ToolInfo {
	return []shared.ToolInfo{
		{Name: "Docker", Available: true, Version: "27.1.1", Source: "binary"},
		{Name: "Trivy", Available: true, Version: "0.55.0", Source: "docker"},
		{Name: "Gitleaks", Available: false},
	}
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want dashboard.Model", next)
	}
	return updated, cmd
}

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewStartsEverySectionLoading(t *testing.T) {
	m := New(testConfig(), &shared.State{})

	loading := map[string]bool{
		"services":   m.loadingServices,
		"gitlab":     m.loadingForge,
		"docker":     m.loadingDocker,
		"oci":        m.loadingOCI,
		"workspaces": m.loadingWorkspaces,
		"tools":      m.loadingTools,
	}
	for name, isLoading := range loading {
		if !isLoading {
			t.Errorf("%s does not start loading, so its section shows empty data before the first result", name)
		}
	}
	if m.serviceStatus != shared.ServiceStatusUnknown {
		t.Errorf("serviceStatus = %q before any check, want unknown", m.serviceStatus)
	}
}

func TestNewReadsTheRefreshInterval(t *testing.T) {
	cfg := testConfig()
	cfg.Status.RefreshInterval = 45

	if got := New(cfg, &shared.State{}).refreshInterval; got != 45*time.Second {
		t.Errorf("refreshInterval = %v, want 45s", got)
	}
}

func TestInitLoadsEverything(t *testing.T) {
	if cmd := New(testConfig(), &shared.State{}).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so the dashboard never populates")
	}
}

func TestInEditModeIsAlwaysFalse(t *testing.T) {
	m, _ := loadedModel(t)

	if m.InEditMode() {
		t.Error("the dashboard has no input fields, so InEditMode() must stay false")
	}
}

// ── Result handling ──────────────────────────────────────────────────────────

// Every handler mirrors its result into the shared state, which is what the
// other views read instead of refetching.
func TestResultsArePublishedToSharedState(t *testing.T) {
	m, state := loadedModel(t)

	if state.ForgeStats == nil || state.ForgeStats.AssignedChangeRequests == nil || *state.ForgeStats.AssignedChangeRequests != 3 {
		t.Errorf("shared ForgeStats = %+v, want the fetched counts", state.ForgeStats)
	}
	if state.DockerStats == nil || state.DockerStats.Running != 2 {
		t.Errorf("shared DockerStats = %+v, want the fetched counts", state.DockerStats)
	}
	if state.OCIStats == nil || state.OCIStats.ImagesCount != 8 {
		t.Errorf("shared OCIStats = %+v, want the fetched counts", state.OCIStats)
	}
	if state.WorkspaceCount != 6 {
		t.Errorf("shared WorkspaceCount = %d, want 6", state.WorkspaceCount)
	}
	if len(state.Tools) != 3 {
		t.Errorf("shared Tools holds %d entries, want 3", len(state.Tools))
	}
	if len(state.ServiceComponents) != 5 {
		t.Errorf("shared ServiceComponents holds %d entries, want 5", len(state.ServiceComponents))
	}
	if state.ServiceStatus != shared.ServiceStatusDegraded {
		t.Errorf("shared ServiceStatus = %q, want degraded", state.ServiceStatus)
	}

	// And the model itself stopped loading.
	if m.loadingServices || m.loadingForge || m.loadingDocker || m.loadingOCI || m.loadingWorkspaces || m.loadingTools {
		t.Error("a section is still loading after its result arrived")
	}
}

func TestComputeGlobalStatus(t *testing.T) {
	tests := []struct {
		name       string
		components []status.ComponentStatus
		want       shared.ServiceGlobalStatus
	}{
		{"no monitors", nil, shared.ServiceStatusUnknown},
		{
			"every monitor up",
			[]status.ComponentStatus{{Status: status.StatusOK}, {Status: status.StatusOK}},
			shared.ServiceStatusAllOK,
		},
		{
			"every monitor down",
			[]status.ComponentStatus{{Status: status.StatusDown}, {Status: status.StatusError}},
			shared.ServiceStatusDown,
		},
		{
			"mixed",
			[]status.ComponentStatus{{Status: status.StatusOK}, {Status: status.StatusDown}},
			shared.ServiceStatusDegraded,
		},
		{
			// A warning is not an OK, so one warning alone reads as down.
			"warning only",
			[]status.ComponentStatus{{Status: status.StatusWarning}},
			shared.ServiceStatusDown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeGlobalStatus(tc.components); got != tc.want {
				t.Errorf("computeGlobalStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFilterComponentsSplitsServicesFromCertificates(t *testing.T) {
	m, _ := loadedModel(t)

	services := m.filterComponents(false)
	if len(services) != 3 {
		t.Errorf("filterComponents(false) returned %d entries, want the three non-SSL monitors", len(services))
	}
	for _, c := range services {
		if c.Type == status.TypeSSL {
			t.Errorf("%q is an SSL monitor but appeared in the services list", c.Name)
		}
	}

	certificates := m.filterComponents(true)
	if len(certificates) != 2 {
		t.Errorf("filterComponents(true) returned %d entries, want the two SSL monitors", len(certificates))
	}
}

func TestCountStatuses(t *testing.T) {
	ok, down, errCount := countStatuses([]status.ComponentStatus{
		{Status: status.StatusOK},
		{Status: status.StatusOK},
		{Status: status.StatusDown},
		{Status: status.StatusWarning},
		{Status: status.StatusError},
	})

	if ok != 2 {
		t.Errorf("ok = %d, want 2", ok)
	}
	if down != 1 {
		t.Errorf("down = %d, want 1", down)
	}
	// Warning and Error both land in the error bucket.
	if errCount != 2 {
		t.Errorf("errCount = %d, want 2", errCount)
	}
}

// ── Keys ─────────────────────────────────────────────────────────────────────

func TestCtrlRMarksEverySectionLoadingAgain(t *testing.T) {
	m, _ := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if cmd == nil {
		t.Fatal("ctrl+r issued no command")
	}
	for name, isLoading := range map[string]bool{
		"services":   m.loadingServices,
		"gitlab":     m.loadingForge,
		"docker":     m.loadingDocker,
		"oci":        m.loadingOCI,
		"workspaces": m.loadingWorkspaces,
	} {
		if !isLoading {
			t.Errorf("%s is not marked loading after ctrl+r", name)
		}
	}
}

// The browser shortcuts need a session: without a user there is no username to
// build the issues URL from, so the key is greyed and says why rather than
// opening a broken link or falling through in silence (Rule 130).
func TestBrowserShortcutsRequireASession(t *testing.T) {
	m, _ := newTestModel(t) // not authenticated

	for _, key := range []string{keymap.Requests, keymap.Issues} {
		if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%s is offered without a signed-in user", key)
		}
		next, _ := step(t, m, testutil.Key(key))
		if got := plain(next.RenderFooter(200)); !strings.Contains(got, reasonNoSession) {
			t.Errorf("%s was declined without saying why — footer:\n%s", key, got)
		}
	}

	authenticated, _ := authenticatedModel(t)
	_, cmd := step(t, authenticated, testutil.Key(keymap.Issues))
	if cmd == nil {
		t.Error("i did nothing for a signed-in user")
	}
}

func TestRefreshTickReloads(t *testing.T) {
	m, _ := loadedModel(t)

	_, cmd := step(t, m, RefreshTickMsg(time.Now()))

	if cmd == nil {
		t.Error("the refresh tick issued no command, so the dashboard goes stale")
	}
}

func TestUnhandledKeysAreInert(t *testing.T) {
	m, _ := loadedModel(t)

	_, cmd := step(t, m, testutil.Key("z"))

	if cmd != nil {
		t.Error("an unbound key issued a command")
	}
}

func TestWindowSizeIsStored(t *testing.T) {
	m, _ := newTestModel(t)

	m = feed(t, m, tea.WindowSizeMsg{Width: 200, Height: 60})

	if m.width != 200 || m.height != 60 {
		t.Errorf("window size = %dx%d, want 200x60", m.width, m.height)
	}
}

// ── Version cleaning ─────────────────────────────────────────────────────────

func TestCleanVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trims whitespace", "  27.1.1\n", "27.1.1"},
		{"drops the docker prefix", "docker: 0.55.0", "0.55.0"},
		{"keeps the first line only", "0.55.0\nextra noise", "0.55.0"},
		{"drops the git preamble", "git version 2.46.0", "2.46.0"},
		{"drops the Trivy preamble", "Version: 0.55.0", "0.55.0"},
		{"empty stays empty", "   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanVersion(tc.in); got != tc.want {
				t.Errorf("cleanVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A binary that is not on PATH must come back unavailable rather than
// half-populated: the tools card keys off Available alone.
func TestDetectBinaryToolReportsAMissingBinary(t *testing.T) {
	tool := detectBinaryTool("Nope", "devdesk-definitely-not-a-real-binary")

	if tool.Available {
		t.Error("a missing binary was reported as available")
	}
	if tool.Source != "" || tool.Version != "" {
		t.Errorf("a missing binary carried source=%q version=%q, want both empty", tool.Source, tool.Version)
	}
	if tool.Name != "Nope" {
		t.Errorf("Name = %q, want it preserved", tool.Name)
	}
}

// Passing no version arguments exercises the lookup without spawning anything.
func TestDetectBinaryToolReportsAPresentBinary(t *testing.T) {
	binary := "sh"
	if runtime.GOOS == "windows" {
		binary = "cmd"
	}

	tool := detectBinaryTool("Shell", binary)

	if !tool.Available {
		t.Skipf("%q is not on PATH in this environment", binary)
	}
	if tool.Source != "binary" {
		t.Errorf("Source = %q for a binary found on PATH, want \"binary\"", tool.Source)
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// tierCases covers one terminal size per palier.
var tierCases = []struct {
	name    string
	width   int
	height  int
	want    tier
	columns int
}{
	// Heights are content lines, which is what the view is handed — roughly the
	// terminal's rows less 13 (header, title line, footer). Only `wide` reads
	// the height: fewer columns means more stacking, so shortening a terminal
	// can never justify dropping one.
	{"a narrow terminal", 80, 24, tierCompact, 1},
	{"a short but wide terminal", 200, 12, tierStandard, 2},
	{"an HD terminal", 120, 20, tierStandard, 2},
	{"a 4K terminal", 240, 45, tierWide, 3},
}

func TestThePalierFollowsBothDimensions(t *testing.T) {
	for _, tc := range tierCases {
		got := layoutTier(tc.width, tc.height)
		if got != tc.want {
			t.Errorf("layoutTier(%d, %d) on %s = %v, want %v", tc.width, tc.height, tc.name, got, tc.want)
		}
		if cols := got.columns(); cols != tc.columns {
			t.Errorf("%s lays out %d columns, want %d", tc.name, cols, tc.columns)
		}
	}
}

func TestTheColumnWidthHasAFloor(t *testing.T) {
	// A terminal too narrow to divide must not produce a column the sections
	// cannot render a label and a value in.
	if got := tierCompact.columnWidth(10); got != minColumnWidth {
		t.Errorf("columnWidth(10) = %d, want the floor of %d", got, minColumnWidth)
	}
}

// TestEverySectionKeepsItsHeightWhateverItsState is the rule §3.19 phase 1
// exists for: a section declares its height and fills it, so a value landing in
// one column can never move another. Sans lui, la première section ajoutée
// ensuite réintroduit le reflow.
func TestEverySectionKeepsItsHeightWhateverItsState(t *testing.T) {
	unknown, _ := newTestModel(t)  // nothing has landed yet
	loaded, _ := loadedModel(t)    // every result message absorbed
	unavailable := withNoDocker(t) // Docker absent, GitLab signed out

	states := map[string]Model{"unknown": unknown, "loaded": loaded, "unavailable": unavailable}

	// Le bloc des outils est l'exception, et elle est délibérée : sa hauteur
	// suit l'inventaire de la machine (voir toolsBlock), pas l'arrivée d'un
	// résultat. Les trois états partagent donc le même inventaire, ce qui laisse
	// le test attraper tout le reste — c'est-à-dire tout ce qui bouge d'un
	// rafraîchissement à l'autre.
	for name, m := range states {
		states[name] = feed(t, m, ToolsDetectedMsg{Tools: toolFixtures()})
	}

	for _, s := range append(overviewSections(config.ForgeGitLab), resourceSections()...) {
		want := -1
		for name, m := range states {
			got := len(s.render(m, 40, tierStandard))
			if want == -1 {
				want = got
				continue
			}
			if got != want {
				t.Errorf("section %q renders %d lines when %s and %d in another state — that is the reflow",
					s.title, got, name, want)
			}
		}
		if want == 0 {
			t.Errorf("section %q renders nothing", s.title)
		}
	}
}

// TestABoxNeverTruncatesItsSection — la hauteur d'une boîte est dérivée des
// sections à l'écran, pas d'une constante. Une constante trop basse ne se voit
// pas : la boîte reste bien formée, elle perd sa dernière ligne. C'est le
// piège qui attend la phase 3, quand un graphe rendra une section plus haute.
func TestABoxNeverTruncatesItsSection(t *testing.T) {
	m, _ := loadedModel(t)

	const extra = 3
	tall := section{title: "Tall", render: func(Model, int, tier) []string {
		lines := make([]string, 0, nominalInnerHeight+extra)
		for i := range nominalInnerHeight + extra {
			lines = append(lines, "line "+string(rune('a'+i)))
		}
		return lines
	}}

	columns := [][]section{{tall}}
	inner := m.innerHeights(columns, 40, tierStandard)
	if want := nominalInnerHeight + extra + trailingBlank; inner[0] != want {
		t.Errorf("innerHeight = %d for a section of %d lines, want its own height plus the trailing blank (%d)",
			inner[0], nominalInnerHeight+extra, want)
	}

	out := plain(strings.Join(m.renderColumn(columns[0], 40, inner, tierStandard), "\n"))
	last := "line " + string(rune('a'+nominalInnerHeight+extra-1))
	if !strings.Contains(out, last) {
		t.Errorf("the box dropped %q — it truncated its section:\n%s", last, out)
	}
}

// TestTheGridRowsLineUp — à nombre de boîtes égal, deux colonnes doivent faire
// exactement la même hauteur : une boîte plus haute que sa voisine décale la
// rangée suivante, et ça se lit comme un bug de rendu plutôt que comme un
// choix. (À `wide`, la troisième colonne porte une boîte de plus et dépasse
// délibérément.)
func TestTheGridRowsLineUp(t *testing.T) {
	m, _ := loadedModel(t)
	columns := m.columnsFor(tierStandard)
	inner := m.innerHeights(columns, 56, tierStandard)

	want := len(m.renderColumn(columns[0], 56, inner, tierStandard))
	for i, col := range columns[1:] {
		if got := len(m.renderColumn(col, 56, inner, tierStandard)); got != want {
			t.Errorf("column %d is %d lines tall, column 0 is %d — the rows do not line up", i+1, got, want)
		}
	}
}

// TestAnUnavailableSourceKeepsItsLabels — "Docker not available" used to
// replace the block. With a skeleton the labels stay: what the dashboard would
// show is itself an answer.
func TestAnUnavailableSourceKeepsItsLabels(t *testing.T) {
	lines := renderDockerSection(withNoDocker(t), 40, tierStandard)

	for _, label := range []string{"Containers", "Resources", "images", "volumes", "networks"} {
		if !containsLine(lines, label) {
			t.Errorf("with no Docker, the section dropped the %q label: %q", label, lines)
		}
	}
}

// TestAnUnmeasuredValueIsNotZero — three value states, not two.
func TestAnUnmeasuredValueIsNotZero(t *testing.T) {
	if stripANSI(unknownValue()) == stripANSI(countValue(0)) {
		t.Error("an unmeasured value renders like a measured zero — the two cannot be told apart")
	}
	if stripANSI(unknownValue()) != "-" {
		t.Errorf("unknown renders %q, want %q", stripANSI(unknownValue()), "-")
	}
	if stripANSI(countValue(0)) != "0" {
		t.Errorf("a measured zero renders %q, want %q", stripANSI(countValue(0)), "0")
	}
}

// routerOverhead is what the router keeps for itself out of the terminal's
// rows: header (9), title line (1), footer with a tab bar (3).
const routerOverhead = 9 + 1 + 3

// TestTheOverviewFitsAtTheHeightItNeeds — le viewport du routeur **ne défile
// pas** : rien ne lui transmet de touche, donc ce qui dépasse est perdu en
// silence, pas repoussé sous une barre de défilement. La hauteur de la grille
// est donc une promesse, et gridHeight() est ce qu'elle vaut.
func TestTheOverviewFitsAtTheHeightItNeeds(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	need := m.gridHeight(tierStandard)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: need})

	if got := strings.Count(m.View(), "\n") + 1; got > need {
		t.Errorf("the overview renders %d lines into the %d it measured", got, need)
	}
	t.Logf("the two-column grid needs %d content lines, i.e. a %d-row terminal",
		need, need+routerOverhead)
}

// TestTheChosenPalierLosesTheFewestLines — en dessous de gridHeight() aucun
// layout ne tient, et le palier n'a plus à choisir le meilleur mais le moins
// mauvais : une grille 2×2 amputée d'une ligne bat une colonne de quatre boîtes
// amputée de dix-neuf. C'est cette règle-là qui justifie que
// standardMinHeight soit *sous* gridHeight(), et non un oubli.
func TestTheChosenPalierLosesTheFewestLines(t *testing.T) {
	m, _ := loadedModel(t)

	for height := 12; height <= 40; height++ {
		chosen := layoutTier(120, height)
		lost := overflowAt(t, m, 120, height, chosen)

		for _, other := range []tier{tierCompact, tierStandard} {
			if other == chosen {
				continue
			}
			if alt := overflowAt(t, m, 120, height, other); alt < lost {
				t.Errorf("at %d content lines the layout chose %v and loses %d lines, while %v would lose %d",
					height, chosen, lost, other, alt)
			}
		}
	}
}

// overflowAt reports how many lines a palier's grid would lose at a height.
func overflowAt(t *testing.T, m Model, width, height int, at tier) int {
	t.Helper()

	columns := m.columnsFor(at)
	inner := m.innerHeights(columns, at.columnWidth(width), at)

	tallest := 0
	for _, col := range columns {
		if n := len(m.renderColumn(col, at.columnWidth(width), inner, at)); n > tallest {
			tallest = n
		}
	}
	return max(tallest-height, 0)
}

// TestNoFactIsUnreachableAtAnyPalier — le palier décide où est un fait, jamais
// s'il existe. A section that shows at `wide` shows at `compact` too, inline or
// behind a tab: a fact that vanishes on a small terminal is indistinguishable
// from a bug.
func TestNoFactIsUnreachableAtAnyPalier(t *testing.T) {
	m, _ := loadedModel(t)

	for _, tc := range tierCases {
		var shown []string
		for tab := range dashboardTab(tabCountFor(tc.want)) {
			at := m
			at.activeTab = tab
			for _, col := range at.columnsFor(tc.want) {
				for _, s := range col {
					shown = append(shown, s.title)
				}
			}
		}
		for _, s := range append(overviewSections(config.ForgeGitLab), resourceSections()...) {
			if !containsLine(shown, s.title) {
				t.Errorf("section %q is reachable from no tab on %s", s.title, tc.name)
			}
		}
	}
}

// TestNoBoxIsReachableFromTwoTabs — un onglet existe pour ce qui n'a pas de vue
// à lui *et* qui n'est pas déjà à l'écran. À `wide`, la troisième colonne porte
// les boîtes de Resources : offrir l'onglet en plus donne les mêmes trois
// boîtes à deux endroits, ce qu'un onglet est censé éviter.
func TestNoBoxIsReachableFromTwoTabs(t *testing.T) {
	m, _ := loadedModel(t)

	for _, tc := range tierCases {
		owner := map[string]dashboardTab{}
		for tab := range dashboardTab(tabCountFor(tc.want)) {
			at := m
			at.activeTab = tab
			for _, col := range at.columnsFor(tc.want) {
				for _, s := range col {
					if first, seen := owner[s.title]; seen {
						t.Errorf("on %s, the %q box is on tab %d and tab %d", tc.name, s.title, first, tab)
						continue
					}
					owner[s.title] = tab
				}
			}
		}
	}
}

// TestTheResourcesTabIsNotOfferedWhenItsBoxesAreOnScreen — et il ne suffit pas
// de ne pas le proposer : la touche ne doit rien faire et le raccourci ne doit
// pas être annoncé (Rule 130).
func TestTheResourcesTabIsNotOfferedWhenItsBoxesAreOnScreen(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 240, Height: 45}) // tierWide

	if got := plain(m.RenderFooter(240)); strings.Contains(got, "Resources") {
		t.Errorf("the tab bar still offers Resources while its boxes are on screen:\n%s", got)
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabOverview {
		t.Errorf("tab moved to %v when there is only one tab", m.activeTab)
	}

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "tab") {
		t.Error("tab is offered where there is only one tab")
	}
}

// A terminal shrinking back below `wide` gets the tab again; growing into it
// while on Resources must not strand the view on a tab that no longer exists.
func TestGrowingIntoWideLeavesTheResourcesTab(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 20}, testutil.Key("tab"))
	if m.activeTab != tabResources {
		t.Fatalf("the view is on %v, want the Resources tab before growing", m.activeTab)
	}

	m = feed(t, m, tea.WindowSizeMsg{Width: 240, Height: 45})
	if m.activeTab != tabOverview {
		t.Errorf("growing into wide left the view on %v, a tab that no longer exists", m.activeTab)
	}
}

// TestEveryRenderedLineIsExactlyTheViewWidth — Rule 116. Une seule ligne trop
// longue décale tout ce qui est à sa droite.
func TestEveryRenderedLineIsExactlyTheViewWidth(t *testing.T) {
	m, _ := loadedModel(t)

	for _, tc := range tierCases {
		at := feed(t, m, tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		for i, line := range strings.Split(at.View(), "\n") {
			if got := lipgloss.Width(line); got != tc.width {
				t.Errorf("on %s, line %d is %d cells wide, want %d", tc.name, i, got, tc.width)
			}
		}
	}
}

// withNoDocker returns a model told that Docker is absent and GitLab signed
// out — the "unavailable" state, distinct from "not measured yet".
func withNoDocker(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t)
	return feed(t, m,
		StatusCheckMsg{Result: status.MonitorResult{Timestamp: time.Now()}},
		ForgeStatsMsg{},
		DockerStatsMsg{Stats: shared.DockerStats{Available: false}},
		OCIStatsMsg{Stats: shared.OCIStats{Available: false}},
		WorkspaceStatsMsg{Count: 0},
		ToolsDetectedMsg{Tools: nil},
	)
}

// containsLine reports whether any line carries the substring.
func containsLine(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(stripANSI(l), stripANSI(substr)) {
			return true
		}
	}
	return false
}

// stripANSI removes escape sequences so an assertion compares what is on
// screen rather than how it is coloured.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
