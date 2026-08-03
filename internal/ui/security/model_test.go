package security

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// ── Constructors ─────────────────────────────────────────────────────────────

// The form opens with the options the context was left with, not with hardcoded
// defaults: a user who turned licence scanning off should not find it back on.
func TestNewAdoptsTheConfiguredOptions(t *testing.T) {
	cfg := testConfig()
	cfg.Scan.EnableVuln = false
	cfg.Scan.EnableLicense = true
	cfg.Scan.GitleaksHistory = true
	cfg.Scan.TrivyServer = "https://trivy:4954"

	m := New(cfg)

	if m.enableVuln || !m.enableLicense || !m.gitleaksHistory {
		t.Errorf("options = vuln:%v licence:%v history:%v, want the configured values",
			m.enableVuln, m.enableLicense, m.gitleaksHistory)
	}
	if m.trivyServerInput.Value() != "https://trivy:4954" {
		t.Errorf("the Trivy server field = %q, want the configured URL", m.trivyServerInput.Value())
	}
	if m.state != StateInput || m.targetType != "directory" {
		t.Errorf("a new model opens in state %v on target type %q", m.state, m.targetType)
	}
}

func TestConstructorsPrefillTheirTarget(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		m := NewWithTarget(testConfig(), "/tmp/repo")

		if m.targetPath != "/tmp/repo" || m.targetInput.Value() != "/tmp/repo" {
			t.Errorf("target = %q / %q", m.targetPath, m.targetInput.Value())
		}
		if m.targetType != "directory" || m.isImageScan {
			t.Errorf("target type = %q, image = %v", m.targetType, m.isImageScan)
		}
	})

	t.Run("image", func(t *testing.T) {
		m := NewWithImageTarget(testConfig(), "nginx:latest", true)

		if m.targetType != "image" || !m.isImageScan || !m.returnToOCIImages {
			t.Errorf("type=%q image=%v return=%v", m.targetType, m.isImageScan, m.returnToOCIImages)
		}
		if m.targetInput.Value() != "nginx:latest" {
			t.Errorf("target = %q", m.targetInput.Value())
		}
	})

	// "all" is a sentinel rather than an image name, so the field says so.
	t.Run("every image", func(t *testing.T) {
		m := NewWithImageTarget(testConfig(), "all", true)

		if m.targetInput.Placeholder != "All OCI Images" {
			t.Errorf("placeholder = %q, want it to name the batch", m.targetInput.Placeholder)
		}
	})

	t.Run("returning to workspaces", func(t *testing.T) {
		m := NewWithTargetReturnToWorkspaces(testConfig(), "/tmp/repo")

		if !m.returnToWorkspaces || m.targetPath != "/tmp/repo" {
			t.Errorf("return=%v target=%q", m.returnToWorkspaces, m.targetPath)
		}
	})
}

// Opening a cached result skips the form and the scan entirely.
func TestNewWithPreloadedResultOpensOnTheResults(t *testing.T) {
	result := resultFixture()

	m := feed(t, NewWithPreloadedResult(testConfig(), result), tea.WindowSizeMsg{Width: 160, Height: 30})

	if m.state != StateResults {
		t.Errorf("state = %v, want StateResults", m.state)
	}
	if m.targetPath != result.Target || m.activeTab != TabCVE {
		t.Errorf("target = %q, tab = %d", m.targetPath, m.activeTab)
	}
	if len(m.findingsTable.Rows()) == 0 {
		t.Error("the table is empty; the resize should have populated it")
	}
}

func TestInitChecksDependencies(t *testing.T) {
	if cmd := New(testConfig()).Init(); cmd == nil {
		t.Error("Init() issued no dependency check")
	}

	m := feed(t, newTestModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{
		TrivyAvailable: true, TrivyVersion: "0.50.0", DockerAvailable: true,
	}})
	if !m.deps.TrivyAvailable || m.deps.TrivyVersion != "0.50.0" {
		t.Errorf("deps = %+v, want the checked status", m.deps)
	}
}

