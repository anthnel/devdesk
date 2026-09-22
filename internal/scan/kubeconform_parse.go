package scan

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthnel/devdesk/internal/k8s"
)

// The identities a kubeconform finding carries. They are DevDesk's, not the
// tool's — kubeconform has no rule ids — and they are stable so that a
// re-scan can say whether a finding went away.
const (
	// K8sSchemaID is a resource that does not match its kind's schema.
	K8sSchemaID = "K8S-SCHEMA"
	// K8sAPIRemovedID is a built-in kind asked for under an apiVersion the
	// target release does not serve.
	K8sAPIRemovedID = "K8S-API-REMOVED"
	// K8sParseID is a document kubeconform could not read as a resource.
	K8sParseID = "K8S-PARSE"
)

// K8sDeprecationGuide is where a removed apiVersion is explained, per kind.
const K8sDeprecationGuide = "https://kubernetes.io/docs/reference/using-api/deprecation-guide/"

// kubeconform's status strings, as pkg/output/json.go writes them.
const (
	kubeconformInvalid = "statusInvalid"
	kubeconformError   = "statusError"
)

const kubeconformMissingSchema = "could not find schema for "

// builtinGroups are the API groups Kubernetes itself serves. A resource of one
// of them with no schema was asked for under a version the release no longer
// has; a resource of any other group is a custom one, whose schema is in a CRD
// kubeconform does not read.
//
// A closed list, not a suffix rule: several CRD groups end in ".k8s.io" too —
// the Gateway API's gateway.networking.k8s.io, the snapshot controller's
// snapshot.storage.k8s.io — and a suffix rule would report every Gateway as a
// removed API.
var builtinGroups = map[string]bool{
	"":                             true, // core, "v1"
	"admissionregistration.k8s.io": true,
	"apiextensions.k8s.io":         true,
	"apiregistration.k8s.io":       true,
	"apps":                         true,
	"authentication.k8s.io":        true,
	"authorization.k8s.io":         true,
	"autoscaling":                  true,
	"batch":                        true,
	"certificates.k8s.io":          true,
	"coordination.k8s.io":          true,
	"discovery.k8s.io":             true,
	"events.k8s.io":                true,
	"extensions":                   true,
	"flowcontrol.apiserver.k8s.io": true,
	"internal.apiserver.k8s.io":    true,
	"networking.k8s.io":            true,
	"node.k8s.io":                  true,
	"policy":                       true,
	"rbac.authorization.k8s.io":    true,
	"resource.k8s.io":              true,
	"scheduling.k8s.io":            true,
	"storage.k8s.io":               true,
	"storagemigration.k8s.io":      true,
}

// additionalProperty is how the schema library words an unknown field under
// -strict. The path it comes with is the parent's, so the key named here is
// what locates the line.
var additionalProperty = regexp.MustCompile(`additional properties? '([^']+)'`)

type kubeconformOutput struct {
	Resources []kubeconformResource `json:"resources"`
}

type kubeconformResource struct {
	Filename         string                    `json:"filename"`
	Kind             string                    `json:"kind"`
	Name             string                    `json:"name"`
	Version          string                    `json:"version"`
	Status           string                    `json:"status"`
	Msg              string                    `json:"msg"`
	ValidationErrors []kubeconformFieldProblem `json:"validationErrors"`
}

type kubeconformFieldProblem struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
}

// parseKubeconformOutput turns the report into findings, and counts the custom
// resources nobody could validate. read returns a manifest's content by the
// name kubeconform reported, to find the line a pointer designates; a file it
// cannot read keeps its findings, at line 0.
//
// origin, when not nil, names the file a resource came from — for rendered
// input, where kubeconform can only say "stdin".
func parseKubeconformOutput(out []byte, version string, read func(string) ([]byte, error), origin func(kind, name string) string) ([]Finding, int, error) {
	var report kubeconformOutput
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, 0, fmt.Errorf("parsing kubeconform output: %w", err)
	}

	contents := map[string][]byte{}
	content := func(name string) []byte {
		if c, ok := contents[name]; ok {
			return c
		}
		c, err := read(name)
		if err != nil {
			c = nil
		}
		contents[name] = c
		return c
	}

	var findings []Finding
	skipped := 0
	for _, r := range report.Resources {
		if origin != nil {
			r.Filename = origin(r.Kind, r.Name)
		}
		switch r.Status {
		case kubeconformInvalid:
			findings = append(findings, schemaFindings(r, content(r.Filename))...)
		case kubeconformError:
			f, custom := errorFinding(r, version, content(r.Filename))
			if custom {
				skipped++
				continue
			}
			findings = append(findings, f)
		}
	}
	return findings, skipped, nil
}

