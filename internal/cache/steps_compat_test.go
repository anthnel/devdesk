package cache

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-752 (AC6): Steps, Repro and RanFrom are three optional fields of a
// finding, with no cache version. A result cached before them reads back whole,
// and one written by a later build reads back too. Same convention as Class,
// EndLine and IaCType.

const (
	repoBefore752 = "/home/user/repo-before-752"
	oldK8sRender  = `{
  "Target": "/home/user/repo-before-752",
  "TargetType": "directory",
  "Findings": [
    {
      "id": "K8S-RENDER",
      "title": "Overlay does not build: Error: accumulating resources",
      "description": "The directory could not be rendered, so none of its resources were validated.",
      "severity": "HIGH",
      "source": "kustomize",
      "file": "kustomizations/dev/kustomization.yaml",
      "line": 0,
      "message": "Error: accumulating resources",
      "resolution": "Build it locally with kustomize build (or kubectl kustomize) and fix what it reports.",
      "iac_type": "kustomize"
    }
  ]
}`
)

// writeRawWorkspaceResult puts a literal blob where the cache keeps a
// repository's result, the way an older build wrote it.
func writeRawWorkspaceResult(t *testing.T, repo, blob string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	path, err := resultFilePath(repo)
	if err != nil {
		t.Fatalf("resultFilePath: %v", err)
	}
	if err := os.WriteFile(path, []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAResultCachedBefore752ReadsBackWithItsResolutionAlone(t *testing.T) {
	writeRawWorkspaceResult(t, repoBefore752, oldK8sRender)

	result, err := LoadWorkspaceScanResult(repoBefore752)
	if err != nil {
		t.Fatalf("LoadWorkspaceScanResult: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("%d findings, want 1", len(result.Findings))
	}
	f := result.Findings[0]
	if f.ID != scan.K8sRenderID || f.Resolution == "" {
		t.Errorf("finding = %s with resolution %q, want the K8S-RENDER finding and its Resolution", f.ID, f.Resolution)
	}
	if f.Steps != nil || f.Repro != "" || f.RanFrom != "" {
		t.Errorf("steps %q repro %q ran from %q, want none: the blob has no such field", f.Steps, f.Repro, f.RanFrom)
	}
}

// A result written by a later build, with a field this one does not know, still
// reads: encoding/json ignores it, which is what the missing version relies on.
func TestAResultWithFieldsThisBuildDoesNotKnowStillReads(t *testing.T) {
	const blob = `{
  "Target": "/home/user/repo-before-752",
  "Findings": [
    {
      "id": "K8S-RENDER",
      "severity": "HIGH",
      "source": "kustomize",
      "steps": ["Run kustomize build kustomizations/dev and fix the first error it reports."],
      "repro": "kustomize build kustomizations/dev",
      "ran_from": "binary",
      "a_field_from_a_later_build": {"nested": [1, 2, 3]}
    }
  ],
  "another_unknown_field": true
}`
	writeRawWorkspaceResult(t, repoBefore752, blob)

	result, err := LoadWorkspaceScanResult(repoBefore752)
	if err != nil {
		t.Fatalf("LoadWorkspaceScanResult: %v", err)
	}
	f := result.Findings[0]
	if f.Repro != "kustomize build kustomizations/dev" || f.RanFrom != scan.ToolSourceBinary || len(f.Steps) != 1 {
		t.Errorf("steps %q repro %q ran from %q, want the three fields read", f.Steps, f.Repro, f.RanFrom)
	}
}

func findingsOf752() []scan.Finding {
	return []scan.Finding{
		{ID: scan.K8sRenderID, Severity: scan.SeverityHigh, Source: scan.SourceKustomize, File: "kustomizations/dev/kustomization.yaml",
			Resolution: "kustomize refuses by default a file outside the overlay directory.",
			Steps:      []string{"Reference a directory that has its own kustomization.yaml, or", "move the file below kustomizations/dev."},
			Repro:      "kustomize build kustomizations/dev", RanFrom: scan.ToolSourceBinary},
		{ID: scan.K8sRenderID, Severity: scan.SeverityHigh, Source: scan.SourceHelm, File: "charts/api/Chart.yaml",
			Steps: []string{"Run helm template charts/api and fix the first error it reports."}, RanFrom: scan.ToolSourceContainer},
		{ID: "CVE-2026-0001", Severity: scan.SeverityHigh, Source: scan.SourceTrivy, PkgName: "libfoo"},
	}
}

func TestA752ResultRoundTripsEqual(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	want := &scan.Result{Target: "/home/user/repo", Findings: findingsOf752()}

	if err := SaveWorkspaceScanResult("/home/user/repo", want); err != nil {
		t.Fatalf("SaveWorkspaceScanResult: %v", err)
	}
	got, err := LoadWorkspaceScanResult("/home/user/repo")
	if err != nil {
		t.Fatalf("LoadWorkspaceScanResult: %v", err)
	}
	if !reflect.DeepEqual(got.Findings, want.Findings) {
		t.Errorf("findings changed on the way:\n got %+v\nwant %+v", got.Findings, want.Findings)
	}
}

func TestA752ImageResultRoundTripsEqual(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	want := &scan.Result{Target: "app:1.0", Findings: findingsOf752()}

	if err := SaveImageScanResult("app:1.0", want); err != nil {
		t.Fatalf("SaveImageScanResult: %v", err)
	}
	got, err := LoadImageScanResult("app:1.0")
	if err != nil {
		t.Fatalf("LoadImageScanResult: %v", err)
	}
	if !reflect.DeepEqual(got.Findings, want.Findings) {
		t.Errorf("findings changed on the way:\n got %+v\nwant %+v", got.Findings, want.Findings)
	}
}

// A finding with none of the three fields writes none of them: a result of a
// third-party finding is byte for byte what it was before.
func TestAFindingWithoutStepsWritesNoneOfTheNewKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	result := &scan.Result{Target: "/home/user/repo", Findings: []scan.Finding{findingsOf752()[2]}}
	if err := SaveWorkspaceScanResult("/home/user/repo", result); err != nil {
		t.Fatal(err)
	}
	path, err := resultFilePath("/home/user/repo")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"steps"`, `"repro"`, `"ran_from"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("the file holds %s for a finding without it", key)
		}
	}
}
