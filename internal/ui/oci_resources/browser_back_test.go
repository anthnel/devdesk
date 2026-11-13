package ociresources

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-307 (IS-09), step 1: ← and esc on tags go back to where the tag
// table was opened from, level 1 (untouched, no new search) when → opened it,
// the search form otherwise.

var backKeys = []string{"left", "esc"}

// levelOneSnapshot is what the tests compare before → and after the way back.
type levelOneSnapshot struct {
	names  []string
	cursor int
	images int
}

func snapshotLevelOne(m Model) levelOneSnapshot {
	b := m.registryBrowser
	return levelOneSnapshot{lvl1Names(m), b.searchTable.Cursor(), len(b.images)}
}

// c.1: back from tags opened with → shows level 1 as it was, with no request.
func TestBackFromRightTagsReturnsToLevelOne(t *testing.T) {
	for _, key := range backKeys {
		t.Run(key, func(t *testing.T) {
			withFullTagRecorder(t)
			fake := &fakeSearcher{}
			withImageSearcher(t, fake)
			m := selectLevelOneRow(t, drillRows(t), "hub", "library/nginx")
			before := snapshotLevelOne(m)
			searches := fake.calls()

			m, _, _ = pressRight(t, m)
			if m.registryBrowser.state != browserStateTags {
				t.Fatalf("state = %d after →, want tags", m.registryBrowser.state)
			}
			m, cmd := step(t, m, testutil.Key(key))

			b := m.registryBrowser
			if b.state != browserStateImages {
				t.Fatalf("state = %d after %s, want level 1 (browserStateImages)", b.state, key)
			}
			if cmd != nil {
				t.Errorf("Cmd = %T, want nil (no new request)", testutil.Msg(cmd))
			}
			if got := snapshotLevelOne(m); !slices.Equal(got.names, before.names) || got.cursor != before.cursor || got.images != before.images {
				t.Errorf("level 1 = %+v, want %+v", got, before)
			}
			if fake.calls() != searches {
				t.Errorf("searches = %d, want %d", fake.calls(), searches)
			}
			if b.InEditMode() {
				t.Error("InEditMode() is true at level 1")
			}
			// Level 1's table owns the keys again, the tag table does not.
			m = feed(t, m, testutil.Key("home"), testutil.Key("down"))
			if got := m.registryBrowser.searchTable.Cursor(); got != 1 {
				t.Errorf("cursor after home, down = %d, want 1: the level 1 table is not focused", got)
			}
		})
	}
}

// c.1: the confirmed query, the order and the cursor survive the round trip.
func TestBackToLevelOneKeepsItsFilterAndOrder(t *testing.T) {
	for _, key := range backKeys {
		t.Run(key, func(t *testing.T) {
			withFullTagRecorder(t)
			m := drillRows(t)
			m = feed(t, m, testutil.Key("/"))
			m = feed(t, m, testutil.Type("ngi")...)
			m = feed(t, m, testutil.Key("enter"), testutil.Key("home"), testutil.Key("down"))
			b := m.registryBrowser
			if got := b.searchTable.FilterBar().SearchQuery(); got != "ngi" {
				t.Fatalf("query = %q, want ngi", got)
			}
			before := snapshotLevelOne(m)
			if before.cursor != 1 {
				t.Fatalf("cursor = %d, want the 2nd visible row", before.cursor)
			}

			m, _, _ = pressRight(t, m)
			m = feed(t, m, testutil.Key(key))

			b = m.registryBrowser
			if b.state != browserStateImages {
				t.Fatalf("state = %d, want level 1", b.state)
			}
			if got := b.searchTable.FilterBar().SearchQuery(); got != "ngi" {
				t.Errorf("query = %q after the way back, want ngi", got)
			}
			if got := snapshotLevelOne(m); !slices.Equal(got.names, before.names) || got.cursor != before.cursor {
				t.Errorf("level 1 = %+v, want %+v", got, before)
			}
			if !b.FilterIsVisible() {
				t.Error("the level 1 filter bar is not shown")
			}
		})
	}
}

// c.2: leaving the tags drops the lot; its late answers change nothing.
func TestALateTagAnswerAfterBackIsIgnored(t *testing.T) {
	for _, key := range backKeys {
		t.Run(key, func(t *testing.T) {
			m := afterRight(t) // → pressed, nothing answered yet
			old := m.registryBrowser.gen
			before := snapshotLevelOne(m)

			m = feed(t, m, testutil.Key(key))
			b := m.registryBrowser
			if b.state != browserStateImages {
				t.Fatalf("state = %d, want level 1", b.state)
			}
			if b.gen == old {
				t.Error("gen did not change")
			}
			if b.cancel != nil {
				t.Error("the lot's cancel func is still held")
			}

			late := failedTags(m, nil)
			late.Gen, late.Err, late.Tags = old, nil, []string{"v1"}
			failed := failedTags(m, errors.New("boom"))
			failed.Gen = old
			meta := MultiRegistryTagsMetaMsg{Gen: old, EntryKey: "hub", RegistryURL: "docker.io", Repo: "library/nginx"}
			m = feed(t, m, late, failed, meta)

			b = m.registryBrowser
			if b.state != browserStateImages {
				t.Errorf("state = %d, a late answer moved the browser", b.state)
			}
			if b.pendingSearches != 0 {
				t.Errorf("pendingSearches = %d, want 0", b.pendingSearches)
			}
			if _, loading := b.LoadingLabel(); loading {
				t.Error("still loading after the way back")
			}
			if m.footer.IsSet() {
				t.Errorf("footer = %q, want none", m.footer.Text())
			}
			if got := len(b.tagTable.Items()); got != 0 {
				t.Errorf("tag rows = %d, want 0", got)
			}
			if got := snapshotLevelOne(m); !slices.Equal(got.names, before.names) || got.cursor != before.cursor {
				t.Errorf("level 1 = %+v, want %+v", got, before)
			}
		})
	}
}

