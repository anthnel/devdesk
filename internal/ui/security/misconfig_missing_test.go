package security

import (
	"os"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-689 (S-04): the greyed refusal of a Pod Security finding that has no
// safe fix says what is missing. The text itself is tested in
// internal/remediation (TestMissingForEveryRuleWithoutAFix); here the view
// shows it, as the reason of the greyed key and in the footer when the key is
// pressed, and writes nothing. Footer timers are never run: the footer is read
// through Text.

const missingManifest = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  containers:
    - name: app
      image: nginx:1.27
`

// missingFinding is a Pod Security finding on deploy.yaml.
func missingFinding(id string, refs ...string) scan.Finding {
	return scan.Finding{
		ID: id, Title: "Pod Security finding", Severity: scan.SeverityMedium,
		Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: 4, EndLine: 8, References: refs,
	}
}

// The 14 findings the text covers: the 13 rules without any fix, and KSV-0104 on
// an explicit Unconfined, the one seccomp case the fix of DEVDESK-688 (S-03)
// refuses from the finding alone. KSV-0030 and KSV-0104 absent have a fix now:
// they open its diff (misconfig_seccomp_test.go), and this text only comes back
// when that fix declines.
func missingFindings() []scan.Finding {
	unconfined := missingFinding("KSV-0104", pssBaselineLn)
	unconfined.SeccompOrigin = "unconfined"
	list := []scan.Finding{unconfined}
	for _, id := range []string{
		"KSV-0103", "KSV-0022", "KSV-0023", "KSV-0024", "KSV-0002", "KSV-0025", "KSV-0026",
		"KSV-0027", "KSV-0028", "KSV-0012", "KSV-0105", "KSV-0106", "KSV-0121",
	} {
		list = append(list, missingFinding(id))
	}
	return list
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Criteria 1 and 2: the refusal is the rule's own text, as the reason of the
// greyed key and in the footer; no confirmation, no file touched, and the key
// set is the one of a fixable finding (Rule 130).
func TestTheRefusalOfAPodSecurityFindingSaysWhatIsMissing(t *testing.T) {
	fixable, _ := hostModel(t, hostDaemonSet, hostFinding("KSV-0009", "hostNetwork"))
	wantKeys := testutil.ShortcutKeys(fixable.GetShortcuts())

	for _, f := range missingFindings() {
		name := f.ID + "/" + f.SeccompOrigin
		t.Run(name, func(t *testing.T) {
			want, ok := remediation.MissingFor(f)
			if !ok || want == "" {
				t.Fatalf("MissingFor(%s) = %q, %v; no text to show", f.ID, want, ok)
			}
			m, dir := hostModel(t, missingManifest, f)

			got := m.canFixMisconfig()
			if got.Enabled() || got.Reason != want {
				t.Errorf("reason = %q (enabled %v), want %q", got.Reason, got.Enabled(), want)
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), writeRemediationKey) {
				t.Error("the write key is not greyed")
			}
			if !testutil.HasShortcut(m.GetShortcuts(), writeRemediationKey) {
				t.Error("the write key left the column")
			}
			if keys := testutil.ShortcutKeys(m.GetShortcuts()); strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
				t.Errorf("keys = %v, want the same set as on a fixable finding: %v", keys, wantKeys)
			}

			m, _ = step(t, m, testutil.Key(writeRemediationKey))
			if got := m.footer.Text(); got != want {
				t.Errorf("footer = %q, want %q", got, want)
			}
			if m.confirmModal != nil {
				t.Error("a confirmation opened for a finding with no fix")
			}
			if b := manifestOnDisk(t, dir); b != missingManifest {
				t.Errorf("the file was modified:\n%s", b)
			}
			if names := dirEntries(t, dir); len(names) != 1 || names[0] != "deploy.yaml" {
				t.Errorf("the directory holds %v, want deploy.yaml alone", names)
			}
		})
	}
}

// Criterion 3: restricted, and a finding no story will ever fix. The text says
// what is missing and offers no correction.
func TestRunAsNonRootRefusalSaysWhatIsMissingWithoutOfferingAFix(t *testing.T) {
	m, _ := hostModel(t, missingManifest, missingFinding("KSV-0012"))
	got := m.canFixMisconfig()
	if got.Enabled() {
		t.Fatal("the write key is lit for KSV-0012")
	}
	lower := strings.ToLower(got.Reason)
	for _, want := range []string{"runasnonroot", "true", "restricted"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the reason lacks %q: %s", want, got.Reason)
		}
	}
	if got.Reason == remediation.ReasonNoFixForRule {
		t.Error("the reason is still the generic one")
	}
	if strings.Contains(lower, "ctrl+o") {
		t.Errorf("the reason proposes a key: %s", got.Reason)
	}
}

// Criterion 4: the anchor is not in the table. The refusal says the profile
// comes from Trivy and points to the check, without a field or a value.
func TestTheRefusalOfARuleOutsideTheTableSendsToTrivy(t *testing.T) {
	f := missingFinding("KSV-9999", "https://avd.example/KSV-9999", pssRestrictLn)
	m, dir := hostModel(t, missingManifest, f)

	got := m.canFixMisconfig()
	want, ok := remediation.MissingFor(f)
	if !ok || want == "" {
		t.Fatalf("MissingFor = %q, %v; no text to show", want, ok)
	}
	if got.Enabled() || got.Reason != want {
		t.Errorf("reason = %q (enabled %v), want %q", got.Reason, got.Enabled(), want)
	}
	lower := strings.ToLower(got.Reason)
	for _, w := range []string{"trivy", "restricted", "ksv-9999"} {
		if !strings.Contains(lower, w) {
			t.Errorf("the reason lacks %q: %s", w, got.Reason)
		}
	}
	for _, invented := range []string{"securitycontext", "runas", "seccomp", "capabilities", "hostpath"} {
		if strings.Contains(lower, invented) {
			t.Errorf("the reason names %q: %s", invented, got.Reason)
		}
	}
	m, _ = step(t, m, testutil.Key(writeRemediationKey))
	if m.footer.Text() != want || m.confirmModal != nil {
		t.Errorf("footer = %q, modal open = %v", m.footer.Text(), m.confirmModal != nil)
	}
	if b := manifestOnDisk(t, dir); b != missingManifest {
		t.Errorf("the file was modified:\n%s", b)
	}
}

// A Kubernetes finding that is not Pod Security keeps the generic refusal.
func TestARuleOutsidePodSecurityKeepsTheGenericRefusal(t *testing.T) {
	m, _ := hostModel(t, missingManifest, missingFinding("KSV-0003"))
	if got := m.canFixMisconfig(); got.Enabled() || got.Reason != remediation.ReasonNoFixForRule {
		t.Errorf("reason = %q, want %q", got.Reason, remediation.ReasonNoFixForRule)
	}
}

// Criterion 6: a rule that has a fix opens the confirmation diff, whatever the
// profile; the "what is missing" text is not what the user gets.
func TestARuleWithAFixOpensTheDiffNotTheMissingText(t *testing.T) {
	for _, hr := range hostRules {
		t.Run(hr.id, func(t *testing.T) {
			f := hostFinding(hr.id, hr.field)
			f.References = []string{pssBaselineLn}
			if text, ok := remediation.MissingFor(f); ok {
				t.Fatalf("MissingFor = %q for a rule that has a fix", text)
			}
			m, _ := hostModel(t, hostDaemonSet, f)
			m, cmd := step(t, m, testutil.Key(writeRemediationKey))
			m = feed(t, m, run(t, cmd))
			if m.confirmModal == nil {
				t.Fatalf("no confirmation opened; footer: %q", m.footer.Text())
			}
		})
	}
}
