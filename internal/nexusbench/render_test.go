package nexusbench

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// updateGolden rewrites the golden file instead of comparing with it:
//
//	go test ./internal/nexusbench -run TestRenderGolden -update
//
// Read the diff before committing it: the golden is the reviewed manifest.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

const (
	goldenDefault = "testdata/render-default.golden.yaml"

	labelManagedBy = "app.kubernetes.io/managed-by"
	labelPartOf    = "app.kubernetes.io/part-of"
	labelName      = "app.kubernetes.io/name"
	benchLabel     = "devdesk-nexus-bench"
	psaPrefix      = "pod-security.kubernetes.io/"
	statusPath     = "/service/rest/v1/status"
)

// The decoded shapes below hold only what the tests look at. yaml.v3 lower-cases
// a field name when there is no tag, so every field that is not all lower case
// carries one.

type objectMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type object struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   objectMeta `yaml:"metadata"`
}

type pvcObject struct {
	object `yaml:",inline"`
	Spec   struct {
		AccessModes      []string `yaml:"accessModes"`
		StorageClassName *string  `yaml:"storageClassName"`
		VolumeName       string   `yaml:"volumeName"`
		Resources        struct {
			Requests map[string]string `yaml:"requests"`
		} `yaml:"resources"`
	} `yaml:"spec"`
}

type securityContext struct {
	RunAsUser                *int64 `yaml:"runAsUser"`
	RunAsGroup               *int64 `yaml:"runAsGroup"`
	RunAsNonRoot             *bool  `yaml:"runAsNonRoot"`
	AllowPrivilegeEscalation *bool  `yaml:"allowPrivilegeEscalation"`
	Capabilities             *struct {
		Drop []string `yaml:"drop"`
		Add  []string `yaml:"add"`
	} `yaml:"capabilities"`
	SeccompProfile *struct {
		Type string `yaml:"type"`
	} `yaml:"seccompProfile"`
}

type probe struct {
	HTTPGet *struct {
		Path string    `yaml:"path"`
		Port yaml.Node `yaml:"port"`
	} `yaml:"httpGet"`
	PeriodSeconds    int `yaml:"periodSeconds"`
	FailureThreshold int `yaml:"failureThreshold"`
}

type container struct {
	Name    string `yaml:"name"`
	Image   string `yaml:"image"`
	EnvFrom []any  `yaml:"envFrom"`
	Env     []struct {
		Name      string    `yaml:"name"`
		Value     yaml.Node `yaml:"value"`
		ValueFrom yaml.Node `yaml:"valueFrom"`
	} `yaml:"env"`
	Ports []struct {
		Name          string `yaml:"name"`
		ContainerPort int    `yaml:"containerPort"`
	} `yaml:"ports"`
	Resources struct {
		Requests map[string]string `yaml:"requests"`
		Limits   map[string]string `yaml:"limits"`
	} `yaml:"resources"`
	SecurityContext *securityContext `yaml:"securityContext"`
	VolumeMounts    []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
	} `yaml:"volumeMounts"`
	StartupProbe   *probe `yaml:"startupProbe"`
	ReadinessProbe *probe `yaml:"readinessProbe"`
	LivenessProbe  *probe `yaml:"livenessProbe"`
}

type podSpec struct {
	AutomountServiceAccountToken *bool            `yaml:"automountServiceAccountToken"`
	EnableServiceLinks           *bool            `yaml:"enableServiceLinks"`
	SecurityContext              *securityContext `yaml:"securityContext"`
	InitContainers               []container      `yaml:"initContainers"`
	Containers                   []container      `yaml:"containers"`
	Volumes                      []struct {
		Name                  string `yaml:"name"`
		PersistentVolumeClaim *struct {
			ClaimName string `yaml:"claimName"`
		} `yaml:"persistentVolumeClaim"`
		Secret    any `yaml:"secret"`
		ConfigMap any `yaml:"configMap"`
		HostPath  any `yaml:"hostPath"`
	} `yaml:"volumes"`
}

