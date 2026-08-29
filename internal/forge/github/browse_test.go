package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/forge"
)

// page writes a paginated list response, telling the client whether another
// follows the way GitHub does — with a Link header.
func page(w http.ResponseWriter, body, nextURL string) {
	if nextURL != "" {
		w.Header().Set("Link", `<`+nextURL+`>; rel="next"`)
	}
	_, _ = w.Write([]byte(body))
}

// TestEveryPageOfAListIsWalked is D34's rule applied to the second backend.
// GitHub paginates by Link header rather than by X-Next-Page, so the loop is
// different and the property is the same: a list is complete or it is an error.
func TestEveryPageOfAListIsWalked(t *testing.T) {
	var base string
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v3/user") {
			_, _ = w.Write([]byte(`{"login":"ada","name":"Ada"}`))
			return
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			page(w, `[{"login":"one"}]`, base+"?page=2")
		case "2":
			page(w, `[{"login":"two"}]`, base+"?page=3")
		default:
			page(w, `[{"login":"three"}]`, "")
		}
	})
	base = fake.server.URL + "/api/v3/user/orgs"

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	// The personal namespace plus three pages of one organisation each.
	if len(got) != 4 {
		t.Fatalf("got %d namespaces, want 4: %+v", len(got), got)
	}
	if got[3].Name != "three" {
		t.Errorf("the last page was dropped: got %q", got[3].Name)
	}
}

// TestThePersonalAccountIsARootNamespace is the bug the first version of this
// backend shipped: a personal GitHub account belongs to no organisation, so the
// explorer opened empty and said so — on the account shape most people have.
//
// The reasoning that excluded it was wrong on its own terms. *No* GitHub
// organisation can be created or deleted through the API, so the personal
// account is not less capable than an organisation but more: it is the one
// namespace where a repository can be created and deleted.
func TestThePersonalAccountIsARootNamespace(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v3/user") {
			_, _ = w.Write([]byte(`{"login":"ada","name":"Ada Lovelace"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`)) // no organisations, the common case
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("an account with no organisation got %d namespaces, want its own: %+v", len(got), got)
	}

	personal := got[0]
	if personal.Path != "ada" || personal.Name != "Ada Lovelace" {
		t.Errorf("personal namespace = %+v, want the signed-in user", personal)
	}
	// The empty ID is the interface's own word for "the user's own namespace",
	// and it is what go-github's Create takes for the `org` argument. The login
	// would 404: GitHub refuses to treat a user as an organisation.
	if personal.ID != "" {
		t.Errorf("personal namespace ID = %q, want the empty string", personal.ID)
	}
	if personal.Role != "Owner" {
		t.Errorf("role = %q, want Owner — it is the account the token belongs to", personal.Role)
	}
}

// TestThePersonalAccountComesFirst — it is where a solo account's repositories
// are, and burying it under a list of organisations would hide the only row
// half of them have.
func TestThePersonalAccountComesFirst(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v3/user") {
			_, _ = w.Write([]byte(`{"login":"ada"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"login":"acme"},{"login":"beta"}]`))
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 3 || got[0].Path != "ada" {
		t.Errorf("namespaces = %+v, want the personal account first", got)
	}
}

// TestAUserThatCannotBeReadStillListsTheOrganizations — the organisations are
// worth showing on their own, so a failed CurrentUser drops the row rather than
// the listing.
func TestAUserThatCannotBeReadStillListsTheOrganizations(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v3/user") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"login":"acme"}]`))
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 1 || got[0].Path != "acme" {
		t.Errorf("namespaces = %+v, want the organisation alone", got)
	}
}