// schemaFindings is one finding per field the schema rejected, so each one has
// its own line and can be fixed on its own.
func schemaFindings(r kubeconformResource, content []byte) []Finding {
	problems := r.ValidationErrors
	if len(problems) == 0 {
		// An invalid resource with no field detail still gets reported, on the
		// resource as a whole.
		problems = []kubeconformFieldProblem{{Msg: r.Msg}}
	}
	out := make([]Finding, 0, len(problems))
	for _, p := range problems {
		f := k8sFinding(r, K8sSchemaID)
		f.Title = fmt.Sprintf("%s: %s", resourceLabel(r), p.Msg)
		f.Description = fmt.Sprintf("The resource does not match the schema of %s %s: the API server would reject it.", r.Version, r.Kind)
		f.Message = strings.TrimSpace(pointerLabel(p.Path) + " " + p.Msg)
		f.Resolution = "Correct the field so it matches the API's schema for this kind and apiVersion."
		f.Line, f.EndLine = locateProblem(content, r, p)
		out = append(out, f)
	}
	return out
}

// errorFinding classifies a resource kubeconform could not validate. The
// second result says it is a custom resource, which is skipped rather than
// reported: its schema is not missing, it lives somewhere kubeconform does not
// look.
func errorFinding(r kubeconformResource, version string, content []byte) (Finding, bool) {
	if strings.HasPrefix(r.Msg, kubeconformMissingSchema) {
		if !builtinGroups[apiGroup(r.Version)] {
			return Finding{}, true
		}
		f := k8sFinding(r, K8sAPIRemovedID)
		f.Title = fmt.Sprintf("%s: %s %s is not served by Kubernetes %s", resourceLabel(r), r.Version, r.Kind, version)
		f.Description = "This apiVersion has been removed from the Kubernetes release the manifests are validated against: the API server would refuse the resource."
		f.Message = r.Msg
		f.Resolution = fmt.Sprintf("Migrate %s to an apiVersion Kubernetes %s serves; some migrations also change fields.", r.Kind, version)
		f.References = []string{K8sDeprecationGuide}
		if span, ok := k8s.DocumentField(content, r.Kind, r.Name, "apiVersion"); ok {
			f.Line, f.EndLine = span.Line, span.EndLine
		}
		return f, false
	}
	f := k8sFinding(r, K8sParseID)
	f.Title = "Cannot be read as a Kubernetes resource"
	if r.Kind != "" {
		f.Title = resourceLabel(r) + ": cannot be read as a Kubernetes resource"
	}
	f.Description = "kubeconform could not parse this document as a resource."
	f.Message = r.Msg
	f.Resolution = "Check the document's YAML syntax and that it carries apiVersion, kind and metadata."
	return f, false
}

// k8sFinding is what every kubeconform finding shares.
func k8sFinding(r kubeconformResource, id string) Finding {
	return Finding{
		ID:       id,
		Severity: SeverityHigh,
		Source:   SourceKubeconform,
		File:     r.Filename,
		IaCType:  "kubernetes",
	}
}

// locateProblem finds the line of one rejected field. An unknown field is
// reported at its parent, with the key in the message; the key's own line is
// the one to show, and the parent's is the fallback.
func locateProblem(content []byte, r kubeconformResource, p kubeconformFieldProblem) (int, int) {
	if content == nil {
		return 0, 0
	}
	if m := additionalProperty.FindStringSubmatch(p.Msg); m != nil {
		if span, ok := k8s.Locate(content, r.Kind, r.Name, strings.TrimSuffix(p.Path, "/")+"/"+escapePointer(m[1])); ok {
			return span.Line, span.EndLine
		}
	}
	if span, ok := k8s.Locate(content, r.Kind, r.Name, p.Path); ok {
		return span.Line, span.EndLine
	}
	return 0, 0
}

// apiGroup is the group of an apiVersion: "apps" for apps/v1, "" for v1.
func apiGroup(apiVersion string) string {
	if i := strings.LastIndex(apiVersion, "/"); i >= 0 {
		return apiVersion[:i]
	}
	return ""
}

func resourceLabel(r kubeconformResource) string {
	if r.Name == "" {
		return r.Kind
	}
	return r.Kind + "/" + r.Name
}

func pointerLabel(path string) string {
	if path == "" {
		return ""
	}
	return path + ":"
}

func escapePointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
