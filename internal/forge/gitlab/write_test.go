package gitlab

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/forge"
)

// TestAPermanentDeleteAddressesTheRenamedPath is the two-step GitLab requires,
// and the reason the interface takes the whole value rather than an ID: the
// second call names `<path>-deletion_scheduled-<id>`, which the first one
// created.
func TestAPermanentDeleteAddressesTheRenamedPath(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	ns := forge.Namespace{ID: "42", Path: "acme/legacy"}
	if err := fake.forge(t).DeleteNamespace(context.Background(), ns, true); err != nil {
		t.Fatalf("DeleteNamespace() error = %v", err)
	}

	calls := fake.calls()
	if len(calls) != 2 {
		t.Fatalf("got %d requests, want 2 (schedule then remove): %v", len(calls), fake.paths())
	}
	if !strings.HasSuffix(calls[0].Path, "/groups/42") {
		t.Errorf("the first call addressed %q, want the group by id", calls[0].Path)
	}
	if !strings.Contains(calls[1].Path, "acme/legacy-deletion_scheduled-42") {
		t.Errorf("the second call addressed %q, want the renamed path", calls[1].Path)
	}
	if calls[1].Query.Get("permanently_remove") != "true" {
		t.Errorf("permanently_remove = %q, want true", calls[1].Query.Get("permanently_remove"))
	}
}

// TestAnOrdinaryDeleteIsOneCall — scheduling is the whole operation, and a
// second call would permanently remove something the user did not ask to lose.
func TestAnOrdinaryDeleteIsOneCall(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	repo := forge.Repository{ID: "7", Path: "acme/api"}
	if err := fake.forge(t).DeleteRepository(context.Background(), repo, false); err != nil {
		t.Fatalf("DeleteRepository() error = %v", err)
	}
	if got := len(fake.calls()); got != 1 {
		t.Fatalf("got %d requests, want 1: %v", got, fake.paths())
	}
}

// TestAFailedScheduleDoesNotAskForARemoval — the renamed path does not exist if
// the rename never happened, so the second call would report a confusing 404
// for a failure that already had a reason.
func TestAFailedScheduleDoesNotAskForARemoval(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
	})

	ns := forge.Namespace{ID: "42", Path: "acme/legacy"}
	if err := fake.forge(t).DeleteNamespace(context.Background(), ns, true); err == nil {
		t.Fatal("DeleteNamespace() succeeded on a refused schedule, want an error")
	}
	if got := len(fake.calls()); got != 1 {
		t.Errorf("got %d requests after a refused schedule, want 1: %v", got, fake.paths())
	}
}

// TestCreatingUnderAParentSendsTheNumericID is the one place the opaque ID has
// to be read back: CreateGroupOptions.ParentID is an *int64 and a path will not
// do. Only this package may do it.
func TestCreatingUnderAParentSendsTheNumericID(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":99,"name":"team","full_path":"acme/team"}`))
	})

	ns, err := fake.forge(t).CreateNamespace(context.Background(), forge.NewNamespace{
		Name: "team", Slug: "team", Visibility: "private", ParentID: "42",
	})
	if err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	if ns.ID != "99" || ns.Path != "acme/team" {
		t.Errorf("created namespace = %+v, want id 99 at acme/team", ns)
	}

	var body struct {
		ParentID *int64 `json:"parent_id"`
	}
	fake.calls()[0].decode(t, &body)
	if body.ParentID == nil || *body.ParentID != 42 {
		t.Errorf("parent_id = %v, want 42", body.ParentID)
	}
}

// TestARootNamespaceSendsNoParent — an empty ParentID means the root, and
// sending a zero would ask GitLab for the group whose id is 0.
func TestARootNamespaceSendsNoParent(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":99,"name":"acme","full_path":"acme"}`))
	})

	if _, err := fake.forge(t).CreateNamespace(context.Background(), forge.NewNamespace{
		Name: "acme", Slug: "acme", Visibility: "private",
	}); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}

	var body map[string]any
	fake.calls()[0].decode(t, &body)
	if _, sent := body["parent_id"]; sent {
		t.Errorf("parent_id was sent for a root namespace: %v", body["parent_id"])
	}
}

