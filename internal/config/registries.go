package config

import (
	"fmt"
	"net/url"
	"strings"
)

// A repository-manager group is a pullable registry as well as a container for
// other registries, which is why both kinds live in one list with a
// discriminator rather than in two lists (§3.8, decision A).
const (
	KindRegistry = "registry"
	KindGroup    = "group"
)

// The repository manager serving a group. It is declared, not sniffed from the
// URL: that is what lets a second implementation exist without guessing
// (§3.8, decision F).
const (
	ProviderGeneric     = "generic"
	ProviderNexus       = "nexus"
	ProviderHarbor      = "harbor"
	ProviderArtifactory = "artifactory"
	ProviderGitLab      = "gitlab"
)

// Whether DevDesk may send stored credentials to an entry.
//
// `docker login` takes a registry host, not a path: ~/.docker/config.json is
// keyed on host[:port], so a path-based group and all of its members share one
// single credential entry. That is why `inherit` is not a convenience — it is
// the only thing the credential store can represent for a member — and why the
// one useful per-member override is refusing to send them at all (§3.8).
const (
	AuthCredentials = "credentials"
	AuthAnonymous   = "anonymous"
	AuthInherit     = "inherit"
)

// fallbackSlug names an entry whose alias and URL both reduce to nothing.
const fallbackSlug = "registry"

// AuthModes returns the modes an entry may take, in the order a form cycles
// them. The first is the default, and it is what the entry did before the mode
// existed: use whatever `docker login` stored.
func AuthModes(isMember bool) []string {
	if isMember {
		return []string{AuthInherit, AuthAnonymous}
	}
	return []string{AuthCredentials, AuthAnonymous}
}

// ResolveAuthMode returns the mode in force for item. A member that inherits
// takes its group's; an entry that says nothing keeps the behaviour it had
// before the mode existed.
func ResolveAuthMode(item RegistryItem, parent *RegistryItem) string {
	mode := item.AuthMode
	if mode == AuthInherit {
		// Nothing to inherit from: refusing to send credentials is the only
		// answer that cannot leak them.
		if parent == nil {
			return AuthAnonymous
		}
		mode = parent.AuthMode
	}
	if mode == "" {
		return AuthCredentials
	}
	return mode
}

// UsesCredentials reports whether mode permits sending stored credentials.
// An unresolved `inherit` counts as anonymous — resolve it with ResolveAuthMode
// before asking.
func UsesCredentials(mode string) bool {
	return mode != AuthAnonymous && mode != AuthInherit
}

// Providers returns the declared providers in the order a form cycles them,
// generic first because it is the default for a group that says nothing.
func Providers() []string {
	return []string{ProviderGeneric, ProviderNexus, ProviderHarbor, ProviderArtifactory, ProviderGitLab}
}

// Kinds returns the two kinds in the order a form cycles them.
func Kinds() []string {
	return []string{KindRegistry, KindGroup}
}

// Slugify reduces s to the [a-z0-9-] alphabet a slug is restricted to, so that
// it can be a file name, a cache key and a YAML scalar without quoting.
// Returns "" when nothing survives.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// DeriveSlug proposes a slug for an entry that declares none: the alias when
// there is one, the URL's host otherwise.
func DeriveSlug(item RegistryItem) string {
	if s := Slugify(item.Alias); s != "" {
		return s
	}
	return Slugify(registryHost(item.URL))
}

// registryHost extracts host[:port] from a registry URL, which may be written
// with or without a scheme — "registry.example.com:5000/path" is a legal thing
// to type into the form.
func registryHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return raw
}

// normalizeRegistries fills in what a config written by an older build does not
// carry, and refuses one that cannot round-trip.
//
// Slugs DevDesk derives are made unique here. Slugs the file declares are never
// rewritten: a member points at its group by slug, so renaming one silently
// would move that group's members under a different group. A file that declares
// the same slug twice, or points at a group that is not there, is an error the
// user has to resolve — the form is what stops either from being written.
func normalizeRegistries(items []RegistryItem) error {
	for i := range items {
		applyRegistryKind(&items[i])
		applyAuthMode(&items[i])
	}
	taken, err := declaredSlugs(items)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].Slug != "" {
			continue
		}
		items[i].Slug = uniqueSlug(ProposeSlug(items[i]), taken)
		taken[items[i].Slug] = true
	}
	if err := checkParents(items, taken); err != nil {
		return err
	}
	if err := checkRepoPrefixes(items); err != nil {
		return err
	}
	return checkAuthModes(items)
}

