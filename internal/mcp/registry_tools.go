package mcp

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registries_list, and the tool that is deliberately not beside it.
//
// §3.38 named `registry_tags` alongside this one, "from the group cache, never
// the network". That turned out not to be buildable as stated, and the reason is
// worth keeping rather than rediscovering: **there is no tag cache.** The group
// cache holds discovered *members*; tags are fetched over HTTP when the browser
// searches, and nothing stores them.
//
// Fetching them here would need a credential for every registry anyone actually
// runs, and decision 6 of the same entry is that no tool reads the §3.9 secret
// store. An anonymous-only tag listing would answer "no tags" for a private
// registry — an absence read as an emptiness, which is D20's shape — so it is
// worse than not existing. What DevDesk knows and an agent cannot get cheaply is
// the registry *configuration* and what discovery found, and that is this tool.

// registryMember is one repository fronted by a group, as one discovery found
// it.
type registryMember struct {
	Alias      string `json:"alias,omitempty"`
	URL        string `json:"url"`
	RepoPrefix string `json:"repo_prefix,omitempty"`
	Address    string `json:"address" jsonschema:"url and repo_prefix joined; this is the head of the pull reference exactly"`
}

// registryDiscovery is what one probe of a group established, and when.
//
// It is a pointer on the entry, and nil means nobody has ever probed. An entry
// present with no members is the other answer — somebody asked, and it is not a
// group — which is exactly the distinction the cache exists to keep (§3.8,
// decision 3), and it would be lost if both came back as an empty list.
type registryDiscovery struct {
	Members      []registryMember `json:"members"`
	DiscoveredAt time.Time        `json:"discovered_at"`
	AgeSeconds   int64            `json:"age_seconds" jsonschema:"how long ago the discovery ran; members are cached and may no longer be what the manager serves"`
}

type registryEntry struct {
	Slug       string `json:"slug" jsonschema:"DevDesk's own identifier: what parent points at and what the group cache is keyed on"`
	Alias      string `json:"alias,omitempty"`
	URL        string `json:"url"`
	RepoPrefix string `json:"repo_prefix,omitempty"`
	Address    string `json:"address" jsonschema:"url and repo_prefix joined; browse and pull both derive from this pair, which is what stops them meaning two different repositories"`
	Kind       string `json:"kind" jsonschema:"registry or group"`
	Provider   string `json:"provider,omitempty" jsonschema:"generic, nexus, harbor, artifactory or gitlab; declared in the config, never guessed from the URL"`
	AuthMode   string `json:"auth_mode" jsonschema:"credentials, anonymous, or inherit for a group member"`
	Parent     string `json:"parent,omitempty" jsonschema:"the slug of the group this entry is served through"`

	Discovery *registryDiscovery `json:"discovery,omitempty" jsonschema:"present only for a group that has been probed at least once; absent means nobody looked, which is not the same as it having no members"`
}

type registriesListOut struct {
	Context    string          `json:"context"`
	Registries []registryEntry `json:"registries"`
}

func registerRegistriesList(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "registries_list",
		Description: toolDescription("registries_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, registriesListOut, error) {
		return nil, registriesListOut{Context: env.Context, Registries: listRegistries(env)}, nil
	})
}

// listRegistries reports the configured entries and, for a group, whatever the
// last discovery found.
//
// It reads the cache and never the network — the browser's own rule, and what
// lets it answer on the first frame and offline. A group nobody has probed is
// reported as such rather than probed here: a discovery is a write (it caches
// its result) as well as a network call, and this server does neither.
func listRegistries(env *Env) []registryEntry {
	items := env.Config.Registry.Registries
	entries := make([]registryEntry, 0, len(items))

	groups, err := cache.ReadRegistryGroups()
	if err != nil {
		log.Printf("ERROR [mcp/registries] read registry group cache: %v", err)
		groups = nil
	}
	now := time.Now()

	for _, item := range items {
		entry := registryEntry{
			Slug:       item.Slug,
			Alias:      item.Alias,
			URL:        item.URL,
			RepoPrefix: item.RepoPrefix,
			Address:    registryAddress(item.URL, item.RepoPrefix),
			Kind:       item.Kind,
			Provider:   item.Provider,
			AuthMode:   item.AuthMode,
			Parent:     item.Parent,
		}

		if cached, ok := groups[item.Slug]; ok {
			discovery := &registryDiscovery{
				Members:      make([]registryMember, 0, len(cached.Members)),
				DiscoveredAt: cached.DiscoveredAt,
			}
			if !cached.DiscoveredAt.IsZero() {
				discovery.AgeSeconds = int64(now.Sub(cached.DiscoveredAt).Seconds())
			}
			for _, member := range cached.Members {
				discovery.Members = append(discovery.Members, registryMember{
					Alias:      member.Alias,
					URL:        member.URL,
					RepoPrefix: member.RepoPrefix,
					Address:    registryAddress(member.URL, member.RepoPrefix),
				})
			}
			entry.Discovery = discovery
		}

		entries = append(entries, entry)
	}

	// The config list has an order the user wrote it in, which is not one an
	// agent should depend on; slug is the identity, so it is the order.
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Slug) < strings.ToLower(entries[j].Slug)
	})
	return entries
}

// registryAddress joins a URL and a repository prefix into the one string a
// registry is reached by (§3.18, D39).
//
// It is the head of the pull reference exactly, which is the whole point: the
// browse and the pull have to derive from the same pair, or they mean two
// different repositories — one that lists 200 and one that pulls 404.
func registryAddress(url, repoPrefix string) string {
	if repoPrefix == "" {
		return url
	}
	return strings.TrimSuffix(url, "/") + "/" + repoPrefix
}
