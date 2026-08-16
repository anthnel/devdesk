package scan

import (
	"context"
	"testing"
)

// Le verdict a trois valeurs, et c'est le fond de ce fichier : « rien trouvé »
// et « personne n'a cherché » se ressemblent beaucoup et ne se règlent pas de la
// même façon. Les caches et les colonnes qui affichent une icône lisent
// SecretVerdict, et rien d'autre ne calcule ce verdict dans l'application.

// verdictOf renders a verdict for a failure message: nil n'a pas de %v lisible.
func verdictOf(v *bool) string {
	switch {
	case v == nil:
		return "unknown"
	case *v:
		return "found"
	}
	return "clean"
}

// Un secret trouvé par Trivy seul est un secret. Les deux calculs que
// SecretVerdict remplace cherchaient « une finding dont Source vaut gitleaks » —
// or Gitleaks ne sait pas lire une image, donc tout secret d'image était invisible,
// et un dépôt dont Trivy trouvait les seuls secrets se lisait propre.
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

// Avoir cherché et n'avoir rien trouvé est un verdict, et c'est le seul cas où
// l'icône verte est méritée.
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

// Les trois façons de ne pas avoir cherché rendent le même « je ne sais pas ».
// C'est le cas que le booléen ne savait pas dire : il rendait false, c'est-à-dire
// « propre », d'une cible que personne n'a regardée.
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
			// Ni Gitleaks ni Trivy : aucune étape secrets ne démarre, et le scan
			// le signale par ailleurs dans Errors (D20).
			name:    "neither tool is installed",
			opts:    everyStage(),
			deps:    noSecretTool,
			replies: map[string]stageReply{},
		},
		{
			// L'étape a tourné et a échoué. Elle n'a rien trouvé, ce qui ne veut
			// pas dire qu'il n'y avait rien.
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

// Une étape secrets sur deux suffit : Gitleaks lit l'historique git, Trivy le
// contenu, et l'une comme l'autre est un regard porté sur la cible.
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
