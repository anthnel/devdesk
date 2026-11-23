package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-751 (step 5): ctrl+o from the detail is the Misconfigurations tab's
// ctrl+o — the same preview, the same confirmation that defaults to No, the same
// write, the same re-scan — and the detail is rebuilt on what the re-scan found,
// or closed with the reason the footer already gives. Everything changes in
// Update (Rule 110): these tests feed messages and never touch a goroutine.

// fixDetail is the detail of a DS002 finding about a real Dockerfile.
func fixDetail(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatal("enter did not open the detail")
	}
	return m, dir
}

// reScanned is the finding as a re-scan reports it after the fix moved it.
func reScanned(m Model) scan.Finding {
	f := *m.selectedFinding
	f.Line++
	f.Description = "Reworded after the fix"
	return f
}

func resultOn(dir string, findings ...scan.Finding) *scan.Result {
	return &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: findings}
}

// preparedFrom presses ctrl+o and runs the preparation, as the user and the
// runtime would.
func preparedFrom(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	return feed(t, m, run(t, cmd))
}

// ── The same confirmation ────────────────────────────────────────────────────

func TestCtrlOInTheDetailOpensTheConfirmationOfTheResultsState(t *testing.T) {
	results, dir := misconfigModel(t, fixableDockerfile, "AVD-DS-0002")
	detail := feed(t, results, testutil.Key("enter"))

	fromResults := preparedFrom(t, results)
	fromDetail := preparedFrom(t, detail)

	if fromResults.confirmModal == nil || fromDetail.confirmModal == nil {
		t.Fatalf("no confirmation (results %v, detail %v)", fromResults.confirmModal != nil, fromDetail.confirmModal != nil)
	}
	if got, want := fromDetail.confirmModal.View(), fromResults.confirmModal.View(); got != want {
		t.Errorf("the confirmations differ:\nresults:\n%s\ndetail:\n%s", want, got)
	}
	if fromDetail.state != StateDetails {
		t.Error("ctrl+o left the detail")
	}
	if got := fixFile(t, dir); got != fixableDockerfile {
		t.Errorf("the file was written before the answer:\n%s", got)
	}
}

func TestAnsweringNoInTheDetailLeavesTheFileAndTheDetailAlone(t *testing.T) {
	m, dir := fixDetail(t)
	m = feed(t, preparedFrom(t, m), components.ConfirmModalNoMsg{})
	if m.confirmModal != nil || m.misconfigPending != nil {
		t.Error("the pending write outlived a No")
	}
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Error("No left the detail")
	}
	if got := fixFile(t, dir); got != fixableDockerfile {
		t.Errorf("No wrote the file:\n%s", got)
	}
}

// A rule that declines this instance when the file is read is an answer, not a
// failure: a warning, and no confirmation.
func TestAFixTheRuleDeclinesFromTheDetailIsAWarning(t *testing.T) {
	const alpine = "FROM alpine:3.21\nENTRYPOINT [\"/app\"]\n"
	m, _ := misconfigModel(t, alpine, "AVD-DS-0002")
	m = preparedFrom(t, feed(t, m, testutil.Key("enter")))
	if m.confirmModal != nil {
		t.Error("a declined fix opened a confirmation")
	}
	if m.footer.Level() != components.LevelWarning || !strings.Contains(m.footer.Text(), "distribution") {
		t.Errorf("footer = %q (level %v), want the rule's own reason as a warning", m.footer.Text(), m.footer.Level())
	}
}

// ── Writing, and the status while it is checked ──────────────────────────────

func TestConfirmingInTheDetailWritesTheFileAndStartsTheVerification(t *testing.T) {
	m, dir := fixDetail(t)
	m = writeFix(t, m, dir)

	if got := fixFile(t, dir); !strings.Contains(got, "USER appuser") {
		t.Errorf("the file was not written:\n%s", got)
	}
	if m.misconfigVerifying == nil || m.misconfigVerifying.Target != dir {
		t.Fatalf("no verification is pending for %s: %+v", dir, m.misconfigVerifying)
	}
	if m.state != StateDetails {
		t.Error("the write left the detail before the verdict")
	}
}

