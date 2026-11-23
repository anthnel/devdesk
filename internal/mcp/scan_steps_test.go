package mcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-752 (step 4): the steps DevDesk wrote cross the pipe. Neither the Repro
// nor RanFrom does (decision of the orchestrator: expose steps only, extend later):
// the Repro is a command to paste in a shell, which no caller of this server asked
// for, and RanFrom is how the UI knows why a Repro is missing.

func storeRepoResult(t *testing.T, repo string, findings ...scan.Finding) {
	t.Helper()
	if err := cache.SaveWorkspaceScanResult(repo, &scan.Result{Target: repo, Findings: findings}); err != nil {
		t.Fatalf("store workspace result: %v", err)
	}
}

func TestScanResultCarriesTheStepsOfAFindingDevDeskWroteAndNotItsRepro(t *testing.T) {
	fakeCacheHome(t)
	repo := existingDir(t)
	steps := []string{
		"Reference a directory that has its own kustomization.yaml, or",
		"move the file below kustomizations/dev.",
	}
	storeRepoResult(t, repo, scan.Finding{
		ID: scan.K8sRenderID, Title: "Overlay does not build", Severity: scan.SeverityHigh,
		Source: scan.SourceKustomize, File: "kustomizations/dev/kustomization.yaml", IaCType: "kustomize",
		Resolution: "kustomize refuses by default a file outside the overlay directory.",
		Steps:      steps, Repro: "kustomize build kustomizations/dev", RanFrom: scan.ToolSourceBinary,
	})

	cs := connect(t, testEnv(nil))
	var out scanResultOut
	callTool(t, cs, "scan_result", map[string]any{"target": repo}, &out)

	if len(out.Findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(out.Findings))
	}
	f := out.Findings[0]
	if !reflect.DeepEqual(f.Steps, steps) {
		t.Errorf("steps = %q, want %q", f.Steps, steps)
	}
	if f.Resolution == "" {
		t.Error("the resolution is gone")
	}
	raw := callToolRaw(t, cs, "scan_result", map[string]any{"target": repo})
	for _, key := range []string{"repro", "Repro", "ran_from", "RanFrom", "kustomize build"} {
		if strings.Contains(raw, key) {
			t.Errorf("%q crossed the pipe: %s", key, raw)
		}
	}
}

// A third-party finding has neither key: omitempty, not an empty list or string.
func TestScanResultOmitsStepsAndReproOnAThirdPartyFinding(t *testing.T) {
	fakeCacheHome(t)
	repo := existingDir(t)
	storeRepoResult(t, repo, scan.Finding{
		ID: "KSV-0012", Title: "Runs as root", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", Line: 3,
		Resolution: "Set runAsNonRoot to true.",
	})

	raw := callToolRaw(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": repo})

	for _, key := range []string{"steps", "repro", "ran_from"} {
		if strings.Contains(raw, key) {
			t.Errorf("the answer holds %q for a finding that has none: %s", key, raw)
		}
	}
	if !strings.Contains(raw, "Set runAsNonRoot to true.") {
		t.Errorf("the resolution is gone: %s", raw)
	}
}

// The schema says what the field is, so a caller does not guess, and does not
// declare the Repro or RanFrom.
func TestTheScanResultSchemaExplainsStepsAndNothingMore(t *testing.T) {
	cs := connect(t, testEnv(nil))
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "scan_result" {
			continue
		}
		schema, ok := tool.OutputSchema.(map[string]any)
		if !ok {
			t.Fatalf("output schema is a %T", tool.OutputSchema)
		}
		text := strings.ToLower(schemaText(schema))
		for _, want := range []string{"\"steps\"", "remediation steps"} {
			if !strings.Contains(text, want) {
				t.Errorf("the scan_result schema lacks %s", want)
			}
		}
		for _, banned := range []string{"\"repro\"", "ran_from"} {
			if strings.Contains(text, banned) {
				t.Errorf("the scan_result schema exposes %s", banned)
			}
		}
		return
	}
	t.Fatal("no scan_result tool")
}

func schemaText(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				b.WriteString("\"" + k + "\" ")
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		case string:
			b.WriteString(t + " ")
		}
	}
	walk(v)
	return b.String()
}
