package scan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/k8s"
)

// A Helm chart or a Kustomize overlay is validated after it is rendered
// (§3.80). Both renderers are optional: without them the directory is
// reported as not rendered, never as clean, and never as a missing tool —
// the schema stage did all it could.
//
// Measured on helm v4.3.0's sources (pkg/cmd/lint.go and
// pkg/chart/v2/lint/support/message.go) and kustomize v5.8.1's Dockerfile:
// alpine/helm's entrypoint is `helm`, the kustomize image's is
// /app/kustomize, so in both the arguments follow the image name.

// Default images for the two renderers.
const (
	DefaultHelmImage      = "alpine/helm"
	DefaultKustomizeImage = "registry.k8s.io/kustomize/kustomize:v5.8.1"
)

// IDs of the findings the renderers themselves produce.
const (
	// HelmLintID is a message helm lint reported, at WARNING or ERROR.
	HelmLintID = "HELM-LINT"
	// K8sRenderID is a chart or an overlay that did not render at all.
	K8sRenderID = "K8S-RENDER"
)

// helmLintLine is one message of helm lint: `[SEVERITY] path: message`, as
// support.Message.Error formats it.
var helmLintLine = regexp.MustCompile(`^\[(ERROR|WARNING|INFO|UNKNOWN)\] ([^:]*): (.*)$`)

// helmSourceLine is the comment helm template writes above each document,
// naming the template it came from: `# Source: <chart name>/templates/x.yaml`.
var helmSourceLine = regexp.MustCompile(`^# Source: (\S+)`)

