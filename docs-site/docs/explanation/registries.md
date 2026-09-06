# Registries

DevDesk can talk to plain container registries and to repository-manager
groups (Nexus, Harbor, Artifactory, and similar) that front several
registries behind one connector. This page explains how those two shapes
share a single configuration model, and how the registry group cache keeps
a discovered group's members available offline.

## One list, two kinds of entry

`RegistryConfig.Registries` is a single flat list holding both plain
registries and repository-manager groups, distinguished by a `kind` field. A
group is itself pullable and fronts several member registries, which is why
both shapes live in the same list rather than two separate ones.

| Field | Meaning |
|---|---|
| `slug` | DevDesk's own identifier — what `parent` points at, and what the group cache is keyed on. Everything that talks to Docker directly stays keyed on `url`, because Docker itself is. |
| `kind` | `registry` or `group` |
| `parent` | slug of the owning group, carried by discovered members (not normally set by hand in config) |
| `provider` | `generic`, `nexus`, `harbor`, `artifactory`, `gitlab` — declared, never sniffed from the URL |
| `auth_mode` | `credentials`, `anonymous`, or `inherit` for a member |
| `repo_prefix` | what's prepended to the repository name; together with `url` this is the entry's full address |

## An entry is an address, not a URL

The pair `(url, repo_prefix)` is what a registry is actually reached by, and
both the browse path and the pull reference are derived from it — browsing
asks `<url>/v2/<prefix>/<repo>/tags/list`, and the pull reference is
`<url>/<prefix>/<repo>:<tag>`. Treating `url` alone as the identity of a
registry invites the two to drift apart: an earlier version of the Nexus
detector synthesized member addresses as `host/repository/<name>`, which
browses successfully (the path exists) but can never pull (Docker's client
always puts `/v2/` immediately after the host, so a repository path baked in
front of it lands in the wrong place).

Like `provider`, how a repository is addressed is **declared, never
sniffed**. Whether a repository manager exposes a member on a path prefix, a
dedicated port, or a subdomain is a setting on that repository, and the one
API endpoint that would reveal it typically answers `403` to an ordinary
pull account — the same endpoint that would say what a group's members even
are. An instance that refuses to answer refuses both questions at once, so
guessing at the shape isn't a fallback; it's the failure mode this design
avoids.

A few consequences follow from treating the prefix as part of the address:

- **The prefix is applied exactly once**, at search time, and becomes part
  of the resolved repository name from then on — nothing downstream needs
  to know about it separately, which would otherwise be two places free to
  drift apart.
- **A group itself may not declare a prefix.** A group is only reachable
  through its own connector; applying a path-prefix trick to a group's own
  address the way it's applied to a member just produces a `404`.
- **A URL no longer uniquely identifies an entry**, now that several
  entries can share one host. Anything that used to key on URL — resolving
  a search result back to its entry, tracking which rows are selected in
  the browser — now keys on the entry itself (or `(group, member)` for a
  discovered member), so two entries on the same host stay independent.

The Nexus detector no longer synthesizes an address at all: for a group
reached as a path prefix, it reports each member as `(host, memberName)`
directly rather than trying to build a browsable-but-unpullable shortcut.
For a group reached through its own connector, it reports the group's URL
with **no** prefix, because which connector a given member answers on is
precisely the thing the detector cannot know from the outside — an empty
prefix against a bare host fails visibly, where a guessed path used to fail
in a way that looked like it might be working.

## Authentication follows the address, not a flag

`auth_mode` is read before any credential lookup, on every code path that
talks to a registry. `anonymous` means literally nothing is sent — not even
a configured username. This matters because Docker's own login is keyed on
host: authenticating once against a registry manager instance used to
authenticate every repository it serves, whether or not that was intended
for a specific member.

That same host-level keying is why a **member can't declare credentials of
its own** — it shares its group's single credential entry, since there's
nowhere else for a per-member password to live — and why `inherit` on a
standalone entry with no group is rejected outright. Both are caught at
config load time rather than surfacing later as a confusing auth failure.

## Slugs and provider detection

`internal/config/registries.go` normalizes the registry list at load time
and is the only place that assigns slugs:

- A slug DevDesk derives on its own (from an alias, or else from the URL's
  host) is made unique by appending a suffix if needed (`prod`, `prod-2`). A
  slug the config file states explicitly is never rewritten — it's a link
  target for `parent` references — so a duplicate, or a `parent` pointing at
  a group that doesn't exist, fails the whole config load rather than
  silently producing something the application can't honor.
- An older entry that predates the `kind` field, but carries a
  `management_url` or has `/repository/` in its URL, is migrated to
  `kind: group`, `provider: nexus` — those were exactly the two signals the
  original Nexus detector used to accept an entry. A `kind` the file states
  explicitly is never second-guessed.

`provider` is what selects a detector in `internal/registrymgr`: matching
proceeds by comparing the declared provider against each detector, falling
back to a generic detector that discovers nothing. A registry is only ever
probed because it was declared as something specific — never because its
URL merely looked like a particular vendor's — so a plain registry costs no
extra HTTP call, and detector registration order has no effect on the
outcome.

## The registry group cache

Discovered group membership is cached separately from configuration, in
`internal/cache/registry_groups.go`, keyed by group slug. The reasoning: a
discovered member is derived data whose source of truth is the server, while
`config.yaml` is what the user has explicitly declared.

- Discovery runs only for entries with `kind: group`, and only on demand —
  triggered explicitly from the registries view, never automatically. A
  successful discovery writes straight through to the cache.
- An **empty** discovery result is itself stored: "I asked, and this isn't
  actually a group" is a real answer worth remembering. A **failed**
  discovery is not stored — an unreachable manager must not erase
  previously known membership, so the two outcomes are handled distinctly.
- A visible "last discovered" age accompanies the member count in the UI —
  without it, a stale cache looks exactly as current as a fresh one.
- **The registry browser reads only this cache, never the network.** It
  opens instantly on first render and works offline; drilling into a
  group's members is a cache lookup, not an API call.
- Per-context state remembers which discovered members a user has
  deliberately *un*-checked in the browser, rather than which ones are
  checked — storing the exceptions is what lets a newly discovered member
  show up checked by default instead of silently excluded until someone
  notices and opts it in.

!!! note
    A picker entry is identified by a stable key — the slug for a
    standalone registry, or a `(group, member)` pair for a discovered
    member — never by URL. Two registries can legitimately share a host (the
    config only enforces unique slugs, not unique URLs), so keying
    selection state on URL would make checking one silently check or
    uncheck the other.