// TestThePersonalNamespaceListsWhatTheUserOwns — a different endpoint, and
// `Affiliation: owner` keeps it to what they own: without it the list also
// carries every repository they collaborate on, which belongs under whoever
// owns it and would appear twice in a tree that shows both.
func TestThePersonalNamespaceListsWhatTheUserOwns(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"dotfiles","full_name":"ada/dotfiles"}]`))
	})

	children, err := fake.forge(t).Children(context.Background(), "", forge.BrowseOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if len(children.Repositories) != 1 || children.Repositories[0].Path != "ada/dotfiles" {
		t.Fatalf("repositories = %+v", children.Repositories)
	}

	call := fake.calls()[0]
	if !strings.HasSuffix(call.Path, "/api/v3/user/repos") {
		t.Errorf("the listing addressed %q, want /user/repos", call.Path)
	}
	if got := call.Query.Get("affiliation"); got != "owner" {
		t.Errorf("affiliation = %q, want owner", got)
	}
}

// TestAnOrganizationHoldsNoOrganization is Shape.MaxNamespaceDepth of 1 seen
// from the data: the empty slice is the answer, not a missing feature.
func TestAnOrganizationHoldsNoOrganization(t *testing.T) {
	fake := newFakeGitHub(t, childrenHandler)

	children, err := fake.forge(t).Children(context.Background(), "acme", forge.BrowseOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if len(children.Namespaces) != 0 {
		t.Errorf("an organisation returned %d namespaces, want none", len(children.Namespaces))
	}
	if len(children.Repositories) != 2 {
		t.Fatalf("got %d repositories, want 2", len(children.Repositories))
	}
}

// TestArchivedRepositoriesAreDroppedUnlessAsked — GitHub has no filter for it,
// so the backend does it after the fact rather than pretending the option does
// not exist.
func TestArchivedRepositoriesAreDroppedUnlessAsked(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts forge.BrowseOptions
		want int
	}{
		{"browsing shows what is there", forge.BrowseOptions{IncludeArchived: true}, 2},
		{"a clone excludes archived", forge.BrowseOptions{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGitHub(t, childrenHandler)
			children, err := fake.forge(t).Children(context.Background(), "acme", tc.opts)
			if err != nil {
				t.Fatalf("Children() error = %v", err)
			}
			if len(children.Repositories) != tc.want {
				t.Errorf("got %d repositories, want %d: %+v", len(children.Repositories), tc.want, children.Repositories)
			}
		})
	}
}

// TestAnUndecoratedListingCostsNothingExtra is the property the clone's walk
// depends on, and it holds on both backends for the same reason.
func TestAnUndecoratedListingCostsNothingExtra(t *testing.T) {
	fake := newFakeGitHub(t, childrenHandler)

	children, err := fake.forge(t).Children(context.Background(), "acme", forge.BrowseOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}

	if n := fake.countPaths("/actions/runs"); n != 0 {
		t.Errorf("an undecorated listing made %d workflow requests, want 0", n)
	}
	if children.Repositories[0].Role != "" || children.Repositories[0].CIStatus != "" {
		t.Errorf("an undecorated repository carries a decoration: %+v", children.Repositories[0])
	}
}

// TestADecoratedListingResolvesTheUserOnce pins the cost of the decoration:
// the role lookups need the caller's login, and asking per row would add a
// request per row on top of the two per repository.
func TestADecoratedListingResolvesTheUserOnce(t *testing.T) {
	fake := newFakeGitHub(t, childrenHandler)

	children, err := fake.forge(t).Children(context.Background(), "acme",
		forge.BrowseOptions{IncludeArchived: true, Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}

	if n := fake.countPaths("/api/v3/user"); n != 1 {
		t.Errorf("the current user was fetched %d times, want 1: %v", n, fake.paths())
	}
	if got := children.Repositories[0].Role; got != "Maintainer" {
		t.Errorf("role = %q, want Maintainer — `maintain` is the permission the fake grants", got)
	}
	if got := children.Repositories[0].CIStatus; got != forge.CIStatusSuccess {
		t.Errorf("CI status = %q, want the last run's conclusion", got)
	}
}

// TestPermissionFlagsBecomeAWord is the humanisation the interface promises,
// and the reason it is the backend's job: GitHub returns a set of booleans
// where GitLab returns a number, and neither maps onto the other.
func TestPermissionFlagsBecomeAWord(t *testing.T) {
	for _, tc := range []struct {
		perms map[string]bool
		want  string
	}{
		{map[string]bool{"admin": true, "push": true, "pull": true}, "Owner"},
		{map[string]bool{"maintain": true, "push": true}, "Maintainer"},
		{map[string]bool{"push": true, "pull": true}, "Developer"},
		{map[string]bool{"triage": true, "pull": true}, "Reporter"},
		{map[string]bool{"pull": true}, "Guest"},
		// Not known is not the lowest role there is.
		{map[string]bool{}, ""},
		{nil, ""},
	} {
		if got := roleName(tc.perms); got != tc.want {
			t.Errorf("roleName(%v) = %q, want %q", tc.perms, got, tc.want)
		}
	}
}

// TestARunningWorkflowReportsItsStatus — a run in flight has no conclusion, and
// falling through to "" would make a repository whose build is running read as
// having no CI at all.
//
// It used to assert the raw "in_progress", which is how D66 stayed invisible:
// the test pinned GitHub's own word as if it were the interface's.
func TestARunningWorkflowReportsItsStatus(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v3/user"):
			_, _ = w.Write([]byte(`{"login":"ada","name":"Ada"}`))
		case strings.Contains(r.URL.Path, "/actions/runs"):
			_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1,"status":"in_progress","conclusion":""}]}`))
		case strings.Contains(r.URL.Path, "/memberships/"):
			_, _ = w.Write([]byte(`{"role":"member"}`))
		default:
			_, _ = w.Write([]byte(`[{"name":"api","full_name":"acme/api","owner":{"login":"acme"},"permissions":{"push":true}}]`))
		}
	})

	children, err := fake.forge(t).Children(context.Background(), "acme",
		forge.BrowseOptions{IncludeArchived: true, Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if got := children.Repositories[0].CIStatus; got != forge.CIStatusRunning {
		t.Errorf("CI status = %q, want %q — the run's status, translated", got, forge.CIStatusRunning)
	}
}

// TestAnEnterpriseInternalVisibilityIsReported — `Visibility` is the field
// Enterprise fills; falling back to the `Private` boolean alone would report
// `internal` as `private`, which is a different thing.
func TestAnEnterpriseInternalVisibilityIsReported(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"api","full_name":"acme/api","private":true,"visibility":"internal"}]`))
	})

	children, err := fake.forge(t).Children(context.Background(), "acme", forge.BrowseOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if got := children.Repositories[0].Visibility; got != "internal" {
		t.Errorf("visibility = %q, want internal", got)
	}
}

// TestAFailedListingIsAnError — an empty explorer must never mean "this
// organisation contains nothing". Same rule as the GitLab backend's, and it is
// the one that stops D20 reappearing a level up.
func TestAFailedListingIsAnError(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	})
	f := fake.forge(t)

	if _, err := f.RootNamespaces(context.Background(), forge.BrowseOptions{}); err == nil {
		t.Error("RootNamespaces() succeeded against a failing server")
	}
	if _, err := f.Children(context.Background(), "acme", forge.BrowseOptions{}); err == nil {
		t.Error("Children() succeeded against a failing server")
	}
	if _, err := f.CurrentUser(context.Background()); err == nil {
		t.Error("CurrentUser() succeeded against a failing server")
	}
}

// TestADecorationThatFailsDoesNotFailTheListing is the other direction, and
// deliberately: a column short beats an empty explorer.
func TestADecorationThatFailsDoesNotFailTheListing(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v3/user"):
			_, _ = w.Write([]byte(`{"login":"ada"}`))
		case strings.Contains(r.URL.Path, "/actions/runs"), strings.Contains(r.URL.Path, "/memberships/"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
		default:
			_, _ = w.Write([]byte(`[{"name":"api","full_name":"acme/api"}]`))
		}
	})

	children, err := fake.forge(t).Children(context.Background(), "acme",
		forge.BrowseOptions{IncludeArchived: true, Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v — a refused decoration must not fail the listing", err)
	}
	if len(children.Repositories) != 1 || children.Repositories[0].CIStatus != "" {
		t.Errorf("a refused decoration invented a value: %+v", children.Repositories)
	}
}

// TestTheUserIsMemoised — every decorated listing needs the login, and asking
// the host each time would add a request per listing the GitLab backend does
// not make either.
func TestTheUserIsMemoised(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"login":"ada","name":"Ada"}`))
	})
	f := fake.forge(t)

	for range 3 {
		if _, err := f.CurrentUser(context.Background()); err != nil {
			t.Fatalf("CurrentUser() error = %v", err)
		}
	}
	if n := len(fake.calls()); n != 1 {
		t.Errorf("three calls made %d requests, want 1", n)
	}
}

