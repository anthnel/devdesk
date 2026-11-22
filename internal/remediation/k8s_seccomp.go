package remediation

import (
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/k8s"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// The seccomp fix (DEVDESK-688, S-03): KSV-0030 and KSV-0104 are satisfied by
// securityContext.seccompProfile.type: RuntimeDefault on the pod spec, written
// as lines inserted under the key of the pod spec (or of its securityContext),
// so no byte of the file is rewritten. It runs only on a pod with no seccomp
// setting that conflicts: an explicit Unconfined or Localhost, an annotation, a
// Windows pod and anything it cannot locate without ambiguity are declined.

// The reasons the seccomp fix declines, besides the shared ones of k8s.go.
const (
	ReasonSeccompUnconfined    = "A container or the pod sets seccompProfile Unconfined: set it to RuntimeDefault by hand if the workload allows it"
	ReasonSeccompLocalhost     = "A container or the pod sets a Localhost seccomp profile; the fix does not replace it"
	ReasonSeccompNoType        = "A seccompProfile is written without a type; set its type by hand"
	ReasonSeccompAnnotation    = "A seccomp annotation is set on this workload; the fix does not mix it with seccompProfile"
	ReasonWindowsPod           = "This is a Windows pod (spec.os.name: windows); the fix does not set seccomp on it"
	ReasonSeccompAlreadySet    = "The pod already sets seccompProfile.type: RuntimeDefault"
	ReasonEmptySecurityContext = "The pod's securityContext is empty (null); write it as a block to use the fix"
)

// seccompWarning is shown with the diff: RuntimeDefault filters system calls a
// workload may have been making unfiltered.
const seccompWarning = "A workload that ran without a seccomp filter may fail at run time under RuntimeDefault (a blocked system call); the re-scan cannot detect that"

// seccompInMessage is how KSV-0104 names its container and workload.
var seccompInMessage = regexp.MustCompile(`container "([^"]+)" of (\w+) "([^"]+)"`)

// seccompTarget refuses a stale finding: KSV-0104 by kind, name and container
// inside the lines; KSV-0030 (no name in its message) by a container starting
// inside the lines.
func seccompTarget(doc *yaml.Node, ps k8s.PodSpec, f scan.Finding) string {
	inLines := func(c *yaml.Node) bool {
		return c.Line >= f.Line && (f.EndLine <= 0 || c.Line <= f.EndLine)
	}
	if RuleKey(f.ID) != ruleSeccomp {
		for _, c := range ps.Containers {
			if inLines(c) {
				return ""
			}
		}
		return ReasonContainerNotFound
	}
	m := seccompInMessage.FindStringSubmatch(f.Message)
	if m == nil || !strings.EqualFold(k8s.Scalar(doc, "kind"), m[2]) {
		return ReasonWorkloadChanged
	}
	if _, meta, ok := k8s.Entry(doc, "metadata"); !ok || k8s.Scalar(meta, "name") != m[3] {
		return ReasonWorkloadChanged
	}
	for _, c := range ps.Containers {
		if k8s.Scalar(c, "name") == m[1] && inLines(c) {
			return ""
		}
	}
	return ReasonContainerNotFound
}

// seccompRefusal: Windows, annotation, merge or duplicate keys, then the type
// of the pod and of every container (init and ephemeral included).
func seccompRefusal(doc *yaml.Node, ps k8s.PodSpec) string {
	if _, os, ok := k8s.Entry(ps.Spec, "os"); ok && strings.EqualFold(k8s.Scalar(os, "name"), "windows") {
		return ReasonWindowsPod
	}
	if k8s.HasSeccompAnnotation(doc) {
		return ReasonSeccompAnnotation
	}
	if seccompKeysAmbiguous(ps) {
		return ReasonAmbiguousField
	}
	return seccompTypesRefusal(ps)
}

// seccompKeysAmbiguous reports a merge key, an alias used as a key, or a
// securityContext or seccompProfile written twice, in the pod spec, the pod's
// securityContext, and each container and its securityContext: a seccompProfile
// brought by a merge would be invisible, and which duplicate is in force is not
// for the fix to decide.
func seccompKeysAmbiguous(ps k8s.PodSpec) bool {
	if ambiguousKeys(ps.Spec, "securityContext") {
		return true
	}
	for _, n := range append([]*yaml.Node{ps.Spec}, ps.Containers...) {
		if n != ps.Spec && ambiguousKeys(n, "securityContext") {
			return true
		}
		if _, sc, ok := k8s.Entry(n, "securityContext"); ok && ambiguousKeys(sc, "seccompProfile") {
			return true
		}
	}
	return false
}

// ambiguousKeys reports whether a mapping has a merge key, an alias used as a
// key, or one of names written twice. Anything but a mapping has none.
func ambiguousKeys(m *yaml.Node, names ...string) bool {
	if m.Kind != yaml.MappingNode {
		return false
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.AliasNode || k.Tag == "!!merge" {
			return true
		}
		if !slices.Contains(names, k.Value) {
			continue
		}
		if seen[k.Value] {
			return true
		}
		seen[k.Value] = true
	}
	return false
}

// seccompTypesRefusal reads the profile the pod and every container set: an
// explicit Unconfined or Localhost anywhere, then a seccompProfile with no
// type, then a pod that already has RuntimeDefault. A container that has
// RuntimeDefault while another has nothing is not a refusal: the pod-level
// setting changes nothing for it.
func seccompTypesRefusal(ps k8s.PodSpec) string {
	nodes := append([]*yaml.Node{ps.Spec}, ps.Containers...)
	for _, want := range []struct{ typ, reason string }{
		{"Unconfined", ReasonSeccompUnconfined}, {"Localhost", ReasonSeccompLocalhost},
	} {
		for _, n := range nodes {
			if k8s.SeccompTypeOf(n) == want.typ {
				return want.reason
			}
		}
	}
	for _, n := range nodes {
		if _, sc, ok := k8s.Entry(n, "securityContext"); ok {
			if _, _, has := k8s.Entry(sc, "seccompProfile"); has && k8s.SeccompTypeOf(n) == "" {
				return ReasonSeccompNoType
			}
		}
	}
	switch k8s.SeccompTypeOf(ps.Spec) {
	case "":
		return ""
	case "RuntimeDefault":
		return ReasonSeccompAlreadySet
	}
	// A type this fix does not know is as good as none: adding a seccompProfile
	// beside it would write the key twice.
	return ReasonSeccompNoType
}

// fixSeccomp satisfies KSV-0030 and KSV-0104 (absent) with
// securityContext.seccompProfile.type: RuntimeDefault on the pod spec.
func fixSeccomp(content []byte, f scan.Finding) ([]patch.Edit, string) {
	doc, ps, reason := locatePodSpec(content, f)
	if reason != "" {
		return nil, reason
	}
	if reason := seccompTarget(doc, ps, f); reason != "" {
		return nil, reason
	}
	key, sc, reason := podSpecEntry(doc, ps.Spec, "securityContext")
	if reason != "" {
		return nil, reason
	}
	if reason := seccompRefusal(doc, ps); reason != "" {
		return nil, reason
	}
	switch {
	case sc == nil:
		return insertBelowKey(content, ps.Key, ps.Spec, ps.Key, ps.Spec, []yamlLine{
			{0, "securityContext:"}, {1, "seccompProfile:"}, {2, "type: RuntimeDefault"},
		})
	case sc.Kind == yaml.ScalarNode && sc.Tag == "!!null":
		return nil, ReasonEmptySecurityContext
	case sc.Kind != yaml.MappingNode:
		return nil, ReasonNoInsertionPoint
	case sc.Style&yaml.FlowStyle != 0:
		return nil, ReasonInlineMapping
	}
	return insertBelowKey(content, key, sc, key, sc, []yamlLine{{0, "seccompProfile:"}, {1, "type: RuntimeDefault"}})
}

// yamlLine is one line to insert, depth levels below the first child of a key.
type yamlLine struct {
	depth int
	text  string
}

// insertBelowKey inserts lines at the start of the line after key, at the
// indentation of value's first key plus depth steps of IndentStep(stepKey,
// stepValue). Under the key and not above its first child, a comment written
// above that child stays on it, and the insertion never falls at the end of a
// file that has no final newline. The inserted lines end like the key's line.
func insertBelowKey(content []byte, key, value, stepKey, stepValue *yaml.Node, lines []yamlLine) ([]patch.Edit, string) {
	step, ok := k8s.IndentStep(stepKey, stepValue)
	if !ok || value.Kind != yaml.MappingNode || len(value.Content) == 0 {
		return nil, ReasonNoInsertionPoint
	}
	_, indent, ok := k8s.OwnLineIndent(content, value.Content[0])
	if !ok {
		return nil, ReasonNoInsertionPoint
	}
	at, ok := k8s.LineStart(content, key.Line+1)
	if !ok {
		return nil, ReasonNoInsertionPoint
	}
	eol := k8s.LineEndingAt(content, key.Line)
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(indent + strings.Repeat(" ", l.depth*step) + l.text + eol)
	}
	return []patch.Edit{{Span: patch.Span{Start: at, End: at}, New: text.String()}}, ""
}

// seccompDeclines greys KSV-0104 on an explicit Unconfined with the S-04 text:
// the finding alone says it, no need to read the file (Rule 130). Everything
// else the fix declines is read in the manifest, at the press of the key.
func seccompDeclines(f scan.Finding) string {
	if RuleKey(f.ID) != ruleSeccomp || f.SeccompOrigin != "unconfined" {
		return ""
	}
	text, _ := MissingFor(f)
	return text
}
