package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-396 (IS-01 criterion 6): the level 1 table of the registry browser
// is declared to the activity-column guard as "oci/registry-search".

func TestSearchColumnZeroIsTheActivityColumn(t *testing.T) {
	checkActivityColumnZero(t, "newSearchTable", newSearchTable().Shapes())
}

// The dot column is always at rest, and shows the image icon there.
func TestSearchColumnZeroIsAlwaysAtRest(t *testing.T) {
	c := searchColumns()[0]
	if c.Indicator == nil {
		t.Fatal("column 0 has no Indicator")
	}
	r := searchRow{img: imagesearch.Image{Name: "library/nginx"}, label: "hub"}
	if got := c.Indicator(r); got != indicator.Idle {
		t.Errorf("Indicator = %v, want Idle", got)
	}
	if got := c.Cell(r); got != theme.IconDocker {
		t.Errorf("resting cell = %q, want %q", got, theme.IconDocker)
	}
	if got, want := c.Style(r).GetForeground(), theme.IconStyle(theme.IconRoleImage).GetForeground(); got != want {
		t.Errorf("resting style = %v, want IconRoleImage %v", got, want)
	}
}

func TestSearchTableRenderedWidthFollowsTheTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		tbl := newSearchTable()
		tbl.Resize(width, 30)
		if got, want := tbl.RenderedWidth(), width-2; got != want {
			t.Errorf("width %d: the line spans %d, want %d (Rule 116)", width, got, want)
		}
	}
}

// ActivityTables reports the level 1 table browser closed or open.
func TestActivityTablesListsTheRegistrySearch(t *testing.T) {
	closed := feed(t, loadedModel(t), testutil.Resize(60, 30))
	tbl := closed.ActivityTables()["oci/registry-search"]
	if tbl == nil {
		t.Fatal("ActivityTables lacks oci/registry-search with the browser closed")
	}
	checkActivityColumnZero(t, "closed browser", tbl.Shapes())
	if got := tbl.RenderedWidth(); got < 58 || got > 60 {
		t.Errorf("closed: the line spans %d at 60 columns, want 58 to 60", got)
	}

	open := feed(t, closed, testutil.Key(keymap.Browser))
	if open.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	tbl = open.ActivityTables()["oci/registry-search"]
	if tbl == nil {
		t.Fatal("ActivityTables lacks oci/registry-search with the browser open")
	}
	if got := tbl.RenderedWidth(); got < 58 || got > 60 {
		t.Errorf("open: the line spans %d at 60 columns, want 58 to 60", got)
	}
}
