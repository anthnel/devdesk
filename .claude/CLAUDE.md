# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> [!IMPORTANT]
> **AI Collaboration Rule**:
> - **Planning**, **Brainstorming**, **Implementation** and **Bug fixes** are all
>   in scope. Planning is no longer delegated to Gemini (Antigravity).
> - Implementation plans go in `.claude/plans/` (see
>   [rules/planning.md](rules/planning.md)); design decisions that outlive a plan
>   are recorded in `docs/backlog.md`.
> - Always refer to [project-context.md](file:///home/anthoni/projects/gitlab/devsecops/devdesk/project-context.md) for global project rules.

## Project Overview

DevDesk is a terminal-based TUI (Text User Interface) application built with Go and Bubble Tea framework. It provides DevSecOps functionality including:
- System status monitoring (HTTP/HTTPS, ICMP, DNS, SSL checks)
- GitLab integration (authentication, project explorer, clone/pull)
- Security scanning (Trivy CVE/misconfig/SBOM + Gitleaks secrets)
- Docker container management with real-time metrics
- OCI resource management (image scanning, container launching, network inspection)
- Network diagnostics (ICMP, DNS, TCP traceroute, Netcat, HTTP, SSL) + real-time port monitoring
- Multi-context configuration management via YAML
- Local workspaces management with git metadata

## Build and Development Commands

Tasks live in `mise.toml` — there is no Makefile. `mise tasks` lists them all.

```bash
# Development (fastest)
mise run dev          # Run directly with go run
go run .

# Build
mise run build        # Build binary to bin/dk
mise run run          # Build and run

# Testing
mise run test         # Run all tests
mise run cover        # Coverage per package
go test -v ./...      # Run all tests with verbose output
go test ./internal/command  # Run tests for specific package
mise run test-race    # Detect race conditions (critical for Bubble Tea Cmds)

# Code Quality
mise run fmt          # Format code
mise run vet          # Run go vet
mise run lint         # MANDATORY before commits - golangci-lint
mise run tidy         # Tidy dependencies
mise run check        # fmt + vet + lint + test in one go

# Installation
mise run install      # Install to $GOPATH/bin
```

`test-race` needs cgo and therefore a C compiler (`gcc`/`clang`) on `PATH`.

## Git workflow — pull requests only

**Never push directly to `main`.** The Entire mirror rejects it:

```
! [remote rejected] main -> main (protected branch)
```

This is enforced by the mirror, not by GitHub (`gh api repos/anthnel/devdesk/branches/main`
reports `"protected": false`). Every other branch, including
`entire/checkpoints/v1`, pushes through the mirror and is forwarded to GitHub.

```bash
git switch -c <branch>                          # work
git push origin <branch>                        # via the mirror — forwarded to GitHub
gh pr create --base main --head <branch>
gh pr merge <n> --squash --delete-branch
git fetch github main && git merge --ff-only github/main   # see below
```

**After a merge, fast-forward from `github`, not from `origin`.** The mirror
lags GitHub by a minute or two, so `git fetch origin` right after
`gh pr merge` returns the *previous* `main` — with no error, which is the part
that misleads. `gh pr view <n> --json state` says `MERGED` while
`git rev-parse origin/main` still points at the commit before it.

Pushing branches still goes through `origin`: the mirror forwards them, and it
is the regional path. It is only the read-back immediately after a merge that
has to come from the source of truth.

**Merging several branches cut from the same commit conflicts in
`docs/backlog.md`.** Every fix inserts its entry at the top of §1.1, so the
second and third merges land on the same anchor. The resolution is always to
keep both sides — they are independent entries, not competing edits.

### Remotes

| Remote | URL | Use |
|--------|-----|-----|
| `origin` | `entire://aws-eu-central-1.entire.io/gh/anthnel/devdesk` | Entire mirror — clone, fetch, push branches |
| `github` | `https://github.com/anthnel/devdesk.git` | source of truth; fallback for direct pushes |

The repository is hosted on GitHub and mirrored to EntireDB in `aws-eu-central-1`
(a second placement exists in `aws-us-east-2`). Prefer `origin` for day-to-day
work: it is the regional path and is what keeps agent reads fast.

Semantic search (`entire search`, and the `entire:*` skills that depend on it) is
**not available in the EU region** — the server answers `semantic search is not
yet available in the region(s) hosting this search`. Nothing in the repo config
fixes this.

## Architecture

### Bubble Tea Application Structure

This is a **multi-view TUI application** using the Elm Architecture (TEA) pattern via Bubble Tea. Understanding the routing and view lifecycle is critical:

**Main Components:**
- `main.go` - Entry point, initializes config and Bubble Tea program
- `internal/app/app.go` - **Router/orchestrator** that manages view switching and command mode
- `internal/app/messages.go` - Cross-view messages (GitLab, scan results, pull, etc.)
- `internal/shared/state.go` - SharedState for cross-view data (GitLab client, user, stats)
- `internal/ui/*/` - Individual views

**Key Architecture Pattern:**
```
App (Router)
├── Manages: currentView, commandMode, viewport, sharedState
├── Routes messages to active view
├── Handles view switching via command parser
├── Manages multi-context configuration
└── Views (lazy-loaded):
    ├── dashboard       - Overview (stats, tools, service status)
    ├── status          - System monitoring (CRUD monitors)
    ├── gitlab-auth     - GitLab authentication form
    ├── gitlab-explorer - GitLab project/group browser + clone
    ├── workspaces      - Local workspace management + git metadata
    ├── security        - Trivy + Gitleaks scanner with multi-tab results
    ├── containers      - Docker container list + live metrics
    ├── oci-resources   - OCI resource list, scan, launch containers, network inspection
    ├── netdiag         - Network diagnostics (Docker-based tools) + real-time port monitor
    └── configuration   - Every scalar setting in the current context
```

### View Switching & Command Mode

Press `:` to enter command mode, then type:
- `dashboard` or `d` - Switch to dashboard view
- `status` or `s` - Switch to status view
- `gitlab-auth` or `gla` - Switch to GitLab auth view
- `gitlab-explorer` or `gle` - Switch to GitLab explorer view
- `workspaces` or `w` - Switch to workspaces view
- `security` or `sec` - Switch to security scanner view
- `containers` or `c` - Switch to containers view
- `oci-resources` or `oci` - Switch to OCI resources view
- `netdiag` or `net` - Switch to network diagnostics view
- `configuration`, `config` or `cfg` - Switch to the configuration view
- `context <name>` or `ctx <name>` - Switch configuration context
- `context list` - Show available contexts
- `quit` - Exit application

Command parsing and tab-completion live in `internal/command/`. `ParseCommand()` returns a structured `Command{Type, View, Args}` supporting `CommandView`, `CommandContext`, `CommandQuit`, `CommandUnknown`.

**There is no `:theme` command.** The theme is a setting, so the configuration
view owns it — the picker wrote `app.theme` behind the settings form's back,
which is one setting with two writers. Its overlay, `internal/app/theme.go` and
`CommandTheme` are all gone; `applyThemeNow` in `internal/app/configuration.go`
is what swaps the palette now.

**Important:** The `FormView` interface (`InEditMode()`) prevents command mode activation when forms are active. Views with active forms must implement this interface.

**`HeaderView` is all four methods or none.** The router probes for it with a
type assertion and falls back silently, so a view supplying `GetTitle` and
`GetShortcuts` but not `GetIcon` and `GetHeaderInfo` satisfies nothing and
renders an empty viewport title — with nothing to say so. `command.ViewNames()`
and `TestEveryViewSuppliesItsHeaderAndHelp` turn that into a contract every view
is checked against.

`ViewNames()` is **not** `FullNames()`: the latter also carries the action
commands (`context`, `theme`, `quit`), which is right for completion and wrong
for anything meaning "a view" — the configuration view's `default_view` field
offered `quit` as a landing view until they were separated.

### Multi-Context Configuration

The app supports multiple configuration contexts (e.g., work, personal, client-A):
- Contexts are stored in `~/.devdesk/contexts/<name>/config.yaml`
- Current context is tracked in `~/.devdesk/current-context`
- Each context has isolated GitLab credentials via Git Credential Manager
- Context switching reinitializes all views with new config

### Configuration System

Config loaded from `~/.devdesk/config.yaml` with schema defined in `internal/config/config.go`:
- `App` - Global settings (theme, default view, workspaces dir, `secret_backend`)
- `Status` - Monitoring settings (refresh interval, components)
- `GitLab` - GitLab URL and clone settings
- `Registry` - OCI registry configuration (see Registry model below)
- `Scan` - Security scanning (Trivy, Gitleaks)

**No secret goes in this file.** `GitLabConfig` has no `Token` and
`RegistryConfig` has no `Password`; both live in the host secret store (see
Credentials Management). Do not add a secret-bearing field back — the schema is
what makes the guarantee checkable.

Config is injected into views at creation. Use `config.Save()` to persist changes.

### Configuration view — `internal/ui/configuration`

Edits every **scalar** setting a context carries, in five tabs (`app`, `gitlab`,
`scan`, `docker`, `status`). Lists stay where they are consulted: monitors keep
their CRUD in `status`, registries keep `RegistryForm` in `oci-resources`.
Duplicating them here would be the opposite of the point.

Tabs are not decoration. Rule 135 reserves `Tab` for tabs and `↑↓` for fields,
so a tabbed form is the only layout where both keys have exactly one job.

Settings are declared as a table of `field` values in `fields.go`, each holding
**one pointer accessor** into the config (`func(*config.Config) *string`) rather
than a get/set pair. Twenty-nine settings with two closures each is where the
copy-paste defects of §2 came from; one reference means the read and the write
cannot disagree about which setting they mean.
`TestEveryFieldCarriesTheAccessorItsKindNeeds` and
`TestNoTwoFieldsAddressTheSameSetting` are what keep the table honest.

Fields inside a tab are grouped under a heading with a Nerd Font icon
(`SubTitleStyle`, the same treatment the security form used): `scan` separates
**Scanners**, **Trivy**, **Gitleaks** and **Limits**. `group()` stamps the
heading onto a contiguous run rather than each field carrying its own, so a run
cannot be split by a typo and render its heading twice —
`TestEachTabRendersItsGroupHeadingsOnceInOrder` pins that.

`GetTitle()` carries the context (`󰙨 Configuration · default`): a configuration
belongs to one, and editing `workspaces_dir` in the wrong context is otherwise
silent, because the fields look identical in all of them.

Chevrons and values are aligned on one column per tab, padded on the **head**
(label plus a cycle field's select icon) rather than on the label — padding the
label leaves a cycle field's chevron two cells right of every other. Checkboxes
are excluded from the measurement: they have no value, so a long checkbox label
would push every value right for nothing. `theme.RenderCheckbox` already emits
the focus indicator, so the view must not add a second.

| Kind | Control | Persists |
|---|---|---|
| closed set | cycle `←→` (Rule 132) | immediately |
| boolean | checkbox, `Space` only | immediately |
| text / integer | `textinput` | on blur, **after validation** |

**A refused value keeps the cursor on its field.** An unparseable integer or a
malformed Trivy address is reported (Rule 128) and *not* written — coercing to
zero is how `trivy_server: ":"` reached a config file in the first place.

Two settings are special-cased, matched by label:

- **Theme** applies as it is cycled, not on blur — otherwise the user chooses
  blind.
- **Secret backend** is confirmed when focus *leaves* the field, not on every
  `←→`, and **nothing is migrated between backends**. §3.9 removed the option
  that wrote a token to two stores at once; copying one here would rebuild it.
  Declining restores the previous value.

**`gitlab.url` belongs to this view, not to the auth view.** Both used to write
it, so neither was authoritative and editing it in one left the other stale. The
auth view now shows it read-only, points at `:config`, and owns only the token
and the act of logging in — which is where the §3.9 line falls: this view's
contract is "everything here goes to `config.yaml`", and a token never does.

Changing the URL closes the client-side GitLab session (`GitLabURLChanged` on
the message) and says so, rather than forbidding the change — the same call as
for the secret backend. The field is recognised by **accessor identity**
(`f.str(cfg) == &cfg.GitLab.URL`), not by label: two tabs could both hold a
field called "URL".

`ConfigSavedMsg` goes to the router, which drops every view *except this one* so
they rebuild against the saved config — keeping the configuration view is what
stops a save throwing away the cursor after every keystroke. `BackendChanged`
is separate because it is the one change no view can rebuild itself into: the
router has to resolve a fresh `credentials.Selection`.

### Registry model

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

### Shared State

**A session is set and cleared by the router, both ways.** `setAuthenticated`
and `clearAuthenticated` in `internal/app/gitlab.go` are mirrors, and every
GitLab-backed view reads `sharedState` rather than holding its own answer. A
view resetting only its own fields is what D28 was: logging out left
`GitLabClient` and `CurrentUser` in place, so the explorer kept browsing and the
header kept naming a signed-out user.

Clearing `sharedState` does not empty a table a view already loaded, so a
session ending also drops the views — all but the one on screen that reported
it.


`internal/shared/state.go` holds cross-view data injected at view creation:
- `Secrets`, `SecretNotices` — the context's secret store and what the migration off plaintext reported
- `GitLabClient`, `IsAuthenticated`, `CurrentUser` — GitLab session
- `CachedGroups`, `CachedProjects` — GitLab data cache
- `GitLabStats`, `DockerStats`, `OCIStats` — Dashboard counters
- `ServiceStatus`, `ServiceComponents` — Status monitoring results
- `WorkspaceCount`, `Tools []ToolInfo` — Tool availability (Trivy, Gitleaks, Docker)

### Cross-View Communication

Key messages in `internal/app/messages.go`:
- `SwitchViewMsg` — navigate to another view
- `SelectionRequestMsg` / `SelectionResultMsg` — selection mode (e.g., workspaces opened from security view to pick a repo)
- `ImageScanResultLoadedMsg` / `WorkspaceScanResultLoadedMsg` — cached results ready

### Security Scanning

**Where a scanner runs from is configured, not guessed.** `scan.trivy_source`
and `scan.gitleaks_source` take `auto | binary | image`:

| Value | Resolution |
|---|---|
| `auto` (default) | the binary when there is one, the Docker image otherwise |
| `binary` | `trivy_path` when set, else the name on `PATH` — **fails rather than falling back to Docker** |
| `image` | `trivy_image`, even when a binary is installed |

`scan.CheckDependencies(cfg.Scan)` resolves both tools and fills
`DependencyStatus`; `deps.TrivySpec()` / `GitleaksSpec()` hand a `ToolSpec`
(source + binary + image) to the command builders. `ToolSpec` replaced the
`(source ToolSource, image string)` pair those builders used to take — the pair
had nowhere to carry a configured path, which is why `trivy_path` sat unread
for so long (D27). Do not add a positional `binary` parameter back; put it on
the spec.

The loud failure on `binary` is deliberate: falling back to Docker is what made
the unread path invisible, because scans kept working with something other than
what was asked for.

`internal/scan/` orchestrates Trivy + Gitleaks:
- `scanner.go` — runs both tools concurrently, streams progress via `ProgressUpdate` channel
- `trivy.go` — CVE, secret, SBOM, misconfiguration detection
- `gitleaks.go` — secrets detection with custom config support
- `category.go` — **where a finding goes: one rule, for everyone**

**Secret scanning uses both tools, and they are not redundant.** Gitleaks reads
a repository's working tree and git history; Trivy reads the target's content.
Only Trivy's half applies to an image — Gitleaks cannot scan one — which is why
an image scan had no secret stage at all before. Both are gated on
`scan.enable_secret` and feed the one Secrets tab; the Source column names the
tool.

Two consequences worth keeping:

- The vulnerability stage passes `--scanners vuln` **explicitly for images**.
  Trivy's default there is `vuln,secret`, so that stage was running a secret
  scan whose output nothing read — and would now report each secret twice.
- `i` (add to `.gitleaksignore`) is offered for **Gitleaks findings only**. That
  file is matched on a Gitleaks fingerprint, which a Trivy secret does not have;
  `AddToGitleaksIgnore` would fabricate one and report success for a line
  nothing will ever match.

**`scan.Categorize` is the only thing that decides a finding's family.** There
were two rules: `Result.CountFindings` switched on `Source` alone, the security
view's tabs on `Source` plus `PkgName` plus `Match`. Three inputs separated them
— a `trivy` finding with no `PkgName`, an undeclared source, and a `trivy`
finding carrying a `Match` — and each produced a finding **counted in the header
but present in no tab**, so invisible in the table. Classification is on the
source and nothing else now, which is what required Trivy secrets to have a
source of their own (`trivy-secret`) rather than being recognised by a `Match`.
`TestEveryFindingIsCountedExactlyOnce` and
`TestTheTabCountsAgreeWithTheResultCounters` are what hold the two ends together.

**Security view** (`internal/ui/security/model.go`) has three states:
`StateInventory` (the landing page), `StateResults` and `StateDetails` (with
remediation info).

**The scan form is gone** (phase 3), and with it `StateScanning`: there is no
screen that runs one scan and waits on it. The inventory rescans in the
background with a spinner on the row, the way the images list does. What went
with the form, because nothing else read it:

| Gone | Why it existed |
|---|---|
| `form.go`, `renderInputView` and the seven field renderers | the form |
| `StateInput`, `StateScanning`, `renderScanningView` | its two screens |
| `startScan`, the progress channel, `scanGen`, `cancelScan` | running one scan in place |
| `deps`, `checkDependencies`, `DepsCheckedMsg` | the Start button, the last reader of "is a scanner installed" |
| `homeState` | which of the two landing states to return to |
| `SelectionRequestMsg` / `SelectionResultMsg` / `SelectionCancelledMsg` | picking a target by borrowing another view |
| `NewWithTarget`, `NewWithImageTarget`, `NewWithTargetReturnToWorkspaces` | prefilling its fields |

`NewWithPreloadedResult` is the only constructor left besides `New`.

**Only the workspaces view is ever lent now.** The form borrowed it for a
directory and the images view for an image; the explorer borrows it for a clone
destination, and that is all. So `ociresources.NewForSelection`,
`ImageSelectedMsg`, `SelectionCancelledMsg`, `ResetSelectionMsg` and the OCI
view's `selectionMode` are gone, and `app/selection.go` is no longer
parameterised over who is borrowing.

**A missing stored result rescans in the list it came from.** `enter` on a
scanned row asks the router for the result file; when it is gone,
`rescanInOrigin` hands `workspaces.ScanRequestMsg` or
`ociresources.ScanRequestMsg` to that list and stays there — the target lives
there, and so does the scan that replaces it. It used to open the form with the
target filled in. Both message types were declared and unhandled before this;
they have handlers now, which is what they were named for.

`LaunchBatchScanMsg`, `LaunchSingleImageScanMsg` and `app.handleLaunchScan` went
with the form: they carried the *options* the form had collected to whoever
would run the scan, and the options come from the configuration view now.

**The header carries the context and one count, and nothing else.**
`app_header.go`'s `buildInfoLines` renders exactly `headerMinHeight` (7) lines
and drops the rest **in silence**, and the results state used to sit at exactly
7 — an eighth field would have vanished. Most had stopped earning their line:
the Trivy and Gitleaks versions answered "can I scan?" (the dashboard's
question, from `shared.State.Tools`), `Filter` read `ALL` permanently and meant
nothing on the Secrets tab or in the details, and `Secrets`/`Licenses`
duplicated the tab bar a line below. The context replaced them and was the one
thing missing: the scan caches are scoped to a context, so identical-looking
rows mean different things in two of them.

| State | Info |
|---|---|
| Inventory | `Context`, `Targets` |
| Results / Details | `Context`, `Findings` |
| Form / Scanning | `Context` |

### The security inventory

`:sec` opens on **everything the current context has scanned**, read from
`ImageScanCache` and `WorkspaceScanCache` — one `datatable` over images and
repositories, sorted by CRITICAL descending. The form it replaced asked two
questions already answered elsewhere: the options come from the configuration
view, and a target is either a known image or something under `workspaces_dir`.

| Key | Effect |
|---|---|
| `enter` | open the row's stored findings (Rule 126: reads the cache, never scans) |
| `ctrl+s` | rescan the row, **overwriting** its entry |
| `ctrl+a` | **purge** every entry and rescan every target |
| `ctrl+r` | reload from the caches |

**The inventory runs its own scans.** With the options in the config there is
nothing to carry to whoever would run one — which is the only reason the
cross-view delegation exists. It writes to the same two caches, so a rescan here
and `ctrl+s` in the images list are the same operation.

Three invariants, each with a test that fails without it:

- **`ctrl+a` purges the counts, not the rows.** The rows *are* the list of what
  has been scanned; dropping them empties the view for the length of the scans
  and loses the targets on a close. A purged row prints `-`, not `0`.
- **A reload keeps an in-flight scan's marker.** The cache says nothing about a
  scan still running, so a refresh landing mid-rescan would clear the spinner
  and leave the row looking settled.
- **`InventoryScanFinishedMsg` is routed to the security view wherever the user
  is** (`app.routeToSecurityView`), the same reason `routeToOCIImagesView`
  exists. Everything else is forwarded to the active view only, and a lost
  completion leaves a row spinning for the life of the view.

`esc` and `ctrl+r` return to the inventory, or to the list the results were
opened from when `OriginView` is set.

### Registry group cache

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

### Scan Cache

Two independent disk+memory caches in `internal/cache/`:
- `ImageScanCache` — keyed by `"repo:tag"`, metadata at `~/.devdesk/cache/image-scans.json`, full results in `image-results/<sha256>.json`
- `WorkspaceScanCache` — keyed by absolute repo path, metadata at `~/.devdesk/cache/workspace-scans.json`, full results in `workspace-results/<sha256>.json`

**Both are scoped to a configuration context**, because the configuration is:
`workspaces_dir` and the registry list are per context, so two contexts
legitimately hold different roots and different images. A cache is bound to one
context at construction — `NewImageScanCache(config.CurrentContextName())` —
and `Get`, `Set`, `GetAll` and `Delete` only ever see that context's entries.

`internal/cache/scan_file.go` owns the on-disk shape both share
(`{version, contexts: {name: {key: entry}}}`) and the upgrade from the flat
`{key: entry}` file that predates contexts. The legacy file is recognised by
`Contexts == nil` after a successful unmarshal, and the upgrade is **written
back on the first open** rather than deferred to the next `Set`: deferring
would let every context that opened the file claim the legacy entries in turn,
so ownership would depend on which context happened to write first.

The result blobs under `image-results/` and `workspace-results/` are unchanged
— they are content-addressed by SHA256 of the target, and only the metadata
index is keyed by context.

Cache invalidation: `ctrl+s` (single) overwrites; `ctrl+a` (all) purges cache then rescans.

### Docker / OCI Integration

- `internal/docker/client.go` — wraps Docker CLI (exec-based): list, metrics, stop, restart, pause, remove, prune
- `internal/docker/netdiag.go` — ephemeral container runners with `--network host`: `RunPing`, `RunDNS`, `RunTraceroute`, `RunTCPTraceroute`, `RunNetcat`, `RunCurl`, `RunSSLCert` → returns `DiagResult{Success, Output}`
- `internal/docker/network.go` — `RunSS(image, numeric)` for real-time port table (mounts host DNS files, uses `--privileged --net=host --pid=host`), `KillProcess(image, pid)`, `PortInfo` struct, `parseSSOutput()` multi-format parser
- `internal/oci/oci.go` — OCI registry HTTP client: list tags/templates, download + extract tar.gz

### Network Diagnostics View

`internal/ui/netdiag/` — two-tab interface:
- **Diagnostics tab** (`model.go`): Interactive form with target/port inputs and checkboxes to select tests (ICMP, DNS, Traceroute, TCP Traceroute, Netcat, HTTP/HTTPS, SSL). Runs selected tests in parallel via Docker ephemeral containers. Results table uses Nerd Font icons.
- **Ports tab** (`ports_model.go`): Live `ss` monitoring with real-time filtering by protocol (TCP/UDP), state (LISTEN/ESTAB), and text search. `ctrl+k` kills a process (requires privileged container). Active filter shown in status line.

Both tabs use Docker with host network/PID namespaces. DNS hostname resolution uses mounted host DNS files (`/etc/resolv.conf`, `/etc/hosts`, `/etc/nsswitch.conf`).

### Status Monitoring System

**Factory Pattern for Checkers:**
- `internal/status/checker.go` - Main orchestrator
- `internal/status/http_checker.go` - HTTP/HTTPS checks
- `internal/status/icmp_checker.go` - ICMP ping checks
- `internal/status/dns_checker.go` - DNS resolution checks

Each checker implements `CheckerInterface`. The main `Checker.CheckOne()` uses a factory pattern to instantiate the right checker based on `component.Type`.

**Parallel Execution:**
- `CheckAll()` runs all component checks concurrently using goroutines and sync.WaitGroup
- Results are collected and returned as `[]ComponentStatus`

### Credentials Management

No secret DevDesk holds is written to a file DevDesk owns. `internal/credentials`
implements the `Storage` interface three ways and `Select()` picks exactly one —
writing to several at once is what let the old "secure" option store a token in
the credential manager *and* in plaintext:

| Storage | Backend | Notes |
|---------|---------|-------|
| `KeyringStorage` | Windows Credential Manager / macOS Keychain / Secret Service | via `zalando/go-keyring`, no cgo. The default. |
| `GitCredentialStorage` | git's configured credential helper | Only when the helper is not `store`, which writes plaintext. Context-aware. |
| `MemoryStorage` | this process | Last resort, and deliberately worse: the auth view says nothing was saved. |

`Select(context, preference)` returns a `Selection{Storage, Backend, Detail}`.
The order is keyring → git credential → memory; `app.secret_backend` in the
context config pins the head of it (`auto`, `keyring`, `git-credential`). A
pinned backend that is unreachable falls through to memory rather than silently
to the other one.

The router resolves one `Selection` per context and keeps it in
`shared.State.Secrets`. It must be reused for the lifetime of the context — a
second `Select()` call yields a fresh `MemoryStorage` that cannot see what the
first one holds.

`MigrateLegacySecrets` runs at startup and on every context switch: a
`gitlab.token` or `registry.password` left in a context file by an older build
is moved into the store and deleted from the YAML, with the result reported in
the auth view.

### Bubble Tea Message Flow

Custom messages defined in view models (e.g., `internal/ui/status/model.go`):
```go
type TickMsg time.Time           // Countdown timer
type CheckStartedMsg struct{}    // Check initiated
type CheckCompleteMsg struct{}   // Check results ready
```

Commands return these messages to trigger async operations. The Bubble Tea `Update()` method handles them.

### Component CRUD Operations

Status view supports adding/editing/deleting monitors:
- `internal/ui/status/components/component_form.go` - Form component
- `internal/ui/status/components/confirm_modal.go` - Confirmation dialog
- Changes persist to `~/.devdesk/config.yaml` via `config.Save()`

State flags in Model: `creating`, `editing`, `confirming`, `selectedIdx`

## Tables — `internal/ui/datatable`

The shared mechanism behind the application's tables: column widths, sorting,
sort arrows, filter matching, cursor clamping and cursor-to-object resolution.
`theme.DefaultTableStyles()` and friends still own the *look*.

```go
datatable.New(datatable.Config[T]{
    Columns: []datatable.Column[T]{{
        Title: "Name", MinWidth: 20, Flex: 1,
        Cell:   func(x T) string { … },  // plain text — Rule 122 by construction
        Less:   func(a, b T) bool { … }, // nil = not sortable
        Search: func(x T) string { … },  // nil = not searchable
    }},
    SortColumn:     0,
    SortDesc:       false, // true opens on the descending order
    SelectedStyles: func(x T) table.Styles { … }, // e.g. TableStylesForSeverity
})
```

`SortDesc` exists for count columns: ascending is their useless end, and cycling
`.` past it on every open is not a default. A direction with no sortable column
to apply it to is dropped with the column, or the first `.` opens descending
with the arrow on nothing.

**A sortable column's `MinWidth` is not its whole ask.** `titleFor` appends a
sort arrow the view never accounted for, so `solveWidths` reserves
`width(Title) + 2` for any column with a `Less` — otherwise a narrow one renders
`CRIT ▼` into five cells and loses exactly the character that says how it is
sorted. The reserve applies whether or not the column is the sorted one, so
cycling `.` does not resize it and shift every column beside it.

Three things it guarantees that hand-wired tables did not:

- **`Selected()` cannot disagree with the screen.** The filtered, sorted slice is
  built once and kept; nothing replays the pipeline to resolve a cursor.
- **Rule 116 holds at every width.** The solver distributes the shortfall across
  columns rather than clamping each one after the remainder is computed, which is
  how several views overflowed on narrow terminals.
- **The cursor is clamped in one place** — `SetItems`, both ends — and otherwise
  left where it was, so a periodic refresh keeps the scroll position. `GotoTop()`
  is explicit for views that do want a reset.

`SelectedStyles` returns styles rather than a state keyword so the component
never learns what a severity is. `containers` is its one client: it is the only
table whose selection colour depends on the row (exited or dead reads as an
error).

When a row shows something that is not on the domain object — the images tab
shows the scan cache, whether a scan is running, and the alias-substituted name
— the view defines a **row type** carrying that decoration (`imageRow`) rather
than closing over the model. The columns are built once, in `New`, so they
cannot reach it; and carrying it means a column sorts by the same value it
prints.

`SetCursor` is clamped and exists for the views that remember a position across
a reload — workspaces and the explorer each restore one per drill-down level.
The selected row is pinned to the content width: column widths count cells, and
a Nerd Font icon does not always render as wide as it counts, so the highlight
would otherwise stop short of the right border.

**All fifteen tables are migrated.** A new table uses `datatable`; there is no
second way to build one.

Two views keep a filter of their own, and deliberately. `security` selects
findings by tab and by severity, and `status` drives both its tables from one
search box so the header counts agree — in both cases the view filters and calls
`SetItems`, because a `FilterBar` query narrows a list that is already settled
and these decide which rows exist at all. `security` also calls `GotoTop`
explicitly on a tab change, which is the reset `SetItems` does not make.

`status` is the two-table case: two `datatable.Model` plus a focus helper.
`Focus` and `Blur` carry the styles with them (Rule 118), so a tab switch does
not touch `SetStyles`.

## Testing

Test files follow Go conventions (`*_test.go`):
- `internal/command/parser_test.go` - Command parser tests
- `internal/app/app_test.go` - App router tests

Use table-driven tests where appropriate.

## Dependencies

Key libraries (see `go.mod`):
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - Pre-built TUI components (table, textinput, spinner)
- `github.com/charmbracelet/lipgloss` - Styling
- `gitlab.com/gitlab-org/api/client-go` - GitLab API client
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality (maintained fork of go-ping/ping)
- `github.com/zalando/go-keyring` - host secret manager (wincred / Keychain / Secret Service), no cgo
- `gopkg.in/yaml.v3` - YAML configuration

## Code Conventions

- Comments: French or English both accepted
- UI text and logs: **English US only** (Rule 129)
- Bubbletea models define their own message types
- Use `theme` package for consistent styling (`internal/ui/theme/`)
- Config changes must call `config.Save()` to persist
- All colors defined in `theme/colors.go`, all styles in `theme/styles.go`
- Message naming: `[ComponentName][Action]Msg` (e.g., `ComponentFormSubmitMsg`)

## Critical Bubble Tea Rules

**NEVER modify model state inside a Cmd** (Rule 110) - Race condition:
- `Update()` is the ONLY place to modify model state
- `View()` is read-only
- `Cmd` functions do I/O and return messages

**NEVER use `style.Render()` inside `table.Row{}`** (Rule 122) - ANSI sequences corrupt all subsequent rows:
- Use plain text: `theme.IconError + " error"` not `theme.StatusErrorStyle.Render(...)`
- Apply row styling via `table.SetStyles(theme.TableStylesForState("error"))`

**Update() case extraction**: If a case block exceeds 5 lines, extract to `handle[MessageType]()` method returning `(tea.Model, tea.Cmd)`