// TestACounterThatCouldNotBeReadStaysNil is D52 in the backend: five
// independent requests, and the one that fails leaves nil rather than a zero
// the dashboard would render as "you have none".
func TestACounterThatCouldNotBeReadStaysNil(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/issues") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
			return
		}
		w.Header().Set("X-Total", "3")
		_, _ = w.Write([]byte(`[]`))
	})

	stats, err := fake.forge(t).DashboardStats(context.Background(), forge.User{ID: "9", Username: "ada"})
	if err != nil {
		t.Fatalf("DashboardStats() error = %v", err)
	}

	if stats.AssignedIssues != nil {
		t.Errorf("a refused issue count reported %d, want nil", *stats.AssignedIssues)
	}
	if stats.AssignedChangeRequests == nil || *stats.AssignedChangeRequests != 3 {
		t.Errorf("assigned change requests = %v, want 3", stats.AssignedChangeRequests)
	}
	if stats.Namespaces == nil || *stats.Namespaces != 3 {
		t.Errorf("namespaces = %v, want 3", stats.Namespaces)
	}
}

// TestAnInitialCommitTargetsMain pins the branch and the actions, which is what
// a template is applied by.
func TestAnInitialCommitTargetsMain(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})

	err := fake.forge(t).InitialCommit(context.Background(), "11", []forge.FileChange{
		{Action: forge.FileCreate, Path: "README.md", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("InitialCommit() error = %v", err)
	}

	var body struct {
		Branch  string `json:"branch"`
		Actions []struct {
			Action   string `json:"action"`
			FilePath string `json:"file_path"`
		} `json:"actions"`
	}
	fake.calls()[0].decode(t, &body)
	if body.Branch != "main" {
		t.Errorf("branch = %q, want main", body.Branch)
	}
	if len(body.Actions) != 1 || body.Actions[0].Action != "create" || body.Actions[0].FilePath != "README.md" {
		t.Errorf("actions = %+v, want one create of README.md", body.Actions)
	}
}

// TestCreatingARepositoryPlacesItInItsNamespace — NamespaceID is the second
// place a path will not do.
func TestCreatingARepositoryPlacesItInItsNamespace(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":11,"name":"api","path_with_namespace":"acme/api","visibility":"private"}`))
	})

	repo, err := fake.forge(t).CreateRepository(context.Background(), forge.NewRepository{
		Name: "api", Slug: "api", Visibility: "private", NamespaceID: "42",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if repo.ID != "11" || repo.Path != "acme/api" {
		t.Errorf("created repository = %+v, want id 11 at acme/api", repo)
	}
	if repo.Role != "" || repo.CIStatus != "" {
		t.Errorf("a freshly created repository carries a decoration: %+v", repo)
	}

	var body struct {
		NamespaceID *int64 `json:"namespace_id"`
	}
	fake.calls()[0].decode(t, &body)
	if body.NamespaceID == nil || *body.NamespaceID != 42 {
		t.Errorf("namespace_id = %v, want 42", body.NamespaceID)
	}
}

// TestAPermanentRepositoryDeleteAddressesTheRenamedPath is DeleteNamespace's
// twin, and it is tested separately because they are two code paths: a shared
// helper would have to know which service to call, which is the switch this
// avoids.
func TestAPermanentRepositoryDeleteAddressesTheRenamedPath(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	repo := forge.Repository{ID: "11", Path: "acme/api"}
	if err := fake.forge(t).DeleteRepository(context.Background(), repo, true); err != nil {
		t.Fatalf("DeleteRepository() error = %v", err)
	}

	calls := fake.calls()
	if len(calls) != 2 {
		t.Fatalf("got %d requests, want 2: %v", len(calls), fake.paths())
	}
	if !strings.Contains(calls[1].Path, "acme/api-deletion_scheduled-11") {
		t.Errorf("the second call addressed %q, want the renamed path", calls[1].Path)
	}
}

// TestAnIdentifierThatIsNotGitLabsIsRefused — the opaque ID is this package's
// to write and to read, and a value from somewhere else is a bug rather than a
// request to guess.
func TestAnIdentifierThatIsNotGitLabsIsRefused(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	f := fake.forge(t)

	if _, err := f.CreateNamespace(context.Background(), forge.NewNamespace{ParentID: "acme"}); err == nil {
		t.Error("CreateNamespace() accepted a non-numeric parent, want an error")
	}
	if _, err := f.CreateRepository(context.Background(), forge.NewRepository{NamespaceID: "acme"}); err == nil {
		t.Error("CreateRepository() accepted a non-numeric namespace, want an error")
	}
	if _, err := f.DashboardStats(context.Background(), forge.User{ID: "ada"}); err == nil {
		t.Error("DashboardStats() accepted a non-numeric user id, want an error")
	}
}

// TestAFailedListingIsAnError — a listing that could not be read must not come
// back as an empty one. That is the D20 shape at the level above the counters:
// an empty explorer reads as "this group has nothing in it".
//
// The failure is a 403 rather than a 500 on purpose: go-gitlab wraps its
// transport in retryablehttp, which retries 429 and 5xx with an exponential
// backoff. A 500 here made this one test take **35 seconds** — measured — and
// says nothing more than a 403 does. It is also the realistic failure: a token
// whose scope does not cover the endpoint.
func TestAFailedListingIsAnError(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
	})
	f := fake.forge(t)

	if _, err := f.RootNamespaces(context.Background(), forge.BrowseOptions{}); err == nil {
		t.Error("RootNamespaces() succeeded against a failing server, want an error")
	}
	if _, err := f.Children(context.Background(), "7", forge.BrowseOptions{}); err == nil {
		t.Error("Children() succeeded against a failing server, want an error")
	}
	if _, err := f.CurrentUser(context.Background()); err == nil {
		t.Error("CurrentUser() succeeded against a failing server, want an error")
	}
}

