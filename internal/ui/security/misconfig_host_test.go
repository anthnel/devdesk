package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The host namespace fixes through the Misconfigurations tab (DEVDESK-687, S-02):
// the same key, the same confirmation that defaults to No, the same write and
// the same re-scan as KSV-0017, with a warning in the confirmation.

const hostDaemonSet = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: agent
spec:
  selector:
    matchLabels: {app: agent}
  template:
    metadata:
      labels: {app: agent}
    spec:
      hostNetwork: true # the agent reads the host's interfaces
      hostPID: true
      hostIPC: true
      containers:
        - name: agent
          image: agent:1.0
`

var hostRules = []struct{ id, field string }{
	{"KSV-0009", "hostNetwork"},
	{"KSV-0010", "hostPID"},
	{"KSV-0008", "hostIPC"},
}

func hostFinding(id, field string) scan.Finding {
	line := 1 + strings.Count(hostDaemonSet[:strings.Index(hostDaemonSet, "    spec:\n")], "\n")
	return scan.Finding{
		ID: id, Title: "Host namespace shared", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: strings.Count(hostDaemonSet, "\n"),
		Message: "DaemonSet 'agent' should not set 'spec.template.spec." + field + "' to true",
	}
}

// hostModel opens the Misconfigurations tab on a directory holding the manifest
// and the one finding about it.
func hostModel(t *testing.T, manifest string, f scan.Finding) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deploy.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	result := &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: []scan.Finding{f}}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m, dir
}

func manifestOnDisk(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "deploy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The write key is lit for the three rules, on a DaemonSet too.
func TestTheWriteKeyIsLitForTheHostNamespaceRules(t *testing.T) {
	for _, hr := range hostRules {
		m, _ := hostModel(t, hostDaemonSet, hostFinding(hr.id, hr.field))
		if got := m.canFixMisconfig(); !got.Enabled() {
			t.Errorf("%s: declined: %q", hr.id, got.Reason)
		}
		if !testutil.ShortcutEnabled(m.GetShortcuts(), writeRemediationKey) {
			t.Errorf("%s: the write key is greyed", hr.id)
		}
	}
}

// Criteria 1, 2 and 3: the confirmation shows the true -> false diff and the
// warning, for the three fields and for a DaemonSet; nothing is written until
// the answer is yes, and No leaves the file identical byte for byte.
func TestTheHostFixConfirmationShowsTheDiffAndTheWarning(t *testing.T) {
	for _, hr := range hostRules {
		t.Run(hr.field, func(t *testing.T) {
			m, dir := hostModel(t, hostDaemonSet, hostFinding(hr.id, hr.field))
			m, cmd := step(t, m, testutil.Key(writeRemediationKey))
			m = feed(t, m, run(t, cmd))

			if m.confirmModal == nil {
				t.Fatalf("no confirmation opened; footer: %s", m.RenderFooter(160))
			}
			body := m.confirmModal.View()
			for _, want := range []string{
				"-      " + hr.field + ": true",
				"+      " + hr.field + ": false",
				"reverses an explicit choice by the author",
				"node agents",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the confirmation lacks %q:\n%s", want, body)
				}
			}
			if got := manifestOnDisk(t, dir); got != hostDaemonSet {
				t.Errorf("the file was written before the confirmation:\n%s", got)
			}

			m = feed(t, m, sharedcomponents.ConfirmModalNoMsg{})
			if m.misconfigPending != nil {
				t.Error("the pending write survived a No")
			}
			if got := manifestOnDisk(t, dir); got != hostDaemonSet {
				t.Errorf("No changed the file:\n%s", got)
			}
		})
	}
}

// A confirmed fix writes the one value and leaves a verification pending for
// the rule that was fixed.
func TestAConfirmedHostFixWritesOnlyThatField(t *testing.T) {
	for _, hr := range hostRules {
		t.Run(hr.field, func(t *testing.T) {
			m, dir := hostModel(t, hostDaemonSet, hostFinding(hr.id, hr.field))
			m = writeFix(t, m, dir)

			want := strings.Replace(hostDaemonSet, hr.field+": true", hr.field+": false", 1)
			if got := manifestOnDisk(t, dir); got != want {
				t.Errorf("the write changed more than the value:\n%s", got)
			}
			if m.misconfigVerifying == nil || m.misconfigVerifying.Rule != hr.id || m.misconfigVerifying.File != "deploy.yaml" {
				t.Errorf("verification = %+v, want %s in deploy.yaml", m.misconfigVerifying, hr.id)
			}
		})
	}
}

// Criterion 5: the re-scan judges the occurrence that was fixed. The same pod
// still carrying hostPID: true keeps its own finding open: one fix per finding.
func TestFixingHostNetworkLeavesHostPIDOpen(t *testing.T) {
	net, pid := hostFinding("KSV-0009", "hostNetwork"), hostFinding("KSV-0010", "hostPID")
	m, dir := hostModel(t, hostDaemonSet, net)
	m = writeFix(t, m, dir)

	if got := manifestOnDisk(t, dir); !strings.Contains(got, "hostPID: true") || !strings.Contains(got, "hostNetwork: false") {
		t.Fatalf("the fix did not leave exactly hostNetwork changed:\n%s", got)
	}
	if m.misconfigVerifying == nil {
		t.Fatal("no verification pending after the write")
	}
	v := m.misconfigVerifying

	after := &scan.Result{Findings: []scan.Finding{pid}}
	if holdsRule(after, v.Rule, v.File, v.Instance) {
		t.Error("the fixed occurrence (hostNetwork) still reads as open")
	}
	if !holdsRule(after, "KSV-0010", v.File, instanceOf(pid)) {
		t.Error("the hostPID finding of the same pod reads as cleared")
	}
	still := &scan.Result{Findings: []scan.Finding{net, pid}}
	if !holdsRule(still, v.Rule, v.File, v.Instance) {
		t.Error("a re-scan that still reports hostNetwork reads as cleared")
	}
}

// Criterion 6: an instance the fix cannot place exactly is declined with its
// reason, as a warning in the footer, and nothing is written. As for KSV-0017,
// the declines that need the file's content are answered when the fix is
// computed, so the key stays lit and the footer carries the reason.
func TestAHostFixTheCatalogDeclinesIsAWarningAndWritesNothing(t *testing.T) {
	const aliased = `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: agent
