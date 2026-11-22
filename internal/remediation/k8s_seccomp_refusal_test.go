package remediation

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-688 (S-03, criteria 3 and 6, the part the file settles) — the seccomp
// fix is exact or it declines. Every refusal below is read in the manifest, at
// the press of the key; the finding alone only settles KSV-0104 Unconfined (see
// the catalog test). A refusal returns the reason, no edit, and leaves the
// bytes alone.

// seccompMessage030 is what Trivy 0.76.2 says for KSV-0030 on every container
// of every workload: it names nothing (testdata/pss/trivy-0.76.2.json).
const seccompMessage030 = "Either Pod or Container should set 'securityContext.seccompProfile.type' to 'RuntimeDefault'"

// containerBlock is the first and last line of the block of the container
// named name, the way Trivy reports them: from its "- name:" line to the last
// line indented deeper than the dash.
func containerBlock(t *testing.T, src, name string) (start, end int) {
	t.Helper()
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: "+name {
			continue
		}
		dash := strings.Index(l, "-")
		start, end = i+1, i+1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if len(lines[j])-len(strings.TrimLeft(lines[j], " ")) <= dash {
				break
			}
			end = j + 1
		}
		return start, end
	}
	t.Fatalf("no container %q in:\n%s", name, src)
	return 0, 0
}

// seccompFinding is what Trivy reports for one bare container of a workload:
// KSV-0030 with its fixed message, KSV-0104 with kind, name and container. A
// line of 0 means the container's block in src.
func seccompFinding(t *testing.T, id, src, kind, workload, container string, line, end int) scan.Finding {
	t.Helper()
	if line == 0 {
		line, end = containerBlock(t, src, container)
	}
	f := scan.Finding{
		ID: id, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: end, Message: seccompMessage030,
	}
	if id == "KSV-0104" {
		f.Message = fmt.Sprintf(`container %q of %s %q in "default" namespace should specify a seccomp profile`,
			container, strings.ToLower(kind), workload)
		f.SeccompOrigin = "absent"
	}
	return f
}

// depOpts describes a Deployment "w" by the lines that vary.
type depOpts struct {
	root     []string // lines at column 1, between metadata and spec (anchors)
	meta     []string // lines under metadata:
	tmplMeta []string // lines under spec.template.metadata:
	pod      []string // lines of the pod spec, before containers:
	ctrs     []string // containers, each built with ctr
}

func writeLines(b *strings.Builder, indent string, lines []string) {
	for _, l := range lines {
		b.WriteString(indent + l + "\n")
	}
}

// deployment writes a Deployment whose pod spec keys sit at column 7.
func deployment(o depOpts) string {
	var b strings.Builder
	b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: w\n")
	writeLines(&b, "  ", o.meta)
	writeLines(&b, "", o.root)
	b.WriteString("spec:\n  template:\n")
	if len(o.tmplMeta) > 0 {
		b.WriteString("    metadata:\n")
		writeLines(&b, "      ", o.tmplMeta)
	}
	b.WriteString("    spec:\n")
	writeLines(&b, "      ", o.pod)
	b.WriteString("      containers:\n")
	for _, c := range o.ctrs {
		b.WriteString(c)
	}
	return b.String()
}

// ctr is one container of a deployment, with extra lines in its block.
func ctr(name string, lines ...string) string {
	s := "        - name: " + name + "\n          image: img:1\n"
	for _, l := range lines {
		s += "          " + l + "\n"
	}
	return s
}

// Short names for the seccomp blocks the refusals look at.
func seccompOf(typ string, extra ...string) []string {
	return append([]string{"securityContext:", "  seccompProfile:", "    type: " + typ}, extra...)
}

type refusalCase struct {
	name      string
	src       string
	kind      string // "" is Deployment
	container string // the container the finding is about; "" is app
	line, end int    // 0: the container's block
	edit      func(*scan.Finding)
	only      string // restrict to one rule id
	want      string
}

