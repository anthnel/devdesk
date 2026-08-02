package dashboard

import (
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The dashboard's Cmds run status checks, hit the GitLab API, shell out to
// docker and du, and open a browser. None of them is executed here: the tests
// feed the result messages directly and assert on the model and its rendering.

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.GitLab.URL = "https://gitlab.example.com"
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
	state.CurrentUser = &gitlabclient.User{ID: 7, Username: "anthoni"}
	return m, state
}

// loadedModel returns a model that has absorbed one of every result message.
func loadedModel(t *testing.T) (Model, *shared.State) {
	t.Helper()
	m, state := authenticatedModel(t)
	m = feed(t, m,
		StatusCheckMsg{Result: status.MonitorResult{Components: componentFixtures(), Timestamp: time.Now()}},
		GitLabStatsMsg{Stats: shared.GitLabStats{AssignedMRs: 3, ReviewMRs: 2, AssignedIssues: 5, TotalProjects: 12, TotalGroups: 4}},
		DockerStatsMsg{Stats: shared.DockerStats{Available: true, Running: 2, Stopped: 1, Paused: 1}},
		OCIStatsMsg{Stats: shared.OCIStats{Available: true, ImagesCount: 8, ImagesSize: "1.2GB", ContainersCount: 4, ContainersSize: "300MB", VolumesCount: 2, VolumesSize: "50MB"}},
		WorkspaceStatsMsg{Count: 6, DiskSize: "4.2G"},
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
		{Name: "cert-ok", Type: status.TypeSSL, Status: status.StatusOK},
		{Name: "cert-expired", Type: status.TypeSSL, Status: status.StatusDown},
	}
}

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
		"gitlab":     m.loadingGitLab,
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

	if state.GitLabStats == nil || state.GitLabStats.AssignedMRs != 3 {
		t.Errorf("shared GitLabStats = %+v, want the fetched counts", state.GitLabStats)
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
	if m.loadingServices || m.loadingGitLab || m.loadingDocker || m.loadingOCI || m.loadingWorkspaces || m.loadingTools {
		t.Error("a section is still loading after its result arrived")
	}
}

func TestStatusCheckRecordsTheTimestamp(t *testing.T) {
	m, _ := newTestModel(t)
	at := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)

	m = feed(t, m, StatusCheckMsg{Result: status.MonitorResult{Components: componentFixtures(), Timestamp: at}})

	if !m.lastRefresh.Equal(at) {
		t.Errorf("lastRefresh = %v, want %v", m.lastRefresh, at)
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
		"gitlab":     m.loadingGitLab,
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
// build the issues URL from, so the key must do nothing rather than open a
// broken link.
func TestBrowserShortcutsRequireASession(t *testing.T) {
	m, _ := newTestModel(t) // not authenticated

	_, cmd := step(t, m, testutil.Key("i"))
	if cmd != nil {
		t.Error("i opened a URL without a signed-in user")
	}

	authenticated, _ := authenticatedModel(t)
	_, cmd = step(t, authenticated, testutil.Key("i"))
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

func TestColumnWidthHasAFloor(t *testing.T) {
	m, _ := newTestModel(t)

	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if got := m.columnWidth(); got != (120-6)/2 {
		t.Errorf("columnWidth() = %d at 120 columns, want %d", got, (120-6)/2)
	}

	// A narrow terminal must not produce a width the sections cannot render in.
	m = feed(t, m, tea.WindowSizeMsg{Width: 20, Height: 40})
	if got := m.columnWidth(); got != 30 {
		t.Errorf("columnWidth() = %d on a narrow terminal, want the floor of 30", got)
	}
}

func TestSectionsToLinesSeparatesWithBlankLines(t *testing.T) {
	lines := sectionsToLines([]string{"a\nb", "c"}, 10)

	// Two lines, a separator, then one line.
	if len(lines) != 4 {
		t.Fatalf("sectionsToLines returned %d lines, want 4: %q", len(lines), lines)
	}
	if strings.TrimSpace(lines[2]) != "" {
		t.Errorf("line 2 = %q, want the blank separator", lines[2])
	}
}

func TestSectionsToLinesAddsNoTrailingSeparator(t *testing.T) {
	lines := sectionsToLines([]string{"only"}, 10)

	if len(lines) != 1 {
		t.Errorf("a single section produced %d lines, want 1: %q", len(lines), lines)
	}
}
