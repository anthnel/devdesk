package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// DEVDESK-752 (step 2): what a scan writes on the findings the schema stage and
// its renderers build — Steps always, Repro and RanFrom only when the tool ran on
// this machine and one plain command reproduces the finding.

// asContainers is the report with these tools run from an image.
func asContainers(r Report, ids ...ToolID) Report {
	tools := make(map[ToolID]ToolStatus, len(r.Tools))
	for k, v := range r.Tools {
		tools[k] = v
	}
	for _, id := range ids {
		tool, _ := ToolByID(id)
		tools[id] = ToolStatus{Available: true, Source: ToolSourceContainer, Image: tool.DefaultImage}
	}
	r.Tools = tools
	return r
}

// subcommandOf is the helm or kustomize subcommand an invocation carries, from a
// binary or from `run --rm … <image> <subcommand> …` alike.
func subcommandOf(tc toolCmd) string {
	for _, a := range tc.Args {
		switch a {
		case "lint", "template", "build":
			return a
		}
	}
	return ""
}

// failingRenderRunner answers kustomize build with an error and helm with its
// recorded lint output and a failing template. git (asked for the mount of a
// container renderer) has no repository to name.
func failingRenderRunner(t *testing.T, kustomizeStderr, helmStderr string) {
	t.Helper()
	lint, err := os.ReadFile(filepath.Join("testdata", "helm_lint_broken.txt"))
	if err != nil {
		t.Fatal(err)
	}
	useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		if tc.Name == "git" {
			return nil, &exitError{Code: 128, Stderr: "not a git repository"}
		}
		switch subcommandOf(tc) {
		case "lint":
			return []byte(strings.ReplaceAll(string(lint), "charts/broken", "charts/api")), nil
		case "template":
			return nil, &exitError{Code: 1, Stderr: helmStderr}
		case "build":
			return nil, &exitError{Code: 1, Stderr: kustomizeStderr}
		}
		t.Errorf("unexpected invocation: %s", tc)
		return nil, nil
	}})
}

// findingWhere is the one finding of a result matching pred.
func findingWhere(t *testing.T, result *Result, what string, pred func(Finding) bool) Finding {
	t.Helper()
	var found []Finding
	for _, f := range result.Findings {
		if pred(f) {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d findings for %s, want 1, among %+v", len(found), what, result.Findings)
	}
	return found[0]
}

func renderFindingOf(t *testing.T, result *Result) Finding {
	return findingWhere(t, result, "the render failure", func(f Finding) bool { return f.ID == K8sRenderID })
}

const kustomizationDev = "kustomizations/dev"

func kustomizeRepo(t *testing.T) string {
	t.Helper()
	return writeRepo(t, map[string]string{kustomizationDev + "/kustomization.yaml": "resources:\n  - ../../notes/infra.yaml\n"})
}

// AC1 and AC3: the load restrictor, from a kustomize on this machine. The steps
// are the two the plan writes, the Resolution is DEVDESK-742's, byte for byte,
// and the Repro is the command a person would type in the scanned directory.
func TestALoadRestrictorFailureHasItsStepsItsResolutionAndItsRepro(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, loadRestrictorStderr, "")
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolKustomize)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)

	f := renderFindingOf(t, result)
	wantSteps := []string{
		"Reference a directory that has its own kustomization.yaml, or",
		"move the file below kustomizations/dev.",
	}
	if !reflect.DeepEqual(f.Steps, wantSteps) {
		t.Errorf("steps = %q, want %q", f.Steps, wantSteps)
	}
	if f.Resolution != loadRestrictorResolution {
		t.Errorf("the Resolution changed:\n got %q\nwant %q", f.Resolution, loadRestrictorResolution)
	}
	if f.Repro != "kustomize build kustomizations/dev" || f.RanFrom != ToolSourceBinary {
		t.Errorf("repro %q ran from %q, want %q from a binary", f.Repro, f.RanFrom, "kustomize build kustomizations/dev")
	}
	if f.Severity != SeverityHigh || f.Message != loadRestrictorStderr {
		t.Errorf("severity %s, message %q: the finding itself must not change", f.Severity, f.Message)
	}
}

