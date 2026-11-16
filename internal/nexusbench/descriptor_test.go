package nexusbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// goldenDescriptor is the descriptor of the default bench with the sample
// results below. Like the render golden it is reviewed by hand:
//
//	go test ./internal/nexusbench -run TestDescriptorGolden -update
const goldenDescriptor = "testdata/descriptor.example.json"

const (
	sampleNodeIP       = "192.168.1.31"
	sampleNexusVersion = "3.98.1-01"
)

func sampleDeploy() DeployResult {
	return DeployResult{ReachableHosts: []string{"127.0.0.1", sampleNodeIP}, NodeIP: sampleNodeIP}
}

func sampleProvision() ProvisionResult {
	return ProvisionResult{NexusVersion: sampleNexusVersion, Edition: "COMMUNITY", EULAAccepted: true}
}

func sampleDescriptor() Descriptor {
	return BuildDescriptor(Defaults(), sampleDeploy(), sampleProvision())
}

func boolPtr(b bool) *bool { return &b }

// findAccount returns the account of a user, or fails the test.
func findAccount(t *testing.T, d Descriptor, userID string) DescriptorAccount {
	t.Helper()
	for _, a := range d.Accounts {
		if a.UserID == userID {
			return a
		}
	}
	t.Fatalf("no account %q in %+v", userID, d.Accounts)
	return DescriptorAccount{}
}

func TestBuildDescriptorDefaults(t *testing.T) {
	d := sampleDescriptor()

	want := Descriptor{
		SchemaVersion: 1,
		KubeContext:   "datomir",
		Namespace:     "nexus-bench",
		Image:         "docker.io/sonatype/nexus3:3.98.1",
		NexusVersion:  sampleNexusVersion,
		Edition:       "COMMUNITY",
		EULAAccepted:  true,
		Hosts:         []string{"127.0.0.1", sampleNodeIP},
		REST: Endpoint{
			NodePort: 30481, ContainerPort: 8081,
			URLs: []string{"http://127.0.0.1:30481", "http://" + sampleNodeIP + ":30481"},
		},
		DockerConnectors: []DockerConnector{{
			Repository: "docker-group", NodePort: 30482, ContainerPort: 8082,
			URLs: []string{"http://127.0.0.1:30482", "http://" + sampleNodeIP + ":30482"},
		}},
		Repositories: []DescriptorRepository{
			{Name: "docker-hosted", Type: "hosted"},
			{Name: "docker-proxy", Type: "proxy", RemoteURL: "http://bench-upstream.nexus-bench.svc.cluster.local:5000"},
			{Name: "docker-group", Type: "group", Members: []string{"docker-hosted", "docker-proxy"}},
		},
		Realms: []string{"NexusAuthenticatingRealm", "DockerToken"},
		Accounts: []DescriptorAccount{
			{UserID: "admin", Roles: []string{"nx-admin"}, Purpose: "bench administrator"},
			{
				UserID: "bench-pull", Roles: []string{"bench-docker-pull"},
				Privileges: []string{"nx-repository-view-docker-*-browse", "nx-repository-view-docker-*-read"},
				Purpose:    "read-only pull",
			},
			{UserID: "anonymous", Enabled: boolPtr(true), Roles: []string{"nx-anonymous"}, Purpose: "anonymous access"},
		},
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("BuildDescriptor(Defaults(), ...) =\n%+v\nwant\n%+v", d, want)
	}
}

