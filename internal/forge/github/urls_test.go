package github

import (
	"net/url"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/forge"
)

func TestCloneURLsAreBuiltPerMethod(t *testing.T) {
	for _, tc := range []struct {
		name    string
		baseURL string
		method  forge.CloneMethod
		want    string
	}{
		{"https", "https://github.com", forge.CloneHTTPS, "https://github.com/acme/api.git"},
		{"ssh strips the scheme", "https://github.com", forge.CloneSSH, "git@github.com:acme/api.git"},
		{"a trailing slash does not double up", "https://github.com/", forge.CloneHTTPS, "https://github.com/acme/api.git"},
		{"enterprise keeps its host", "https://git.acme.test", forge.CloneSSH, "git@git.acme.test:acme/api.git"},
		{"an empty host means the public instance", "", forge.CloneHTTPS, "https://github.com/acme/api.git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewWithClient(nil, tc.baseURL)
			if got := f.CloneURL(forge.Repository{Path: "acme/api"}, tc.method); got != tc.want {
				t.Errorf("CloneURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheIssuesLinkEscapesTheQuery — GitHub has no per-assignee path, so the
// filter goes in a search parameter, which is exactly why it is escaped rather
// than concatenated.
func TestTheIssuesLinkEscapesTheQuery(t *testing.T) {
	f := NewWithClient(nil, "https://github.com")
	got := f.AssignedIssuesURL(forge.User{Username: "a&b=c"})

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("AssignedIssuesURL() produced an unparseable URL %q: %v", got, err)
	}
	q := parsed.Query().Get("q")
	if !strings.Contains(q, "assignee:a&b=c") {
		t.Errorf("q = %q, want the username intact inside the query", q)
	}
	if !strings.Contains(q, "is:issue") {
		t.Errorf("q = %q, want it filtered to issues", q)
	}
}

func TestTheChangeRequestsLinkIsThePullDashboard(t *testing.T) {
	f := NewWithClient(nil, "https://github.com/")
	if got, want := f.ChangeRequestsURL(), "https://github.com/pulls"; got != want {
		t.Errorf("ChangeRequestsURL() = %q, want %q", got, want)
	}
}

// TestEnterpriseGetsItsAPIPathAndKeepsItsWebHost — the API lives under /api/v3
// on Enterprise, and the configured URL is the *web* host clone URLs and
// browser links are built from. Conflating the two would send a user to
// https://git.acme.test/api/v3/acme/api.
func TestEnterpriseGetsItsAPIPathAndKeepsItsWebHost(t *testing.T) {
	f, err := New("https://git.acme.test", "ghp_x")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := f.client.BaseURL.String(); !strings.Contains(got, "/api/v3/") {
		t.Errorf("API base = %q, want it under /api/v3/", got)
	}
	if got := f.CloneURL(forge.Repository{Path: "acme/api"}, forge.CloneHTTPS); got != "https://git.acme.test/acme/api.git" {
		t.Errorf("CloneURL() = %q, want the web host", got)
	}
}

// TestDotComKeepsTheDefaultAPIHost — github.com's API is api.github.com, not
// github.com/api/v3, so the Enterprise treatment must not be applied to it.
func TestDotComKeepsTheDefaultAPIHost(t *testing.T) {
	f, err := New("https://github.com", "ghp_x")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := f.client.BaseURL.String(); !strings.Contains(got, "api.github.com") {
		t.Errorf("API base = %q, want api.github.com", got)
	}
}