// AC4 at the source: from an image there is no Repro (what ran is not what is on
// this machine), and RanFrom says why. The steps are still there.
func TestAContainerRenderFailureHasStepsAndNoRepro(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, loadRestrictorStderr, "")
	deps := asContainers(binaries(ToolTrivy, ToolGitleaks, ToolKubeconform), ToolKustomize)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)

	f := renderFindingOf(t, result)
	if f.Repro != "" || f.RanFrom != ToolSourceContainer {
		t.Errorf("repro %q ran from %q, want no Repro and %q", f.Repro, f.RanFrom, ToolSourceContainer)
	}
	if len(f.Steps) != 2 || !strings.HasPrefix(f.Steps[0], "Reference a directory") {
		t.Errorf("steps = %q, want the two load restrictor steps", f.Steps)
	}
	if f.Resolution != loadRestrictorResolution {
		t.Errorf("the Resolution changed: %q", f.Resolution)
	}
}

func TestAnyOtherKustomizeFailureAsksToBuildAndFixTheFirstError(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch", "")
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolKustomize)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)

	f := renderFindingOf(t, result)
	want := []string{"Run kustomize build kustomizations/dev and fix the first error it reports."}
	if !reflect.DeepEqual(f.Steps, want) {
		t.Errorf("steps = %q, want %q", f.Steps, want)
	}
	if f.Repro != "kustomize build kustomizations/dev" {
		t.Errorf("repro = %q", f.Repro)
	}
}

// The user's own arguments (scan.tools.*.args) may carry a secret (--set
// password=..., a URL with a token): the Repro is the line DevDesk built, and
// leaves them out (security F3).
func TestTheReproLeavesOutTheUsersArguments(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, loadRestrictorStderr, "")
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolKustomize)
	opts := k8sSchemaFor("1.36.0")
	opts.Tools.Kustomize.Args = []string{"--load-restrictor=LoadRestrictionsNone", "--token=hunter2"}

	result, _ := newScannerWithDeps(opts, deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)

	if got, want := renderFindingOf(t, result).Repro, "kustomize build kustomizations/dev"; got != want {
		t.Errorf("repro = %q, want %q", got, want)
	}
}

// A scan that ran a binary DevDesk was told to use by path shows that path: it is
// what ran, and the plain name may mean another.
func TestTheReproNamesTheConfiguredBinaryWhenPathLeadsElsewhere(t *testing.T) {
	isolateHome(t)
	useLookPath(t, map[string]string{"kustomize": "/usr/bin/kustomize"})
	failingRenderRunner(t, loadRestrictorStderr, "")
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolKustomize)
	deps.Tools[ToolKustomize] = ToolStatus{Available: true, Source: ToolSourceBinary, Binary: "/opt/k/kustomize"}

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)

	if got, want := renderFindingOf(t, result).Repro, "/opt/k/kustomize build kustomizations/dev"; got != want {
		t.Errorf("repro = %q, want %q", got, want)
	}
}

// AC2 and AC3 for a chart: the render failure and what helm lint says about the
// same chart, each with its steps and the command that reproduces it.
func TestAChartFailureAndItsLintMessagesCarryStepsAndRepro(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, "", "Error: found in Chart.yaml, but missing in charts/ directory: redis")
	repo := writeRepo(t, map[string]string{
		"charts/api/Chart.yaml":            "apiVersion: v2\nname: webapp\nversion: 0.1.0\n",
		"charts/api/templates/deploy.yaml": "kind: {{ .Values.kind }}\n",
	})
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolHelm)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	render := renderFindingOf(t, result)
	if render.Repro != "helm template charts/api" || render.RanFrom != ToolSourceBinary {
		t.Errorf("render: repro %q ran from %q", render.Repro, render.RanFrom)
	}
	wantSteps := []string{
		"Run helm template charts/api and fix the first error it reports.",
		"If a dependency is missing, run helm dependency build charts/api.",
	}
	if !reflect.DeepEqual(render.Steps, wantSteps) {
		t.Errorf("render steps = %q, want %q", render.Steps, wantSteps)
	}

	lints := 0
	for _, f := range result.Findings {
		if f.ID != HelmLintID {
			continue
		}
		lints++
		if f.Repro != "helm lint charts/api" || f.RanFrom != ToolSourceBinary {
			t.Errorf("lint %s: repro %q ran from %q, want %q from a binary", f.File, f.Repro, f.RanFrom, "helm lint charts/api")
		}
		// AC2: a lint message had no Resolution; its steps are what the detail shows.
		if len(f.Steps) < 1 || !strings.Contains(f.Steps[0], f.File) || !strings.Contains(strings.Join(f.Steps, " "), "helm lint charts/api") {
			t.Errorf("lint %s: steps = %q, want the file and the command to run again", f.File, f.Steps)
		}
		if f.Resolution != "" {
			t.Errorf("lint %s: Resolution = %q, want it left empty", f.File, f.Resolution)
		}
	}
	if lints == 0 {
		t.Error("no HELM-LINT finding: the fixture lost its warnings")
	}
}