// childrenHandler serves one organisation with two repositories, one archived,
// plus the decoration endpoints.
func childrenHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/api/v3/user"):
		_, _ = w.Write([]byte(`{"login":"ada","name":"Ada"}`))
	case strings.Contains(r.URL.Path, "/actions/runs"):
		_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1,"status":"completed","conclusion":"success"}]}`))
	case strings.Contains(r.URL.Path, "/memberships/"):
		_, _ = w.Write([]byte(`{"role":"admin"}`))
	case strings.Contains(r.URL.Path, "/orgs/"):
		_, _ = fmt.Fprint(w, `[
			{"name":"api","full_name":"acme/api","owner":{"login":"acme"},"private":true,"permissions":{"maintain":true}},
			{"name":"legacy","full_name":"acme/legacy","owner":{"login":"acme"},"archived":true}
		]`)
	default:
		_, _ = w.Write([]byte(`[{"login":"acme","name":"Acme"}]`))
	}
}

// ── D66 : GitHub's words are not the interface's ─────────────────────────────

// Every value GitHub Actions can report, and what it becomes. The table is the
// documentation: a mapping argued in a comment and unchecked is what let
// `failure` reach the CI column as `failu…` for the life of the GitHub backend.
func TestEveryActionsOutcomeBecomesADeclaredStatus(t *testing.T) {
	for _, tc := range []struct {
		conclusion, status, want string
	}{
		// Conclusions — a finished run carries `completed` as its status, which
		// is why the status half of every one of these is ignored.
		{"success", "completed", forge.CIStatusSuccess},
		{"failure", "completed", forge.CIStatusFailed},
		{"timed_out", "completed", forge.CIStatusFailed},
		{"startup_failure", "completed", forge.CIStatusFailed},
		{"cancelled", "completed", forge.CIStatusCanceled},
		{"action_required", "completed", forge.CIStatusManual},
		{"skipped", "completed", forge.CIStatusSkipped},
		{"neutral", "completed", forge.CIStatusSkipped},
		{"stale", "completed", forge.CIStatusSkipped},

		// Run states — read only when there is no conclusion yet.
		{"", "in_progress", forge.CIStatusRunning},
		{"", "queued", forge.CIStatusPending},
		{"", "requested", forge.CIStatusPending},
		{"", "waiting", forge.CIStatusPending},
		{"", "pending", forge.CIStatusPending},

		// Nothing to say. An empty cell, never GitHub's own spelling.
		{"", "", ""},
		{"", "completed", ""},
		{"", "a_status_github_has_not_invented_yet", ""},
		{"a_conclusion_github_has_not_invented_yet", "completed", ""},
	} {
		if got := ciStatusOf(tc.conclusion, tc.status); got != tc.want {
			t.Errorf("ciStatusOf(%q, %q) = %q, want %q", tc.conclusion, tc.status, got, tc.want)
		}
	}
}

// The conclusion wins over the status, and this is why the mapper takes both:
// a completed run carries `completed` in one field and the verdict in the
// other, so reading either alone loses half the answer.
func TestAConclusionOutranksTheRunState(t *testing.T) {
	if got := ciStatusOf("failure", "completed"); got != forge.CIStatusFailed {
		t.Errorf("ciStatusOf(failure, completed) = %q, want %q", got, forge.CIStatusFailed)
	}
	if got := ciStatusOf("", "in_progress"); got != forge.CIStatusRunning {
		t.Errorf("a run with no conclusion should fall back to its status, got %q", got)
	}
}

// Nothing this backend emits may be a word the interface has not declared. That
// is the whole of D66 stated as an invariant: the previous code passed GitHub's
// vocabulary through untouched, and no test could see it because no test knew
// what the vocabulary was.
func TestTheBackendOnlyEmitsDeclaredStatuses(t *testing.T) {
	declared := map[string]bool{}
	for _, status := range forge.CIStatuses() {
		declared[status] = true
	}

	for _, table := range []map[string]string{ciConclusions, ciRunStates} {
		for from, to := range table {
			if !declared[to] {
				t.Errorf("%q maps to %q, which forge.CIStatuses() does not declare", from, to)
			}
		}
	}
}

// A failed build must not read as a warning. This is the symptom the user saw
// in the other direction — an unmapped value reaching the view — and the reason
// the fix belongs here rather than in a wider switch upstream.
func TestAFailedRunIsReportedAsFailed(t *testing.T) {
	fake := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v3/user"):
			_, _ = w.Write([]byte(`{"login":"ada","name":"Ada"}`))
		case strings.Contains(r.URL.Path, "/actions/runs"):
			_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":1,"status":"completed","conclusion":"failure"}]}`))
		case strings.Contains(r.URL.Path, "/memberships/"):
			_, _ = w.Write([]byte(`{"role":"member"}`))
		default:
			_, _ = w.Write([]byte(`[{"name":"api","full_name":"acme/api","owner":{"login":"acme"},"permissions":{"push":true}}]`))
		}
	})

	children, err := fake.forge(t).Children(context.Background(), "acme",
		forge.BrowseOptions{IncludeArchived: true, Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if got := children.Repositories[0].CIStatus; got != forge.CIStatusFailed {
		t.Errorf("CI status = %q, want %q — GitHub says \"failure\"", got, forge.CIStatusFailed)
	}
}
