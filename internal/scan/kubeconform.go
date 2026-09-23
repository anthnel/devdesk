package scan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/k8s"
)

// kubeconform validates Kubernetes manifests against the API's JSON schemas
// (§3.80): a wrong type, a missing required field, an unknown field under
// -strict, an apiVersion the target release no longer serves. Trivy's
// misconfiguration scan already lints the same files for security; this is
// the other question — whether the API server would accept them at all.
//
// Measured against kubeconform v0.8.0's sources, not its README: the JSON
// output, the status strings and the "could not find schema" wording below are
// what pkg/output/json.go and pkg/validator/validator.go write.

// DefaultKubeconformImage is the image kubeconform runs from in a container.
// Its entrypoint is the binary itself, so the arguments follow the image name.
const DefaultKubeconformImage = "ghcr.io/yannh/kubeconform"

// kubeconformCacheMount is where the schema cache is mounted in a container.
const kubeconformCacheMount = "/cache"

// KubeconformOptions is what one validation run needs beyond the tool itself.
type KubeconformOptions struct {
	// KubernetesVersion is the release to validate against, x.y.z or master.
	KubernetesVersion string
	// CacheDir is where downloaded schemas are kept between runs. Empty means
	// every run downloads them again — slower, not wrong.
	CacheDir string
	// Helm and Kustomize render charts and overlays before validation. Either
	// may be unresolved (Source none), which leaves what it would have
	// rendered unvalidated and says so in the report.
	Helm      ToolSpec
	Kustomize ToolSpec
}

// KubeconformReport is what one run found, beyond its findings.
type KubeconformReport struct {
	Findings []Finding
	// Validated is how many files were handed to kubeconform.
	Validated int
	// Unrendered are the charts and Kustomize overlays nobody validated,
	// because the tool that renders them was not available. They are not
	// errors — the scan did everything it could — but they are not "clean"
	// either, and saying nothing would read as though they were.
	Unrendered []string
	// SkippedCustom counts resources whose kind no schema describes because it
	// is a custom resource. Their schema lives in the CRD, which kubeconform
	// does not read.
	SkippedCustom int
}

// kubeconformArgs builds the invocation over files relative to target.
//
// The files are passed explicitly rather than the directory: kubeconform
// reports "missing 'kind' key" on every YAML file that is not a manifest —
// a CI configuration, a Helm values file — and k8s.Discover has already told
// the two apart. The tool runs in target so the file names it reports are the
// relative ones the findings carry.
func kubeconformArgs(target string, files []string, tool ToolSpec, opts KubeconformOptions) toolCmd {
	docker := tool.Source == ToolSourceContainer
	flags := kubeconformFlags(tool, opts)

	if !docker {
		args := append(flags, nativePaths(files)...)
		return toolCmd{Name: kubeconformBinary(tool), Args: args, Dir: target}
	}

	args := []string{"run", "--rm", "-v", target + ":" + containerScanPath + ":ro"}
	if opts.CacheDir != "" {
		args = append(args, "-v", opts.CacheDir+":"+kubeconformCacheMount)
	}
	args = append(args, "-w", containerScanPath, kubeconformImage(tool.Image))
	args = append(args, flags...)
	// Inside the container the paths are Linux ones, whatever the host is.
	return toolCmd{Name: engine.Current().Binary, Args: append(args, files...)}
}

// kubeconformFlags are the options every validation run shares, raw files or
// rendered input.
func kubeconformFlags(tool ToolSpec, opts KubeconformOptions) []string {
	flags := []string{"-output", "json", "-strict"}
	if opts.KubernetesVersion != "" {
		flags = append(flags, "-kubernetes-version", opts.KubernetesVersion)
	}
	if opts.CacheDir != "" {
		cache := opts.CacheDir
		if tool.Source == ToolSourceContainer {
			cache = kubeconformCacheMount
		}
		flags = append(flags, "-cache", cache)
	}
	return flags
}

// RunKubeconform finds the repository's manifests and validates them: the
// plain ones as files, the charts and overlays once rendered.
//
// The plain manifests are skipped when there are none rather than handed over
// as an empty list: kubeconform with no file argument reads stdin, and would
// wait there.
func RunKubeconform(ctx context.Context, target string, tool ToolSpec, opts KubeconformOptions, progressFn func(string)) (*KubeconformReport, error) {
	layout, err := k8s.Discover(target)
	if err != nil {
		return nil, fmt.Errorf("listing manifests: %w", err)
	}
	if opts.CacheDir != "" {
		if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
			// Validation still works without a cache; it downloads each time.
			opts.CacheDir = ""
		}
	}
	v := &k8sValidation{target: target, kubeconform: tool, opts: opts,
		report: &KubeconformReport{Validated: len(layout.Manifests)}}

	if len(layout.Manifests) > 0 {
		if err := v.validateFiles(ctx, layout.Manifests, progressFn); err != nil {
			return nil, err
		}
	}
	if err := v.renderCharts(ctx, layout.Charts); err != nil {
		return nil, err
	}
	if err := v.renderKustomizations(ctx, layout.Kustomizations); err != nil {
		return nil, err
	}
	return v.report, nil
}