// H2: the tag filters do not outlive the tag table (the title would show a
// stale [alias] over level 1).
func TestBackFromTagsClearsTheTagFilters(t *testing.T) {
	for _, key := range backKeys {
		t.Run(key, func(t *testing.T) {
			withFullTagRecorder(t)
			m, _, _ := pressRight(t, selectLevelOneRow(t, drillRows(t), "hub", "library/nginx"))
			// A → lot has one source, so `r` has no stop to cycle to: set the
			// filter the way a multi-source table would have.
			m.registryBrowser.registryFilter = resultFilter{entryKey: "hub"}
			m = feed(t, m, testutil.Key(key))

			b := m.registryBrowser
			if b.state != browserStateImages {
				t.Errorf("state = %d, want level 1", b.state)
			}
			if !b.registryFilter.isEmpty() {
				t.Errorf("registryFilter = %+v, want it cleared", b.registryFilter)
			}
			if b.tagsImage != "" {
				t.Errorf("tagsImage = %q, want it cleared", b.tagsImage)
			}
			title := m.GetTitle()
			if strings.Contains(title, "[") {
				t.Errorf("title = %q, want no [registry] filter label", title)
			}
			if !strings.Contains(title, "nginx") {
				t.Errorf("title = %q, want the typed text", title)
			}
		})
	}
}

// c.5: esc closes the open tag filter bar first, then goes back.
func TestEscClosesTheTagFilterFirst(t *testing.T) {
	withFullTagRecorder(t)
	m, _, _ := pressRight(t, selectLevelOneRow(t, drillRows(t), "hub", "library/nginx"))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("v1")...)
	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateTags || m.registryBrowser.filterActive {
		t.Fatalf("after the first esc: state %d, filterActive %v; want tags with the bar closed",
			m.registryBrowser.state, m.registryBrowser.filterActive)
	}
	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateImages {
		t.Errorf("after the second esc: state = %d, want level 1", m.registryBrowser.state)
	}
}

// c.5: while the tag filter takes text, ← belongs to the text field.
func TestLeftWhileTypingTheTagFilterStaysInTheFilter(t *testing.T) {
	withFullTagRecorder(t)
	m, _, _ := pressRight(t, selectLevelOneRow(t, drillRows(t), "hub", "library/nginx"))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("v1")...)
	m = feed(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateTags || !m.registryBrowser.filterActive {
		t.Errorf("state %d, filterActive %v; want tags with the bar still open",
			m.registryBrowser.state, m.registryBrowser.filterActive)
	}
}

// c.3: tags opened by full name go back to the form, on the Open Tags button.
func TestFullNameTagsGoBackToTheForm(t *testing.T) {
	for _, key := range backKeys {
		t.Run(key, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			withFullTagRecorder(t)
			m := typeInto(t, browsingModel(t), "nginx")
			b := m.registryBrowser
			m = downTo(t, m, b.brFieldButton(1))
			selected := checkedKeys(m)
			m, cmd := step(t, m, testutil.Key("enter"))
			m = feed(t, m, testutil.Msgs(cmd)...)
			if m.registryBrowser.state != browserStateTags {
				t.Fatalf("state = %d, want tags", m.registryBrowser.state)
			}
			old := m.registryBrowser.gen

			m = feed(t, m, testutil.Key(key))

			b = m.registryBrowser
			if b.state != browserStateInput {
				t.Fatalf("state = %d, want the form", b.state)
			}
			if b.focusedField != b.brFieldButton(1) {
				t.Errorf("focus = %d, want the Open Tags button %d", b.focusedField, b.brFieldButton(1))
			}
			if got := b.repoInput.Value(); got != "nginx" {
				t.Errorf("name = %q, want it kept", got)
			}
			if got := checkedKeys(m); !sameStrings(got, selected) {
				t.Errorf("selection = %v, want %v", got, selected)
			}
			if b.gen == old {
				t.Error("gen did not change: the tag lot was not abandoned (H1)")
			}
		})
	}
}

// A second → after the way back is a fresh lot on the new row.
func TestRightAgainAfterBackOpensANewLot(t *testing.T) {
	rec := withFullTagRecorder(t)
	m := selectLevelOneRow(t, drillRows(t), "hub", "library/nginx")
	m, _, _ = pressRight(t, m)
	m = feed(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want level 1", m.registryBrowser.state)
	}
	rec.reset()

	m = selectLevelOneRow(t, m, "prod", "nginx")
	m, _, _ = pressRight(t, m)

	b := m.registryBrowser
	calls := rec.sorted()
	if len(calls) != 1 || !strings.Contains(calls[0], "registry.example.com") || strings.Contains(calls[0], "library") {
		t.Errorf("requests = %v, want one, to prod, for nginx", calls)
	}
	if b.state != browserStateTags || !b.tagsFromSearch {
		t.Errorf("state %d, tagsFromSearch %v; want tags opened from level 1", b.state, b.tagsFromSearch)
	}
	if b.tagsImage != "nginx" {
		t.Errorf("tagsImage = %q, want nginx", b.tagsImage)
	}
}
