package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// While registries answer one after the other, the row under the cursor stays
// the same row, identified by EntryKey + Repo and never by its index
// (DEVDESK-302, IS-04 c.8). datatable is untouched: the view does the tracking.

// cursorBrowser opens a browser on two registries and starts a search for
// "nginx". It returns the browser and the two entry keys.
func cursorBrowser(t *testing.T) (b *RegistryBrowser, keyA, keyB string) {
	t.Helper()
	b = newRegistryBrowser([]config.RegistryItem{
		{URL: "a.example.com", Alias: "a", Slug: "a"},
		{URL: "b.example.com", Alias: "b", Slug: "b"},
	}, nil, nil, 120, 30)
	if len(b.entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(b.entries))
	}
	b.startImageSearch("nginx", b.entries)
	return b, b.entries[0].key, b.entries[1].key
}

func cursorImg(key, repo string) imagesearch.Image {
	return imagesearch.Image{EntryKey: key, Name: repo, Repo: repo}
}

func arrive(t *testing.T, b *RegistryBrowser, key string, repos ...string) {
	t.Helper()
	imgs := make([]imagesearch.Image, len(repos))
	for i, r := range repos {
		imgs[i] = cursorImg(key, r)
	}
	if !b.AddImageResults(ImageSearchResultMsg{Gen: b.gen, EntryKey: key, Images: imgs}) {
		t.Fatal("the result was refused")
	}
}

func assertSelected(t *testing.T, b *RegistryBrowser, key, repo string) {
	t.Helper()
	got, ok := b.searchTable.Selected()
	if !ok {
		t.Fatalf("nothing selected, want %s|%s", key, repo)
	}
	if got.img.EntryKey != key || got.img.Repo != repo {
		t.Errorf("selected %s|%s (cursor %d), want %s|%s",
			got.img.EntryKey, got.img.Repo, b.searchTable.Cursor(), key, repo)
	}
}

func TestSearchCursorStaysWhenRowsInsertBeforeTheSelection(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx-proxy", "nginx-ui")
	b.searchTable.SetCursor(1)
	assertSelected(t, b, keyB, "nginx-ui")

	// An exact match sorts before every prefix match.
	arrive(t, b, keyA, "nginx", "nginx-aaa")
	assertSelected(t, b, keyB, "nginx-ui")
}

func TestSearchCursorStaysWhenRowsInsertAfterTheSelection(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx", "nginx-proxy")
	b.searchTable.SetCursor(1)
	assertSelected(t, b, keyB, "nginx-proxy")

	arrive(t, b, keyA, "nginx-zzz", "my-nginx")
	assertSelected(t, b, keyB, "nginx-proxy")
}

func TestSearchCursorStaysWhenRowsInsertOnBothSides(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx-proxy")
	assertSelected(t, b, keyB, "nginx-proxy")

	arrive(t, b, keyA, "nginx", "nginx-zzz")
	assertSelected(t, b, keyB, "nginx-proxy")
}

// The same Repo from two registries is two rows: the key tells them apart.
func TestSearchCursorDistinguishesTheSameRepoOnTwoRegistries(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx")
	assertSelected(t, b, keyB, "nginx")

	arrive(t, b, keyA, "nginx") // sorts before b's row (label a < b)
	assertSelected(t, b, keyB, "nginx")
}

// A selected row that no longer exists is not an error: the cursor stays in
// range and on a row, and a later arrival keeps it there.
func TestSearchCursorWhenTheSelectedRowDisappears(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx", "nginx-proxy")
	b.searchTable.SetCursor(1)

	b.images = b.images[:1] // the selected row is gone
	b.rebuildSearchTable()
	if c, n := b.searchTable.Cursor(), len(b.searchTable.Visible()); c < 0 || c >= n {
		t.Fatalf("cursor %d out of range [0,%d)", c, n)
	}
	assertSelected(t, b, keyB, "nginx")

	arrive(t, b, keyA, "nginx-aaa")
	if c, n := b.searchTable.Cursor(), len(b.searchTable.Visible()); c < 0 || c >= n {
		t.Fatalf("cursor %d out of range [0,%d) after an arrival", c, n)
	}
	assertSelected(t, b, keyB, "nginx")
}

// With a confirmed filter, the selected row is one of the visible rows and
// arrivals, matching the filter or not, leave it selected.
func TestSearchCursorUnderAConfirmedFilter(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx", "nginx-proxy", "nginx-ui")
	b.searchTable.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("proxy") {
		b.searchTable.Update(msg)
	}
	b.searchTable.Update(testutil.Key("enter"))
	if got := len(b.searchTable.Visible()); got != 1 {
		t.Fatalf("%d rows visible under the filter, want 1", got)
	}
	assertSelected(t, b, keyB, "nginx-proxy")

	arrive(t, b, keyA, "nginx-aaa", "proxy-nginx") // one hidden, one visible
	if got := len(b.searchTable.Visible()); got != 2 {
		t.Fatalf("%d rows visible, want 2", got)
	}
	assertSelected(t, b, keyB, "nginx-proxy")
}

func TestSearchCursorOnTheFirstRowReceivedWhenTheTableWasEmpty(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	// An empty answer first: the table stays empty.
	arrive(t, b, keyA)
	if _, ok := b.searchTable.Selected(); ok {
		t.Fatal("a row is selected in an empty table")
	}
	arrive(t, b, keyB, "nginx-ui", "nginx")
	// The first row of the order in force.
	assertSelected(t, b, keyB, "nginx")
	if c := b.searchTable.Cursor(); c != 0 {
		t.Errorf("cursor = %d, want 0", c)
	}
}

// A new search starts from an empty table: the cursor goes to the first row
// RECEIVED, and the selection of the previous search is not carried over, even
// when the same row comes back later (IS-04 c.8, "a new search returns the
// cursor to the first row").
func TestSearchCursorReturnsToTheFirstRowOnANewSearch(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arrive(t, b, keyB, "nginx", "nginx-proxy", "nginx-ui")
	b.searchTable.SetCursor(2) // not the first row
	assertSelected(t, b, keyB, "nginx-ui")

	b.startImageSearch("nginx", b.entries)
	if _, ok := b.searchTable.Selected(); ok {
		t.Fatal("a row is selected right after a new search started")
	}

	// The first row received of the new search is selected, at index 0.
	arrive(t, b, keyA, "nginx-zzz", "nginx-zzzz")
	if c := b.searchTable.Cursor(); c != 0 {
		t.Errorf("cursor = %d, want 0 on the first row of a new search", c)
	}
	assertSelected(t, b, keyA, "nginx-zzz")

	// The old selection is not remembered: when its row comes back it sorts
	// before the cursor row, and the cursor stays on the row it was on.
	arrive(t, b, keyB, "nginx-ui")
	assertSelected(t, b, keyA, "nginx-zzz")
	if c := b.searchTable.Cursor(); c != 1 {
		t.Errorf("cursor = %d, want 1 (nginx-ui inserted before the selection)", c)
	}
}
