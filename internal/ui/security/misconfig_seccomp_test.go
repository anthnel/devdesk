package security

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The seccomp fix through the Misconfigurations tab (DEVDESK-688, S-03): the
// same key, the same confirmation that defaults to No and the same write as the
// host namespace fixes, with three differences the tests below pin down.
//
//   - The key is greyed from the finding alone only for KSV-0104 on an explicit
//     Unconfined (criterion 6, Rule 130: nothing reads the file when the header
//     is drawn). Every other refusal needs the manifest, so it comes at the
//     press of the key, as a warning in the footer, with nothing written.
//   - The confirmation carries the run-time warning (criterion 4).
//   - One edit sets the profile for the whole pod, so the re-scan judges the
//     workload that was edited, not the occurrence that was selected
//     (criterion 5).
//
// Footer timers are never run: the footer is read through Text and Level.

const seccompTitle = "Set seccompProfile.type: RuntimeDefault on the pod"

const seccompPod = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  containers:
    - name: app
      image: nginx:1.27
`

// seccompPodFixed is seccompPod after the fix: three lines under spec:.
const seccompPodFixed = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: nginx:1.27
`

// containerLines is the line range of the container block named name, the way
// Trivy reports it: from its "- name:" line to the line before the next entry.
func containerLines(t *testing.T, src, name string) (line, end int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	indent := -1
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		if indent < 0 {
			if trimmed == "- name: "+name {
				indent, line, end = len(l)-len(trimmed), i+1, i+1
			}
			continue
		}
		if strings.TrimSpace(l) == "" || len(l)-len(trimmed) <= indent {
			break
		}
		end = i + 1
	}
	if indent < 0 {
		t.Fatalf("no container %q in:\n%s", name, src)
	}
	return line, end
}

// seccompFinding is a KSV-0030 or KSV-0104 on one container of src, worded and
// placed the way Trivy 0.76.2 does: KSV-0030 names nothing, KSV-0104 names its
// container and workload (kind in lower case), both cover the container's block.
func seccompFinding(t *testing.T, id, src, container, kind, name string) scan.Finding {
	t.Helper()
	line, end := containerLines(t, src, container)
	f := scan.Finding{
		ID: id, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: end, Severity: scan.SeverityMedium,
	}
	switch id {
	case "KSV-0030":
		f.Title = "Runtime/Default Seccomp profile not set"
		f.Message = "Either Pod or Container should set 'securityContext.seccompProfile.type' to 'RuntimeDefault'"
	default:
		f.Title = "Seccomp policies disabled"
		f.Message = `container "` + container + `" of ` + strings.ToLower(kind) + ` "` + name + `" in "demo" namespace should specify a seccomp profile`
		f.SeccompOrigin = "absent"
	}
	return f
}

// seccompModel opens the Misconfigurations tab on a directory holding the
// manifest and the findings about it.
func seccompModel(t *testing.T, manifest string, findings ...scan.Finding) (Model, string) {
	t.Helper()
	m, dir := hostModel(t, manifest, findings[0])
	m.result.Findings = findings
	m.updateFindingsTable()
	return m, dir
}

// selectFinding moves the cursor to the finding that satisfies want.
func selectFinding(t *testing.T, m Model, want func(scan.Finding) bool) Model {
	t.Helper()
	for i, f := range m.findingsTable.Items() {
		if want(f) {
			m.findingsTable.SetCursor(i)
			return m
		}
	}
	t.Fatal("the finding is not in the table")
	return m
}

func ofContainer(id string, f scan.Finding, line int) bool {
	return f.ID == id && f.Line == line
}

// ── Criterion 3: what the finding alone settles is greyed in advance ─────────