// ── Form navigation ──────────────────────────────────────────────────────────

// Rule 135: ↑/↓ move between fields, and only those keys.
func TestFieldNavigationWraps(t *testing.T) {
	m := newTestModel(t)
	if m.focusedField != 0 {
		t.Fatalf("focusedField = %d on a new form", m.focusedField)
	}

	m = feed(t, m, testutil.Key("up"))
	if want := m.totalFields() - 1; m.focusedField != want {
		t.Errorf("focusedField = %d after up from the first field, want %d", m.focusedField, want)
	}

	m = feed(t, m, testutil.Key("down"))
	if m.focusedField != 0 {
		t.Errorf("focusedField = %d after wrapping forward", m.focusedField)
	}
}

// The text fields take focus as navigation reaches them, so typing lands in the
// right one without an extra keystroke.
func TestTextFieldsTakeFocusOnArrival(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("down")) // field 1, the target path

	if !m.targetInput.Focused() {
		t.Error("the target field did not take focus")
	}

	m = feed(t, m, testutil.Type("/srv/app")...)
	if m.targetPath != "/srv/app" {
		t.Errorf("targetPath = %q after typing", m.targetPath)
	}

	m = feed(t, m, testutil.Key("down"))
	if m.targetInput.Focused() {
		t.Error("the target field kept focus after moving on")
	}
}

// Trivy's client-server protocol supports neither misconfiguration, licence nor
// SBOM scanning, so those three are skipped rather than offered and ignored.
func TestServerModeSkipsTheOptionsItCannotRun(t *testing.T) {
	m := newTestModel(t)
	m.trivyServerInput.SetValue("https://trivy:4954")

	for _, field := range []int{4, 5, 6} {
		if !m.isFieldSkipped(field) {
			t.Errorf("field %d is reachable in server mode", field)
		}
	}

	m.focusedField = 3
	if next := m.nextField(); next != 7 {
		t.Errorf("nextField() from 3 = %d, want it to jump over 4-6 to 7", next)
	}

	m.focusedField = 7
	if prev := m.prevField(); prev != 3 {
		t.Errorf("prevField() from 7 = %d, want 3", prev)
	}
}

func TestServerModeDisablesTheOptionsItCannotRun(t *testing.T) {
	m := newTestModel(t)
	m.enableMisconfig, m.enableLicense, m.generateSBOM = true, true, true
	m.focusedField = 7
	m.trivyServerInput.SetValue("https://trivy:4954")

	m = feed(t, m, testutil.Key("down")) // leaving the server field applies the constraint

	if m.enableMisconfig || m.enableLicense || m.generateSBOM {
		t.Errorf("misconfig=%v licence=%v sbom=%v in server mode",
			m.enableMisconfig, m.enableLicense, m.generateSBOM)
	}
}

// Space toggles a checkbox and nothing else does (Rule 135).
func TestSpaceTogglesEachCheckbox(t *testing.T) {
	tests := []struct {
		field int
		read  func(Model) bool
	}{
		{2, func(m Model) bool { return m.enableVuln }},
		{3, func(m Model) bool { return m.enableSecret }},
		{4, func(m Model) bool { return m.enableMisconfig }},
		{5, func(m Model) bool { return m.enableLicense }},
		{6, func(m Model) bool { return m.generateSBOM }},
		{8, func(m Model) bool { return m.ignoreUnfixed }},
		{9, func(m Model) bool { return m.ignoreEOL }},
		{11, func(m Model) bool { return m.gitleaksHistory }},
	}

	for _, tt := range tests {
		m := newTestModel(t)
		m.focusedField = tt.field
		before := tt.read(m)

		toggled := feed(t, m, testutil.Key(" "))
		if tt.read(toggled) == before {
			t.Errorf("space on field %d did not toggle it", tt.field)
		}

		// Rule 135: enter starts the scan, it does not toggle.
		entered := feed(t, m, testutil.Key("enter"))
		if tt.read(entered) != before {
			t.Errorf("enter on field %d toggled it", tt.field)
		}
	}
}