// validateFiles validates plain manifests, where each finding can be given the
// line its pointer designates.
func (v *k8sValidation) validateFiles(ctx context.Context, files []string, progressFn func(string)) error {
	cmd := kubeconformArgs(v.target, files, v.kubeconform, v.opts)
	log.Printf("Running: %s", cmd)
	out, err := runner.Run(ctx, cmd, progressFn)
	if err := kubeconformFailure(out, err); err != nil {
		return err
	}
	findings, skipped, err := parseKubeconformOutput(out, v.opts.KubernetesVersion, fileReader(v.target), nil)
	if err != nil {
		return err
	}
	v.report.Findings = append(v.report.Findings, findings...)
	v.report.SkippedCustom += skipped
	return nil
}

// kubeconformFailure tells a run that found problems from one that failed.
// kubeconform exits 1 for both; what separates them is whether it wrote its
// JSON report.
func kubeconformFailure(out []byte, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exitError
	if errors.As(err, &exitErr) && exitErr.Code == 1 && looksLikeJSON(out) {
		return nil
	}
	return err
}

func looksLikeJSON(out []byte) bool {
	for _, b := range out {
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// fileReader reads a file relative to root, for locating a finding's line.
func fileReader(root string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) //nolint:gosec // a manifest of the repository being scanned
	}
}

// nativePaths turns the discovered, slash-separated names into the host's.
func nativePaths(files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = filepath.FromSlash(f)
	}
	return out
}

func kubeconformImage(image string) string {
	if image == "" {
		return DefaultKubeconformImage
	}
	return image
}

func kubeconformBinary(tool ToolSpec) string {
	if tool.Binary != "" {
		return tool.Binary
	}
	return "kubeconform"
}

// KubeconformCacheDir is where schemas are cached: beside the scan caches,
// under ~/.devdesk/cache. Empty when there is no home directory, which only
// costs the cache.
func KubeconformCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".devdesk", "cache", "kubeconform")
}

// kubernetesVersionPattern is what kubeconform accepts for -kubernetes-version:
// its own regexp, from pkg/config/config.go.
var kubernetesVersionPattern = regexp.MustCompile(`^(master|\d+\.\d+\.\d+)$`)

// ValidateKubernetesVersion refuses what kubeconform would refuse, so the
// configuration view says it at the field rather than every scan failing on
// it. Empty is accepted: it means the default release.
func ValidateKubernetesVersion(v string) error {
	if v == "" || kubernetesVersionPattern.MatchString(v) {
		return nil
	}
	return fmt.Errorf("%q is not a version: use a full x.y.z such as %s, or master", v, config.DefaultKubernetesVersion)
}

// kubeconformOptions is what this scanner validates against, and the
// renderers it has — an unavailable one is handed over unresolved.
func (s *Scanner) kubeconformOptions() KubeconformOptions {
	opts := KubeconformOptions{KubernetesVersion: s.options.Tools.Kubeconform.KubernetesVersion, CacheDir: KubeconformCacheDir()}
	// A renderer serves only when it is ticked: installed and unticked, it is
	// not used (§3.86), and its charts or overlays are reported as not rendered.
	if s.runs(CategoryIDMisconfig, ToolHelm, TargetDirectory) {
		opts.Helm = s.deps.Spec(ToolHelm)
	}
	if s.runs(CategoryIDMisconfig, ToolKustomize, TargetDirectory) {
		opts.Kustomize = s.deps.Spec(ToolKustomize)
	}
	return opts
}

// runKubeconformStage is the schema stage of Scan, shaped like the others:
// progress, then findings or a stage error under the result's lock.
func (s *Scanner) runKubeconformStage(ctx context.Context, target string, result *Result, mu *sync.Mutex, notify func(ProgressUpdate)) {
	const stage, label = "k8s-schema", "Kubernetes schema"
	notify(ProgressUpdate{Stage: stage, Label: label, Status: StageRunning})
	progressFn := func(detail string) {
		notify(ProgressUpdate{Stage: stage, Label: label, Status: StageRunning, Detail: detail})
	}
	report, err := RunKubeconform(ctx, target, s.deps.Spec(ToolKubeconform), s.kubeconformOptions(), progressFn)

	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		recordStageError(result, "kubeconform", err)
		notify(ProgressUpdate{Stage: stage, Label: label, Status: StageError, Detail: err.Error()})
		return
	}
	if report.SkippedCustom > 0 {
		log.Printf("[scan/kubeconform] %s: %d custom resources not validated (their schema is in a CRD)", target, report.SkippedCustom)
	}
	if len(report.Unrendered) > 0 {
		log.Printf("[scan/kubeconform] %s: not rendered, so not validated: %s", target, strings.Join(report.Unrendered, ", "))
	}
	result.K8sUnrendered = report.Unrendered
	result.Findings = append(result.Findings, report.Findings...)
	notify(ProgressUpdate{Stage: stage, Label: label, Status: StageDone})
}
