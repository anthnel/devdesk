package security

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-753, step 1: fixtures shared by the change-preview tests.
//
// One fixture per rule of the catalog, keyed by the AVD id remediation.CatalogIDs
// lists: the tests walk the catalog, so a rule added without a fixture fails
// TestEveryCatalogRuleHasAChangeFixture instead of going untested.

// changeFixture is a directory and the one finding the catalog fixes in it.
type changeFixture struct {
	// id is the AVD id as remediation.CatalogIDs spells it.
	id string
	// files is the directory, by slash path relative to it.
	files map[string]string
	// finding is what the scanner reports about it.
	finding scan.Finding
	// edited is the file the fix writes: the finding's own, but for the build
	// context rule, whose fix lands in the .dockerignore beside the Dockerfile.
	edited string
}

const changeWebDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels: {app: web}
    spec:
      containers:
        - name: api
          image: api:1.0
          securityContext:
            privileged: true
        - name: sidecar
          image: side:1.0
`

const changeRemovedAPI = `apiVersion: batch/v1beta1 # nightly job
kind: CronJob
metadata:
  name: nightly
`

func dockerfileFixture(id, src string, line, end int) changeFixture {
	return changeFixture{
		id: id, files: map[string]string{"Dockerfile": src}, edited: "Dockerfile",
		finding: scan.Finding{
			ID: id, Title: "Dockerfile rule " + id, Severity: scan.SeverityHigh,
			Source: scan.SourceTrivyMisconfig, File: "Dockerfile", Line: line, EndLine: end,
		},
	}
}

func manifestFixture(id, file, src string, f scan.Finding) changeFixture {
	f.ID, f.File = id, file
	if f.Title == "" {
		f.Title = "Manifest rule " + id
	}
	if f.Severity == "" {
		f.Severity = scan.SeverityHigh
	}
	if f.Source == "" {
		f.Source = scan.SourceTrivyMisconfig
	}
	f.IaCType = "kubernetes"
	return changeFixture{id: id, files: map[string]string{file: src}, edited: file, finding: f}
}

// changeFixtures is one fixture per rule of the catalog.
func changeFixtures(t *testing.T) []changeFixture {
	t.Helper()
	// KSV-0001 is fixed on the sidecar: the privileged api container is refused
	// by the rule (the API server would reject the manifest).
	web := func(id, field, container string, line, end int) changeFixture {
		return manifestFixture(id, "deploy.yaml", changeWebDeployment, scan.Finding{
			Line: line, EndLine: end,
			Message: "Container '" + container + "' of Deployment 'web' should set 'securityContext." + field + "' to false",
		})
	}
	host := func(hr struct{ id, field string }) changeFixture {
		return manifestFixture(hr.id, "deploy.yaml", hostDaemonSet, hostFinding(hr.id, hr.field))
	}
	seccomp := func(id string) changeFixture {
		return manifestFixture(id, "deploy.yaml", seccompPod, seccompFinding(t, id, seccompPod, "app", "Pod", "demo"))
	}
	fixtures := []changeFixture{
		dockerfileFixture("AVD-DS-0002", "FROM debian:12\nRUN apt-get install -y curl\nENTRYPOINT [\"/app\"]\n", 1, 3),
		dockerfileFixture("AVD-DS-0005", "FROM debian:12\nADD app.jar /app/app.jar\n", 2, 2),
		dockerfileFixture("AVD-DS-0011", "FROM debian:12\nCOPY a b dest\n", 2, 2),
		dockerfileFixture("AVD-DS-0015", "FROM centos:7\nRUN yum install -y curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0019", "FROM fedora:40\nRUN dnf install -y curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0020", "FROM opensuse/leap\nRUN zypper install curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0021", "FROM debian:12\nRUN apt-get install curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0022", "FROM debian:12\nMAINTAINER Jane Doe <jane@example.com>\n", 2, 2),
		dockerfileFixture("AVD-DS-0025", "FROM alpine:3\nRUN apk add curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0027", "FROM registry.access.redhat.com/ubi9-micro\nRUN microdnf install curl\n", 2, 2),
		dockerfileFixture("AVD-DS-0029", "FROM debian:12\nRUN apt-get install -y curl\n", 2, 2),
		web("KSV-0001", "allowPrivilegeEscalation", "sidecar", 17, 18),
		web("KSV-0017", "privileged", "api", 13, 16),
		host(struct{ id, field string }{"KSV-0008", "hostIPC"}),
		host(struct{ id, field string }{"KSV-0009", "hostNetwork"}),
		host(struct{ id, field string }{"KSV-0010", "hostPID"}),
		seccomp("KSV-0030"),
		seccomp("KSV-0104"),
		manifestFixture(scan.K8sAPIRemovedID, "jobs.yaml", changeRemovedAPI, scan.Finding{
			Source: scan.SourceKubeconform, Line: 1, EndLine: 1,
		}),
		{
			id:     scan.BuildContextGitID,
			files:  map[string]string{"Dockerfile": contextDockerfile, ".dockerignore": "node_modules\n"},
			edited: ".dockerignore",
			finding: scan.Finding{
				ID: scan.BuildContextGitID, Title: "The build context copies .git", Severity: scan.SeverityMedium,
				Source: scan.SourceBuildContext, IaCType: "dockerfile", File: "Dockerfile", Line: 2, EndLine: 2,
			},
		},
	}
	return fixtures
}

// writeTree writes files under dir and gives each the modification time at, so a
// test never depends on the clock of the machine running it.
func writeTree(t *testing.T, dir string, files map[string]string, at time.Time) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		touch(t, path, at)
	}
}

// touch sets the modification time of path (following a link).
func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// readTree reads the file name under dir.
func readTree(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// scannedAtFixed is the instant the fixtures' scans ended. Files are given times
// around it with os.Chtimes.
var scannedAtFixed = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// changeModel opens the Misconfigurations tab on dir with the one finding, in a
// result whose scan ended at endedAt (the zero time: unknown).
func changeModel(t *testing.T, dir string, f scan.Finding, endedAt time.Time) Model {
	t.Helper()
	result := &scan.Result{
		Target: dir, TargetType: scan.TargetDirectory, Findings: []scan.Finding{f},
		StartTime: endedAt.Add(-time.Minute), EndTime: endedAt,
	}
	if endedAt.IsZero() {
		result.StartTime = time.Time{}
	}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

// previewed presses the write key and runs the Cmd it returns, which is the
// computation the preview and the confirmation share. It returns the message the
// Cmd answered with and the model that has read it.
func previewed(t *testing.T, m Model) (Model, MisconfigFixPreparedMsg) {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	if cmd == nil {
		t.Fatalf("the write key started nothing; footer: %q", m.footer.Text())
	}
	msg, ok := run(t, cmd).(MisconfigFixPreparedMsg)
	if !ok {
		t.Fatalf("the write key did not answer with a MisconfigFixPreparedMsg")
	}
	return feed(t, m, msg), msg
}

// fixtureOnDisk writes the fixture under a fresh directory, every file stamped
// before the scan, and returns the directory.
func fixtureOnDisk(t *testing.T, fx changeFixture) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, fx.files, scannedAtFixed.Add(-time.Hour))
	return dir
}

// ruleOf is the catalog rule of a fixture.
func ruleOf(t *testing.T, fx changeFixture) remediation.Rule {
	t.Helper()
	rule, ok := remediation.FixFor(fx.finding)
	if !ok {
		t.Fatalf("%s: the catalog has no rule for the fixture's finding", fx.id)
	}
	return rule
}
