# Registries — the model and the group cache

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Registry model

`RegistryConfig.Registries` is one flat list holding both plain registries and
repository-manager groups, told apart by `kind` (§3.8). A group fronts several
registries and is pullable itself, which is why they share a list.

A new context starts with one entry, Docker Hub (`config.DefaultHubRegistry()`);
see `configuration.md`, "The default Docker Hub registry".

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
asks `<url>/v2/<prefix>/<repo>/tags/list` (through `oci.ListRegistryTags`, the one tag lister, which follows `Link` pagination), the pull reference is
`<url>/<prefix>/<repo>:<tag>` — so they cannot mean two different repositories.
That disagreement is exactly what D39 was: `NexusDetector` synthesised a member
as `host/repository/<name>`, which browses (200) and cannot pull (404), because
the Docker client puts `/v2/` first and the whole path after it.

Like `provider`, it is **declared, never sniffed**. Whether a repository answers
on a path prefix, a connector port or a subdomain is a setting on that repository
(`docker.httpPort`, `docker.httpsPort`, `docker.subdomain`), and the one endpoint
carrying it refuses an ordinary pull account (403 or 404, see "Known limits" below) — the same endpoint that
would say what a group's members are. An instance that refuses one refuses both,
so guessing is not a fallback, it is the defect.

The same declaration drives the image search header (DEVDESK-308, reworded by
DEVDESK-629): a `provider: nexus` + `kind: group` entry, its discovered members
and a declared entry with `parent: <slug>` pointing at such a group get a role
(group, proxy, hosted, from the member's `Type` that discovery stores), and the
browser's level 1 header says on a `May omit images` line which of them may not
have listed every image. The provider is read from the config
(`config.IsNexusGroup`), never guessed from the URL. See `network.md`, "Level 1
header summary".

The image search (`network.md`, `CatalogSearcher`, DEVDESK-306) reads the same
pair, with `auth_mode` through `credsFor`. Two ways to read a catalog:

- **A Nexus member** (a discovered member of a `provider: nexus` group, or a
  `parent:` entry of one, DEVDESK-579) is read on its own path,
  `/v2/<repo_prefix>/_catalog`. Nexus (3.98.1-01 Community, measured on the bench
  on 2026-11-17) answers with names that do not carry the
  prefix; DevDesk rebuilds `<repo_prefix>/<name>`. The pull account gets 200
  and the full list on `_catalog`, like admin (same version, bench, 2026-11-17;
  hypothesis H2 confirmed). Not measured: an account restricted to one
  repository, the company instance. An empty catalog is "no
  result"; a 404, 401, 403 or 405 is refused. One walk per member.
- **Any other entry** walks the root catalog of its host (`/v2/_catalog`), keeps
  the names under `<repo_prefix>/` and matches the typed text on the part after
  it. A catalog with no name under the prefix is refused, not "no result".
  Entries that share an API URL and an identity share one walk per search
  (DEVDESK-311, `network.md`); an entry with other credentials, another port or
  another subdomain is walked on its own.

**A discovered group is searched as one unit.** The members the detector found
with no address of their own (same URL and `repo_prefix` as the group) are merged
into one search unit, the group, labelled `<alias> (group)`. Nexus reads the
catalog through the group without saying which member holds a name, so rows carry
no member. The picker's selection does not narrow this: a single ticked bare
member searches the whole group, and an unticked member's name can appear. A
group that was never discovered (a pull account) is searched as itself. See
`network.md`, "Image search and Nexus members".

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

An entry whose URL carries a path (a Nexus `/repository/<name>` connector,
`http://127.0.0.1:30481/repository/docker-hosted`) finds that login too
(DEVDESK-635): `docker.GetStoredCreds` and `docker.RegistryLoginState` share
`credentialPhases`, which looks under the exact address first, then under the
entry's `host[:port]` **as written** (`hostKey`, with its `https://` and
`http://` variants and the credential helpers asked for that key). The port is
never added, dropped or widened: `nexus.example` and `nexus.example:443` are two
keys, and a login stored for `127.0.0.1` is not found for an entry on port
30481. The exact key wins over the host's. `auth_mode: anonymous` still sends
nothing, whatever the lookup could find; `credsFor` reads it first.

