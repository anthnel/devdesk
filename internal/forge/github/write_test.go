package github

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
)

// TestAnOrganizationCannotBeCreatedOrDeleted is what the interface asks a
// backend to do about a capability it does not have: refuse, naming the reason.
//
// The alternative would be creating a repository under the user's own account
// and calling it an organisation, which is the shape of failure §3.6 exists to
// make unexpressible.
func TestAnOrganizationCannotBeCreatedOrDeleted(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused operation reached the network: %s %s", r.Method, r.URL.Path)
	})
	f := fake.forge(t)

	if _, err := f.CreateNamespace(context.Background(), forge.NewNamespace{Name: "acme"}); err == nil {
		t.Error("CreateNamespace() succeeded, want a refusal")
	}
	if err := f.DeleteNamespace(context.Background(), forge.Namespace{ID: "acme"}, false); err == nil {
		t.Error("DeleteNamespace() succeeded, want a refusal")
	}
}

// TestAPermanentDeleteIsRefused — GitHub deletes at once and has no grace
// period, so honouring the flag would report a distinction the platform does
// not make. Shape.PermanentDelete is false so the checkbox is never offered;
// this is the guard behind it.
func TestAPermanentDeleteIsRefused(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	f := fake.forge(t)

	if err := f.DeleteRepository(context.Background(), forge.Repository{ID: "acme/api"}, true); err == nil {
		t.Error("a permanent delete succeeded, want a refusal")
	}
	if n := len(fake.calls()); n != 0 {
		t.Errorf("a refused delete made %d requests, want 0", n)
	}

	if err := f.DeleteRepository(context.Background(), forge.Repository{ID: "acme/api"}, false); err != nil {
		t.Errorf("an ordinary delete failed: %v", err)
	}
	if got := fake.paths(); len(got) != 1 || !strings.HasSuffix(got[0], "/repos/acme/api") {
		t.Errorf("the delete addressed %v, want /repos/acme/api", got)
	}
}

// TestCreatingARepositoryPlacesItInItsOrganization — an empty NamespaceID is
// the user's own account, which is what go-github's empty `org` does, so the
// interface's rule needs no translation.
func TestCreatingARepositoryPlacesItInItsOrganization(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"api","full_name":"acme/api","private":true,"visibility":"private"}`))
	})

	repo, err := fake.forge(t).CreateRepository(context.Background(), forge.NewRepository{
		Name: "api", Slug: "api", Visibility: "private", NamespaceID: "acme",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if repo.ID != "acme/api" || repo.Path != "acme/api" {
		t.Errorf("created repository = %+v, want acme/api for both id and path", repo)
	}

	call := fake.calls()[0]
	if !strings.HasSuffix(call.Path, "/orgs/acme/repos") {
		t.Errorf("the create addressed %q, want the organisation's repos", call.Path)
	}

	var body struct {
		Private  *bool `json:"private"`
		AutoInit *bool `json:"auto_init"`
	}
	call.decode(t, &body)
	if body.Private == nil || !*body.Private {
		t.Errorf("private = %v, want true for a private repository", body.Private)
	}
	// AutoInit off is not incidental: a generated README would give the initial
	// commit a parent it does not expect.
	if body.AutoInit == nil || *body.AutoInit {
		t.Errorf("auto_init = %v, want false", body.AutoInit)
	}
}

// TestAVisibilityGitHubDoesNotHaveIsRefused — the shape says which values
// exist, and sending one it does not have would fail at the server with a
// message about the API rather than about the setting.
func TestAVisibilityGitHubDoesNotHaveIsRefused(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a refused create reached the network: %s", r.URL.Path)
	})

	_, err := fake.forge(t).CreateRepository(context.Background(), forge.NewRepository{
		Name: "api", Slug: "api", Visibility: "internal",
	})
	if err == nil {
		t.Fatal("CreateRepository() accepted `internal`, which GitHub does not offer")
	}
	if !strings.Contains(err.Error(), "internal") {
		t.Errorf("error = %q, want it to name the visibility", err)
	}
}

// TestAnInitialCommitIsOneCommit is the whole reason InitialCommit uses the git
// data API: Repositories.CreateFile in a loop would make one commit per file,
// so a three-file template would arrive as three commits.
func TestAnInitialCommitIsOneCommit(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blob-sha"}`))
		case strings.HasSuffix(r.URL.Path, "/git/trees"):
			_, _ = w.Write([]byte(`{"sha":"tree-sha"}`))
		case strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"commit-sha"}`))
		default:
			_, _ = w.Write([]byte(`{"ref":"refs/heads/main"}`))
		}
	})

	err := fake.forge(t).InitialCommit(context.Background(), "acme/api", []forge.FileChange{
		{Action: forge.FileCreate, Path: "README.md", Content: "hello"},
		{Action: forge.FileCreate, Path: "Makefile", Content: "all:"},
	})
	if err != nil {
		t.Fatalf("InitialCommit() error = %v", err)
	}

	calls := fake.calls()
	if n := len(calls); n != 5 {
		t.Fatalf("got %d requests, want 5 (two blobs, a tree, a commit, a ref): %v", n, fake.paths())
	}
	if strings.Count(strings.Join(fake.paths(), " "), "/git/commits") != 1 {
		t.Errorf("more than one commit was created: %v", fake.paths())
	}

	// The commit has no parent, which is what makes it an *initial* one.
	var commit struct {
		Parents []any  `json:"parents"`
		Message string `json:"message"`
	}
	calls[3].decode(t, &commit)
	if len(commit.Parents) != 0 {
		t.Errorf("the commit has %d parents, want none", len(commit.Parents))
	}
	if commit.Message != initialCommitMessage {
		t.Errorf("message = %q, want the same wording as the GitLab backend", commit.Message)
	}

	var ref struct {
		Ref string `json:"ref"`
	}
	calls[4].decode(t, &ref)
	if ref.Ref != "refs/heads/"+defaultBranch {
		t.Errorf("ref = %q, want refs/heads/%s", ref.Ref, defaultBranch)
	}
}

// TestAnInitialCommitRefusesADeletion — there is nothing to delete in a
// repository with no commits, and dropping the entry silently would hide a
// template that means something else than it says.
func TestAnInitialCommitRefusesADeletion(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sha":"x"}`))
	})

	err := fake.forge(t).InitialCommit(context.Background(), "acme/api", []forge.FileChange{
		{Action: forge.FileDelete, Path: "gone.txt"},
	})
	if err == nil {
		t.Fatal("InitialCommit() accepted a deletion")
	}
	if !strings.Contains(err.Error(), "gone.txt") {
		t.Errorf("error = %q, want it to name the file", err)
	}
}

