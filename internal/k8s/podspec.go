package k8s

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// PodSpec is one pod spec of a workload and its containers (containers,
// initContainers and ephemeralContainers).
type PodSpec struct {
	Spec       *yaml.Node
	Containers []*yaml.Node
}

// seccompAnnotationPrefix is the pre-1.19 way to set a container's seccomp
// profile. Trivy still reads it, so the type in force has to as well.
const seccompAnnotationPrefix = "container.seccomp.security.alpha.kubernetes.io/"

// podSpecPaths is where each workload kind keeps its pod spec, as the keys to
// follow from the document's root. The kind decides, not a search for a
// "containers" key: a YAML anchor or an unrelated field named that way is not
// a pod spec.
var podSpecPaths = map[string][]string{
	"Pod":                   {"spec"},
	"PodTemplate":           {"template", "spec"},
	"Deployment":            {"spec", "template", "spec"},
	"StatefulSet":           {"spec", "template", "spec"},
	"DaemonSet":             {"spec", "template", "spec"},
	"ReplicaSet":            {"spec", "template", "spec"},
	"ReplicationController": {"spec", "template", "spec"},
	"Job":                   {"spec", "template", "spec"},
	"CronJob":               {"spec", "jobTemplate", "spec", "template", "spec"},
}

// PodSpecs returns every pod spec of a workload document, aliases resolved.
// A document that is not a workload has none.
func PodSpecs(doc *yaml.Node) []PodSpec {
	path, ok := podSpecPaths[Scalar(doc, "kind")]
	if !ok {
		return nil
	}
	spec := follow(doc, path)
	if spec == nil || spec.Kind != yaml.MappingNode {
		return nil
	}
	ps := PodSpec{Spec: spec}
	for _, key := range []string{"containers", "initContainers", "ephemeralContainers"} {
		_, list, ok := Entry(spec, key)
		if !ok || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, c := range list.Content {
			if c = resolveAlias(c); c.Kind == yaml.MappingNode {
				ps.Containers = append(ps.Containers, c)
			}
		}
	}
	return []PodSpec{ps}
}

// SeccompType is the profile type in force for container, in Trivy's order:
// the container's securityContext, then the pod's, then the container
// annotation. Empty when none is set. doc is the workload the pod spec belongs
// to: the annotation sits on the metadata next to the pod spec, which the spec
// node alone does not reach.
func SeccompType(doc *yaml.Node, spec PodSpec, container *yaml.Node) string {
	if t := seccompTypeOf(container); t != "" {
		return t
	}
	if t := seccompTypeOf(spec.Spec); t != "" {
		return t
	}
	path, ok := podSpecPaths[Scalar(doc, "kind")]
	if !ok {
		return ""
	}
	// The pod's metadata is the sibling of the pod spec: "spec" becomes "metadata".
	meta := append(append([]string(nil), path[:len(path)-1]...), "metadata")
	podMeta := follow(doc, meta)
	if podMeta == nil {
		return ""
	}
	_, annotations, ok := Entry(podMeta, "annotations")
	if !ok {
		return ""
	}
	return Scalar(annotations, seccompAnnotationPrefix+Scalar(container, "name"))
}

// seccompTypeOf reads securityContext.seccompProfile.type of a container or a
// pod spec.
func seccompTypeOf(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	_, sc, ok := Entry(n, "securityContext")
	if !ok {
		return ""
	}
	_, profile, ok := Entry(sc, "seccompProfile")
	if !ok {
		return ""
	}
	return strings.TrimSpace(Scalar(profile, "type"))
}

// follow walks keys down from n, resolving aliases, and returns nil when the
// path is not there.
func follow(n *yaml.Node, keys []string) *yaml.Node {
	for _, key := range keys {
		if n == nil {
			return nil
		}
		_, v, ok := Entry(n, key)
		if !ok {
			return nil
		}
		n = v
	}
	return n
}