func TestTheSeccompFixDeclinesAndWritesNothing(t *testing.T) {
	app := ctr("app")
	other := func(lines ...string) string { return ctr("other", lines...) }
	tests := []refusalCase{
		// An explicit Unconfined or Localhost is somebody's choice, or a mistake
		// to look at: the fix never replaces it (a container other than the one of
		// the finding counts too, init and ephemeral included).
		{name: "Unconfined at the pod", want: ReasonSeccompUnconfined,
			src: deployment(depOpts{pod: seccompOf("Unconfined"), ctrs: []string{app}})},
		{name: "Unconfined on another container", want: ReasonSeccompUnconfined,
			src: deployment(depOpts{ctrs: []string{app, other(seccompOf("Unconfined")...)}})},
		{name: "Unconfined on the container of the finding", want: ReasonSeccompUnconfined, only: "KSV-0030",
			src: deployment(depOpts{ctrs: []string{ctr("app", seccompOf("Unconfined")...)}})},
		{name: "Unconfined on an init container", want: ReasonSeccompUnconfined,
			src: deployment(depOpts{
				pod:  append([]string{"initContainers:", "  - name: init", "    image: busybox"}, indent("    ", seccompOf("Unconfined"))...),
				ctrs: []string{app}})},
		{name: "Unconfined on an ephemeral container", want: ReasonSeccompUnconfined,
			src: deployment(depOpts{
				pod:  append([]string{"ephemeralContainers:", "  - name: dbg", "    image: busybox"}, indent("    ", seccompOf("Unconfined"))...),
				ctrs: []string{app}})},
		{name: "Localhost at the pod", want: ReasonSeccompLocalhost,
			src: deployment(depOpts{pod: seccompOf("Localhost", "  localhostProfile: p.json"), ctrs: []string{app}})},
		{name: "Localhost on a container", want: ReasonSeccompLocalhost,
			src: deployment(depOpts{ctrs: []string{app, other(seccompOf("Localhost", "  localhostProfile: p.json")...)}})},
		{name: "Localhost on an ephemeral container", want: ReasonSeccompLocalhost,
			src: deployment(depOpts{
				pod:  append([]string{"ephemeralContainers:", "  - name: dbg", "    image: busybox"}, indent("    ", seccompOf("Localhost", "  localhostProfile: p.json"))...),
				ctrs: []string{app}})},

		// A seccompProfile with no type: adding a type next to it would write the
		// key twice.
		{name: "a seccompProfile {} at the pod", want: ReasonSeccompNoType,
			src: deployment(depOpts{pod: []string{"securityContext:", "  seccompProfile: {}"}, ctrs: []string{app}})},
		{name: "a seccompProfile {} on a container", want: ReasonSeccompNoType,
			src: deployment(depOpts{ctrs: []string{app, other("securityContext:", "  seccompProfile: {}")}})},
		{name: "a null seccompProfile at the pod", want: ReasonSeccompNoType,
			src: deployment(depOpts{pod: []string{"securityContext:", "  seccompProfile:"}, ctrs: []string{app}})},

		// An annotation, wherever it is: the pre-1.19 way is not mixed with the field.
		{name: "an annotation of a container on the pod template", want: ReasonSeccompAnnotation,
			src: deployment(depOpts{
				tmplMeta: []string{"annotations:", "  container.seccomp.security.alpha.kubernetes.io/app: runtime/default"},
				ctrs:     []string{app}})},
		{name: "the annotation of the pod on the pod template", want: ReasonSeccompAnnotation,
			src: deployment(depOpts{
				tmplMeta: []string{"annotations:", "  seccomp.security.alpha.kubernetes.io/pod: runtime/default"},
				ctrs:     []string{app}})},
		{name: "the annotation on the Deployment itself", want: ReasonSeccompAnnotation,
			src: deployment(depOpts{
				meta: []string{"annotations:", "  seccomp.security.alpha.kubernetes.io/pod: runtime/default"},
				ctrs: []string{app}})},
		{name: "the annotation on a Pod", kind: "Pod", want: ReasonSeccompAnnotation,
			src: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\n  annotations:\n    container.seccomp.security.alpha.kubernetes.io/app: runtime/default\nspec:\n  containers:\n    - name: app\n      image: img:1\n"},

		// A Windows pod has no seccomp.
		{name: "spec.os.name windows", want: ReasonWindowsPod,
			src: deployment(depOpts{pod: []string{"os:", "  name: windows"}, ctrs: []string{app}})},
		{name: "spec.os.name Windows", want: ReasonWindowsPod,
			src: deployment(depOpts{pod: []string{"os:", "  name: Windows"}, ctrs: []string{app}})},

		// A securityContext in flow style is not edited, like the other fixes.
		{name: "an inline securityContext", want: ReasonInlineMapping,
			src: deployment(depOpts{pod: []string{"securityContext: {runAsUser: 1}"}, ctrs: []string{app}})},
		{name: "an empty inline securityContext", want: ReasonInlineMapping,
			src: deployment(depOpts{pod: []string{"securityContext: {}"}, ctrs: []string{app}})},
		{name: "a pod spec written inline", kind: "Pod", want: ReasonNoInsertionPoint, line: 5, end: 5,
			src: "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec: {containers: [{name: app, image: img}]}\n"},

		// A null securityContext: an edit would replace a token, not add a key.
		{name: "an empty securityContext", want: ReasonEmptySecurityContext,
			src: deployment(depOpts{pod: []string{"securityContext:", "hostNetwork: false"}, ctrs: []string{app}})},
		{name: "a securityContext of ~", want: ReasonEmptySecurityContext,
			src: deployment(depOpts{pod: []string{"securityContext: ~"}, ctrs: []string{app}})},
		{name: "a securityContext of null", want: ReasonEmptySecurityContext,
			src: deployment(depOpts{pod: []string{"securityContext: null"}, ctrs: []string{app}})},

		// What other places share: editing it would change them too.
		{name: "a pod spec shared through an alias", want: ReasonSharedByAlias,
			src: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: w\nx-podspec: &podspec\n  containers:\n    - name: app\n      image: img:1\nspec:\n  template:\n    spec: *podspec\n"},
		{name: "a securityContext that is an alias", want: ReasonSharedByAlias,
			src: deployment(depOpts{root: []string{"x-sc: &sc", "  runAsUser: 1"}, pod: []string{"securityContext: *sc"}, ctrs: []string{app}})},
		{name: "a securityContext another place points at", want: ReasonSharedByAlias,
			src: deployment(depOpts{pod: []string{"securityContext: &sc", "  runAsUser: 1"}, ctrs: []string{app, other("securityContext: *sc")}})},

		// Which of two keys is in force is not for the fix to decide.
		{name: "a securityContext written twice", want: ReasonAmbiguousField,
			src: deployment(depOpts{pod: []string{"securityContext:", "  runAsUser: 1", "securityContext:", "  fsGroup: 2"}, ctrs: []string{app}})},
		{name: "a seccompProfile written twice", want: ReasonAmbiguousField,
			src: deployment(depOpts{pod: []string{"securityContext:", "  seccompProfile:", "    type: RuntimeDefault", "  seccompProfile:", "    type: RuntimeDefault"}, ctrs: []string{app}})},
		{name: "a merge key in the pod spec", want: ReasonAmbiguousField,
			src: deployment(depOpts{root: []string{"x-base: &base", "  dnsPolicy: ClusterFirst"}, pod: []string{"<<: *base"}, ctrs: []string{app}})},
		{name: "a merge key beside a securityContext", want: ReasonAmbiguousField,
			src: deployment(depOpts{root: []string{"x-base: &base", "  dnsPolicy: ClusterFirst"}, pod: []string{"<<: *base", "securityContext:", "  runAsUser: 1"}, ctrs: []string{app}})},
		{name: "a merge key in a container", want: ReasonAmbiguousField,
			src: deployment(depOpts{root: []string{"x-base: &base", "  workingDir: /app"}, ctrs: []string{app, other("<<: *base")}})},
		{name: "a merge key in the securityContext of a container", want: ReasonAmbiguousField,
			src: deployment(depOpts{root: []string{"x-base: &base", "  runAsUser: 1"}, ctrs: []string{app, other("securityContext:", "  <<: *base")}})},

		// Nothing left to fix.
		{name: "a pod that already sets RuntimeDefault", want: ReasonSeccompAlreadySet,
			src: deployment(depOpts{pod: seccompOf("RuntimeDefault"), ctrs: []string{app}})},

		// A stale finding: the file changed since the scan.
		{name: "KSV-0104 naming another workload", want: ReasonWorkloadChanged, only: "KSV-0104",
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Message = strings.Replace(f.Message, `"w"`, `"other"`, 1) }},
		{name: "KSV-0104 naming another kind", want: ReasonWorkloadChanged, only: "KSV-0104",
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Message = strings.Replace(f.Message, "deployment", "statefulset", 1) }},
		{name: "KSV-0104 naming a container the lines do not hold", want: ReasonContainerNotFound, only: "KSV-0104",
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Message = strings.Replace(f.Message, `"app"`, `"ghost"`, 1) }},
		{name: "KSV-0104 whose lines are another container's", want: ReasonContainerNotFound, only: "KSV-0104",
			src:  deployment(depOpts{ctrs: []string{app, other()}}),
			edit: func(f *scan.Finding) { f.Line, f.EndLine = 11, 12 }},
		{name: "KSV-0030 whose lines hold no container", want: ReasonContainerNotFound, only: "KSV-0030",
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Line, f.EndLine = 1, 4 }},
		{name: "a line past the end of the file", want: ReasonUnreadableSpan,
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Line, f.EndLine = 99, 100 }},
		{name: "no line at all", want: ReasonUnreadableSpan,
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.Line, f.EndLine = 0, 0 }},
		{name: "a Helm template", want: ReasonNotAManifest,
			src:  deployment(depOpts{ctrs: []string{app}}),
			edit: func(f *scan.Finding) { f.IaCType = "helm" }},
	}
	for _, tt := range tests {
		for _, id := range []string{"KSV-0030", "KSV-0104"} {
			if tt.only != "" && tt.only != id {
				continue
			}
			t.Run(tt.name+" "+id, func(t *testing.T) {
				kind, container := tt.kind, tt.container
				if kind == "" {
					kind = "Deployment"
				}
				if container == "" {
					container = "app"
				}
				f := seccompFinding(t, id, tt.src, kind, "w", container, tt.line, tt.end)
				if tt.edit != nil {
					tt.edit(&f)
				}
				content := []byte(tt.src)

				edits, reason := fixSeccomp(content, f)
				if reason != tt.want {
					t.Errorf("reason = %q, want %q", reason, tt.want)
				}
				if len(edits) != 0 {
					t.Errorf("a declined fix returned %d edits", len(edits))
				}
				if !bytes.Equal(content, []byte(tt.src)) {
					t.Error("the declined fix changed the bytes it was given")
				}
				assertLayers(t, content, f, tt.want)
			})
		}
	}
}

