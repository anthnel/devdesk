package nexusbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The tests of the schema 2 descriptor (DEVDESK-505, NX-02a, step 8; Plane
// DEVDESK-543). Decision D-M of the plan:
//
//	BuildDescriptor(p Params, d DeployResult, r ProvisionResult, s SeedResult) Descriptor
//
//   - schemaVersion is 2 and LoadDescriptor accepts nothing else: a schema 1 file
//     is a *BenchError whose Check says to run `mise run nexus-bench:up`;
//   - three sections follow "accounts", in this order: registryAccess (scheme
//     "http", tlsVerify false, routing "path"), upstream (service, image, url,
//     persistent true, exposure "port-forward") and dataset (one entry per image
//     of the SeedResult, in its order: name, tag, digest, location, cachedInProxy);
//   - cachedInProxy is DatasetImage.PullThrough: a seeded image is cached in
//     docker-proxy exactly when Seed pulled it through;
//   - the dataset comes from the SeedResult, not from Dataset(): an empty result
//     lists no image, and the section is then "[]", never null;
//   - ProxyPending false (the nominal case of NX-02) lists docker-proxy and makes
//     it a member of docker-group;
//   - no secret, no timestamp, no credential in a URL.
//
// The golden testdata/descriptor.example.json is v1 until the code lands: the
// golden tests of descriptor_test.go are red for that reason, and the coder
// regenerates it with `-run TestDescriptorGolden -update` and reviews the diff.

// descriptorV1 is what a bench of NX-01 wrote: the committed descriptor.json
// before NX-02.
const descriptorV1 = `{
  "schemaVersion": 1,
  "kubeContext": "datomir",
  "namespace": "nexus-bench",
  "image": "docker.io/sonatype/nexus3:3.98.1",
  "nexusVersion": "3.98.1-01",
  "edition": "COMMUNITY",
  "eulaAccepted": true,
  "hosts": ["127.0.0.1"],
  "rest": {"nodePort": 30481, "containerPort": 8081, "urls": ["http://127.0.0.1:30481"]},
  "dockerConnectors": [],
  "repositories": [{"name": "docker-hosted", "type": "hosted"}],
  "realms": ["NexusAuthenticatingRealm"],
  "accounts": []
}
`

// rawKeys returns the keys of a JSON object, in file order.
func rawKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	return topLevelKeys(t, raw)
}

