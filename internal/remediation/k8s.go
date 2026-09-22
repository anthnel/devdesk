package remediation

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/k8s"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// Kubernetes manifests (§3.80). The same discipline as the Dockerfile rules:
// an edit that is exactly right or a decline, never an approximation. What is
// new is that a manifest is YAML, so a fix may have to add a key rather than
// change a token — and that is still a byte-range edit here, computed from
// yaml.v3's node positions, never a re-serialisation of the document, which
// would drop the author's comments and formatting.
//
// Three rules, and the ones left out were each left out for a reason:
//
//   - KSV-0017 (privileged) and KSV-0001 (allowPrivilegeEscalation) name the
//     exact field and value to set, and the ids were read from trivy-checks'
//     rego sources (checks/kubernetes/privileged.rego and
//     can_elevate_its_own_privileges.rego), not guessed.
//   - K8S-API-REMOVED is fixed only where the Kubernetes deprecation guide says
//     "No notable changes" — renaming the apiVersion is then the whole
//     migration. Where it lists changes (Ingress, HPA, PodDisruptionBudget,
//     the apps/v1 workloads, CRDs, webhooks…), a rename would produce a
//     manifest that validates and behaves differently.
//   - runAsNonRoot, readOnlyRootFilesystem and dropping ALL capabilities pass
//     their rule and can stop the container from starting: whether the image
//     runs as root, writes to its filesystem or needs a capability is not in
//     the manifest. It is the USER 1000 lesson of §3.78 — the re-scan would
//     approve a pod that no longer runs.
//   - Resource requests and limits have no universal value.
//   - A seccomp profile can be set at the pod or the container level, and
//     which one the author meant is not something the finding says.

// The reasons the Kubernetes fixes decline.
const (
	ReasonNotAManifest = "The catalog only fixes plain Kubernetes manifests — a Helm chart or a Kustomize overlay is fixed in its values, template or patch"
	// ReasonContainerNotFound is a stale finding: the file no longer has the
	// container it named, at the lines it named.
	ReasonContainerNotFound = "The reported container is not at the reported lines of this file"
	ReasonNotPlainBoolean   = "The value is not a plain true/false this fix can rewrite"
	ReasonInlineMapping     = "This securityContext is written inline ({…}); the fix only edits block style"
	ReasonNoInsertionPoint  = "No key of this container starts its own line, so there is nowhere to insert the setting"
	ReasonNotPrivileged     = "The container is no longer privileged"
	ReasonEscalationOff     = "The container already sets allowPrivilegeEscalation: false"
	// ReasonPrivilegedContainer and ReasonSysAdmin are the API server's own
	// validation: it refuses allowPrivilegeEscalation: false on a container
	// that is privileged or adds CAP_SYS_ADMIN. The fix would pass Trivy's
	// rule and produce a manifest nobody can apply.
	ReasonPrivilegedContainer = "The container is privileged, and the API server refuses allowPrivilegeEscalation: false with it — fix KSV-0017 first"
	ReasonSysAdmin            = "The container adds CAP_SYS_ADMIN, and the API server refuses allowPrivilegeEscalation: false with it"
	ReasonNoSimpleMigration   = "Moving this kind to a served apiVersion changes more than the apiVersion — see the Kubernetes deprecation guide"
)

// containerInMessage is how Trivy's KSV rules name the container they are
// about: "Container 'api' of Deployment 'web' should set …".
var containerInMessage = regexp.MustCompile(`Container '([^']+)'`)

// renamedAPIs are the removals whose migration is the apiVersion alone:
// every entry is one the deprecation guide marks "No notable changes",
// checked against kubernetes/website on 2026-09-22. Keyed "apiVersion Kind".
var renamedAPIs = map[string]string{
	"batch/v1beta1 CronJob":                                "batch/v1",
	"storage.k8s.io/v1beta1 CSIStorageCapacity":            "storage.k8s.io/v1",
	"node.k8s.io/v1beta1 RuntimeClass":                     "node.k8s.io/v1",
	"apiregistration.k8s.io/v1beta1 APIService":            "apiregistration.k8s.io/v1",
	"coordination.k8s.io/v1beta1 Lease":                    "coordination.k8s.io/v1",
	"networking.k8s.io/v1beta1 IngressClass":               "networking.k8s.io/v1",
	"rbac.authorization.k8s.io/v1beta1 ClusterRole":        "rbac.authorization.k8s.io/v1",
	"rbac.authorization.k8s.io/v1beta1 ClusterRoleBinding": "rbac.authorization.k8s.io/v1",
	"rbac.authorization.k8s.io/v1beta1 Role":               "rbac.authorization.k8s.io/v1",
	"rbac.authorization.k8s.io/v1beta1 RoleBinding":        "rbac.authorization.k8s.io/v1",
	"scheduling.k8s.io/v1beta1 PriorityClass":              "scheduling.k8s.io/v1",
	"storage.k8s.io/v1beta1 CSIDriver":                     "storage.k8s.io/v1",
	"storage.k8s.io/v1beta1 CSINode":                       "storage.k8s.io/v1",
	"storage.k8s.io/v1beta1 StorageClass":                  "storage.k8s.io/v1",
	"storage.k8s.io/v1beta1 VolumeAttachment":              "storage.k8s.io/v1",
}

// plainManifest refuses a finding that is not about a file of plain YAML.
func plainManifest(f scan.Finding) string {
	if f.IaCType != "kubernetes" {
		return ReasonNotAManifest
	}
	return ""
}