// checkRepoPrefixes refuses a prefix that cannot mean what it says.
//
// A prefix is a path segment placed in front of the repository name, so a slash
// at either end would double one when the two are joined — and the reference
// would then be wrong in a way only the remote could report. A prefix on a group
// is refused outright: a group is reachable only through its own connector, and
// the same trick applied to it answers 404 (§3.18).
func checkRepoPrefixes(items []RegistryItem) error {
	for i := range items {
		prefix := strings.TrimSpace(items[i].RepoPrefix)
		items[i].RepoPrefix = prefix
		if prefix == "" {
			continue
		}
		if items[i].Kind == KindGroup {
			return fmt.Errorf(
				"registry %q is a group and cannot declare a repo prefix: a group is reached through its own connector", items[i].Slug)
		}
		if strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
			return fmt.Errorf(
				"registry %q declares the repo prefix %q: write it without a leading or trailing slash", items[i].Slug, prefix)
		}
	}
	return nil
}

// applyAuthMode migrates the boolean auth_mode replaced, and clears it so the
// next save writes the file without it.
func applyAuthMode(item *RegistryItem) {
	if item.AuthMode == "" {
		item.AuthMode = AuthAnonymous
		if item.AuthEnabled { //nolint:staticcheck // reading the deprecated field is the migration
			item.AuthMode = AuthCredentials
		}
	}
	item.AuthEnabled = false //nolint:staticcheck // same
}

// checkAuthModes refuses a mode the entry cannot act on.
func checkAuthModes(items []RegistryItem) error {
	for _, item := range items {
		switch item.AuthMode {
		case AuthAnonymous:
		case AuthCredentials:
			// A member shares its group's host, so it shares its group's single
			// credential entry; a password of its own is not something
			// `docker login` can store (§3.8).
			if item.Parent != "" {
				return fmt.Errorf("registry %q is a member of %q and cannot hold credentials of its own: use %q or %q",
					item.Slug, item.Parent, AuthInherit, AuthAnonymous)
			}
		case AuthInherit:
			if item.Parent == "" {
				return fmt.Errorf("registry %q declares %q but belongs to no group", item.Slug, AuthInherit)
			}
		default:
			return fmt.Errorf("registry %q declares the unknown auth mode %q", item.Slug, item.AuthMode)
		}
	}
	return nil
}

// applyRegistryKind fills in kind and provider for an entry that predates them.
func applyRegistryKind(item *RegistryItem) {
	legacyGroup := item.Kind == "" && looksLikeLegacyNexus(*item)

	if item.Kind == "" {
		item.Kind = KindRegistry
		if legacyGroup {
			item.Kind = KindGroup
		}
	}
	if item.Kind == KindGroup && item.Provider == "" {
		item.Provider = ProviderGeneric
		if legacyGroup {
			item.Provider = ProviderNexus
		}
	}
}

// looksLikeLegacyNexus reports whether an entry written before `kind` existed
// would have been probed as a Nexus group.
//
// NexusDetector.CanHandle used to return true for either of these, so both are
// migrated into the declaration that now says so. The path test over-declares:
// a Nexus *hosted* repository also lives under /repository/ and is not a group.
// That is deliberate — detection answers "not a group" for it exactly as it does
// today, and `kind: group` is visible in the table and one keystroke from being
// corrected, whereas quietly dropping a real group's discovery would not be.
func looksLikeLegacyNexus(item RegistryItem) bool {
	return strings.TrimSpace(item.ManagementURL) != "" || strings.Contains(item.URL, "/repository/")
}

// declaredSlugs collects the slugs the file states, rejecting a duplicate.
func declaredSlugs(items []RegistryItem) (map[string]bool, error) {
	taken := make(map[string]bool, len(items))
	for i := range items {
		slug := strings.TrimSpace(items[i].Slug)
		items[i].Slug = slug
		if slug == "" {
			continue
		}
		if taken[slug] {
			return nil, fmt.Errorf(
				"two registries declare the slug %q: a slug identifies a group's members and must be unique", slug)
		}
		taken[slug] = true
	}
	return taken, nil
}

// ProposeSlug returns the slug to offer for an entry that declares none: the
// derived one, or a fallback when there is nothing to derive from. It is what
// the form suggests and what normalization writes.
func ProposeSlug(item RegistryItem) string {
	if base := DeriveSlug(item); base != "" {
		return base
	}
	return fallbackSlug
}

// uniqueSlug returns base, or base-2, base-3… until one is free.
func uniqueSlug(base string, taken map[string]bool) string {
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}

// checkParents refuses a member whose group is not in the list, which would
// otherwise be an entry nothing can ever reach.
func checkParents(items []RegistryItem, taken map[string]bool) error {
	for i := range items {
		parent := strings.TrimSpace(items[i].Parent)
		items[i].Parent = parent
		if parent == "" {
			continue
		}
		if !taken[parent] {
			return fmt.Errorf("registry %q names the parent group %q, which is not configured", items[i].Slug, parent)
		}
	}
	return nil
}
