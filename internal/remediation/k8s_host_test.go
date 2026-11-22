package remediation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// The host namespace fixes (DEVDESK-687, S-02): hostNetwork (KSV-0009), hostPID
// (KSV-0010) and hostIPC (KSV-0008) set to false on the pod spec, as a byte
// edit of the one `true` token. Trivy reports these on the pod, not on a
// container, and names the field in its message (see
// testdata/pss/trivy-0.76.2.json: "Pod 'all' should not set
// 'spec.template.spec.hostIPC' to true", lines of the pod spec to the end of the
// resource).

var hostFields = []struct{ id, field string }{
	{"KSV-0009", "hostNetwork"},
	{"KSV-0010", "hostPID"},
	{"KSV-0008", "hostIPC"},
}

// hostFinding is what Trivy reports for field on a workload whose pod spec
// starts at line and the document ends at endLine.
func hostFinding(id, kind, field string, line, endLine int) scan.Finding {
	return scan.Finding{
		ID: id, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: endLine,
		Message: fmt.Sprintf("%s 'w' should not set 'spec.template.spec.%s' to true", kind, field),
	}
}

// lineWith is the 1-based number of the first line of src containing needle.
func lineWith(t *testing.T, src, needle string) int {
	t.Helper()
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("%q is not in:\n%s", needle, src)
	return 0
}

// diffOf is the confirmation's diff: the same edits the write would apply.
func diffOf(t *testing.T, src string, f scan.Finding) string {
	t.Helper()
	rule, ok := FixFor(f)
	if !ok {
		t.Fatalf("no rule for %q", f.ID)
	}
	edits, reason := rule.Fix([]byte(src), f)
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	diff, err := patch.Diff("deploy.yaml", []byte(src), edits)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return diff
}

// ── Criterion 1: only true -> false, every other byte kept ───────────────────

const hostManifest = `# Node agent, owned by the platform team.
apiVersion: apps/v1
kind: Deployment
metadata:
  name: w
  labels: &labels # shared labels
    app: w
spec:
  selector:
    matchLabels: *labels
  template:
    metadata:
      labels: *labels
    spec:
      # the agent talks to the host
      hostNetwork: true   # needed by the agent
      hostPID: true       # needed too
      hostIPC: true       # and this one
      containers:
        - name: api
          image: api:1.0 # pinned
`

func TestAHostFieldBecomesFalseAndNothingElseChanges(t *testing.T) {
	for _, eol := range []string{"\n", "\r\n"} {
		name := map[string]string{"\n": "LF", "\r\n": "CRLF"}[eol]
		for _, hf := range hostFields {
			t.Run(name+" "+hf.field, func(t *testing.T) {
				f := hostFinding(hf.id, "Deployment", hf.field, lineWith(t, hostManifest, "    spec:"), 22)
				src := strings.ReplaceAll(hostManifest, "\n", eol)

				out, reason := apply(t, src, f)
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				// Comments, anchors, aliases, spacing, line endings and the two
				// other host fields (one fix per finding) are all still there.
				if strings.Count(src, hf.field+": true") != 1 {
					t.Fatal("test manifest broken: the field must appear once")
				}
				want := strings.Replace(src, hf.field+": true", hf.field+": false", 1)
				if out != want {
					t.Errorf("the fix changed more than the value:\n got: %q\nwant: %q", out, want)
				}
			})
		}
	}
}

// The diff the user confirms is exactly one line out, one line in, and the two
// differ by that value alone.
func TestTheHostFieldDiffIsOneLineChangedFromTrueToFalse(t *testing.T) {
	for _, hf := range hostFields {
		t.Run(hf.field, func(t *testing.T) {
			f := hostFinding(hf.id, "Deployment", hf.field, lineWith(t, hostManifest, "    spec:"), 22)
			diff := diffOf(t, hostManifest, f)

			var removed, added []string
			for _, l := range strings.Split(diff, "\n") {
				switch {
				case strings.HasPrefix(l, "---"), strings.HasPrefix(l, "+++"):
				case strings.HasPrefix(l, "-"):
					removed = append(removed, l[1:])
				case strings.HasPrefix(l, "+"):
					added = append(added, l[1:])
				}
			}
			if len(removed) != 1 || len(added) != 1 {
				t.Fatalf("want one line out and one in, got -%v +%v\n%s", removed, added, diff)
			}
			if got := strings.Replace(removed[0], hf.field+": true", hf.field+": false", 1); got != added[0] {
				t.Errorf("the lines differ by more than the value:\n-%s\n+%s", removed[0], added[0])
			}
			if !strings.Contains(removed[0], hf.field+": true") || !strings.Contains(added[0], hf.field+": false") {
				t.Errorf("the change is not true -> false:\n-%s\n+%s", removed[0], added[0])
			}
		})
	}
}