// fixPrivileged satisfies KSV-0017 by turning privileged: true into false.
// The key exists — that is what the rule fired on — so this is a token swap.
func fixPrivileged(content []byte, f scan.Finding) ([]patch.Edit, string) {
	c, reason := reportedContainer(content, f)
	if reason != "" {
		return nil, reason
	}
	_, sc, _ := k8s.Entry(c, "securityContext")
	var value *yaml.Node
	if sc != nil {
		_, value, _ = k8s.Entry(sc, "privileged")
	}
	if value == nil || value.Value == "false" {
		return nil, ReasonNotPrivileged
	}
	return swapBoolean(content, value)
}

// fixPrivilegeEscalation satisfies KSV-0001: allowPrivilegeEscalation: false on
// the container Trivy named. The rule fires on a value of true and on no value
// at all, so this is either a swap or an insertion, and an insertion goes
// above an existing key, at that key's indentation.
func fixPrivilegeEscalation(content []byte, f scan.Finding) ([]patch.Edit, string) {
	c, reason := reportedContainer(content, f)
	if reason != "" {
		return nil, reason
	}
	_, sc, hasContext := k8s.Entry(c, "securityContext")
	if hasContext {
		if reason := escalationRefused(sc); reason != "" {
			return nil, reason
		}
		if _, value, ok := k8s.Entry(sc, "allowPrivilegeEscalation"); ok {
			if value.Value == "false" {
				return nil, ReasonEscalationOff
			}
			return swapBoolean(content, value)
		}
		if sc.Kind != yaml.MappingNode || sc.Style&yaml.FlowStyle != 0 || len(sc.Content) == 0 {
			return nil, ReasonInlineMapping
		}
		return insertAbove(content, sc.Content[0], "allowPrivilegeEscalation: false")
	}
	for i := 0; i < len(c.Content); i += 2 {
		if _, _, ok := k8s.OwnLineIndent(content, c.Content[i]); ok {
			nl := k8s.LineEnding(content)
			return insertAbove(content, c.Content[i], "securityContext:"+nl+"{indent}  allowPrivilegeEscalation: false")
		}
	}
	return nil, ReasonNoInsertionPoint
}

// escalationRefused is the API server's validation of a securityContext that
// would carry allowPrivilegeEscalation: false.
func escalationRefused(sc *yaml.Node) string {
	if k8s.Scalar(sc, "privileged") == "true" {
		return ReasonPrivilegedContainer
	}
	if _, caps, ok := k8s.Entry(sc, "capabilities"); ok {
		if _, add, ok := k8s.Entry(caps, "add"); ok {
			for _, c := range add.Content {
				if name := strings.TrimPrefix(strings.ToUpper(c.Value), "CAP_"); name == "SYS_ADMIN" {
					return ReasonSysAdmin
				}
			}
		}
	}
	return ""
}

// fixRemovedAPI satisfies K8S-API-REMOVED by renaming the apiVersion, for the
// kinds where that is the entire migration.
func fixRemovedAPI(content []byte, f scan.Finding) ([]patch.Edit, string) {
	if reason := plainManifest(f); reason != "" {
		return nil, reason
	}
	doc, ok := k8s.DocumentAt(content, f.Line)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	_, version, ok := k8s.Entry(doc, "apiVersion")
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	target, ok := renamedAPIs[version.Value+" "+k8s.Scalar(doc, "kind")]
	if !ok {
		return nil, ReasonNoSimpleMigration
	}
	start, end, ok := k8s.PlainScalarSpan(content, version)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	return []patch.Edit{{Span: patch.Span{Start: start, End: end}, Old: version.Value, New: target}}, ""
}

// reportedContainer finds the container a KSV finding is about: in the
// document holding the reported line, by the name Trivy's message gives. A
// finding whose file no longer matches is declined rather than applied to
// whatever sits there now.
func reportedContainer(content []byte, f scan.Finding) (*yaml.Node, string) {
	if reason := plainManifest(f); reason != "" {
		return nil, reason
	}
	m := containerInMessage.FindStringSubmatch(f.Message)
	if m == nil || f.Line <= 0 {
		return nil, ReasonUnreadableSpan
	}
	doc, ok := k8s.DocumentAt(content, f.Line)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	for _, c := range k8s.Containers(doc) {
		if k8s.Scalar(c, "name") != m[1] {
			continue
		}
		if f.EndLine > 0 && (c.Line < f.Line || c.Line > f.EndLine) {
			continue
		}
		return c, ""
	}
	return nil, ReasonContainerNotFound
}

// swapBoolean replaces a plain true with false.
func swapBoolean(content []byte, value *yaml.Node) ([]patch.Edit, string) {
	if value.Value != "true" {
		return nil, ReasonNotPlainBoolean
	}
	start, end, ok := k8s.PlainScalarSpan(content, value)
	if !ok {
		return nil, ReasonNotPlainBoolean
	}
	return []patch.Edit{{Span: patch.Span{Start: start, End: end}, Old: value.Value, New: "false"}}, ""
}

// insertAbove adds text as new lines above key, at key's indentation. A
// "{indent}" in text stands for that indentation on the lines after the first.
func insertAbove(content []byte, key *yaml.Node, text string) ([]patch.Edit, string) {
	lineStart, indent, ok := k8s.OwnLineIndent(content, key)
	if !ok {
		return nil, ReasonNoInsertionPoint
	}
	text = strings.ReplaceAll(text, "{indent}", indent)
	return []patch.Edit{{
		Span: patch.Span{Start: lineStart, End: lineStart},
		New:  indent + text + k8s.LineEnding(content),
	}}, ""
}