// Only the verification re-scan is announced, as a state of the screen with the
// shared activity dot (Rule 128), not the preparation of the fix. The Info the
// write sets expires after three seconds; the status is what stays.
func TestTheDetailFooterSaysRescanningWhileTheVerificationRuns(t *testing.T) {
	m, dir := fixDetail(t)
	m = writeFix(t, m, dir)
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

	st := m.status()
	if !strings.Contains(st.Text, "Re-scanning") || !st.Activity {
		t.Errorf("status = %+v, want a \"Re-scanning\" text with Activity", st)
	}
	if got := footerText(m, 160); !strings.Contains(got, "Re-scanning") {
		t.Errorf("footer = %q, want it to say Re-scanning", got)
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink() is false while the footer draws the activity dot: the dot would stay fixed")
	}
}

func TestTheRescanStatusIsOnlyForTheTargetBeingVerified(t *testing.T) {
	m, _ := fixDetail(t)
	if _, ok := m.verifyingStatus(); ok {
		t.Error("a detail with no verification says it is re-scanning")
	}
	m.misconfigVerifying = &misconfigVerify{Target: "/somewhere/else", Rule: "AVD-DS-0002"}
	if _, ok := m.verifyingStatus(); ok {
		t.Error("a verification of another target is announced on this detail")
	}
	m.misconfigVerifying = &misconfigVerify{Target: m.targetPath, Rule: "AVD-DS-0002"}
	st, ok := m.verifyingStatus()
	if !ok || !st.Activity || !strings.Contains(st.Text, "Re-scanning") {
		t.Errorf("verifyingStatus() = (%+v, %v), want Re-scanning with Activity", st, ok)
	}
}

func TestPreparingTheFixDoesNotAnnounceARescan(t *testing.T) {
	m, _ := fixDetail(t)
	m = preparedFrom(t, m)
	if strings.Contains(m.status().Text, "Re-scanning") || m.status().Activity {
		t.Errorf("status = %+v before anything was written", m.status())
	}
}

// ── What the verdict does to the detail ──────────────────────────────────────

func TestAFixThatClearsTheFindingClosesTheDetailWithItsReason(t *testing.T) {
	m, dir := fixDetail(t)
	m = writeFix(t, m, dir)
	m = feed(t, m, MisconfigVerifiedMsg{
		Target: dir, Rule: "AVD-DS-0002", File: "Dockerfile", Cleared: true, Result: resultOn(dir),
	})

	if m.state != StateResults || m.selectedFinding != nil {
		t.Errorf("state = %v, selectedFinding = %v: the detail of a finding that is gone must close", m.state, m.selectedFinding)
	}
	if !strings.Contains(footerText(m, 160), "cleared") {
		t.Errorf("the footer does not say why the detail closed:\n%s", footerText(m, 160))
	}
	if n := len(m.findingsTable.Items()); n != 0 {
		t.Errorf("the table still lists %d finding(s)", n)
	}
}