func TestAHelmLintFromAContainerHasStepsAndNoRepro(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, "", "Error: found in Chart.yaml, but missing in charts/ directory: redis")
	repo := writeRepo(t, map[string]string{
		"charts/api/Chart.yaml":            "apiVersion: v2\nname: webapp\nversion: 0.1.0\n",
		"charts/api/templates/deploy.yaml": "kind: {{ .Values.kind }}\n",
	})
	deps := asContainers(binaries(ToolTrivy, ToolGitleaks, ToolKubeconform), ToolHelm)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	for _, f := range result.Findings {
		if f.ID != HelmLintID && f.ID != K8sRenderID {
			continue
		}
		if f.Repro != "" || f.RanFrom != ToolSourceContainer || len(f.Steps) == 0 {
			t.Errorf("%s %s: repro %q ran from %q steps %q, want steps, no Repro, %q",
				f.ID, f.File, f.Repro, f.RanFrom, f.Steps, ToolSourceContainer)
		}
	}
}

// ── The schema findings ──────────────────────────────────────────────────────

// A plain manifest: the command is kubeconform on that file, with the options the
// user would give it. DevDesk's own `-output json` and `-cache <dir>` are left
// out, and so are the files that are not the finding's.
func TestAPlainSchemaFindingHasStepsAndTheKubeconformCommand(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"deploy/app.yaml": appManifest})
	byStage(t, map[string]stageReply{"k8s-schema": {stdout: string(kubeconformFixture(t)), err: &exitError{Code: 1}}})
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	schema := 0
	for _, f := range result.Findings {
		if f.ID != K8sSchemaID {
			continue
		}
		schema++
		if want := "kubeconform -strict -kubernetes-version 1.36.0 deploy/app.yaml"; f.Repro != want || f.RanFrom != ToolSourceBinary {
			t.Errorf("repro %q ran from %q, want %q from a binary", f.Repro, f.RanFrom, want)
		}
		if strings.Contains(f.Repro, "-output") || strings.Contains(f.Repro, "-cache") {
			t.Errorf("DevDesk's own flags are on the line: %q", f.Repro)
		}
		if !strings.Contains(strings.Join(f.Steps, " "), "Kubernetes 1.36.0") {
			t.Errorf("steps = %q, want them to name Kubernetes 1.36.0", f.Steps)
		}
	}
	if schema != 2 {
		t.Errorf("%d K8S-SCHEMA findings, want the 2 of the fixture", schema)
	}
}

func TestTheSchemaReproLeavesOutTheUsersArguments(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"deploy/app.yaml": appManifest})
	byStage(t, map[string]stageReply{"k8s-schema": {stdout: string(kubeconformFixture(t)), err: &exitError{Code: 1}}})
	opts := k8sSchemaFor("1.36.0")
	opts.Tools.Kubeconform.Args = []string{"-ignore-missing-schemas", "-schema-location", "https://user:hunter2@example.com/{{.Group}}"}
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform)

	result, _ := newScannerWithDeps(opts, deps).Scan(t.Context(), repo, TargetDirectory)

	f := findingWhere(t, result, "the unknown field", func(f Finding) bool {
		return f.ID == K8sSchemaID && strings.Contains(f.Message, "imagePullPolicyy")
	})
	if want := "kubeconform -strict -kubernetes-version 1.36.0 deploy/app.yaml"; f.Repro != want {
		t.Errorf("repro = %q, want %q", f.Repro, want)
	}
}

