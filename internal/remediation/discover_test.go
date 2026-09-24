package remediation

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// registry answers with the given tags per repository and counts its requests.
type registry struct {
	tags  map[string][]string
	calls map[string]int
	err   error
}

func newRegistry(tags map[string][]string) *registry {
	return &registry{tags: tags, calls: map[string]int{}}
}

func (r *registry) list(ref Ref) ([]string, error) {
	r.calls[ref.Repository]++
	return r.tags[ref.Repository], r.err
}

func TestDiscoverFindsEachBaseImageAndItsCandidates(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Dockerfile": "FROM golang:1.21 AS build\nRUN go build\nFROM alpine:3.18\n",
	})
	reg := newRegistry(map[string][]string{
		"library/golang": {"1.20", "1.21", "1.22", "1.23"},
		"library/alpine": {"3.18", "3.19", "3.20"},
	})

	entries, truncated, err := Discover(root, reg.list, TrackSameLine, 2)
	if err != nil || truncated {
		t.Fatalf("Discover: %v, truncated %v", err, truncated)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	build, final := entries[0], entries[1]
	if build.File != "Dockerfile" || build.StageLabel != "build" || build.Image != "golang:1.21" {
		t.Errorf("build entry = %+v", build)
	}
	if want := []string{"golang:1.23", "golang:1.22"}; !reflect.DeepEqual(build.Candidates, want) {
		t.Errorf("build candidates = %v, want %v", build.Candidates, want)
	}
	if final.StageLabel != "#2" || final.Image != "alpine:3.18" {
		t.Errorf("final entry = %+v", final)
	}
	if want := []string{"alpine:3.20", "alpine:3.19"}; !reflect.DeepEqual(final.Candidates, want) {
		t.Errorf("final candidates = %v, want %v", final.Candidates, want)
	}
}

// Every stage is considered — a CVE in a build stage can reach the image that
// ships — but scratch and a reference to an earlier stage are not images.
func TestDiscoverSkipsWhatIsNotAnImage(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Dockerfile": "FROM golang:1.21 AS build\nFROM build AS again\nFROM scratch\n",
	})
	entries, _, err := Discover(root, newRegistry(nil).list, TrackSameLine, 3)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) != 1 || entries[0].StageLabel != "build" {
		t.Errorf("entries = %+v, want only the build stage", entries)
	}
}

func TestDiscoverAsksTheRegistryOncePerRepository(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/Dockerfile": "FROM alpine:3.18\n",
		"b/Dockerfile": "FROM alpine:3.19\nFROM alpine:3.18 AS x\n",
	})
	reg := newRegistry(map[string][]string{"library/alpine": {"3.18", "3.19", "3.20"}})

	if _, _, err := Discover(root, reg.list, TrackSameLine, 3); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if reg.calls["library/alpine"] != 1 {
		t.Errorf("asked the registry %d times, want once for the shared repository", reg.calls["library/alpine"])
	}
}

// A tag with no version to move from is answered without a request: latest and
// a codename do not depend on what the registry holds.
func TestDiscoverDoesNotAskAboutATagItCannotMove(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Dockerfile": "FROM debian:bookworm-slim\nFROM ubuntu\nFROM alpine@sha256:abc\n",
	})
	reg := newRegistry(map[string][]string{})

	entries, _, err := Discover(root, reg.list, TrackSameLine, 3)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(reg.calls) != 0 {
		t.Errorf("asked the registry: %v", reg.calls)
	}
	for _, e := range entries {
		if len(e.Candidates) != 0 || e.Reason == "" {
			t.Errorf("%s: candidates %v, reason %q; want none, with a reason", e.Image, e.Candidates, e.Reason)
		}
	}
}

// A floating tag says it floats, and a digest-only reference says it is pinned:
// the two used to share one reason that named both (§3.79).
func TestAFloatingTagSaysSoAndADigestSaysItIsPinned(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Dockerfile": "FROM dhi.io/python:3-dev\nFROM ubuntu\nFROM alpine@sha256:abc\nFROM golang:1.23\n",
	})
	reg := newRegistry(map[string][]string{"library/golang": {"1.23", "1.24"}})
	entries, _, err := Discover(root, reg.list, TrackSameLine, 3)
	if err != nil || len(entries) != 4 {
		t.Fatalf("Discover: %v, %d entries", err, len(entries))
	}
	want := []struct {
		floating bool
		reason   string
	}{
		{false, ""}, // 3-dev carries a version: it is a versioned line, not a floating tag
		{true, ReasonFloating},
		{false, ReasonPinnedByDigest},
		{false, ""},
	}
	for i, w := range want {
		e := entries[i]
		if e.Floating != w.floating || (w.reason != "" && e.Reason != w.reason) {
			t.Errorf("%s: floating %v, reason %q; want %v, %q", e.Image, e.Floating, e.Reason, w.floating, w.reason)
		}
	}
}

func TestAnUnresolvableReferenceIsListedWithItsReason(t *testing.T) {
	root := repoWith(t, map[string]string{"Dockerfile": "FROM ${BASE}\n"})
	entries, _, err := Discover(root, newRegistry(nil).list, TrackSameLine, 3)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Discover: %v, %d entries", err, len(entries))
	}
	if entries[0].Image != "" || entries[0].Reason == "" {
		t.Errorf("entry = %+v, want no image and a reason", entries[0])
	}
}

// One registry down must not take the other stages with it.
func TestARegistryFailureIsThatEntrysReasonAlone(t *testing.T) {
	root := repoWith(t, map[string]string{"Dockerfile": "FROM alpine:3.18\n"})
	reg := newRegistry(nil)
	reg.err = errors.New("connection refused")

	entries, _, err := Discover(root, reg.list, TrackSameLine, 3)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) != 1 || len(entries[0].Candidates) != 0 || entries[0].Reason == "" {
		t.Errorf("entries = %+v", entries)
	}
	// The reason is shown to the user; the transport error goes to the log.
	if got := entries[0].Reason; got != "could not list tags of alpine — check logs" {
		t.Errorf("Reason = %q", got)
	}
}

func TestCandidatesKeepTheNameAsWritten(t *testing.T) {
	root := repoWith(t, map[string]string{"Dockerfile": "FROM ghcr.io/o/app:1.2\n"})
	reg := newRegistry(map[string][]string{"o/app": {"1.2", "1.3"}})
	entries, _, err := Discover(root, reg.list, TrackSameLine, 3)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := []string{"ghcr.io/o/app:1.3"}; !reflect.DeepEqual(entries[0].Candidates, want) {
		t.Errorf("candidates = %v, want %v", entries[0].Candidates, want)
	}
}

func TestDiscoverOfAMissingRootIsAnError(t *testing.T) {
	if _, _, err := Discover(filepath.Join(t.TempDir(), "absent"), newRegistry(nil).list, TrackSameLine, 3); err == nil {
		t.Error("a missing root was not reported")
	}
}

func TestDiscoverOfARepositoryWithNoDockerfileIsEmpty(t *testing.T) {
	root := repoWith(t, map[string]string{"main.go": "package main\n"})
	entries, _, err := Discover(root, newRegistry(nil).list, TrackSameLine, 3)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries %v, err %v; want none", entries, err)
	}
}