// A checkbox the server cannot honour must not toggle either.
func TestServerModeCheckboxesDoNotToggle(t *testing.T) {
	m := newTestModel(t)
	m.trivyServerInput.SetValue("https://trivy:4954")
	m.focusedField = 4

	if toggled := feed(t, m, testutil.Key(" ")); toggled.enableMisconfig {
		t.Error("a server-incompatible checkbox toggled")
	}
}

// Rule 132: ←→ cycle the target type, and switching clears a path that no
// longer means anything.
func TestTargetTypeCyclesAndClearsThePath(t *testing.T) {
	m := feed(t, NewWithTarget(testConfig(), "/tmp/repo"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.focusedField = 0

	m = feed(t, m, testutil.Key("right"))
	if m.targetType != "image" {
		t.Errorf("targetType = %q after →", m.targetType)
	}
	if m.targetPath != "" || m.targetInput.Value() != "" {
		t.Errorf("the directory path survived the switch to image: %q", m.targetPath)
	}
	if m.targetInput.Placeholder != "image:tag" {
		t.Errorf("placeholder = %q, want it to describe an image", m.targetInput.Placeholder)
	}

	m = feed(t, m, testutil.Key("left"))
	if m.targetType != "directory" {
		t.Errorf("targetType = %q after ←", m.targetType)
	}
}

// ← and → on any other field must not cycle anything.
func TestArrowsOnlyCycleTheTypeField(t *testing.T) {
	m := newTestModel(t)
	m.focusedField = 2

	moved := feed(t, m, testutil.Key("left"), testutil.Key("right"))

	if moved.targetType != m.targetType || moved.focusedField != 2 {
		t.Errorf("←→ on a checkbox changed type=%q field=%d", moved.targetType, moved.focusedField)
	}
}

// ── Starting a scan ──────────────────────────────────────────────────────────

func TestScanNeedsATarget(t *testing.T) {
	m, cmd := step(t, newTestModel(t), testutil.Key("enter"))

	if cmd != nil {
		t.Errorf("a scan with no target issued %T", testutil.Msg(cmd))
	}
	if m.err == nil || !strings.Contains(m.err.Error(), "target path is required") {
		t.Errorf("err = %v, want it to name the missing target", m.err)
	}
	if m.state != StateInput {
		t.Errorf("state = %v, want to stay on the form", m.state)
	}
}

// Opened from the OCI images view, the scan is handed back rather than run here
// — that view owns the batch and the progress display.
func TestImageScanOpenedFromOCIIsDelegated(t *testing.T) {
	t.Run("one image", func(t *testing.T) {
		m := feed(t, NewWithImageTarget(testConfig(), "nginx:latest", true), tea.WindowSizeMsg{Width: 160, Height: 30})

		m, cmd := step(t, m, testutil.Key("enter"))

		msg, ok := testutil.MsgOf[ociresources.LaunchSingleImageScanMsg](cmd)
		if !ok {
			t.Fatalf("enter produced %T, want a launch message", testutil.Msg(cmd))
		}
		if msg.ImageName != "nginx:latest" {
			t.Errorf("delegated %q", msg.ImageName)
		}
		if m.state != StateInput {
			t.Errorf("state = %v; the scan runs in the other view", m.state)
		}
	})

	t.Run("every image", func(t *testing.T) {
		m := feed(t, NewWithImageTarget(testConfig(), "all", true), tea.WindowSizeMsg{Width: 160, Height: 30})

		_, cmd := step(t, m, testutil.Key("enter"))

		if _, ok := testutil.MsgOf[ociresources.LaunchBatchScanMsg](cmd); !ok {
			t.Fatalf("enter produced %T, want a batch launch", testutil.Msg(cmd))
		}
	})
}

// The options reach the other view, or the scan runs with settings the user did
// not choose.
func TestDelegatedScanCarriesTheFormOptions(t *testing.T) {
	m := feed(t, NewWithImageTarget(testConfig(), "nginx:latest", true), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.enableSecret = false
	m.ignoreUnfixed = true
	m.trivyServerInput.SetValue("https://trivy:4954")

	_, cmd := step(t, m, testutil.Key("enter"))

	msg, _ := testutil.MsgOf[ociresources.LaunchSingleImageScanMsg](cmd)
	if msg.Opts.EnableSecret || !msg.Opts.IgnoreUnfixed {
		t.Errorf("options = %+v, want the form's", msg.Opts)
	}
	if msg.Opts.TrivyServer != "https://trivy:4954" {
		t.Errorf("TrivyServer = %q", msg.Opts.TrivyServer)
	}
}

// ── Progress ─────────────────────────────────────────────────────────────────

// Stages are kept in arrival order and updated in place, so a stage that
// reports twice does not appear twice.
func TestProgressUpdatesStagesInPlace(t *testing.T) {
	m := newTestModel(t)

	m = feed(t, m,
		ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Detail: "starting"}},
		ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "secret", Label: "Secrets", Detail: "starting"}},
		ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Detail: "done"}},
	)

	if len(m.scanStages) != 2 {
		t.Fatalf("scanStages = %+v, want one entry per stage", m.scanStages)
	}
	if m.scanStages[0].Stage != "vuln" || m.scanStages[0].Detail != "done" {
		t.Errorf("first stage = %+v, want it updated in place", m.scanStages[0])
	}
	if m.scanStages[1].Stage != "secret" {
		t.Errorf("arrival order was not kept: %+v", m.scanStages)
	}
}

