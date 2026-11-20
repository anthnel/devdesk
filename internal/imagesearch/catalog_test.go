package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// These tests pin CatalogSearcher (DEVDESK-306 step 5) with a fake List: the
// client-side filter, the prefix rules, the row cap and the partial mark.

// fakeList records its calls and returns a canned walk.
type fakeList struct {
	names     []string
	truncated bool
	err       error

	calls              int
	apiURL, user, pass string
	catalogPrefix      string
	maxPages, pageSize int
}

func (f *fakeList) list(_ context.Context, apiURL, catalogPrefix, username, password string, maxPages, pageSize int) ([]string, bool, error) {
	f.calls++
	f.apiURL, f.catalogPrefix, f.user, f.pass, f.maxPages, f.pageSize = apiURL, catalogPrefix, username, password, maxPages, pageSize
	return f.names, f.truncated, f.err
}

func searchWith(f *fakeList, tg Target, q Query) ([]Image, bool, error) {
	return CatalogSearcher{List: f.list}.Search(context.Background(), tg, q)
}

func TestCatalogConstants(t *testing.T) {
	if CatalogMaxPages != 10 || CatalogPageSize != 1000 || CatalogMaxRows != 100 || CatalogMinQueryRunes != 2 {
		t.Errorf("constants = %d %d %d %d, want 10 1000 100 2",
			CatalogMaxPages, CatalogPageSize, CatalogMaxRows, CatalogMinQueryRunes)
	}
}

func TestCatalogQueryTooShort(t *testing.T) {
	for _, text := range []string{"", "a", " a ", "é", "  "} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			f := &fakeList{names: []string{"app"}}
			imgs, partial, err := searchWith(f, Target{}, Query{Text: text})
			if !errors.Is(err, ErrQueryTooShort) {
				t.Errorf("err = %v, want ErrQueryTooShort", err)
			}
			if imgs != nil || partial {
				t.Errorf("got %v partial=%v, want nil false", imgs, partial)
			}
			if f.calls != 0 {
				t.Errorf("List called %d times, want 0", f.calls)
			}
		})
	}
	t.Run("two runes walk", func(t *testing.T) {
		f := &fakeList{names: []string{"ab"}}
		if _, _, err := searchWith(f, Target{}, Query{Text: "ab"}); err != nil {
			t.Fatal(err)
		}
		if f.calls != 1 {
			t.Errorf("List called %d times, want 1", f.calls)
		}
	})
}

func TestCatalogListArguments(t *testing.T) {
	f := &fakeList{names: []string{"app"}}
	tg := Target{APIURL: "https://reg.example", Username: "u", Password: "p"}
	if _, _, err := searchWith(f, tg, Query{Text: "app"}); err != nil {
		t.Fatal(err)
	}
	if f.apiURL != tg.APIURL || f.user != "u" || f.pass != "p" || f.maxPages != 10 || f.pageSize != 1000 {
		t.Errorf("List got %q %q %q %d %d", f.apiURL, f.user, f.pass, f.maxPages, f.pageSize)
	}
	if f.catalogPrefix != "" {
		t.Errorf("List got catalogPrefix %q, want empty: a root walk", f.catalogPrefix)
	}
}

