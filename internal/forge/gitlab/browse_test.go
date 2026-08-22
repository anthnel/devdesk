package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/forge"
)

// page writes a paginated list response, telling the client whether another
// page follows the way GitLab does — with an X-Next-Page header.
func page(w http.ResponseWriter, body string, next int) {
	if next > 0 {
		w.Header().Set("X-Next-Page", fmt.Sprint(next))
	}
	_, _ = w.Write([]byte(body))
}

// TestEveryPageOfAListIsWalked is D34: four call sites asked for page 1 and
// kept what came back, so a group with more than a hundred children was cut
// with nothing saying so.
func TestEveryPageOfAListIsWalked(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			page(w, `[{"id":1,"name":"one","full_path":"one"}]`, 2)
		case "2":
			page(w, `[{"id":2,"name":"two","full_path":"two"}]`, 3)
		default:
			page(w, `[{"id":3,"name":"three","full_path":"three"}]`, 0)
		}
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d namespaces across three pages, want 3: %+v", len(got), got)
	}
	if got[2].Name != "three" {
		t.Errorf("the last page was dropped: got %q, want %q", got[2].Name, "three")
	}
}

// TestAServerPointingBackAtThePageItServedTerminates is the runaway guard. A
// NextPage that does not advance would spin for ever against `!= 0` alone.
func TestAServerPointingBackAtThePageItServedTerminates(t *testing.T) {
	fake := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		page(w, `[{"id":1,"name":"one","full_path":"one"}]`, 1)
	})

	got, err := fake.forge(t).RootNamespaces(context.Background(), forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("RootNamespaces() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d namespaces, want 1 — the walk did not stop", len(got))
	}
}

// TestAnUndecoratedListingCostsNothingExtra is the property the clone's walk
// depends on: no role lookup, no pipeline lookup, whatever the row count.
func TestAnUndecoratedListingCostsNothingExtra(t *testing.T) {
	fake := newFakeGitLab(t, childrenHandler)

	children, err := fake.forge(t).Children(context.Background(), "7", forge.BrowseOptions{})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if len(children.Repositories) != 2 {
		t.Fatalf("got %d repositories, want 2", len(children.Repositories))
	}

	if n := fake.countPaths("/members/"); n != 0 {
		t.Errorf("an undecorated listing made %d member requests, want 0", n)
	}
	if n := fake.countPaths("/pipelines"); n != 0 {
		t.Errorf("an undecorated listing made %d pipeline requests, want 0", n)
	}
	if children.Repositories[0].Role != "" || children.Repositories[0].CIStatus != "" {
		t.Errorf("an undecorated repository carries a role or a CI status: %+v", children.Repositories[0])
	}
}

// TestADecoratedListingResolvesTheUserOnce pins the cost of the decoration.
// Asking who the caller is inside the loop would add one request per row, on
// top of the two per repository the decoration already costs.
func TestADecoratedListingResolvesTheUserOnce(t *testing.T) {
	fake := newFakeGitLab(t, childrenHandler)

	children, err := fake.forge(t).Children(context.Background(), "7", forge.BrowseOptions{Decorated: true})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}

	if n := fake.countPaths("/user"); n != 1 {
		t.Errorf("the current user was fetched %d times, want 1", n)
	}
	if got := children.Repositories[0].Role; got != "Maintainer" {
		t.Errorf("role = %q, want %q — an access level of 40 is a Maintainer", got, "Maintainer")
	}
	if got := children.Repositories[0].CIStatus; got != "success" {
		t.Errorf("CI status = %q, want %q", got, "success")
	}
	if got := children.Namespaces[0].Role; got != "Owner" {
		t.Errorf("namespace role = %q, want %q", got, "Owner")
	}
}

// TestArchivedRepositoriesAreExcludedUnlessAsked is what
// gitlab.pull.include_archived means, and the only place it is read.
func TestArchivedRepositoriesAreExcludedUnlessAsked(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     forge.BrowseOptions
		wantSent bool
	}{
		{"browsing shows what is there", forge.BrowseOptions{IncludeArchived: true}, false},
		{"a clone excludes archived", forge.BrowseOptions{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGitLab(t, childrenHandler)
			if _, err := fake.forge(t).Children(context.Background(), "7", tc.opts); err != nil {
				t.Fatalf("Children() error = %v", err)
			}

			sent := false
			for _, call := range fake.calls() {
				if strings.Contains(call.Path, "/projects") && call.Query.Get("archived") == "false" {
					sent = true
				}
			}
			if sent != tc.wantSent {
				t.Errorf("archived=false sent = %v, want %v", sent, tc.wantSent)
			}
		})
	}
}

// TestAnAccessLevelBecomesAWord is the humanisation the interface promises: a
// backend returns a role, never a number, because GitLab's levels and GitHub's
// words do not align one to one.
func TestAnAccessLevelBecomesAWord(t *testing.T) {
	for _, tc := range []struct {
		level int
		want  string
	}{
		{50, "Owner"}, {40, "Maintainer"}, {30, "Developer"},
		{20, "Reporter"}, {10, "Guest"},
		{45, "Maintainer"}, // between two levels, rounds down
		{0, ""},            // not known is not the lowest role there is
		{5, ""},
	} {
		if got := roleName(tc.level); got != tc.want {
			t.Errorf("roleName(%d) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

// childrenHandler serves one group with one subgroup and two projects, plus the
// decoration endpoints.
func childrenHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/user"):
		_, _ = w.Write([]byte(`{"id":9,"username":"ada","name":"Ada"}`))
	case strings.Contains(r.URL.Path, "/subgroups"):
		_, _ = w.Write([]byte(`[{"id":8,"name":"sub","full_path":"acme/sub"}]`))
	case strings.Contains(r.URL.Path, "/pipelines"):
		_, _ = w.Write([]byte(`[{"id":1,"status":"success"}]`))
	// The two member cases come before the list ones: a decoration path is
	// `/projects/11/members/all/9`, which contains `/projects` too.
	case strings.Contains(r.URL.Path, "/groups/") && strings.Contains(r.URL.Path, "/members/"):
		_, _ = w.Write([]byte(`{"id":9,"access_level":50}`))
	case strings.Contains(r.URL.Path, "/members/"):
		_, _ = w.Write([]byte(`{"id":9,"access_level":40}`))
	case strings.Contains(r.URL.Path, "/projects"):
		_, _ = w.Write([]byte(`[{"id":11,"name":"api","path_with_namespace":"acme/api"},{"id":12,"name":"web","path_with_namespace":"acme/web"}]`))
	default:
		http.NotFound(w, r)
	}
}
