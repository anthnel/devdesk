# Registries — the model and the group cache

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Registry model

`RegistryConfig.Registries` is one flat list holding both plain registries and
repository-manager groups, told apart by `kind` (§3.8). A group fronts several
registries and is pullable itself, which is why they share a list.

| Field | Meaning |
|---|---|
| `slug` | DevDesk's own identifier: what `parent` points at and what the group cache is keyed on. Everything Docker-facing stays keyed on `url`, because Docker is. |
| `kind` | `registry` or `group` |
| `parent` | slug of the owning group — carried by discovered members, not normally by config entries |
| `provider` | `generic`, `nexus`, `harbor`, `artifactory`, `gitlab`; declared, never sniffed from the URL |
| `auth_mode` | `credentials`, `anonymous`, or `inherit` for a member. Replaces `auth_enabled`, which is migrated at load and then dropped. |
| `repo_prefix` | what goes in front of the repository name. With `url` it is the entry's **address** (§3.18) |

**An entry is an address, not a URL** (§3.18, D39). The pair `(url, repo_prefix)`
is what a registry is reached by, and both directions derive from it — the browse
asks `<url>/v2/<prefix>/<repo>/tags/list`, the pull reference is
`<url>/<prefix>/<repo>:<tag>` — so they cannot mean two different repositories.
That disagreement is exactly what D39 was: `NexusDetector` synthesised a member
as `host/repository/<name>`, which browses (200) and cannot pull (404), because
the Docker client puts `/v2/` first and the whole path after it.

Like `provider`, it is **declared, never sniffed**. Whether a repository answers
on a path prefix, a connector port or a subdomain is a setting on that repository
(`docker.httpPort`, `docker.httpsPort`, `docker.subdomain`), and the one endpoint
carrying it answers 403 to an ordinary pull account — the same endpoint that
would say what a group's members are. An instance that refuses one refuses both,
so guessing is not a fallback, it is the defect.

Three things follow, each with a test:

- **The prefix is applied once**, in `submitSearch`. It is then already part of
  `MultiRegistryTag.Repo`, so `multiImageName` and `registryAPIURL` know nothing
  about it — teaching both would be two places free to drift.
- **A group may not declare one**, and `LoadContext` refuses it: a group is
  reachable only through its own connector, and the same trick applied to a group
  answers 404. `RegistryForm` therefore does not offer the field on a group.
- **A URL no longer identifies an entry.** Several entries share one host now, so
  `entryFor` resolves by entry key, `resultFilter` holds a key, every
  `MultiRegistryTag` carries one, and `memberKey` takes the prefix as a third
  segment — a member's URL is its *group's*. `registryRef(url, prefix)` is the one
  place an entry becomes something a user reads, and it is the head of the pull
  reference exactly.

`NexusDetector` synthesises nothing any more. A group written as a path prefix
serves its members the same way, so it emits `(host, memberName)` — consistent by
construction. Reached through a connector of its own it emits the group's URL and
**no** prefix, because which connector a member answers on is precisely what it
cannot read: an empty prefix against a bare host is wrong and visibly so, where
the synthesised path was wrong and plausible.

**`auth_mode` is read before any credential lookup**, on both paths that talk to
a registry: `detectRegistryGroupCmd` and the browser's `credsFor`. `anonymous`
sends nothing, not even a configured username. This is what `auth_enabled` never
did (D12): `docker login` is keyed on host, so one login against a Nexus
instance used to authenticate every repository it serves.

That same host-keying is why a **member cannot declare `credentials` of its
own** — it shares its group's single credential entry, so a password of its own
has nowhere to go — and why `inherit` on an entry with no group is refused.
Both are load-time errors.

`internal/config/registries.go` normalizes the list at load and is the only
place that decides a slug. Two rules hold it together:

- A slug **DevDesk derives** (from the alias, else the URL host) is made unique
  by stepping aside — `prod`, `prod-2`. A slug **the file declares** is never
  rewritten, because it is a link target; a duplicate, or a `parent` naming no
  configured group, makes `LoadContext` fail rather than load a config the
  application cannot honour.
- An entry from before `kind` existed that carries a `management_url`, **or**
  whose URL contains `/repository/`, migrates to `kind: group`,
  `provider: nexus` — those were the two things `NexusDetector.CanHandle` used
  to accept. A kind the file states is never second-guessed.

`provider` is what picks the detector in `internal/registrymgr`: `DetectGroup`
matches `Detector.Provider()` against the declared value and falls back to
`GenericDetector`, which discovers nothing. A registry is probed because it was
declared as something, never because its URL looked like it — so registration
order decides nothing, and a plain registry costs no HTTP call. The provider
names are stated in both packages on purpose; `TestTheProviderVocabularyMatchesTheConfig`
keeps them in step.

`RegistryForm` is what keeps the file loadable: it refuses a duplicate or
badly-formed slug instead of correcting it, and drops the group-only fields when
the kind is not a group.


## Registry group cache

`internal/cache/registry_groups.go` — `RegistryGroupCache`, keyed by **group
slug**, metadata at `~/.devdesk/cache/registry-groups.json`. It holds the members
one discovery found and when: discovered members are derived data with a server
as their source of truth, and `config.yaml` is what the user declares.

- Discovery runs **only for `kind: group`**, and only when asked: `ctrl+r` on a
  group row in the Registries tab. `detectRegistryGroupCmd` writes through.
- An empty result **is** stored — "asked, and it is not a group" is an answer.
  A *failed* one is not: an unreachable manager must not erase what was last
  known, which is why `registrymgr` distinguishes the two (D23).
- The `Members` column shows `count · TimeAgo(discovered_at)`, or `never`. It is
  not decoration: a cache with no visible age looks current whatever it holds.
- The **registry browser reads this cache**, never the network: it opens on the
  first frame and works offline. `→` on a group row in the Registries tab drills
  into its members, `←`/`esc` go back.
- `internal/cache/browser_selection.go` remembers what the browser had
  **un**checked, per context. Storing the exceptions is what makes a
  newly-discovered member arrive checked rather than silently excluded.
- **A picker entry is identified by `browserRegistryEntry.key`, never by its
  URL** (D40). Two registries may be declared on one host — the form enforces
  slug uniqueness, not URL uniqueness — so a URL ticks and unticks both, and the
  exclusion outlives the session. The key is the slug for a standalone registry
  and `memberKey(groupSlug, memberURL)` for a member; `Slug` alone will not do,
  since a member carries its *group's*. Read it through `selected(entry)` rather
  than indexing `selectedRegs` at a new site.