func TestCatalogSearch(t *testing.T) {
	type row struct{ name, repo string }
	cases := []struct {
		name      string
		target    Target
		text      string
		names     []string
		truncated bool
		want      []row
		partial   bool
	}{
		{
			name: "case-insensitive filter, Hub fields empty",
			text: "NGinx", names: []string{"nginx", "bitnami/nginx", "redis"},
			want: []row{{"nginx", "nginx"}, {"bitnami/nginx", "bitnami/nginx"}},
		},
		// The prefix rows below use a neutral "myproject": the filter is generic (a
		// Harbor project or a GitLab group does prefix its catalog names), but
		// Nexus 3.98.1 never returns names prefixed by the repository (NX-03, H1).
		// What Nexus does return is replayed by TestCatalogOnMeasuredNexusAnswers
		// (catalog_fixture_test.go).
		{
			name:   "prefix stripped from Name, kept once in Repo",
			target: Target{RepoPrefix: "myproject"},
			text:   "api", names: []string{"myproject/api", "other/api"},
			want: []row{{"api", "myproject/api"}},
		},
		{
			name:   "prefix typed with slashes",
			target: Target{RepoPrefix: "/myproject/"},
			text:   "api", names: []string{"myproject/api", "other/api"},
			want: []row{{"api", "myproject/api"}},
		},
		{
			name:   "filter does not see the prefix",
			target: Target{RepoPrefix: "myproject"},
			text:   "project", names: []string{"myproject/api"},
		},
		{
			name:   "prefix present, nothing matches",
			target: Target{RepoPrefix: "myproject"},
			text:   "zzz", names: []string{"myproject/api"},
		},
		{
			name:   "prefix present, only invalid names under it",
			target: Target{RepoPrefix: "p"},
			text:   "ngi", names: []string{"p/NGI", "p/-ngi", "p/a\x1b[31mngi"},
		},
		{
			name: "empty catalog", text: "nginx", names: nil,
		},
		{
			name: "empty catalog truncated keeps the mark", text: "nginx", names: nil, truncated: true, partial: true,
		},
		{
			name: "truncated with few rows is partial", text: "nginx", names: []string{"nginx"}, truncated: true,
			want: []row{{"nginx", "nginx"}}, partial: true,
		},
		{
			name:   "truncated, prefix absent: no result, partial (Q1)",
			target: Target{RepoPrefix: "myproject"},
			text:   "api", names: []string{"other/api"}, truncated: true, partial: true,
		},
		{
			name:   "truncated, prefix present but no match",
			target: Target{RepoPrefix: "myproject"},
			text:   "zzz", names: []string{"myproject/api"}, truncated: true, partial: true,
		},
		{
			name: "sorted by tier, then name, then repo",
			text: "web", names: []string{"x/webby", "x/web", "webapp", "web", "a/web"},
			want: []row{{"web", "web"}, {"a/web", "a/web"}, {"x/web", "x/web"}, {"webapp", "webapp"}, {"x/webby", "x/webby"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeList{names: tc.names, truncated: tc.truncated}
			imgs, partial, err := searchWith(f, tc.target, Query{Text: tc.text})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			var got []row
			for _, im := range imgs {
				got = append(got, row{im.Name, im.Repo})
				if im.Description != "" || im.Stars != 0 || im.HasStars || im.Official {
					t.Errorf("%s: Hub field set: %+v", im.Repo, im)
				}
				if im.EntryKey != tc.target.EntryKey {
					t.Errorf("EntryKey = %q, want %q", im.EntryKey, tc.target.EntryKey)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
			if partial != tc.partial {
				t.Errorf("partial = %v, want %v", partial, tc.partial)
			}
		})
	}
}

func TestCatalogEntryKeyCopied(t *testing.T) {
	f := &fakeList{names: []string{"app"}}
	imgs, _, err := searchWith(f, Target{EntryKey: "k1"}, Query{Text: "app"})
	if err != nil || len(imgs) != 1 || imgs[0].EntryKey != "k1" {
		t.Errorf("imgs = %+v err = %v, want EntryKey k1", imgs, err)
	}
}

// The prefix is a neutral "myproject": see the note in TestCatalogSearch. The
// refusal on a real, unprefixed Nexus catalog is in TestCatalogOnMeasuredNexusAnswers.
func TestCatalogPrefixNotInCatalog(t *testing.T) {
	tg := Target{RepoPrefix: "myproject"}
	t.Run("complete walk is an error", func(t *testing.T) {
		f := &fakeList{names: []string{"other/api", "other/web"}}
		imgs, partial, err := searchWith(f, tg, Query{Text: "api"})
		if !errors.Is(err, ErrPrefixNotInCatalog) {
			t.Fatalf("err = %v, want ErrPrefixNotInCatalog", err)
		}
		if !strings.Contains(err.Error(), "myproject") {
			t.Errorf("message %q lacks the prefix", err)
		}
		if imgs != nil || partial {
			t.Errorf("got %v partial=%v, want nil false", imgs, partial)
		}
		if got := Classify(err, 0, partial); got.Verdict != VerdictRefused {
			t.Errorf("verdict = %v, want refused", got.Verdict)
		}
	})
	t.Run("truncated walk is no result, partial", func(t *testing.T) {
		f := &fakeList{names: []string{"other/api", "other/web"}, truncated: true}
		imgs, partial, err := searchWith(f, tg, Query{Text: "api"})
		if err != nil || imgs != nil || !partial {
			t.Errorf("got %v partial=%v err=%v, want nil true nil", imgs, partial, err)
		}
		if got := Classify(err, len(imgs), partial); got != (Outcome{Verdict: VerdictNoResult, Partial: true}) {
			t.Errorf("outcome = %+v", got)
		}
	})
}

func TestCatalogListError(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeList{names: []string{"nginx"}, truncated: true, err: boom}
	imgs, partial, err := searchWith(f, Target{}, Query{Text: "nginx"})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if imgs != nil || partial {
		t.Errorf("got %v partial=%v, want nil false", imgs, partial)
	}
}

func TestCatalogRowCap(t *testing.T) {
	var names []string
	for i := range 249 {
		names = append(names, fmt.Sprintf("nginx-%03d", i))
	}
	names = append(names, "nginx") // the exact match comes last in the catalog
	t.Run("250 matches keep 100, exact first", func(t *testing.T) {
		f := &fakeList{names: names}
		imgs, partial, err := searchWith(f, Target{}, Query{Text: "nginx"})
		if err != nil {
			t.Fatal(err)
		}
		if len(imgs) != 100 || !partial {
			t.Fatalf("rows = %d partial = %v, want 100 true", len(imgs), partial)
		}
		if imgs[0].Name != "nginx" {
			t.Errorf("first = %q, want nginx", imgs[0].Name)
		}
	})
	t.Run("Limit below 100 lowers the cap", func(t *testing.T) {
		f := &fakeList{names: names}
		imgs, partial, err := searchWith(f, Target{}, Query{Text: "nginx", Limit: 5})
		if err != nil || len(imgs) != 5 || !partial {
			t.Errorf("rows = %d partial = %v err = %v, want 5 true nil", len(imgs), partial, err)
		}
	})
	t.Run("Limit above 100 does not raise the cap", func(t *testing.T) {
		f := &fakeList{names: names}
		imgs, _, err := searchWith(f, Target{}, Query{Text: "nginx", Limit: 500})
		if err != nil || len(imgs) != 100 {
			t.Errorf("rows = %d err = %v, want 100 nil", len(imgs), err)
		}
	})
	t.Run("exactly 100 matches is not cut", func(t *testing.T) {
		f := &fakeList{names: names[:100]}
		imgs, partial, err := searchWith(f, Target{}, Query{Text: "nginx"})
		if err != nil || len(imgs) != 100 || partial {
			t.Errorf("rows = %d partial = %v err = %v, want 100 false nil", len(imgs), partial, err)
		}
	})
}

// Names a registry may return but that are not valid repository names are
// never rendered: no control byte or ANSI sequence reaches the table.
func TestCatalogInvalidNamesNeverRendered(t *testing.T) {
	f := &fakeList{names: []string{
		"-rm", "-nginx", "UPPER", "NGINX", "a//nginx", "a//b",
		"nginx\x1b[31m", "\x1b]0;pwned\x07nginx", "ngi\x00nx", "nginx\n", "ng inx", "nginx/", "/nginx",
		"nginx",
	}}
	imgs, _, err := searchWith(f, Target{}, Query{Text: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 || imgs[0].Name != "nginx" || imgs[0].Repo != "nginx" {
		t.Fatalf("rows = %+v, want only nginx", imgs)
	}
	for _, im := range imgs {
		for _, r := range im.Name + im.Repo {
			if r < 0x20 || r == 0x7f {
				t.Errorf("control rune %U in %+v", r, im)
			}
		}
	}
}

// DEVDESK-579 (NX-12), step 2: a Nexus group member's catalog is served under its
// own path, /v2/<prefix>/_catalog, and lists names relative to it. A target with
// CatalogUnderPrefix walks that catalog (the lister gets the prefix), applies no
// R1 (no name is expected to carry the prefix) and rebuilds Repo as
// "<prefix>/<name>". Without the flag nothing changes: root catalog and R1.

// memberCatalog is what a Nexus hosted member lists, measured: unprefixed names.
var memberCatalog = []string{"api", "shared/base", "team/api", "tools/cli"}

func underPrefixTarget(prefix string) Target {
	return Target{EntryKey: "k-" + prefix, APIURL: "https://nexus.example", RepoPrefix: prefix, CatalogUnderPrefix: true}
}

func TestCatalogUnderPrefixRows(t *testing.T) {
	type row struct{ name, repo string }
	cases := []struct {
		name      string
		prefix    string
		text      string
		names     []string
		truncated bool
		want      []row
		partial   bool
	}{
		{
			name: "names are relative, Repo is rebuilt", prefix: "docker-hosted", text: "api", names: memberCatalog,
			want: []row{{"api", "docker-hosted/api"}, {"team/api", "docker-hosted/team/api"}},
		},
		{
			name: "a nested prefix is kept whole", prefix: "a/b", text: "api", names: memberCatalog,
			want: []row{{"api", "a/b/api"}, {"team/api", "a/b/team/api"}},
		},
		{
			// No R1: a name that looks prefixed is a name of the member, not a prefix to
			// strip. The prefix is the member's own path, never part of the listing.
			name: "a name that starts with the prefix is not stripped", prefix: "docker-hosted", text: "api",
			names: []string{"docker-hosted/api"},
			want:  []row{{"docker-hosted/api", "docker-hosted/docker-hosted/api"}},
		},
		{
			name: "names of other projects are not dropped", prefix: "docker-proxy", text: "mirror",
			names: []string{"mirror/cached", "mirror/uncached", "shared/base"},
			want:  []row{{"mirror/cached", "docker-proxy/mirror/cached"}, {"mirror/uncached", "docker-proxy/mirror/uncached"}},
		},
		{
			name: "the filter does not see the prefix", prefix: "docker-hosted", text: "hosted", names: memberCatalog,
		},
		{
			name: "no match is no result", prefix: "docker-hosted", text: "zzz", names: memberCatalog,
		},
		{
			name: "invalid names are never rendered", prefix: "docker-hosted", text: "api",
			names: []string{"API", "-api", "a//api", "api\x1b[31m", "api"},
			want:  []row{{"api", "docker-hosted/api"}},
		},
		{
			name: "sorted by tier, then name", prefix: "p", text: "web",
			names: []string{"x/webby", "x/web", "webapp", "web"},
			want:  []row{{"web", "p/web"}, {"x/web", "p/x/web"}, {"webapp", "p/webapp"}, {"x/webby", "p/x/webby"}},
		},
		{
			name: "truncated keeps the partial mark", prefix: "docker-hosted", text: "api", names: memberCatalog,
			truncated: true, partial: true,
			want: []row{{"api", "docker-hosted/api"}, {"team/api", "docker-hosted/team/api"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeList{names: tc.names, truncated: tc.truncated}
			tg := underPrefixTarget(tc.prefix)
			imgs, partial, err := searchWith(f, tg, Query{Text: tc.text})
			if err != nil {
				t.Fatalf("err = %v, want nil (no R1 on a catalog read by path)", err)
			}
			var got []row
			for _, im := range imgs {
				got = append(got, row{im.Name, im.Repo})
				if im.EntryKey != tg.EntryKey {
					t.Errorf("EntryKey = %q, want %q", im.EntryKey, tg.EntryKey)
				}
				if im.Description != "" || im.Stars != 0 || im.HasStars || im.Official {
					t.Errorf("%s: Hub field set: %+v", im.Repo, im)
				}
				if !validRepoPath(im.Repo) {
					t.Errorf("Repo %q is not a valid repository path", im.Repo)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
			if partial != tc.partial {
				t.Errorf("partial = %v, want %v", partial, tc.partial)
			}
			if f.calls != 1 {
				t.Errorf("List called %d times, want 1", f.calls)
			}
		})
	}
}

// The member answered, so an empty catalog is "no result", never a refusal: the
// prefix is not looked for in the names.
func TestCatalogUnderPrefixEmptyCatalogIsNoResult(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprintf("truncated=%v", truncated), func(t *testing.T) {
			f := &fakeList{names: nil, truncated: truncated}
			imgs, partial, err := searchWith(f, underPrefixTarget("docker-hosted"), Query{Text: "api"})
			if err != nil || imgs != nil || partial != truncated {
				t.Fatalf("got %v partial=%v err=%v, want nil %v nil", imgs, partial, err, truncated)
			}
			want := Outcome{Verdict: VerdictNoResult, Partial: truncated}
			if got := Classify(err, len(imgs), partial); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
		})
	}
}

// R1 is not applied: whatever the names, ErrPrefixNotInCatalog never comes back.
func TestCatalogUnderPrefixNeverReportsAMissingPrefix(t *testing.T) {
	f := &fakeList{names: []string{"other/api", "web"}}
	imgs, _, err := searchWith(f, underPrefixTarget("docker-hosted"), Query{Text: "api"})
	if errors.Is(err, ErrPrefixNotInCatalog) {
		t.Fatalf("err = %v: R1 was applied to a catalog read by path", err)
	}
	if err != nil || len(imgs) != 1 || imgs[0].Repo != "docker-hosted/other/api" {
		t.Errorf("imgs = %+v err = %v, want docker-hosted/other/api", imgs, err)
	}
}

// The lister walks /v2/<catalogPrefix>/_catalog; a root walk passes "".
func TestCatalogListerReceivesTheCatalogPrefix(t *testing.T) {
	cases := []struct {
		name   string
		target Target
		want   string
	}{
		{"under the prefix", underPrefixTarget("docker-hosted"), "docker-hosted"},
		{"nested prefix", underPrefixTarget("a/b"), "a/b"},
		{"root walk with a prefix filter keeps the root", Target{RepoPrefix: "docker-hosted"}, ""},
		{"root walk without a prefix", Target{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeList{names: []string{"docker-hosted/api"}}
			tc.target.APIURL = "https://nexus.example"
			if _, _, err := searchWith(f, tc.target, Query{Text: "api"}); err != nil {
				t.Fatal(err)
			}
			if f.calls != 1 || f.catalogPrefix != tc.want {
				t.Errorf("List called %d times with catalogPrefix %q, want once with %q", f.calls, f.catalogPrefix, tc.want)
			}
			if f.apiURL != "https://nexus.example" {
				t.Errorf("apiURL = %q: the prefix must not be folded into it", f.apiURL)
			}
		})
	}
}

// Without CatalogUnderPrefix a prefixed entry is unchanged to the byte: the root
// catalog, R1 comprised.
func TestCatalogWithoutTheFlagKeepsRootAndR1(t *testing.T) {
	t.Run("names carrying the prefix", func(t *testing.T) {
		f := &fakeList{names: []string{"docker-hosted/api", "other/api"}}
		imgs, _, err := searchWith(f, Target{RepoPrefix: "docker-hosted"}, Query{Text: "api"})
		if err != nil || len(imgs) != 1 || imgs[0].Name != "api" || imgs[0].Repo != "docker-hosted/api" {
			t.Fatalf("imgs = %+v err = %v, want {api, docker-hosted/api}", imgs, err)
		}
		if f.catalogPrefix != "" {
			t.Errorf("catalogPrefix = %q, want empty", f.catalogPrefix)
		}
	})
	t.Run("names not carrying the prefix are refused", func(t *testing.T) {
		f := &fakeList{names: memberCatalog}
		_, _, err := searchWith(f, Target{RepoPrefix: "docker-hosted"}, Query{Text: "api"})
		if !errors.Is(err, ErrPrefixNotInCatalog) {
			t.Fatalf("err = %v, want ErrPrefixNotInCatalog", err)
		}
	})
}

// An empty or invalid prefix is refused before any request: the catalog path is
// built from it. The cause names the prefix, quoted, so no control byte reaches
// the screen.
func TestCatalogUnderPrefixRefusesAnInvalidPrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"empty", ""},
		{"only slashes", "///"},
		{"dot-dot", ".."},
		{"dot-dot inside", "a/../b"},
		{"dot", "."},
		{"dot segment", "a/./b"},
		{"empty segment", "a//b"},
		{"leading slash", "/a"},
		{"trailing slash", "a/"},
		{"uppercase", "Docker-Hosted"},
		{"space", "my repo"},
		{"percent", "a%2fb"},
		{"backslash", `a\b`},
		{"query", "a?b"},
		{"control character", "a\x1b[31m"},
		{"NUL", "a\x00b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeList{names: memberCatalog}
			imgs, partial, err := searchWith(f, underPrefixTarget(tc.prefix), Query{Text: "api"})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if imgs != nil || partial {
				t.Errorf("got %v partial=%v, want nil false", imgs, partial)
			}
			if f.calls != 0 {
				t.Errorf("List called %d times, want 0: the path must not be built from it", f.calls)
			}
			if got := Classify(err, 0, partial); got != (Outcome{Verdict: VerdictRefused}) {
				t.Errorf("outcome = %+v, want refused", got)
			}
			for _, r := range err.Error() {
				if r < 0x20 || r == 0x7f {
					t.Errorf("control rune %U in the cause %q", r, err)
				}
			}
			if q := fmt.Sprintf("%q", tc.prefix); tc.prefix != "" && !strings.Contains(err.Error(), q) {
				t.Errorf("cause %q does not name the prefix as %s", err, q)
			}
		})
	}
}

// What the walk says about a member comes through unchanged, so Classify tells a
// refusal from a failure. A broken member never turns into rows or into a
// verdict about another target (that part is in catalog_under_http_test.go).
func TestCatalogUnderPrefixListErrorsKeepTheirVerdict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Verdict
	}{
		{"404, a group on the HTTP port", &oci.StatusError{Code: 404, Source: oci.SourceRegistry}, VerdictRefused},
		{"401", &oci.StatusError{Code: 401, Source: oci.SourceRegistry}, VerdictRefused},
		{"403", &oci.StatusError{Code: 403, Source: oci.SourceRegistry}, VerdictRefused},
		{"405", &oci.StatusError{Code: 405, Source: oci.SourceRegistry}, VerdictRefused},
		{"500", &oci.StatusError{Code: 500, Source: oci.SourceRegistry}, VerdictFailed},
		{"503", &oci.StatusError{Code: 503, Source: oci.SourceRegistry}, VerdictFailed},
		{"429", &oci.StatusError{Code: 429, Source: oci.SourceRegistry}, VerdictFailed},
		{"network", errors.New("connection reset"), VerdictFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeList{names: memberCatalog, truncated: true, err: tc.err}
			imgs, partial, err := searchWith(f, underPrefixTarget("docker-group"), Query{Text: "api"})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if imgs != nil || partial {
				t.Errorf("got %v partial=%v, want nil false", imgs, partial)
			}
			if got := Classify(err, len(imgs), partial); got != (Outcome{Verdict: tc.want}) {
				t.Errorf("outcome = %+v, want %v", got, tc.want)
			}
		})
	}
}