// TestAnIdentifierWithoutTwoHalvesIsRefused — the opaque ID is this package's
// to write and to read, and a value from somewhere else is a bug rather than a
// request to guess.
func TestAnIdentifierWithoutTwoHalvesIsRefused(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	f := fake.forge(t)

	if err := f.DeleteRepository(context.Background(), forge.Repository{ID: "api"}, false); err == nil {
		t.Error("DeleteRepository() accepted an identifier with no owner")
	}
	if err := f.InitialCommit(context.Background(), "api", []forge.FileChange{
		{Action: forge.FileCreate, Path: "a", Content: "b"},
	}); err == nil {
		t.Error("InitialCommit() accepted an identifier with no owner")
	}
}

// TestACounterThatCouldNotBeReadStaysNil is D52 in the second backend: five
// independent requests, and the one that fails leaves nil rather than a zero
// the dashboard would render as "you have none".
func TestACounterThatCouldNotBeReadStaysNil(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/search/issues"):
			if strings.Contains(r.URL.Query().Get("q"), "is:issue") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
				return
			}
			_, _ = w.Write([]byte(`{"total_count":3,"items":[]}`))
		case strings.Contains(r.URL.Path, "/user/repos"):
			_, _ = w.Write([]byte(`[{"name":"api"},{"name":"web"}]`))
		default:
			_, _ = w.Write([]byte(`[{"login":"acme"}]`))
		}
	})

	stats, err := fake.forge(t).DashboardStats(context.Background(), forge.User{ID: "ada", Username: "ada"})
	if err != nil {
		t.Fatalf("DashboardStats() error = %v", err)
	}

	if stats.AssignedIssues != nil {
		t.Errorf("a refused issue search reported %d, want nil", *stats.AssignedIssues)
	}
	if stats.AssignedChangeRequests == nil || *stats.AssignedChangeRequests != 3 {
		t.Errorf("assigned pull requests = %v, want 3", stats.AssignedChangeRequests)
	}
	if stats.Repositories == nil || *stats.Repositories != 2 {
		t.Errorf("repositories = %v, want 2", stats.Repositories)
	}
	if stats.Namespaces == nil || *stats.Namespaces != 1 {
		t.Errorf("organisations = %v, want 1", stats.Namespaces)
	}
}

// TestTheShapeIsGitHubs pins what the backend promises the rest of the
// application, and it is the interesting half of the abstraction: everything
// here differs from GitLab.
func TestTheShapeIsGitHubs(t *testing.T) {
	shape := NewWithClient(nil, "https://github.com").Shape()

	if shape.Name != config.ForgeGitHub {
		t.Errorf("Name = %q, want github", shape.Name)
	}
	if shape.AllowsVisibility("internal") {
		t.Error("`internal` is offered, and GitHub.com does not have it")
	}
	if shape.PermanentDelete {
		t.Error("PermanentDelete is true, but there is no grace period to bypass")
	}
	if shape.CanNestUnder(1) {
		t.Error("CanNestUnder(1) = true, but an organisation does not hold an organisation")
	}
	if !shape.CanNestUnder(0) {
		t.Error("CanNestUnder(0) = false — every forge takes a root namespace")
	}
}
