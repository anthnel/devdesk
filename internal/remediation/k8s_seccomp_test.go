package remediation

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/k8s"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-688 (S-03, criteria 1 and 2) — a pod with no seccompProfile anywhere
// gets securityContext.seccompProfile.type: RuntimeDefault at the pod spec, as
// lines inserted under the key of the pod spec (or of its securityContext):
// no byte of the file is rewritten, the indentation is the file's own, and the
// diff the user confirms has only added lines.

// seccompApply is what the view does: compute the edits, rewrite.
func seccompApply(t *testing.T, src string, f scan.Finding) (string, string) {
	t.Helper()
	edits, reason := fixSeccomp([]byte(src), f)
	if reason != "" {
		return "", reason
	}
	out, err := patch.Rewrite([]byte(src), edits)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	return string(out), ""
}

// spliceAfterLine is src with add inserted as new lines after line (1-based).
func spliceAfterLine(t *testing.T, src string, line int, add string) string {
	t.Helper()
	at, ok := k8s.LineStart([]byte(src), line+1)
	if !ok {
		t.Fatalf("no line %d in:\n%s", line+1, src)
	}
	return src[:at] + add + src[at:]
}

// seccompIDs are the two rules the one fix closes.
var seccompIDs = []string{"KSV-0030", "KSV-0104"}

// ── Criterion 1: every kind, the pod spec of the right path ──────────────────

// seccompKindManifest writes a workload of kind whose pod spec is bare, among
// keys named like what the fix adds that are not the pod spec's: a label, each
// level above the pod spec, a container's own securityContext and an env var.
// It returns the line of the pod spec's key.
func seccompKindManifest(kind string, path []string) (src string, keyLine int) {
	lines := []string{
		"apiVersion: v1", "kind: " + kind, "metadata:", "  name: w", "  labels:",
		"    securityContext: decoy # label",
	}
	for i, key := range path {
		indent := strings.Repeat("  ", i)
		lines = append(lines, indent+key+":")
		if i == len(path)-1 {
			keyLine = len(lines)
			break
		}
		lines = append(lines, indent+"  securityContext: {runAsUser: 1} # decoy above the pod spec")
	}
	in := strings.Repeat("  ", len(path))
	lines = append(lines,
		in+"containers:",
		in+"  - name: app",
		in+"    image: app:1.0",
		in+"    securityContext:",
		in+"      runAsUser: 1000 # decoy container",
		in+"    env:",
		in+"      - name: securityContext",
		in+"        value: x",
	)
	return strings.Join(lines, "\n") + "\n", keyLine
}