func TestBuildDescriptorFollowsParams(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(p *Params)
		deploy    DeployResult
		provision ProvisionResult
		wantHosts []string
		check     func(t *testing.T, d Descriptor)
	}{
		{
			name:      "explicit Hosts replace the discovered ones",
			mutate:    func(p *Params) { p.Hosts = []string{"10.0.0.5", "bench.home.arpa"} },
			deploy:    sampleDeploy(),
			provision: sampleProvision(),
			wantHosts: []string{"10.0.0.5", "bench.home.arpa"},
			check: func(t *testing.T, d Descriptor) {
				wantURLs := []string{"http://10.0.0.5:30481", "http://bench.home.arpa:30481"}
				if !reflect.DeepEqual(d.REST.URLs, wantURLs) {
					t.Errorf("rest urls = %v, want %v", d.REST.URLs, wantURLs)
				}
			},
		},
		{
			name:      "no node IP leaves loopback only",
			mutate:    func(p *Params) {},
			deploy:    DeployResult{ReachableHosts: []string{"127.0.0.1"}},
			provision: sampleProvision(),
			wantHosts: []string{"127.0.0.1"},
			check: func(t *testing.T, d Descriptor) {
				if want := []string{"http://127.0.0.1:30482"}; !reflect.DeepEqual(d.DockerConnectors[0].URLs, want) {
					t.Errorf("docker urls = %v, want %v", d.DockerConnectors[0].URLs, want)
				}
			},
		},
		{
			name: "custom ports, namespace, context, upstream and no anonymous access",
			mutate: func(p *Params) {
				p.Context = "other-ctx"
				p.Namespace = "bench-2"
				p.UpstreamURL = "http://upstream.example:5000"
				p.HTTPNodePort = 31000
				p.DockerNodePort = 31001
				p.Image = "registry.example/nexus"
				p.ImageTag = "3.99.0"
				p.Anonymous = false
			},
			deploy:    sampleDeploy(),
			provision: ProvisionResult{NexusVersion: "3.99.0-02", Edition: "PRO", EULAAccepted: false},
			wantHosts: []string{"127.0.0.1", sampleNodeIP},
			check: func(t *testing.T, d Descriptor) {
				if d.KubeContext != "other-ctx" || d.Namespace != "bench-2" {
					t.Errorf("context/namespace = %q/%q, want other-ctx/bench-2", d.KubeContext, d.Namespace)
				}
				if d.Image != "registry.example/nexus:3.99.0" {
					t.Errorf("image = %q, want registry.example/nexus:3.99.0", d.Image)
				}
				if d.NexusVersion != "3.99.0-02" || d.Edition != "PRO" || d.EULAAccepted {
					t.Errorf("provision results = %q/%q/%v, want 3.99.0-02/PRO/false", d.NexusVersion, d.Edition, d.EULAAccepted)
				}
				if d.REST.NodePort != 31000 || d.REST.ContainerPort != 8081 {
					t.Errorf("rest ports = %d/%d, want 31000/8081", d.REST.NodePort, d.REST.ContainerPort)
				}
				if got := d.DockerConnectors[0]; got.NodePort != 31001 || got.ContainerPort != 8082 {
					t.Errorf("docker connector ports = %d/%d, want 31001/8082", got.NodePort, got.ContainerPort)
				}
				if got := d.REST.URLs[0]; got != "http://127.0.0.1:31000" {
					t.Errorf("rest url = %q, want http://127.0.0.1:31000", got)
				}
				for _, r := range d.Repositories {
					if r.Type == "proxy" && r.RemoteURL != "http://upstream.example:5000" {
						t.Errorf("proxy remoteUrl = %q, want the custom upstream", r.RemoteURL)
					}
				}
				anon := findAccount(t, d, "anonymous")
				if anon.Enabled == nil || *anon.Enabled {
					t.Errorf("anonymous enabled = %v, want a present false", anon.Enabled)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			tt.mutate(&p)
			d := BuildDescriptor(p, tt.deploy, tt.provision)
			if d.SchemaVersion != 1 {
				t.Errorf("schemaVersion = %d, want 1", d.SchemaVersion)
			}
			if !reflect.DeepEqual(d.Hosts, tt.wantHosts) {
				t.Errorf("hosts = %v, want %v", d.Hosts, tt.wantHosts)
			}
			tt.check(t, d)
		})
	}
}

func TestBuildDescriptorDoesNotAliasParams(t *testing.T) {
	p := Defaults()
	p.Hosts = []string{"10.0.0.5"}
	d := BuildDescriptor(p, sampleDeploy(), sampleProvision())
	p.Hosts[0] = "changed"
	if d.Hosts[0] != "10.0.0.5" {
		t.Errorf("the descriptor shares its Hosts slice with Params: hosts = %v", d.Hosts)
	}
}

func TestDescriptorGolden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "descriptor.json")
	if err := WriteDescriptor(path, sampleDescriptor()); err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenDescriptor), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenDescriptor, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenDescriptor)
	if err != nil {
		t.Fatalf("%v\nrun: go test ./internal/nexusbench -run TestDescriptorGolden -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the written descriptor differs from %s (run with -update and review the diff)\n--- got ---\n%s\n--- want ---\n%s", goldenDescriptor, got, want)
	}
}

