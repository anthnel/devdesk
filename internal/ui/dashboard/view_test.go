package dashboard

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Whole view ───────────────────────────────────────────────────────────────

func TestViewRendersEverySection(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()

	for _, heading := range []string{"GitLab", "Services", "Certificates", "Workspaces", "OCI Resources", "Containers", "Tools"} {
		if !strings.Contains(out, heading) {
			t.Errorf("View() is missing the %q section", heading)
		}
	}
}

// The two columns are stitched line by line rather than with JoinHorizontal
// (Rule 115), so every row must come out the same width — a short row would
// show the terminal's own background.
func TestViewRowsAreUniformWidth(t *testing.T) {
	m, _ := loadedModel(t)

	lines := strings.Split(m.View(), "\n")
	if len(lines) < 5 {
		t.Fatalf("View() produced %d lines, want a full dashboard", len(lines))
	}
	width := len([]rune(lines[0]))
	for i, line := range lines {
		if got := len([]rune(line)); got != width {
			t.Fatalf("line %d is %d runes wide, want %d — the columns are not padded to a common width", i, got, width)
		}
	}
}

func TestViewSurvivesANarrowTerminal(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, testutil.Resize(20, 10))

	if out := m.View(); out == "" {
		t.Error("View() returned nothing on a narrow terminal")
	}
}

// ── Loading and empty states ─────────────────────────────────────────────────

func TestEverySectionReportsItsOwnLoadingState(t *testing.T) {
	m, _ := authenticatedModel(t) // nothing loaded yet

	out := m.View()
	if count := strings.Count(out, "Loading..."); count < 4 {
		t.Errorf("View() shows %d loading placeholders, want one per pending section", count)
	}
	if !strings.Contains(out, "Detecting...") {
		t.Error("the tools section does not report detection in progress")
	}
}

// Sections settle independently: one slow fetch must not hold the others back.
func TestSectionsSettleIndependently(t *testing.T) {
	m, _ := authenticatedModel(t)

	m = feed(t, m, DockerStatsMsg{Stats: shared.DockerStats{Available: true, Running: 2}})

	out := m.View()
	if !strings.Contains(out, "running") {
		t.Error("the containers section did not render once its own result arrived")
	}
	if !strings.Contains(out, "Loading...") {
		t.Error("the sections still waiting stopped showing their placeholder")
	}
}

func TestGitLabSectionPromptsWhenSignedOut(t *testing.T) {
	m, _ := newTestModel(t)

	out := m.View()
	if !strings.Contains(out, "Not connected") {
		t.Error("the GitLab section does not report a missing session")
	}
	if !strings.Contains(out, "gitlab-auth") {
		t.Error("the GitLab section does not point at the command that fixes it")
	}
}

func TestGitLabSectionShowsTheUserAndCounts(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	if !strings.Contains(out, "anthoni") {
		t.Error("the GitLab section does not name the signed-in user")
	}
	if !strings.Contains(out, "Merge Requests") || !strings.Contains(out, "Issues") {
		t.Error("the GitLab section does not show the MR and issue counts")
	}
}

func TestServicesSectionCountsByStatus(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	// Fixtures: one OK, one down, one warning among the non-SSL monitors.
	for _, want := range []string{"1 OK", "1 Down", "1 Error"} {
		if !strings.Contains(out, want) {
			t.Errorf("the services section is missing %q:\n%s", want, out)
		}
	}
}

// Zero counters are omitted rather than rendered as "0 Down", so a healthy
// dashboard stays quiet.
func TestServicesSectionOmitsZeroCounters(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, StatusCheckMsg{Result: status.MonitorResult{
		Components: []status.ComponentStatus{{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK}},
	}})

	out := m.View()
	if !strings.Contains(out, "1 OK") {
		t.Error("the services section does not report the healthy monitor")
	}
	if strings.Contains(out, "Down") || strings.Contains(out, "Error") {
		t.Errorf("the services section rendered empty counters:\n%s", out)
	}
}

func TestServicesSectionPromptsWhenNothingIsConfigured(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, StatusCheckMsg{Result: status.MonitorResult{Components: nil}})

	out := m.View()
	if !strings.Contains(out, "No monitors configured") {
		t.Error("the services section does not report an empty configuration")
	}
	if !strings.Contains(out, "No SSL monitors") {
		t.Error("the certificates section does not report an empty configuration")
	}
}

func TestCertificatesSectionCountsByStatus(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	if !strings.Contains(out, "1 valid") {
		t.Error("the certificates section does not report the valid certificate")
	}
	if !strings.Contains(out, "1 expired") {
		t.Error("the certificates section does not report the expired certificate")
	}
}

func TestWorkspacesSectionShowsCountPathAndSize(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	if !strings.Contains(out, "6 workspaces") {
		t.Error("the workspaces section does not show the count")
	}
	if !strings.Contains(out, "4.2G") {
		t.Error("the workspaces section does not show the disk usage")
	}
	if !strings.Contains(out, "~/workspaces") {
		t.Error("the workspaces section does not show the configured directory")
	}
}