// renderer runs one tool in the repository, the way every other tool runs:
// in the repository for a binary, with the repository mounted read-only at
// /scan and as the working directory for an image.
func renderer(target string, tool ToolSpec, defaultImage, binary string, args ...string) toolCmd {
	if tool.Source != ToolSourceContainer {
		name := tool.Binary
		if name == "" {
			name = binary
		}
		return toolCmd{Name: name, Args: args, Dir: target}
	}
	run := []string{"run", "--rm", "-v", target + ":" + containerScanPath + ":ro", "-w", containerScanPath, orDefault(tool.Image, defaultImage)}
	return toolCmd{Name: engine.Current().Binary, Args: append(run, args...)}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func helmArgs(target, chart string, tool ToolSpec, sub string) toolCmd {
	return renderer(target, tool, DefaultHelmImage, "helm", sub, chart)
}

func kustomizeArgs(target, dir string, tool ToolSpec) toolCmd {
	return renderer(target, tool, DefaultKustomizeImage, "kustomize", "build", dir)
}

// kubeconformStdinArgs validates what arrives on stdin. A container gets -i
// and no repository mount: it reads nothing but its input and its cache.
func kubeconformStdinArgs(rendered []byte, tool ToolSpec, opts KubeconformOptions) toolCmd {
	flags := kubeconformFlags(tool, opts)
	if tool.Source != ToolSourceContainer {
		return toolCmd{Name: kubeconformBinary(tool), Args: append(flags, "-"), Stdin: rendered}
	}
	args := []string{"run", "--rm", "-i"}
	if opts.CacheDir != "" {
		args = append(args, "-v", opts.CacheDir+":"+kubeconformCacheMount)
	}
	args = append(args, kubeconformImage(tool.Image))
	args = append(args, flags...)
	return toolCmd{Name: engine.Current().Binary, Args: append(args, "-"), Stdin: rendered}
}

// k8sValidation is one schema stage over one repository: the tools it has,
// what it validates against, and the report it fills in.
type k8sValidation struct {
	target      string
	kubeconform ToolSpec
	opts        KubeconformOptions
	report      *KubeconformReport
}

// available reports whether an optional renderer was resolved at all.
func available(tool ToolSpec) bool {
	return tool.Source != ToolSourceNone && tool.Source != ""
}

// renderCharts lints, renders and validates each chart. Without helm, every
// chart is reported as not rendered.
func (v *k8sValidation) renderCharts(ctx context.Context, charts []string) error {
	tool := v.opts.Helm
	for _, chart := range charts {
		if !available(tool) {
			v.report.Unrendered = append(v.report.Unrendered, chart)
			continue
		}
		lint := helmLint(ctx, v.target, chart, tool)
		v.report.Findings = append(v.report.Findings, lint...)

		cmd := helmArgs(v.target, chart, tool, "template")
		log.Printf("Running: %s", cmd)
		rendered, err := runner.Run(ctx, cmd, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// helm lint renders the chart too, so a chart that does not
			// render has usually been reported by it already, with more to
			// say. The render failure is the fallback, not a second copy.
			if !hasError(lint) {
				v.report.Findings = append(v.report.Findings,
					renderFailure(SourceHelm, "helm", path.Join(chart, "Chart.yaml"), "Chart does not render", err))
			}
			continue
		}
		origins := helmOrigins(rendered, chart)
		err = v.validateRendered(ctx, rendered, "helm", func(kind, name string) string {
			if file, ok := origins[kind+"/"+name]; ok {
				return file
			}
			return chart
		})
		if err != nil {
			return fmt.Errorf("chart %s: %w", chart, err)
		}
	}
	return nil
}

// renderKustomizations builds and validates each overlay. Without kustomize,
// every overlay is reported as not rendered.
func (v *k8sValidation) renderKustomizations(ctx context.Context, dirs []string) error {
	tool := v.opts.Kustomize
	for _, dir := range dirs {
		if !available(tool) {
			v.report.Unrendered = append(v.report.Unrendered, dir)
			continue
		}
		file := path.Join(dir, k8s.KustomizationFile(filepath.Join(v.target, filepath.FromSlash(dir))))
		cmd := kustomizeArgs(v.target, dir, tool)
		log.Printf("Running: %s", cmd)
		rendered, err := runner.Run(ctx, cmd, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			v.report.Findings = append(v.report.Findings,
				renderFailure(SourceKustomize, "kustomize", file, "Overlay does not build", err))
			continue
		}
		// kustomize adds no origin to what it emits unless the kustomization
		// asks for it, and DevDesk does not write into the file to ask. The
		// overlay is what was built, so it is what a finding points at.
		err = v.validateRendered(ctx, rendered, "kustomize", func(string, string) string { return file })
		if err != nil {
			return fmt.Errorf("overlay %s: %w", dir, err)
		}
	}
	return nil
}

// validateRendered runs kubeconform over rendered YAML and points each
// finding back at the file it came from. A rendered line is not a line of
// any file in the repository, so these findings carry none.
func (v *k8sValidation) validateRendered(ctx context.Context, rendered []byte, dialect string, origin func(kind, name string) string) error {
	if len(bytes.TrimSpace(rendered)) == 0 {
		return nil
	}
	cmd := kubeconformStdinArgs(rendered, v.kubeconform, v.opts)
	log.Printf("Running: %s", cmd)
	out, err := runner.Run(ctx, cmd, nil)
	if err := kubeconformFailure(out, err); err != nil {
		return err
	}
	noFile := func(string) ([]byte, error) { return nil, errors.New("rendered, not a file") }
	findings, skipped, err := parseKubeconformOutput(out, v.opts.KubernetesVersion, noFile, origin)
	if err != nil {
		return err
	}
	v.report.SkippedCustom += skipped
	for i := range findings {
		findings[i].IaCType = dialect
	}
	v.report.Findings = append(v.report.Findings, findings...)
	return nil
}

// helmLint runs helm lint and returns its warnings and errors. helm exits 1
// when a chart fails; its messages are still the answer. A run that printed
// nothing parseable is logged and yields nothing — the render that follows
// will say whether the chart is usable.
func helmLint(ctx context.Context, target, chart string, tool ToolSpec) []Finding {
	cmd := helmArgs(target, chart, tool, "lint")
	log.Printf("Running: %s", cmd)
	out, err := runner.Run(ctx, cmd, nil)
	findings, recognised := parseHelmLint(out, chart, target)
	if err != nil && !recognised {
		log.Printf("ERROR [scan/helm] lint %s: %v", chart, err)
	}
	return findings
}

// parseHelmLint turns helm lint's text into findings. The second result says
// the output was helm lint's at all.
//
// A message's path is usually relative to the chart ("templates/cm.yaml"),
// but not always: the missing-dependency warning names the chart directory
// absolutely — "/scan/charts/broken" in a container, measured on helm 4.3.0.
// Both are brought back to a path relative to the repository.
func parseHelmLint(out []byte, chart, target string) ([]Finding, bool) {
	file := func(p string) string { return helmLintPath(p, chart, target) }
	var findings []Finding
	recognised := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "==> Linting"):
			recognised = true
		case strings.HasPrefix(line, "Error "):
			// Printed instead of the messages when the chart could not be
			// loaded at all.
			recognised = true
			findings = append(findings, helmLintFinding(chart, SeverityHigh, strings.TrimPrefix(line, "Error ")))
		default:
			m := helmLintLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			recognised = true
			switch m[1] {
			case "ERROR":
				findings = append(findings, helmLintFinding(file(m[2]), SeverityHigh, m[3]))
			case "WARNING":
				findings = append(findings, helmLintFinding(file(m[2]), SeverityLow, m[3]))
			}
		}
	}
	return findings, recognised
}

