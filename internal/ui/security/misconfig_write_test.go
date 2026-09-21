package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

const fixableDockerfile = "FROM debian:12\nRUN apt-get install -y curl\nENTRYPOINT [\"/app\"]\n"

// misconfigModel opens the Misconfigurations tab on a real directory holding one
// Dockerfile and one misconfiguration finding about it.
//
// The file is real because a write is what these tests are about: a fixture that
// only existed in memory would let the write path pass while never touching a
// disk it cannot reach.
func misconfigModel(t *testing.T, content, ruleID string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return misconfigModelOn(t, dir, ruleID, scan.TargetDirectory), dir
}

func misconfigModelOn(t *testing.T, target, ruleID string, kind scan.TargetType) Model {
	t.Helper()
	result := &scan.Result{
		Target: target, TargetType: kind,
		Findings: []scan.Finding{{
			ID: ruleID, Title: "Image user should not be root", Severity: scan.SeverityHigh,
			Source: scan.SourceTrivyMisconfig, File: "Dockerfile", Line: 1, EndLine: 3,
		}},
	}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

// fixFile is the Dockerfile as it stands on disk.
func fixFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// writeFix presses the write key, runs the prepare Cmd, and answers the modal
// yes — the whole path a user walks, with nothing skipped.
func writeFix(t *testing.T, m Model, dir string) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))
	if m.confirmModal == nil {
		t.Fatal("no confirmation opened")
	}
	m, cmd = step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	return feed(t, m, run(t, cmd))
}

// ── Availability (Rule 130) ──────────────────────────────────────────────────

// The key set does not change from one tab to another: what changes is whether
// the entry is greyed, and what it is labelled.
func TestTheWriteKeyIsPresentOnEveryResultsTab(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	for tab := range tabCount {
		m.activeTab = tab
		if !testutil.HasShortcut(m.GetShortcuts(), writeRemediationKey) {
			t.Errorf("tab %d does not offer %s", tab, writeRemediationKey)
		}
	}
}

// Off the Misconfigurations tab the key still belongs to the Remediation one,
// so the refusal names a tab rather than saying nothing.
func TestOutsideTheMisconfigTabTheFixRefusesWithAReason(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m.activeTab = TabCVE
	if got := m.canFixMisconfig(); got.Enabled() || got.Reason != reasonNotMisconfigTab {
		t.Errorf("reason = %q, want %q", got.Reason, reasonNotMisconfigTab)
	}
}

// An image's files are inside the image. Offering a write for them would send
// the user to a path that is not on disk.
func TestAnImageTargetCannotBeFixed(t *testing.T) {
	m := misconfigModelOn(t, "app:1.0", "AVD-DS-0002", scan.TargetImage)
	if got := m.canFixMisconfig(); got.Enabled() || got.Reason != reasonFileNotOnDisk {
		t.Errorf("reason = %q, want %q", got.Reason, reasonFileNotOnDisk)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), writeRemediationKey) {
		t.Error("the write key is not greyed on an image target")
	}
}

// A rule outside the catalog is the ordinary case. Greying it — rather than
// hiding it or offering it — is what makes the boundary between the built-in
// fixes and the agent path visible.
func TestAnUncataloguedRuleIsGreyedNotHidden(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0026")
	if got := m.canFixMisconfig(); got.Enabled() || got.Reason != remediation.ReasonNoFixForRule {
		t.Errorf("reason = %q, want %q", got.Reason, remediation.ReasonNoFixForRule)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), writeRemediationKey) {
		t.Error("an uncatalogued rule leaves the write key lit")
	}
}

func TestACataloguedRuleLightsTheWriteKey(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	if got := m.canFixMisconfig(); !got.Enabled() {
		t.Errorf("declined: %q", got.Reason)
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), writeRemediationKey) {
		t.Error("the write key is greyed on a rule the catalog handles")
	}
}

// The label follows the object: the Misconfigurations tab holds findings about
// files that are not all Dockerfiles.
func TestTheWriteKeyIsLabelledForTheTabItActsOn(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	if got := shortcutDescription(t, m.GetShortcuts(), writeRemediationKey); got != "Apply built-in fix" {
		t.Errorf("description = %q on the Misconfigurations tab", got)
	}
	m.activeTab = TabRemediation
	if got := shortcutDescription(t, m.GetShortcuts(), writeRemediationKey); got != "Write Dockerfile" {
		t.Errorf("description = %q on the Remediation tab", got)
	}
}

