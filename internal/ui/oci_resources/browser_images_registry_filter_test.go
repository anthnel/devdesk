package ociresources

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-310 (IS-06) step 5: `r` on level 1 narrows the image list to one
// group or one registry. Black box: keys in, rows, header, footer and bar out.
// The `r` shortcut entry and the help text are tested in browser_images_view_test.go
// and browser_images_registry_filter_shortcut_test.go (step 6).

const (
	rfWidth = 180
	rfDesc  = "Bitnami packaged"
)

var (
	rfM1  = memberKey("prod", "registry.example.com", "docker-hosted")
	rfM2  = memberKey("prod", "registry.example.com", "dhi-proxy")
	rfHub = "hub"
)

// rfSearchModel starts a level 1 search over the group prod (members m1, m2)
// and the lone registry hub, in that form order. No answer has arrived yet.
func rfSearchModel(t *testing.T) (Model, *fakeSearcher) {
	t.Helper()
	fake := &fakeSearcher{}
	withImageSearcher(t, fake)
	return searchLot(t, groupedModel(t)), fake
}

func rfImage(name, desc string) []imagesearch.Image {
	return []imagesearch.Image{{Name: name, Repo: name, Description: desc}}
}

// rfAnswered has m1 "nginx", m2 "redis" and hub "nginx-hub" in the table.
func rfAnswered(t *testing.T) (Model, *fakeSearcher) {
	t.Helper()
	m, fake := rfSearchModel(t)
	m = answerFor(t, m, rfM1, rfImage("nginx", ""), false, nil)
	m = answerFor(t, m, rfM2, rfImage("redis", ""), false, nil)
	m = answerFor(t, m, rfHub, rfImage("nginx-hub", ""), false, nil)
	if m.registryBrowser.pendingSearches != 0 {
		t.Fatalf("fixture: pendingSearches = %d", m.registryBrowser.pendingSearches)
	}
	return m, fake
}

func rfNames(m Model) []string {
	names := lvl1Names(m)
	slices.Sort(names)
	return names
}

func rfWantNames(t *testing.T, m Model, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := rfNames(m); !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func rfBar(m Model) string { return ansi.Strip(m.registryBrowser.FilterBarView(rfWidth)) }

// AC2: `r` goes through the group, each registry, then off.
func TestRCyclesTheGroupEachRegistryThenOffOnLevelOne(t *testing.T) {
	m, _ := rfAnswered(t)
	b := m.registryBrowser
	if m.GetFooterHeight() != 2 {
		t.Fatalf("fixture: footer height = %d, want 2", m.GetFooterHeight())
	}

	steps := []struct {
		label string
		rows  []string
	}{
		{b.filterLabel(resultFilter{groupSlug: "prod"}), []string{"nginx", "redis"}},
		{b.filterLabel(resultFilter{entryKey: rfM1}), []string{"nginx"}},
		{b.filterLabel(resultFilter{entryKey: rfM2}), []string{"redis"}},
		{b.filterLabel(resultFilter{entryKey: rfHub}), []string{"nginx-hub"}},
	}
	for i, s := range steps {
		m = feed(t, m, testutil.Key("r"))
		rfWantNames(t, m, s.rows...)
		if !b.FilterIsVisible() {
			t.Errorf("stop %d: the bar is hidden", i)
		}
		if !strings.Contains(rfBar(m), s.label) {
			t.Errorf("stop %d: token %q missing in the bar %q", i, s.label, rfBar(m))
		}
		if got := m.GetFooterHeight(); got != 4 {
			t.Errorf("stop %d: footer height = %d, want 4", i, got)
		}
		if got := lvl1Header(t, m, "Images"); got != strconv.Itoa(len(s.rows)) {
			t.Errorf("stop %d: Images = %q, want %d", i, got, len(s.rows))
		}
	}
	m = feed(t, m, testutil.Key("r"))
	rfWantNames(t, m, "nginx", "redis", "nginx-hub")
	if b.FilterIsVisible() {
		t.Error("off: the bar is still visible")
	}
	if got := m.GetFooterHeight(); got != 2 {
		t.Errorf("off: footer height = %d, want 2", got)
	}
}

// The cursor follows its row through the narrowing.
func TestRKeepsTheCursorOnTheSameImage(t *testing.T) {
	m, _ := rfAnswered(t)
	b := m.registryBrowser
	b.searchTable.SetCursor(slices.IndexFunc(b.searchTable.Visible(), func(r searchRow) bool { return r.img.Name == "redis" }))
	assertSelected(t, b, rfM2, "redis")
	m = feed(t, m, testutil.Key("r")) // group prod
	assertSelected(t, m.registryBrowser, rfM2, "redis")
}

// AC3: one registry with rows (the other found nothing): nothing to narrow to.
func TestRDoesNothingWithASingleRegistryHoldingRows(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := registryModel(t, anonymousRegistry("docker.io", "hub"), anonymousRegistry("dhi.io", "dhi.io"))
	m = searchLot(t, m)
	m = answerFor(t, m, "hub", rfImage("nginx", ""), false, nil)
	m = answerFor(t, m, "dhi.io", nil, false, nil)
	m = feed(t, m, testutil.Key("r"))
	rfWantNames(t, m, "nginx")
	if m.registryBrowser.FilterIsVisible() {
		t.Error("the bar is visible with a single stop")
	}
	if got := m.GetFooterHeight(); got != 2 {
		t.Errorf("footer height = %d, want 2", got)
	}
}

// AC1: `/` still searches the description, and never asks a registry again.
func TestSlashStillMatchesTheDescriptionAndDoesNotSearchAgain(t *testing.T) {
	m, fake := rfSearchModel(t)
	m = answerFor(t, m, rfM1, rfImage("web", rfDesc), false, nil)
	m = answerFor(t, m, rfM2, rfImage("redis", "key value"), false, nil)
	m = answerFor(t, m, rfHub, rfImage("other", ""), false, nil)
	calls := fake.calls()
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("bit")...)
	m = feed(t, m, testutil.Key("enter"))
	rfWantNames(t, m, "web")
	if got := fake.calls(); got != calls {
		t.Errorf("searcher calls = %d after the filter, want %d", got, calls)
	}
}

