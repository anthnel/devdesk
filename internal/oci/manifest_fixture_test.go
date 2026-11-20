package oci_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-580 (NX-08b, PR3, NX-04): the HEAD of a manifest through docker-proxy
// ("is this image pullable"), and the tag list, replayed from the answers MEASURED
// on Nexus 3.98.1-01 with the upstream reachable and then cut. A measurement that
// contradicts a criterion or the code is pinned as measured, never fixed here (see
// docs/measurements/2026-11-20-nexus-cached-only.md).

// cachedOnlyFake replays the named fixtures of the cachedonly campaign. A name
// that is not embedded fails the test, so a removed fixture cannot leave it vacuous.
func cachedOnlyFake(t *testing.T, names ...string) (*fakenexus.Server, []nexusfixture.Fixture) {
	t.Helper()
	all, err := nexusfixture.Load("cachedonly")
	if err != nil {
		t.Fatalf("Load(cachedonly): %v", err)
	}
	var picked []nexusfixture.Fixture
	for _, n := range names {
		found := false
		for _, f := range all {
			if f.Name == n {
				picked = append(picked, f)
				found = true
			}
		}
		if !found {
			t.Fatalf("the cachedonly campaign has no fixture %q", n)
		}
	}
	return fakenexus.New(t, picked...), picked
}