// TestASubgroupListingFailureIsNotHiddenByTheProjects — the two halves are
// fetched in sequence, and a caller that only checked the second would return
// the projects of a group whose subgroups it could not read.
func TestASubgroupListingFailureIsNotHiddenByTheProjects(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/subgroups") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"403"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":11,"name":"api","path_with_namespace":"acme/api"}]`))
	})

	if _, err := fake.forge(t).Children(context.Background(), "7", forge.BrowseOptions{}); err == nil {
		t.Error("Children() succeeded with an unreadable subgroup listing, want an error")
	}
}

// TestADecorationThatFailsDoesNotFailTheListing is the other direction, and
// deliberately: a column short is worth more than an explorer showing nothing.
func TestADecorationThatFailsDoesNotFailTheListing(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			_, _ = w.Write([]byte(`{"id":9,"username":"ada","name":"Ada"}`))
		case strings.Contains(r.URL.Path, "/pipelines"), strings.Contains(r.URL.Path, "/members/"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"403"}`))
		case strings.Contains(r.URL.Path, "/subgroups"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`[{"id":11,"name":"api","path_with_namespace":"acme/api"}]`))
		}
	})

	children, err := fake.forge(t).Children(context.Background(), "7", forge.BrowseOptions{Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v — a refused decoration must not fail the listing", err)
	}
	if len(children.Repositories) != 1 {
		t.Fatalf("got %d repositories, want 1", len(children.Repositories))
	}
	if children.Repositories[0].Role != "" || children.Repositories[0].CIStatus != "" {
		t.Errorf("a refused decoration invented a value: %+v", children.Repositories[0])
	}
}

// TestADecorationIsSkippedWhenTheUserCannotBeResolved — asking per row would
// then make one failing request per row for a column that cannot be filled.
func TestADecorationIsSkippedWhenTheUserCannotBeResolved(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"401"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":1,"name":"one","full_path":"one"}]`))
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{Decorated: true})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 1 || got[0].Role != "" {
		t.Errorf("namespaces = %+v, want one with no role", got)
	}
	if n := fake.countPaths("/members/"); n != 0 {
		t.Errorf("%d member requests were made with no user to ask about, want 0", n)
	}
}