// ── Criterion 2: the warning travels with the rule ───────────────────────────

// The warning is the rule's, not the kind's: every type reaches the diff
// through the same rule, so it shows for all of them (the UI test checks the
// confirmation carries it).
func TestEveryHostFixCarriesTheReversalWarning(t *testing.T) {
	for _, hf := range hostFields {
		t.Run(hf.field, func(t *testing.T) {
			rule, ok := FixFor(hostFinding(hf.id, "Deployment", hf.field, 1, 2))
			if !ok {
				t.Fatalf("no rule for %s", hf.id)
			}
			w := strings.ToLower(rule.Warning)
			for _, want := range []string{"reverses an explicit choice", "node agents"} {
				if !strings.Contains(w, want) {
					t.Errorf("Warning = %q, want it to say %q", rule.Warning, want)
				}
			}
		})
	}
}

// ── Criteria 3 and 4: every kind, the pod spec of the right path ─────────────

var hostKinds = []struct {
	kind string
	path []string // keys from the document root to the pod spec
}{
	{"Pod", []string{"spec"}},
	{"Deployment", []string{"spec", "template", "spec"}},
	{"StatefulSet", []string{"spec", "template", "spec"}},
	{"DaemonSet", []string{"spec", "template", "spec"}},
	{"ReplicaSet", []string{"spec", "template", "spec"}},
	{"Job", []string{"spec", "template", "spec"}},
	{"CronJob", []string{"spec", "jobTemplate", "spec", "template", "spec"}},
}

// workloadWithDecoys writes a workload of kind whose pod spec carries the real
// `field: true # real`, surrounded by the same key in places that are not the
// pod spec: a label, the mapping above each level on the way down, and a
// container. Only the marked line is the one a fix may touch. It returns the
// line the pod spec starts on, like Trivy does.
func workloadWithDecoys(kind string, path []string, field string) (src string, podSpecLine, lastLine int) {
	lines := []string{
		"apiVersion: v1", "kind: " + kind, "metadata:", "  name: w", "  labels:",
		"    " + field + ": \"true\" # decoy label",
	}
	for i, key := range path {
		indent := strings.Repeat("  ", i)
		lines = append(lines, indent+key+":")
		if i == len(path)-1 {
			podSpecLine = len(lines)
			break
		}
		lines = append(lines, indent+"  "+field+": true # decoy above the pod spec")
	}
	in := strings.Repeat("  ", len(path))
	lines = append(lines,
		in+field+": true # real",
		in+"containers:",
		in+"  - name: app",
		in+"    image: app:1.0",
		in+"    "+field+": true # decoy container",
		in+"    env:",
		in+"      - name: "+field,
		in+"        value: \"true\"",
	)
	return strings.Join(lines, "\n") + "\n", podSpecLine, len(lines)
}

