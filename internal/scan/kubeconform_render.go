package scan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

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

// renderScope is where a renderer runs. A binary runs in the target and sees
// the whole host. A container sees only mount, read-only at /scan, and works in
// the target inside it: mount is the target itself, or the git repository
// holding it, so an overlay or a chart can reach a sibling of the target as it
// does on the host (DEVDESK-742).
type renderScope struct {
	target string
	mount  string
	// rel is the target relative to mount, with forward slashes; "" when the
	// target is what is mounted.
	rel string
}

// targetOnly mounts the target and nothing else.
func targetOnly(target string) renderScope { return renderScope{target: target, mount: target} }

// workdir is the target inside the container.
func (s renderScope) workdir() string { return path.Join(containerScanPath, s.rel) }

// mountedTarget is the target as a path under mount on the host.
func (s renderScope) mountedTarget() string { return filepath.Join(s.mount, filepath.FromSlash(s.rel)) }

// userHomeDir is the home directory the mount guard keeps out of reach. Tests
// swap it.
var userHomeDir = os.UserHomeDir

// resolveRenderScope asks git, in the target, for the repository holding it.
// Without an answer (no git, not a repository), or when the repository's
// configuration moves its work tree elsewhere (core.worktree, which a cloned
// .git file can bring along), the target alone is mounted.
func resolveRenderScope(ctx context.Context, target string) renderScope {
	git := func(args ...string) ([]byte, error) {
		return runner.Run(ctx, toolCmd{Name: "git", Args: args, Dir: target, Env: gitEnviron(os.Environ())}, nil)
	}
	out, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		log.Printf("[scan/k8s] %s: no git repository to mount, the renderers see the target only: %v", target, err)
		return targetOnly(target)
	}
	worktree, err := git("config", "--get", "core.worktree")
	var exitErr *exitError
	unset := errors.As(err, &exitErr) && exitErr.Code == 1 && len(bytes.TrimSpace(worktree)) == 0
	if !unset {
		log.Printf("[scan/k8s] %s: core.worktree is set or unreadable (%q, %v), the renderers see the target only",
			target, strings.TrimSpace(string(worktree)), err)
		return targetOnly(target)
	}
	scope, ok := widerScope(target, strings.TrimSpace(string(out)))
	if !ok {
		return targetOnly(target)
	}
	log.Printf("[scan/k8s] renderers in a container mount %s, the repository holding %s", scope.mount, target)
	return scope
}

// gitEnviron is env without the variables that point git at another
// repository, work tree or configuration: the mount is decided by the target
// alone.
func gitEnviron(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "GIT_DIR", name == "GIT_WORK_TREE", name == "GIT_COMMON_DIR",
			name == "GIT_CEILING_DIRECTORIES", strings.HasPrefix(name, "GIT_CONFIG"):
			continue
		}
		out = append(out, kv)
	}
	return out
}

// widerScope mounts top, the repository git named, when it is safe to: top is
// a strict ancestor of the target once symlinks are resolved, neither the
// filesystem root nor the home directory nor anything above it, and it holds
// the repository's .git itself (gitDirIsHere). Anything else, an unknown home
// included, keeps the target alone.
func widerScope(target, top string) (renderScope, bool) {
	if top == "" || !filepath.IsAbs(top) {
		return renderScope{}, false
	}
	root, err := filepath.EvalSymlinks(top)
	if err != nil {
		return renderScope{}, false
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return renderScope{}, false
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return renderScope{}, false
	}
	if filepath.Dir(root) == root {
		return renderScope{}, false
	}
	home, err := userHomeDir()
	if err != nil || home == "" {
		return renderScope{}, false
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if r, err := filepath.Rel(root, filepath.Clean(home)); err == nil &&
		(r == "." || (r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))) {
		return renderScope{}, false
	}
	if !gitDirIsHere(root) {
		return renderScope{}, false
	}
	return renderScope{target: target, mount: root, rel: filepath.ToSlash(rel)}, true
}

// maxGitFile bounds what is read of a .git file or a worktree's gitdir file:
// one line, a path.
const maxGitFile = 4096