// A rule that survives its own fix keeps the detail open on what the re-scan
// found: the new line, the new wording. A stale detail would contradict the
// warning in the footer.
func TestAFindingThatSurvivesIsShownAgainAsTheRescanReadsIt(t *testing.T) {
	m, dir := fixDetail(t)
	m = writeFix(t, m, dir)
	again := reScanned(m)
	m = feed(t, m, MisconfigVerifiedMsg{
		Target: dir, Rule: "AVD-DS-0002", File: "Dockerfile", Cleared: false, Result: resultOn(dir, again),
	})

	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatalf("state = %v, selectedFinding = %v: the finding is still reported, the detail stays", m.state, m.selectedFinding)
	}
	if m.selectedFinding.Line != again.Line || m.selectedFinding.Description != again.Description {
		t.Errorf("selectedFinding = line %d %q, want the re-scan's line %d %q",
			m.selectedFinding.Line, m.selectedFinding.Description, again.Line, again.Description)
	}
	content := ansi.Strip(m.detailsViewport.GetContent())
	if !strings.Contains(content, "Reworded after the fix") || !strings.Contains(content, "Dockerfile:2") {
		t.Errorf("the viewport still shows the old reading:\n%s", content)
	}
	if strings.Contains(content, "Dockerfile:1") {
		t.Errorf("the viewport still shows the old line:\n%s", content)
	}
	if !strings.Contains(footerText(m, 160), "still reported") {
		t.Errorf("the footer does not carry the warning:\n%s", footerText(m, 160))
	}
	if m.misconfigVerifying != nil || strings.Contains(m.status().Text, "Re-scanning") {
		t.Error("the detail still says it is re-scanning after the verdict")
	}
	if m.footer.Level() != components.LevelWarning {
		t.Errorf("footer level = %v, want a warning", m.footer.Level())
	}
}

// Not cleared and gone from the result (another finding of the file is what the
// re-scan still reports): the detail has nothing to show and says why.
func TestAFindingGoneWithoutBeingClearedClosesTheDetailWithTheWarning(t *testing.T) {
	m, dir := fixDetail(t)
	other := misconfigOf("AVD-DS-0026")
	m = feed(t, m, MisconfigVerifiedMsg{
		Target: dir, Rule: "AVD-DS-0002", File: "Dockerfile", Cleared: false, Result: resultOn(dir, other),
	})
	if m.state != StateResults || m.selectedFinding != nil {
		t.Errorf("state = %v, selectedFinding = %v, want the results and no stale detail", m.state, m.selectedFinding)
	}
	if !strings.Contains(footerText(m, 160), "still reported") {
		t.Errorf("the footer does not give the reason:\n%s", footerText(m, 160))
	}
}

// A verdict that cleared one rule says nothing against the finding on screen when
// the re-scan still lists it.
func TestAClearedVerdictKeepsTheDetailOfAFindingStillListed(t *testing.T) {
	m, dir := fixDetail(t)
	same := *m.selectedFinding
	m = feed(t, m, MisconfigVerifiedMsg{
		Target: dir, Rule: "AVD-DS-0001", File: "Dockerfile", Cleared: true, Result: resultOn(dir, same),
	})
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.ID != same.ID {
		t.Errorf("state = %v, selectedFinding = %v, want the detail kept", m.state, m.selectedFinding)
	}
}

// The detail is about one finding, and the one the re-scan puts back is that
// one — not whatever setResult's GotoTop left under the table's cursor.
func TestARefreshedDetailKeepsItsFindingWhenTheTableGoesBackToTheTop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(fixableDockerfile), 0o644); err != nil {
		t.Fatal(err)
	}
	high := misconfigOf("AVD-DS-0002")
	high.Severity = scan.SeverityHigh
	low := misconfigOf("AVD-DS-0026")
	low.Severity, low.Title, low.Line = scan.SeverityLow, "Add HEALTHCHECK", 2
	result := resultOn(dir, high, low)
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(160, 30))
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	m = selectFinding(t, m, func(f scan.Finding) bool { return f.ID == low.ID })
	m = feed(t, m, testutil.Key("enter"))

	m = feed(t, m, MisconfigVerifiedMsg{
		Target: dir, Rule: high.ID, File: "Dockerfile", Cleared: false, Result: resultOn(dir, high, low),
	})
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.ID != low.ID {
		t.Fatalf("the detail shows %v after the refresh, want %s", m.selectedFinding, low.ID)
	}
	if top, _ := m.findingsTable.Selected(); top.ID == low.ID {
		t.Fatal("the fixture must leave another finding under the table's cursor")
	}
	if got, ok := m.subjectFinding(); !ok || got.ID != low.ID {
		t.Errorf("subjectFinding() = %q, want the detail's %q", got.ID, low.ID)
	}
	if got, want := m.canFixMisconfig().Reason, noFixReason(low); got != want {
		t.Errorf("ctrl+o is decided on %q, want the detail's finding (%q)", got, want)
	}
}

