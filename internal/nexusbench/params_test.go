package nexusbench

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// envFrom builds a getenv function over a fixed map. An unset variable reads as
// the empty string, like os.Getenv, and an empty value counts as unset.
func envFrom(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// resolve runs FromEnv on top of Defaults and then Validate, the way the
// command does. It returns the first error, from either step.
func resolve(env map[string]string) (Params, error) {
	p, err := FromEnv(Defaults(), envFrom(env))
	if err != nil {
		return p, err
	}
	return p, p.Validate()
}

func TestDefaults(t *testing.T) {
	p := Defaults()

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Context", p.Context, "datomir"},
		{"Namespace", p.Namespace, "nexus-bench"},
		{"HTTPNodePort", p.HTTPNodePort, 30481},
		{"DockerNodePort", p.DockerNodePort, 30482},
		{"CPURequest", p.CPURequest, "500m"},
		{"CPULimit", p.CPULimit, "2"},
		{"MemoryRequest", p.MemoryRequest, "3Gi"},
		{"MemoryLimit", p.MemoryLimit, "4Gi"},
		{"StorageSize", p.StorageSize, "5Gi"},
		{"Image", p.Image, "docker.io/sonatype/nexus3"},
		{"ImageTag", p.ImageTag, "3.98.1"},
		{
			"JVMOpts", p.JVMOpts,
			"-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		},
		{"Timeout", p.Timeout, 10 * time.Minute},
		{"DeleteTimeout", p.DeleteTimeout, 3 * time.Minute},
		{"UpstreamURL", p.UpstreamURL, "http://bench-upstream.nexus-bench.svc.cluster.local:5000"},
		{"Anonymous", p.Anonymous, true},
		{"AcceptEULA", p.AcceptEULA, false},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !reflect.DeepEqual(c.got, c.want) {
				t.Errorf("Defaults().%s = %#v, want %#v", c.name, c.got, c.want)
			}
		})
	}

	if err := p.Validate(); err != nil {
		t.Errorf("Defaults().Validate() = %v, want nil", err)
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		name  string
		image string
		tag   string
		want  string
	}{
		{"defaults", "docker.io/sonatype/nexus3", "3.98.1", "docker.io/sonatype/nexus3:3.98.1"},
		{"other tag", "docker.io/sonatype/nexus3", "3.99.0", "docker.io/sonatype/nexus3:3.99.0"},
		{"other image", "registry.example.org/team/nexus", "1.2.3", "registry.example.org/team/nexus:1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			p.Image, p.ImageTag = tt.image, tt.tag
			if got := p.ImageRef(); got != tt.want {
				t.Errorf("ImageRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromEnvNothingSetKeepsBase(t *testing.T) {
	got, err := FromEnv(Defaults(), envFrom(nil))
	if err != nil {
		t.Fatalf("FromEnv(no variable) error = %v", err)
	}
	if want := Defaults(); !reflect.DeepEqual(got, want) {
		t.Errorf("FromEnv(no variable) = %#v, want the base %#v", got, want)
	}

	// An empty value reads as unset.
	got, err = FromEnv(Defaults(), envFrom(map[string]string{
		"NEXUS_BENCH_NAMESPACE": "",
		"NEXUS_BENCH_CONTEXT":   "",
	}))
	if err != nil {
		t.Fatalf("FromEnv(empty values) error = %v", err)
	}
	if want := Defaults(); !reflect.DeepEqual(got, want) {
		t.Errorf("FromEnv(empty values) = %#v, want the base %#v", got, want)
	}
}

func TestFromEnvDoesNotMutateBase(t *testing.T) {
	base := Defaults()
	base.Hosts = []string{"127.0.0.1"}
	snapshot := base
	snapshot.Hosts = append([]string(nil), base.Hosts...)

	_, err := FromEnv(base, envFrom(map[string]string{
		"NEXUS_BENCH_NAMESPACE": "nexus-bench-2",
		"NEXUS_BENCH_HOSTS":     "10.0.0.5,10.0.0.6",
	}))
	if err != nil {
		t.Fatalf("FromEnv error = %v", err)
	}
	if !reflect.DeepEqual(base, snapshot) {
		t.Errorf("FromEnv changed its base: got %#v, want %#v", base, snapshot)
	}
}

// Each variable overrides exactly its own field; every other field keeps the
// default.
func TestFromEnvOverrides(t *testing.T) {
	tests := []struct {
		variable string
		value    string
		mutate   func(want *Params)
	}{
		{"NEXUS_BENCH_CONTEXT", "other-ctx", func(w *Params) { w.Context = "other-ctx" }},
		{"NEXUS_BENCH_NAMESPACE", "nexus-bench-2", func(w *Params) {
			w.Namespace = "nexus-bench-2"
			w.UpstreamURL = "http://bench-upstream.nexus-bench-2.svc.cluster.local:5000"
		}},
		{"NEXUS_BENCH_HTTP_NODEPORT", "30500", func(w *Params) { w.HTTPNodePort = 30500 }},
		{"NEXUS_BENCH_DOCKER_NODEPORT", "30501", func(w *Params) { w.DockerNodePort = 30501 }},
		{"NEXUS_BENCH_CPU_REQUEST", "250m", func(w *Params) { w.CPURequest = "250m" }},
		{"NEXUS_BENCH_CPU_LIMIT", "4", func(w *Params) { w.CPULimit = "4" }},
		{"NEXUS_BENCH_MEMORY_REQUEST", "2Gi", func(w *Params) { w.MemoryRequest = "2Gi" }},
		{"NEXUS_BENCH_MEMORY_LIMIT", "6Gi", func(w *Params) { w.MemoryLimit = "6Gi" }},
		{"NEXUS_BENCH_STORAGE_SIZE", "10Gi", func(w *Params) { w.StorageSize = "10Gi" }},
		{"NEXUS_BENCH_JVM_OPTS", "-Xms2g -Xmx2g", func(w *Params) { w.JVMOpts = "-Xms2g -Xmx2g" }},
		{"NEXUS_BENCH_IMAGE", "docker.io/sonatype/nexus3-custom", func(w *Params) { w.Image = "docker.io/sonatype/nexus3-custom" }},
		{"NEXUS_BENCH_IMAGE_TAG", "3.99.0", func(w *Params) { w.ImageTag = "3.99.0" }},
		{"NEXUS_BENCH_UPSTREAM_URL", "http://upstream.example.org:5000", func(w *Params) { w.UpstreamURL = "http://upstream.example.org:5000" }},
		{"NEXUS_BENCH_ANONYMOUS", "false", func(w *Params) { w.Anonymous = false }},
		{"NEXUS_BENCH_ACCEPT_EULA", "yes", func(w *Params) { w.AcceptEULA = true }},
		{"NEXUS_BENCH_HOSTS", "127.0.0.1,10.0.0.5", func(w *Params) { w.Hosts = []string{"127.0.0.1", "10.0.0.5"} }},
		{"NEXUS_BENCH_TIMEOUT", "20m", func(w *Params) { w.Timeout = 20 * time.Minute }},
		{"NEXUS_BENCH_DELETE_TIMEOUT", "5m", func(w *Params) { w.DeleteTimeout = 5 * time.Minute }},
		{"NEXUS_BENCH_SECRETS", "/some/dir/secrets.json", func(w *Params) { w.SecretsPath = "/some/dir/secrets.json" }},
		{"NEXUS_BENCH_DESCRIPTOR", "/some/dir/descriptor.json", func(w *Params) { w.DescriptorPath = "/some/dir/descriptor.json" }},
	}
	for _, tt := range tests {
		t.Run(tt.variable, func(t *testing.T) {
			want := Defaults()
			tt.mutate(&want)

			got, err := FromEnv(Defaults(), envFrom(map[string]string{tt.variable: tt.value}))
			if err != nil {
				t.Fatalf("FromEnv(%s=%q) error = %v", tt.variable, tt.value, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("FromEnv(%s=%q)\n got  %#v\n want %#v", tt.variable, tt.value, got, want)
			}
		})
	}
}

func TestFromEnvExplicitUpstreamWinsOverDerived(t *testing.T) {
	got, err := FromEnv(Defaults(), envFrom(map[string]string{
		"NEXUS_BENCH_NAMESPACE":    "nexus-bench-2",
		"NEXUS_BENCH_UPSTREAM_URL": "http://upstream.example.org:5000",
	}))
	if err != nil {
		t.Fatalf("FromEnv error = %v", err)
	}
	if want := "http://upstream.example.org:5000"; got.UpstreamURL != want {
		t.Errorf("UpstreamURL = %q, want the explicit %q", got.UpstreamURL, want)
	}
	if got.Namespace != "nexus-bench-2" {
		t.Errorf("Namespace = %q, want nexus-bench-2", got.Namespace)
	}
}

func TestFromEnvAcceptEULA(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"yes", true},
		{"no", false},
		{"true", false},
		{"1", false},
		{"y", false},
		{"ok", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run("value="+tt.value, func(t *testing.T) {
			got, err := FromEnv(Defaults(), envFrom(map[string]string{"NEXUS_BENCH_ACCEPT_EULA": tt.value}))
			if err != nil {
				t.Fatalf("FromEnv error = %v", err)
			}
			if got.AcceptEULA != tt.want {
				t.Errorf("AcceptEULA with %q = %v, want %v", tt.value, got.AcceptEULA, tt.want)
			}
		})
	}
}

// A value that cannot be parsed is an error from FromEnv itself, and the
// message names the variable so the user knows which one to fix.
func TestFromEnvParseErrorNamesTheVariable(t *testing.T) {
	tests := []struct {
		variable string
		value    string
	}{
		{"NEXUS_BENCH_HTTP_NODEPORT", "abc"},
		{"NEXUS_BENCH_DOCKER_NODEPORT", "30.5"},
		{"NEXUS_BENCH_TIMEOUT", "soon"},
		{"NEXUS_BENCH_DELETE_TIMEOUT", "3 minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.variable, func(t *testing.T) {
			_, err := FromEnv(Defaults(), envFrom(map[string]string{tt.variable: tt.value}))
			if err == nil {
				t.Fatalf("FromEnv(%s=%q) error = nil, want an error", tt.variable, tt.value)
			}
			if !strings.Contains(err.Error(), tt.variable) {
				t.Errorf("error %q does not name %s", err, tt.variable)
			}
		})
	}
}

// Validate refuses on the Params themselves, and the error names the variable
// the user has to fix.
func TestValidateRefuses(t *testing.T) {
	const (
		ns      = "NEXUS_BENCH_NAMESPACE"
		ctx     = "NEXUS_BENCH_CONTEXT"
		httpNP  = "NEXUS_BENCH_HTTP_NODEPORT"
		dockNP  = "NEXUS_BENCH_DOCKER_NODEPORT"
		cpuReq  = "NEXUS_BENCH_CPU_REQUEST"
		cpuLim  = "NEXUS_BENCH_CPU_LIMIT"
		memReq  = "NEXUS_BENCH_MEMORY_REQUEST"
		memLim  = "NEXUS_BENCH_MEMORY_LIMIT"
		storage = "NEXUS_BENCH_STORAGE_SIZE"
		jvm     = "NEXUS_BENCH_JVM_OPTS"
		image   = "NEXUS_BENCH_IMAGE"
		tag     = "NEXUS_BENCH_IMAGE_TAG"
		upURL   = "NEXUS_BENCH_UPSTREAM_URL"
		hosts   = "NEXUS_BENCH_HOSTS"
		secrets = "NEXUS_BENCH_SECRETS"
		descr   = "NEXUS_BENCH_DESCRIPTOR"
		timeout = "NEXUS_BENCH_TIMEOUT"
		delTime = "NEXUS_BENCH_DELETE_TIMEOUT"
	)
	tests := []struct {
		name     string
		variable string
		mutate   func(p *Params)
	}{
		// Reserved or foreign namespaces.
		{"namespace kube-system, mixed case", ns, func(p *Params) { p.Namespace = "Kube-System" }},
		{"namespace kube-system", ns, func(p *Params) { p.Namespace = "kube-system" }},
		{"namespace kube-public", ns, func(p *Params) { p.Namespace = "kube-public" }},
		{"namespace default", ns, func(p *Params) { p.Namespace = "default" }},
		{"namespace plane", ns, func(p *Params) { p.Namespace = "plane" }},
		{"namespace app-a-dev", ns, func(p *Params) { p.Namespace = "app-a-dev" }},
		{"namespace flux-system", ns, func(p *Params) { p.Namespace = "flux-system" }},
		{"namespace local-path-storage", ns, func(p *Params) { p.Namespace = "local-path-storage" }},
		{"namespace traefik", ns, func(p *Params) { p.Namespace = "traefik" }},
		{"namespace kyverno", ns, func(p *Params) { p.Namespace = "kyverno" }},
		{"namespace observability", ns, func(p *Params) { p.Namespace = "observability" }},
		// Not DNS-1123 labels.
		{"namespace of 64 characters", ns, func(p *Params) { p.Namespace = "n" + strings.Repeat("a", 63) }},
		{"namespace empty", ns, func(p *Params) { p.Namespace = "" }},
		{"namespace with underscore", ns, func(p *Params) { p.Namespace = "nexus_bench" }},
		{"namespace with upper case", ns, func(p *Params) { p.Namespace = "Nexus-Bench" }},
		{"namespace starting with a dash", ns, func(p *Params) { p.Namespace = "-nexus" }},
		{"namespace ending with a dash", ns, func(p *Params) { p.Namespace = "nexus-" }},
		{"namespace with a dot", ns, func(p *Params) { p.Namespace = "nexus.bench" }},
		{"namespace with a newline", ns, func(p *Params) { p.Namespace = "nexus\nbench" }},
		// Context.
		{"context with a path traversal", ctx, func(p *Params) { p.Context = "a/../b" }},
		{"context starting with a dash", ctx, func(p *Params) { p.Context = "-x" }},
		{"context with a space", ctx, func(p *Params) { p.Context = "a b" }},
		{"context empty", ctx, func(p *Params) { p.Context = "" }},
		{"context of 254 characters", ctx, func(p *Params) { p.Context = strings.Repeat("a", 254) }},
		// NodePorts.
		{"HTTP NodePort below the range", httpNP, func(p *Params) { p.HTTPNodePort = 29999 }},
		{"HTTP NodePort above the range", httpNP, func(p *Params) { p.HTTPNodePort = 32768 }},
		{"Docker NodePort below the range", dockNP, func(p *Params) { p.DockerNodePort = 29999 }},
		{"Docker NodePort above the range", dockNP, func(p *Params) { p.DockerNodePort = 32768 }},
		{"NodePort zero", httpNP, func(p *Params) { p.HTTPNodePort = 0 }},
		{"two NodePorts equal", httpNP, func(p *Params) { p.DockerNodePort = p.HTTPNodePort }},
		{"two NodePorts equal, other name", dockNP, func(p *Params) { p.DockerNodePort = p.HTTPNodePort }},
		// Quantities.
		{"memory limit 4G!", memLim, func(p *Params) { p.MemoryLimit = "4G!" }},
		{"memory request with a space", memReq, func(p *Params) { p.MemoryRequest = "3 Gi" }},
		{"CPU limit with a letter suffix that is not a unit", cpuLim, func(p *Params) { p.CPULimit = "2cores" }},
		{"storage size with a newline", storage, func(p *Params) { p.StorageSize = "5Gi\nfoo: bar" }},
		{"CPU request zero", cpuReq, func(p *Params) { p.CPURequest = "0" }},
		{"CPU request zero milli", cpuReq, func(p *Params) { p.CPURequest = "0m" }},
		{"CPU limit zero decimal", cpuLim, func(p *Params) { p.CPULimit = "0.0" }},
		{"memory request zero", memReq, func(p *Params) { p.MemoryRequest = "0" }},
		{"memory limit zero Gi", memLim, func(p *Params) { p.MemoryLimit = "0Gi" }},
		{"storage size zero", storage, func(p *Params) { p.StorageSize = "0" }},
		{"storage size zero Gi", storage, func(p *Params) { p.StorageSize = "0Gi" }},
		// A request above its limit: Kubernetes would reject the pod.
		{"CPU request above the limit", cpuReq, func(p *Params) { p.CPURequest = "4" }},
		{"CPU request above the limit, milli", cpuLim, func(p *Params) { p.CPURequest = "2001m" }},
		{"memory limit below the default request", memReq, func(p *Params) { p.MemoryLimit = "2Gi" }},
		{"memory request above the limit, mixed units", memLim, func(p *Params) { p.MemoryRequest = "4097Mi" }},
		// JVM options: the value must be a safe plain YAML scalar by itself.
		{"JVM options empty", jvm, func(p *Params) { p.JVMOpts = "" }},
		{"JVM options with a newline", jvm, func(p *Params) { p.JVMOpts = "-Xms1g\nfoo: bar" }},
		{"JVM options with a tab", jvm, func(p *Params) { p.JVMOpts = "-Xms1g\t-Xmx1g" }},
		{"JVM options with a mapping key", jvm, func(p *Params) { p.JVMOpts = "a: b" }},
		{"JVM options ending with a colon", jvm, func(p *Params) { p.JVMOpts = "a:" }},
		{"JVM options starting with a sequence entry", jvm, func(p *Params) { p.JVMOpts = "- x" }},
		{"JVM options starting with a lone dash", jvm, func(p *Params) { p.JVMOpts = "-" }},
		{"JVM options starting with an alias-like at sign", jvm, func(p *Params) { p.JVMOpts = "@x" }},
		{"JVM options starting with a directive percent", jvm, func(p *Params) { p.JVMOpts = "%x" }},
		{"JVM options starting with a comma", jvm, func(p *Params) { p.JVMOpts = ",x" }},
		{"JVM options starting with a colon", jvm, func(p *Params) { p.JVMOpts = ":x" }},
		{"JVM options starting with a space", jvm, func(p *Params) { p.JVMOpts = " -Xms1g" }},
		{"JVM options with a non-ASCII letter", jvm, func(p *Params) { p.JVMOpts = "-Dname=\u00e9" }},
		{"JVM options with a command substitution", jvm, func(p *Params) { p.JVMOpts = "$(id)" }},
		{"JVM options with a comment", jvm, func(p *Params) { p.JVMOpts = "-Xms1g #x" }},
		{"JVM options too long", jvm, func(p *Params) { p.JVMOpts = "-D" + strings.Repeat("a", 511) }},
		// Image.
		{"image tag with a newline", tag, func(p *Params) { p.ImageTag = "1\nfoo" }},
		{"image tag empty", tag, func(p *Params) { p.ImageTag = "" }},
		{"image with a newline", image, func(p *Params) { p.Image = "docker.io/x\nfoo" }},
		{"image in the zot registry", image, func(p *Params) { p.Image = "tvp-zot.incus:5000/x" }},
		// Upstream URL.
		{"upstream ftp scheme", upURL, func(p *Params) { p.UpstreamURL = "ftp://x" }},
		{"upstream without scheme", upURL, func(p *Params) { p.UpstreamURL = "bench-upstream:5000" }},
		{"upstream with a newline", upURL, func(p *Params) { p.UpstreamURL = "http://a:5000\nfoo" }},
		{"upstream with credentials", upURL, func(p *Params) { p.UpstreamURL = "http://u@h:5000" }},
		{"upstream without a host", upURL, func(p *Params) { p.UpstreamURL = "http://:5000" }},
		{"upstream with an empty port", upURL, func(p *Params) { p.UpstreamURL = "http://a:" }},
		{"upstream with port zero", upURL, func(p *Params) { p.UpstreamURL = "http://a:0" }},
		{"upstream with a port above 65535", upURL, func(p *Params) { p.UpstreamURL = "http://a:99999" }},
		// Hosts.
		{"host with a newline", hosts, func(p *Params) { p.Hosts = []string{"a\nb"} }},
		{"host with a port", hosts, func(p *Params) { p.Hosts = []string{"a:b"} }},
		{"host of 254 characters", hosts, func(p *Params) { p.Hosts = []string{strings.Repeat("a", 254)} }},
		{"second host invalid", hosts, func(p *Params) { p.Hosts = []string{"127.0.0.1", "a b"} }},
		// Paths.
		{"secrets path with a newline", secrets, func(p *Params) { p.SecretsPath = "/a\nb" }},
		{"descriptor path with a newline", descr, func(p *Params) { p.DescriptorPath = "/a\nb" }},
		{"secrets path too long", secrets, func(p *Params) { p.SecretsPath = "/" + strings.Repeat("a", 4096) }},
		// Timeouts.
		{"timeout zero", timeout, func(p *Params) { p.Timeout = 0 }},
		{"delete timeout negative", delTime, func(p *Params) { p.DeleteTimeout = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			tt.mutate(&p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil for %+v, want an error", p)
			}
			if !strings.Contains(err.Error(), tt.variable) {
				t.Errorf("error %q does not name %s", err, tt.variable)
			}
		})
	}
}

// A request above its limit is reported with both variable names, so the user
// sees which pair to fix; the defaults are the trap (3Gi request against a
// lone NEXUS_BENCH_MEMORY_LIMIT=2Gi).
func TestRequestAboveLimitNamesBothVariables(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"memory limit alone below the default request",
			map[string]string{"NEXUS_BENCH_MEMORY_LIMIT": "2Gi"},
			[]string{"NEXUS_BENCH_MEMORY_REQUEST", "NEXUS_BENCH_MEMORY_LIMIT"}},
		{"CPU request 4 against limit 2",
			map[string]string{"NEXUS_BENCH_CPU_REQUEST": "4"},
			[]string{"NEXUS_BENCH_CPU_REQUEST", "NEXUS_BENCH_CPU_LIMIT"}},
		{"CPU request in milli-cores above a whole-core limit",
			map[string]string{"NEXUS_BENCH_CPU_REQUEST": "2500m"},
			[]string{"NEXUS_BENCH_CPU_REQUEST", "NEXUS_BENCH_CPU_LIMIT"}},
		{"memory request in Mi above a Gi limit",
			map[string]string{"NEXUS_BENCH_MEMORY_REQUEST": "5000Mi"},
			[]string{"NEXUS_BENCH_MEMORY_REQUEST", "NEXUS_BENCH_MEMORY_LIMIT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolve(tt.env)
			if err == nil {
				t.Fatalf("resolve(%v) error = nil, want an error", tt.env)
			}
			for _, name := range tt.want {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q does not name %s", err, name)
				}
			}
		})
	}
}

