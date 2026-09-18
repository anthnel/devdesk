package template

import (
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

func TestFromOCIMarksEntriesDiscovered(t *testing.T) {
	got := FromOCI("https://registry.example", []oci.TemplateEntry{
		{Repository: "group/templates/java-library", Tag: "v1", Name: "java-library:v1"},
	})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	e := got[0]
	if !e.Discovered || e.Slug != "oci-group-templates-java-library-v1" {
		t.Errorf("entry = %+v, want a discovered entry with a derived slug", e)
	}
	if err := e.Validate(); err != nil {
		t.Errorf("a discovered entry does not validate: %v", err)
	}
	if e.Source != (Source{Kind: KindOCI, URL: "https://registry.example", Path: "group/templates/java-library", Ref: "v1"}) {
		t.Errorf("source = %+v", e.Source)
	}
}

// Adopting a discovered template must not leave it listed twice.
func TestMergeDropsADiscoveredEntryTheCatalogAlreadyHolds(t *testing.T) {
	src := Source{Kind: KindOCI, URL: "https://r", Path: "a/b", Ref: "v1"}
	declared := []Entry{{Slug: "mine", Name: "Mine", Source: src}}
	discovered := []Entry{
		{Slug: "oci-a-b-v1", Name: "b:v1", Source: src, Discovered: true},
		{Slug: "oci-a-c-v1", Name: "c:v1", Source: Source{Kind: KindOCI, URL: "https://r", Path: "a/c", Ref: "v1"}, Discovered: true},
	}

	got := Merge(declared, discovered)
	if len(got) != 2 || got[0].Slug != "mine" || got[1].Slug != "oci-a-c-v1" {
		t.Errorf("Merge() = %+v, want the declared entry then only the uncovered one", got)
	}
}
