package ociresources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-301 (IS-02): level 1 shows what Docker Hub returns (Stars, Official,
// Description) and the Searcher is wired in production.

// catalogServer serves /v2/_catalog with names and counts every request it gets.
func catalogServer(t *testing.T, names ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path != "/v2/_catalog" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string][]string{"repositories": names})
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// TestProductionWiresHubAndCatalogSearchers (DEVDESK-306): the production
// variable is set (TestMain saved it before clearing it for the rest of the
// suite); a private registry is searched through its catalog, no longer
// ErrUnsupported, and dhi.io stays unsupported.
func TestProductionWiresHubAndCatalogSearchers(t *testing.T) {
	if productionImageSearcher == nil {
		t.Fatal("defaultImageSearcher is nil in production: level 1 stays off")
	}
	srv, requests := catalogServer(t, "api", "web")
	imgs, _, err := productionImageSearcher.Search(context.Background(),
		imagesearch.Target{EntryKey: "prod", RegistryURL: "registry.example.com", APIURL: srv.URL},
		imagesearch.Query{Text: "api"})
	if errors.Is(err, imagesearch.ErrUnsupported) {
		t.Fatal("a private registry answered ErrUnsupported: the catalog searcher is not wired")
	}
	if err != nil {
		t.Fatalf("private registry search: %v", err)
	}
	if len(imgs) != 1 || imgs[0].Repo != "api" {
		t.Fatalf("rows = %+v, want the single catalog name api", imgs)
	}
	if requests.Load() == 0 {
		t.Error("the catalog was never asked")
	}

	before := requests.Load()
	_, _, err = productionImageSearcher.Search(context.Background(),
		imagesearch.Target{RegistryURL: "dhi.io", APIURL: srv.URL}, imagesearch.Query{Text: "nginx"})
	if !errors.Is(err, imagesearch.ErrUnsupported) {
		t.Fatalf("dhi.io answered %v, want imagesearch.ErrUnsupported", err)
	}
	if requests.Load() != before {
		t.Error("dhi.io was asked")
	}
}

// hubRows is a realistic level 1 content: an official image with stars, a user
// image, and one that carries no star count at all.
func hubRows() []searchRow {
	return []searchRow{
		{label: "hub", img: imagesearch.Image{
			Name: "nginx", Repo: "library/nginx", Official: true, Stars: 21000, HasStars: true,
			Description: "Official build of Nginx, a web server and reverse proxy.",
		}},
		{label: "hub", img: imagesearch.Image{
			Name: "bitnami/nginx", Repo: "bitnami/nginx", Stars: 190, HasStars: true,
			Description: "Bitnami container image for NGINX",
		}},
		{label: "hub", img: imagesearch.Image{Name: "someone/nginx-lab", Repo: "someone/nginx-lab"}},
	}
}

func searchColumnByTitle(t *testing.T, title string) func(searchRow) string {
	t.Helper()
	for _, c := range searchColumns() {
		if c.Title == title {
			return c.Cell
		}
	}
	t.Fatalf("no %q column", title)
	return nil
}

// Declared order: activity, Name, Registry, Stars, Official, Description.
func TestSearchColumnsAreDeclaredInOrder(t *testing.T) {
	var got []string
	for _, s := range newSearchTable().Shapes() {
		got = append(got, s.Title)
	}
	want := []string{"", "Name", "Registry", "Stars", "Official", "Description"}
	if !slices.Equal(got, want) {
		t.Fatalf("columns = %q, want %q", got, want)
	}
}

func TestOfficialCellIsPlainOfficialOrEmpty(t *testing.T) {
	cell := searchColumnByTitle(t, "Official")
	rows := hubRows()
	if got := cell(rows[0]); got != "official" {
		t.Errorf("official image: cell = %q, want %q", got, "official")
	}
	if got := cell(rows[1]); got != "" {
		t.Errorf("user image: cell = %q, want empty", got)
	}
}

