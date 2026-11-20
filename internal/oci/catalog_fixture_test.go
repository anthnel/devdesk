package oci_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-580: the catalog walk replayed against the pages NX-03 measured. DevDesk
// asks for n=1000, so on the six names of the bench it never pages; these tests
// call the walker with a page size of 1 or 2 to follow the Link headers Nexus
// really sent, with the last name escaped ("last=shared%2Fbase"). They lock the
// measurement: they pass as soon as the fixtures are there, and fail when a page
// of the measured chain is missing.

const (
	groupHost = "127.0.0.1:30482" // the group connector
	pathHost  = "127.0.0.1:30481" // the HTTP port, repositories by path
)

// pageFakeTB captures the errors of a fake so that a test can expect them. It
// embeds the real T for everything else (Cleanup stops the fake servers).
type pageFakeTB struct {
	testing.TB
	errs []string
}

func (f *pageFakeTB) Helper() {}

func (f *pageFakeTB) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

// measuredPages loads the named catalog fixtures (without the "--admin" suffix),
// in the order given, and fails the test on a name that is not embedded.
func measuredPages(t *testing.T, names ...string) []nexusfixture.Fixture {
	t.Helper()
	all, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	var out []nexusfixture.Fixture
	for _, name := range names {
		i := slices.IndexFunc(all, func(f nexusfixture.Fixture) bool { return f.Name == name+"--admin" })
		if i < 0 {
			t.Fatalf("the embedded catalog has no fixture %s--admin", name)
		}
		out = append(out, all[i])
	}
	return out
}

func pageNames(prefix string, n int) []string {
	var names []string
	for k := range n {
		names = append(names, fmt.Sprintf("%s-p%d", prefix, k))
	}
	return names
}

func walk(t *testing.T, base, prefix string, pageSize int) ([]string, bool, error) {
	t.Helper()
	return oci.ListCatalogUnderContext(context.Background(), base, prefix,
		fakenexus.User(nexusfixture.Admin), fakenexus.Password, 10, pageSize)
}

func requireHits(t *testing.T, fake *fakenexus.Server, names []string, want int) {
	t.Helper()
	total := 0
	for _, name := range names {
		got := fake.Hits(name + "--admin")
		total += got
		if got != want {
			t.Errorf("Hits(%s--admin) = %d, want %d", name, got, want)
		}
	}
	if total != want*len(names) {
		t.Errorf("%d requests in all, want %d", total, want*len(names))
	}
}

func TestCatalogWalkFollowsEveryMeasuredPage(t *testing.T) {
	pages := pageNames("page-group-n1", 6)

	t.Run("six pages of one name", func(t *testing.T) {
		fake := fakenexus.New(t, measuredPages(t, pages...)...)
		names, truncated, err := walk(t, fake.URL(groupHost), "", 1)
		if err != nil {
			t.Fatalf("ListCatalogUnderContext: %v", err)
		}
		want := []string{"api", "mirror/cached", "mirror/uncached", "shared/base", "team/api", "tools/cli"}
		if !slices.Equal(names, want) {
			t.Errorf("names = %v, want %v (the order Nexus sent)", names, want)
		}
		if truncated {
			t.Error("truncated = true, want false: the last page has no Link")
		}
		requireHits(t, fake, pages, 1)
	})

	t.Run("a lost page", func(t *testing.T) {
		lost := slices.DeleteFunc(slices.Clone(pages), func(n string) bool { return n == "page-group-n1-p3" })
		spy := &pageFakeTB{TB: t}
		fake := fakenexus.New(spy, measuredPages(t, lost...)...)
		names, _, err := walk(t, fake.URL(groupHost), "", 1)
		var statusErr *oci.StatusError
		if !errors.As(err, &statusErr) || statusErr.Code != 501 {
			t.Fatalf("err = %v, want a *oci.StatusError 501 for the page no fixture answers", err)
		}
		if len(names) != 0 {
			t.Errorf("names = %v, want none on an error", names)
		}
		if len(spy.errs) != 1 {
			t.Errorf("the fake reported %d unrecorded request(s) %q, want exactly the one for page 3", len(spy.errs), spy.errs)
		}
	})
}

// Nexus pages a group inconsistently (NX-03): a name comes back on two pages.
// DEVDESK-629 (F8): the walker drops the repeats in silence and keeps the order
// of first appearance; the frozen note describes the walker before it did.
func TestCatalogWalkDeduplicatesTheMeasuredGroupN2Answer(t *testing.T) {
	pages := pageNames("page-group-n2", 5)
	fake := fakenexus.New(t, measuredPages(t, pages...)...)
	names, truncated, err := walk(t, fake.URL(groupHost), "", 2)
	if err != nil {
		t.Fatalf("ListCatalogUnderContext: %v", err)
	}
	want := []string{"api", "mirror/cached", "mirror/uncached", "shared/base", "team/api", "tools/cli"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v (each name once, first-appearance order)", names, want)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
	for _, name := range want {
		if got := count(names, name); got != 1 {
			t.Errorf("%s appears %d times, want 1", name, got)
		}
	}
	requireHits(t, fake, pages, 1)
}

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

// A member read by its path keeps its Link on that path: the measured next link
// of docker-hosted and docker-proxy stays under /v2/<repository>/_catalog.
func TestCatalogWalkUnderAMemberFollowsItsMeasuredPages(t *testing.T) {
	tests := []struct {
		prefix string
		pages  []string
		want   []string
	}{
		{
			prefix: "docker-hosted",
			pages:  pageNames("page-hosted-n2", 2),
			want:   []string{"api", "shared/base", "team/api", "tools/cli"},
		},
		{
			prefix: "docker-proxy",
			pages:  pageNames("page-proxy-n2", 2),
			want:   []string{"mirror/cached", "mirror/uncached", "shared/base"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			fake := fakenexus.New(t, measuredPages(t, tt.pages...)...)
			names, truncated, err := walk(t, fake.URL(pathHost), tt.prefix, 2)
			if err != nil {
				t.Fatalf("ListCatalogUnderContext: %v", err)
			}
			if !slices.Equal(names, tt.want) {
				t.Errorf("names = %v, want %v", names, tt.want)
			}
			if truncated {
				t.Error("truncated = true, want false: the walk left the member's path or stopped early")
			}
			requireHits(t, fake, tt.pages, 1)
		})
	}
}
