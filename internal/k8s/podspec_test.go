package k8s

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// docAt returns the document holding line of content, failing the test when
// there is none.
func docAt(t *testing.T, content string, line int) *yaml.Node {
	t.Helper()
	doc, ok := DocumentAt([]byte(content), line)
	if !ok {
		t.Fatalf("no document at line %d", line)
	}
	return doc
}

// containerNames lists the name of each container of a pod spec, in order.
func containerNames(spec PodSpec) []string {
	var names []string
	for _, c := range spec.Containers {
		names = append(names, Scalar(c, "name"))
	}
	return names
}

// containerNamed finds one container of a pod spec by name.
func containerNamed(t *testing.T, spec PodSpec, name string) *yaml.Node {
	t.Helper()
	for _, c := range spec.Containers {
		if Scalar(c, "name") == name {
			return c
		}
	}
	t.Fatalf("no container %q in %v", name, containerNames(spec))
	return nil
}

// DEVDESK-686 (S-01, criterion 5) — every workload kind keeps its pod spec at a
// different depth. The seccomp type in force is a property of a pod spec and its
// container, so the lookup has to find the pod spec first, wherever the kind
// hides it.
func TestPodSpecsFindsThePodSpecOfEachWorkloadKind(t *testing.T) {
	const containers = "containers:\n  - name: app\n    image: nginx\n"
	// manifest nests the containers under the given pod spec path.
	manifest := func(header string, path ...string) string {
		out := header + "metadata:\n  name: x\nspec:\n"
		depth := 1
		for _, key := range path {
			out += strings.Repeat("  ", depth) + key + ":\n"
			depth++
		}
		for _, line := range strings.SplitAfter(containers, "\n") {
			if line != "" {
				out += strings.Repeat("  ", depth) + line
			}
		}
		return out
	}
	tests := []struct {
		name string
		doc  string
	}{
		{"Pod", manifest("apiVersion: v1\nkind: Pod\n")},
		{"Deployment", manifest("apiVersion: apps/v1\nkind: Deployment\n", "template", "spec")},
		{"StatefulSet", manifest("apiVersion: apps/v1\nkind: StatefulSet\n", "template", "spec")},
		{"DaemonSet", manifest("apiVersion: apps/v1\nkind: DaemonSet\n", "template", "spec")},
		{"ReplicaSet", manifest("apiVersion: apps/v1\nkind: ReplicaSet\n", "template", "spec")},
		{"Job", manifest("apiVersion: batch/v1\nkind: Job\n", "template", "spec")},
		{"CronJob", manifest("apiVersion: batch/v1\nkind: CronJob\n", "jobTemplate", "spec", "template", "spec")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := PodSpecs(docAt(t, tt.doc, 1))
			if len(specs) != 1 {
				t.Fatalf("got %d pod specs, want 1 in:\n%s", len(specs), tt.doc)
			}
			if specs[0].Spec == nil || specs[0].Spec.Kind != yaml.MappingNode {
				t.Fatalf("Spec = %+v, want the pod spec mapping", specs[0].Spec)
			}
			if got := containerNames(specs[0]); !slices.Equal(got, []string{"app"}) {
				t.Errorf("containers = %v, want [app]", got)
			}
		})
	}
}

// A document that is not a workload has no pod spec: no spec is invented for a
// ConfigMap or a Service, and a document without a mapping root is no panic.
func TestPodSpecsOfADocumentWithoutAPodIsEmpty(t *testing.T) {
	for name, doc := range map[string]string{
		"ConfigMap":              "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\ndata:\n  a: b\n",
		"Service":                "apiVersion: v1\nkind: Service\nmetadata:\n  name: x\nspec:\n  ports:\n    - port: 80\n",
		"Pod without containers": "apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\nspec: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			for _, ps := range PodSpecs(docAt(t, doc, 1)) {
				if len(ps.Containers) != 0 {
					t.Errorf("got containers %v, want none", containerNames(ps))
				}
			}
		})
	}
}

// Containers lists containers, initContainers and ephemeralContainers, in the
// order the manifest has them; Trivy reads only the first two but the pod spec
// reports all three so the container is found by name wherever it sits.
func TestPodSpecsListsEveryKindOfContainer(t *testing.T) {
	doc := docAt(t, `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  initContainers:
    - name: init
      image: busybox
  containers:
    - name: app
      image: nginx
    - name: sidecar
      image: nginx
  ephemeralContainers:
    - name: debug
      image: busybox
`, 1)
	specs := PodSpecs(doc)
	if len(specs) != 1 {
		t.Fatalf("got %d pod specs, want 1", len(specs))
	}
	got := containerNames(specs[0])
	slices.Sort(got)
	if want := []string{"app", "debug", "init", "sidecar"}; !slices.Equal(got, want) {
		t.Errorf("containers = %v, want %v", got, want)
	}
}

// A multi-document file: the pod spec read is the one of the document the line
// belongs to, not the first document's.
func TestPodSpecsReadsTheDocumentOfTheLine(t *testing.T) {
	const content = `apiVersion: v1
kind: Pod
metadata:
  name: first
spec:
  containers:
    - name: one
      image: nginx
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: second
spec:
  template:
    spec:
      containers:
        - name: two
          image: nginx
`
	first := PodSpecs(docAt(t, content, 7))
	second := PodSpecs(docAt(t, content, 18))
	if len(first) != 1 || !slices.Equal(containerNames(first[0]), []string{"one"}) {
		t.Errorf("first document: got %v", first)
	}
	if len(second) != 1 || !slices.Equal(containerNames(second[0]), []string{"two"}) {
		t.Errorf("second document: got %v", second)
	}
}