func TestTheFixEditsThePodSpecOfEveryKind(t *testing.T) {
	for _, k := range hostKinds {
		for _, hf := range hostFields {
			t.Run(k.kind+" "+hf.field, func(t *testing.T) {
				src, line, last := workloadWithDecoys(k.kind, k.path, hf.field)
				out, reason := apply(t, src, hostFinding(hf.id, k.kind, hf.field, line, last))
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				want := strings.Replace(src, hf.field+": true # real", hf.field+": false # real", 1)
				if out != want {
					t.Errorf("a decoy was touched, or the real field was not:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

// A DaemonSet is the workload that most often has hostNetwork on purpose. The
// fix is offered like for the others: no refusal of its own, the warning of
// criterion 2 is the answer to that.
func TestADaemonSetHostNetworkIsOfferedLikeTheOtherKinds(t *testing.T) {
	src, line, last := workloadWithDecoys("DaemonSet", []string{"spec", "template", "spec"}, "hostNetwork")
	f := hostFinding("KSV-0009", "DaemonSet", "hostNetwork", line, last)

	rule, ok := FixFor(f)
	if !ok {
		t.Fatal("a DaemonSet finding has no rule")
	}
	edits, reason := rule.Fix([]byte(src), f)
	if reason != "" || len(edits) != 1 {
		t.Fatalf("a DaemonSet is declined (%q, %d edits)", reason, len(edits))
	}
	if !strings.Contains(strings.ToLower(rule.Warning), "node agents") {
		t.Errorf("the DaemonSet fix has no warning: %q", rule.Warning)
	}
}

// A file with several documents: the finding's line says which one.
func TestTheFixEditsTheDocumentTheFindingIsIn(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  hostNetwork: true # first
  containers: [{name: a, image: a}]
---
apiVersion: v1
kind: Pod
metadata:
  name: second
spec:
  hostNetwork: true # second
  containers: [{name: b, image: b}]
`
	out, reason := apply(t, src, namedHostFinding("Pod", "second", 13, 15))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if want := strings.Replace(src, "hostNetwork: true # second", "hostNetwork: false # second", 1); out != want {
		t.Errorf("got:\n%s\nwant only the second pod changed", out)
	}
}

// ── Criterion 6: declines, with the reason, and no edit ──────────────────────

func TestTheHostFixDeclinesWhatItCannotLocateExactly(t *testing.T) {
	const head = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: w\n"
	const tail = "      containers:\n        - name: api\n          image: api:1.0\n"
	podSpec := func(lines ...string) string {
		return head + "spec:\n  template:\n    spec:\n" + strings.Join(lines, "\n") + "\n" + tail
	}
	helm := hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 13)
	helm.IaCType = "helm"

	tests := []struct {
		name string
		src  string
		f    scan.Finding
		want string // "" means: any reason
	}{
		{"a pod spec shared through an alias",
			head + "x-podspec: &podspec\n  hostNetwork: true\n  containers:\n    - name: api\n      image: api:1.0\nspec:\n  template:\n    spec: *podspec\n",
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 5, 11), ReasonSharedByAlias},
		{"a value another field shares through an anchor",
			podSpec("      hostNetwork: &flag true", "      hostPID: *flag"),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 12), ReasonSharedByAlias},
		{"a value that is itself an alias",
			head + "x-flag: &flag true\nspec:\n  template:\n    spec:\n      hostNetwork: *flag\n" + tail,
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 13), ReasonSharedByAlias},
		{"a field merged from an anchor",
			head + "x-base: &base\n  hostNetwork: true\nspec:\n  template:\n    spec:\n      <<: *base\n" + tail,
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 14), ""},
		{"a duplicated key (both true)",
			podSpec("      hostNetwork: true", "      hostNetwork: true"),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 12), ReasonAmbiguousField},
		{"a duplicated key (true then false)",
			podSpec("      hostNetwork: true", "      hostNetwork: false"),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 12), ReasonAmbiguousField},
		{"a value that is already false",
			podSpec("      hostNetwork: false"),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonHostFieldNotTrue},
		{"a field the pod spec does not set",
			podSpec("      dnsPolicy: ClusterFirst"),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonHostFieldNotTrue},
		{"a double-quoted true", podSpec(`      hostNetwork: "true"`),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonNotPlainBoolean},
		{"a single-quoted true", podSpec(`      hostNetwork: 'true'`),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonNotPlainBoolean},
		{"a tagged true", podSpec(`      hostNetwork: !!bool true`),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonNotPlainBoolean},
		{"a yes", podSpec(`      hostNetwork: yes`),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonNotPlainBoolean},
		{"a capitalised True", podSpec(`      hostNetwork: True`),
			hostFinding("KSV-0009", "Deployment", "hostNetwork", 7, 11), ReasonNotPlainBoolean},
		{"a Helm template", podSpec("      hostNetwork: true"), helm, ReasonNotAManifest},
		{"a document that is not a workload",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w\ndata:\n  hostNetwork: true\n",
			hostFinding("KSV-0009", "ConfigMap", "hostNetwork", 1, 6), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, reason := apply(t, tt.src, tt.f)
			if reason == "" {
				t.Fatalf("the fix was offered, and would produce:\n%s", out)
			}
			if tt.want != "" && reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
		})
	}
}

// Declining one field of a pod must not be the way another one is reached: the
// finding names the field, and only that field is looked at.
func TestTheHostFixReadsTheFieldTheFindingNames(t *testing.T) {
	const src = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  hostNetwork: true\n  hostPID: false\n  containers: [{name: a, image: a}]\n"
	if _, reason := apply(t, src, hostFinding("KSV-0010", "Pod", "hostPID", 5, 8)); reason != ReasonHostFieldNotTrue {
		t.Errorf("hostPID: reason = %q, want %q (hostNetwork being true is not its business)", reason, ReasonHostFieldNotTrue)
	}
}
