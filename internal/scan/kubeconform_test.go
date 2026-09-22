package scan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// appManifest is the file the fixture report is about; its line numbers are
// what the findings are checked against.
const appManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  replicas: "3"
  template:
    spec:
      containers:
        - name: api
          image: api:1
          imagePullPolicyy: Always
---
apiVersion: batch/v1beta1
kind: CronJob
metadata:
  name: nightly
`

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func kubeconformFixture(t *testing.T) []byte {
	t.Helper()
	out, err := os.ReadFile(filepath.Join("testdata", "kubeconform_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The files are named one by one, relative to the repository the tool runs in,
// so the names it reports back are the ones a finding carries.
func TestKubeconformRunsInTheRepositoryOnTheFilesItWasGiven(t *testing.T) {
	tc := kubeconformArgs("/repo", []string{"deploy/app.yaml"},
		ToolSpec{Source: ToolSourceBinary, Binary: "/opt/kubeconform"},
		KubeconformOptions{KubernetesVersion: "1.36.0", CacheDir: "/home/u/.devdesk/cache/kubeconform"})

	if tc.Name != "/opt/kubeconform" || tc.Dir != "/repo" {
		t.Errorf("ran %q in %q, want the configured binary in the repository", tc.Name, tc.Dir)
	}
	want := []string{"-output", "json", "-strict", "-kubernetes-version", "1.36.0",
		"-cache", "/home/u/.devdesk/cache/kubeconform", filepath.FromSlash("deploy/app.yaml")}
	if !reflect.DeepEqual(tc.Args, want) {
		t.Errorf("args = %q, want %q", tc.Args, want)
	}
}

// In a container the repository is mounted read-only, the schema cache is
// mounted writable, and every path the tool sees is the container's.
func TestKubeconformInAContainerSeesOnlyItsOwnPaths(t *testing.T) {
	tc := kubeconformArgs("/repo", []string{"deploy/app.yaml"},
		ToolSpec{Source: ToolSourceContainer},
		KubeconformOptions{KubernetesVersion: "1.36.0", CacheDir: "/home/u/cache"})

	got := strings.Join(tc.Args, " ")
	for _, want := range []string{
		"-v /repo:/scan:ro",
		"-v /home/u/cache:/cache",
		"-w /scan " + DefaultKubeconformImage,
		"-cache /cache deploy/app.yaml",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("container invocation %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "-cache /home/u/cache") {
		t.Errorf("the host cache path reached the container's argv: %q", got)
	}
}

func TestTheKubeconformReportBecomesFindings(t *testing.T) {
	read := func(name string) ([]byte, error) {
		if name == "deploy/app.yaml" {
			return []byte(appManifest), nil
		}
		return nil, os.ErrNotExist
	}

	findings, skipped, err := parseKubeconformOutput(kubeconformFixture(t), "1.36.0", read, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The two custom resources are skipped — the Gateway API's group ends in
	// .k8s.io, which is exactly why the built-in groups are a closed list.
	if skipped != 2 {
		t.Errorf("skipped = %d, want the two custom resources", skipped)
	}
	if len(findings) != 4 {
		t.Fatalf("got %d findings, want 4: %+v", len(findings), findings)
	}

	unknown, badType, removed, parse := findings[0], findings[1], findings[2], findings[3]

	// An unknown field is reported at its parent; the key's own line is the
	// one worth showing.
	if unknown.ID != K8sSchemaID || unknown.Line != 12 {
		t.Errorf("unknown field: id %q line %d, want %s on line 12", unknown.ID, unknown.Line, K8sSchemaID)
	}
	if badType.Line != 6 || !strings.Contains(badType.Message, "/spec/replicas") {
		t.Errorf("wrong type: line %d message %q, want line 6 and the pointer", badType.Line, badType.Message)
	}
	if removed.ID != K8sAPIRemovedID || removed.Line != 14 {
		t.Errorf("removed API: id %q line %d, want %s on the apiVersion line, 14", removed.ID, removed.Line, K8sAPIRemovedID)
	}
	if !strings.Contains(removed.Title, "batch/v1beta1 CronJob is not served by Kubernetes 1.36.0") {
		t.Errorf("removed API title = %q", removed.Title)
	}
	// A file that could not be read keeps its finding, with no line rather
	// than an invented one.
	if parse.ID != K8sParseID || parse.Line != 0 || parse.File != "deploy/broken.yaml" {
		t.Errorf("parse error = %+v", parse)
	}

	for _, f := range findings {
		if f.Source != SourceKubeconform || f.IaCType != "kubernetes" || f.Severity != SeverityHigh {
			t.Errorf("%s: source %q type %q severity %q", f.ID, f.Source, f.IaCType, f.Severity)
		}
		if Categorize(f) != CategoryMisconfiguration {
			t.Errorf("%s is not on the Misconfigurations tab", f.ID)
		}
	}
}

// kubeconform exits 1 both when it found something and when it could not run.
// The report on stdout is what tells them apart.
func TestKubeconformExitOneIsAFindingOnlyWithAReport(t *testing.T) {
	found := &exitError{Code: 1}
	if err := kubeconformFailure([]byte(`{"resources":[]}`), found); err != nil {
		t.Errorf("exit 1 with a report was treated as a failure: %v", err)
	}
	if err := kubeconformFailure(nil, &exitError{Code: 1, Stderr: "bad flag"}); err == nil {
		t.Error("exit 1 with no report was treated as a result")
	}
	if err := kubeconformFailure([]byte(`{}`), errors.New("killed")); err == nil {
		t.Error("a killed process was treated as a result")
	}
}

// The stage runs on a directory, validates its manifests only, and says which
// charts and overlays it could not look at rather than reading as clean there.
func TestTheSchemaStageValidatesTheRepositorysManifests(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{
		"deploy/app.yaml":               appManifest,
		".gitlab-ci.yml":                "stages: [build]\n",
		"charts/api/Chart.yaml":         "apiVersion: v2\nname: api\n",
		"charts/api/templates/dep.yaml": "{{ .Values.x }}\n",
	})
	r := byStage(t, map[string]stageReply{
		"k8s-schema": {stdout: string(kubeconformFixture(t)), err: &exitError{Code: 1}},
	})
	deps := everyTool()
	deps.KubeconformAvailable, deps.KubeconformSource = true, ToolSourceBinary

	result, _ := newScannerWithDeps(ScanOptions{EnableK8sSchema: true, KubernetesVersion: "1.36.0"}, deps).
		Scan(t.Context(), repo, TargetDirectory)

	cmd := r.commandForStage("k8s-schema")
	if !strings.HasSuffix(cmd, filepath.FromSlash("deploy/app.yaml")) || strings.Contains(cmd, "gitlab-ci") {
		t.Errorf("validated %q, want the manifest and only it", cmd)
	}
	if result.MisconfigCount != 4 {
		t.Errorf("MisconfigCount = %d, want the 4 schema findings", result.MisconfigCount)
	}
	if want := []string{"charts/api"}; !reflect.DeepEqual(result.K8sUnrendered, want) {
		t.Errorf("K8sUnrendered = %v, want %v", result.K8sUnrendered, want)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none", result.Errors)
	}
}

// A repository with no manifest runs nothing: kubeconform with no file reads
// stdin, and would wait there.
func TestTheSchemaStageRunsNothingWithoutManifests(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{"README.md": "# hi\n"})
	r := byStage(t, map[string]stageReply{})
	deps := everyTool()
	deps.KubeconformAvailable, deps.KubeconformSource = true, ToolSourceBinary

	result, _ := newScannerWithDeps(ScanOptions{EnableK8sSchema: true}, deps).Scan(t.Context(), repo, TargetDirectory)

	if r.count() != 0 {
		t.Errorf("ran %v on a repository with no manifest", r.commands())
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v, want none", result.Errors)
	}
}

// Asked for and not installed is said, like the other tools — and only for a
// directory, since an image has no manifests to validate.
func TestAMissingKubeconformIsReported(t *testing.T) {
	s := newScannerWithDeps(ScanOptions{EnableK8sSchema: true}, everyTool())

	if errs := s.missingToolErrors(TargetDirectory); len(errs) != 1 || !strings.Contains(errs[0], "kubeconform") {
		t.Errorf("directory: errors = %v, want kubeconform reported missing", errs)
	}
	if errs := s.missingToolErrors(TargetImage); len(errs) != 0 {
		t.Errorf("image: errors = %v, want none", errs)
	}
}

// kubeconform is resolved like every other tool, and its settings reach
// detection through NewScanner (the gap plumber had, §3.80 phase 0).
func TestKubeconformIsResolvedLikeTheOthers(t *testing.T) {
	installTools(t, "present", "kubeconform", "docker")

	s := NewScanner(OptionsFromConfig(&config.Config{Scan: config.ScanConfig{
		KubeconformSource: config.ToolSourceImage,
		KubeconformImage:  "mirror.example/kubeconform:0.8.0",
	}}))

	if s.deps.KubeconformSource != ToolSourceContainer || s.deps.KubeconformImage != "mirror.example/kubeconform:0.8.0" {
		t.Errorf("kubeconform resolved as %q with image %q, want the configured image",
			s.deps.KubeconformSource, s.deps.KubeconformImage)
	}
}

// isolateHome keeps the schema cache the stage creates out of the real home.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestTheKubernetesVersionIsCheckedAsKubeconformWould(t *testing.T) {
	for v, ok := range map[string]bool{"": true, "master": true, "1.36.0": true, "1.36": false, "v1.36.0": false} {
		if err := ValidateKubernetesVersion(v); (err == nil) != ok {
			t.Errorf("ValidateKubernetesVersion(%q) = %v, want ok=%v", v, err, ok)
		}
	}
}
