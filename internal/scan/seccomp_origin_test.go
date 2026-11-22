package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/k8s"
)

// DEVDESK-686 (S-01, criterion 5) — Trivy words KSV-0104 the same way for an
// explicit Unconfined and for a missing seccomp profile, and points at the
// container's block, not at the line that says Unconfined. The only way to tell
// the two apart is to read the manifest again at scan time, which is what these
// tests pin down.

const (
	pssFixtureDir = "../remediation/testdata/pss"
	ksv104Message = `container "%s" of pod "x" in "demo" namespace should specify a seccomp profile`
)

// isKSV0104 spells the rule the three ways Trivy does, independently of the
// code under test.
func isKSV0104(id string) bool {
	id = strings.ReplaceAll(strings.TrimPrefix(strings.ToUpper(id), "AVD-"), "-", "")
	return id == "KSV0104" || id == "KSV104"
}

var messageContainer = regexp.MustCompile(`container "([^"]+)"`)

// ksv104For builds the finding Trivy reports for the container named name in
// content, with the span it reports: from the "- name:" line to the last line
// of the container's block.
func ksv104For(t *testing.T, content, name string) Finding {
	t.Helper()
	lines := strings.Split(content, "\n")
	start := -1
	indent := 0
	for i, l := range lines {
		if strings.Contains(l, "- name: "+name) && strings.HasSuffix(strings.TrimSpace(l), name) {
			start, indent = i, len(l)-len(strings.TrimLeft(l, " "))
			break
		}
	}
	if start < 0 {
		t.Fatalf("no container %q in the manifest", name)
	}
	end := start
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "---" {
			break
		}
		if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= indent {
			break
		}
		if strings.TrimSpace(l) != "" {
			end = i
		}
	}
	return Finding{
		ID: "KSV-0104", Title: "Seccomp policies disabled", Source: SourceTrivyMisconfig,
		File: "app.yaml", Line: start + 1, EndLine: end + 1, IaCType: "kubernetes",
		Message: fmt.Sprintf(ksv104Message, name),
	}
}