// KSV-0104 on an explicit Unconfined is settled by the finding: the key is
// greyed with the S-04 text as its reason, and the file is not read to say so
// (the header calls this on every frame, Rule 130).
func TestKSV0104OnUnconfinedIsGreyedFromTheFindingAlone(t *testing.T) {
	const unconfinedContainer = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        seccompProfile:
          type: Unconfined
`
	f := seccompFinding(t, "KSV-0104", unconfinedContainer, "app", "Pod", "demo")
	f.SeccompOrigin = "unconfined"
	f.References = []string{pssBaselineLn}
	want, ok := remediation.MissingFor(f)
	if !ok || want == "" {
		t.Fatalf("MissingFor = %q, %v; no text to show", want, ok)
	}
	fixable, _ := hostModel(t, hostDaemonSet, hostFinding("KSV-0009", "hostNetwork"))
	wantKeys := testutil.ShortcutKeys(fixable.GetShortcuts())

	m, dir := hostModel(t, unconfinedContainer, f)
	// Gone from disk: nothing but the finding can have answered.
	if err := os.Remove(filepath.Join(dir, "deploy.yaml")); err != nil {
		t.Fatal(err)
	}

	if got := m.canFixMisconfig(); got.Enabled() || got.Reason != want {
		t.Errorf("reason = %q (enabled %v), want the S-04 text %q", got.Reason, got.Enabled(), want)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), writeRemediationKey) {
		t.Error("the write key is not greyed")
	}
	if keys := testutil.ShortcutKeys(m.GetShortcuts()); strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Errorf("keys = %v, want the same set as on a fixable finding: %v", keys, wantKeys)
	}
	m, _ = step(t, m, testutil.Key(writeRemediationKey))
	if m.footer.Text() != want || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q at level %v, want %q as a warning", m.footer.Text(), m.footer.Level(), want)
	}
	if m.confirmModal != nil {
		t.Error("a confirmation opened for a refused fix")
	}
}

// The greying follows the finding, not the file: a KSV-0104 that says absent
// keeps its key lit even when the manifest says Unconfined, and the refusal
// comes at the press of the key (TestAFixTheCatalogDeclinesIsAWarning…).
func TestKSV0104OnAbsentAndKSV0030KeepTheirKeyLit(t *testing.T) {
	for _, id := range []string{"KSV-0030", "KSV-0104"} {
		f := seccompFinding(t, id, seccompPod, "app", "Pod", "demo")
		m, _ := hostModel(t, seccompPod, f)
		if got := m.canFixMisconfig(); !got.Enabled() {
			t.Errorf("%s: declined in advance: %q", id, got.Reason)
		}
		if !testutil.ShortcutEnabled(m.GetShortcuts(), writeRemediationKey) {
			t.Errorf("%s: the write key is greyed", id)
		}
	}
	unknown := seccompFinding(t, "KSV-0104", seccompPod, "app", "Pod", "demo")
	unknown.SeccompOrigin = ""
	m, _ := hostModel(t, seccompPod, unknown)
	if got := m.canFixMisconfig(); !got.Enabled() {
		t.Errorf("KSV-0104 of unknown origin: declined in advance: %q", got.Reason)
	}
}

// ── Criterion 3: what needs the manifest is refused at the press of the key ──

// Each case is a manifest the fix will not touch. The key is lit, the press
// answers in the footer, as a warning, with the reason of the case; no
// confirmation opens, the file and the directory are as they were, and the key
// set is the one of a fixable finding (Rule 130).
func TestAFixTheSeccompCatalogDeclinesIsAWarningAndWritesNothing(t *testing.T) {
	withContainer := func(podSpec, container string) string {
		return "apiVersion: v1\nkind: Pod\nmetadata:\n  name: demo\n" + podSpec + container
	}
	const app = "  containers:\n    - name: app\n      image: nginx:1.27\n"
	tests := []struct {
		name, src, want string
	}{
		{"Unconfined on the pod", withContainer("spec:\n  securityContext:\n    seccompProfile:\n      type: Unconfined\n", app), remediation.ReasonSeccompUnconfined},
		{"Unconfined on a container", withContainer("spec:\n", "  containers:\n    - name: app\n      image: nginx:1.27\n      securityContext:\n        seccompProfile:\n          type: Unconfined\n"), remediation.ReasonSeccompUnconfined},
		{"Unconfined on an init container", withContainer("spec:\n  initContainers:\n    - name: init\n      image: busybox:1\n      securityContext:\n        seccompProfile:\n          type: Unconfined\n", app), remediation.ReasonSeccompUnconfined},
		{"Unconfined on an ephemeral container", withContainer("spec:\n  ephemeralContainers:\n    - name: debug\n      image: busybox:1\n      securityContext:\n        seccompProfile:\n          type: Unconfined\n", app), remediation.ReasonSeccompUnconfined},
		{"Localhost on the pod", withContainer("spec:\n  securityContext:\n    seccompProfile:\n      type: Localhost\n      localhostProfile: p.json\n", app), remediation.ReasonSeccompLocalhost},
		{"Localhost on a container", withContainer("spec:\n", "  containers:\n    - name: app\n      image: nginx:1.27\n      securityContext:\n        seccompProfile:\n          type: Localhost\n          localhostProfile: p.json\n"), remediation.ReasonSeccompLocalhost},
		{"a seccomp annotation", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: demo\n  annotations:\n    container.seccomp.security.alpha.kubernetes.io/app: runtime/default\nspec:\n" + app, remediation.ReasonSeccompAnnotation},
		{"a Windows pod", withContainer("spec:\n  os:\n    name: windows\n", app), remediation.ReasonWindowsPod},
		{"an inline securityContext", withContainer("spec:\n  securityContext: {runAsUser: 1000}\n", app), remediation.ReasonInlineMapping},
		{"a securityContext shared by an alias", "x-context: &ctx\n  runAsUser: 1000\n" + withContainer("spec:\n  securityContext: *ctx\n", app), remediation.ReasonSharedByAlias},
		{"a pod already on RuntimeDefault", withContainer("spec:\n  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n", app), remediation.ReasonSeccompAlreadySet},
	}
	fixable, _ := hostModel(t, hostDaemonSet, hostFinding("KSV-0009", "hostNetwork"))
	wantKeys := testutil.ShortcutKeys(fixable.GetShortcuts())

	for _, tt := range tests {
		for _, id := range []string{"KSV-0030", "KSV-0104"} {
			t.Run(tt.name+"/"+id, func(t *testing.T) {
				f := seccompFinding(t, id, tt.src, "app", "Pod", "demo")
				m, dir := hostModel(t, tt.src, f)
				if got := m.canFixMisconfig(); !got.Enabled() {
					t.Fatalf("greyed in advance with %q: the file decides, the key stays lit", got.Reason)
				}

				m, cmd := step(t, m, testutil.Key(writeRemediationKey))
				m = feed(t, m, run(t, cmd))

				if m.footer.Text() != tt.want || m.footer.Level() != sharedcomponents.LevelWarning {
					t.Errorf("footer = %q at level %v, want %q as a warning", m.footer.Text(), m.footer.Level(), tt.want)
				}
				if m.confirmModal != nil || m.misconfigPending != nil {
					t.Error("a declined fix opened a confirmation")
				}
				if got := manifestOnDisk(t, dir); got != tt.src {
					t.Errorf("a declined fix wrote the file:\n%s", got)
				}
				if names := dirEntries(t, dir); len(names) != 1 || names[0] != "deploy.yaml" {
					t.Errorf("the directory holds %v, want deploy.yaml alone", names)
				}
				if keys := testutil.ShortcutKeys(m.GetShortcuts()); strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
					t.Errorf("keys = %v, want the same set as on a fixable finding: %v", keys, wantKeys)
				}
			})
		}
	}
}

// modalLines are the lines of the confirmation without its frame: the box
// border and the two columns of margin are gone, the indentation of the text
// (and of the diff, after its "+") is kept.
func modalLines(m Model) []string {
	var out []string
	for _, l := range strings.Split(plain(m.confirmModal.View()), "\n") {
		l = strings.TrimRight(l, " │")
		if rest, ok := strings.CutPrefix(l, "│  "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// ── Criteria 1 and 4: the fix is offered, with its warning and its diff ──────

// KSV-0030, KSV-0104 absent and KSV-0104 of unknown origin on a bare pod open
// the confirmation: the title, the run-time warning, then the diff, which only
// adds lines. Nothing is written before the answer, and No leaves the file
// identical byte for byte.
func TestTheSeccompFixConfirmationShowsTheWarningAndTheDiff(t *testing.T) {
	unknown := seccompFinding(t, "KSV-0104", seccompPod, "app", "Pod", "demo")
	unknown.SeccompOrigin = ""
	tests := []struct {
		name string
		f    scan.Finding
	}{
		{"KSV-0030", seccompFinding(t, "KSV-0030", seccompPod, "app", "Pod", "demo")},
		{"KSV-0104 absent", seccompFinding(t, "KSV-0104", seccompPod, "app", "Pod", "demo")},
		{"KSV-0104 unknown origin", unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, ok := remediation.FixFor(tt.f)
			if !ok || !strings.Contains(rule.Warning, "run time") || !strings.Contains(rule.Warning, "re-scan") {
				t.Fatalf("the catalog entry has no run-time warning: %+v", rule)
			}
			m, dir := hostModel(t, seccompPod, tt.f)
			m, cmd := step(t, m, testutil.Key(writeRemediationKey))
			m = feed(t, m, run(t, cmd))

			if m.confirmModal == nil {
				t.Fatalf("no confirmation opened; footer: %q", m.footer.Text())
			}
			lines := modalLines(m)
			for _, want := range []string{
				seccompTitle,
				"Warning: " + rule.Warning,
				"+  securityContext:",
				"+    seccompProfile:",
				"+      type: RuntimeDefault",
			} {
				if !slices.Contains(lines, want) {
					t.Errorf("the confirmation lacks the line %q:\n%s", want, strings.Join(lines, "\n"))
				}
			}
			w := slices.Index(lines, "Warning: "+rule.Warning)
			d := slices.Index(lines, "+  securityContext:")
			if w < 0 || d < w {
				t.Errorf("the warning must come before the diff:\n%s", strings.Join(lines, "\n"))
			}
			for _, l := range lines {
				if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "--- ") {
					t.Errorf("the diff removes a line: %q", l)
				}
			}
			if got := manifestOnDisk(t, dir); got != seccompPod {
				t.Errorf("the file was written before the confirmation:\n%s", got)
			}

			// No: nothing is written, nothing stays pending.
			no := feed(t, m, sharedcomponents.ConfirmModalNoMsg{})
			if no.misconfigPending != nil || no.misconfigVerifying != nil {
				t.Error("a pending write or verification survived a No")
			}
			if got := manifestOnDisk(t, dir); got != seccompPod {
				t.Errorf("No changed the file:\n%s", got)
			}

			// Yes: the three lines, and only those.
			m, cmd = step(t, m, sharedcomponents.ConfirmModalYesMsg{})
			m = feed(t, m, run(t, cmd))
			if got := manifestOnDisk(t, dir); got != seccompPodFixed {
				t.Errorf("the write is not the three lines under spec:\n%s", got)
			}
			if m.misconfigVerifying == nil || m.misconfigVerifying.File != "deploy.yaml" {
				t.Errorf("verification = %+v, want one on deploy.yaml", m.misconfigVerifying)
			}
		})
	}
}

// A pod where a container already has RuntimeDefault and another has nothing
// is a fix too: the pod-level setting changes nothing for the first.
func TestAPartlyCoveredPodGetsTheFix(t *testing.T) {
	const mixed = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  containers:
    - name: a
      image: a:1.0
      securityContext:
        seccompProfile:
          type: RuntimeDefault
    - name: b
      image: b:1.0
`
	m, dir := hostModel(t, mixed, seccompFinding(t, "KSV-0030", mixed, "b", "Pod", "demo"))
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))
	if m.confirmModal == nil {
		t.Fatalf("no confirmation opened; footer: %q", m.footer.Text())
	}
	if got := manifestOnDisk(t, dir); got != mixed {
		t.Errorf("the file was written before the confirmation:\n%s", got)
	}
}

