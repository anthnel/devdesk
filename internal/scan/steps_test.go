package scan

import (
	"reflect"
	"strings"
	"testing"
)

// DEVDESK-752 (step 2): the steps DevDesk writes for the findings it builds, as
// pure functions, and the findings the parsers build from third-party output,
// which get none.

// One action per step, in imperative English, alternatives as separate steps
// (the convention proposed in the plan, Q-a).

func TestRenderStepsForTheLoadRestrictor(t *testing.T) {
	got := renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceBinary, loadRestrictorStderr, false)

	want := []string{
		"Reference a directory that has its own kustomization.yaml, or",
		"move the file below kustomizations/dev.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

func TestRenderStepsForAnyOtherKustomizeFailure(t *testing.T) {
	const stderr = "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch"
	want := []string{"Run kustomize build kustomizations/dev and fix the first error it reports."}

	if got := renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceBinary, stderr, false); !reflect.DeepEqual(got, want) {
		t.Errorf("binary: steps = %q, want %q", got, want)
	}
	if got := renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceContainer, stderr, false); !reflect.DeepEqual(got, want) {
		t.Errorf("container, nothing about a missing path: steps = %q, want %q", got, want)
	}
}

// A kustomize image sees only what DevDesk mounts: a path it cannot find may be
// one the mount hides, and a binary sees the whole file system (renderResolution
// says the same in its own words).
func TestRenderStepsAddTheMountStepOnlyInAContainer(t *testing.T) {
	const stderr = "Error: open /scan/x/patch.yaml: no such file or directory"

	inContainer := renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceContainer, stderr, false)
	if len(inContainer) != 2 || !strings.HasPrefix(inContainer[0], "Run kustomize build kustomizations/dev") ||
		!strings.Contains(inContainer[1], "scan.tools.kustomize.source: binary") {
		t.Errorf("container: steps = %q, want the build step then the mount step", inContainer)
	}
	if onHost := renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceBinary, stderr, false); len(onHost) != 1 {
		t.Errorf("binary: steps = %q, want the build step alone", onHost)
	}
}

