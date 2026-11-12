package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-153: Networks, Volumes and Registries put the activity column first.

func checkActivityColumnZero(t *testing.T, name string, shapes []datatable.ColumnShape) {
	t.Helper()
	if len(shapes) < 2 {
		t.Fatalf("%s: %d columns", name, len(shapes))
	}
	z := shapes[0]
	if z.Title != "" || !z.Indicator || z.Sizing != datatable.SizingFixed || z.MinWidth != datatable.IconColumnWidth || z.Less || z.Search {
		t.Errorf("%s: column 0 = %+v, want the untitled fixed activity column of width %d", name, z, datatable.IconColumnWidth)
	}
	for i, s := range shapes[1:] {
		if s.Indicator {
			t.Errorf("%s: column %d also carries an Indicator", name, i+1)
		}
	}
}

func TestNetworkColumnZeroIsTheActivityColumn(t *testing.T) {
	cols := networkColumns()
	m := loadedModel(t)
	checkActivityColumnZero(t, "networks", m.networkTable.Shapes())
	c := cols[0]
	if c.Indicator == nil {
		t.Fatal("networks: column 0 has no Indicator")
	}
	n := docker.Network{ID: "net1", Name: "bridge"}
	if got := c.Indicator(n); got != indicator.Idle {
		t.Errorf("Indicator = %v, want Idle", got)
	}
	if got := c.Cell(n); got != "" {
		t.Errorf("resting cell = %q, want empty", got)
	}
	if cols[1].Title != "ID" || cols[2].Title != "Name" {
		t.Errorf("columns 1, 2 = %q, %q, want ID, Name", cols[1].Title, cols[2].Title)
	}
}

func TestVolumeColumnZeroIsTheActivityColumn(t *testing.T) {
	cols := volumeColumns()
	m := loadedModel(t)
	checkActivityColumnZero(t, "volumes", m.volumeTable.Shapes())
	c := cols[0]
	if c.Indicator == nil {
		t.Fatal("volumes: column 0 has no Indicator")
	}
	v := docker.Volume{Name: "pgdata"}
	if got := c.Indicator(v); got != indicator.Idle {
		t.Errorf("Indicator = %v, want Idle", got)
	}
	if got := c.Cell(v); got != "" {
		t.Errorf("resting cell = %q, want empty", got)
	}
	if cols[1].Title != "Name" {
		t.Errorf("column 1 = %q, want Name", cols[1].Title)
	}
}

func TestRegistryColumnZeroIsTheActivityColumn(t *testing.T) {
	cols := registryColumns()
	m := registriesTab(t)
	checkActivityColumnZero(t, "registries", m.registryTable.Shapes())
	c := cols[0]
	if c.Indicator == nil {
		t.Fatal("registries: column 0 has no Indicator")
	}
	r := registryRow{alias: "hub", url: "docker.io"}
	if got := c.Indicator(r); got != indicator.Idle {
		t.Errorf("Indicator = %v, want Idle", got)
	}
	if got := c.Cell(r); got != "" {
		t.Errorf("resting cell = %q, want empty", got)
	}
	if cols[1].Title != "Alias" {
		t.Errorf("column 1 = %q, want Alias", cols[1].Title)
	}
}

func TestActivityTablesListsTheSixTables(t *testing.T) {
	got := loadedModel(t).ActivityTables()
	for _, name := range []string{"oci/images", "oci/networks", "oci/volumes", "oci/registries", "oci/registry-browser", "oci/registry-search"} {
		if got[name] == nil {
			t.Errorf("ActivityTables lacks %q", name)
		}
	}
	if len(got) != 6 {
		t.Errorf("ActivityTables has %d tables, want 6", len(got))
	}
}

// Rule 116 on every tab, now that each carries the extra first column.
func TestEveryTabFillsTheWidthWithTheActivityColumn(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		m := feed(t, loadedModel(t), RegistryGroupCacheLoadedMsg{}, testutil.Resize(width, 30))
		for name, table := range m.ActivityTables() {
			if got, want := table.RenderedWidth(), width-2; got != want {
				t.Errorf("width %d, %s: the line spans %d, want %d (Rule 116)", width, name, got, want)
			}
		}
	}
}