// A pod spec shared through a YAML alias resolves to the anchored mapping, as
// Containers already does. The anchor sits outside the workload's own paths, so
// the one pod spec of the Deployment is the only one reported.
func TestPodSpecsResolvesAnAliasedPodSpec(t *testing.T) {
	doc := docAt(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: x
x-shared: &podspec
  containers:
    - name: app
      image: nginx
      securityContext:
        seccompProfile:
          type: Unconfined
spec:
  template:
    spec: *podspec
`, 1)
	specs := PodSpecs(doc)
	if len(specs) != 1 {
		t.Fatalf("got %d pod specs, want 1 (the Deployment's, through the alias)", len(specs))
	}
	if specs[0].Spec == nil || specs[0].Spec.Kind != yaml.MappingNode {
		t.Fatalf("Spec = %+v, want the resolved mapping, not the alias node", specs[0].Spec)
	}
	if got := containerNames(specs[0]); !slices.Equal(got, []string{"app"}) {
		t.Fatalf("containers = %v, want [app]", got)
	}
	if got := SeccompType(doc, specs[0], containerNamed(t, specs[0], "app")); got != "Unconfined" {
		t.Errorf("SeccompType = %q, want Unconfined", got)
	}
}

// The seccomp type in force, in Trivy's order (seccomp_profile_unconfined.rego
// and lib/kubernetes.rego, measured in DEVDESK-686): the container's own
// securityContext, then the pod's, then the container annotation. Trivy refuses
// both "Unconfined" and an empty type, so the answer is "" when none is set.
func TestSeccompType(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		container string
		want      string
	}{
		{
			name: "pod level Unconfined",
			manifest: `apiVersion: v1
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
`,
			container: "app", want: "Unconfined",
		},
		{
			name: "container level Unconfined",
			manifest: `apiVersion: v1
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
`,
			container: "app", want: "Unconfined",
		},
		{
			name: "nothing set",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  containers:
    - name: app
      image: nginx
`,
			container: "app", want: "",
		},
		{
			name: "pod Unconfined, container RuntimeDefault: the container wins",
			manifest: `apiVersion: v1
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
`,
			container: "app", want: "RuntimeDefault",
		},
		{
			name: "container securityContext without a seccomp profile falls back to the pod",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: nginx
      securityContext:
        privileged: true
`,
			container: "app", want: "RuntimeDefault",
		},
		{
			name: "Localhost",
			manifest: `apiVersion: v1
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
`,
			container: "app", want: "Localhost",
		},
		{
			name: "an empty seccompProfile is no type",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
spec:
  containers:
    - name: app
      image: nginx
      securityContext:
        seccompProfile: {}
`,
			container: "app", want: "",
		},
		{
			name: "annotation alone",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
  annotations:
    container.seccomp.security.alpha.kubernetes.io/app: unconfined
spec:
  containers:
    - name: app
      image: nginx
`,
			container: "app", want: "unconfined",
		},
		{
			name: "annotation of another container is not this container's",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
  annotations:
    container.seccomp.security.alpha.kubernetes.io/other: unconfined
spec:
  containers:
    - name: app
      image: nginx
`,
			container: "app", want: "",
		},
		{
			name: "pod securityContext wins over the annotation",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: x
  annotations:
    container.seccomp.security.alpha.kubernetes.io/app: unconfined
spec:
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: nginx
`,
			container: "app", want: "RuntimeDefault",
		},
		{
			name: "annotation on the pod template of a Deployment",
			manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: x
spec:
  template:
    metadata:
      annotations:
        container.seccomp.security.alpha.kubernetes.io/app: unconfined
    spec:
      containers:
        - name: app
          image: nginx
`,
			container: "app", want: "unconfined",
		},
		{
			name: "Deployment, pod level Unconfined",
			manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: x
spec:
  template:
    spec:
      securityContext:
        seccompProfile:
          type: Unconfined
      containers:
        - name: app
          image: nginx
`,
			container: "app", want: "Unconfined",
		},
		{
			name: "CronJob, container level Unconfined",
			manifest: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: x
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: app
              image: nginx
              securityContext:
                seccompProfile:
                  type: Unconfined
`,
			container: "app", want: "Unconfined",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := docAt(t, tt.manifest, 1)
			specs := PodSpecs(doc)
			if len(specs) != 1 {
				t.Fatalf("got %d pod specs, want 1", len(specs))
			}
			got := SeccompType(doc, specs[0], containerNamed(t, specs[0], tt.container))
			if got != tt.want {
				t.Errorf("SeccompType = %q, want %q", got, tt.want)
			}
		})
	}
}

// Containers of one Deployment answer on their own: one container's
// Unconfined does not leak to its neighbours.
func TestSeccompTypeIsPerContainer(t *testing.T) {
	doc := docAt(t, `apiVersion: apps/v1
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
`, 1)
	specs := PodSpecs(doc)
	if len(specs) != 1 {
		t.Fatalf("got %d pod specs, want 1", len(specs))
	}
	want := map[string]string{"web": "RuntimeDefault", "sidecar": "Unconfined", "bare": ""}
	for name, w := range want {
		if got := SeccompType(doc, specs[0], containerNamed(t, specs[0], name)); got != w {
			t.Errorf("SeccompType(%s) = %q, want %q", name, got, w)
		}
	}
}
