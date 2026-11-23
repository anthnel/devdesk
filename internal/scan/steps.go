package scan

import (
	"fmt"
	"strings"
)

// The numbered steps DevDesk writes for the findings it builds itself
// (DEVDESK-752). Every function here is pure: the caller says what it ran and
// what the tool printed. A step is one imperative sentence in English US with
// one action; alternatives are separate steps. The Resolution of the same
// finding is untouched.

// kustomizeMountStep is added for a kustomize image that could not find a path
// (see mountResolution): the container sees only what DevDesk mounts.
const kustomizeMountStep = "If this reference points outside the directory DevDesk mounts into the container, " +
	"scan the enclosing directory or set scan.tools.kustomize.source: binary."

// isLoadRestrictor reports whether kustomize refused a file outside the overlay
// directory.
func isLoadRestrictor(detail string) bool {
	return strings.Contains(detail, "security; file") && strings.Contains(detail, "is not in or below")
}

// renderSteps are the steps for a chart or an overlay that did not render.
// target is the overlay directory or the chart, relative to the scanned
// directory; detail is what the tool printed.
func renderSteps(source, target string, from ToolSource, detail string) []string {
	if source != SourceKustomize {
		return []string{
			fmt.Sprintf("Run helm template %s and fix the first error it reports.", target),
			fmt.Sprintf("If a dependency is missing, run helm dependency build %s.", target),
		}
	}
	var steps []string
	if isLoadRestrictor(detail) {
		steps = []string{
			"Reference a directory that has its own kustomization.yaml, or",
			fmt.Sprintf("move the file below %s.", target),
		}
	} else {
		steps = []string{fmt.Sprintf("Run kustomize build %s and fix the first error it reports.", target)}
	}
	if from == ToolSourceContainer &&
		(strings.Contains(detail, "evalsymlink failure") || strings.Contains(detail, "no such file or directory")) {
		steps = append(steps, kustomizeMountStep)
	}
	return steps
}

// helmLintSteps are the steps for one message of helm lint: file is the finding's
// file and chart the chart directory.
func helmLintSteps(file, chart string) []string {
	return []string{
		fmt.Sprintf("Fix what helm lint reports in %s.", file),
		fmt.Sprintf("Run helm lint %s again to confirm.", chart),
	}
}

// schemaSteps are the steps for a field the schema rejected. rendered is "" for
// a plain manifest and the renderer's dialect (helm, kustomize) for a rendered
// one.
func schemaSteps(r kubeconformResource, p kubeconformFieldProblem, version, rendered string) []string {
	field := p.Path
	if m := additionalProperty.FindStringSubmatch(p.Msg); m != nil {
		field = strings.TrimSuffix(p.Path, "/") + "/" + m[1]
	}
	if field == "" {
		field = "the resource"
	}
	release := "Kubernetes"
	if version != "" {
		release += " " + version
	}
	steps := []string{fmt.Sprintf("Correct %s so it matches the %s %s schema of %s.", field, r.Version, r.Kind, release)}
	if rendered != "" {
		steps = append(steps, fmt.Sprintf("Render it with %s and validate the output against %s.", rendered, release))
	}
	return steps
}

// secretSteps are the steps for a secret Gitleaks or Trivy found. It returns a
// new slice on every call: a shared one would let a caller change every finding.
func secretSteps() []string {
	return []string{
		"Revoke or rotate this secret where it was issued: treat it as exposed.",
		"Remove it from the file and read it from a secret store or an environment variable.",
	}
}