const (
	podUnconfinedAtPod = `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  securityContext:
    seccompProfile:
      type: Unconfined
  containers:
    - name: app
      image: nginx
`
	podUnconfinedAtContainer = `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  containers:
    - name: app
      image: nginx
      securityContext:
        seccompProfile:
          type: Unconfined
`
	podWithoutSeccomp = `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  containers:
    - name: app
      image: nginx
`
	deploymentThree = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: three
spec:
  template:
    spec:
      containers:
        - name: web
          image: nginx
          securityContext:
            seccompProfile:
              type: RuntimeDefault
        - name: sidecar
          image: busybox
          securityContext:
            seccompProfile:
              type: Unconfined
        - name: bare
          image: busybox
`
)

func TestSeccompOriginIsReadFromTheManifest(t *testing.T) {
	annotated := `apiVersion: v1
kind: Pod
metadata:
  name: x
  annotations:
    container.seccomp.security.alpha.kubernetes.io/app: unconfined
spec:
  containers:
    - name: app
      image: nginx
`
	podRuntimeDefaultOverUnconfined := `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  securityContext:
    seccompProfile:
      type: Unconfined
  containers:
    - name: app
      image: nginx
      securityContext:
        seccompProfile:
          type: RuntimeDefault
`
	localhost := `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  containers:
    - name: app
      image: nginx
      securityContext:
        seccompProfile:
          type: Localhost
          localhostProfile: profiles/audit.json
`
	initContainer := `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  initContainers:
    - name: app
      image: busybox
      securityContext:
        seccompProfile:
          type: Unconfined
  containers:
    - name: main
      image: nginx
`

	tests := []struct {
		name      string
		content   string
		container string
		want      string
	}{
		{"Unconfined at the pod", podUnconfinedAtPod, "app", SeccompUnconfined},
		{"Unconfined at the container", podUnconfinedAtContainer, "app", SeccompUnconfined},
		{"no seccomp profile at all", podWithoutSeccomp, "app", SeccompAbsent},
		{"unconfined in lower case", strings.Replace(podUnconfinedAtContainer, "Unconfined", "unconfined", 1), "app", SeccompUnconfined},
		{"UNCONFINED in upper case", strings.Replace(podUnconfinedAtContainer, "Unconfined", "UNCONFINED", 1), "app", SeccompUnconfined},
		{"the container annotation alone", annotated, "app", SeccompUnconfined},
		{"an init container", initContainer, "app", SeccompUnconfined},
		{"Deployment: the Unconfined container", deploymentThree, "sidecar", SeccompUnconfined},
		{"Deployment: the container with no profile", deploymentThree, "bare", SeccompAbsent},
		// The manifest no longer says what Trivy read: a profile is now set, so
		// no cause can be named. Unknown, never a guess.
		{"Deployment: a container that has a profile now", deploymentThree, "web", ""},
		{"the pod is Unconfined but the container overrides it", podRuntimeDefaultOverUnconfined, "app", ""},
		{"Localhost", localhost, "app", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := ksv104For(t, tt.content, tt.container)
			if got := seccompOrigin([]byte(tt.content), f); got != tt.want {
				t.Errorf("seccompOrigin = %q, want %q (finding %d..%d)", got, tt.want, f.Line, f.EndLine)
			}
		})
	}
}

// Anything the manifest does not confirm leaves the origin empty, which the
// profile reading treats as baseline: nothing is hidden without proof.
func TestSeccompOriginIsEmptyWhenTheManifestCannotConfirm(t *testing.T) {
	const twoDocs = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  containers:
    - name: app
      image: nginx
---
apiVersion: v1
kind: Pod
metadata:
  name: second
spec:
  securityContext:
    seccompProfile:
      type: Unconfined
  containers:
    - name: app
      image: nginx
`
	t.Run("two documents, each read on its own", func(t *testing.T) {
		first := ksv104For(t, twoDocs, "app")
		if got := seccompOrigin([]byte(twoDocs), first); got != SeccompAbsent {
			t.Errorf("first document: %q, want %q", got, SeccompAbsent)
		}
		second := first
		second.Line, second.EndLine = 19, 20
		if got := seccompOrigin([]byte(twoDocs), second); got != SeccompUnconfined {
			t.Errorf("second document: %q, want %q", got, SeccompUnconfined)
		}
	})

	t.Run("the file changed since the scan", func(t *testing.T) {
		f := ksv104For(t, podWithoutSeccomp, "app")
		shifted := strings.Repeat("# added later\n", 30) + podWithoutSeccomp
		if got := seccompOrigin([]byte(shifted), f); got != "" {
			t.Errorf("seccompOrigin = %q, want empty: the container is no longer in the reported lines", got)
		}
	})

	t.Run("the container is not in the manifest", func(t *testing.T) {
		f := ksv104For(t, podWithoutSeccomp, "app")
		f.Message = fmt.Sprintf(ksv104Message, "ghost")
		if got := seccompOrigin([]byte(podWithoutSeccomp), f); got != "" {
			t.Errorf("seccompOrigin = %q, want empty", got)
		}
	})

	t.Run("a container outside the reported lines is not this finding's", func(t *testing.T) {
		f := ksv104For(t, deploymentThree, "sidecar")  // lines of sidecar...
		f.Message = fmt.Sprintf(ksv104Message, "bare") // ...message of bare
		if got := seccompOrigin([]byte(deploymentThree), f); got != "" {
			t.Errorf("seccompOrigin = %q, want empty", got)
		}
	})

	t.Run("a message that names no container", func(t *testing.T) {
		f := ksv104For(t, podWithoutSeccomp, "app")
		f.Message = "should specify a seccomp profile"
		if got := seccompOrigin([]byte(podWithoutSeccomp), f); got != "" {
			t.Errorf("seccompOrigin = %q, want empty", got)
		}
	})

	t.Run("content that is not YAML", func(t *testing.T) {
		f := ksv104For(t, podWithoutSeccomp, "app")
		if got := seccompOrigin([]byte("{{ not: [yaml"), f); got != "" {
			t.Errorf("seccompOrigin = %q, want empty", got)
		}
	})
}

// annotateSeccomp is what Scan calls: it reads each manifest under root and
// fills the origin of the KSV-0104 findings of a directory scan.
func TestAnnotateSeccomp(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"pod.yaml":        podUnconfinedAtPod,
		"deploy/dep.yaml": deploymentThree,
		"absent.yaml":     podWithoutSeccomp,
	})
	at := func(file, content, container, id string) Finding {
		f := ksv104For(t, content, container)
		f.File, f.ID = file, id
		return f
	}
	other := at("absent.yaml", podWithoutSeccomp, "app", "KSV-0017") // same span, another rule
	helm := at("absent.yaml", podWithoutSeccomp, "app", "KSV-0104")
	helm.IaCType = "helm"
	conformity := at("absent.yaml", podWithoutSeccomp, "app", "KSV-0104")
	conformity.Source = SourceKubeconform

	findings := []Finding{
		at("pod.yaml", podUnconfinedAtPod, "app", "KSV-0104"),
		at("deploy/dep.yaml", deploymentThree, "sidecar", "KSV-0104"),
		at("deploy/dep.yaml", deploymentThree, "bare", "KSV-0104"),
		at("absent.yaml", podWithoutSeccomp, "app", "AVD-KSV-0104"),
		at("absent.yaml", podWithoutSeccomp, "app", "KSV104"),
		other, helm, conformity,
	}
	before := append([]Finding(nil), findings...)

	annotateSeccomp(root, findings)

	want := []string{SeccompUnconfined, SeccompUnconfined, SeccompAbsent, SeccompAbsent, SeccompAbsent, "", "", ""}
	for i, f := range findings {
		if f.SeccompOrigin != want[i] {
			t.Errorf("finding %d (%s %s %q): SeccompOrigin = %q, want %q", i, f.ID, f.File, f.Source, f.SeccompOrigin, want[i])
		}
		f.SeccompOrigin = ""
		if fmt.Sprintf("%+v", f) != fmt.Sprintf("%+v", before[i]) {
			t.Errorf("finding %d changed beyond SeccompOrigin:\n got  %+v\n want %+v", i, f, before[i])
		}
	}
}