func shortcutDescription(t *testing.T, list shortcut.Shortcuts, key string) string {
	t.Helper()
	for _, s := range list {
		if s.Key == key {
			return s.Description
		}
	}
	t.Fatalf("no shortcut %q", key)
	return ""
}

// ── Nothing is written without a confirmation ────────────────────────────────

// Rule 104: the key opens a question, it does not act. The file is untouched
// until the answer is yes.
func TestTheWriteKeyOpensAConfirmationAndWritesNothing(t *testing.T) {
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))

	if m.confirmModal == nil {
		t.Fatal("no confirmation opened")
	}
	if got := fixFile(t, dir); got != fixableDockerfile {
		t.Errorf("the file was written before the confirmation:\n%s", got)
	}
}

func TestAnsweringNoWritesNothing(t *testing.T) {
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))
	m = feed(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if m.misconfigPending != nil {
		t.Error("the pending write survived a No")
	}
	if got := fixFile(t, dir); got != fixableDockerfile {
		t.Errorf("No wrote the file anyway:\n%s", got)
	}
}

// The confirmation shows the diff itself. A block insertion has no one-line
// summary that conveys what is about to land in the file.
func TestTheConfirmationShowsTheDiff(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))

	body := m.confirmModal.View()
	for _, want := range []string{"Dockerfile", "adduser", "USER appuser"} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, body)
		}
	}
}

// ── Writing ──────────────────────────────────────────────────────────────────

func TestAConfirmedFixWritesTheFile(t *testing.T) {
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m = writeFix(t, m, dir)

	got := fixFile(t, dir)
	for _, want := range []string{"ARG UID=10001", "RUN adduser \\", "USER appuser\nENTRYPOINT"} {
		if !strings.Contains(got, want) {
			t.Errorf("the written file lacks %q:\n%s", want, got)
		}
	}
	// What was already there is still there, byte for byte.
	if !strings.HasPrefix(got, fixableDockerfile[:len("FROM debian:12\nRUN apt-get install -y curl\n")]) {
		t.Errorf("the write changed more than it inserted:\n%s", got)
	}
	if m.misconfigPending != nil {
		t.Error("the pending write outlived the write")
	}
}

// The finding on screen is still the one the scan reported: the file changed,
// the result did not. Claiming the rule is cleared would be inferring it.
func TestTheFooterSendsTheUserToARescan(t *testing.T) {
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m = writeFix(t, m, dir)
	if got := m.RenderFooter(160); !strings.Contains(got, "re-scan") {
		t.Errorf("the footer does not mention the re-scan:\n%s", got)
	}
}

// A rule the catalog holds can still decline this instance — and that is a
// warning, not a failure: nothing broke, the fix does not apply here.
func TestAnInstanceTheRuleDeclinesIsAWarningNotAnError(t *testing.T) {
	const alpine = "FROM alpine:3.21\nENTRYPOINT [\"/app\"]\n"
	m, _ := misconfigModel(t, alpine, "AVD-DS-0002")
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))

	if m.confirmModal != nil {
		t.Error("a declined fix opened a confirmation")
	}
	if got := m.RenderFooter(160); !strings.Contains(got, "distribution") {
		t.Errorf("the footer does not carry the rule's own reason:\n%s", got)
	}
}

// The file changed between the preview and the answer: the user confirmed a
// diff that is no longer the one that would be applied, so nothing is written.
func TestAFileChangedSinceThePreviewIsNotWritten(t *testing.T) {
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))

	const changed = "FROM debian:12\nENTRYPOINT [\"/other\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd = step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	m = feed(t, m, run(t, cmd))

	if got := fixFile(t, dir); got != changed {
		t.Errorf("the stale write went through:\n%s", got)
	}
	if got := m.RenderFooter(160); !strings.Contains(got, "changed since the preview") {
		t.Errorf("the footer does not say the file changed:\n%s", got)
	}
}

// An answer about a result that is no longer on screen is dropped rather than
// applied to whatever replaced it.
func TestAPreparedFixForAnotherTargetIsIgnored(t *testing.T) {
	m, _ := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m = feed(t, m, MisconfigFixPreparedMsg{Target: "/somewhere/else", File: preparedWrite{File: "Dockerfile"}})
	if m.confirmModal != nil {
		t.Error("a confirmation opened for another target")
	}
}
