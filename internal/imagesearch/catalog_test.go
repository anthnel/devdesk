package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
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
	maxPages, pageSize int
}

func (f *fakeList) list(_ context.Context, apiURL, username, password string, maxPages, pageSize int) ([]string, bool, error) {
	f.calls++
	f.apiURL, f.user, f.pass, f.maxPages, f.pageSize = apiURL, username, password, maxPages, pageSize
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
		{
			name:   "prefix stripped from Name, kept once in Repo",
			target: Target{RepoPrefix: "docker-hosted"},
			text:   "api", names: []string{"docker-hosted/api", "other/api"},
			want: []row{{"api", "docker-hosted/api"}},
		},
		{
			name:   "prefix typed with slashes",
			target: Target{RepoPrefix: "/docker-hosted/"},
			text:   "api", names: []string{"docker-hosted/api", "other/api"},
			want: []row{{"api", "docker-hosted/api"}},
		},
		{
			name:   "filter does not see the prefix",
			target: Target{RepoPrefix: "docker-hosted"},
			text:   "hosted", names: []string{"docker-hosted/api"},
		},
		{
			name:   "prefix present, nothing matches",
			target: Target{RepoPrefix: "docker-hosted"},
			text:   "zzz", names: []string{"docker-hosted/api"},
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
			target: Target{RepoPrefix: "docker-hosted"},
			text:   "api", names: []string{"other/api"}, truncated: true, partial: true,
		},
		{
			name:   "truncated, prefix present but no match",
			target: Target{RepoPrefix: "docker-hosted"},
			text:   "zzz", names: []string{"docker-hosted/api"}, truncated: true, partial: true,
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

func TestCatalogPrefixNotInCatalog(t *testing.T) {
	tg := Target{RepoPrefix: "docker-hosted"}
	t.Run("complete walk is an error", func(t *testing.T) {
		f := &fakeList{names: []string{"other/api", "other/web"}}
		imgs, partial, err := searchWith(f, tg, Query{Text: "api"})
		if !errors.Is(err, ErrPrefixNotInCatalog) {
			t.Fatalf("err = %v, want ErrPrefixNotInCatalog", err)
		}
		if !strings.Contains(err.Error(), "docker-hosted") {
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