func TestTheSeccompBlockIsAddedToThePodSpecOfEveryKind(t *testing.T) {
	kinds := []struct {
		kind string
		path []string
	}{
		{"Pod", []string{"spec"}},
		{"Deployment", []string{"spec", "template", "spec"}},
		{"StatefulSet", []string{"spec", "template", "spec"}},
		{"DaemonSet", []string{"spec", "template", "spec"}},
		{"ReplicaSet", []string{"spec", "template", "spec"}},
		{"Job", []string{"spec", "template", "spec"}},
		{"CronJob", []string{"spec", "jobTemplate", "spec", "template", "spec"}},
	}
	for _, k := range kinds {
		for _, id := range seccompIDs {
			t.Run(k.kind+" "+id, func(t *testing.T) {
				src, keyLine := seccompKindManifest(k.kind, k.path)
				in := strings.Repeat("  ", len(k.path))
				want := spliceAfterLine(t, src, keyLine,
					in+"securityContext:\n"+in+"  seccompProfile:\n"+in+"    type: RuntimeDefault\n")

				out, reason := seccompApply(t, src, seccompFinding(t, id, src, k.kind, "w", "app", 0, 0))
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				if out != want {
					t.Errorf("a decoy was touched, or the block is not under the pod spec's key:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

// The finding may be about any container: the fix is the pod's. An init
// container is signalled too (Trivy 0.76.2), and gives the same edit.
func TestTheSameEditComesFromEveryBareContainerAndBothRules(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: w
spec:
  initContainers:
    - name: init
      image: busybox
  containers:
    - name: app
      image: app:1.0
    - name: sidecar
      image: side:1.0
`
	want := spliceAfterLine(t, src, 5, "  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n")
	for _, container := range []string{"init", "app", "sidecar"} {
		for _, id := range seccompIDs {
			t.Run(container+" "+id, func(t *testing.T) {
				out, reason := seccompApply(t, src, seccompFinding(t, id, src, "Pod", "w", container, 0, 0))
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				if out != want {
					t.Errorf("got:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

// ── The file's own indentation ───────────────────────────────────────────────

func TestTheInsertedLinesFollowTheIndentationOfTheFile(t *testing.T) {
	tests := []struct {
		name string
		kind string
		src  string
		line int    // the line the block goes after
		add  string // what is added
	}{
		{"four spaces (a CronJob)", "CronJob",
			"apiVersion: batch/v1\nkind: CronJob\nmetadata:\n    name: w\nspec:\n    schedule: \"0 * * * *\"\n    jobTemplate:\n        spec:\n            template:\n                spec:\n                    containers:\n                        - name: app\n                          image: app:1.0\n                    restartPolicy: Never\n",
			10,
			"                    securityContext:\n                        seccompProfile:\n                            type: RuntimeDefault\n"},
		{"three spaces", "Pod",
			"apiVersion: v1\nkind: Pod\nmetadata:\n   name: w\nspec:\n   containers:\n      - name: app\n        image: app:1.0\n",
			5,
			"   securityContext:\n      seccompProfile:\n         type: RuntimeDefault\n"},
		{"two spaces, keys at column 7 (a Deployment)", "Deployment",
			"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: w\nspec:\n  template:\n    spec:\n      containers:\n        - name: app\n          image: app:1.0\n",
			7,
			"      securityContext:\n        seccompProfile:\n          type: RuntimeDefault\n"},
	}
	for _, tt := range tests {
		for _, id := range seccompIDs {
			t.Run(tt.name+" "+id, func(t *testing.T) {
				out, reason := seccompApply(t, tt.src, seccompFinding(t, id, tt.src, tt.kind, "w", "app", 0, 0))
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				if want := spliceAfterLine(t, tt.src, tt.line, tt.add); out != want {
					t.Errorf("got:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

func TestACRLFFileGetsCRLFLinesAndNoBareLF(t *testing.T) {
	const lf = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  containers:\n    - name: app\n      image: app:1.0\n"
	src := strings.ReplaceAll(lf, "\n", "\r\n")
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			out, reason := seccompApply(t, src, seccompFinding(t, id, src, "Pod", "w", "app", 0, 0))
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			want := strings.ReplaceAll(spliceAfterLine(t, lf, 5, "  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n"), "\n", "\r\n")
			if out != want {
				t.Errorf("got %q\nwant %q", out, want)
			}
			if strings.Count(out, "\n") != strings.Count(out, "\r\n") {
				t.Error("a bare LF was added to a CRLF file")
			}
		})
	}
}

// ── Comments and the existing block ──────────────────────────────────────────

// The block goes under the key of the pod spec, not above its first child, so
// a comment written above that child stays on it; a comment at the end of the
// key's line stays too.
func TestTheBlockDoesNotSeparateAComment(t *testing.T) {
	const src = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: w
spec:
  template:
    spec: # the pod
      # the agent talks to the host
      containers:
        - name: app
          image: app:1.0
`
	want := spliceAfterLine(t, src, 7, "      securityContext:\n        seccompProfile:\n          type: RuntimeDefault\n")
	out, reason := seccompApply(t, src, seccompFinding(t, "KSV-0030", src, "Deployment", "w", "app", 0, 0))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if !strings.Contains(out, "      # the agent talks to the host\n      containers:\n") {
		t.Error("the comment is no longer right above containers:")
	}
	if !strings.Contains(out, "    spec: # the pod\n") {
		t.Error("the comment at the end of the spec: line was lost")
	}
}

// A securityContext the pod already has gets the key and nothing else: the
// other keys are byte for byte what they were, and the block's own step is the
// one used.
func TestAnExistingSecurityContextOnlyGainsTheKey(t *testing.T) {
	const head = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: w\nspec:\n  template:\n    spec:\n"
	const tail = "      containers:\n        - name: app\n          image: app:1.0\n"
	tests := []struct {
		name  string
		block string // the securityContext as written
		add   string
	}{
		{"two spaces",
			"      securityContext:\n        runAsUser: 1000\n        fsGroup: 2000\n",
			"        seccompProfile:\n          type: RuntimeDefault\n"},
		{"a block deeper than the pod spec's step",
			"      securityContext:\n          runAsUser: 1000\n          fsGroup: 2000\n",
			"          seccompProfile:\n              type: RuntimeDefault\n"},
		{"a comment under the key",
			"      securityContext: # the pod's\n        # run as the app user\n        runAsUser: 1000\n",
			"        seccompProfile:\n          type: RuntimeDefault\n"},
	}
	for _, tt := range tests {
		for _, id := range seccompIDs {
			t.Run(tt.name+" "+id, func(t *testing.T) {
				src := head + tt.block + tail
				out, reason := seccompApply(t, src, seccompFinding(t, id, src, "Deployment", "w", "app", 0, 0))
				if reason != "" {
					t.Fatalf("declined: %s", reason)
				}
				if want := spliceAfterLine(t, src, 8, tt.add); out != want {
					t.Errorf("got:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

// ── Bytes kept ───────────────────────────────────────────────────────────────

// A file with no final newline keeps its last byte: the block goes under the
// key, never after the end of the file.
func TestAFileWithoutAFinalNewlineKeepsItsLastByte(t *testing.T) {
	const src = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  containers: [{name: app, image: app:1.0}]"
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			f := seccompFinding(t, id, src, "Pod", "w", "app", 6, 6)
			out, reason := seccompApply(t, src, f)
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			want := spliceAfterLine(t, src, 5, "  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n")
			if out != want || !strings.HasSuffix(out, "app:1.0}]") {
				t.Errorf("got %q\nwant %q", out, want)
			}
		})
	}
}

// In a file of several documents only the one the finding is in changes, and
// the anchors of the others stay.
func TestOnlyTheDocumentOfTheFindingChanges(t *testing.T) {
	const src = `apiVersion: v1
kind: Pod
metadata:
  name: first
  labels: &labels # shared
    app: first
spec:
  containers:
    - name: a
      image: a:1
---
apiVersion: v1
kind: Pod
metadata:
  name: second
  labels: {app: second}
spec:
  containers:
    - name: b
      image: b:1
---
apiVersion: v1
kind: Pod
metadata:
  name: third
  labels: &third
    app: third
spec:
  containers:
    - name: c
      image: c:1
`
	want := spliceAfterLine(t, src, 17, "  securityContext:\n    seccompProfile:\n      type: RuntimeDefault\n")
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			f := seccompFinding(t, id, src, "Pod", "second", "b", 0, 0)
			out, reason := seccompApply(t, src, f)
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			if out != want {
				t.Errorf("got:\n%s\nwant only the second pod changed:\n%s", out, want)
			}
		})
	}
}

// The diff the user confirms has added lines and nothing else.
func TestTheSeccompDiffOnlyAddsLines(t *testing.T) {
	const bare = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  containers:\n    - name: app\n      image: app:1.0\n"
	const withContext = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: w\nspec:\n  securityContext:\n    runAsUser: 1000\n  containers:\n    - name: app\n      image: app:1.0\n"
	for _, tt := range []struct {
		name  string
		src   string
		added int
	}{{"no securityContext", bare, 3}, {"a securityContext", withContext, 2}} {
		t.Run(tt.name, func(t *testing.T) {
			edits, reason := fixSeccomp([]byte(tt.src), seccompFinding(t, "KSV-0030", tt.src, "Pod", "w", "app", 0, 0))
			if reason != "" || len(edits) != 1 {
				t.Fatalf("reason %q, %d edits; want one edit", reason, len(edits))
			}
			if edits[0].Old != "" || edits[0].Span.Start != edits[0].Span.End {
				t.Errorf("the edit is not an insertion: %+v", edits[0])
			}
			diff, err := patch.Diff("deploy.yaml", []byte(tt.src), edits)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			var added int
			for _, l := range strings.Split(diff, "\n") {
				switch {
				case strings.HasPrefix(l, "---"), strings.HasPrefix(l, "+++"):
				case strings.HasPrefix(l, "-"):
					t.Errorf("the diff removes a line: %q\n%s", l, diff)
				case strings.HasPrefix(l, "+"):
					added++
				}
			}
			if added != tt.added {
				t.Errorf("the diff adds %d lines, want %d:\n%s", added, tt.added, diff)
			}
		})
	}
}

// Running the fix on its own output finds a pod that is already done.
func TestTheSeccompFixIsNotOfferedTwice(t *testing.T) {
	src := deployment(depOpts{ctrs: []string{ctr("app"), ctr("b")}})
	for _, id := range seccompIDs {
		t.Run(id, func(t *testing.T) {
			out, reason := seccompApply(t, src, seccompFinding(t, id, src, "Deployment", "w", "app", 0, 0))
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			if got := strings.Count(out, "RuntimeDefault"); got != 1 {
				t.Fatalf("%d RuntimeDefault in the output, want 1:\n%s", got, out)
			}
			_, reason = seccompApply(t, out, seccompFinding(t, id, out, "Deployment", "w", "b", 0, 0))
			if reason != ReasonSeccompAlreadySet {
				t.Errorf("second run: reason = %q, want %q", reason, ReasonSeccompAlreadySet)
			}
		})
	}
}

// ── insertBelowKey ───────────────────────────────────────────────────────────

func TestInsertBelowKeyInsertsAtTheStartOfTheLineAfterTheKey(t *testing.T) {
	const src = "kind: Pod\nspec:\n    containers: []\n"
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	key, value, _ := k8s.Entry(doc.Content[0], "spec")

	edits, reason := insertBelowKey([]byte(src), key, value, key, value, []yamlLine{{0, "a:"}, {1, "b: c"}})
	if reason != "" || len(edits) != 1 {
		t.Fatalf("reason %q, %d edits", reason, len(edits))
	}
	out, err := patch.Rewrite([]byte(src), edits)
	if err != nil {
		t.Fatal(err)
	}
	want := "kind: Pod\nspec:\n    a:\n        b: c\n    containers: []\n"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestInsertBelowKeyRefusesAValueItCannotIndentBy(t *testing.T) {
	for _, src := range []string{"spec: {containers: []}\n", "spec: 3\n", "spec:\n"} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatal(err)
		}
		key, value, _ := k8s.Entry(doc.Content[0], "spec")
		edits, reason := insertBelowKey([]byte(src), key, value, key, value, []yamlLine{{0, "a:"}})
		if reason == "" || len(edits) != 0 {
			t.Errorf("%q: reason %q, %d edits; want a refusal", strings.TrimSpace(src), reason, len(edits))
		}
	}
}