func TestASchemaFindingFromAContainerHasNoRepro(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"deploy/app.yaml": appManifest})
	byStage(t, map[string]stageReply{"k8s-schema": {stdout: string(kubeconformFixture(t)), err: &exitError{Code: 1}}})
	deps := asContainers(binaries(ToolTrivy, ToolGitleaks), ToolKubeconform)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	for _, f := range result.Findings {
		if f.ID == K8sSchemaID && (f.Repro != "" || f.RanFrom != ToolSourceContainer || len(f.Steps) == 0) {
			t.Errorf("%s: repro %q ran from %q steps %q, want steps, no Repro, %q", f.ID, f.Repro, f.RanFrom, f.Steps, ToolSourceContainer)
		}
	}
}

// A path a shell would split gives no Repro rather than a command that splits it:
// the finding still says the tool ran here.
func TestAFileWithASpaceInItsNameGetsNoRepro(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"my k8s/app.yaml": appManifest})
	byStage(t, map[string]stageReply{"k8s-schema": {err: &exitError{Code: 1}, stdout: `{"resources":[{"filename":"my k8s/app.yaml",
	  "kind":"Deployment","name":"api","version":"apps/v1","status":"statusInvalid",
	  "validationErrors":[{"path":"/spec/replicas","msg":"got string, want integer"}]}]}`}})
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	f := findingWhere(t, result, "the schema finding", func(f Finding) bool { return f.ID == K8sSchemaID })
	if f.Repro != "" || f.RanFrom != ToolSourceBinary {
		t.Errorf("repro %q ran from %q, want no Repro and %q", f.Repro, f.RanFrom, ToolSourceBinary)
	}
	if len(f.Steps) == 0 {
		t.Error("no steps: the steps do not depend on the command")
	}
}

// A finding found in what helm or kustomize rendered is checked by a pipe, which
// no single plain command says: no Repro, no RanFrom, and the step names the
// render command instead.
func TestARenderedSchemaFindingHasStepsAndNoRepro(t *testing.T) {
	isolateHome(t)
	renderRunner(t, stageReply{})
	deps := renderingDeps()

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), renderRepo(t), TargetDirectory)

	for _, tc := range []struct{ file, tool string }{
		{"charts/api/templates/deploy.yaml", "helm"},
		{"k8s/overlays/prod/kustomization.yml", "kustomize"},
	} {
		f := findingWhere(t, result, tc.file, func(f Finding) bool { return f.Source == SourceKubeconform && f.File == tc.file })
		if f.Repro != "" || f.RanFrom != "" {
			t.Errorf("%s: repro %q ran from %q, want neither", tc.file, f.Repro, f.RanFrom)
		}
		joined := strings.Join(f.Steps, " ")
		if len(f.Steps) < 2 || !strings.Contains(joined, "Kubernetes 1.36.0") || !strings.Contains(joined, "Render it with "+tc.tool) {
			t.Errorf("%s: steps = %q, want the field step and the render step naming Kubernetes 1.36.0", tc.file, f.Steps)
		}
	}
}

// A manifest whose name starts with "-" would be read as an option once pasted:
// no Repro, and the finding still says the tool ran here (security F1).
func TestAFileThatStartsWithADashGetsNoRepro(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"-schema-location=foo.yaml": appManifest})
	byStage(t, map[string]stageReply{"k8s-schema": {err: &exitError{Code: 1}, stdout: `{"resources":[{"filename":"-schema-location=foo.yaml",
	  "kind":"Deployment","name":"api","version":"apps/v1","status":"statusInvalid",
	  "validationErrors":[{"path":"/spec/replicas","msg":"got string, want integer"}]}]}`}})
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	f := findingWhere(t, result, "the schema finding", func(f Finding) bool { return f.ID == K8sSchemaID })
	if f.Repro != "" || f.RanFrom != ToolSourceBinary {
		t.Errorf("repro %q ran from %q, want no Repro and %q", f.Repro, f.RanFrom, ToolSourceBinary)
	}
}