func TestAVerdictThatCouldNotBeReadLeavesTheDetailAsItWas(t *testing.T) {
	m, dir := fixDetail(t)
	before := m.selectedFinding.Line
	m = feed(t, m, MisconfigVerifiedMsg{Target: dir, Rule: "AVD-DS-0002", File: "Dockerfile", Err: os.ErrNotExist})
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.Line != before {
		t.Error("an unreadable verdict changed the detail")
	}
	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want an error", m.footer.Level())
	}
}

func TestAVerdictForAnotherTargetChangesNothingInTheDetail(t *testing.T) {
	m, _ := fixDetail(t)
	before := ansi.Strip(m.detailsViewport.GetContent())
	m = feed(t, m, MisconfigVerifiedMsg{
		Target: "/somewhere/else", Rule: "AVD-DS-0002", File: "Dockerfile", Cleared: true, Result: resultOn("/somewhere/else"),
	})
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatal("a verdict for another target closed the detail")
	}
	if got := ansi.Strip(m.detailsViewport.GetContent()); got != before {
		t.Error("a verdict for another target rebuilt the detail")
	}
}

// ── refreshDetails on its own ────────────────────────────────────────────────

func TestRefreshDetailsRebuildsOnTheResultOnScreen(t *testing.T) {
	m, dir := fixDetail(t)
	again := reScanned(m)
	m.result = resultOn(dir, again)

	if !m.refreshDetails() {
		t.Fatal("refreshDetails() = false for a finding the result still holds")
	}
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.Description != again.Description {
		t.Errorf("state = %v, selectedFinding = %v, want the re-scan's reading", m.state, m.selectedFinding)
	}
	if !strings.Contains(ansi.Strip(m.detailsViewport.GetContent()), again.Description) {
		t.Error("the viewport was not rebuilt")
	}
}

func TestRefreshDetailsClosesWhenTheFindingIsGone(t *testing.T) {
	m, dir := fixDetail(t)
	m.result = resultOn(dir)

	if m.refreshDetails() {
		t.Fatal("refreshDetails() = true for a finding the result no longer holds")
	}
	if m.state != StateResults || m.selectedFinding != nil {
		t.Errorf("state = %v, selectedFinding = %v, want the results and no finding", m.state, m.selectedFinding)
	}
}

// Outside the details state there is nothing to rebuild or to close.
func TestRefreshDetailsDoesNothingInTheResultsState(t *testing.T) {
	m := scannedModel(t)
	if m.refreshDetails() {
		t.Error("refreshDetails() reports an open detail in the results state")
	}
	if m.state != StateResults {
		t.Errorf("state = %v, want the results", m.state)
	}
}

// ── A failed write ───────────────────────────────────────────────────────────

func TestAFileChangedSinceThePreviewIsAnErrorInTheDetail(t *testing.T) {
	m, dir := fixDetail(t)
	m = preparedFrom(t, m)
	const changed = "FROM debian:12\nENTRYPOINT [\"/other\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	m = feed(t, m, run(t, cmd))

	if m.footer.Level() != components.LevelError || !strings.Contains(m.footer.Text(), "changed since the preview") {
		t.Errorf("footer = %q (level %v), want the failure as an error", m.footer.Text(), m.footer.Level())
	}
	if got := fixFile(t, dir); got != changed {
		t.Errorf("the stale write went through:\n%s", got)
	}
	if m.misconfigVerifying != nil {
		t.Error("a failed write left a verification pending")
	}
	if m.state != StateDetails {
		t.Error("a failed write left the detail")
	}
}