// assertLayers checks the two pieces the fix is made of against the same
// manifest: the stale check sees a stale finding, the refusal sees what the pod
// and its containers set.
func assertLayers(t *testing.T, content []byte, f scan.Finding, want string) {
	t.Helper()
	doc, ps, reason := locatePodSpec(content, f)
	if reason != "" {
		return
	}
	switch want {
	case ReasonWorkloadChanged, ReasonContainerNotFound:
		if got := seccompTarget(doc, ps, f); got != want {
			t.Errorf("seccompTarget = %q, want %q", got, want)
		}
	case ReasonSeccompUnconfined, ReasonSeccompLocalhost, ReasonSeccompNoType,
		ReasonSeccompAnnotation, ReasonWindowsPod, ReasonSeccompAlreadySet:
		if got := seccompTarget(doc, ps, f); got != "" {
			t.Errorf("seccompTarget = %q on a finding that is not stale", got)
		}
		if got := seccompRefusal(doc, ps); got != want {
			t.Errorf("seccompRefusal = %q, want %q", got, want)
		}
	}
}

// indent prefixes every line.
func indent(prefix string, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}
	return out
}

// A pod where some containers already set RuntimeDefault and one sets nothing
// is offered the fix (decision of 2026-11-22): a RuntimeDefault at the pod
// changes nothing for the containers that already have one, so the risk is the
// base case's, no more.
func TestAMixedPodIsOffered(t *testing.T) {
	src := deployment(depOpts{ctrs: []string{
		ctr("a", seccompOf("RuntimeDefault")...),
		ctr("b"),
	}})
	for _, id := range []string{"KSV-0030", "KSV-0104"} {
		t.Run(id, func(t *testing.T) {
			edits, reason := fixSeccomp([]byte(src), seccompFinding(t, id, src, "Deployment", "w", "b", 0, 0))
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			if len(edits) != 1 {
				t.Errorf("got %d edits, want 1", len(edits))
			}
		})
	}
}