// The same refusals, reached through the environment: whichever of FromEnv or
// Validate reports it, the user gets an error.
func TestEnvRefusals(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"namespace Kube-System", map[string]string{"NEXUS_BENCH_NAMESPACE": "Kube-System"}},
		{"namespace default", map[string]string{"NEXUS_BENCH_NAMESPACE": "default"}},
		{"namespace plane", map[string]string{"NEXUS_BENCH_NAMESPACE": "plane"}},
		{"namespace app-a-dev", map[string]string{"NEXUS_BENCH_NAMESPACE": "app-a-dev"}},
		{"namespace of 64 characters", map[string]string{"NEXUS_BENCH_NAMESPACE": "n" + strings.Repeat("a", 63)}},
		{"HTTP NodePort 29999", map[string]string{"NEXUS_BENCH_HTTP_NODEPORT": "29999"}},
		{"HTTP NodePort 32768", map[string]string{"NEXUS_BENCH_HTTP_NODEPORT": "32768"}},
		{"both NodePorts equal", map[string]string{
			"NEXUS_BENCH_HTTP_NODEPORT":   "30490",
			"NEXUS_BENCH_DOCKER_NODEPORT": "30490",
		}},
		{"memory limit 4G!", map[string]string{"NEXUS_BENCH_MEMORY_LIMIT": "4G!"}},
		{"upstream ftp", map[string]string{"NEXUS_BENCH_UPSTREAM_URL": "ftp://x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolve(tt.env); err == nil {
				t.Errorf("resolve(%v) error = nil, want an error", tt.env)
			}
		})
	}
}