type deploymentObject struct {
	object `yaml:",inline"`
	Spec   struct {
		Replicas *int `yaml:"replicas"`
		Strategy struct {
			Type          string    `yaml:"type"`
			RollingUpdate yaml.Node `yaml:"rollingUpdate"`
		} `yaml:"strategy"`
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata objectMeta `yaml:"metadata"`
			Spec     podSpec    `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type serviceObject struct {
	object `yaml:",inline"`
	Spec   struct {
		Type     string            `yaml:"type"`
		Selector map[string]string `yaml:"selector"`
		Ports    []struct {
			Name       string    `yaml:"name"`
			Port       int       `yaml:"port"`
			TargetPort yaml.Node `yaml:"targetPort"`
			NodePort   int       `yaml:"nodePort"`
		} `yaml:"ports"`
	} `yaml:"spec"`
}

// rendered is a manifest stream decoded document by document.
type rendered struct {
	raw   []byte
	nodes []*yaml.Node // the content of each document, in stream order
	kinds []string
	names []string // metadata.name of each document, in stream order
}

// decodeStream splits a multi-document stream and decodes each document. A
// stream that does not decode, or that holds an empty document, fails the test.
func decodeStream(t *testing.T, raw []byte) rendered {
	t.Helper()
	r := rendered{raw: raw}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for i := 0; ; i++ {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("document %d does not decode: %v\n%s", i, err, raw)
		}
		if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
			t.Fatalf("document %d is empty or is not a mapping", i)
		}
		var o object
		if err := doc.Content[0].Decode(&o); err != nil {
			t.Fatalf("document %d: %v", i, err)
		}
		r.nodes = append(r.nodes, doc.Content[0])
		r.kinds = append(r.kinds, o.Kind)
		r.names = append(r.names, o.Metadata.Name)
	}
	return r
}

// index returns the position of the document of the given kind and name. An
// empty name asks for the only document of that kind. The bench renders two
// PersistentVolumeClaims, two Deployments and two Services (Nexus and its
// upstream), so a helper that does not say which one it wants is a mistake.
func (r rendered) index(t *testing.T, kind, name string) int {
	t.Helper()
	found := -1
	for i, k := range r.kinds {
		if k != kind || (name != "" && r.names[i] != name) {
			continue
		}
		if found >= 0 {
			t.Fatalf("two documents of kind %s named %q", kind, name)
		}
		found = i
	}
	if found < 0 {
		t.Fatalf("no document of kind %s named %q in %v %v", kind, name, r.kinds, r.names)
	}
	return found
}

// of decodes the document of the given kind and name into out.
func (r rendered) of(t *testing.T, kind, name string, out any) {
	t.Helper()
	if err := r.nodes[r.index(t, kind, name)].Decode(out); err != nil {
		t.Fatalf("decode %s %s: %v", kind, name, err)
	}
}

// text is one document written back as YAML without its comments, so a test can
// look for a word in the object and not in what the template says around it.
func (r rendered) text(t *testing.T, kind, name string) string {
	t.Helper()
	out, err := yaml.Marshal(r.nodes[r.index(t, kind, name)])
	if err != nil {
		t.Fatalf("encode %s %s: %v", kind, name, err)
	}
	return string(out)
}

func (r rendered) pvc(t *testing.T) pvcObject { return r.pvcNamed(t, "nexus-data") }

func (r rendered) pvcNamed(t *testing.T, name string) pvcObject {
	t.Helper()
	var o pvcObject
	r.of(t, "PersistentVolumeClaim", name, &o)
	return o
}

func (r rendered) deployment(t *testing.T) deploymentObject { return r.deploymentNamed(t, "nexus") }

func (r rendered) deploymentNamed(t *testing.T, name string) deploymentObject {
	t.Helper()
	var o deploymentObject
	r.of(t, "Deployment", name, &o)
	return o
}

func (r rendered) service(t *testing.T) serviceObject { return r.serviceNamed(t, "nexus") }

func (r rendered) serviceNamed(t *testing.T, name string) serviceObject {
	t.Helper()
	var o serviceObject
	r.of(t, "Service", name, &o)
	return o
}

func (r rendered) namespace(t *testing.T) object {
	t.Helper()
	var o object
	r.of(t, "Namespace", "", &o)
	return o
}

func mustRender(t *testing.T, p Params) rendered {
	t.Helper()
	if err := p.Validate(); err != nil {
		t.Fatalf("the test params are not valid: %v", err)
	}
	raw, err := Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return decodeStream(t, raw)
}

// customParams moves every value the manifests carry away from its default.
func customParams() Params {
	p := Defaults()
	p.Namespace = "nexus-bench-2"
	p.HTTPNodePort = 30581
	p.DockerNodePort = 30582
	p.Image = "registry.example.com/team/nexus3"
	p.ImageTag = "3.99.0"
	p.CPURequest = "750m"
	p.CPULimit = "1500m"
	p.MemoryRequest = "2560Mi"
	p.MemoryLimit = "5Gi"
	p.StorageSize = "7Gi"
	p.JVMOpts = "-Xms512m -Xmx512m -XX:MaxDirectMemorySize=768m"
	p.UpstreamImage = "registry.example.com/team/registry"
	p.UpstreamImageTag = "3.0.1"
	return p
}

// Every object this package renders, in the order Render writes them: the
// Namespace, the Nexus objects, then the upstream registry's.
var (
	wantKinds = []string{
		"Namespace",
		"PersistentVolumeClaim", "Deployment", "Service",
		"PersistentVolumeClaim", "Deployment", "Service",
	}
	wantNames = []string{
		"", // the Namespace carries the parameter, not a fixed name
		"nexus-data", "nexus", "nexus",
		upstreamName + "-data", upstreamName, upstreamName,
	}
)

const (
	upstreamName = "bench-upstream"
	upstreamPort = 5000
)

func TestRenderGolden(t *testing.T) {
	got, err := Render(Defaults())
	if err != nil {
		t.Fatalf("Render(Defaults()): %v", err)
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenDefault), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenDefault, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenDefault)
	if err != nil {
		t.Fatalf("%v\nrun: go test ./internal/nexusbench -run TestRenderGolden -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the render of Defaults() differs from %s (run with -update and review the diff)\n--- got ---\n%s\n--- want ---\n%s", goldenDefault, got, want)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			first, err := Render(p)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 5; i++ {
				again, err := Render(p)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first, again) {
					t.Fatalf("render %d differs from the first one", i+2)
				}
			}
		})
	}
}

func TestRenderIgnoresParamsTheManifestsDoNotCarry(t *testing.T) {
	base, err := Render(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	p := Defaults()
	p.Context = "other-context"
	p.UpstreamURL = "https://upstream.example.com:5000" // a URL changes no manifest, even with the upstream deployed
	p.Anonymous = false
	p.AcceptEULA = true
	p.Hosts = []string{"10.0.0.1", "node.example.com"}
	p.Timeout = 1
	p.DeleteTimeout = 2
	p.SecretsPath = "/tmp/secrets.json"
	p.DescriptorPath = "/tmp/descriptor.json"
	got, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(base, got) {
		t.Errorf("a parameter the manifests do not carry changed the render:\n--- defaults ---\n%s\n--- changed ---\n%s", base, got)
	}
}

func TestRenderStream(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			if !reflect.DeepEqual(r.kinds, wantKinds) {
				t.Fatalf("kinds = %v, want %v (Namespace first)", r.kinds, wantKinds)
			}
			for i, want := range wantNames {
				if want == "" {
					want = p.Namespace
				}
				if r.names[i] != want {
					t.Errorf("document %d (%s) is named %q, want %q", i, r.kinds[i], r.names[i], want)
				}
			}
			for i, n := range r.nodes {
				var o object
				if err := n.Decode(&o); err != nil {
					t.Fatal(err)
				}
				if o.APIVersion == "" || o.Metadata.Name == "" {
					t.Errorf("document %d (%s) has no apiVersion or no metadata.name", i, o.Kind)
				}
				if o.Kind == "Namespace" {
					if o.Metadata.Name != p.Namespace {
						t.Errorf("Namespace name = %q, want %q", o.Metadata.Name, p.Namespace)
					}
					if o.Metadata.Namespace != "" {
						t.Errorf("a Namespace carries metadata.namespace = %q", o.Metadata.Namespace)
					}
					continue
				}
				if o.Metadata.Namespace != p.Namespace {
					t.Errorf("%s metadata.namespace = %q, want %q", o.Kind, o.Metadata.Namespace, p.Namespace)
				}
			}
		})
	}
}

func TestRenderNamespaceOnly(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			raw, err := RenderNamespace(p)
			if err != nil {
				t.Fatal(err)
			}
			only := decodeStream(t, raw)
			if !reflect.DeepEqual(only.kinds, []string{"Namespace"}) {
				t.Fatalf("kinds = %v, want only a Namespace", only.kinds)
			}
			// The Namespace of RenderNamespace is the one Render starts with.
			var alone, first any
			if err := only.nodes[0].Decode(&alone); err != nil {
				t.Fatal(err)
			}
			full := mustRender(t, p)
			if err := full.nodes[0].Decode(&first); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(alone, first) {
				t.Errorf("RenderNamespace = %v, want the first document of Render = %v", alone, first)
			}
			again, err := RenderNamespace(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, again) {
				t.Error("RenderNamespace is not deterministic")
			}
		})
	}
}

func TestRenderNamespaceLabels(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			ns := mustRender(t, p).namespace(t)
			for key, want := range map[string]string{labelManagedBy: benchLabel, labelPartOf: benchLabel} {
				if got := ns.Metadata.Labels[key]; got != want {
					t.Errorf("label %s = %q, want %q", key, got, want)
				}
			}
			for key := range ns.Metadata.Labels {
				if strings.HasPrefix(key, psaPrefix) {
					t.Errorf("the Namespace carries the Pod Security label %s: the bench never sets one", key)
				}
			}
		})
	}
}

func TestRenderNamespaceCarriesNoPodSecurityMetadata(t *testing.T) {
	raw, err := RenderNamespace(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), psaPrefix) {
		t.Errorf("the Namespace manifest mentions %s:\n%s", psaPrefix, raw)
	}
}

func TestRenderNothingSensitive(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			lower := strings.ToLower(string(r.raw))
			for _, word := range []string{"password", "secret"} {
				if i := strings.Index(lower, word); i >= 0 {
					t.Errorf("the render holds %q: ...%s...", word, r.raw[max(0, i-40):min(len(r.raw), i+40)])
				}
			}
			for _, kind := range r.kinds {
				if kind == "Secret" || kind == "ConfigMap" {
					t.Errorf("the render holds a %s", kind)
				}
			}
			for _, d := range []deploymentObject{r.deployment(t), r.deploymentNamed(t, upstreamName)} {
				for _, v := range d.Spec.Template.Spec.Volumes {
					if v.Secret != nil || v.ConfigMap != nil || v.HostPath != nil {
						t.Errorf("%s: volume %s is a secret, a ConfigMap or a hostPath", d.Metadata.Name, v.Name)
					}
				}
				for _, c := range allContainers(d) {
					if len(c.EnvFrom) != 0 {
						t.Errorf("%s: container %s reads its environment from a source (envFrom)", d.Metadata.Name, c.Name)
					}
					for _, e := range c.Env {
						if e.ValueFrom.Kind != 0 {
							t.Errorf("%s: container %s: env %s comes from valueFrom", d.Metadata.Name, c.Name, e.Name)
						}
					}
				}
			}
		})
	}
}

func allContainers(d deploymentObject) []container {
	spec := d.Spec.Template.Spec
	return append(append([]container(nil), spec.InitContainers...), spec.Containers...)
}

func TestRenderPVC(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			pvc := mustRender(t, p).pvc(t)
			if !reflect.DeepEqual(pvc.Spec.AccessModes, []string{"ReadWriteOnce"}) {
				t.Errorf("accessModes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
			}
			if got := pvc.Spec.Resources.Requests["storage"]; got != p.StorageSize {
				t.Errorf("storage request = %q, want %q", got, p.StorageSize)
			}
			if pvc.Spec.StorageClassName != nil {
				t.Errorf("storageClassName = %q, want it absent so the cluster default class applies", *pvc.Spec.StorageClassName)
			}
			if pvc.Spec.VolumeName != "" {
				t.Errorf("volumeName = %q, want it absent: the bench binds to no existing volume", pvc.Spec.VolumeName)
			}
		})
	}
}

func TestRenderDeploymentShape(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			d := r.deployment(t)
			spec := d.Spec

			if spec.Replicas == nil || *spec.Replicas != 1 {
				t.Errorf("replicas = %v, want 1", spec.Replicas)
			}
			if spec.Strategy.Type != "Recreate" {
				t.Errorf("strategy.type = %q, want Recreate (one ReadWriteOnce volume)", spec.Strategy.Type)
			}
			if spec.Strategy.RollingUpdate.Kind != 0 {
				t.Error("strategy.rollingUpdate is set next to Recreate: Kubernetes refuses it")
			}

			if got := spec.Selector.MatchLabels[labelName]; got != "nexus" {
				t.Errorf("selector %s = %q, want nexus", labelName, got)
			}
			podLabels := spec.Template.Metadata.Labels
			if podLabels[labelName] != "nexus" {
				t.Errorf("pod label %s = %q, want nexus", labelName, podLabels[labelName])
			}
			for k, v := range spec.Selector.MatchLabels {
				if podLabels[k] != v {
					t.Errorf("the selector asks %s=%s but the pods carry %q", k, v, podLabels[k])
				}
			}

			pod := spec.Template.Spec
			if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
				t.Errorf("automountServiceAccountToken = %v, want false", pod.AutomountServiceAccountToken)
			}
			if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
				t.Errorf("enableServiceLinks = %v, want false", pod.EnableServiceLinks)
			}
			if len(pod.Containers) != 1 {
				t.Fatalf("%d containers, want 1", len(pod.Containers))
			}

			pvc := r.pvc(t)
			mounted := false
			for _, c := range pod.Containers {
				for _, m := range c.VolumeMounts {
					if m.MountPath != "/nexus-data" {
						continue
					}
					for _, v := range pod.Volumes {
						if v.Name == m.Name && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvc.Metadata.Name {
							mounted = true
						}
					}
				}
			}
			if !mounted {
				t.Errorf("no container mounts the PVC %q on /nexus-data", pvc.Metadata.Name)
			}
		})
	}
}

func TestRenderContainersAreHardened(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			d := mustRender(t, p).deployment(t)
			pod := d.Spec.Template.Spec
			for _, c := range allContainers(d) {
				sc := c.SecurityContext
				if sc == nil {
					t.Fatalf("container %s has no securityContext", c.Name)
				}
				// The pod level and the container level both count: the
				// container's value wins, as in Kubernetes.
				user := firstInt64(sc.RunAsUser, podSC(pod).RunAsUser)
				group := firstInt64(sc.RunAsGroup, podSC(pod).RunAsGroup)
				nonRoot := firstBool(sc.RunAsNonRoot, podSC(pod).RunAsNonRoot)
				if user == nil || *user != 200 {
					t.Errorf("container %s: runAsUser = %v, want 200 (the nexus user of the image)", c.Name, user)
				}
				if group == nil || *group != 200 {
					t.Errorf("container %s: runAsGroup = %v, want 200", c.Name, group)
				}
				if nonRoot == nil || !*nonRoot {
					t.Errorf("container %s: runAsNonRoot = %v, want true", c.Name, nonRoot)
				}
				if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
					t.Errorf("container %s: allowPrivilegeEscalation = %v, want false", c.Name, sc.AllowPrivilegeEscalation)
				}
				if sc.Capabilities == nil || !reflect.DeepEqual(sc.Capabilities.Drop, []string{"ALL"}) || len(sc.Capabilities.Add) != 0 {
					t.Errorf("container %s: capabilities = %+v, want drop [ALL] and no add", c.Name, sc.Capabilities)
				}
				seccomp := sc.SeccompProfile
				if seccomp == nil {
					seccomp = podSC(pod).SeccompProfile
				}
				if seccomp == nil || seccomp.Type != "RuntimeDefault" {
					t.Errorf("container %s: seccompProfile = %+v, want RuntimeDefault", c.Name, seccomp)
				}
			}
		})
	}
}

func podSC(pod podSpec) securityContext {
	if pod.SecurityContext == nil {
		return securityContext{}
	}
	return *pod.SecurityContext
}

func firstInt64(values ...*int64) *int64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstBool(values ...*bool) *bool {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func TestRenderContainerResourcesAndImage(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			d := mustRender(t, p).deployment(t)
			for _, c := range allContainers(d) {
				want := map[string]map[string]string{
					"requests": {"cpu": p.CPURequest, "memory": p.MemoryRequest},
					"limits":   {"cpu": p.CPULimit, "memory": p.MemoryLimit},
				}
				got := map[string]map[string]string{"requests": c.Resources.Requests, "limits": c.Resources.Limits}
				for kind, resources := range want {
					for resource, value := range resources {
						if got[kind][resource] != value {
							t.Errorf("container %s: %s.%s = %q, want %q", c.Name, kind, resource, got[kind][resource], value)
						}
					}
				}
				if c.Image != p.ImageRef() {
					t.Errorf("container %s: image = %q, want %q", c.Name, c.Image, p.ImageRef())
				}
				if strings.HasPrefix(strings.ToLower(c.Image), zotRegistry) {
					t.Errorf("container %s: image %q is in %s, where unsigned images are refused", c.Name, c.Image, zotRegistry)
				}
			}
		})
	}
}

func TestRenderJVMEnvironment(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			d := mustRender(t, p).deployment(t)
			found := 0
			for _, c := range allContainers(d) {
				for _, e := range c.Env {
					if e.Name != "INSTALL4J_ADD_VM_PARAMS" {
						continue
					}
					found++
					if e.Value.Value != p.JVMOpts {
						t.Errorf("INSTALL4J_ADD_VM_PARAMS = %q, want %q", e.Value.Value, p.JVMOpts)
					}
				}
			}
			if found != 1 {
				t.Errorf("INSTALL4J_ADD_VM_PARAMS is set %d times, want once", found)
			}
		})
	}
}

func TestRenderProbes(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			d := mustRender(t, p).deployment(t)
			for _, c := range allContainers(d) {
				if c.LivenessProbe != nil {
					t.Errorf("container %s has a livenessProbe: a restart in the middle of the first start would loop", c.Name)
				}
				for probeName, pr := range map[string]*probe{"startupProbe": c.StartupProbe, "readinessProbe": c.ReadinessProbe} {
					if pr == nil || pr.HTTPGet == nil {
						t.Errorf("container %s: %s is missing or is not an httpGet", c.Name, probeName)
						continue
					}
					if pr.HTTPGet.Path != statusPath {
						t.Errorf("container %s: %s path = %q, want %q", c.Name, probeName, pr.HTTPGet.Path, statusPath)
					}
					if port := resolvePort(pr.HTTPGet.Port, c); port != 8081 {
						t.Errorf("container %s: %s port = %q resolves to %d, want 8081", c.Name, probeName, pr.HTTPGet.Port.Value, port)
					}
				}
				// The start of Nexus takes minutes: the startup probe waits up to 10 minutes.
				if sp := c.StartupProbe; sp != nil {
					if budget := sp.PeriodSeconds * sp.FailureThreshold; budget < 600 {
						t.Errorf("container %s: startupProbe waits %d s (periodSeconds %d x failureThreshold %d), want at least 600", c.Name, budget, sp.PeriodSeconds, sp.FailureThreshold)
					}
				}
			}
		})
	}
}

// resolvePort turns a probe or Service port, a number or a container port name,
// into the number; 0 when it names no port of the container.
func resolvePort(n yaml.Node, c container) int {
	if v, err := strconv.Atoi(n.Value); err == nil {
		return v
	}
	for _, p := range c.Ports {
		if p.Name == n.Value {
			return p.ContainerPort
		}
	}
	return 0
}

func TestRenderService(t *testing.T) {
	for name, p := range map[string]Params{
		"defaults": Defaults(),
		"custom":   customParams(),
	} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			svc := r.service(t)
			d := r.deployment(t)
			c := d.Spec.Template.Spec.Containers[0]

			if svc.Metadata.Name != "nexus" {
				t.Errorf("Service name = %q, want nexus", svc.Metadata.Name)
			}
			if svc.Spec.Type != "NodePort" {
				t.Errorf("Service type = %q, want NodePort", svc.Spec.Type)
			}
			if got := svc.Spec.Selector[labelName]; got != "nexus" {
				t.Errorf("Service selector %s = %q, want nexus", labelName, got)
			}
			for k, v := range svc.Spec.Selector {
				if d.Spec.Template.Metadata.Labels[k] != v {
					t.Errorf("the Service selects %s=%s but the pods carry %q", k, v, d.Spec.Template.Metadata.Labels[k])
				}
			}

			type port struct{ port, nodePort int }
			want := map[string]port{
				"http":         {8081, p.HTTPNodePort},
				"docker-group": {8082, p.DockerNodePort},
			}
			got := map[string]port{}
			for _, sp := range svc.Spec.Ports {
				got[sp.Name] = port{sp.Port, sp.NodePort}
				if target := resolvePort(sp.TargetPort, c); target != sp.Port {
					t.Errorf("port %s: targetPort %q resolves to %d on the container, want %d", sp.Name, sp.TargetPort.Value, target, sp.Port)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Service ports (port, nodePort) = %v, want %v", got, want)
			}
			if len(svc.Spec.Ports) != len(want) {
				t.Errorf("%d Service ports, want %d", len(svc.Spec.Ports), len(want))
			}

			containerPorts := map[int]bool{}
			for _, cp := range c.Ports {
				containerPorts[cp.ContainerPort] = true
			}
			if !containerPorts[8081] || !containerPorts[8082] {
				t.Errorf("container ports = %v, want 8081 and 8082 declared", containerPorts)
			}
		})
	}
}

// TestRenderCarriesNonDefaultValues checks that a render of other values
// reports them everywhere and keeps no default behind.
func TestRenderCarriesNonDefaultValues(t *testing.T) {
	p := customParams()
	r := mustRender(t, p)
	text := string(r.raw)

	for _, leftover := range []string{"30481", "30482", "docker.io/sonatype", "3.98.1", "-Xms1g", "javaprefs", "3Gi", "4Gi", "docker.io/library/registry", "3.0.0"} {
		if strings.Contains(text, leftover) {
			t.Errorf("the render still holds the default %q", leftover)
		}
	}
	if got := r.pvc(t).Spec.Resources.Requests["storage"]; got != "7Gi" {
		t.Errorf("storage = %q, want 7Gi", got)
	}
	if ns := r.namespace(t); ns.Metadata.Name != "nexus-bench-2" {
		t.Errorf("Namespace = %q, want nexus-bench-2", ns.Metadata.Name)
	}
	svc := r.service(t)
	nodePorts := map[int]bool{}
	for _, sp := range svc.Spec.Ports {
		nodePorts[sp.NodePort] = true
	}
	if !nodePorts[30581] || !nodePorts[30582] || len(nodePorts) != 2 {
		t.Errorf("nodePorts = %v, want 30581 and 30582", nodePorts)
	}
}

// TestRenderQuotesEveryValue pins the engagement of docs/architecture/nexus-bench.md:
// the template writes each value it renders between double quotes, so the
// validation is the first guard and not the only one.
func TestRenderQuotesEveryValue(t *testing.T) {
	p := customParams()
	text := string(mustRender(t, p).raw)

	values := []string{
		p.Namespace, p.ImageRef(), p.JVMOpts,
		p.CPURequest, p.CPULimit, p.MemoryRequest, p.MemoryLimit, p.StorageSize,
		p.UpstreamImageRef(),
	}
	for _, v := range values {
		occurrences := 0
		for from := 0; ; {
			i := strings.Index(text[from:], v)
			if i < 0 {
				break
			}
			i += from
			from = i + len(v)
			occurrences++
			if i == 0 || text[i-1] != '"' || from >= len(text) || text[from] != '"' {
				start, end := max(0, i-30), min(len(text), from+30)
				t.Errorf("the value %q is not written between double quotes: ...%s...", v, text[start:end])
			}
		}
		if occurrences == 0 {
			t.Errorf("the value %q does not appear in the render", v)
		}
	}
}

// TestRenderRoundTripsTrickyJVMOpts renders values that Validate accepts and
// that a plain YAML scalar would read as a number, a boolean, a null, a float or
// a mapping, and checks the value decodes as the same string.
func TestRenderRoundTripsTrickyJVMOpts(t *testing.T) {
	cases := []string{
		"-Dfoo=a:b",
		"-Dfoo=a:b -Dbar=c:d",
		"-Dpath=/a/b+c@d%e,f",
		"-Xmx1g",
		"true",
		"false",
		"null",
		"yes",
		"no",
		"on",
		"123",
		"1.5",
		"1e3",
		"0x1F",
		"0o17",
		"12:30:45",
		"1,000",
		"-1",
		"-.inf",
		"-.nan",
		"2001-01-23",
		"a b",
	}
	for _, opts := range cases {
		t.Run(opts, func(t *testing.T) {
			p := Defaults()
			p.JVMOpts = opts
			if err := p.Validate(); err != nil {
				t.Fatalf("the case is meant to be valid but Validate refuses %q: %v", opts, err)
			}
			d := mustRender(t, p).deployment(t)
			found := false
			for _, c := range allContainers(d) {
				for _, e := range c.Env {
					if e.Name != "INSTALL4J_ADD_VM_PARAMS" {
						continue
					}
					found = true
					if e.Value.Tag != "!!str" {
						t.Errorf("the value decodes as %s, want !!str", e.Value.Tag)
					}
					if e.Value.Value != opts {
						t.Errorf("decoded value = %q, want %q", e.Value.Value, opts)
					}
				}
			}
			if !found {
				t.Error("INSTALL4J_ADD_VM_PARAMS is not set")
			}
		})
	}
}

func TestRenderRefusesInvalidParams(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *Params)
		wantIn string // a variable name Validate reports, proving the error comes from it
	}{
		{"reserved namespace", func(p *Params) { p.Namespace = "default" }, "NEXUS_BENCH_NAMESPACE"},
		{"kube namespace", func(p *Params) { p.Namespace = "kube-system" }, "NEXUS_BENCH_NAMESPACE"},
		{"namespace with an upper-case letter", func(p *Params) { p.Namespace = "Bench" }, "NEXUS_BENCH_NAMESPACE"},
		{"empty namespace", func(p *Params) { p.Namespace = "" }, "NEXUS_BENCH_NAMESPACE"},
		{"namespace that injects a key", func(p *Params) { p.Namespace = "a\nkind: Secret" }, "NEXUS_BENCH_NAMESPACE"},
		{"same NodePort twice", func(p *Params) { p.DockerNodePort = p.HTTPNodePort }, "NEXUS_BENCH_HTTP_NODEPORT"},
		{"NodePort out of range", func(p *Params) { p.HTTPNodePort = 8081 }, "NEXUS_BENCH_HTTP_NODEPORT"},
		{"image from the zot registry", func(p *Params) { p.Image = "tvp-zot.incus:5000/nexus3" }, "NEXUS_BENCH_IMAGE"},
		{"upstream image from the zot registry", func(p *Params) { p.UpstreamImage = "tvp-zot.incus:5000/library/registry" }, "NEXUS_BENCH_UPSTREAM_IMAGE"},
		{"upstream image tag with a space", func(p *Params) { p.UpstreamImageTag = "3.0 0" }, "NEXUS_BENCH_UPSTREAM_IMAGE_TAG"},
		{"image tag with a space", func(p *Params) { p.ImageTag = "3.98 1" }, "NEXUS_BENCH_IMAGE_TAG"},
		{"empty JVM options", func(p *Params) { p.JVMOpts = "" }, "NEXUS_BENCH_JVM_OPTS"},
		{"JVM options with a newline", func(p *Params) { p.JVMOpts = "-Xmx1g\n  - name: x" }, "NEXUS_BENCH_JVM_OPTS"},
		{"JVM options with a quote", func(p *Params) { p.JVMOpts = `-Dx="a"` }, "NEXUS_BENCH_JVM_OPTS"},
		{"JVM options holding a colon and a space", func(p *Params) { p.JVMOpts = "-Dx: y" }, "NEXUS_BENCH_JVM_OPTS"},
		{"CPU request above its limit", func(p *Params) { p.CPURequest = "3" }, "NEXUS_BENCH_CPU_REQUEST"},
		{"memory that is not a quantity", func(p *Params) { p.MemoryLimit = "lots" }, "NEXUS_BENCH_MEMORY_LIMIT"},
		{"zero storage", func(p *Params) { p.StorageSize = "0Gi" }, "NEXUS_BENCH_STORAGE_SIZE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Defaults()
			tc.mutate(&p)
			if p.Validate() == nil {
				t.Fatal("the test case is meant to be invalid but Validate accepts it")
			}

			raw, err := Render(p)
			if err == nil {
				t.Fatalf("Render accepted invalid params and wrote:\n%s", raw)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("Render error = %q, want it to carry Validate's message naming %s", err, tc.wantIn)
			}
			if len(raw) != 0 {
				t.Errorf("Render returned %d bytes next to an error, want none", len(raw))
			}

			raw, err = RenderNamespace(p)
			if err == nil {
				t.Fatalf("RenderNamespace accepted invalid params and wrote:\n%s", raw)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("RenderNamespace error = %q, want it to carry Validate's message naming %s", err, tc.wantIn)
			}
			if len(raw) != 0 {
				t.Errorf("RenderNamespace returned %d bytes next to an error, want none", len(raw))
			}
		})
	}
}

// The tests below pin the upstream registry the bench deploys next to Nexus
// (DEVDESK-505, step 2): a PVC, a Deployment and a ClusterIP Service, all named
// bench-upstream.

// upstreamObjectName is the name of the upstream object of the given kind.
func upstreamObjectName(kind string) string {
	if kind == "PersistentVolumeClaim" {
		return upstreamName + "-data"
	}
	return upstreamName
}

func TestRenderUpstreamLabels(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			if len(r.nodes) != len(wantKinds) {
				t.Fatalf("%d documents, want %d: %v", len(r.nodes), len(wantKinds), r.kinds)
			}
			for i, n := range r.nodes {
				var o object
				if err := n.Decode(&o); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{labelManagedBy, labelPartOf} {
					if got := o.Metadata.Labels[key]; got != benchLabel {
						t.Errorf("document %d (%s %s): label %s = %q, want %q", i, o.Kind, o.Metadata.Name, key, got, benchLabel)
					}
				}
			}
			for _, kind := range []string{"PersistentVolumeClaim", "Deployment", "Service"} {
				var o object
				r.of(t, kind, upstreamObjectName(kind), &o)
				if got := o.Metadata.Labels[labelName]; got != upstreamName {
					t.Errorf("%s: label %s = %q, want %s", kind, labelName, got, upstreamName)
				}
			}
			d := r.deploymentNamed(t, upstreamName)
			for _, key := range []string{labelManagedBy, labelPartOf} {
				if got := d.Spec.Template.Metadata.Labels[key]; got != benchLabel {
					t.Errorf("pod template label %s = %q, want %q", key, got, benchLabel)
				}
			}
		})
	}
}

func TestRenderUpstreamPVC(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			pvc := mustRender(t, p).pvcNamed(t, upstreamName+"-data")
			if !reflect.DeepEqual(pvc.Spec.AccessModes, []string{"ReadWriteOnce"}) {
				t.Errorf("accessModes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
			}
			// 1Gi whatever NEXUS_BENCH_STORAGE_SIZE says: that one sizes Nexus.
			if got := pvc.Spec.Resources.Requests["storage"]; got != "1Gi" {
				t.Errorf("storage request = %q, want 1Gi (not the Nexus size %q)", got, p.StorageSize)
			}
			if pvc.Spec.StorageClassName != nil {
				t.Errorf("storageClassName = %q, want it absent", *pvc.Spec.StorageClassName)
			}
			if pvc.Spec.VolumeName != "" {
				t.Errorf("volumeName = %q, want it absent", pvc.Spec.VolumeName)
			}
		})
	}
}

// TestRenderUpstreamDeploymentDoesNotWriteReplicas pins F5: the Deployment of the
// upstream leaves spec.replicas out, so that kubectl scale (cut and restore) owns
// the field and a server-side apply of the whole stream neither conflicts with it
// nor puts the upstream back by itself. Nexus keeps its replicas: 1.
func TestRenderUpstreamDeploymentDoesNotWriteReplicas(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			up := r.deploymentNamed(t, upstreamName)
			if up.Spec.Replicas != nil {
				t.Errorf("the upstream Deployment writes replicas = %d, want the field absent", *up.Spec.Replicas)
			}
			if text := r.text(t, "Deployment", upstreamName); strings.Contains(text, "replicas") {
				t.Errorf("the upstream Deployment mentions replicas:\n%s", text)
			}
			nexus := r.deployment(t)
			if nexus.Spec.Replicas == nil || *nexus.Spec.Replicas != 1 {
				t.Errorf("the nexus Deployment writes replicas = %v, want 1", nexus.Spec.Replicas)
			}
		})
	}
}

func TestRenderUpstreamDeploymentShape(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			d := r.deploymentNamed(t, upstreamName)
			spec := d.Spec
			pod := spec.Template.Spec

			if spec.Strategy.Type != "Recreate" {
				t.Errorf("strategy.type = %q, want Recreate (one ReadWriteOnce volume)", spec.Strategy.Type)
			}
			if spec.Strategy.RollingUpdate.Kind != 0 {
				t.Error("strategy.rollingUpdate is set next to Recreate: Kubernetes refuses it")
			}
			if got := spec.Selector.MatchLabels[labelName]; got != upstreamName {
				t.Errorf("selector %s = %q, want %s", labelName, got, upstreamName)
			}
			for k, v := range spec.Selector.MatchLabels {
				if got := spec.Template.Metadata.Labels[k]; got != v {
					t.Errorf("the selector asks %s=%s but the pods carry %q", k, v, got)
				}
			}
			if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
				t.Errorf("automountServiceAccountToken = %v, want false", pod.AutomountServiceAccountToken)
			}
			if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
				t.Errorf("enableServiceLinks = %v, want false", pod.EnableServiceLinks)
			}
			if len(pod.InitContainers) != 0 || len(pod.Containers) != 1 {
				t.Fatalf("%d init containers and %d containers, want 0 and 1", len(pod.InitContainers), len(pod.Containers))
			}
			c := pod.Containers[0]

			if c.Image != p.UpstreamImageRef() {
				t.Errorf("image = %q, want %q", c.Image, p.UpstreamImageRef())
			}
			if strings.HasPrefix(strings.ToLower(c.Image), zotRegistry) {
				t.Errorf("image %q is in %s, where unsigned images are refused", c.Image, zotRegistry)
			}
			if len(c.Ports) != 1 || c.Ports[0].ContainerPort != upstreamPort {
				t.Errorf("container ports = %+v, want the single port %d", c.Ports, upstreamPort)
			}

			// Distribution v3 traces by default; the bench turns it off.
			traces := 0
			for _, e := range c.Env {
				if e.Name == "OTEL_TRACES_EXPORTER" {
					traces++
					if e.Value.Value != "none" || e.Value.Tag != "!!str" {
						t.Errorf("OTEL_TRACES_EXPORTER = %q (%s), want the string none", e.Value.Value, e.Value.Tag)
					}
				}
			}
			if traces != 1 {
				t.Errorf("OTEL_TRACES_EXPORTER is set %d times, want once", traces)
			}

			// The data volume is the upstream's own claim, not the one of Nexus.
			mounted := false
			for _, m := range c.VolumeMounts {
				for _, v := range pod.Volumes {
					if v.Name == m.Name && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == upstreamName+"-data" {
						mounted = true
					}
				}
			}
			if !mounted {
				t.Errorf("the container does not mount the claim %s-data", upstreamName)
			}
			for _, v := range pod.Volumes {
				if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName != upstreamName+"-data" {
					t.Errorf("volume %s claims %q: the upstream never touches another claim", v.Name, v.PersistentVolumeClaim.ClaimName)
				}
			}
		})
	}
}

// TestRenderUpstreamResources: the upstream is small and fixed; the Nexus
// resource parameters do not size it.
func TestRenderUpstreamResources(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			c := mustRender(t, p).deploymentNamed(t, upstreamName).Spec.Template.Spec.Containers[0]
			want := map[string]map[string]string{
				"requests": {"cpu": "50m", "memory": "64Mi"},
				"limits":   {"cpu": "500m", "memory": "256Mi"},
			}
			if !reflect.DeepEqual(c.Resources.Requests, want["requests"]) {
				t.Errorf("requests = %v, want %v", c.Resources.Requests, want["requests"])
			}
			if !reflect.DeepEqual(c.Resources.Limits, want["limits"]) {
				t.Errorf("limits = %v, want %v", c.Resources.Limits, want["limits"])
			}
		})
	}
}

func TestRenderUpstreamContainerIsHardened(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			d := mustRender(t, p).deploymentNamed(t, upstreamName)
			pod := d.Spec.Template.Spec
			c := pod.Containers[0]
			sc := c.SecurityContext
			if sc == nil {
				t.Fatal("the container has no securityContext")
			}
			user := firstInt64(sc.RunAsUser, podSC(pod).RunAsUser)
			group := firstInt64(sc.RunAsGroup, podSC(pod).RunAsGroup)
			nonRoot := firstBool(sc.RunAsNonRoot, podSC(pod).RunAsNonRoot)
			if user == nil || *user != 1000 {
				t.Errorf("runAsUser = %v, want 1000", user)
			}
			if group == nil || *group != 1000 {
				t.Errorf("runAsGroup = %v, want 1000", group)
			}
			if nonRoot == nil || !*nonRoot {
				t.Errorf("runAsNonRoot = %v, want true", nonRoot)
			}
			if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
				t.Errorf("allowPrivilegeEscalation = %v, want false", sc.AllowPrivilegeEscalation)
			}
			if sc.Capabilities == nil || !reflect.DeepEqual(sc.Capabilities.Drop, []string{"ALL"}) || len(sc.Capabilities.Add) != 0 {
				t.Errorf("capabilities = %+v, want drop [ALL] and no add", sc.Capabilities)
			}
			seccomp := sc.SeccompProfile
			if seccomp == nil {
				seccomp = podSC(pod).SeccompProfile
			}
			if seccomp == nil || seccomp.Type != "RuntimeDefault" {
				t.Errorf("seccompProfile = %+v, want RuntimeDefault", seccomp)
			}
		})
	}
}

func TestRenderUpstreamReadinessProbe(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			c := mustRender(t, p).deploymentNamed(t, upstreamName).Spec.Template.Spec.Containers[0]
			pr := c.ReadinessProbe
			if pr == nil || pr.HTTPGet == nil {
				t.Fatal("readinessProbe is missing or is not an httpGet")
			}
			if pr.HTTPGet.Path != "/v2/" {
				t.Errorf("readinessProbe path = %q, want /v2/", pr.HTTPGet.Path)
			}
			if port := resolvePort(pr.HTTPGet.Port, c); port != upstreamPort {
				t.Errorf("readinessProbe port = %q resolves to %d, want %d", pr.HTTPGet.Port.Value, port, upstreamPort)
			}
		})
	}
}

// TestRenderUpstreamServiceIsClusterInternal: the upstream is reached by the
// Service DNS name inside the cluster and by a port-forward from the host, never
// through a node port.
func TestRenderUpstreamServiceIsClusterInternal(t *testing.T) {
	for name, p := range map[string]Params{"defaults": Defaults(), "custom": customParams()} {
		t.Run(name, func(t *testing.T) {
			r := mustRender(t, p)
			svc := r.serviceNamed(t, upstreamName)
			d := r.deploymentNamed(t, upstreamName)
			c := d.Spec.Template.Spec.Containers[0]

			if svc.Spec.Type != "ClusterIP" {
				t.Errorf("Service type = %q, want ClusterIP", svc.Spec.Type)
			}
			if len(svc.Spec.Ports) != 1 {
				t.Fatalf("%d Service ports, want 1", len(svc.Spec.Ports))
			}
			sp := svc.Spec.Ports[0]
			if sp.Name != "registry" || sp.Port != upstreamPort {
				t.Errorf("Service port = %s %d, want registry %d", sp.Name, sp.Port, upstreamPort)
			}
			if target := resolvePort(sp.TargetPort, c); target != upstreamPort {
				t.Errorf("targetPort %q resolves to %d on the container, want %d", sp.TargetPort.Value, target, upstreamPort)
			}
			if sp.NodePort != 0 {
				t.Errorf("nodePort = %d, want none", sp.NodePort)
			}
			for k, v := range svc.Spec.Selector {
				if got := d.Spec.Template.Metadata.Labels[k]; got != v {
					t.Errorf("the Service selects %s=%s but the pods carry %q", k, v, got)
				}
			}
			if got := svc.Spec.Selector[labelName]; got != upstreamName {
				t.Errorf("Service selector %s = %q, want %s", labelName, got, upstreamName)
			}

			// The word, not only the decoded field: no nodePort key and no NodePort type.
			for _, kind := range []string{"Service", "Deployment", "PersistentVolumeClaim"} {
				if text := strings.ToLower(r.text(t, kind, upstreamObjectName(kind))); strings.Contains(text, "nodeport") {
					t.Errorf("the upstream %s mentions a node port:\n%s", kind, text)
				}
			}
		})
	}
}

// changedDocuments renders both parameter sets and returns, per document, whether
// its decoded content differs between the two renders.
func changedDocuments(t *testing.T, before, after Params) (r rendered, changed []bool) {
	t.Helper()
	a, b := mustRender(t, before), mustRender(t, after)
	if len(a.nodes) != len(wantKinds) || len(b.nodes) != len(wantKinds) {
		t.Fatalf("%d and %d documents, want %d each", len(a.nodes), len(b.nodes), len(wantKinds))
	}
	for i := range a.nodes {
		var x, y any
		if err := a.nodes[i].Decode(&x); err != nil {
			t.Fatal(err)
		}
		if err := b.nodes[i].Decode(&y); err != nil {
			t.Fatal(err)
		}
		changed = append(changed, !reflect.DeepEqual(x, y))
	}
	return a, changed
}

// TestRenderUpstreamImageOnlyMovesTheUpstream: the upstream image changes the
// upstream Deployment and no other document.
func TestRenderUpstreamImageOnlyMovesTheUpstream(t *testing.T) {
	p := Defaults()
	p.UpstreamImage = "registry.example.com/team/registry"
	p.UpstreamImageTag = "3.0.1"
	base, changed := changedDocuments(t, Defaults(), p)
	for i, got := range changed {
		want := base.kinds[i] == "Deployment" && base.names[i] == upstreamName
		if got != want {
			t.Errorf("document %d (%s %s): changed = %v after moving the upstream image, want %v", i, base.kinds[i], base.names[i], got, want)
		}
	}
}

// TestRenderNexusParamsDoNotReachTheUpstream: every parameter that only shapes
// Nexus (image, node ports, resources, storage, JVM options) leaves the three
// bench-upstream documents byte-for-byte the same.
func TestRenderNexusParamsDoNotReachTheUpstream(t *testing.T) {
	p := customParams()
	// Keep what the upstream legitimately follows: the namespace and its own image.
	p.Namespace = Defaults().Namespace
	p.UpstreamImage = Defaults().UpstreamImage
	p.UpstreamImageTag = Defaults().UpstreamImageTag
	base, changed := changedDocuments(t, Defaults(), p)

	nexusMoved := false
	for i, got := range changed {
		if base.names[i] == upstreamName || base.names[i] == upstreamName+"-data" {
			if got {
				t.Errorf("document %d (%s %s) changed when only Nexus parameters changed", i, base.kinds[i], base.names[i])
			}
			continue
		}
		nexusMoved = nexusMoved || got
	}
	if !nexusMoved {
		t.Error("no Nexus document changed: the test moves nothing")
	}
}