func TestAnnotateSeccompLeavesTheOriginEmptyWhenItCannotRead(t *testing.T) {
	root := writeRepo(t, map[string]string{
		// The file was edited after Trivy read it: thirty lines were added on top.
		"edited.yaml": strings.Repeat("# added later\n", 30) + podWithoutSeccomp,
	})
	outside := writeRepo(t, map[string]string{"secret.yaml": podWithoutSeccomp})
	rel, err := filepath.Rel(root, filepath.Join(outside, "secret.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	at := func(file string) Finding {
		f := ksv104For(t, podWithoutSeccomp, "app")
		f.File = file
		return f
	}
	findings := []Finding{
		at("edited.yaml"),
		at("deleted.yaml"),
		at("."), // a directory
		at(rel), // leaves the scanned root
		at(filepath.Join(outside, "secret.yaml")), // absolute, outside the root
	}
	annotateSeccomp(root, findings)

	for i, f := range findings {
		if f.SeccompOrigin != "" {
			t.Errorf("finding %d (%s): SeccompOrigin = %q, want empty", i, f.File, f.SeccompOrigin)
		}
	}
}

// ── From the recorded Trivy output ──────────────────────────────────────────

// The real report, parsed as the scan parses it, and the real manifests it was
// recorded on, written to a scanned directory. Each KSV-0104 of the output has
// to come back with the cause the manifest gives it.
func TestEveryRecordedKSV0104GetsItsOrigin(t *testing.T) {
	report, err := os.ReadFile(filepath.Join(pssFixtureDir, "trivy-0.76.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := recordedManifests(t)

	findings, err := parseTrivyOutput(report)
	if err != nil {
		t.Fatal(err)
	}
	annotateSeccomp(root, findings)

	want := map[string]string{
		"pod-all.yaml#app":                      SeccompAbsent,
		"pod-volumes.yaml#app":                  SeccompAbsent,
		"seccomp-absent.yaml#app":               SeccompAbsent,
		"seccomp-ctr-unconfined.yaml#app":       SeccompUnconfined,
		"seccomp-pod-unconfined.yaml#app":       SeccompUnconfined,
		"seccomp-three-containers.yaml#sidecar": SeccompUnconfined,
		"seccomp-three-containers.yaml#bare":    SeccompAbsent,
	}
	got := map[string]string{}
	for _, f := range findings {
		if !isKSV0104(f.ID) {
			if f.SeccompOrigin != "" {
				t.Errorf("%s (%s) got SeccompOrigin %q: only KSV-0104 is classified", f.ID, f.File, f.SeccompOrigin)
			}
			continue
		}
		m := messageContainer.FindStringSubmatch(f.Message)
		if m == nil {
			t.Fatalf("recorded message names no container: %q", f.Message)
		}
		got[f.File+"#"+m[1]] = f.SeccompOrigin
	}
	for key, w := range want {
		if g, ok := got[key]; !ok || g != w {
			t.Errorf("%s: SeccompOrigin = %q (present %v), want %q", key, g, ok, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d KSV-0104 findings in the recorded output, want %d: %v", len(got), len(want), got)
	}
}

// recordedManifests writes the manifests the fixture was recorded on into a
// fresh directory and returns it.
func recordedManifests(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(pssFixtureDir, "manifests")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	return writeRepo(t, files)
}

// The same, through Scan: the classification happens in the misconfiguration
// stage of a directory scan, so a caller of Scan gets it without asking.
func TestScanOfADirectoryClassifiesKSV0104(t *testing.T) {
	isolateHome(t)
	report, err := os.ReadFile(filepath.Join(pssFixtureDir, "trivy-0.76.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := recordedManifests(t)
	byStage(t, map[string]stageReply{"misconfig": {stdout: string(report)}})

	result, err := newScannerWithDeps(scanFor(CategoryIDMisconfig), everyTool()).
		Scan(t.Context(), root, TargetDirectory)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	counts := map[string]int{}
	for _, f := range result.Findings {
		if isKSV0104(f.ID) {
			counts[f.SeccompOrigin]++
		}
	}
	if counts[SeccompUnconfined] != 3 || counts[SeccompAbsent] != 4 || len(counts) != 2 {
		t.Errorf("KSV-0104 origins = %v, want 3 unconfined and 4 absent", counts)
	}
}

// An image has no manifest on disk to read again: the origin stays unknown,
// even when the target happens to be a directory that holds the same file.
func TestScanOfAnImageDoesNotClassifyKSV0104(t *testing.T) {
	isolateHome(t)
	report, err := os.ReadFile(filepath.Join(pssFixtureDir, "trivy-0.76.2.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := recordedManifests(t)
	byStage(t, map[string]stageReply{"misconfig": {stdout: string(report)}})

	result, err := newScannerWithDeps(scanFor(CategoryIDMisconfig), everyTool()).
		Scan(t.Context(), root, TargetImage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	seen := 0
	for _, f := range result.Findings {
		if isKSV0104(f.ID) {
			seen++
			if f.SeccompOrigin != "" {
				t.Errorf("image scan: %s %s has SeccompOrigin %q, want empty", f.ID, f.File, f.SeccompOrigin)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no KSV-0104 reached the result: the check below would be vacuous")
	}
}

// ── Duplicated container names, one decode per file, cancellation ──────────

// An init container and a container can share a name. When the reported lines
// hold both, the message cannot say which one fired: the origin stays empty
// (baseline), so nothing is hidden on a guess.
func TestSeccompOriginIsEmptyWhenTwoContainersShareTheName(t *testing.T) {
	const sameName = `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  initContainers:
    - name: app
      image: busybox
      securityContext:
        seccompProfile:
          type: Unconfined
  containers:
    - name: app
      image: nginx
`
	lines := strings.Count(sameName, "\n")
	wide := ksv104For(t, sameName, "app")
	wide.Line, wide.EndLine = 1, lines // the span covers the whole pod

	if got := seccompOrigin([]byte(sameName), wide); got != "" {
		t.Errorf("both containers in the span: seccompOrigin = %q, want empty", got)
	}
	wide.EndLine = 0 // no span at all: nothing to tell them apart either
	if got := seccompOrigin([]byte(sameName), wide); got != "" {
		t.Errorf("no span: seccompOrigin = %q, want empty", got)
	}
	// A span that holds only one of them is still unambiguous.
	narrow := ksv104For(t, sameName, "app")
	if got := seccompOrigin([]byte(sameName), narrow); got != SeccompUnconfined {
		t.Errorf("the span of the first container only: seccompOrigin = %q, want %q", got, SeccompUnconfined)
	}
}

// The manifest is decoded once per file, however many findings point into it.
func TestAnnotateSeccompDecodesEachFileOnce(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"deploy/dep.yaml": deploymentThree,
		"absent.yaml":     podWithoutSeccomp,
	})
	var decodes int
	old := indexDocuments
	indexDocuments = func(content []byte) *k8s.DocumentIndex {
		decodes++
		return old(content)
	}
	t.Cleanup(func() { indexDocuments = old })

	at := func(file, content, container string) Finding {
		f := ksv104For(t, content, container)
		f.File = file
		return f
	}
	var findings []Finding
	for range 50 {
		findings = append(findings,
			at("deploy/dep.yaml", deploymentThree, "sidecar"),
			at("deploy/dep.yaml", deploymentThree, "bare"),
			at("absent.yaml", podWithoutSeccomp, "app"),
		)
	}
	annotateSeccomp(root, findings)

	if decodes != 2 {
		t.Errorf("%d decodes for 2 files and %d findings, want 2", decodes, len(findings))
	}
	want := []string{SeccompUnconfined, SeccompAbsent, SeccompAbsent}
	for i, f := range findings {
		if f.SeccompOrigin != want[i%3] {
			t.Errorf("finding %d: SeccompOrigin = %q, want %q", i, f.SeccompOrigin, want[i%3])
		}
	}
}

// A scan that is cancelled has no use for the classification: the work stops
// and the origins stay empty.
func TestAnnotateSeccompStopsWhenTheContextIsDone(t *testing.T) {
	root := writeRepo(t, map[string]string{"absent.yaml": podWithoutSeccomp})
	findings := []Finding{ksv104For(t, podWithoutSeccomp, "app")}
	findings[0].File = "absent.yaml"

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	annotateSeccompContext(ctx, root, findings)
	if findings[0].SeccompOrigin != "" {
		t.Errorf("SeccompOrigin = %q after cancellation, want empty", findings[0].SeccompOrigin)
	}

	annotateSeccompContext(t.Context(), root, findings)
	if findings[0].SeccompOrigin != SeccompAbsent {
		t.Errorf("SeccompOrigin = %q with a live context, want %q", findings[0].SeccompOrigin, SeccompAbsent)
	}
}