func TestSpinnerTicksOnlyWhileScanning(t *testing.T) {
	if _, cmd := step(t, newTestModel(t), spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner ticked on the form")
	}

	scanning := newTestModel(t)
	scanning.state = StateScanning
	if _, cmd := step(t, scanning, spinner.TickMsg{}); cmd == nil {
		t.Error("the spinner stopped during a scan")
	}
}

// ── Completion ───────────────────────────────────────────────────────────────

func TestScanCompleteShowsTheResults(t *testing.T) {
	m := scannedModel(t)

	if m.state != StateResults {
		t.Fatalf("state = %v after a successful scan", m.state)
	}
	if m.result == nil {
		t.Fatal("the result was not kept")
	}
	if got := rowIDs(m.findingsTable.Rows()); !equal(got, []string{"CVE-2026-0001", "CVE-2026-0002"}) {
		t.Errorf("the CVE tab shows %v", got)
	}
}

func TestScanFailureReturnsToTheForm(t *testing.T) {
	m := newTestModel(t)

	m = feed(t, m, ScanCompleteMsg{Error: errors.New("trivy: exit status 1"), Gen: m.scanGen})

	if m.state != StateInput {
		t.Errorf("state = %v after a failed scan, want the form", m.state)
	}
	if m.err == nil {
		t.Error("the failure was not recorded")
	}
}

// A cancelled scan still finishes in the background and sends its result. The
// generation counter is what stops it overwriting whatever the user did next.
func TestACancelledScanResultIsDiscarded(t *testing.T) {
	m := newTestModel(t)
	m.state = StateScanning
	staleGen := m.scanGen

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateInput {
		t.Fatalf("esc left state = %v, want the form", m.state)
	}

	m = feed(t, m, ScanCompleteMsg{Result: resultFixture(), Gen: staleGen})

	if m.state != StateInput || m.result != nil {
		t.Errorf("a stale result was applied: state=%v result=%v", m.state, m.result)
	}
}

// Opened from the workspaces view, the summary goes back so the row can show
// its counters without re-reading the cache.
func TestWorkspaceScanReportsBackToWorkspaces(t *testing.T) {
	m := feed(t, NewWithTargetReturnToWorkspaces(testConfig(), "/tmp/repo"), tea.WindowSizeMsg{Width: 160, Height: 30})

	_, cmd := step(t, m, ScanCompleteMsg{Result: resultFixture(), Gen: m.scanGen})

	msg, ok := testutil.MsgOf[workspaces.WorkspaceScanCompleteMsg](cmd)
	if !ok {
		t.Fatalf("completion produced %T, want the workspaces summary", testutil.Msg(cmd))
	}
	if msg.RepoPath != "/tmp/repo" {
		t.Errorf("RepoPath = %q", msg.RepoPath)
	}
	if msg.Critical != 1 || msg.High != 1 || msg.Medium != 1 || msg.Low != 1 {
		t.Errorf("counts = %+v, want the result's", msg)
	}
	// The fixture holds a gitleaks finding, which is what marks a repo sensitive.
	if !msg.Sensitive {
		t.Error("Sensitive is false despite a secret in the findings")
	}
}