// The zot image is refused with the reason, so the user learns about the
// Kyverno policy instead of meeting an admission error later.
func TestEnvImageInZotMentionsKyverno(t *testing.T) {
	_, err := resolve(map[string]string{"NEXUS_BENCH_IMAGE": "tvp-zot.incus:5000/x"})
	if err == nil {
		t.Fatal("resolve(NEXUS_BENCH_IMAGE=tvp-zot.incus:5000/x) error = nil, want an error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "kyverno") {
		t.Errorf("error %q does not mention the Kyverno policy", err)
	}
}

func TestValidateAccepts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *Params)
	}{
		{"defaults", func(p *Params) {}},
		{"namespace nexus-bench-2", func(p *Params) { p.Namespace = "nexus-bench-2" }},
		{"namespace of 63 characters", func(p *Params) { p.Namespace = "n" + strings.Repeat("a", 62) }},
		{"namespace with digits first", func(p *Params) { p.Namespace = "2nd-bench" }},
		{"lowest NodePort", func(p *Params) { p.HTTPNodePort = 30000 }},
		{"highest NodePort", func(p *Params) { p.DockerNodePort = 32767 }},
		{"CPU in milli-cores", func(p *Params) { p.CPURequest = "250m" }},
		{"CPU as a decimal", func(p *Params) { p.CPULimit = "1.5" }},
		{"memory in Mi", func(p *Params) { p.MemoryRequest = "512Mi" }},
		{"memory in decimal G", func(p *Params) { p.MemoryLimit = "4G" }},
		{"CPU request equals the limit", func(p *Params) { p.CPURequest = "2" }},
		{"CPU request equals the limit in milli-cores", func(p *Params) { p.CPURequest = "2000m" }},
		{"memory request equals the limit", func(p *Params) { p.MemoryRequest = "4Gi" }},
		{"memory request equals the limit in Mi", func(p *Params) { p.MemoryRequest = "4096Mi" }},
		{"JVM options starting with a letter", func(p *Params) { p.JVMOpts = "Xmx1g" }},
		{"JVM options starting with a digit", func(p *Params) { p.JVMOpts = "1g" }},
		{"JVM options with a colon inside a word", func(p *Params) { p.JVMOpts = "-Dfoo=a:b" }},
		{"upstream at the highest port", func(p *Params) { p.UpstreamURL = "http://a:65535" }},
		{"upstream without a port", func(p *Params) { p.UpstreamURL = "https://upstream.example.org" }},
		{"hosts, IPv4, IPv6 and a name", func(p *Params) { p.Hosts = []string{"127.0.0.1", "::1", "node-1.example.org"} }},
		{"https upstream", func(p *Params) { p.UpstreamURL = "https://upstream.example.org:5000" }},
		{"other image on Docker Hub", func(p *Params) { p.Image = "docker.io/sonatype/nexus3-custom" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			tt.mutate(&p)
			if err := p.Validate(); err != nil {
				t.Errorf("Validate() = %v for %+v, want nil", err, p)
			}
		})
	}
}

// A valid namespace other than the default is accepted through the environment
// and the upstream URL follows it.
func TestEnvNonDefaultNamespaceDerivesUpstream(t *testing.T) {
	got, err := resolve(map[string]string{"NEXUS_BENCH_NAMESPACE": "nexus-bench-2"})
	if err != nil {
		t.Fatalf("resolve(NEXUS_BENCH_NAMESPACE=nexus-bench-2) error = %v, want nil", err)
	}
	if got.Namespace != "nexus-bench-2" {
		t.Errorf("Namespace = %q, want nexus-bench-2", got.Namespace)
	}
	want := "http://bench-upstream.nexus-bench-2.svc.cluster.local:5000"
	if got.UpstreamURL != want {
		t.Errorf("UpstreamURL = %q, want %q", got.UpstreamURL, want)
	}
}
