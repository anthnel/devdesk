package forge

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// TestOnlyThePublicInstancesAreRecognised is the whole of DetectType's
// contract. Self-hosted is the case that matters and `git.acme.com` could be
// either, so an unknown host must answer "" — a wrong guess that looks
// confident is worse than no guess at all.
func TestOnlyThePublicInstancesAreRecognised(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"https://gitlab.com", config.ForgeGitLab},
		{"https://gitlab.com/", config.ForgeGitLab},
		{"gitlab.com", config.ForgeGitLab}, // typed with no scheme
		{"HTTPS://GitLab.com", config.ForgeGitLab},
		{"https://gitlab.com:8443", config.ForgeGitLab},
		{"https://github.com", config.ForgeGitHub},
		{"github.com", config.ForgeGitHub},

		// Self-hosted, and the two names that look like they should work.
		{"https://git.acme.test", ""},
		{"https://gitlab.acme.test", ""},
		{"https://github.acme.test", ""},

		// Nothing to read.
		{"", ""},
		{"   ", ""},
		{"://not a url", ""},
	} {
		if got := DetectType(tc.url); got != tc.want {
			t.Errorf("DetectType(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// TestAPathIsNotAHost — `gitlab.com.evil.test` ends in neither, and a check on
// the *string* rather than on the parsed host would say GitLab for it.
func TestAPathIsNotAHost(t *testing.T) {
	for _, url := range []string{
		"https://evil.test/gitlab.com",
		"https://gitlab.com.evil.test",
		"https://notgithub.com",
	} {
		if got := DetectType(url); got != "" {
			t.Errorf("DetectType(%q) = %q, want no detection", url, got)
		}
	}
}

// TestEveryConfiguredForgeHasAShape is the guard against a half-added forge,
// the same one the vocabulary has: a `type:` the configuration view offers but
// no shape answers to would render the wrong platform's visibility list.
func TestEveryConfiguredForgeHasAShape(t *testing.T) {
	seen := map[string]bool{}
	for _, forgeType := range config.ForgeTypes() {
		shape := ShapeFor(forgeType)
		if shape.Name != forgeType {
			t.Errorf("ShapeFor(%q) answered for %q — one of them has no shape of its own", forgeType, shape.Name)
		}
		if len(shape.Visibilities) == 0 {
			t.Errorf("%q declares no visibility, so its creation form offers nothing", forgeType)
		}
		seen[shape.Name] = true
	}
	if len(seen) != len(config.ForgeTypes()) {
		t.Errorf("%d forges collapsed to %d shapes", len(config.ForgeTypes()), len(seen))
	}
}

// TestAnUnknownForgeStillHasAShape — belt to applyDefaults' brace, and the
// same fallback the vocabulary makes.
func TestAnUnknownForgeStillHasAShape(t *testing.T) {
	if got := ShapeFor("bitbucket").Name; got != config.ForgeGitLab {
		t.Errorf("an unknown forge resolved to %q, want the GitLab fallback", got)
	}
}

// TestTheBackendAndTheTableAgree — a backend's Shape() delegates, so there is
// one table rather than two that can drift.
func TestTheShapesDifferWhereItMatters(t *testing.T) {
	gitlab, github := ShapeFor(config.ForgeGitLab), ShapeFor(config.ForgeGitHub)

	if !gitlab.AllowsVisibility("internal") || github.AllowsVisibility("internal") {
		t.Error("`internal` should be GitLab's alone")
	}
	if !gitlab.PermanentDelete || github.PermanentDelete {
		t.Error("only GitLab has a grace period to bypass")
	}
	if !gitlab.CanNestUnder(3) || github.CanNestUnder(1) {
		t.Error("GitLab nests without a limit, GitHub not at all")
	}
}