func TestSensitiveIsFalseWithoutASecret(t *testing.T) {
	result := resultFixture()
	result.Findings = result.Findings[:2] // the two CVEs only
	m := feed(t, NewWithTargetReturnToWorkspaces(testConfig(), "/tmp/repo"), tea.WindowSizeMsg{Width: 160, Height: 30})

	_, cmd := step(t, m, ScanCompleteMsg{Result: result, Gen: m.scanGen})

	msg, _ := testutil.MsgOf[workspaces.WorkspaceScanCompleteMsg](cmd)
	if msg.Sensitive {
		t.Error("Sensitive is true with no secret in the findings")
	}
}

// ── Tabs and filters ─────────────────────────────────────────────────────────

// Each tab is a different question about the same findings, so the routing is
// what makes the counts mean anything.
func TestEachTabShowsItsOwnFindings(t *testing.T) {
	tests := []struct {
		tab  int
		want []string
	}{
		{TabCVE, []string{"CVE-2026-0001", "CVE-2026-0002"}},
		{TabSecrets, []string{"aws-access-token"}},
		{TabLicense, []string{"GPL-3.0"}},
		{TabMisconfig, []string{"DS002"}},
	}

	for _, tt := range tests {
		m := scannedModel(t)
		m.switchTab(tt.tab)

		if got := rowIDs(m.findingsTable.Rows()); !equal(got, tt.want) {
			t.Errorf("tab %d shows %v, want %v", tt.tab, got, tt.want)
		}
	}
}

func TestTabCountsMatchTheTabs(t *testing.T) {
	m := scannedModel(t)

	cve, secrets, licences, misconfigs := m.countFindingsByTab()

	if cve != 2 || secrets != 1 || licences != 1 || misconfigs != 1 {
		t.Errorf("counts = %d/%d/%d/%d, want 2/1/1/1", cve, secrets, licences, misconfigs)
	}
}

func TestTabKeysAndCycling(t *testing.T) {
	m := scannedModel(t)

	for key, want := range map[string]int{"1": TabCVE, "2": TabSecrets, "3": TabLicense, "4": TabMisconfig} {
		if got := feed(t, m, testutil.Key(key)).activeTab; got != want {
			t.Errorf("%q selected tab %d, want %d", key, got, want)
		}
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != TabSecrets {
		t.Errorf("tab moved to %d, want the next one", m.activeTab)
	}

	m = feed(t, m, testutil.Key("shift+tab"))
	if m.activeTab != TabCVE {
		t.Errorf("shift+tab moved to %d, want back to the first", m.activeTab)
	}

	// Cycling wraps rather than stopping at the last tab.
	m = feed(t, m, testutil.Keys("tab", "tab", "tab", "tab")...)
	if m.activeTab != TabCVE {
		t.Errorf("a full cycle ended on tab %d", m.activeTab)
	}
}

// '.' walks the severity floor: each step admits one more level.
func TestSeverityFilterIsCumulative(t *testing.T) {
	m := scannedModel(t)

	steps := []struct {
		filter string
		want   []string
	}{
		{"critical", []string{"CVE-2026-0001"}},
		{"high", []string{"CVE-2026-0001"}},
		{"medium", []string{"CVE-2026-0001", "CVE-2026-0002"}},
		{"low", []string{"CVE-2026-0001", "CVE-2026-0002"}},
		{"all", []string{"CVE-2026-0001", "CVE-2026-0002"}},
	}

	for _, s := range steps {
		m = feed(t, m, testutil.Key("."))
		if m.severityFilter != s.filter {
			t.Fatalf("severityFilter = %q, want %q", m.severityFilter, s.filter)
		}
		if got := rowIDs(m.findingsTable.Rows()); !equal(got, s.want) {
			t.Errorf("filter %q shows %v, want %v", s.filter, got, s.want)
		}
	}
}

// The secrets tab has no severity axis worth filtering, so '.' is inert there.
func TestSeverityFilterIsInertOnTheSecretsTab(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)

	filtered := feed(t, m, testutil.Key("."))

	if filtered.severityFilter != "all" {
		t.Errorf("severityFilter = %q on the secrets tab", filtered.severityFilter)
	}
}