// benchDigest is the digest the bench descriptor gives an upstream image: the
// dataset is what the bench seeds, so a digest served by Nexus must be this one.
func benchDigest(t *testing.T, name, tag string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash("../nexusbench/descriptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Dataset []struct {
			Name, Tag, Digest, Location string
		} `json:"dataset"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Dataset {
		if e.Name == name && e.Tag == tag && e.Location == "upstream" {
			return e.Digest
		}
	}
	t.Fatalf("the bench descriptor has no upstream image %s:%s", name, tag)
	return ""
}

// wantExchange pins the recorded request and status of a one-exchange fixture (a
// fixture regenerated with another status fails here before any replay hides it).
func wantExchange(t *testing.T, f nexusfixture.Fixture, target string, status int) nexusfixture.Exchange {
	t.Helper()
	if len(f.Exchanges) != 1 {
		t.Fatalf("%s: %d exchanges, want 1", f.Name, len(f.Exchanges))
	}
	ex := f.Exchanges[0]
	if ex.Target != target || ex.Status != status {
		t.Fatalf("%s recorded %s %s -> %d, this test was written for %s -> %d: the measurement changed",
			f.Name, ex.Method, ex.Target, ex.Status, target, status)
	}
	return ex
}

func TestManifestDigestOnMeasuredCutUpstream(t *testing.T) {
	user, pass := fakenexus.User(nexusfixture.Admin), fakenexus.Password

	cases := []struct {
		name    string
		fixture string
		repo    string
		target  string
		status  int
		// digestOf is the dataset image whose digest must come back ("" = an error).
		digestOf string
		effect   string // the "effect:" header of the fixture
	}{
		{
			// Nominal, the upstream reachable: the HEAD of an image the proxy did not
			// hold is answered (it fetched it) with the digest of the upstream, and
			// that read cached it (reported by the session of 2026-11-20, not replayable by a
			// fixture: the bench said dataset: differs after it).
			name: "nominal, uncached image", fixture: "nominal-zz-head-proxy-mirror-uncached--admin",
			repo: "docker-proxy/mirror/uncached", target: "/v2/docker-proxy/mirror/uncached/manifests/1.0", status: 200,
			digestOf: "mirror/uncached", effect: nexusfixture.EffectCaches,
		},
		{
			// Upstream cut, proxy auto-blocked: an image in the cache is still served.
			name: "cut, cached image", fixture: "cut2-head-proxy-mirror-cached--admin",
			repo: "docker-proxy/mirror/cached", target: "/v2/docker-proxy/mirror/cached/manifests/1.0", status: 200,
			digestOf: "mirror/cached", effect: nexusfixture.EffectCaches,
		},
		{
			// Upstream cut: nothing to serve. A 404 with an empty body (a HEAD has none):
			// DevDesk gets no typed cause, only "registry returned 404 for <repo>:<tag>".
			name: "cut, uncached image", fixture: "cut2-head-proxy-mirror-uncached--admin",
			repo: "docker-proxy/mirror/uncached", target: "/v2/docker-proxy/mirror/uncached/manifests/1.0", status: 404,
			effect: nexusfixture.EffectCaches,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fixtures := cachedOnlyFake(t, tc.fixture)
			ex := wantExchange(t, fixtures[0], tc.target, tc.status)
			if ex.Method != "HEAD" {
				t.Fatalf("%s is a %s, want HEAD", tc.fixture, ex.Method)
			}
			if fixtures[0].Meta.Effect != tc.effect {
				t.Fatalf("%s: effect %q, want %q", tc.fixture, fixtures[0].Meta.Effect, tc.effect)
			}

			digest, err := oci.ManifestDigest(s.URL("127.0.0.1:30481"), tc.repo, "1.0", user, pass)

			if tc.digestOf != "" {
				want := benchDigest(t, tc.digestOf, "1.0")
				if err != nil || digest != want {
					t.Fatalf("ManifestDigest = %q, %v, want %q: the digest of the dataset image %s", digest, err, want, tc.digestOf)
				}
				if got := ex.Header.Get("Docker-Content-Digest"); got != want {
					t.Errorf("the fixture says %q, the dataset %q", got, want)
				}
			} else {
				if err == nil || digest != "" {
					t.Fatalf("ManifestDigest = %q, %v, want an error", digest, err)
				}
				// Measured behavior, not a wish: the HEAD path returns an untyped
				// error, so NX-04's criterion "typed cause" holds on the catalog and
				// tags path only. Nobody can errors.As a status out of it today.
				var se *oci.StatusError
				if errors.As(err, &se) {
					t.Errorf("err is now a *oci.StatusError (%v): the HEAD path became typed, update the measurement note", err)
				}
				if want := "registry returned 404 for " + tc.repo + ":1.0"; err.Error() != want {
					t.Errorf("err = %q, want %q", err, want)
				}
			}
			if h := s.Hits(tc.fixture); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", tc.fixture, h)
			}
		})
	}
}

// ListRegistryTagsContext on the measured tag lists: what the Tags level of the
// registry browser reads. With the upstream cut and the proxy blocked, the proxy
// refuses the tag list of an image it holds (404, a typed status) while the group
// answers 200 with an EMPTY list for the same image (a silent omission, no error).
func TestTagsOnMeasuredCutUpstream(t *testing.T) {
	user, pass := fakenexus.User(nexusfixture.Admin), fakenexus.Password
	const proxyHost, groupHost = "127.0.0.1:30481", "127.0.0.1:30482"

	cases := []struct {
		name    string
		fixture string
		host    string
		repo    string
		target  string
		status  int
		tags    []string
		code    int // the StatusError Code expected, 0 = none
	}{
		{"nominal, cached image", "nominal-tags-proxy-mirror-cached--admin", proxyHost, "docker-proxy/mirror/cached",
			"/v2/docker-proxy/mirror/cached/tags/list", 200, []string{"1.0"}, 0},
		// The tag list of an image the proxy holds nothing of is served from the
		// upstream: listed, although not cached.
		{"nominal, uncached image", "nominal-tags-proxy-mirror-uncached--admin", proxyHost, "docker-proxy/mirror/uncached",
			"/v2/docker-proxy/mirror/uncached/tags/list", 200, []string{"1.0"}, 0},
		{"cut, cached image, proxy", "cut2-tags-proxy-mirror-cached--admin", proxyHost, "docker-proxy/mirror/cached",
			"/v2/docker-proxy/mirror/cached/tags/list", 404, nil, 404},
		{"cut, uncached image, proxy", "cut2-tags-proxy-mirror-uncached--admin", proxyHost, "docker-proxy/mirror/uncached",
			"/v2/docker-proxy/mirror/uncached/tags/list", 404, nil, 404},
		{"cut, cached image, group", "cut2-tags-group-mirror-cached--admin", groupHost, "mirror/cached",
			"/v2/mirror/cached/tags/list", 200, []string{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fixtures := cachedOnlyFake(t, tc.fixture)
			wantExchange(t, fixtures[0], tc.target, tc.status)

			tags, err := oci.ListRegistryTagsContext(context.Background(), s.URL(tc.host), tc.repo, user, pass)

			if tc.code == 0 {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if len(tags) != len(tc.tags) || (len(tags) > 0 && tags[0] != tc.tags[0]) {
					t.Errorf("tags = %v, want %v", tags, tc.tags)
				}
				return
			}
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Code != tc.code || se.Source != oci.SourceRegistry {
				t.Errorf("err = %v (%T), want *oci.StatusError{%d, SourceRegistry}", err, err, tc.code)
			}
			if len(tags) != 0 {
				t.Errorf("tags = %v, want none", tags)
			}
		})
	}
}