// helmLintPath resolves a path helm lint printed to one relative to the
// repository.
func helmLintPath(p, chart, target string) string {
	p = strings.TrimSuffix(p, "/")
	switch {
	case p == containerScanPath:
		return "."
	case strings.HasPrefix(p, containerScanPath+"/"):
		return strings.TrimPrefix(p, containerScanPath+"/")
	case filepath.IsAbs(p):
		if rel, err := filepath.Rel(target, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(p)
	default:
		return path.Join(chart, p)
	}
}

func helmLintFinding(file string, severity SeverityLevel, msg string) Finding {
	return Finding{
		ID:          HelmLintID,
		Title:       msg,
		Description: "helm lint reported this about the chart.",
		Message:     msg,
		Severity:    severity,
		Source:      SourceHelm,
		File:        file,
		IaCType:     "helm",
	}
}

func hasError(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityHigh {
			return true
		}
	}
	return false
}

// renderFailure is a chart or an overlay that could not be rendered: nothing
// in it was validated, and `helm install` or `kubectl apply -k` would fail the
// same way.
func renderFailure(source, dialect, file, title string, err error) Finding {
	detail := err.Error()
	var exitErr *exitError
	if errors.As(err, &exitErr) && exitErr.Stderr != "" {
		detail = strings.TrimSpace(exitErr.Stderr)
	}
	first, _, _ := strings.Cut(detail, "\n")
	return Finding{
		ID:          K8sRenderID,
		Title:       title + ": " + first,
		Description: "The directory could not be rendered, so none of its resources were validated.",
		Message:     detail,
		Resolution:  "Render it locally and fix what the tool reports; a chart's dependencies must be vendored in charts/ or fetched with helm dependency build.",
		Severity:    SeverityHigh,
		Source:      source,
		File:        file,
		IaCType:     dialect,
	}
}

// helmOrigins maps each rendered resource, by kind and name, to the template
// it came from, relative to the repository. helm names the template under the
// chart's own name, which need not be its directory's.
func helmOrigins(rendered []byte, chart string) map[string]string {
	origins := map[string]string{}
	for _, doc := range splitYAMLDocuments(rendered) {
		var source string
		for _, line := range strings.Split(doc, "\n") {
			if m := helmSourceLine.FindStringSubmatch(line); m != nil {
				source = m[1]
				break
			}
		}
		if source == "" {
			continue
		}
		var meta struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if yaml.Unmarshal([]byte(doc), &meta) != nil || meta.Kind == "" {
			continue
		}
		key := meta.Kind + "/" + meta.Metadata.Name
		if _, seen := origins[key]; seen {
			continue
		}
		_, rest, _ := strings.Cut(source, "/")
		origins[key] = path.Join(chart, rest)
	}
	return origins
}

// splitYAMLDocuments splits a multi-document stream on its `---` separators.
func splitYAMLDocuments(stream []byte) []string {
	var docs []string
	var cur strings.Builder
	r := bufio.NewReader(bytes.NewReader(stream))
	for {
		line, err := r.ReadString('\n')
		if strings.TrimRight(line, "\r\n") == "---" {
			docs = append(docs, cur.String())
			cur.Reset()
		} else {
			cur.WriteString(line)
		}
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
	}
	return append(docs, cur.String())
}