// ── Details ──────────────────────────────────────────────────────────────────

func TestEnterOpensTheDetailsOfTheHighlightedFinding(t *testing.T) {
	m := feed(t, scannedModel(t), testutil.Key("down"), testutil.Key("enter"))

	if m.state != StateDetails {
		t.Fatalf("state = %v after enter", m.state)
	}
	if m.selectedIdx != 1 {
		t.Errorf("selectedIdx = %d, want the highlighted row", m.selectedIdx)
	}
	if view := m.View(); !strings.Contains(view, "CVE-2026-0002") {
		t.Errorf("the details do not show the selected finding:\n%s", view)
	}
}

// An empty tab has nothing to open, and enter must not leave the user on a
// blank details screen.
func TestEnterOnAnEmptyTabDoesNothing(t *testing.T) {
	result := resultFixture()
	result.Findings = nil
	m := newTestModel(t)
	m = feed(t, m, ScanCompleteMsg{Result: result, Gen: m.scanGen}, testutil.Key("enter"))

	if m.state != StateResults {
		t.Errorf("state = %v, want to stay on the empty table", m.state)
	}
}

func TestDetailsGoesBackToTheResults(t *testing.T) {
	for _, key := range []string{"esc", "backspace"} {
		if got := feed(t, detailsModel(t), testutil.Key(key)).state; got != StateResults {
			t.Errorf("%q left state = %v, want StateResults", key, got)
		}
	}
}

// 'o' opens the first advisory link; a finding with none says so rather than
// launching a browser at nothing.
func TestOpeningAReference(t *testing.T) {
	t.Run("with a reference", func(t *testing.T) {
		_, cmd := step(t, detailsModel(t), testutil.Key("o"))

		if cmd == nil {
			t.Error("'o' issued no command for a finding with a reference")
		}
	})

	// The command returned here is the Rule 128 expiry timer, not a browser
	// launch — and it must not be executed, since tea.Tick blocks for its whole
	// duration. The status message is what distinguishes the two: a launch sets
	// none. TestFooterMessagesExpire covers the timer itself.
	t.Run("without one", func(t *testing.T) {
		m := feed(t, scannedModel(t), testutil.Key("down"), testutil.Key("enter"))

		m = feed(t, m, testutil.Key("o"))

		if m.statusMessage != "No references available" {
			t.Errorf("statusMessage = %q, want the view to say there is nothing to open", m.statusMessage)
		}
	})
}

// ── Leaving the results ──────────────────────────────────────────────────────

// Opened directly, esc goes back to the form; opened from another view, it
// returns there.
func TestEscFromTheResults(t *testing.T) {
	t.Run("opened directly", func(t *testing.T) {
		m, cmd := step(t, scannedModel(t), testutil.Key("esc"))

		if m.state != StateInput {
			t.Errorf("state = %v, want the form", m.state)
		}
		if cmd != nil {
			t.Errorf("esc emitted %T with no origin view", testutil.Msg(cmd))
		}
	})

	t.Run("opened from another view", func(t *testing.T) {
		m := scannedModel(t)
		m.OriginView = command.ViewWorkspaces

		_, cmd := step(t, m, testutil.Key("esc"))

		msg, ok := testutil.MsgOf[BackToOriginMsg](cmd)
		if !ok {
			t.Fatalf("esc emitted %T, want a return to the origin", testutil.Msg(cmd))
		}
		if msg.Origin != command.ViewWorkspaces {
			t.Errorf("Origin = %q", msg.Origin)
		}
	})
}