func TestRenderStepsForAChart(t *testing.T) {
	got := renderSteps(SourceHelm, "charts/api", ToolSourceBinary, "Error: found in Chart.yaml, but missing in charts/ directory: redis", false)

	want := []string{
		"Run helm template charts/api and fix the first error it reports.",
		"If a dependency is missing, run helm dependency build charts/api.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
	for _, s := range got {
		if strings.Contains(s, "kustomize") {
			t.Errorf("a chart step names kustomize: %q", s)
		}
	}
}

func TestHelmLintSteps(t *testing.T) {
	got := helmLintSteps("charts/api/templates/cm.yaml", "charts/api")

	want := []string{
		"Fix what helm lint reports in charts/api/templates/cm.yaml.",
		"Run helm lint charts/api again to confirm.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

func TestSchemaStepsNameTheKubernetesVersionTheValidationUsed(t *testing.T) {
	r := kubeconformResource{Kind: "Deployment", Name: "api", Version: "apps/v1"}
	p := kubeconformFieldProblem{Path: "/spec/replicas", Msg: "got string, want integer"}

	plain := schemaSteps(r, p, "1.36.0", "")
	if want := []string{"Correct /spec/replicas so it matches the apps/v1 Deployment schema of Kubernetes 1.36.0."}; !reflect.DeepEqual(plain, want) {
		t.Errorf("plain manifest: steps = %q, want %q", plain, want)
	}

	// The finding is in what a renderer produced: the fix is in the template or
	// the overlay, and the check is the render, which is named.
	for _, dialect := range []string{"helm", "kustomize"} {
		rendered := schemaSteps(r, p, "1.36.0", dialect)
		if len(rendered) != 2 || rendered[0] != plain[0] {
			t.Fatalf("%s: steps = %q, want the plain step then one more", dialect, rendered)
		}
		if want := "Render it with " + dialect + " and validate the output against Kubernetes 1.36.0."; rendered[1] != want {
			t.Errorf("%s: second step = %q, want %q", dialect, rendered[1], want)
		}
	}
}

// wantSecretSteps are the two steps of a secret, written out: a test that
// compared a finding with secretSteps() would pass on two nils.
func wantSecretSteps() []string {
	return []string{
		"Revoke or rotate this secret where it was issued: treat it as exposed.",
		"Remove it from the file and read it from a secret store or an environment variable.",
	}
}

func TestSecretStepsAreNewOnEveryCall(t *testing.T) {
	first := secretSteps()

	want := wantSecretSteps()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("steps = %q, want %q", first, want)
	}
	first[0] = "changed by a caller"
	if again := secretSteps(); !reflect.DeepEqual(again, want) {
		t.Errorf("a second call returned %q: the slice is shared", again)
	}
}

// ── What the parsers write ───────────────────────────────────────────────────

func TestAGitleaksFindingGetsTheSecretStepsAndNoRepro(t *testing.T) {
	findings, err := parseGitleaksOutput([]byte(`[
	  {"RuleID":"aws-access-token","Description":"AWS key","StartLine":3,"File":"config/prod.env","Secret":"AKIAIOSFODNN7EXAMPLE","Fingerprint":"config/prod.env:aws-access-token:3"},
	  {"RuleID":"generic-api-key","Description":"A key","StartLine":9,"File":"config/dev.env","Secret":"dummy-value-not-a-secret","Fingerprint":"config/dev.env:generic-api-key:9"}
	]`))
	if err != nil || len(findings) != 2 {
		t.Fatalf("parse: %d findings, err %v", len(findings), err)
	}

	for _, f := range findings {
		if !reflect.DeepEqual(f.Steps, wantSecretSteps()) {
			t.Errorf("%s: steps = %q, want the secret steps", f.ID, f.Steps)
		}
		// Q-c: a re-scan of the whole repository would print the secret.
		if f.Repro != "" || f.RanFrom != "" {
			t.Errorf("%s: repro %q ran from %q, want none", f.ID, f.Repro, f.RanFrom)
		}
		if f.Resolution != "" {
			t.Errorf("%s: the Resolution became %q, want it left empty", f.ID, f.Resolution)
		}
	}
	// Two findings must not share a backing array.
	if len(findings[0].Steps) == 0 || len(findings[1].Steps) == 0 {
		return
	}
	findings[0].Steps[0] = "changed by a caller"
	if findings[1].Steps[0] == "changed by a caller" {
		t.Error("two findings share their steps")
	}
}

func TestATrivySecretGetsTheSecretSteps(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{"Results":[{"Target":"app/.env","Secrets":[
	  {"RuleID":"aws-access-key-id","Category":"AWS","Severity":"CRITICAL","Title":"AWS key","StartLine":3,"Match":"AKIAIOSFODNN7EXAMPLE"}]}]}`))
	if err != nil || len(findings) != 1 {
		t.Fatalf("parse: %d findings, err %v", len(findings), err)
	}
	f := findings[0]
	if f.Source != SourceTrivySecret || !reflect.DeepEqual(f.Steps, wantSecretSteps()) || f.Repro != "" {
		t.Errorf("finding = source %q steps %q repro %q", f.Source, f.Steps, f.Repro)
	}
}

// AC5 at the source: what Trivy says is shown as Trivy said it. A vulnerability,
// a misconfiguration and a license carry no Steps, no Repro and no RanFrom, and
// the Resolution is the scanner's, untouched.
func TestThirdPartyFindingsGetNoStepsAndNoRepro(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{"Results":[{
	  "Target":"deploy/app.yaml","Class":"config","Type":"kubernetes",
	  "Vulnerabilities":[{"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"libfoo","InstalledVersion":"1.0","FixedVersion":"1.1"}],
	  "Licenses":[{"Name":"GPL-3.0","PkgName":"libbar","Category":"restricted","Severity":"HIGH","FilePath":"vendor/libbar"}],
	  "Misconfigurations":[{"AVDID":"AVD-KSV-0001","Title":"t","Severity":"HIGH","Resolution":"Set allowPrivilegeEscalation to false.","CauseMetadata":{"StartLine":7}}]
	}]}`))
	if err != nil || len(findings) != 3 {
		t.Fatalf("parse: %d findings, err %v", len(findings), err)
	}
	for _, f := range findings {
		if f.Steps != nil || f.Repro != "" || f.RanFrom != "" {
			t.Errorf("%s (%s): steps %q repro %q ran from %q, want none", f.ID, f.Source, f.Steps, f.Repro, f.RanFrom)
		}
	}
	for _, f := range findings {
		if f.Source == SourceTrivyMisconfig && f.Resolution != "Set allowPrivilegeEscalation to false." {
			t.Errorf("the Resolution became %q, want Trivy's sentence untouched", f.Resolution)
		}
	}
}

func TestKubeconformFindingsThatAreNotAboutAFieldGetNoSteps(t *testing.T) {
	findings, _, err := parseKubeconformOutput(kubeconformFixture(t), "1.36.0",
		func(string) ([]byte, error) { return []byte(appManifest), nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema int
	for _, f := range findings {
		switch f.ID {
		case K8sSchemaID:
			schema++
			if len(f.Steps) == 0 || !strings.Contains(strings.Join(f.Steps, " "), "Kubernetes 1.36.0") {
				t.Errorf("%s: steps = %q, want at least one naming Kubernetes 1.36.0", f.ID, f.Steps)
			}
		default:
			// K8S-API-REMOVED and K8S-PARSE keep their Resolution as it is.
			if f.Steps != nil {
				t.Errorf("%s: steps = %q, want none", f.ID, f.Steps)
			}
		}
	}
	if schema != 2 {
		t.Errorf("%d K8S-SCHEMA findings, want the 2 of the fixture", schema)
	}
}

// The Resolution of a schema finding stays as it was (the plan: Resolution texts
// unchanged); the steps are added before it.
func TestASchemaFindingKeepsItsResolution(t *testing.T) {
	findings, _, err := parseKubeconformOutput(kubeconformFixture(t), "1.36.0",
		func(string) ([]byte, error) { return []byte(appManifest), nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.ID == K8sSchemaID && f.Resolution != "Correct the field so it matches the API's schema for this kind and apiVersion." {
			t.Errorf("Resolution = %q", f.Resolution)
		}
	}
}