// gitDirIsHere reports whether root holds its repository's .git: a directory,
// or the file of a linked worktree ("gitdir: <dir>") whose git directory names
// root's .git back in its own gitdir file. A symlink, a file that says
// anything else, or a git directory that does not point back is refused: git
// was then told where the work tree is, rather than finding it.
func gitDirIsHere(root string) bool {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return true
	}
	if !info.Mode().IsRegular() {
		return false
	}
	line, ok := readGitFile(dotGit)
	gitDir, found := strings.CutPrefix(line, "gitdir: ")
	if !ok || !found {
		return false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	back, ok := readGitFile(filepath.Join(gitDir, "gitdir"))
	if !ok {
		return false
	}
	if !filepath.IsAbs(back) {
		back = filepath.Join(gitDir, back)
	}
	a, errA := filepath.EvalSymlinks(back)
	b, errB := filepath.EvalSymlinks(dotGit)
	return errA == nil && errB == nil && a == b
}

// readGitFile reads the one line of a small regular file.
func readGitFile(p string) (string, bool) {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxGitFile {
		return "", false
	}
	data, err := os.ReadFile(p) //nolint:gosec // a .git file of the repository being scanned
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// renderer runs one tool in the repository, the way every other tool runs:
// in the target for a binary; for an image, with the scope's mount read-only
// at /scan and the target inside it as the working directory.
func renderer(scope renderScope, tool ToolSpec, defaultImage, binary string, args ...string) toolCmd {
	// helm gets them on lint and on template alike: only the flags the two
	// share (--values, --set…) make sense there.
	args = afterSubcommand(args, tool.Args)
	if tool.Source != ToolSourceContainer {
		name := tool.Binary
		if name == "" {
			name = binary
		}
		return toolCmd{Name: name, Args: args, Dir: scope.target}
	}
	return containerRun{
		Flags: []string{"-v", scope.mount + ":" + containerScanPath + ":ro", "-w", scope.workdir()},
		Image: orDefault(tool.Image, defaultImage),
		Argv:  args,
	}.cmd()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func helmArgs(scope renderScope, chart string, tool ToolSpec, sub string) toolCmd {
	return renderer(scope, tool, DefaultHelmImage, "helm", sub, chart)
}

func kustomizeArgs(scope renderScope, dir string, tool ToolSpec) toolCmd {
	return renderer(scope, tool, DefaultKustomizeImage, "kustomize", "build", dir)
}

// kubeconformStdinArgs validates what arrives on stdin. A container gets -i
// and no repository mount: it reads nothing but its input and its cache.
func kubeconformStdinArgs(rendered []byte, tool ToolSpec, opts KubeconformOptions) toolCmd {
	flags := kubeconformFlags(tool, opts)
	if tool.Source != ToolSourceContainer {
		return toolCmd{Name: kubeconformBinary(tool), Args: append(flags, "-"), Stdin: rendered}
	}
	engineArgs := []string{"-i"}
	if opts.CacheDir != "" {
		engineArgs = append(engineArgs, "-v", opts.CacheDir+":"+kubeconformCacheMount)
	}
	return containerRun{
		Flags: engineArgs, Image: kubeconformImage(tool.Image), Argv: append(flags, "-"), Stdin: rendered,
	}.cmd()
}

// k8sValidation is one schema stage over one repository: the tools it has,
// what it validates against, and the report it fills in.
type k8sValidation struct {
	target string
	// scope is where helm and kustomize run, resolved once per stage.
	scope       renderScope
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
		lint := helmLint(ctx, v.scope, chart, tool)
		v.report.Findings = append(v.report.Findings, lint...)

		cmd := helmArgs(v.scope, chart, tool, "template")
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
		cmd := kustomizeArgs(v.scope, dir, tool)
		log.Printf("Running: %s", cmd)
		rendered, err := runner.Run(ctx, cmd, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			v.report.Findings = append(v.report.Findings, v.overlayFailure(ctx, tool, dir, file, err))
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

// overlayFailure is the finding for an overlay kustomize did not build. In a
// container, an overlay that failed to accumulate its resources and reaches a
// directory outside the mount failed because DevDesk could not show it that
// directory, not because it is broken: it is reported as not validated, LOW,
// rather than blamed (DEVDESK-742), and kustomize's own error goes to the log.
// Any other failure keeps the HIGH finding and kustomize's whole message.
func (v *k8sValidation) overlayFailure(ctx context.Context, tool ToolSpec, dir, file string, err error) Finding {
	if tool.Source == ToolSourceContainer && strings.Contains(err.Error(), "accumulating resources") {
		overlay := filepath.Join(v.scope.mountedTarget(), filepath.FromSlash(dir))
		if from, ref, ok := k8s.EscapingReference(ctx, v.scope.mount, overlay); ok {
			log.Printf("[scan/k8s] overlay %s not built: %s references %s, outside what the container sees (%s): %v",
				dir, from, ref, v.scope.mount, err)
			return outsideMountFinding(file, v.relToTarget(from), dir, ref)
		}
	}
	return renderFailure(SourceKustomize, "kustomize", file, "Overlay does not build", err)
}

// relToTarget is a real host path under the mount, relative to the target
// with forward slashes. The target is resolved too, as EscapingReference
// resolves what it returns.
func (v *k8sValidation) relToTarget(p string) string {
	target := v.scope.mountedTarget()
	if real, err := filepath.EvalSymlinks(target); err == nil {
		target = real
	}
	rel, err := filepath.Rel(target, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// outsideMountFinding says an overlay was not validated because it reaches
// outside the scanned directory. from is the kustomization holding the
// reference, relative to the target; it is named only when it is not the
// overlay's own.
func outsideMountFinding(file, from, dir, ref string) Finding {
	ref, from = printable(ref), printable(from)
	where := ref
	if from != dir {
		where = ref + ", from " + from
	}
	msg := "Overlay not validated: references a path outside the scanned directory (" + where + ")"
	return Finding{
		ID:    K8sRenderID,
		Title: msg,
		Description: "kustomize runs in a container that sees only the scanned directory, or the git repository " +
			"holding it, read-only. This overlay reaches beyond it, so it was not built and none of its resources " +
			"were validated. That says nothing against the overlay itself.",
		Message: msg,
		Resolution: "Scan the directory that holds everything the overlay references, or use a kustomize binary " +
			"(scan.tools.kustomize.source: binary), which sees the whole filesystem.",
		Severity: SeverityLow,
		Source:   SourceKustomize,
		File:     file,
		IaCType:  "kustomize",
	}
}

// printable drops the control characters of a path the repository's author
// wrote, so no escape sequence or line break reaches a finding.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
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
func helmLint(ctx context.Context, scope renderScope, chart string, tool ToolSpec) []Finding {
	cmd := helmArgs(scope, chart, tool, "lint")
	log.Printf("Running: %s", cmd)
	out, err := runner.Run(ctx, cmd, nil)
	findings, recognised := parseHelmLint(out, chart, scope)
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
// Both are brought back to a path relative to the target.
func parseHelmLint(out []byte, chart string, scope renderScope) ([]Finding, bool) {
	file := func(p string) string { return helmLintPath(p, chart, scope) }
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
// target. In a container the target is the scope's working directory, which is
// /scan itself unless the git repository around the target is mounted.
func helmLintPath(p, chart string, scope renderScope) string {
	p = strings.TrimSuffix(p, "/")
	switch {
	case p == containerScanPath || strings.HasPrefix(p, containerScanPath+"/"):
		if rel, err := filepath.Rel(filepath.FromSlash(scope.workdir()), filepath.FromSlash(p)); err == nil {
			return filepath.ToSlash(rel)
		}
		return p
	case filepath.IsAbs(p):
		if rel, err := filepath.Rel(scope.target, p); err == nil && !strings.HasPrefix(rel, "..") {
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
		Resolution:  renderResolution(source, detail),
		Severity:    SeverityHigh,
		Source:      source,
		File:        file,
		IaCType:     dialect,
	}
}

// How to fix a chart or an overlay that does not render, in the renderer's own
// terms.
const (
	helmRenderResolution = "Render it locally with helm template and fix what it reports; " +
		"a chart's dependencies must be vendored in charts/ or fetched with helm dependency build."
	kustomizeRenderResolution = "Build it locally with kustomize build (or kubectl kustomize) and fix what it reports."
	// kustomize's load restrictor (LoadRestrictionsRootOnly, its default)
	// refuses a file outside the kustomization's directory, with this message.
	// The flag is not reserved (ToolKustomize.ReservedArgs) and lands after
	// `build` (afterSubcommand), where kustomize accepts it.
	loadRestrictorResolution = "kustomize refuses by default a file outside the overlay directory " +
		"(same as kubectl apply -k). Reference a directory with its own kustomization instead, " +
		"or, if your deployment tool allows it, set " +
		"scan.tools.kustomize.args: [--load-restrictor=LoadRestrictionsNone]."
)

// renderResolution picks the advice for a render failure from the renderer
// and what it printed.
func renderResolution(source, detail string) string {
	if source != SourceKustomize {
		return helmRenderResolution
	}
	if strings.Contains(detail, "security; file") && strings.Contains(detail, "is not in or below") {
		return loadRestrictorResolution
	}
	return kustomizeRenderResolution
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