// The seven reasons are shown as-is in the footer of an 80-column terminal:
// English, and each starts with the case, not with a verb of the fix.
func TestTheSeccompReasonsAreEnglishSentencesWithoutAFinalStop(t *testing.T) {
	reasons := map[string]string{
		"ReasonSeccompUnconfined": ReasonSeccompUnconfined, "ReasonSeccompLocalhost": ReasonSeccompLocalhost,
		"ReasonSeccompNoType": ReasonSeccompNoType, "ReasonSeccompAnnotation": ReasonSeccompAnnotation,
		"ReasonWindowsPod": ReasonWindowsPod, "ReasonSeccompAlreadySet": ReasonSeccompAlreadySet,
		"ReasonEmptySecurityContext": ReasonEmptySecurityContext,
	}
	seen := map[string]string{}
	for name, r := range reasons {
		if r == "" {
			t.Errorf("%s is empty", name)
		}
		if strings.HasSuffix(r, ".") {
			t.Errorf("%s ends with a full stop: %q", name, r)
		}
		if other, dup := seen[r]; dup {
			t.Errorf("%s and %s have the same text", name, other)
		}
		seen[r] = name
	}
	for _, want := range []struct{ reason, word string }{
		{ReasonSeccompUnconfined, "Unconfined"}, {ReasonSeccompLocalhost, "Localhost"},
		{ReasonSeccompNoType, "type"}, {ReasonSeccompAnnotation, "annotation"},
		{ReasonWindowsPod, "Windows"}, {ReasonSeccompAlreadySet, "RuntimeDefault"},
		{ReasonEmptySecurityContext, "securityContext"},
	} {
		if !strings.Contains(want.reason, want.word) {
			t.Errorf("%q does not say %q", want.reason, want.word)
		}
	}
}
