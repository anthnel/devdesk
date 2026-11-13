package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/imagesearch"
)

// The default order of the level 1 table is a pure function of the rows and
// the typed query: it must not depend on which registry answered first
// (DEVDESK-302, IS-04 c.5). Stars play no part in it.

func TestMatchTier(t *testing.T) {
	tests := []struct {
		name  string
		query string
		image string
		want  int
	}{
		{"exact", "nginx", "nginx", 0},
		{"exact ignores case of the name", "nginx", "NGINX", 0},
		{"exact ignores case of the query", "NGINX", "nginx", 0},
		{"last segment equals the query", "nginx", "bitnami/nginx", 1},
		{"last segment equals, deeper path", "nginx", "a/b/nginx", 1},
		{"last segment starts with the query", "nginx", "nginx-proxy", 2},
		{"last segment starts with the query, namespaced", "nginx", "bitnami/nginx-ingress", 2},
		{"name contains the query", "nginx", "my-nginx", 3},
		{"namespace contains the query only", "nginx", "nginxinc/proxy", 3},
		{"no match sorts after every tier", "nginx", "redis", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTier(tt.query, tt.image); got != tt.want {
				t.Errorf("matchTier(%q, %q) = %d, want %d", tt.query, tt.image, got, tt.want)
			}
		})
	}
}

func orderRow(name, label, key string, stars int) searchRow {
	return searchRow{
		img:   imagesearch.Image{Name: name, EntryKey: key, Stars: stars, HasStars: stars > 0},
		label: label,
	}
}

func orderIDs(rows []searchRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.label + "|" + r.img.Name + "|" + r.img.EntryKey
	}
	return out
}

func TestSortSearchRowsOrder(t *testing.T) {
	tests := []struct {
		name  string
		query string
		in    []searchRow
		want  []string
	}{
		{
			name:  "tiers first, then alphabetical",
			query: "nginx",
			in: []searchRow{
				orderRow("my-nginx", "hub", "k1", 0),
				orderRow("nginx-proxy", "hub", "k1", 0),
				orderRow("bitnami/nginx", "hub", "k1", 0),
				orderRow("nginx", "hub", "k1", 0),
				orderRow("redis", "hub", "k1", 0),
			},
			want: []string{
				"hub|nginx|k1", "hub|bitnami/nginx|k1", "hub|nginx-proxy|k1",
				"hub|my-nginx|k1", "hub|redis|k1",
			},
		},
		{
			name:  "alphabetical inside a tier is case-insensitive",
			query: "x",
			in: []searchRow{
				orderRow("xb", "hub", "k1", 0),
				orderRow("Xa", "hub", "k1", 0),
				orderRow("xc", "hub", "k1", 0),
			},
			want: []string{"hub|Xa|k1", "hub|xb|k1", "hub|xc|k1"},
		},
		{
			name:  "same name: alias, then EntryKey",
			query: "nginx",
			in: []searchRow{
				orderRow("nginx", "nexus", "k3", 0),
				orderRow("nginx", "hub", "k2", 0),
				orderRow("nginx", "hub", "k1", 0),
			},
			want: []string{"hub|nginx|k1", "hub|nginx|k2", "nexus|nginx|k3"},
		},
		{
			name:  "stars do not influence the order",
			query: "nginx",
			in: []searchRow{
				orderRow("nginx-b", "hub", "k1", 9000),
				orderRow("nginx-a", "hub", "k1", 1),
			},
			want: []string{"hub|nginx-a|k1", "hub|nginx-b|k1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := slices.Clone(tt.in)
			sortSearchRows(tt.query, rows)
			if got := orderIDs(rows); !slices.Equal(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSortSearchRowsIgnoresArrivalOrder feeds the same rows, as several
// registries would deliver them, in every order: the result is one and the
// same.
func TestSortSearchRowsIgnoresArrivalOrder(t *testing.T) {
	hub := []searchRow{
		orderRow("nginx", "hub", "kh", 100),
		orderRow("bitnami/nginx", "hub", "kh", 5),
		orderRow("nginx-proxy", "hub", "kh", 0),
	}
	nexus := []searchRow{
		orderRow("nginx", "nexus", "kn", 0),
		orderRow("my-nginx", "nexus", "kn", 0),
		orderRow("team/nginx", "nexus", "kn", 0),
	}
	quay := []searchRow{
		orderRow("nginx", "quay", "kq", 0),
		orderRow("nginx-unprivileged", "quay", "kq", 0),
	}
	registries := [][]searchRow{hub, nexus, quay}
	perms := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

	var first []string
	for _, p := range perms {
		var rows []searchRow
		for _, i := range p {
			rows = append(rows, registries[i]...)
		}
		sortSearchRows("nginx", rows)
		got := orderIDs(rows)
		if first == nil {
			first = got
			continue
		}
		if !slices.Equal(got, first) {
			t.Errorf("arrival order %v gave %v, want %v", p, got, first)
		}
	}
	// The order itself is pinned too, not only its stability.
	want := []string{
		"hub|nginx|kh", "nexus|nginx|kn", "quay|nginx|kq",
		"hub|bitnami/nginx|kh", "nexus|team/nginx|kn",
		"hub|nginx-proxy|kh", "quay|nginx-unprivileged|kq",
		"nexus|my-nginx|kn",
	}
	if !slices.Equal(first, want) {
		t.Errorf("order = %v, want %v", first, want)
	}
}

// TestAnswerOrderDoesNotChangeTheTable feeds the hub then nexus, then the
// reverse, through the browser: the table shows the same rows in the same
// order.
func TestAnswerOrderDoesNotChangeTheTable(t *testing.T) {
	run := func(first, second string) []string {
		m := lvl1SearchingModel(t)
		answers := map[string][]string{
			"hub":  {"nginx-proxy", "nginx", "bitnami/nginx"},
			"prod": {"my-nginx", "nginx", "redis"},
		}
		m = lvl1Answer(t, m, first, answers[first]...)
		m = lvl1Answer(t, m, second, answers[second]...)
		return orderIDs(m.registryBrowser.searchTable.Items())
	}
	a, b := run("hub", "prod"), run("prod", "hub")
	if len(a) != 6 || !slices.Equal(a, b) {
		t.Errorf("hub then prod = %v, prod then hub = %v", a, b)
	}
	if !strings.Contains(a[0], "|nginx|") || !strings.Contains(a[1], "|nginx|") {
		t.Errorf("exact matches are not first: %v", a)
	}
}

// The last tie-break is Repo: same name, alias and EntryKey, whatever the
// arrival order.
func TestSortSearchRowsBreaksTheLastTieOnRepo(t *testing.T) {
	a := searchRow{img: imagesearch.Image{EntryKey: "k", Name: "nginx", Repo: "a/nginx"}, label: "hub"}
	b := searchRow{img: imagesearch.Image{EntryKey: "k", Name: "nginx", Repo: "b/nginx"}, label: "hub"}
	one := []searchRow{a, b}
	two := []searchRow{b, a}
	sortSearchRows("nginx", one)
	sortSearchRows("nginx", two)
	if one[0].img.Repo != "a/nginx" || two[0].img.Repo != "a/nginx" {
		t.Errorf("orders differ: %q/%q vs %q/%q", one[0].img.Repo, one[1].img.Repo, two[0].img.Repo, two[1].img.Repo)
	}
}