**The registry host's login never goes to another management host (DEVDESK-629,
M1).** `discoveryCreds` (`internal/ui/oci_resources/commands.go`) looks at the
host of `management_url`: when it is not the registry URL's `host[:port]` (compared
case-insensitively, a scheme-less URL read as https), only a login stored for the
management host is sent there, and nothing found means anonymous; the registry
host's login, which the host key above now finds for a URL with a path, is never
used for it. A username or password stated by the user still outranks any stored
one. Same host: unchanged.

**Known risk, left as is (DEVDESK-629, decided by the user).** A login stored for
`h` or `https://h` also serves an entry whose URL is `http://h/path`, so the
Basic header goes in clear over http. That was already true for a URL without a
path; the host key extends it to a URL with one. A separate story may treat "no
Basic over http outside loopback".

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


## The default Docker Hub entry (DEVDESK-663, D171)

A new context starts with one registry entry, Docker Hub, so that `:oci`
`b` can search and list tags on a first launch without any setup. The product
decision is in `docs/product/default-registries/` (`brief.md`, `stories.md`);
the config side is in `configuration.md`, "The default Docker Hub registry".

**Paths that create it**

- `:context new` (`config.CreateContext`, `WriteNewContext`).
- The first launch: `ensureDefaultContext` writes `config.yaml` from the same
  config, and `sessionConfig` (`main.go`) rereads it when the file was just
  created. Without the reread the session would run on `Default()` and its
  first save would write the file without the entry. A failed reread keeps the
  config the session had and logs a warning.
- `dk setup`: `buildConfig` (`internal/ui/setup/answers.go`) sets it itself. A
  confirmed overwrite replaces the whole file, so the old registries go and
  the new context carries Docker Hub only (DEVDESK-662).

**Where it is injected, and why there**

`newContextConfig` (`internal/config/context_create.go`) and `buildConfig`
call `config.DefaultHubRegistry()`, the single constructor. Never put it in
`Default()` or `applyDefaults`: both also build the config `Load` falls back
to, so a user who deletes the entry would see it come back. Each call builds
its own slice.

**An existing context is never modified.** No migration adds the entry to a
file that already exists, and a deleted entry stays deleted.

**The values**

| Field | Value | Why |
|---|---|---|
| `url` | `docker.io` | `oci.IsDockerHub` recognises only `docker.io` and `registry-1.docker.io`. For those the search goes to `HubSearcher`. `index.docker.io` is not recognised: the search would go to `/v2/_catalog` and fail (measured, see below). |
| `slug` | `docker-io` | Written explicitly, as `RegistryForm` would require. |
| `kind` | `registry` | A plain registry, not a group. |
| `provider`, `username` | empty | Nothing to declare, no account. |
| `auth_mode` | `anonymous` | No credentials are sent; the `docker login` of the user is not used for this entry. |
| `alias` | empty | A non-empty alias rewrites the display of every `docker.io/...` image in `:oci` and `:sec` (`docker.ApplyAliases`, fed by `registryalias.From`). |

The entry is an ordinary one: `E` edits it, `D` removes it. `U` is refused on
it with `This registry is marked anonymous` (`loginSelectedRegistry`).

**What was measured (2026-11-21)**

`docs/measurements/2026-11-21-docker-hub-anonymous.md`, on `main` at
`1a20aee9`, a snapshot of Docker Hub's behaviour that day:

- Search: `HubSearcher`, `GET https://index.docker.io/v1/search`, 200, 50
  results, no credentials.
- Tags: `registry-1.docker.io` with an anonymous token from
  `auth.docker.io`; dates from `hub.docker.com` for the 100 most recent tags
  only.
- The pull quota header on the manifest answer read `100;w=3600`; `research.md`
  says 100 pulls per 6 hours. The measure records the header and does not
  settle the difference.
- Not verified: the pull of an image end to end. DevDesk reached the
  `docker pull` step, but the account that ran the measure could not reach the
  Docker daemon.
- With Docker Hub unreachable, search and typed-repository tag errors show
  nothing in the footer (gaps G1 and G2 of the note).

## Where the login lives — the `Logged` column

`docker.RegistryLoginState` reads the engine's auth file and answers three
ways: no credentials, credentials held by a helper (`credsStore` /
`credHelpers`), or a secret **inline** in the file — base64, which is not
encryption. The `Logged` cell is `IconOK`, `IconWarning`, or `IconError`
accordingly, and the footer names the file while an inline row is selected.
DevDesk never wrote that secret (`login` goes through the CLI) and cannot move
it; saying so is all it can do (§3.68).

