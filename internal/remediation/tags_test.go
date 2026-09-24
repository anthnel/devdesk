package remediation

import (
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in   string
		want Ref
	}{
		{"alpine", Ref{Name: "alpine", Repository: "library/alpine"}},
		{"alpine:3.18", Ref{Name: "alpine", Repository: "library/alpine", Tag: "3.18"}},
		{"nginx/nginx:1.25", Ref{Name: "nginx/nginx", Repository: "nginx/nginx", Tag: "1.25"}},
		{"docker.io/library/node:20-alpine", Ref{Name: "docker.io/library/node", Repository: "library/node", Tag: "20-alpine"}},
		{"ghcr.io/owner/app:1.2.3", Ref{Name: "ghcr.io/owner/app", Registry: "ghcr.io", Repository: "owner/app", Tag: "1.2.3"}},
		{"localhost:5000/app:v1", Ref{Name: "localhost:5000/app", Registry: "localhost:5000", Repository: "app", Tag: "v1"}},
		{"localhost/app", Ref{Name: "localhost/app", Registry: "localhost", Repository: "app"}},
		{"registry.example.com:8443/team/app", Ref{Name: "registry.example.com:8443/team/app", Registry: "registry.example.com:8443", Repository: "team/app"}},
		{"alpine:3.18@sha256:abc", Ref{Name: "alpine", Repository: "library/alpine", Tag: "3.18", Digest: "sha256:abc"}},
		{"alpine@sha256:abc", Ref{Name: "alpine", Repository: "library/alpine", Digest: "sha256:abc"}},
	}
	for _, tc := range tests {
		if got := ParseRef(tc.in); got != tc.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestWithTagKeepsTheNameAsWritten(t *testing.T) {
	if got := ParseRef("node:20-alpine").WithTag("22-alpine"); got != "node:22-alpine" {
		t.Errorf("WithTag = %q", got)
	}
	if got := ParseRef("ghcr.io/o/a:1@sha256:abc").WithTag("2"); got != "ghcr.io/o/a:2" {
		t.Errorf("WithTag = %q, want the digest dropped", got)
	}
}

func TestSplitTag(t *testing.T) {
	tests := []struct{ tag, version, variant string }{
		{"3.20.1-alpine3.19", "3.20.1", "alpine3.19"},
		{"20-slim", "20", "slim"},
		{"3.18", "3.18", ""},
		{"v1.2.3", "1.2.3", ""},
		{"bookworm-slim", "", "bookworm-slim"},
		{"latest", "", "latest"},
		{"22.04", "22.04", ""},
		{"3.9rc1", "", "3.9rc1"},
		{"72d32a1d", "", "72d32a1d"},
		{"20-alpine3.19", "20", "alpine3.19"},
		{"1.22_bookworm", "1.22", "bookworm"},
	}
	for _, tc := range tests {
		if v, variant := SplitTag(tc.tag); v != tc.version || variant != tc.variant {
			t.Errorf("SplitTag(%q) = %q, %q; want %q, %q", tc.tag, v, variant, tc.version, tc.variant)
		}
	}
}

var alpineTags = []string{"3.16", "3.17", "3.18", "3.19", "3.20", "3.21", "edge", "latest", "3.21.0", "3.19.1", "3.20-slim", "4.0", "4.1"}

func TestCandidatesStayOnTheLineAndTheVariant(t *testing.T) {
	got, reason := Candidates("3.18", alpineTags, TrackSameLine, 3)
	if want := []string{"3.21", "3.20", "3.19"}; !reflect.DeepEqual(got, want) || reason != "" {
		t.Errorf("Candidates = %v (%q), want %v", got, reason, want)
	}
}

func TestCandidatesKeepThePrecisionOfTheCurrentTag(t *testing.T) {
	// "3.18" floats on the patch level; offering "3.19.1" would pin it.
	got, _ := Candidates("3.18", alpineTags, TrackSameLine, 10)
	for _, tag := range got {
		if tag == "3.19.1" || tag == "3.21.0" {
			t.Errorf("Candidates offered %q, more specific than the tag it replaces", tag)
		}
	}
	got, _ = Candidates("3.19.0", alpineTags, TrackSameLine, 10)
	if want := []string{"3.21.0", "3.19.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a patch-precise tag: Candidates = %v, want %v", got, want)
	}
}

func TestCandidatesNeverChangeVariant(t *testing.T) {
	tags := []string{"20-alpine", "22-alpine", "20-slim", "22-slim", "22", "24-alpine"}
	got, _ := Candidates("20-alpine", tags, TrackNextMajor, 5)
	for _, tag := range got {
		if v, variant := SplitTag(tag); v == "" || variant != "alpine" {
			t.Errorf("Candidates offered %q, want only -alpine tags", tag)
		}
	}
}