// ── Criterion 5: the re-scan judges the workload that was edited ─────────────

const threeContainers = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: three
spec:
  selector:
    matchLabels: {app: three}
  template:
    metadata:
      labels: {app: three}
    spec:
      containers:
        - name: a
          image: a:1.0
        - name: b
          image: b:1.0
        - name: c
          image: c:1.0
---
apiVersion: v1
kind: Pod
metadata:
  name: other
spec:
  containers:
    - name: solo
      image: solo:1.0
`

// threeContainersFixed is threeContainers after the fix on the Deployment: the
// block lands under its pod spec, and the Pod of the second document is as it
// was.
var threeContainersFixed = strings.Replace(threeContainers,
	"    spec:\n      containers:",
	"    spec:\n      securityContext:\n        seccompProfile:\n          type: RuntimeDefault\n      containers:", 1)

// threeContainersFindings is what the scan reports on threeContainers: both
// rules on each of the three containers of the Deployment, and both on the Pod
// of the second document.
func threeContainersFindings(t *testing.T, src string) []scan.Finding {
	t.Helper()
	var list []scan.Finding
	for _, c := range []string{"a", "b", "c"} {
		list = append(list,
			seccompFinding(t, "KSV-0030", src, c, "Deployment", "three"),
			seccompFinding(t, "KSV-0104", src, c, "Deployment", "three"))
	}
	return append(list,
		seccompFinding(t, "KSV-0030", src, "solo", "Pod", "other"),
		seccompFinding(t, "KSV-0104", src, "solo", "Pod", "other"))
}

// isolateScanCache points the scan cache the verification reads at an empty
// directory of the test.
func isolateScanCache(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// confirmedSeccompFix selects the finding, confirms the fix and returns the
// model with its verification pending, and the directory.
func confirmedSeccompFix(t *testing.T, id, container string) (Model, string) {
	t.Helper()
	findings := threeContainersFindings(t, threeContainers)
	m, dir := seccompModel(t, threeContainers, findings...)
	if n := len(m.findingsTable.Items()); n != 8 {
		t.Fatalf("the table holds %d findings, want 8", n)
	}
	line, _ := containerLines(t, threeContainers, container)
	m = selectFinding(t, m, func(f scan.Finding) bool { return ofContainer(id, f, line) })
	m = writeFix(t, m, dir)
	if got := manifestOnDisk(t, dir); got != threeContainersFixed {
		t.Fatalf("the write is not the block under the pod spec of the Deployment:\n%s", got)
	}
	if m.misconfigVerifying == nil {
		t.Fatal("no verification pending after the write")
	}
	return m, dir
}

// verdict stores the re-scan result the way the scan does and reads it the way
// the verification does.
func verdict(t *testing.T, m Model, dir string, findings []scan.Finding) MisconfigVerifiedMsg {
	t.Helper()
	result := &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: findings}
	if err := cache.SaveWorkspaceScanResult(dir, result); err != nil {
		t.Fatal(err)
	}
	msg, ok := run(t, verifyMisconfigCmd(*m.misconfigVerifying)).(MisconfigVerifiedMsg)
	if !ok {
		t.Fatal("the verification did not answer with a MisconfigVerifiedMsg")
	}
	return msg
}

// The three findings of the three containers and the three of the other rule
// are one fix: confirming one of them closes the pod for both rules, so the
// scope the verification is given is the Deployment's document, and both rules.
func TestTheSeccompFixIsVerifiedOverTheWholeWorkload(t *testing.T) {
	starts := []struct{ id, container string }{
		{"KSV-0030", "b"}, {"KSV-0030", "a"}, {"KSV-0104", "a"}, {"KSV-0104", "c"},
	}
	for _, st := range starts {
		t.Run(st.id+" of "+st.container, func(t *testing.T) {
			isolateScanCache(t)
			m, _ := confirmedSeccompFix(t, st.id, st.container)
			scope := m.misconfigVerifying.Scope

			rules := slices.Clone(scope.Rules)
			slices.Sort(rules)
			if strings.Join(rules, ",") != "KSV-0030,KSV-0104" {
				t.Errorf("scope rules = %v, want KSV-0030 and KSV-0104", scope.Rules)
			}
			// The Deployment is document 1: it ends before the "---", and the
			// Pod of document 2 starts after it, in the fixed content.
			lines := strings.Split(threeContainersFixed, "\n")
			sep := slices.Index(lines, "---") + 1
			if scope.Line != 1 || scope.EndLine < sep-1 || scope.EndLine >= sep+1 {
				t.Errorf("scope lines = %d-%d, want the Deployment's document, 1 to %d (the separator is line %d)",
					scope.Line, scope.EndLine, sep-1, sep)
			}
		})
	}
}

// A pod with three containers: the fix confirmed from one finding is judged
// done when the re-scan reports none of the six findings of the pod, even
// though another pod of the same file is still reported with the same
// KSV-0030 text. Whichever finding the fix started from.
func TestAThreeContainerPodIsClearedByOneConfirmedFinding(t *testing.T) {
	starts := []struct{ id, container string }{
		{"KSV-0030", "b"}, {"KSV-0030", "a"}, {"KSV-0104", "a"}, {"KSV-0104", "c"},
	}
	for _, st := range starts {
		t.Run(st.id+" of "+st.container, func(t *testing.T) {
			isolateScanCache(t)
			m, dir := confirmedSeccompFix(t, st.id, st.container)

			// What a re-scan of the fixed file reports: the Pod of the second
			// document, its lines moved by the three lines the fix added.
			other := []scan.Finding{
				seccompFinding(t, "KSV-0030", threeContainersFixed, "solo", "Pod", "other"),
				seccompFinding(t, "KSV-0104", threeContainersFixed, "solo", "Pod", "other"),
			}
			msg := verdict(t, m, dir, other)
			if !msg.Cleared || len(msg.Still) != 0 {
				t.Fatalf("the pod was fixed and still reads as open: Cleared %v, Still %v", msg.Cleared, msg.Still)
			}

			m = feed(t, m, msg)
			if m.misconfigVerifying != nil {
				t.Error("the verification is still pending after its verdict")
			}
			if got, want := m.footer.Text(), "KSV-0030 and KSV-0104 cleared for the whole pod in deploy.yaml — confirmed by a re-scan"; got != want {
				t.Errorf("footer = %q, want %q", got, want)
			}
			if m.footer.Level() != sharedcomponents.LevelInfo {
				t.Errorf("footer level = %v, want info", m.footer.Level())
			}
			// The table is the re-scan's: the six findings of the pod are gone,
			// the two of the other pod are not.
			if n := len(m.findingsTable.Items()); n != 2 {
				t.Errorf("the table holds %d findings, want the 2 of the other pod", n)
			}
		})
	}
}

// A finding of the edited pod that survives keeps the fix open, whichever rule
// it is and whichever container it is on: the warning names what is left.
func TestAFindingOfTheFixedPodThatSurvivesIsReported(t *testing.T) {
	tests := []struct {
		name  string
		left  func(t *testing.T) []scan.Finding
		still []string
		text  string
	}{
		{"a KSV-0104 on b", func(t *testing.T) []scan.Finding {
			return []scan.Finding{seccompFinding(t, "KSV-0104", threeContainersFixed, "b", "Deployment", "three")}
		}, []string{"KSV-0104"}, "KSV-0104 still reported in deploy.yaml after the fix"},
		{"a KSV-0030 on c", func(t *testing.T) []scan.Finding {
			return []scan.Finding{seccompFinding(t, "KSV-0030", threeContainersFixed, "c", "Deployment", "three")}
		}, []string{"KSV-0030"}, "KSV-0030 still reported in deploy.yaml after the fix"},
		{"both rules", func(t *testing.T) []scan.Finding {
			return []scan.Finding{
				seccompFinding(t, "KSV-0030", threeContainersFixed, "a", "Deployment", "three"),
				seccompFinding(t, "KSV-0104", threeContainersFixed, "a", "Deployment", "three"),
			}
		}, []string{"KSV-0030", "KSV-0104"}, "still reported in deploy.yaml after the fix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateScanCache(t)
			m, dir := confirmedSeccompFix(t, "KSV-0030", "b")
			// The surviving finding, and the other pod as always.
			findings := append(tt.left(t),
				seccompFinding(t, "KSV-0030", threeContainersFixed, "solo", "Pod", "other"))
			msg := verdict(t, m, dir, findings)

			if msg.Cleared {
				t.Fatalf("a finding of the fixed pod is still reported and the verdict is cleared")
			}
			got := slices.Clone(msg.Still)
			slices.Sort(got)
			if !slices.Equal(got, tt.still) {
				t.Errorf("Still = %v, want %v", msg.Still, tt.still)
			}
			m = feed(t, m, msg)
			if !strings.Contains(m.footer.Text(), tt.text) || m.footer.Level() != sharedcomponents.LevelWarning {
				t.Errorf("footer = %q at level %v, want a warning with %q", m.footer.Text(), m.footer.Level(), tt.text)
			}
		})
	}
}

// A re-scan that finds the file as it was (the same findings) is the verdict
// "not fixed", for the two rules.
func TestARescanThatChangedNothingLeavesBothRulesOpen(t *testing.T) {
	isolateScanCache(t)
	m, dir := confirmedSeccompFix(t, "KSV-0030", "b")
	msg := verdict(t, m, dir, threeContainersFindings(t, threeContainersFixed))

	got := slices.Clone(msg.Still)
	slices.Sort(got)
	if msg.Cleared || !slices.Equal(got, []string{"KSV-0030", "KSV-0104"}) {
		t.Errorf("Cleared %v, Still %v, want both rules still reported", msg.Cleared, msg.Still)
	}
}

// ── The judgment itself ──────────────────────────────────────────────────────

// stillReported is the verdict: which of the rules of the scope the result
// still reports in the lines of the edited workload. Without a scope it is the
// judgment of every other rule, unchanged; without lines it is the whole file,
// the safe side: a verdict of "still reported" at worst, never a "fixed" that
// was not measured.
func TestStillReportedJudgesByScope(t *testing.T) {
	const file = "deploy.yaml"
	both := []string{"KSV-0030", "KSV-0104"}
	at := func(id string, line int) scan.Finding {
		return scan.Finding{ID: id, Source: scan.SourceTrivyMisconfig, File: file, Line: line, EndLine: line + 2}
	}
	in := func(f scan.Finding, file string) scan.Finding { f.File = file; return f }

	tests := []struct {
		name     string
		scope    remediation.Scope
		findings []scan.Finding
		want     []string
	}{
		{"nothing reported", remediation.Scope{Rules: both, Line: 1, EndLine: 20}, nil, nil},
		{"a finding of the scope inside the lines",
			remediation.Scope{Rules: both, Line: 1, EndLine: 20},
			[]scan.Finding{at("KSV-0104", 12)}, []string{"KSV-0104"}},
		{"the same finding after the lines is another workload's",
			remediation.Scope{Rules: both, Line: 1, EndLine: 20},
			[]scan.Finding{at("KSV-0030", 25), at("KSV-0104", 25)}, nil},
		{"a finding that starts before the lines is another workload's",
			remediation.Scope{Rules: both, Line: 10, EndLine: 20},
			[]scan.Finding{at("KSV-0030", 4)}, nil},
		{"the first and the last line count",
			remediation.Scope{Rules: both, Line: 10, EndLine: 20},
			[]scan.Finding{at("KSV-0030", 10), at("KSV-0104", 20)}, []string{"KSV-0030", "KSV-0104"}},
		{"another file is not this one",
			remediation.Scope{Rules: both, Line: 1, EndLine: 20},
			[]scan.Finding{in(at("KSV-0030", 5), "svc/deploy.yaml")}, nil},
		{"a rule outside the scope does not count",
			remediation.Scope{Rules: both, Line: 1, EndLine: 20},
			[]scan.Finding{at("KSV-0001", 5)}, nil},
		{"the other spellings of a rule count",
			remediation.Scope{Rules: both, Line: 1, EndLine: 20},
			[]scan.Finding{at("AVD-KSV-0104", 5), at("KSV30", 6)}, []string{"KSV-0030", "KSV-0104"}},
		{"no lines: the whole file, so another pod counts",
			remediation.Scope{Rules: both},
			[]scan.Finding{at("KSV-0030", 25)}, []string{"KSV-0030"}},
		{"no lines: the whole file, but not another file",
			remediation.Scope{Rules: both},
			[]scan.Finding{in(at("KSV-0030", 25), "other.yaml")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := misconfigVerify{Rule: "KSV-0030", File: file, Scope: tt.scope}
			got := slices.Clone(stillReported(&scan.Result{Findings: tt.findings}, v))
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("stillReported = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every other rule keeps the judgment on the occurrence that was fixed: no
// scope, the same file, the same title and message.
func TestStillReportedWithoutAScopeJudgesTheOccurrence(t *testing.T) {
	api := scan.Finding{
		ID: "KSV-0001", Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", Title: "Can elevate its own privileges",
		Message: "Container 'api' of Deployment 'web' should set 'securityContext.allowPrivilegeEscalation' to false",
	}
	sidecar := api
	sidecar.Message = strings.Replace(api.Message, "api", "sidecar", 1)
	v := misconfigVerify{Rule: "KSV-0001", File: "deploy.yaml", Instance: instanceOf(api)}

	if got := stillReported(&scan.Result{Findings: []scan.Finding{sidecar}}, v); len(got) != 0 {
		t.Errorf("the untouched sidecar answered for the api container: %v", got)
	}
	if got := stillReported(&scan.Result{Findings: []scan.Finding{api}}, v); !slices.Equal(got, []string{"KSV-0001"}) {
		t.Errorf("the occurrence still reported = %v, want [KSV-0001]", got)
	}
	if got := stillReported(nil, v); len(got) != 0 {
		t.Errorf("no result read as still reported: %v", got)
	}
}
