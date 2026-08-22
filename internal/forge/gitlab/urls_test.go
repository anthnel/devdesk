package gitlab

import (
	"net/url"
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
		{"https", "https://gitlab.com", forge.CloneHTTPS, "https://gitlab.com/acme/api.git"},
		{"ssh strips the scheme", "https://gitlab.com", forge.CloneSSH, "git@gitlab.com:acme/api.git"},
		{"a trailing slash does not double up", "https://gitlab.com/", forge.CloneHTTPS, "https://gitlab.com/acme/api.git"},
		{"plain http is stripped for ssh too", "http://git.acme.test", forge.CloneSSH, "git@git.acme.test:acme/api.git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewWithClient(nil, tc.baseURL)
			got := f.CloneURL(forge.Repository{Path: "acme/api"}, tc.method)
			if got != tc.want {
				t.Errorf("CloneURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheIssuesLinkEscapesTheUsername — the value comes from the API, but a URL
// built by concatenation is how a query parameter ends up meaning something
// else, and nothing at the call site says it is safe.
func TestTheIssuesLinkEscapesTheUsername(t *testing.T) {
	f := NewWithClient(nil, "https://gitlab.com")
	got := f.AssignedIssuesURL(forge.User{Username: "a&b=c"})

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("AssignedIssuesURL() produced an unparseable URL %q: %v", got, err)
	}
	if u := parsed.Query().Get("assignee_username[]"); u != "a&b=c" {
		t.Errorf("assignee_username[] = %q, want %q — the username leaked into the query", u, "a&b=c")
	}
	if parsed.Query().Get("state") != "opened" {
		t.Errorf("state = %q, want opened", parsed.Query().Get("state"))
	}
}

func TestTheChangeRequestsLinkIsTheMergeRequestDashboard(t *testing.T) {
	f := NewWithClient(nil, "https://gitlab.com/")
	if got, want := f.ChangeRequestsURL(), "https://gitlab.com/dashboard/merge_requests"; got != want {
		t.Errorf("ChangeRequestsURL() = %q, want %q", got, want)
	}
}

// TestTheShapeIsGitLabs pins what the backend promises the rest of the
// application: three visibilities, unbounded nesting, and a permanent delete.
func TestTheShapeIsGitLabs(t *testing.T) {
	shape := NewWithClient(nil, "https://gitlab.com").Shape()

	if shape.Name != "gitlab" {
		t.Errorf("Name = %q, want gitlab", shape.Name)
	}
	if !shape.AllowsVisibility("internal") {
		t.Error("`internal` is refused, and it is the visibility GitHub does not have")
	}
	if shape.DefaultVisibility() != "private" {
		t.Errorf("DefaultVisibility() = %q, want private", shape.DefaultVisibility())
	}
	if !shape.PermanentDelete {
		t.Error("PermanentDelete is false, but the two-step removal is implemented")
	}
	if !shape.CanNestUnder(7) {
		t.Error("CanNestUnder(7) = false, but GitLab nesting is declared unbounded")
	}
}