// ctrl+r re-opens the form with the filters reset, so the next scan is not
// silently narrowed by the last one's view settings.
func TestRescanResetsTheView(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabMisconfig)
	m = feed(t, m, testutil.Key("."))

	m = feed(t, m, testutil.Key("ctrl+r"))

	if m.state != StateInput {
		t.Errorf("state = %v after ctrl+r", m.state)
	}
	if m.activeTab != TabCVE || m.severityFilter != "all" {
		t.Errorf("tab = %d, filter = %q after ctrl+r", m.activeTab, m.severityFilter)
	}
}

// ── Ignoring a secret ────────────────────────────────────────────────────────

// 'i' writes to .gitleaksignore, so it asks first and names what it will add.
func TestIgnoringASecretAsksFirst(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)

	m = feed(t, m, testutil.Key("i"))

	if m.confirmModal == nil {
		t.Fatal("'i' wrote to .gitleaksignore without asking")
	}
	if m.findingToIgnore == nil || m.findingToIgnore.ID != "aws-access-token" {
		t.Errorf("findingToIgnore = %v, want the highlighted secret", m.findingToIgnore)
	}
	if view := m.confirmModal.View(); !strings.Contains(view, "config/prod.env") {
		t.Errorf("the confirmation does not name the file:\n%s", view)
	}
}

// The key belongs to the secrets tab; a CVE has nothing to ignore.
func TestIgnoringIsOnlyOfferedOnTheSecretsTab(t *testing.T) {
	m := feed(t, scannedModel(t), testutil.Key("i"))

	if m.confirmModal != nil {
		t.Error("'i' opened a confirmation on the CVE tab")
	}
}

func TestConfirmingAnIgnoreIssuesTheWrite(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)
	m = feed(t, m, testutil.Key("i"))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

	if cmd == nil {
		t.Error("confirming issued no write")
	}
	if m.confirmModal != nil || m.findingToIgnore != nil {
		t.Errorf("confirming left modal=%v finding=%v", m.confirmModal, m.findingToIgnore)
	}
}

func TestDecliningAnIgnoreWritesNothing(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)
	m = feed(t, m, testutil.Key("i"))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if cmd != nil {
		t.Errorf("declining issued %T", testutil.Msg(cmd))
	}
	if m.confirmModal != nil || m.findingToIgnore != nil {
		t.Error("declining left the confirmation state behind")
	}
}

func TestIgnoreResultIsReported(t *testing.T) {
	finding := findingFixtures()[2]

	ok := feed(t, scannedModel(t), SecretIgnoredMsg{Finding: finding})
	if !strings.Contains(ok.statusMessage, "config/prod.env") {
		t.Errorf("statusMessage = %q, want it to name the file", ok.statusMessage)
	}

	failed := feed(t, scannedModel(t), SecretIgnoredMsg{Finding: finding, Error: errors.New("permission denied")})
	if failed.statusMessage == "" {
		t.Error("a failed ignore reported nothing")
	}
	if strings.Contains(failed.statusMessage, "permission denied") {
		t.Errorf("statusMessage = %q; Rule 128 keeps the raw error out of the UI", failed.statusMessage)
	}
}

