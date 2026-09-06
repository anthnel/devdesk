// Package registryalias adapts the configured registry list into the display
// substitutions expected by `docker.ApplyAliases`.
//
// # Why a package rather than a function in either of the two
//
// `internal/docker` does not know `internal/config`, and that is deliberate:
// it is the Docker CLI driver, and `docker.RegistryAlias` is its own type for
// that reason — a driver that learns a YAML file's schema can no longer be
// called without it. The reverse is worse: `config` describes what the user
// writes, and has no reason to depend on how Docker names things.
//
// The adapter therefore sits above both. It lives here because its two
// callers are views — `:oci`'s Images tab and `:sec`'s inventory — which
// display the same image and must write it the same way.
package registryalias

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// From renders the substitutions declared by the configuration.
//
// The declaration order is preserved, and that is not cosmetic:
// `docker.ApplyAliases` keeps the **first** prefix that matches, so that is
// what settles the tie between two registries where one prefixes the other
// (`nexus.example.com` and `nexus.example.com/docker-hosted`). Sorting or
// deduplicating here would change the displayed name without anything
// saying so.
//
// An entry with no alias, or no URL, substitutes nothing: it is dropped
// rather than carried with an empty string, which would match every image
// name.
func From(items []config.RegistryItem) []docker.RegistryAlias {
	aliases := make([]docker.RegistryAlias, 0, len(items))
	for _, item := range items {
		if item.Alias == "" || item.URL == "" {
			continue
		}
		aliases = append(aliases, docker.RegistryAlias{URL: item.URL, Alias: item.Alias})
	}
	return aliases
}
