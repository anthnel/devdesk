package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Recorded from helm v4.3.0 (alpine/helm) on a chart with an unvendored
// dependency and a name that renders empty. INFO lines are not findings; the
// dependency warning names the chart by its absolute path in the container.
func TestHelmLintBecomesFindings(t *testing.T) {
	out, err := os.ReadFile(filepath.Join("testdata", "helm_lint_broken.txt"))
	if err != nil {
		t.Fatal(err)
	}

	findings, recognised := parseHelmLint(out, "charts/broken", "/repo")

	if !recognised {
		t.Fatal("helm lint's own output was not recognised")
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want the two warnings: %+v", len(findings), findings)
	}
	files := []string{findings[0].File, findings[1].File}
	if want := []string{"charts/broken/templates/cm.yaml", "charts/broken"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %q, want %q", files, want)
	}
	for _, f := range findings {
		if f.Severity != SeverityLow || f.Source != SourceHelm || f.IaCType != "helm" || f.ID != HelmLintID {
			t.Errorf("finding = %+v", f)
		}
	}
}

// A binary helm names the chart by its host path; it comes back relative too.
func TestHelmLintPathsComeBackRelativeToTheRepository(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	for in, want := range map[string]string{
		"templates/":                    "charts/api/templates",
		"Chart.yaml":                    "charts/api/Chart.yaml",
		"/scan/charts/api":              "charts/api",
		filepath.Join(repo, "charts/x"): "charts/x",
	} {
		if got := helmLintPath(in, "charts/api", repo); got != want {
			t.Errorf("helmLintPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// helm names a template under the chart's own name, which need not be its
// directory's: "webapp/templates/deploy.yaml" lives in charts/api.
func TestARenderedResourceIsTracedToItsTemplate(t *testing.T) {
	rendered, err := os.ReadFile(filepath.Join("testdata", "helm_template_api.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	origins := helmOrigins(rendered, "charts/api")

	if got := origins["Deployment/release-name-web"]; got != "charts/api/templates/deploy.yaml" {
		t.Errorf("origin = %q, want the template in the chart's directory", got)
	}
}

// Rendered input reaches a containerised kubeconform on stdin: -i, no mount
// of the repository, "-" as the file.
func TestRenderedYAMLReachesKubeconformOnStdin(t *testing.T) {
	tc := kubeconformStdinArgs([]byte("kind: x"), ToolSpec{Source: ToolSourceContainer},
		KubeconformOptions{KubernetesVersion: "1.36.0", CacheDir: "/home/u/cache"})

	got := strings.Join(tc.Args, " ")
	if !strings.HasPrefix(got, "run --rm -i -v /home/u/cache:/cache ") || !strings.HasSuffix(got, " -") {
		t.Errorf("invocation = %q", got)
	}
	if strings.Contains(got, ":/scan") {
		t.Errorf("the repository was mounted for rendered input: %q", got)
	}
	if string(tc.Stdin) != "kind: x" {
		t.Errorf("stdin = %q", tc.Stdin)
	}
}

// renderRepo is a repository with one chart and one overlay built on a base.
func renderRepo(t *testing.T) string {
	t.Helper()
	return writeRepo(t, map[string]string{
		"charts/api/Chart.yaml":               "apiVersion: v2\nname: webapp\nversion: 0.1.0\n",
		"charts/api/templates/deploy.yaml":    "kind: {{ .Values.kind }}\n",
		"k8s/base/kustomization.yaml":         "resources:\n  - deploy.yaml\n",
		"k8s/base/deploy.yaml":                "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: worker\n",
		"k8s/overlays/prod/kustomization.yml": "resources:\n  - ../../base\n",
	})
}

// renderRunner answers each tool of the schema stage the way it answered for
// real, keyed on what the invocation is rather than on its order.
func renderRunner(t *testing.T, helmTemplate stageReply) *scriptedRunner {
	t.Helper()
	lint, _ := os.ReadFile(filepath.Join("testdata", "helm_lint_broken.txt"))
	rendered, _ := os.ReadFile(filepath.Join("testdata", "helm_template_api.yaml"))
	if helmTemplate.stdout == "" && helmTemplate.err == nil {
		helmTemplate.stdout = string(rendered)
	}
	invalid := `{"resources":[{"filename":"stdin","kind":"Deployment","name":"%s","version":"apps/v1",
	  "status":"statusInvalid","validationErrors":[{"path":"/spec/replicas","msg":"got string, want null or integer"}]}]}`
	r := &scriptedRunner{}
	r.reply = func(tc toolCmd) ([]byte, error) {
		args := strings.Join(tc.Args, " ")
		switch {
		case strings.HasPrefix(args, "lint "):
			return []byte(strings.ReplaceAll(string(lint), "charts/broken", "charts/api")), nil
		case strings.HasPrefix(args, "template "):
			return []byte(helmTemplate.stdout), helmTemplate.err
		case strings.HasPrefix(args, "build "):
			return []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: worker\n"), nil
		case strings.HasSuffix(args, " -") && strings.Contains(string(tc.Stdin), "release-name-web"):
			return []byte(strings.ReplaceAll(invalid, "%s", "release-name-web")), &exitError{Code: 1}
		case strings.HasSuffix(args, " -"):
			return []byte(strings.ReplaceAll(invalid, "%s", "worker")), &exitError{Code: 1}
		}
		t.Errorf("unexpected invocation: %s", tc)
		return nil, nil
	}
	useRunner(t, r)
	return r
}

func renderingDeps() Report {
	return binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolHelm, ToolKustomize)
}

// k8sSchemaFor is k8sSchema validated against one release.
func k8sSchemaFor(version string) ScanOptions {
	opts := k8sSchema()
	opts.Tools.Kubeconform.KubernetesVersion = version
	return opts
}

// With helm and kustomize, the chart and the overlay are validated once
// rendered, and every finding points back at a file of the repository — the
// template, or the overlay's kustomization — with no line, since a rendered
// line is no line of either.
func TestChartsAndOverlaysAreValidatedOnceRendered(t *testing.T) {
	isolateHome(t)
	repo := renderRepo(t)
	renderRunner(t, stageReply{})

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), renderingDeps()).
		Scan(t.Context(), repo, TargetDirectory)

	if len(result.K8sUnrendered) != 0 {
		t.Errorf("K8sUnrendered = %v, want none: both renderers were there", result.K8sUnrendered)
	}
	got := map[string]Finding{}
	for _, f := range result.Findings {
		got[f.Source+" "+f.File] = f
	}
	for key, dialect := range map[string]string{
		"kubeconform charts/api/templates/deploy.yaml":    "helm",
		"kubeconform k8s/overlays/prod/kustomization.yml": "kustomize",
		"helm charts/api/templates/cm.yaml":               "helm",
	} {
		f, ok := got[key]
		if !ok {
			t.Errorf("no finding %q among %v", key, keys(got))
			continue
		}
		if f.IaCType != dialect || f.Line != 0 {
			t.Errorf("%s: type %q line %d, want %q and no line", key, f.IaCType, f.Line, dialect)
		}
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none", result.Errors)
	}
}

// A chart that does not render is a finding, not a failed scan — helm lint
// only warns about a missing dependency, and it is the render that fails.
func TestAChartThatDoesNotRenderIsAFinding(t *testing.T) {
	isolateHome(t)
	repo := renderRepo(t)
	renderRunner(t, stageReply{err: &exitError{Code: 1,
		Stderr: "Error: found in Chart.yaml, but missing in charts/ directory: redis\nmore"}})

	result, _ := newScannerWithDeps(k8sSchema(), renderingDeps()).
		Scan(t.Context(), repo, TargetDirectory)

	var render *Finding
	for i, f := range result.Findings {
		if f.ID == K8sRenderID {
			render = &result.Findings[i]
		}
	}
	if render == nil {
		t.Fatalf("no render finding among %+v", result.Findings)
	}
	if render.File != "charts/api/Chart.yaml" || !strings.HasSuffix(render.Title, "missing in charts/ directory: redis") {
		t.Errorf("render finding = %q in %q", render.Title, render.File)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none: the scan itself did not fail", result.Errors)
	}
}

// Without helm or kustomize, nothing about charts and overlays is claimed:
// they are listed as not rendered, and no tool is reported missing — they are
// optional.
func TestWithoutRenderersChartsAndOverlaysAreNotRendered(t *testing.T) {
	isolateHome(t)
	repo := renderRepo(t)
	r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		t.Errorf("ran %s with no renderer and no plain manifest", tc)
		return nil, nil
	}}
	useRunner(t, r)
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform)

	result, _ := newScannerWithDeps(k8sSchema(), deps).Scan(t.Context(), repo, TargetDirectory)

	if want := []string{"charts/api", "k8s/overlays/prod"}; !reflect.DeepEqual(result.K8sUnrendered, want) {
		t.Errorf("K8sUnrendered = %v, want %v", result.K8sUnrendered, want)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none", result.Errors)
	}
}

func keys(m map[string]Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
