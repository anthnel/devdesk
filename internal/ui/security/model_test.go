package security

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Constructors ─────────────────────────────────────────────────────────────

// The one constructor that does not open on the inventory: a stored result read
// back from the cache, which is how findings reach the screen now.
func TestNewWithPreloadedResultOpensOnTheResults(t *testing.T) {
	result := resultFixture()

	m := feed(t, NewWithPreloadedResult(testConfig(), result), tea.WindowSizeMsg{Width: 160, Height: 30})

	if m.state != StateResults {
		t.Errorf("state = %v, want StateResults", m.state)
	}
	if m.targetPath != result.Target || m.activeTab != TabCVE {
		t.Errorf("target = %q, tab = %d", m.targetPath, m.activeTab)
	}
	if len(m.findingsTable.Table().Rows()) == 0 {
		t.Error("the table is empty; the resize should have populated it")
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

		if got := rowIDs(m.findingsTable.Table().Rows()); !equal(got, tt.want) {
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
		if got := rowIDs(m.findingsTable.Table().Rows()); !equal(got, s.want) {
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
	if m.selectedFinding == nil || m.selectedFinding.ID != "CVE-2026-0002" {
		t.Errorf("selectedFinding = %v, want the highlighted row", m.selectedFinding)
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
	m := feed(t, NewWithPreloadedResult(testConfig(), result),
		tea.WindowSizeMsg{Width: 160, Height: 30}, testutil.Key("enter"))

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

		if m.state != StateInventory {
			t.Errorf("state = %v, want the inventory", m.state)
		}
		if _, ok := testutil.MsgOf[InventoryLoadedMsg](cmd); !ok {
			t.Error("esc did not reload the inventory it returned to")
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

// ctrl+r goes back to the inventory with the filters reset, so the next result
// opened is not silently narrowed by the last one's view settings.
func TestRescanResetsTheView(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabMisconfig)
	m = feed(t, m, testutil.Key("."))

	m = feed(t, m, testutil.Key("ctrl+r"))

	if m.state != StateInventory {
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

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode tells the router which keys are not its own. It is a question
// about focus — the inventory's filter or a modal has the keyboard — and
// nothing else. The form's text fields were the third answer.
func TestInEditModeIsTrueOnlyWhereSomethingHasTheKeyboard(t *testing.T) {
	if inventoryModel(t, inventoryFixtures()...).InEditMode() {
		t.Error("InEditMode() is true on a settled inventory")
	}

	// Both used to claim the keyboard for one reason: it was the only way to be
	// handed esc. The router forwards esc on its own now (D15), so they hold no
	// field and claim nothing — which gives them ':', '?' and 'q' back.
	for _, state := range []ViewState{StateResults, StateDetails} {
		m := scannedModel(t)
		m.state = state
		if m.InEditMode() {
			t.Errorf("InEditMode() is true in state %v, where nothing has the keyboard", state)
		}
	}

	withModal := scannedModel(t)
	withModal.switchTab(TabSecrets)
	if !feed(t, withModal, testutil.Key("i")).InEditMode() {
		t.Error("InEditMode() is false with a confirmation open")
	}
}

var _ = tea.Model(Model{})

// ── The tabs and the counters are one classification ─────────────────────────

// The Secrets tab shows both scanners. Gitleaks reads git history and Trivy
// reads the target's content, so neither sees what the other does — and an
// image, which Gitleaks cannot scan at all, has only Trivy's half.
func TestTheSecretsTabShowsGitleaksAndTrivyAlike(t *testing.T) {
	result := resultFixture()
	result.Findings = append(result.Findings, scan.Finding{
		ID: "aws-secret-access-key", Title: "AWS key in a layer", Severity: scan.SeverityCritical,
		Source: scan.SourceTrivySecret, File: "app/.env", Line: 3, Match: "AKIA****MPLE",
	})
	result.CountFindings()

	m := feed(t, NewWithPreloadedResult(testConfig(), result), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.switchTab(TabSecrets)

	sources := map[string]bool{}
	for _, f := range m.findingsTable.Items() {
		sources[f.Source] = true
	}
	for _, want := range []string{scan.SourceGitleaks, scan.SourceTrivySecret} {
		if !sources[want] {
			t.Errorf("the Secrets tab omits findings from %q: %v", want, sources)
		}
	}
}

// The tab labels and scan.Result's counters were computed by two rules that
// disagreed. They are one rule now, so the numbers cannot drift — including for
// the three findings that used to fall between them.
func TestTheTabCountsAgreeWithTheResultCounters(t *testing.T) {
	result := resultFixture()
	result.Findings = append(result.Findings,
		scan.Finding{Source: scan.SourceTrivySecret, Severity: scan.SeverityHigh},
		scan.Finding{Source: scan.SourceTrivy, Severity: scan.SeverityHigh}, // no PkgName
		scan.Finding{Source: "grype", Severity: scan.SeverityLow},           // undeclared source
	)
	result.CountFindings()

	m := Model{result: result}
	cve, secrets, licenses, misconfigs := m.countFindingsByTab()

	vulns := result.Counts.Critical + result.Counts.High + result.Counts.Medium +
		result.Counts.Low + result.Counts.Unknown
	if cve != vulns {
		t.Errorf("the CVE tab shows %d, the header counters %d", cve, vulns)
	}
	if secrets != result.SecretCount {
		t.Errorf("the Secrets tab shows %d, SecretCount is %d", secrets, result.SecretCount)
	}
	if licenses != result.LicenseCount {
		t.Errorf("the Licenses tab shows %d, LicenseCount is %d", licenses, result.LicenseCount)
	}
	if misconfigs != result.MisconfigCount {
		t.Errorf("the Misconfig tab shows %d, MisconfigCount is %d", misconfigs, result.MisconfigCount)
	}
	// Every finding is reachable through some tab; one visible in none of them
	// is the failure the two rules produced.
	if total := cve + secrets + licenses + misconfigs; total != len(result.Findings) {
		t.Errorf("%d findings across the tabs, %d in the result — some are in no tab",
			total, len(result.Findings))
	}
}

// The Source column names the tool, not the category: the tab already says the
// category, and telling the two secret scanners apart is why both are there.
func TestTheSourceColumnNamesTheTool(t *testing.T) {
	tests := map[string]string{
		scan.SourceGitleaks:       "gitleaks",
		scan.SourceTrivySecret:    "trivy",
		scan.SourceTrivy:          "vuln",
		scan.SourceTrivyLicense:   "license",
		scan.SourceTrivyMisconfig: "misconfig",
	}

	for source, want := range tests {
		if got := sourceDisplay(scan.Finding{Source: source}); got != want {
			t.Errorf("sourceDisplay(%q) = %q, want %q", source, got, want)
		}
	}
}

// .gitleaksignore is matched on a Gitleaks fingerprint, which a Trivy secret
// does not have. Offering 'i' there would fabricate one, write it, and report
// success for a line Gitleaks will never match.
func TestIgnoringIsOfferedForGitleaksFindingsOnly(t *testing.T) {
	result := resultFixture()
	result.Findings = append(result.Findings, scan.Finding{
		ID: "aws-secret-access-key", Title: "AWS key in a layer", Severity: scan.SeverityCritical,
		Source: scan.SourceTrivySecret, File: "app/.env", Line: 3,
	})
	result.CountFindings()
	m := feed(t, NewWithPreloadedResult(testConfig(), result), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.switchTab(TabSecrets)

	// The fixtures put the Gitleaks finding first.
	if selected, _ := m.findingsTable.Selected(); selected.Source != scan.SourceGitleaks {
		t.Fatalf("the first secret is %q, want the gitleaks one", selected.Source)
	}
	if !has(m.GetShortcuts(), "i") {
		t.Error("'i' is not offered on a gitleaks finding")
	}
	m, _ = step(t, m, testutil.Key("i"))
	if m.confirmModal == nil {
		t.Error("'i' on a gitleaks finding did not ask for confirmation")
	}

	m.confirmModal = nil
	m = feed(t, m, testutil.Key("down"))
	if selected, _ := m.findingsTable.Selected(); selected.Source != scan.SourceTrivySecret {
		t.Fatalf("the second secret is %q, want the trivy one", selected.Source)
	}
	if has(m.GetShortcuts(), "i") {
		t.Error("'i' is offered on a trivy secret, which .gitleaksignore cannot express")
	}

	m, cmd := step(t, m, testutil.Key("i"))
	if m.confirmModal != nil {
		t.Error("'i' on a trivy secret asked to write a fingerprint it does not have")
	}
	if !strings.Contains(m.statusMessage, "Gitleaks") || cmd == nil {
		t.Errorf("statusMessage = %q with cmd %v, want a reason and a timer (Rule 128)",
			m.statusMessage, cmd != nil)
	}
}

// The details pane is a scrollable viewport, and a long finding is the normal
// case — a description plus references routinely runs past the terminal.
func TestTheDetailsPaneScrolls(t *testing.T) {
	tall := detailsModel(t)
	tall.detailsViewport.Height = 10
	tall.detailsViewport.SetContent(strings.Repeat("a line of the description\n", 40))

	for _, tc := range []struct {
		name string
		keys []string
		want int
	}{
		{"down moves one line", []string{"down"}, 1},
		{"j does the same", []string{"j"}, 1},
		{"back up returns to the top", []string{"down", "down", "up", "up"}, 0},
		{"a half page moves further than a line", []string{"pgdown"}, 5},
		{"G goes to the bottom", []string{"G"}, tall.detailsViewport.TotalLineCount() - 10},
		{"g comes back", []string{"G", "g"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, tall, testutil.Keys(tc.keys...)...)

			if got := m.detailsViewport.YOffset; got != tc.want {
				t.Errorf("offset = %d after %v, want %d", got, tc.keys, tc.want)
			}
			if m.state != StateDetails {
				t.Errorf("scrolling left the details for %v", m.state)
			}
		})
	}
}