// Rule 128: every footer message clears itself after three seconds. Without a
// timer the last thing that happened stays on screen indefinitely.
func TestFooterMessagesExpire(t *testing.T) {
	finding := findingFixtures()[2]

	tests := []struct {
		name string
		open func(*testing.T) (Model, tea.Cmd)
	}{
		{"an ignore succeeded", func(t *testing.T) (Model, tea.Cmd) {
			return step(t, scannedModel(t), SecretIgnoredMsg{Finding: finding})
		}},
		{"an ignore failed", func(t *testing.T) (Model, tea.Cmd) {
			return step(t, scannedModel(t), SecretIgnoredMsg{Finding: finding, Error: errors.New("denied")})
		}},
		{"a finding has no reference", func(t *testing.T) (Model, tea.Cmd) {
			m := feed(t, scannedModel(t), testutil.Key("down"), testutil.Key("enter"))
			return step(t, m, testutil.Key("o"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cmd := tt.open(t)

			if m.statusMessage == "" {
				t.Fatal("nothing was reported")
			}
			if cmd == nil {
				t.Fatal("no clear timer was scheduled")
			}

			if cleared := feed(t, m, clearStatusMsg{}); cleared.statusMessage != "" {
				t.Errorf("statusMessage = %q after the timer fired", cleared.statusMessage)
			}
		})
	}
}

// While the confirmation is open it owns every key, so results shortcuts must
// not fire behind it.
func TestTheConfirmationSwallowsResultsShortcuts(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)
	m = feed(t, m, testutil.Key("i"))

	m = feed(t, m, testutil.Key("1"))

	if m.activeTab != TabSecrets {
		t.Errorf("a tab key fired behind the confirmation: tab = %d", m.activeTab)
	}
}

// ── Selection from another view ──────────────────────────────────────────────

func TestSelectionFillsTheTarget(t *testing.T) {
	m := feed(t, newTestModel(t), SelectionResultMsg{Path: "/srv/app"})

	if m.targetPath != "/srv/app" || m.targetInput.Value() != "/srv/app" {
		t.Errorf("target = %q / %q after a selection", m.targetPath, m.targetInput.Value())
	}
}

func TestCancellingASelectionChangesNothing(t *testing.T) {
	m := feed(t, NewWithTarget(testConfig(), "/tmp/repo"), tea.WindowSizeMsg{Width: 160, Height: 30})

	cancelled := feed(t, m, SelectionCancelledMsg{})

	if cancelled.targetPath != "/tmp/repo" {
		t.Errorf("target = %q after cancelling", cancelled.targetPath)
	}
}

// 'b' on the target field asks the router for a browser, and which one depends
// on what is being scanned.
func TestBrowsingForATarget(t *testing.T) {
	tests := map[string]string{"directory": "directory", "image": "image"}

	for targetType, want := range tests {
		m := newTestModel(t)
		m.targetType = targetType
		m.focusedField = 1
		m.targetInput.Focus()

		_, cmd := step(t, m, testutil.Key("b"))

		msg, ok := testutil.MsgOf[SelectionRequestMsg](cmd)
		if !ok {
			t.Fatalf("'b' on a %s target produced %T", targetType, testutil.Msg(cmd))
		}
		if msg.Type != want {
			t.Errorf("requested a %q browser for a %s target", msg.Type, targetType)
		}
	}
}

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode tells the router which keys are not its own. It is a question
// about focus — a field or a modal has the keyboard — and nothing else.
func TestInEditModeIsTrueOnlyWhereAFieldHasTheKeyboard(t *testing.T) {
	form := newTestModel(t)
	if form.InEditMode() {
		t.Error("InEditMode() is true on the form with a checkbox focused")
	}

	for _, field := range []int{1, 7, 10} {
		typing := newTestModel(t)
		typing.focusedField = field
		if !typing.InEditMode() {
			t.Errorf("InEditMode() is false while typing in field %d", field)
		}
	}

	// These three used to claim the keyboard for one reason: it was the only way
	// to be handed esc. The router forwards esc on its own now (D15), so they
	// hold no field and claim nothing — which gives them ':', '?' and 'q' back.
	for _, state := range []ViewState{StateScanning, StateResults, StateDetails} {
		m := newTestModel(t)
		m.state = state
		if m.InEditMode() {
			t.Errorf("InEditMode() is true in state %v, where no field has the keyboard", state)
		}
	}

	withModal := scannedModel(t)
	withModal.switchTab(TabSecrets)
	if !feed(t, withModal, testutil.Key("i")).InEditMode() {
		t.Error("InEditMode() is false with a confirmation open")
	}
}

var _ = tea.Model(Model{})
