package imageupdate

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/remediation"
)

func TestNewerPatch(t *testing.T) {
	tags := []string{"20.11.1", "20.11.2", "20.11.10", "20.12.0", "21.0.0", "20.11.3-alpine", "20.11", "latest"}
	for current, want := range map[string]string{
		"20.11.1":        "20.11.10", // numeric, not lexical
		"20.11.10":       "",
		"20.11.1-alpine": "20.11.3-alpine",
		"20.11":          "", // floats over its patches: the digest says
		"20":             "",
		"latest":         "",
	} {
		if got := NewerPatch(current, tags); got != want {
			t.Errorf("NewerPatch(%q) = %q, want %q", current, got, want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Now()
	for name, tt := range map[string]struct {
		facts Facts
		local []string
		want  Status
	}{
		"not checked":         {Facts{}, []string{"alpine@sha256:a"}, Status{}},
		"failed":              {Facts{CheckedAt: now, Failed: true, Digest: "sha256:b"}, []string{"alpine@sha256:a"}, Status{}},
		"same digest":         {Facts{CheckedAt: now, Digest: "sha256:a"}, []string{"docker.io/library/alpine@sha256:a"}, Status{}},
		"new build":           {Facts{CheckedAt: now, Digest: "sha256:b"}, []string{"alpine@sha256:a"}, Status{Kind: NewBuild}},
		"pinned in a file":    {Facts{CheckedAt: now, Digest: "sha256:b"}, []string{"sha256:a"}, Status{Kind: NewBuild}},
		"no local digest":     {Facts{CheckedAt: now, Digest: "sha256:b"}, nil, Status{}},
		"patch wins":          {Facts{CheckedAt: now, Digest: "sha256:b", NewerPatch: "1.2.4"}, []string{"x@sha256:a"}, Status{Kind: NewPatch, Tag: "1.2.4"}},
		"patch without local": {Facts{CheckedAt: now, NewerPatch: "1.2.4"}, nil, Status{Kind: NewPatch, Tag: "1.2.4"}},
	} {
		if got := Evaluate(tt.facts, tt.local); got != tt.want {
			t.Errorf("%s: Evaluate = %+v, want %+v", name, got, tt.want)
		}
	}
}

// fakeRegistry answers from maps and counts what it is asked.
type fakeRegistry struct {
	mu      sync.Mutex
	digests map[string]string // repository:tag → digest
	tags    map[string][]string
	asked   []string
}

func (f *fakeRegistry) registry() Registry {
	return Registry{
		Digest: func(ref remediation.Ref, tag string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			key := ref.Repository + ":" + tag
			f.asked = append(f.asked, "digest "+key)
			if d, ok := f.digests[key]; ok {
				return d, nil
			}
			return "", errors.New("not found")
		},
		Tags: func(ref remediation.Ref) ([]string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked = append(f.asked, "tags "+ref.Repository)
			return f.tags[ref.Repository], nil
		},
	}
}

func TestLookup(t *testing.T) {
	now := time.Now()
	reg := &fakeRegistry{
		digests: map[string]string{"library/alpine:latest": "sha256:l", "library/node:20.11.1": "sha256:n", "library/alpine:3.20": "sha256:t"},
		tags:    map[string][]string{"library/node": {"20.11.1", "20.11.4"}},
	}
	if f := lookup(reg.registry(), "alpine", now); f.Digest != "sha256:l" || f.NewerPatch != "" {
		t.Errorf("untagged = %+v, want the digest of latest", f)
	}
	if f := lookup(reg.registry(), "node:20.11.1", now); f.Digest != "sha256:n" || f.NewerPatch != "20.11.4" {
		t.Errorf("patch tag = %+v", f)
	}
	if f := lookup(reg.registry(), "alpine:3.20@sha256:old", now); f.Digest != "sha256:t" {
		t.Errorf("pinned tag = %+v, want the tag's current digest", f)
	}
	if f := lookup(reg.registry(), "myapp:dev", now); !f.Failed {
		t.Errorf("unknown image = %+v, want Failed", f)
	}
	before := len(reg.asked)
	if f := lookup(reg.registry(), "alpine@sha256:abc", now); f.Failed || f.Digest != "" || len(reg.asked) != before {
		t.Errorf("digest-only = %+v, asked %v; want nothing asked", f, reg.asked[before:])
	}
	// Tags are only listed for a tag with patches of its own.
	for _, a := range reg.asked {
		if a == "tags library/alpine" {
			t.Errorf("listed alpine's tags for a tag with no patch component")
		}
	}
}

func TestCheckUsesTheCacheWhileFresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := &fakeRegistry{digests: map[string]string{"library/alpine:latest": "sha256:l"}}
	now := time.Now()

	first := Check(reg.registry(), []string{"alpine:latest", "alpine:latest", "myapp:dev"}, now)
	if first["alpine:latest"].Digest != "sha256:l" || !first["myapp:dev"].Failed {
		t.Fatalf("first check = %+v", first)
	}
	asked := len(reg.asked)
	if asked != 2 {
		t.Errorf("asked %v, want each reference once", reg.asked)
	}

	Check(reg.registry(), []string{"alpine:latest", "myapp:dev"}, now.Add(time.Hour))
	if len(reg.asked) != asked+1 {
		t.Errorf("an hour later asked %v; want only the failed one again (retry after %v)", reg.asked[asked:], retryAfter)
	}
	Check(reg.registry(), []string{"alpine:latest"}, now.Add(freshFor+time.Minute))
	if last := reg.asked[len(reg.asked)-1]; last != "digest library/alpine:latest" {
		t.Errorf("a stale answer was not asked again: %v", reg.asked)
	}
}

func TestTrackerAsksOnceUntilStale(t *testing.T) {
	var tr Tracker
	now := time.Now()
	if due := tr.Due([]string{"a", "b", "a"}, now); len(due) != 2 {
		t.Fatalf("first Due = %v, want a and b once", due)
	}
	if due := tr.Due([]string{"a", "b"}, now); len(due) != 0 {
		t.Errorf("Due while in flight = %v, want nothing", due)
	}
	tr.Store(map[string]Facts{"a": {CheckedAt: now, Digest: "sha256:new"}, "b": {CheckedAt: now}})
	if due := tr.Due([]string{"a", "b"}, now.Add(time.Hour)); len(due) != 0 {
		t.Errorf("Due on fresh answers = %v", due)
	}
	if due := tr.Due([]string{"a"}, now.Add(freshFor+time.Minute)); len(due) != 1 {
		t.Errorf("Due on a stale answer = %v, want it asked again", due)
	}
	if s := tr.Status("a", []string{"x@sha256:old"}); s.Kind != NewBuild {
		t.Errorf("Status = %+v", s)
	}
}