// du is not available everywhere, and an empty size must simply be omitted.
func TestWorkspacesSectionOmitsAnUnknownSize(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, WorkspaceStatsMsg{Count: 2, DiskSize: ""})

	if out := m.View(); !strings.Contains(out, "2 workspaces") || strings.Contains(out, "()") {
		t.Errorf("the workspaces section rendered an empty size:\n%s", out)
	}
}

func TestDockerSectionsReportUnavailability(t *testing.T) {
	m, _ := authenticatedModel(t)

	m = feed(t, m,
		DockerStatsMsg{Stats: shared.DockerStats{Available: false}},
		OCIStatsMsg{Stats: shared.OCIStats{Available: false}},
	)

	if count := strings.Count(m.View(), "Docker not available"); count != 2 {
		t.Errorf("%d sections reported Docker as unavailable, want both the OCI and containers cards", count)
	}
}

func TestContainersSectionCountsByState(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	if !strings.Contains(out, "4 containers") {
		t.Error("the containers section does not show the total")
	}
	for _, want := range []string{"2 running", "1 stopped", "1 paused"} {
		if !strings.Contains(out, want) {
			t.Errorf("the containers section is missing %q", want)
		}
	}
}

func TestContainersSectionOmitsZeroStates(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, DockerStatsMsg{Stats: shared.DockerStats{Available: true, Running: 3}})

	out := m.View()
	if !strings.Contains(out, "3 running") {
		t.Error("the containers section does not show the running count")
	}
	if strings.Contains(out, "stopped") || strings.Contains(out, "paused") {
		t.Errorf("the containers section rendered empty states:\n%s", out)
	}
}

func TestOCISectionShowsCountsAndSizes(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	for _, want := range []string{"Images", "Containers", "Volumes", "1.2GB", "300MB", "50MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("the OCI section is missing %q", want)
		}
	}
}

func TestToolsSectionMarksAvailability(t *testing.T) {
	m, _ := loadedModel(t)

	out := m.View()
	for _, tool := range []string{"Docker", "Trivy", "Gitleaks"} {
		if !strings.Contains(out, tool) {
			t.Errorf("the tools section is missing %q", tool)
		}
	}
	if !strings.Contains(out, "✓ Docker") {
		t.Error("an available tool is not marked with a tick")
	}
	if !strings.Contains(out, "✗ Gitleaks") {
		t.Error("a missing tool is not marked with a cross")
	}
}

// Names are padded to a common width so the ticks line up.
func TestToolsSectionAlignsNames(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, ToolsDetectedMsg{Tools: []shared.ToolInfo{
		{Name: "Git", Available: true},
		{Name: "Gitleaks", Available: true},
	}})

	out := m.View()
	if !strings.Contains(out, "Git     ") {
		t.Errorf("the shorter tool name is not padded to the longest:\n%s", out)
	}
}

func TestToolsSectionReportsAnEmptyDetection(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, ToolsDetectedMsg{Tools: nil})

	if !strings.Contains(m.View(), "No tools detected") {
		t.Error("the tools section does not report an empty detection")
	}
}

// ── Header, footer and help ──────────────────────────────────────────────────

// Rule 130: the browser shortcuts only work with a session, so they must not be
// advertised without one.
func TestShortcutsFollowAuthentication(t *testing.T) {
	m, _ := newTestModel(t)

	signedOut := m.GetShortcuts()
	if hasShortcut(signedOut, "m") || hasShortcut(signedOut, "i") {
		t.Error("the browser shortcuts are advertised without a session")
	}
	if !hasShortcut(signedOut, "ctrl+r") {
		t.Error("refresh is not advertised")
	}

	authenticated, _ := authenticatedModel(t)
	signedIn := authenticated.GetShortcuts()
	if !hasShortcut(signedIn, "m") || !hasShortcut(signedIn, "i") {
		t.Error("the browser shortcuts are missing for a signed-in user")
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	m, _ := authenticatedModel(t)

	for _, s := range m.GetShortcuts() {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return true
		}
	}
	return false
}

func TestGetTitleAndIcon(t *testing.T) {
	m, _ := loadedModel(t)

	if !strings.Contains(m.GetTitle(), "Dashboard") {
		t.Errorf("GetTitle() = %q, want it to name the view", m.GetTitle())
	}
	if m.GetIcon() != "" {
		t.Errorf("GetIcon() = %q, want empty — the title carries the icon", m.GetIcon())
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	m, _ := loadedModel(t)

	info := m.GetHeaderInfo("work")
	if len(info) != 1 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v, want the active context", info)
	}
}

func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	m, _ := loadedModel(t)

	want := m.GetFooterHeight()
	got := strings.Count(m.RenderFooter(120), "\n") + 1
	if got != want {
		t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
	}
}

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := loadedOnly(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 {
		t.Error("the help content lists no key bindings")
	}
}

func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m, _ := authenticatedModel(t)
	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[kb.Key] = true
	}

	for _, s := range m.GetShortcuts() {
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

// loadedOnly drops the shared-state return for the tests that do not need it.
func loadedOnly(t *testing.T) Model {
	t.Helper()
	m, _ := loadedModel(t)
	return m
}
