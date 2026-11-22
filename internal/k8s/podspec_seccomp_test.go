package k8s

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-688 (S-03, step 1) — what the seccomp fix reads from a pod spec: the
// key node of the pod spec (for its column), the profile type a node sets on its
// own, and whether a seccomp annotation is in the way.

// kindsWithPath are the seven workload kinds the fix handles, with the keys to
// follow from the document root to the pod spec.
var kindsWithPath = []struct {
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

func TestPodSpecsKeepsTheKeyNodeOfThePodSpec(t *testing.T) {
	for _, k := range kindsWithPath {
		t.Run(k.kind, func(t *testing.T) {
			lines := []string{"apiVersion: v1", "kind: " + k.kind, "metadata:", "  name: w"}
			keyLine := 0
			for i, key := range k.path {
				lines = append(lines, strings.Repeat("  ", i)+key+":")
				keyLine = len(lines)
			}
			in := strings.Repeat("  ", len(k.path))
			lines = append(lines, in+"containers:", in+"  - name: app", in+"    image: app:1.0")
			src := strings.Join(lines, "\n") + "\n"

			specs := PodSpecs(docAt(t, src, keyLine))
			if len(specs) != 1 {
				t.Fatalf("PodSpecs = %d pod specs, want 1", len(specs))
			}
			key := specs[0].Key
			if key == nil {
				t.Fatal("PodSpec.Key is nil")
			}
			if key.Value != "spec" {
				t.Errorf("Key.Value = %q, want spec", key.Value)
			}
			if key.Line != keyLine {
				t.Errorf("Key.Line = %d, want %d", key.Line, keyLine)
			}
			if want := 2*(len(k.path)-1) + 1; key.Column != want {
				t.Errorf("Key.Column = %d, want %d", key.Column, want)
			}
		})
	}
}

func TestSeccompTypeOf(t *testing.T) {
	tests := []struct {
		name string
		node string // a container or a pod spec
		want string
	}{
		{"a container that is Unconfined",
			"name: app\nsecurityContext:\n  seccompProfile:\n    type: Unconfined\n", "Unconfined"},
		{"a container that is RuntimeDefault",
			"name: app\nsecurityContext:\n  seccompProfile:\n    type: RuntimeDefault\n", "RuntimeDefault"},
		{"a pod spec that is Localhost",
			"securityContext:\n  seccompProfile:\n    type: Localhost\n    localhostProfile: p.json\ncontainers: []\n", "Localhost"},
		{"a pod spec without a securityContext", "containers:\n  - name: app\n", ""},
		{"a securityContext without a seccompProfile", "name: app\nsecurityContext:\n  runAsUser: 1000\n", ""},
		{"a seccompProfile with no type", "name: app\nsecurityContext:\n  seccompProfile: {}\n", ""},
		{"a seccompProfile of another key", "name: app\nsecurityContext:\n  seccompProfile:\n    localhostProfile: p.json\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tt.node), &doc); err != nil {
				t.Fatal(err)
			}
			if got := SeccompTypeOf(doc.Content[0]); got != tt.want {
				t.Errorf("SeccompTypeOf = %q, want %q", got, tt.want)
			}
		})
	}
	if got := SeccompTypeOf(nil); got != "" {
		t.Errorf("SeccompTypeOf(nil) = %q, want empty", got)
	}
}

func TestHasSeccompAnnotation(t *testing.T) {
	const podTail = "spec:\n  containers:\n    - name: app\n      image: app:1.0\n"
	const deployTail = "spec:\n  template:\n    spec:\n      containers:\n        - name: app\n          image: app:1.0\n"
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"a container annotation on a Pod",
			"kind: Pod\nmetadata:\n  name: w\n  annotations:\n    container.seccomp.security.alpha.kubernetes.io/app: runtime/default\n" + podTail, true},
		{"the pod annotation on a Pod",
			"kind: Pod\nmetadata:\n  name: w\n  annotations:\n    seccomp.security.alpha.kubernetes.io/pod: runtime/default\n" + podTail, true},
		{"the annotation on the pod template of a Deployment",
			"kind: Deployment\nmetadata:\n  name: w\nspec:\n  template:\n    metadata:\n      annotations:\n        seccomp.security.alpha.kubernetes.io/pod: runtime/default\n    spec:\n      containers:\n        - name: app\n          image: app:1.0\n", true},
		{"the annotation on the Deployment alone",
			"kind: Deployment\nmetadata:\n  name: w\n  annotations:\n    seccomp.security.alpha.kubernetes.io/pod: runtime/default\n" + deployTail, true},
		{"an AppArmor annotation",
			"kind: Pod\nmetadata:\n  name: w\n  annotations:\n    container.apparmor.security.beta.kubernetes.io/app: runtime/default\n" + podTail, false},
		{"an unrelated annotation",
			"kind: Pod\nmetadata:\n  name: w\n  annotations:\n    team: platform\n" + podTail, false},
		{"no annotations", "kind: Pod\nmetadata:\n  name: w\n" + podTail, false},
		{"no metadata", "kind: Pod\n" + podTail, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSeccompAnnotation(docAt(t, tt.src, 1)); got != tt.want {
				t.Errorf("HasSeccompAnnotation = %v, want %v", got, tt.want)
			}
		})
	}
}
