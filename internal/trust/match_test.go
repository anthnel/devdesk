package trust

import "testing"

func TestRepositoryNormalizesTheWayARuleIsMatched(t *testing.T) {
	for ref, want := range map[string]string{
		"python":                                      "docker.io/library/python",
		"python:3.12":                                 "docker.io/library/python",
		"docker.io/library/python:3.12":               "docker.io/library/python",
		"index.docker.io/bitnami/redis:7":             "docker.io/bitnami/redis",
		"bitnami/redis@sha256:abc":                    "docker.io/bitnami/redis",
		"gcr.io/distroless/static-debian12:nonroot":   "gcr.io/distroless/static-debian12",
		"localhost:5000/demo:1":                       "localhost:5000/demo",
		"registry.corp.example:8443/a/b/c:1@sha256:x": "registry.corp.example:8443/a/b/c",
	} {
		if got := Repository(ref); got != want {
			t.Errorf("Repository(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestMatchingKeepsStarToOneSegment(t *testing.T) {
	for _, c := range []struct {
		pattern, repo string
		want          bool
	}{
		{"gcr.io/distroless/*", "gcr.io/distroless/static", true},
		{"gcr.io/distroless/*", "gcr.io/distroless/a/b", false},
		{"gcr.io/distroless/**", "gcr.io/distroless/a/b", true},
		{"gcr.io/distroless/**", "gcr.io/distroless", false},
		{"gcr.io/distroless/**", "gcr.io/distrolessX/a", false},
		{"docker.io/library/python", "docker.io/library/python", true},
	} {
		if got := matches(c.pattern, c.repo); got != c.want {
			t.Errorf("matches(%q, %q) = %v, want %v", c.pattern, c.repo, got, c.want)
		}
	}
}

func TestLookupPutsTheUserFirstThenTheBuiltinList(t *testing.T) {
	user := Policy{Rules: []Rule{
		{Match: "dhi.io/**", Mode: ModeNone, Source: SourceUser, Origin: "first"},
		{Match: "dhi.io/**", Mode: ModeNone, Source: SourceUser, Origin: "second"},
	}}
	if r, ok := user.Lookup("dhi.io/static:20250419@sha256:x"); !ok || r.Origin != "first" {
		t.Errorf("a user rule on the same scope must win over the built-in one, and the first user rule over the next: %+v", r)
	}
	if r, ok := (Policy{}).Lookup("gcr.io/distroless/static-debian12:nonroot"); !ok || r.Source != SourceBuiltin {
		t.Errorf("distroless should fall to the built-in rule: %+v, %v", r, ok)
	}
	if _, ok := (Policy{}).Lookup("python:3.12"); ok {
		t.Error("an image no rule names must have no rule")
	}
}
