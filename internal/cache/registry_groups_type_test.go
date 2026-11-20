package cache

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// DEVDESK-629 (F2a): a discovered member carries its Nexus type (hosted, proxy,
// group or unknown) in an additive, omitempty field; the file version stays 2.

func TestRegistryGroupMemberTypeRoundTrips(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)
	entry := RegistryGroupEntry{DiscoveredAt: at, Members: []RegistryGroupMember{
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-hosted", Type: "hosted"},
		{Alias: "docker", URL: "https://nexus.example.com", RepoPrefix: "docker-proxy", Type: "proxy"},
		{Alias: "other", URL: "https://nexus.example.com", RepoPrefix: "other"},
	}}
	if err := c.Set("prod", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := reopenGroupCache(c).Get("prod")

	if got == nil || len(got.Members) != 3 {
		t.Fatalf("Get(prod) = %+v, want the three members", got)
	}
	for i, want := range []string{"hosted", "proxy", ""} {
		if got.Members[i].Type != want {
			t.Errorf("member %d Type = %q, want %q", i, got.Members[i].Type, want)
		}
	}
}

func TestRegistryGroupMemberTypeIsOmittedWhenUnknownAndTheVersionStays2(t *testing.T) {
	c := newTestGroupCache(t)
	entry := RegistryGroupEntry{DiscoveredAt: time.Now().Truncate(time.Second), Members: []RegistryGroupMember{
		{Alias: "a", URL: "https://nexus.example.com", RepoPrefix: "a", Type: "hosted"},
		{Alias: "b", URL: "https://nexus.example.com", RepoPrefix: "b"},
	}}
	if err := c.Set("prod", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data := readRaw(t, c.path)

	if got := bytes.Count(data, []byte(`"type"`)); got != 1 {
		t.Errorf(`the file has %d "type" keys, want 1: an unknown type must be omitted (omitempty)`, got)
	}
	if !bytes.Contains(data, []byte(`"type": "hosted"`)) && !bytes.Contains(data, []byte(`"type":"hosted"`)) {
		t.Errorf(`the file does not carry "type": "hosted": %s`, data)
	}
	var version int
	if err := json.Unmarshal(parseEnvelope(t, c.path)["version"], &version); err != nil || version != 2 {
		t.Errorf(`"version" = %d (err %v), want 2 unchanged`, version, err)
	}
}

// A cache written before the field existed decodes with an unknown type, in v2
// and in the legacy bare map alike: nothing is invented.
func TestOldCacheWithoutTypeLoads(t *testing.T) {
	v2 := []byte(`{"version": 2, "groups": {"prod": {"members": [{"alias": "m", "url": "https://nexus.example.com", "repo_prefix": "m"}], "discovered_at": "2026-01-02T03:04:05Z"}}}`)

	groups, _, _, err := decodeRegistryGroupFile(v2)
	if err != nil {
		t.Fatalf("decodeRegistryGroupFile: %v", err)
	}

	e, ok := groups["prod"]
	if !ok || len(e.Members) != 1 {
		t.Fatalf("groups[prod] = %+v, want the one member", e)
	}
	if e.Members[0].Type != "" {
		t.Errorf("Type = %q, want unknown (empty) for a cache with no type", e.Members[0].Type)
	}
	if e.Members[0].RepoPrefix != "m" {
		t.Errorf("RepoPrefix = %q, the rest of the member must load unchanged", e.Members[0].RepoPrefix)
	}
}