func TestLoadDescriptorGoldenMatchesBuild(t *testing.T) {
	got, err := LoadDescriptor(goldenDescriptor)
	if err != nil {
		t.Fatalf("LoadDescriptor(%s): %v", goldenDescriptor, err)
	}
	if want := sampleDescriptor(); !reflect.DeepEqual(got, want) {
		t.Errorf("LoadDescriptor(golden) =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDescriptorFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "descriptor.json")
	if err := WriteDescriptor(path, sampleDescriptor()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	t.Run("indented by two spaces, one final newline", func(t *testing.T) {
		if !strings.HasPrefix(text, "{\n  \"schemaVersion\": 1,\n") {
			t.Errorf("the file does not start with a two-space indented schemaVersion:\n%.80s", text)
		}
		if !strings.HasSuffix(text, "}\n") || strings.HasSuffix(text, "}\n\n") {
			t.Errorf("the file must end with a single newline, ends with %q", text[len(text)-3:])
		}
		if strings.Contains(text, "\t") {
			t.Error("the file holds a tab")
		}
	})

	t.Run("fixed top-level field order", func(t *testing.T) {
		want := []string{
			"schemaVersion", "kubeContext", "namespace", "image", "nexusVersion", "edition",
			"eulaAccepted", "hosts", "rest", "dockerConnectors", "repositories", "realms", "accounts",
		}
		if got := topLevelKeys(t, data); !reflect.DeepEqual(got, want) {
			t.Errorf("top-level keys = %v, want %v", got, want)
		}
	})

	t.Run("no timestamp", func(t *testing.T) {
		if m := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`).FindString(text); m != "" {
			t.Errorf("the descriptor holds a date (%q): it must be reproducible", m)
		}
	})

	t.Run("no pod name", func(t *testing.T) {
		if m := regexp.MustCompile(`nexus-[0-9a-f]{6,}-[a-z0-9]{5}`).FindString(text); m != "" {
			t.Errorf("the descriptor holds a pod name (%q)", m)
		}
	})
}

// topLevelKeys returns the keys of the outermost JSON object, in file order.
func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("the descriptor is not a JSON object: %v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skipped json.RawMessage
		if err := dec.Decode(&skipped); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func TestWriteDescriptorIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var first []byte
	for i, name := range []string{"a.json", "b.json", "a.json"} {
		path := filepath.Join(dir, name)
		if err := WriteDescriptor(path, sampleDescriptor()); err != nil {
			t.Fatalf("write #%d: %v", i+1, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = got
		} else if !bytes.Equal(first, got) {
			t.Errorf("write #%d (%s) differs from the first one:\n%s\n---\n%s", i+1, name, got, first)
		}
	}
}

func TestDescriptorRoundTrip(t *testing.T) {
	custom := Defaults()
	custom.Namespace = "bench-2"
	custom.UpstreamURL = "http://upstream.example:5000"
	custom.Anonymous = false
	custom.Hosts = []string{"10.0.0.5"}

	tests := []struct {
		name string
		d    Descriptor
	}{
		{"defaults", sampleDescriptor()},
		{"custom", BuildDescriptor(custom, sampleDeploy(), ProvisionResult{NexusVersion: "3.99.0-02", Edition: "PRO"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "descriptor.json")
			if err := WriteDescriptor(path, tt.d); err != nil {
				t.Fatalf("WriteDescriptor: %v", err)
			}
			got, err := LoadDescriptor(path)
			if err != nil {
				t.Fatalf("LoadDescriptor: %v", err)
			}
			if !reflect.DeepEqual(got, tt.d) {
				t.Errorf("Load(Write(d)) =\n%+v\nwant\n%+v", got, tt.d)
			}
		})
	}
}

func TestDescriptorHoldsNoSecret(t *testing.T) {
	// Every place a secret could come from carries the marker: the process
	// environment, the parameters that point at secrets, and the secrets themselves.
	for _, name := range []string{"NEXUS_ADMIN_PASSWORD", "NEXUS_BENCH_PASSWORD", "NEXUS_BENCH_TOKEN", "KUBECONFIG"} {
		t.Setenv(name, secretMarker)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))

	p := Defaults()
	p.SecretsPath = filepath.Join(t.TempDir(), secretMarker, "secrets.json")
	p.DescriptorPath = filepath.Join(t.TempDir(), "descriptor.json")
	p.JVMOpts = "-Dmarker=" + secretMarker

	store := newTestStore(t, p.SecretsPath)
	if err := store.Save(Secrets{AdminPassword: secretMarker, PendingAdminPassword: secretMarker, PullPassword: secretMarker}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	d := BuildDescriptor(p, sampleDeploy(), sampleProvision())
	if err := WriteDescriptor(p.DescriptorPath, d); err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	data, err := os.ReadFile(p.DescriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secretMarker)) {
		t.Errorf("the descriptor holds the secret marker:\n%s", data)
	}
	if strings.Contains(strings.ToLower(string(data)), "password") {
		t.Errorf("the descriptor mentions a password:\n%s", data)
	}
	if strings.Contains(string(data), p.SecretsPath) {
		t.Error("the descriptor holds the path of the secrets file")
	}
}

func TestLoadDescriptorErrors(t *testing.T) {
	t.Run("absent file is ErrNoDescriptor", func(t *testing.T) {
		_, err := LoadDescriptor(filepath.Join(t.TempDir(), "descriptor.json"))
		if !errors.Is(err, ErrNoDescriptor) {
			t.Errorf("LoadDescriptor on an absent file: err = %v, want ErrNoDescriptor", err)
		}
	})

	good := mustMarshalIndent(t, sampleDescriptor())
	tests := []struct {
		name    string
		content []byte
		want    string // lower-case fragment the error must hold
	}{
		{"schemaVersion 2", bytes.Replace(good, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`), 1), "schema"},
		{"schemaVersion 0", bytes.Replace(good, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 0`), 1), "schema"},
		{"schemaVersion missing", bytes.Replace(good, []byte("  \"schemaVersion\": 1,\n"), nil, 1), "schema"},
		{"not JSON", []byte("{" + secretMarker + " nope"), ""},
		{"empty file", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "descriptor.json")
			if err := os.WriteFile(path, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadDescriptor(path)
			if err == nil {
				t.Fatal("LoadDescriptor: want an error")
			}
			if errors.Is(err, ErrNoDescriptor) {
				t.Errorf("a present but invalid file must not be reported as absent: %v", err)
			}
			if tt.want != "" && !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Errorf("the error shows the file content: %v", err)
			}
		})
	}
}

// mustMarshalIndent is the format the descriptor is expected to have, built
// independently of WriteDescriptor so a schemaVersion edit can target it.
func mustMarshalIndent(t *testing.T, d Descriptor) []byte {
	t.Helper()
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestWriteDescriptorIsAtomic(t *testing.T) {
	t.Run("a target that cannot be replaced leaves nothing behind", func(t *testing.T) {
		// A non-empty directory: the final rename fails.
		dir := t.TempDir()
		target := filepath.Join(dir, "descriptor.json")
		if err := os.MkdirAll(filepath.Join(target, "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := WriteDescriptor(target, sampleDescriptor()); err == nil {
			t.Fatal("WriteDescriptor over a non-empty directory: want an error")
		}
		assertOnlyEntries(t, dir, "descriptor.json")
		assertOnlyEntries(t, target, "inner")
	})

	t.Run("an impossible path fails without creating anything", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteDescriptor(filepath.Join(blocker, "descriptor.json"), sampleDescriptor()); err == nil {
			t.Fatal("WriteDescriptor under a regular file: want an error")
		}
		assertOnlyEntries(t, dir, "blocker")
	})

	t.Run("a good write leaves only the descriptor", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 3; i++ {
			if err := WriteDescriptor(filepath.Join(dir, "descriptor.json"), sampleDescriptor()); err != nil {
				t.Fatal(err)
			}
		}
		assertOnlyEntries(t, dir, "descriptor.json")
	})
}

func TestDescriptorPath(t *testing.T) {
	t.Run("NEXUS_BENCH_DESCRIPTOR wins", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "elsewhere.json")
		t.Setenv("NEXUS_BENCH_DESCRIPTOR", want)
		if got := DescriptorPath(); got != want {
			t.Errorf("DescriptorPath() = %q, want %q", got, want)
		}
	})

	t.Run("an empty variable counts as unset", func(t *testing.T) {
		t.Setenv("NEXUS_BENCH_DESCRIPTOR", "")
		assertPackageDescriptorPath(t, DescriptorPath())
	})

	t.Run("default is the versioned file of the package, whatever the working directory", func(t *testing.T) {
		t.Setenv("NEXUS_BENCH_DESCRIPTOR", "")
		t.Chdir(t.TempDir())
		assertPackageDescriptorPath(t, DescriptorPath())
	})
}

// assertPackageDescriptorPath checks that got is internal/nexusbench/descriptor.json.
// It only compares paths: the versioned file itself is never written by a test.
func assertPackageDescriptorPath(t *testing.T, got string) {
	t.Helper()
	if !filepath.IsAbs(got) {
		t.Errorf("DescriptorPath() = %q, want an absolute path", got)
	}
	if filepath.Base(got) != "descriptor.json" {
		t.Errorf("DescriptorPath() = %q, want a file named descriptor.json", got)
	}
	if dir := filepath.ToSlash(filepath.Dir(got)); !strings.HasSuffix(dir, "/internal/nexusbench") {
		t.Errorf("DescriptorPath() is in %q, want the internal/nexusbench folder", dir)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(got), "doc.go")); err != nil {
		t.Errorf("DescriptorPath() folder does not hold the package sources: %v", err)
	}
}