// A registry that gives no star count shows nothing, never 0.
func TestStarsCellIsEmptyWhenNotProvided(t *testing.T) {
	cell := searchColumnByTitle(t, "Stars")
	rows := hubRows()
	if got := cell(rows[0]); got != "21000" {
		t.Errorf("stars = %q, want 21000", got)
	}
	if got := cell(rows[2]); got != "" {
		t.Errorf("no star count: cell = %q, want empty (never 0)", got)
	}
	zero := searchRow{img: imagesearch.Image{Name: "x", HasStars: true}}
	if got := cell(zero); got != "0" {
		t.Errorf("a provided zero: cell = %q, want 0", got)
	}
}

func TestDescriptionCellIsTheDescription(t *testing.T) {
	cell := searchColumnByTitle(t, "Description")
	if got, want := cell(hubRows()[1]), "Bitnami container image for NGINX"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

// Rule 122: every cell of every column is plain text.
func TestSearchCellsCarryNoEscapeSequences(t *testing.T) {
	tbl := newSearchTable()
	tbl.Resize(200, 30)
	tbl.SetItems(hubRows())
	for _, c := range searchColumns() {
		for _, r := range hubRows() {
			if got := c.Cell(r); strings.Contains(got, "\x1b") {
				t.Errorf("column %q: cell %q carries an escape sequence", c.Title, got)
			}
		}
	}
	for i, row := range tbl.Table().Rows() {
		for col, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("row %d cell [%d] = %q carries an escape sequence", i, col, cell)
			}
		}
	}
}

func searchTableAt(width int) []string {
	tbl := newSearchTable()
	tbl.Resize(width, 30)
	tbl.SetItems(hubRows())
	var out []string
	for i, c := range tbl.Table().Columns() {
		if c.Width <= 0 {
			continue
		}
		if i == 0 {
			out = append(out, "dot")
			continue
		}
		out = append(out, c.Title)
	}
	return out
}

// At 60 columns only the activity column, Name and Registry remain.
func TestSearchTableAt60ColumnsKeepsActivityNameRegistry(t *testing.T) {
	got := searchTableAt(60)
	want := []string{"dot", "Name", "Registry"}
	if !slices.Equal(got, want) {
		t.Fatalf("visible columns at 60 = %q, want %q", got, want)
	}
}

// Columns leave whole, in the order Description, Stars, Official, and the
// line spans the terminal at every width (Rule 116).
func TestSearchTableDropsDescriptionThenStarsThenOfficial(t *testing.T) {
	dropped := func(vis []string) []string {
		var out []string
		for _, name := range []string{"Description", "Stars", "Official"} {
			if !slices.Contains(vis, name) {
				out = append(out, name)
			}
		}
		return out
	}
	var order []string
	prev := 0
	for width := 260; width >= 40; width-- {
		tbl := newSearchTable()
		tbl.Resize(width, 30)
		tbl.SetItems(hubRows())
		if got, want := tbl.RenderedWidth(), width-2; got != want {
			t.Fatalf("width %d: the line spans %d, want %d (Rule 116)", width, got, want)
		}
		var vis []string
		for _, c := range tbl.Table().Columns() {
			if c.Width > 0 {
				vis = append(vis, c.Title)
			}
		}
		d := dropped(vis)
		for _, name := range d {
			if !slices.Contains(order, name) {
				order = append(order, name)
			}
		}
		if len(d) < prev {
			t.Fatalf("width %d: a dropped column came back (%q)", width, d)
		}
		prev = len(d)
	}
	want := []string{"Description", "Stars", "Official"}
	if !slices.Equal(order, want) {
		t.Fatalf("columns left in the order %q, want %q", order, want)
	}
	if got := searchTableAt(260); !slices.Equal(got, []string{"dot", "Name", "Registry", "Stars", "Official", "Description"}) {
		t.Errorf("at 260 columns the visible columns are %q, want all six", got)
	}
}