func TestSameLineNeverCrossesAMajor(t *testing.T) {
	got, _ := Candidates("3.18", alpineTags, TrackSameLine, 10)
	for _, tag := range got {
		if v, _ := SplitTag(tag); v[0] != '3' {
			t.Errorf("same-line offered %q", tag)
		}
	}
}

func TestNextMajorMayTakeTheSmallestHigherMajorThatExists(t *testing.T) {
	// node's even releases: 20 -> 22, never 21, and not 24 in one jump.
	tags := []string{"18.19", "20.10", "20.11", "22.1", "22.2", "24.0"}
	got, reason := Candidates("20.10", tags, TrackNextMajor, 10)
	if want := []string{"22.2", "22.1", "20.11"}; !reflect.DeepEqual(got, want) || reason != "" {
		t.Errorf("next-major: Candidates = %v (%q), want %v", got, reason, want)
	}
	got, _ = Candidates("20.10", tags, TrackSameLine, 10)
	if want := []string{"20.11"}; !reflect.DeepEqual(got, want) {
		t.Errorf("same-line: Candidates = %v, want %v", got, want)
	}
}

func TestCandidatesAreCapped(t *testing.T) {
	if got, _ := Candidates("3.16", alpineTags, TrackSameLine, 2); len(got) != 2 {
		t.Errorf("got %d candidates, want the cap of 2", len(got))
	}
}

func TestNoCandidateSaysWhy(t *testing.T) {
	tests := []struct {
		name, current string
		tags          []string
	}{
		{"digest only", "", alpineTags},
		{"no version", "bookworm-slim", alpineTags},
		{"already the newest", "3.21", alpineTags},
		// "20" -> "22" crosses the major, which same-line refuses.
		{"a rolling major tag", "20", []string{"18", "20", "22"}},
		{"nothing listed", "3.18", nil},
		{"a commit hash", "72d32a1d", []string{"72d32a1d", "73aa00ff"}},
	}
	for _, tc := range tests {
		got, reason := Candidates(tc.current, tc.tags, TrackSameLine, 3)
		if len(got) != 0 || reason == "" {
			t.Errorf("%s: Candidates = %v, reason %q; want none, with a reason", tc.name, got, reason)
		}
	}
}

func TestParseTrackFallsBackToSameLine(t *testing.T) {
	tests := map[string]Track{
		"same-line": TrackSameLine, "next-major": TrackNextMajor, "": TrackSameLine, "bogus": TrackSameLine,
	}
	for in, want := range tests {
		if got := ParseTrack(in); got != want {
			t.Errorf("ParseTrack(%q) = %q, want %q", in, got, want)
		}
	}
}

// config states the track names and cannot import this package, so a test is
// the only thing that keeps the two in step.
func TestTheTrackNamesMatchTheConfig(t *testing.T) {
	for _, name := range config.BaseImageTracks() {
		if got := ParseTrack(name); string(got) != name {
			t.Errorf("config offers %q, ParseTrack reads it as %q", name, got)
		}
	}
	if string(TrackSameLine) != config.BaseImageTrackSameLine || string(TrackNextMajor) != config.BaseImageTrackNextMajor {
		t.Errorf("track constants %q / %q differ from the config's %q / %q",
			TrackSameLine, TrackNextMajor, config.BaseImageTrackSameLine, config.BaseImageTrackNextMajor)
	}
}

// The reason for a hash tag is that it has no version, not that its "line" has
// nothing newer — the wording the table showed for a commit-hash tag.
func TestAHashTagSaysItHasNoVersion(t *testing.T) {
	_, reason := Candidates("72d32a1d", []string{"72d32a1d", "73aa00ff"}, TrackSameLine, 3)
	if reason != "the tag carries no version to move from" {
		t.Errorf("reason = %q", reason)
	}
}

// A floating reference names content that changes under the same name (§3.79):
// no digest, and no version in the tag.
func TestWhatFloats(t *testing.T) {
	for ref, want := range map[string]bool{
		"alpine":                        true, // the implicit latest
		"alpine:latest":                 true,
		"dhi.io/node:dev":               true,
		"debian:bookworm-slim":          true,
		"ghcr.io/org/app:main":          true,
		"alpine:3.20":                   false,
		"node:20-slim":                  false,
		"alpine@sha256:abc":             false, // pinned
		"alpine:latest@sha256:abc":      false,
		"localhost:5000/team/app:3.1.0": false,
	} {
		if got := ParseRef(ref).Floats(); got != want {
			t.Errorf("ParseRef(%q).Floats() = %v, want %v", ref, got, want)
		}
	}
}
