package scan

import (
	"context"
	"testing"
)

// The verdict has three values, and that is the substance of this file:
// "nothing found" and "nobody looked" look a lot alike and are not resolved
// the same way. Caches and the columns that render an icon read
// SecretVerdict, and nothing else computes this verdict anywhere in the
// application.

// verdictOf renders a verdict for a failure message: nil has no readable %v.
func verdictOf(v *bool) string {
	switch {
	case v == nil:
		return "unknown"
	case *v:
		return "found"
	}
	return "clean"
}

// A secret found by Trivy alone is a secret. The two calculations that
// SecretVerdict replaces both looked for "a finding whose Source is
// gitleaks" — but Gitleaks cannot read an image, so any image secret was
// invisible, and a repo whose only secrets Trivy found read as clean.
func TestASecretFoundByTrivyAloneIsAVerdict(t *testing.T) {
	byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"misconfig":    {stdout: `{"Results":[]}`},
		"trivy-secret": {stdout: trivySecretReport(t, "aws-secret-access-key")},
	})

	result, err := newScannerWithDeps(everyStage(), everyTool()).
		Scan(context.Background(), "api:v1", TargetImage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := result.SecretVerdict()
	if got == nil || !*got {
		t.Errorf("SecretVerdict() = %s on an image whose only secret came from Trivy, want found",
			verdictOf(got))
	}
}

// Having looked and found nothing is a verdict, and it is the only case
// where the green icon is earned.
func TestAStageThatLookedAndFoundNothingSaysSo(t *testing.T) {
	byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"license":      {stdout: `{"Results":[]}`},
		"misconfig":    {stdout: `{"Results":[]}`},
		"secret":       {stdout: `[]`},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	result, err := newScannerWithDeps(everyStage(), everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := result.SecretVerdict()
	if got == nil || *got {
		t.Errorf("SecretVerdict() = %s after two secret stages found nothing, want clean", verdictOf(got))
	}
}

// The three ways of not having looked all render the same "I don't know".
// This is the case the boolean could not express: it rendered false, i.e.
// "clean", for a target nobody had looked at.
func TestNothingLookedMeansNoVerdict(t *testing.T) {
	noSecretStage := everyStage()
	noSecretStage.EnableSecret = false

	noSecretTool := everyTool()
	noSecretTool.GitleaksAvailable = false
	noSecretTool.TrivyAvailable = false

	tests := []struct {
		name    string
		opts    ScanOptions
		deps    DependencyStatus
		replies map[string]stageReply
	}{
		{
			name: "the option is off",
			opts: noSecretStage,
			deps: everyTool(),
			replies: map[string]stageReply{
				"vuln":      {stdout: `{"Results":[]}`},
				"license":   {stdout: `{"Results":[]}`},
				"misconfig": {stdout: `{"Results":[]}`},
			},
		},
		{
			// Neither Gitleaks nor Trivy: no secrets stage starts, and the scan
			// reports it elsewhere in Errors (D20).
			name:    "neither tool is installed",
			opts:    everyStage(),
			deps:    noSecretTool,
			replies: map[string]stageReply{},
		},
		{
			// The stage ran and failed. It found nothing, which does not mean
			// there was nothing.
			name: "both secret stages failed",
			opts: everyStage(),
			deps: everyTool(),
			replies: map[string]stageReply{
				"vuln":         {stdout: `{"Results":[]}`},
				"license":      {stdout: `{"Results":[]}`},
				"misconfig":    {stdout: `{"Results":[]}`},
				"secret":       {err: &exitError{Code: 2}},
				"trivy-secret": {err: &exitError{Code: 2}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byStage(t, tt.replies)

			result, err := newScannerWithDeps(tt.opts, tt.deps).
				Scan(context.Background(), "/repos", TargetDirectory)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}

			if got := result.SecretVerdict(); got != nil {
				t.Errorf("SecretVerdict() = %s, want unknown — nothing looked", verdictOf(got))
			}
		})
	}
}

// One of the two secrets stages is enough: Gitleaks reads the git history,
// Trivy the content, and either one counts as a look at the target.
func TestOneSecretStageIsEnoughToDecide(t *testing.T) {
	deps := everyTool()
	deps.GitleaksAvailable = false

	byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"license":      {stdout: `{"Results":[]}`},
		"misconfig":    {stdout: `{"Results":[]}`},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	result, err := newScannerWithDeps(everyStage(), deps).
		Scan(context.Background(), "/repos", TargetDirectory)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := result.SecretVerdict()
	if got == nil || *got {
		t.Errorf("SecretVerdict() = %s with Trivy's stage alone, want clean", verdictOf(got))
	}
}