x-podspec: &podspec
  hostNetwork: true
  containers:
    - name: agent
      image: agent:1.0
spec:
  template:
    spec: *podspec
`
	const duplicated = `apiVersion: v1
kind: Pod
metadata:
  name: agent
spec:
  hostNetwork: true
  hostNetwork: true
  containers: [{name: agent, image: agent:1.0}]
`
	tests := []struct {
		name, src, want string
		f               scan.Finding
	}{
		{"a pod spec shared through an alias", aliased, remediation.ReasonSharedByAlias, scan.Finding{Line: 5, EndLine: 12}},
		{"a duplicated key", duplicated, remediation.ReasonAmbiguousField, scan.Finding{Line: 5, EndLine: 8, Message: "Pod 'agent' should not set 'spec.hostNetwork' to true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hostFinding("KSV-0009", "hostNetwork")
			f.Line, f.EndLine = tt.f.Line, tt.f.EndLine
			if tt.f.Message != "" {
				f.Message = tt.f.Message
			}
			m, dir := hostModel(t, tt.src, f)
			m, cmd := step(t, m, testutil.Key(writeRemediationKey))
			m = feed(t, m, run(t, cmd))

			if m.confirmModal != nil {
				t.Error("a declined fix opened a confirmation")
			}
			if got := m.RenderFooter(200); !strings.Contains(got, tt.want) {
				t.Errorf("the footer does not carry the reason %q:\n%s", tt.want, got)
			}
			if got := manifestOnDisk(t, dir); got != tt.src {
				t.Errorf("a declined fix wrote the file:\n%s", got)
			}
		})
	}
}
