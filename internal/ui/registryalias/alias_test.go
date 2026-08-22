package registryalias

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

func TestOnlyEntriesThatCanSubstituteAreKept(t *testing.T) {
	got := From([]config.RegistryItem{
		{URL: "nexus.example.com/docker-hosted", Alias: "nx"},
		{URL: "registry.example.com"}, // no alias — nothing to substitute
		{Alias: "orphan"},             // no URL — would match every image name
		{URL: "gitlab.example.com/group", Alias: "gl"},
	})

	want := []docker.RegistryAlias{
		{URL: "nexus.example.com/docker-hosted", Alias: "nx"},
		{URL: "gitlab.example.com/group", Alias: "gl"},
	}
	if len(got) != len(want) {
		t.Fatalf("From() = %v, want the two entries that carry both halves", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("From()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// The order is what settles two registries where one prefixes the other:
// ApplyAliases keeps the first match, so reordering here would rename an image
// with nothing on screen saying why.
func TestDeclarationOrderIsPreserved(t *testing.T) {
	items := []config.RegistryItem{
		{URL: "nexus.example.com/docker-hosted", Alias: "hosted"},
		{URL: "nexus.example.com", Alias: "nx"},
	}

	got := From(items)

	if got[0].Alias != "hosted" {
		t.Fatalf("From() reordered the list: %v", got)
	}
	if name := docker.ApplyAliases("nexus.example.com/docker-hosted/agent:1", got); name != "hosted/agent:1" {
		t.Errorf("ApplyAliases = %q, want the more specific prefix to win by coming first", name)
	}
}

func TestAnEmptyListSubstitutesNothing(t *testing.T) {
	if got := From(nil); len(got) != 0 {
		t.Errorf("From(nil) = %v, want no substitutions", got)
	}
}
