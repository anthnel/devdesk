package ociresources

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/registrymgr"
)

// DEVDESK-629 (F2a): the type a discovery read for a member travels to the cache
// and the table through the one conversion, toCachedMembers.
func TestToCachedMembersKeepsTheType(t *testing.T) {
	got := toCachedMembers([]registrymgr.GroupMember{
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-hosted", Type: "hosted"},
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-proxy", Type: "proxy"},
		{Alias: "x", URL: "https://nexus.example.com", RepoPrefix: "x"},
	})

	want := []cache.RegistryGroupMember{
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-hosted", Type: "hosted"},
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-proxy", Type: "proxy"},
		{Alias: "x", URL: "https://nexus.example.com", RepoPrefix: "x"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d members, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The discovery writes the cache through cacheGroupMembers: the type must reach
// the file, and a reader (a later launch) sees it.
func TestCacheGroupMembersWritesTheType(t *testing.T) {
	isolateGroupHome(t)
	at := time.Now().Truncate(time.Second)

	cacheGroupMembers("prod", []registrymgr.GroupMember{
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-proxy", Type: "proxy"},
	}, at)

	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatal(err)
	}
	e := c.Get("prod")
	if e == nil || len(e.Members) != 1 || e.Members[0].Type != "proxy" {
		t.Fatalf("Get(prod) = %+v, want the member with Type proxy", e)
	}
}