func writtenDescriptor(t *testing.T, d Descriptor) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "descriptor.json")
	if err := WriteDescriptor(path, d); err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDescriptorV2SectionsAndFieldOrder(t *testing.T) {
	data := writtenDescriptor(t, sampleDescriptor())
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		section string
		want    []string
	}{
		{"registryAccess", []string{"scheme", "tlsVerify", "routing"}},
		{"upstream", []string{"service", "image", "url", "persistent", "exposure"}},
	} {
		raw, ok := top[tt.section]
		if !ok {
			t.Errorf("the descriptor has no %q section", tt.section)
			continue
		}
		if got := rawKeys(t, raw); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s keys = %v, want %v", tt.section, got, tt.want)
		}
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(top["dataset"], &entries); err != nil || len(entries) == 0 {
		t.Fatalf("dataset = %s, want a list of images (%v)", top["dataset"], err)
	}
	for i, e := range entries {
		if got, want := rawKeys(t, e), []string{"name", "tag", "digest", "location", "cachedInProxy"}; !reflect.DeepEqual(got, want) {
			t.Errorf("dataset[%d] keys = %v, want %v", i, got, want)
		}
	}

	// A false and a true that are written, not left out.
	text := string(data)
	for _, want := range []string{`"scheme": "http"`, `"tlsVerify": false`, `"routing": "path"`, `"persistent": true`, `"exposure": "port-forward"`, `"service": "bench-upstream"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the descriptor lacks %s:\n%s", want, text)
		}
	}
}

func TestBuildDescriptorDatasetFollowsTheSeedResult(t *testing.T) {
	t.Run("the whole data set, in order, with the cache state", func(t *testing.T) {
		d := sampleDescriptor()
		if want := wantDescriptorDataset(Dataset()); !reflect.DeepEqual(d.Dataset, want) {
			t.Fatalf("dataset =\n%+v\nwant\n%+v", d.Dataset, want)
		}
		cached := 0
		for _, img := range d.Dataset {
			if img.CachedInProxy {
				cached++
				if img.Name != "mirror/cached" {
					t.Errorf("%s:%s is marked cached in the proxy", img.Name, img.Tag)
				}
			}
			if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(img.Digest) {
				t.Errorf("%s:%s digest = %q, want sha256:<64 hex>", img.Name, img.Tag, img.Digest)
			}
		}
		if cached != 1 {
			t.Errorf("%d images are cached in the proxy, want 1", cached)
		}
	})

	t.Run("what the seed result says, not Dataset()", func(t *testing.T) {
		images := []DatasetImage{
			{Name: "z/last", Tag: "9", Location: LocationUpstream, PullThrough: true, Digest: digestAlpine},
			{Name: "a/first", Tag: "1", Location: LocationHosted, Digest: digestBusy},
		}
		d := BuildDescriptor(Defaults(), sampleDeploy(), sampleProvision(), SeedResult{Images: images})
		want := []DescriptorImage{
			{Name: "z/last", Tag: "9", Digest: digestAlpine, Location: "upstream", CachedInProxy: true},
			{Name: "a/first", Tag: "1", Digest: digestBusy, Location: "docker-hosted", CachedInProxy: false},
		}
		if !reflect.DeepEqual(d.Dataset, want) {
			t.Errorf("dataset = %+v, want %+v", d.Dataset, want)
		}
	})

	t.Run("an empty result lists no image, and writes [] not null", func(t *testing.T) {
		d := BuildDescriptor(Defaults(), sampleDeploy(), sampleProvision(), SeedResult{})
		if d.Dataset == nil || len(d.Dataset) != 0 {
			t.Errorf("dataset = %#v, want an empty, non-nil list", d.Dataset)
		}
		if text := string(writtenDescriptor(t, d)); !strings.Contains(text, `"dataset": []`) {
			t.Errorf("the empty dataset is not written as []:\n%s", text)
		}
	})

	t.Run("the descriptor does not share the slice of the result", func(t *testing.T) {
		s := sampleSeed()
		d := BuildDescriptor(Defaults(), sampleDeploy(), sampleProvision(), s)
		if len(d.Dataset) == 0 {
			t.Fatal("the descriptor lists no image")
		}
		first := d.Dataset[0]
		s.Images[0].Name = "changed"
		s.Images[0].PullThrough = !s.Images[0].PullThrough
		if d.Dataset[0] != first {
			t.Errorf("dataset[0] = %+v follows the result, was %+v", d.Dataset[0], first)
		}
	})
}

func TestBuildDescriptorListsTheProxyOnceTheUpstreamResolves(t *testing.T) {
	r := sampleProvision()
	r.ProxyPending = false
	d := BuildDescriptor(Defaults(), sampleDeploy(), r, sampleSeed())
	want := []DescriptorRepository{
		{Name: "docker-hosted", Type: "hosted"},
		{Name: "docker-proxy", Type: "proxy", RemoteURL: "http://bench-upstream.nexus-bench.svc.cluster.local:5000"},
		{Name: "docker-group", Type: "group", Members: []string{"docker-hosted", "docker-proxy"}},
	}
	if !reflect.DeepEqual(d.Repositories, want) {
		t.Errorf("repositories = %+v, want %+v", d.Repositories, want)
	}
}

func TestBuildDescriptorUpstreamFollowsTheImageParameters(t *testing.T) {
	tests := []struct {
		name      string
		image     string
		tag       string
		wantImage string
	}{
		{"defaults", "docker.io/library/registry", "3.0.0", "docker.io/library/registry:3.0.0"},
		{"another tag", "docker.io/library/registry", "3.0.1", "docker.io/library/registry:3.0.1"},
		{"another image", "registry.example/mirror/registry", "2.8.3", "registry.example/mirror/registry:2.8.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			p.UpstreamImage, p.UpstreamImageTag = tt.image, tt.tag
			d := BuildDescriptor(p, sampleDeploy(), sampleProvision(), sampleSeed())
			if d.Upstream.Image != tt.wantImage || d.Upstream.Image != p.UpstreamImageRef() {
				t.Errorf("upstream image = %q, want %q", d.Upstream.Image, tt.wantImage)
			}
			if d.Upstream.Service != "bench-upstream" || !d.Upstream.Persistent || d.Upstream.Exposure != "port-forward" {
				t.Errorf("upstream = %+v, want service bench-upstream, persistent, port-forward", d.Upstream)
			}
			if d.Upstream.URL != p.UpstreamURL {
				t.Errorf("upstream url = %q, want %q", d.Upstream.URL, p.UpstreamURL)
			}
			if want := (RegistryAccess{Scheme: "http", TLSVerify: false, Routing: "path"}); d.RegistryAccess != want {
				t.Errorf("registryAccess = %+v, want %+v", d.RegistryAccess, want)
			}
		})
	}
}

func TestLoadDescriptorRefusesSchemaVersion1(t *testing.T) {
	good := string(mustMarshalIndent(t, sampleDescriptor()))
	tests := []struct {
		name    string
		content string
	}{
		{"a file written by NX-01", descriptorV1},
		{"a v2 body marked as v1", strings.Replace(good, `"schemaVersion": 2`, `"schemaVersion": 1`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "descriptor.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadDescriptor(path)
			if err == nil {
				t.Fatal("LoadDescriptor accepted a schema 1 file")
			}
			if errors.Is(err, ErrNoDescriptor) {
				t.Errorf("a schema 1 file is not an absent one: %v", err)
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error = %#v, want a *BenchError", err)
			}
			if !strings.Contains(be.Check, "nexus-bench:up") {
				t.Errorf("Check = %q, want it to say to run mise run nexus-bench:up", be.Check)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "schema") {
				t.Errorf("error %q does not mention the schema", err)
			}
		})
	}
}

func TestDescriptorV2HoldsNoSecretNoDateNoCredential(t *testing.T) {
	text := string(writtenDescriptor(t, sampleDescriptor()))
	lower := strings.ToLower(text)
	for _, banned := range []string{"password", "secret", "authfile", "credential", strings.ToLower(secretMarker)} {
		if strings.Contains(lower, banned) {
			t.Errorf("the descriptor mentions %q:\n%s", banned, text)
		}
	}
	for label, re := range map[string]*regexp.Regexp{
		"a date":                  regexp.MustCompile(`\d{4}-\d{2}-\d{2}`),
		"a time":                  regexp.MustCompile(`\d{2}:\d{2}:\d{2}`),
		"credentials in a URL":    regexp.MustCompile(`://[^/"\s]*@`),
		"a temporary folder path": regexp.MustCompile(`devdesk-(skopeo|seed)-|/tmp/`),
	} {
		if m := re.FindString(text); m != "" {
			t.Errorf("the descriptor holds %s (%q)", label, m)
		}
	}
}