What `login` writes differs between engines, measured rather than assumed:
docker leaves an empty `auths` entry behind a helper, podman leaves none under
`credHelpers` and ignores `credsStore` entirely. Both run
`docker-credential-<name>` helpers — podman has no prefix of its own (D73).

The lookup has two phases for an entry with a path (exact key, then
`host[:port]`, DEVDESK-635); the `Logged` cell and the credentials sent agree.
Logout (`U`) is unchanged and stays on the exact key; known limit: `docker
logout <url with a path>` may remove the host's login, since the CLI keys on the
host, which then also affects the other entries of that host.

## Registry group cache

`internal/cache/registry_groups.go` — `RegistryGroupCache`, keyed by **group
slug**, metadata at `~/.devdesk/cache/registry-groups.json`. It holds the members
one discovery found and when: discovered members are derived data with a server
as their source of truth, and `config.yaml` is what the user declares.

- Discovery runs **only for `kind: group`**, and only when asked: `ctrl+r` on a
  group row in the Registries tab. `detectRegistryGroupCmd` writes through.
- Discovery has **three outcomes** (DEVDESK-510), and `detectRegistryGroupCmd`
  writes at most once:
  - A **settled answer** is stored: a listing, an empty result ("asked, and it
    is not a group") or a group listed with no member (`0 members`). It clears
    any refusal for the slug.
  - A **refusal** (`registrymgr.ErrMembersRefused`, a `*MembersRefusedError`)
    is stored *beside* the listing, never over it: the manager said it is a
    group and refused to list it to this account, which says nothing about the
    members. A 401 or 403 from the management API counts, and so does the one
    404 Nexus sends an account without `nx-repository-admin` (DEVDESK-578):
    `status` is then 404. The member list is never written for it.
  - Any **other error** (network, deadline, any other 404, 5xx, unreadable
    reply) is stored nowhere: an unreachable manager must not erase what was
    last known (D23). The footer says `Group refresh failed — check logs` at level Error;
    a refusal held from an earlier probe stays as it was.
- **Each member carries its type (DEVDESK-636).** `RegistryGroupMember.Type`
  (`type`, `omitempty`: `hosted`, `proxy`, `group`; absent means unknown) comes
  from one GET per member during the discovery (`memberTypes`, 32 members at
  most, see `network.md`, "The Nexus detector"); a failed lookup leaves it
  unknown and never fails the discovery. `toCachedMembers` is the one
  conversion from `registrymgr.GroupMember` and `cacheGroupMembers` uses it. A
  cache written before the field has no type until the next `ctrl+r`. The file
  version stays 2 (additive; an older build ignores the key). The MCP output does
  not carry the type. The value logged for an unrecognized `type` is cut to 64
  characters before `%q`.
- **File format v2.** `registry-groups.json` is the envelope
  `{"version":2,"groups":{…},"refusals":{…}}`, written atomically; a refusal is
  `{at, status, anonymous}`, no text, no URL. Reading goes through one decoder
  (`decodeRegistryGroupFile`) for the cache and for `ReadRegistryGroups`, which
  returns a `RegistryGroupSnapshot{Groups, Refusals}`. A file with no numeric
  `version` key (or one below 2) is the old bare map: its entries with no member
  are dropped, because an empty entry may be a refusal the old code swallowed.
  Those groups read as `never` until the next `ctrl+r`; nothing is rewritten
  until the next `Set`, `SetRefusal` or `Delete`. An older build cannot read a v2
  file (it expects a map of slugs, logs an error and shows no discovery) and
  overwrites it on its next write: the cache is lost, not corrupted.
- **The Nexus detector does not follow a redirect to another origin
  (DEVDESK-412).** Its client refuses it (`oci.RefuseOffOriginRedirect`), so
  the account is never sent to a host it was not given for. That includes the
  redirect from `http` to `https` of the same host: the origin differs, so enter
  the registry URL in `https`. A refused redirect is a failed discovery (D23): no
  members, the cached ones stay, and the log says the redirect was refused. The
  anonymous retry of the members endpoint starts on any error except this
  refusal. Each Nexus reply is read up to 1 MiB (`maxNexusResponse`, not
  measured); a larger one is an error, not a silent cut. A format endpoint
  that answers 401, 403 or the Nexus 404 (below) is logged with the advice
  `account may lack nx-repository-admin privilege` and is a refusal (above);
  the origin refusal has its own log message, without that advice, and is a
  failed discovery. No members come back in either case.
- **A Nexus reply that is not a repository document is a failed discovery
  (DEVDESK-509).** A non-200 status is a typed error shown as
  `management API returned N` (it was `HTTP N`); an empty body, invalid JSON or
  JSON with no `type` (`{}`, `null`) is an error too, not "not a group". Neither
  is cached (D23), so the cached members stay. A cancelled or timed-out request
  reports the context's error, never a status error.
- The `Members` column shows `count · TimeAgo(discovered_at)`, or `never`. It is
  not decoration: a cache with no visible age looks current whatever it holds.
  A refused group shows `denied · <age>`, or `denied · N known · <age>` when a
  listing is cached (N is still used); the age is the refusal's. State first, so
  a narrow terminal that cuts the end keeps `denied`. The column is
  `SizingContent`, in the warning colour, and whole from 115 columns. A refusal
  no more recent than the listing is stale and ignored (`groupRefusal`).
- **While a group row is selected**, the status line is derived on every render
  (`groupMembersStatus`): Warn for a refusal (`Members could not be read with
  this account`, or `without credentials` for an anonymous probe, then the way
  out: declare them by hand with `repo_prefix`, or `N declared by hand are
  used`), Info `No member found — this group is empty` for a settled empty
  group with nothing declared. The refusal text carries no status line from the
  manager and no URL.
- **`→` on a refused group** opens its members when any are cached or declared;
  otherwise the footer warns (`ctrl+r` is not offered, a refusal would repeat).
  The members level lists the discovered members, then the config entries
  whose `parent` is the group and that the cache does not already hold.
- **Rule 110 holds**: the Cmd writes the disk and returns `RegistryGroupDetectedMsg`
  with the instant `At` it wrote; only the handler changes `groupCache` and
  `groupRefusals`, with that same `At`.
- **`registries_list` (MCP)** reports the refusal as `members_refused` beside
  `discovery`, never in place of it (`mcp.md`).
- **Known limits.** Two processes writing the cache at once: the last write
  wins, as before. The 403 a Nexus gives a pull-only account on the format
  endpoint (D161) is explained once, in the next bullet; what stays open is
  listed in "Measured Nexus behavior" in `network.md`.
- **Measured on the local bench, Nexus 3.98.1-01 Community, 2026-11-17 (NX-03,
  fixtures of DEVDESK-511).** The pull
  account gets **404 `Repository not found`**, not 403, on the detailed group
  endpoint; the anonymous call gets 403. The 403 of D161 was a hypothesis the
  bench disproved. The 2026-08-10 observation of a 403 came from a company
  instance of unknown version; it was not cross-checked, so whether the
  difference is the version or the privileges is open. Since DEVDESK-578 (NX-07b) that 404 is a refusal like the
  403: a `*MembersRefusedError{Status: 404}` whose `Cause` is the
  `*oci.StatusError`, stored in `refusals` with `status: 404`, shown as
  `denied · <age>`, with declared members (`repo_prefix`) still listed.
  `TestDetectGroupOnMeasuredNexusAnswers` pins it on the recorded answers. Only
  a 404 whose JSON `message` is `Repository not found` (inner quotes and
  surrounding spaces removed, case ignored) is requalified; any other 404 (empty
  or HTML body, another JSON, a body over 4 KiB, a 404 on the generic
  `/v1/repositories/<name>` endpoint) stays a failed discovery. **Known limit:**
  Nexus sends the same body for a group that does not exist. If the group is
  deleted between the two calls (the first one established it is a group),
  `denied` shows wrongly until the next successful `ctrl+r`. The detection
  tests replay recorded answers, see "Recorded fixtures" in
  [`nexus-bench.md`](nexus-bench.md).
- The **registry browser reads this cache**, never the network: it opens on the
  first frame and works offline. `→` on a group row in the Registries tab drills
  into its members, `←`/`esc` go back.
- **The image search (level 1) reads the same entries, keys and credentials**
  as the tag flow, and the selection applies to both form actions, Search and
  Open Tags (`submit(action)`, `network.md`): the checked entries (`selected(entry)`, D40), `credsFor`
  (anonymous stays anonymous, D12) and the entry's repository prefix (D39). It
  writes nothing: the remembered selection is unchanged (`network.md`).
  `→` on a level 1 row goes to that row's registry only, whatever the
  selection (DEVDESK-304, `network.md`). Each checked entry gets one verdict per
  search (DEVDESK-303), and `dhi.io` is refused without a request (`network.md`).
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