// fakeHub answers every search with the same Hub v1 payload and counts the
// requests it gets.
func fakeHub(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"name":"nginx","description":"Official build of Nginx.","is_official":true,"star_count":21000},
			{"name":"bitnami/nginx","description":"Bitnami container image for NGINX","is_official":false,"star_count":190},
			{"name":"someone/nginx-lab","description":"","is_official":false}
		]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// c.1 and c.4: with the production routing and a fake Hub, the rows appear
// when the answers arrive; the registry that is not Hub is unsupported and
// asks nobody.
func TestAShortNameListsHubImagesWhenTheyArrive(t *testing.T) {
	srv, requests := fakeHub(t)
	withImageSearcher(t, imagesearch.ByRegistry(imagesearch.HubSearcher{Endpoint: srv.URL, Client: srv.Client()}, nil))

	m := browsingModel(t)
	m, cmd := submitRaw(t, m, "nginx")
	if cmd == nil {
		t.Fatal("the search started nothing")
	}
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want browserStateImages", m.registryBrowser.state)
	}
	m = feed(t, m, testutil.Resize(200, 40))

	start := time.Now()
	results := runCmds(t, cmd)
	for range 2 {
		m = feed(t, m, recvResult(t, results))
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the list took %v, want under 3 s", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the fake Hub got %d requests, want 1 (the private registry is unsupported)", got)
	}
	if got := lvl1Names(m); !slices.Equal(got, []string{"nginx", "bitnami/nginx", "someone/nginx-lab"}) {
		t.Fatalf("rows = %v", got)
	}
	rows := m.registryBrowser.searchTable.Visible()
	if got := rows[0].img.Repo; got != "library/nginx" {
		t.Errorf("official image Repo = %q, want library/nginx", got)
	}
	if got := rows[1].img.Repo; got != "bitnami/nginx" {
		t.Errorf("user image Repo = %q, want bitnami/nginx", got)
	}
	if !rows[0].img.Official || !rows[0].img.HasStars || rows[0].img.Stars != 21000 {
		t.Errorf("official row = %+v", rows[0].img)
	}
	if rows[2].img.HasStars {
		t.Errorf("a result without star_count claims stars: %+v", rows[2].img)
	}
	if got := searchColumnByTitle(t, "Official")(rows[0]); got != "official" {
		t.Errorf("Official cell = %q", got)
	}
	if got := searchColumnByTitle(t, "Stars")(rows[2]); got != "" {
		t.Errorf("Stars cell without a count = %q, want empty", got)
	}
	var titles []string
	for _, s := range m.registryBrowser.searchTable.Shapes() {
		titles = append(titles, s.Title)
	}
	if want := []string{"", "Name", "Registry", "Stars", "Official", "Description"}; !slices.Equal(titles, want) {
		t.Errorf("headers = %q, want %q", titles, want)
	}
}

// The registry prefix is applied once: Hub returns the full path, and only
// the images under the prefix are kept.
func TestHubSearchAppliesTheRegistryPrefixOnce(t *testing.T) {
	srv, _ := fakeHub(t)
	withImageSearcher(t, imagesearch.ByRegistry(imagesearch.HubSearcher{Endpoint: srv.URL, Client: srv.Client()}, nil))

	m := browsingModel(t)
	for i := range m.registryBrowser.entries {
		if m.registryBrowser.entries[i].key == "hub" {
			m.registryBrowser.entries[i].repoPrefix = "bitnami"
		}
	}
	m, cmd := submitRaw(t, m, "nginx")
	results := runCmds(t, cmd)
	for range 2 {
		m = feed(t, m, recvResult(t, results))
	}
	rows := m.registryBrowser.searchTable.Visible()
	if len(rows) != 1 || rows[0].img.Repo != "bitnami/nginx" {
		t.Fatalf("rows = %+v, want the single bitnami/nginx (prefix applied once)", rows)
	}
}