// AC5: r, . and : typed in the open bar go to the field.
func TestRDotAndColonTypedInTheOpenBarGoToTheField(t *testing.T) {
	m, _ := rfAnswered(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key("r")) // group prod
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("r.:")...)
	if !b.searchTable.InEditMode() {
		t.Fatal("the bar closed")
	}
	if got := b.searchTable.FilterBar().SearchQuery(); got != "r.:" {
		t.Errorf("the field holds %q, want %q", got, "r.:")
	}
	if !strings.Contains(rfBar(m), "r.:") {
		t.Errorf("the bar does not show r.: (%q)", rfBar(m))
	}
	if col, desc := b.searchTable.SortState(); col != -1 || desc {
		t.Errorf("SortState = (%d,%v): the dot sorted", col, desc)
	}
	if label := b.filterLabel(resultFilter{groupSlug: "prod"}); !strings.Contains(rfBar(m), label) {
		t.Errorf("the r token %q left the bar: %q", label, rfBar(m))
	}
	m = feed(t, m, testutil.Key("esc"))
	b = m.registryBrowser
	if b.state != browserStateImages || b.searchTable.InEditMode() {
		t.Fatalf("esc: state %d, edit mode %v", b.state, b.searchTable.InEditMode())
	}
	if !b.FilterIsVisible() {
		t.Error("esc dropped the r filter")
	}
}

// AC6: nothing left after r and a query: a header-only table, still full width.
func TestRWithAQueryThatKeepsNothingLeavesAHeaderOnlyTable(t *testing.T) {
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r"))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzz")...)
	m = feed(t, m, testutil.Key("enter"))
	b := m.registryBrowser
	if got := len(b.searchTable.Visible()); got != 0 {
		t.Fatalf("%d rows, want 0", got)
	}
	if got := lvl1Header(t, m, "Images"); got != "0" {
		t.Errorf("Images = %q, want 0", got)
	}
	if got, want := b.searchTable.RenderedWidth(), rfWidth-2; got != want {
		t.Errorf("RenderedWidth = %d, want %d", got, want)
	}
}

// r and / combine with AND, in either order.
func TestRAndSlashCombine(t *testing.T) {
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r")) // group prod: nginx, redis
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("nginx")...)
	m = feed(t, m, testutil.Key("enter"))
	rfWantNames(t, m, "nginx")
	m = feed(t, m, testutil.Key("r")) // m1: still nginx
	rfWantNames(t, m, "nginx")
	m = feed(t, m, testutil.Key("r")) // m2 holds redis, which the query drops
	rfWantNames(t, m)
	m = feed(t, m, testutil.Key("r")) // hub
	rfWantNames(t, m, "nginx-hub")
	m = feed(t, m, testutil.Key("r")) // off: the query alone
	rfWantNames(t, m, "nginx", "nginx-hub")
}

// While the search runs, a filtered registry's rows stay out, the stops grow
// with the registries that answer, and the cursor stays.
func TestRDuringTheSearchHidesFilteredRowsAndGrowsTheStops(t *testing.T) {
	m, _ := rfSearchModel(t)
	m = answerFor(t, m, rfM1, rfImage("nginx", ""), false, nil)
	m = answerFor(t, m, rfHub, rfImage("nginx-hub", ""), false, nil)
	m = feed(t, m, testutil.Key("r")) // stops so far: prod, m1, hub
	m = feed(t, m, testutil.Key("r")) // m1
	rfWantNames(t, m, "nginx")
	b := m.registryBrowser
	assertSelected(t, b, rfM1, "nginx")

	m = answerFor(t, m, rfM2, rfImage("redis", ""), false, nil)
	rfWantNames(t, m, "nginx")
	assertSelected(t, b, rfM1, "nginx")

	m = feed(t, m, testutil.Key("r")) // the new stop m2 now follows m1
	rfWantNames(t, m, "redis")
}

// right and left keep the r filter and its token.
func TestRSurvivesTagsAndBack(t *testing.T) {
	withTagRecorder(t)
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r")) // group prod
	m = feed(t, m, testutil.Key("right"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want tags", m.registryBrowser.state)
	}
	m = feed(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want images", m.registryBrowser.state)
	}
	rfWantNames(t, m, "nginx", "redis")
	if !m.registryBrowser.FilterIsVisible() {
		t.Error("the r token is gone after tags and back")
	}
}

// A new search clears the r filter and its token (Q2).
func TestANewSearchClearsTheRFilter(t *testing.T) {
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r"))
	m = feed(t, m, testutil.Key("left")) // back to the form
	m, _ = submitRaw(t, m, "nginx")
	b := m.registryBrowser
	if b.state != browserStateImages {
		t.Fatalf("state = %d, want images", b.state)
	}
	if b.FilterIsVisible() {
		t.Error("the r token survived a new search")
	}
	m = answerFor(t, m, rfM1, rfImage("nginx", ""), false, nil)
	m = answerFor(t, m, rfM2, rfImage("redis", ""), false, nil)
	m = answerFor(t, m, rfHub, rfImage("nginx-hub", ""), false, nil)
	rfWantNames(t, m, "nginx", "redis", "nginx-hub")
}
