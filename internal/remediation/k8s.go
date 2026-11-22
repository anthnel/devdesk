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
	// The host namespace fixes (DEVDESK-687).
	ReasonHostFieldNotTrue = "The pod spec no longer sets this field to true"
	ReasonSharedByAlias    = "This pod spec or value is shared through a YAML alias, so editing it would change every place that uses it"
	ReasonAmbiguousField   = "This field cannot be located without ambiguity in the pod spec (duplicate or merged key)"
	// ReasonWorkloadChanged is a stale host namespace finding: the workload or
	// the lines at the reported position are not the ones the message names.
	ReasonWorkloadChanged   = "The manifest changed since the scan: the workload at the reported lines is not the one the finding names, scan again"
	ReasonNoSimpleMigration = "Moving this kind to a served apiVersion changes more than the apiVersion — see the Kubernetes deprecation guide"
)

// workloadInMessage is how Trivy's pod-level rules name the workload they are
// about: "Pod 'all' should not set 'spec.template.spec.hostIPC' to true".
var workloadInMessage = regexp.MustCompile(`^(\w+) '([^']+)'`)

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

// fixHostNamespace is the fix of hostNetwork, hostPID and hostIPC: set the
// field of the pod spec to false (DEVDESK-687). The finding is about the pod,
// not a container, and Trivy words its path as spec.template.spec.<field> even
// for a Pod, so the pod spec comes from the workload's kind, not from the
// message.
func fixHostNamespace(field string) func([]byte, scan.Finding) ([]patch.Edit, string) {
	return func(content []byte, f scan.Finding) ([]patch.Edit, string) {
		value, reason := podSpecFlag(content, f, field)
		if reason != "" {
			return nil, reason
		}
		if value.Kind == yaml.ScalarNode && value.Style == 0 && value.Value == "false" {
			return nil, ReasonHostFieldNotTrue
		}
		return swapBoolean(content, value)
	}
}

// podSpecFlag finds the value node of field in the pod spec of the workload
// the finding is in, or says why it cannot be located exactly. It declines a
// pod spec or a value that other places share (a YAML alias, or an anchor
// something else points at: the edit would change them too), a key written
// twice or reached through a merge key (which of them is in force is not for
// this fix to decide), and a field the pod spec does not set. The value is
// returned as written, so the caller still sees a quoted or tagged scalar.
func podSpecFlag(content []byte, f scan.Finding, field string) (*yaml.Node, string) {
	if reason := plainManifest(f); reason != "" {
		return nil, reason
	}
	if f.Line <= 0 {
		return nil, ReasonUnreadableSpan
	}
	doc, ok := k8s.DocumentAt(content, f.Line)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	specs := k8s.PodSpecs(doc)
	if len(specs) != 1 {
		return nil, ReasonUnreadableSpan
	}
	spec := specs[0].Spec
	if !namesWorkload(doc, spec, f) {
		return nil, ReasonWorkloadChanged
	}
	if isShared(doc, spec) {
		return nil, ReasonSharedByAlias
	}
	var value *yaml.Node
	merged := false
	for i := 0; i+1 < len(spec.Content); i += 2 {
		key := spec.Content[i]
		switch {
		case key.Kind == yaml.AliasNode:
			return nil, ReasonAmbiguousField
		case key.Tag == "!!merge":
			merged = true
		case key.Value == field:
			if value != nil {
				return nil, ReasonAmbiguousField
			}
			value = spec.Content[i+1]
		}
	}
	switch {
	case value == nil && merged:
		return nil, ReasonAmbiguousField
	case value == nil:
		return nil, ReasonHostFieldNotTrue
	case value.Kind == yaml.AliasNode || isShared(doc, value):
		return nil, ReasonSharedByAlias
	}
	return value, ""
}

// namesWorkload reports whether doc is the workload Trivy's message names (kind
// and metadata.name) and its pod spec starts inside the lines the finding
// reported: the file may have changed since the scan, and the line alone then
// points at whatever sits there now. A message that names no workload gives
// nothing to compare, so it is refused rather than trusted.
func namesWorkload(doc, spec *yaml.Node, f scan.Finding) bool {
	m := workloadInMessage.FindStringSubmatch(f.Message)
	if m == nil || k8s.Scalar(doc, "kind") != m[1] {
		return false
	}
	_, meta, ok := k8s.Entry(doc, "metadata")
	if !ok || k8s.Scalar(meta, "name") != m[2] {
		return false
	}
	return spec.Line >= f.Line && (f.EndLine <= 0 || spec.Line <= f.EndLine)
}

// isShared reports whether an alias in doc points at n or at a node that
// contains it: editing n then changes what that alias stands for too.
func isShared(doc, n *yaml.Node) bool {
	shared := false
	var walk func(*yaml.Node)
	walk = func(cur *yaml.Node) {
		if shared {
			return
		}
		if cur.Kind == yaml.AliasNode {
			shared = cur.Alias != nil && contains(cur.Alias, n)
			return
		}
		for _, c := range cur.Content {
			walk(c)
		}
	}
	walk(doc)
	return shared
}

// contains reports whether n is root or sits below it, without following aliases.
func contains(root, n *yaml.Node) bool {
	if root == n {
		return true
	}
	for _, c := range root.Content {
		if c.Kind != yaml.AliasNode && contains(c, n) {
			return true
		}
	}
	return false
}
