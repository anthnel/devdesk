# Configuration — the schema and the view

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Configuration System

Config loaded from `~/.devdesk/config.yaml` with schema defined in `internal/config/config.go`:
- `App` - Global settings (theme, default view, workspaces dir, `show_hidden_files`, `secret_backend`, `gauge_glyph`, `motion`)
  `app.motion` (`blink` or `static`) is per context (D3). The case must be exact:
  `Static` is invalid, kept as read, logged as a WARN at load, and behaves as
  the default. An absent key stays absent (it is not written back at load)
  because the default may change with A-06; an explicit value keeps its meaning
  when it does.
  `app.theme` is a theme name or `auto`. A name resolves to a JSON file under
  `~/.devdesk/themes/` first, then to the 12 files of `themes/` embedded in the binary
  (`themes/embed.go`); `dk setup` downloads nothing. `auto` picks `app.theme_dark`
  (default `default`) or `app.theme_light` (default `github-light`) from the terminal's
  background (`AppConfig.EffectiveThemeDark/Light`, never written back); see
  `app-shell.md`. `theme.LoadTheme` refuses the
  whole file when a non-empty colour is not `#rgb` or `#rrggbb` (error names the key and the
  value, e.g. `color_ok: "#ggg" is not #rgb or #rrggbb`); an empty or absent optional colour
  falls back to the semantic palette. A theme that cannot be used (unknown, unreadable, invalid,
  or under 4.5:1 text contrast when `auto` picks it) gives the default theme and a footer
  warning (`App.chooseTheme`): one per invalid candidate, `theme_dark` and `theme_light`
  both checked in `auto`, whether selected or not. Palette variables are `color.Color`; `theme.Hex`
  formats one as `#rrggbb` (`none` for anything that is not a `color.RGBA`).
- `Status` - Monitoring settings (refresh interval, components)
- `Forge` - the platform this context targets: `type`, URL and clone settings
- `Registry` - OCI registry configuration (see Registry model below)
- `Scan` - Security scanning: `categories` (what a scan looks for, and which
  tools run each one) and `tools` (one block per scanner — see `scanning.md`)
- `Network` - what the netdiag view runs on: the tool image, and the dials
  `internal/netcheck` used to hardcode; and `proxy_port` (8080), the one port
  every named forward route is served on (§3.74, see `network.md`) — at or above
  1024, and replaced by the default with a log line when it is not
- `MCP` - the read-only MCP server (§3.38): `enabled`, off by default, and
  `expose`, an allow-list of tool names
- `AgentUsage` - `agent_usage.prices`, the price table of the `:agent-usage` cost
  estimate (DEVDESK-47); no default, see below

**`forge:` was `gitlab:`, and the rename is migrated rather than announced.**
A context targets one platform (§3.6), so the section is named after the role
rather than after the one implementation there was, and it gains a `type:` —
`gitlab` or `github`, **declared, never sniffed**, on the registry `provider`
precedent. An absent `type` means `gitlab`, which is what every file written
before the key existed meant.

`migrateGitLabSection` runs **first** in `applyDefaults`, and that ordering is
the whole of it. Every default below writes into `cfg.Forge`, so a migration
placed after them finds a block that is no longer empty, takes the
field-by-field path, and silently drops the user's own `parallel_jobs: 7` — that
is not a hazard imagined for the comment, it is what the first draft did and
what `TestAConfigCarryingRetiredKeysStillLoads` caught. The other half of the
reason is `docker:` → `network:`'s: `yaml.Unmarshal` is not strict here, so an
un-migrated block is dropped in silence, and the silence would point a context
at no host at all.

**A whole-block copy when `forge:` is absent, field by field when both are
present.** The distinction exists for one field: `IncludeArchived` is a bool, so
"unset" and "deliberately false" are the same value and no per-field guard can
tell them apart — it can only be carried over when the new block says nothing at
all. When both are present the new one wins field by field: its values were
written later, and overwriting them with the old ones would undo the edit that
created the situation.

`forge.type` is stated in `internal/config` and in each backend's `Shape()`, and
`TestTheForgeVocabularyMatchesTheConfig` holds the two in step — config must not
import a backend, and a backend must not be the authority on what a config file
may say, so a test is the only thing that can. Same arrangement as the registry
`provider` names.

The legacy secret migration is unaffected and the order is worth knowing:
`credentials.MigrateLegacySecrets` reads the **raw file** at router
construction, before anything can save, so a plaintext `gitlab.token` reaches
the store before the rename can rewrite the file without it.

**`network:` was `docker:`.** The one key `docker:` held was never a Docker
setting, and the four other tabs of the configuration view are each named
after the section they write. That key — the image the OCI connectivity test
ran — has since been removed along with the feature itself; `NetworkConfig`
now holds only the netdiag dials (`CheckTimeout`, `PingCount`,
`CertExpiryWarnDays`, `PortsRefreshInterval`).

**`scan:` was flat, and it is now `categories:` + `tools:` (§3.86).** Six
tools had grown three to five keys each, side by side with six `enable_*`
switches that named categories and tools interchangeably. `migrateScanSection`
(`scan_migrate.go`) runs right after `migrateGitLabSection`, before the
defaults, on the same model: field by field, only where the new value is empty,
the old keys held in `ScanConfig.Legacy` (inline, every tag `omitempty`) and
cleared so they leave the file on the next save. Two points of its own:

- **The categories are carried whole or not at all.** A `categories:` block that
  is present is an answer, even all off; mixing it with the old switches would
  make one category's state depend on which file wrote it. The historical "all
  switches off means never configured" reading survives **only** inside the
  migration — the one place that still sees the switches. A file with no
  `categories:` at all gets `DefaultScanCategories()`; an empty tool list counts
  as absent, because a zero `Config` saves `tools: []`.
- **One change of meaning, on purpose.** `enable_k8s_schema` ticks kubeconform
  *and* helm and kustomize, which used to serve whenever installed and are now
  required: what is ticked is what the dashboard reports missing.

A category's `tools` list is a slice, so a `Config` copied by value shares it:
`CategoryConfig.With` always builds a new one rather than editing in place.

**`app.gauge_glyph`** picks the fill of the containers load gauges and of the
netdiag waterfall: `shade` (the default — `░` for fill and track alike, told
apart by colour) or `block` (`█` over a `░` track). `block` is an opt-in because
`█` is East Asian ambiguous width and overflows the gauge columns on a terminal
that draws it double-width. It is not derived from the locale: the locale says
how the process *measures*, not what the terminal *draws* (§3.96). It is applied
through `theme.SetGaugeGlyph` at startup, on a context switch and on every save,
and anything but `block` stays on the safe glyph. The setting also drives the glyph of the `:agent-usage` Breakdown bar
(`theme.GaugeFill`, in the agents table column and the Breakdown tab); `block` has
the same ambiguous width there, with the same accepted risk as for the gauges: a
line can overflow by one cell on a terminal that draws it double-width.

**`app.motion`** picks whether activity indicators blink: `blink` (the default)
or `static`. The default is never written: an absent key stays absent on save.
Any other value is kept as read, behaves as `blink` and logs
`WARN [config] app.motion "<v>" is not blink or static, blinking`. A list or map
makes the load fail, like the other `app.*` keys. No environment variable
overrides it. Read through `AppConfig.EffectiveMotion` / `StaticMotion`.

In `:cfg` it is the **Motion** field of the `app` tab, group Appearance, a
`←→` cycle field (Rule 132) declared with `cycleWithDefault`. While the key is
absent the field reads `blink (default)` and nothing is written; the first
arrow starts from `blink`, so `→` gives `static`, and only cycling writes the
key. When the file holds an invalid value, the `app` tab's footer shows a
permanent warning-level `Status` (`Motion "<v>" is not blink or static — rows
keep blinking`), derived on every frame rather than set as a timed message. The
setting applies without a restart: the router calls `applyMotion` on
`ConfigSavedMsg` and on a context switch.

**`scan.pss_profile`** is the Pod Security profile a context aims at (DEVDESK-686):
`all`, `baseline` or `restricted`. It is `yaml:"pss_profile,omitempty"` and
**never defaulted in memory**: an absent key stays `""`, which reads as `all`
(`ScanConfig.EffectivePSSProfile`), so loading a context and saving it does not
add the key. That differs from `scan.base_image_track`, which is defaulted at load
and written back. `PSSProfileValid` accepts `""` and the three names. An invalid
value (hand-edited YAML) is kept as read, behaves as `all`, and logs
`WARN [config] scan.pss_profile "<v>" is not all, baseline or restricted, using all`
once per load, from `applyDefaults`. The file is not rewritten.

The names are stated in `internal/config/pss_profile.go` and again in
`internal/remediation`, which `config` cannot import;
`TestTheProfileNamesMatchTheConfig` keeps them in step. What the setting does is
described in `scanning.md`, "The Pod Security profile of a finding".

In `:cfg` it is the **Pod Security profile** field of the `scan` tab, group
Remediation, a `←→` cycle (Rule 132) declared with `cycleWithDefault`. While the
key is absent it reads `all (default)`; the first arrow starts from `all`, and
only cycling writes the key. A value outside the three is refused with the valid
ones listed. When the file holds one, the `scan` tab's footer shows a permanent
warning-level `Status` (the value, and that `all` is used and nothing is hidden).

**`app.container_engine`** names the engine every container, image, network and
volume comes from: `auto` (the default — docker if it is on PATH, else podman),
`docker`, `podman`, or an explicit path to a binary. It sits in `app` beside
`ide_command` and `terminal_command` — the section that already holds the
external tools this application drives — and deliberately **not** in `network:`,
which carries only netcheck's dials despite its old name (§3.67).

`auto` is written on load rather than left empty, for the same reason
`secret_backend` is: `engine.Resolve` reads `""` as auto either way, but the
configuration view cycles a closed set and a value outside it has nowhere to
start from. The names are stated in `internal/config` and in `internal/engine`,
held in step by `TestEveryConfiguredEngineHasAShape` — the same arrangement as
`forge.type`, one paragraph up. The field settles **on blur**, like the forge:
cycling auto → docker → podman would otherwise re-resolve three times.
`ConfigSavedMsg.EngineChanged` is what tells the router, because rebuilding the
views is not enough on its own — they would be rebuilt against the engine that
is still resolved.

**No secret goes in this file.** `GitLabConfig` has no `Token` and
`RegistryConfig` has no `Password`; both live in the host secret store (see
Credentials Management). Do not add a secret-bearing field back — the schema is
what makes the guarantee checkable.

### A host store that does not answer (DEVDESK-745)

Every call to the host store is bounded: `currentHost()` wraps the store in use in
`boundedHost` (`internal/credentials/hoststore_bound.go`), which runs the call in a
goroutine and gives up after `keyringTimeout`, 3 s, with `ErrKeyringUnresponsive`.
That covers the probe (`KeyringAvailable`, run by `Select` before the first frame)
and `KeyringStorage.Save`, `Load` and `Delete`.

**Why.** go-keyring has no deadline. Reading from a locked Secret Service collection
asks the daemon to unlock it, and go-keyring then waits for the unlock prompt's
`Completed` signal with no timeout (`secret_service.handlePrompt`). In an SSH session
nobody can answer the prompt, so the launch waited forever on a blank screen, the log
stopping after the theme line. Removing `DBUS_SESSION_BUS_ADDRESS` does not avoid it:
godbus falls back to `$XDG_RUNTIME_DIR/bus`, the same bus. Observed on the reporting
machine (2026-11-23), the binary run in a pty that answers terminal queries: without
the variable, the screen stayed empty for 20 s and the goroutine dump showed the same
`handlePrompt` wait as with it.

**Accepted cost (decided, DEVDESK-745).** The bound stays at 3 s for every machine. On a
desktop where the user would unlock the keyring by hand from the prompt, a launch
that reaches it locked takes the fallback (`unlock it and restart`) and waits 3 s
before the first frame. The `Selection` is not re-evaluated when the keyring is
unlocked later in the session: the context keeps its fallback until a context switch
or a restart.

**What follows a timeout.**

- `Select` skips the store as if it were absent: `auto` goes on to the git credential
  helper, then memory; a pinned `keyring` falls to memory, as for an unreachable store.
  `Detail` says the store did not answer, that it is probably locked, and the way out
  (unlock it and restart DevDesk, or set `app.secret_backend`); the auth view and
  `dk setup` show `Detail` as before. An absent store keeps its own message.
- The call is abandoned, not cancelled: the `HostStore` interface has no context and
  go-keyring cannot stop the wait, so its goroutine stays blocked until the store
  answers, possibly for the rest of the process (one goroutine, one D-Bus connection).
- Calls enter the store one at a time (`hostBox.inside`, a one-token channel taken
  before the goroutine starts): twenty callers arriving together at a store that has
  just gone silent leave one goroutine blocked, not twenty; the others give up at their
  own deadline without entering it.
- While such a call is pending, every new call fails at once with
  `ErrKeyringUnresponsive`: a context switch or a later `Load` does not wait another
  3 s, and no further goroutine is leaked. The flag is per store (`hostBox.stuck`); it
  clears when the stuck call returns (the user unlocked the store), and the store is
  then asked again. The `Selection` already resolved for the context is kept,
  per the one-`Selection`-per-context rule; the next `Select` (context switch, restart)
  finds the store again.
- `MigrateLegacySecrets` takes the `Selection` and migrates nothing to one that does not
  persist. Before, a `MemoryStorage.Save` always succeeded, so the plaintext token and
  registry password were deleted from the YAML and lived in memory only, with a note
  saying they had moved into the secret store. A silent store with no usable git helper
  made that path reachable (an absent store already did). The file is now left as it is
  and the note reads `The plaintext secrets stayed in the configuration file: no
  persistent secret store answered.`; a real move names the store it went to.
- A successful `Save` is not taken as proof: `git credential approve` exits 0 whatever
  its helpers keep. Each secret leaves the YAML only once `Load` gives the same value
  back; otherwise it stays, and the note says the store accepted it but did not read it
  back (never the value). Each secret is checked on its own, so a token that reads back
  goes and a registry password that does not stays. `Selection.Persists()` is also
  false for a selection with no storage (the zero value).
- An abandoned `Set` may still land later, once the store is unlocked. So a login whose
  save timed out does not say the token was not saved: `session.saveWarning` reads
  `The secret store did not answer; the token may still be saved once it is unlocked.`
  Any other failed save keeps `The token was not saved: …`.
- Logging out while a call is stuck: `Delete` is refused without being tried, and a
  `Set` still pending may write the token back after the logout. The session ends
  anyway, but the error is no longer dropped: `LogoutCompleteMsg.Err` reaches the auth
  view, which warns in the footer `Logged out, but the token may remain in <store>`. A
  refused `Delete` is not retried later (out of scope).
- A panic in the call (the DEVDESK-610 guard below) is carried back to the caller's
  goroutine, so it still fails the test that reached the store. Limit (tests only): a panic raised in a goroutine whose
  call was already abandoned at the deadline has no caller left to receive it and is
  lost. The guard panics at once, before any wait, so this needs a fake store that
  blocks and then panics.

The 3 s bound is a fixed value, not a setting; the tests of `internal/credentials`
shorten it (`shortKeyringTimeout`), except `TestSelectDoesNotWaitForAHostStoreThatNeverAnswers`,
which keeps it.

### git calls do not depend on the working directory (DEVDESK-745)

Every git call of `internal/credentials` (the helper detection and `git credential
fill/approve/reject`) runs through `gitCommand` (`internal/credentials/git_exec.go`):
from an empty directory created for the call and removed after it, with that
directory's parent appended to `GIT_CEILING_DIRECTORIES` (a value the user set is
kept). git then finds no repository and reads only the system, global, include and
environment scopes. Before, git ran in DevDesk's working directory: the `.git/config`
of the repository DevDesk was started in could add a `credential.helper`, a shell
command of that repository's author, which was selected as the backend, handed the
token on `approve`, and run on every `fill` at launch.
`TestGitIgnoresTheHelperOfTheWorkingDirectoryRepository` starts from such a repository.

### Which git helpers make git a backend (DEVDESK-745)

`gitHelperUsable` reads `git config --null --get-regexp '^credential\..*helper$'` (from
the neutral directory above), which lists the helpers scoped to a URL
(`[credential "https://…"] helper = …`) that `--get-all credential.helper` misses, and
follows `include.path` as git does (`TestGitHelperUsableFollowsIncludes`). git is
refused as a backend when any listed helper is `store` (plaintext) or `cache` (memory
with a timeout: it persists nothing), by name, path or shell command, ignoring case:
`approve` hands the secret to every helper, not only the one DevDesk names. Only an
exactly empty `helper =` resets the list; a value of spaces is a helper for git and
does not hide a `store` before it. A scoped helper is checked but never chosen, since
nothing says it applies to the forge's URL.

Not covered: a `store` reached by another mechanism than a `credential.helper` key (a
wrapper script whose name does not say `credential-store`, a helper that writes its
own plaintext file, `GIT_ASKPASS`, which only answers prompts and is disabled by
`GIT_TERMINAL_PROMPT=0`). The read-back of the migration (below) protects the YAML copy
in those cases; it does not stop a helper from also keeping a plaintext copy.

### Testing code that reaches the secret store

DEVDESK-610. `KeyringStorage` and `KeyringAvailable` call the host store through
`credentials.HostStore` (`internal/credentials/hoststore.go`); `goKeyring{}` is the
production implementation and the only caller of go-keyring.

**The trap.** Without isolation, a test that calls `credentials.Select`,
`KeyringAvailable` or a router that does reaches the machine's real keyring. On a
developer machine with a locked Secret Service, the call waits for an unlock prompt;
since DEVDESK-745 it gives up after 3 s, in every test that does it.

**Isolating.** Use `internal/testutil/fakekeyring`, an in-memory `HostStore`:

- `fakekeyring.Install()` in `TestMain` puts an empty store in use for the whole binary.
- `fakekeyring.Fresh(t)` gives one test its own empty store, restored at cleanup.
- `fakekeyring.FreshUnreachable(t, err)` makes every operation fail with `err` (locked
  keyring, no session bus); `KeyringAvailable` then reports `false`.
- `isolateHome(t)` in `internal/app` and `internal/ui/setup` already calls `Fresh`.

`credentials.SwapHostStoreForTest` is the seam behind these helpers. It panics outside a
test binary and on a nil store; call it from `_test.go` files and `fakekeyring` only.

**Guards.**

- *Runtime.* `refuseUnderTest` runs before every go-keyring call and before every
  `git credential` call. Under `testing.Testing()`, unless the `internal/credentials`
  `TestMain` stood the guard down, it panics with `DEVDESK-610` and the operation. It
  panics instead of returning an error because `Select` would fall back to memory and
  the test would pass silently.
- *Static.* `TestHostStoreGuard*` parses every `.go` file of the module: K1 forbids
  importing go-keyring outside `hoststore.go` and the `_test.go` files of
  `internal/credentials`; K2 forbids `SwapHostStoreForTest` in non-test files other than
  `fakekeyring` and `internal/credentials`; K3 forbids importing `fakekeyring` from
  non-test files.

**Limits.**

- The runtime guard sees only the paths a test executes.
- The static guard has no type information: a package imported under an alias that
  shadows another name, an import path built at run time, and a test that runs an
  external binary reaching the keyring are not detected.
- `gitHelperUsable` reads the git configuration and is not guarded.
- `Install` and `Fresh` change a process-wide store: a test that uses them must not call
  `t.Parallel`.

**`~/.devdesk/trust.yaml` is not a context file** (§3.82). It holds whom to
trust for image signatures — a fact about the world — so it is global, read by
`internal/trust`, and **strict** where `config.Load` is not: `KnownFields(true)`,
`version: 1`, and any error rejects the whole file (a typo would otherwise
weaken a rule in silence). It is edited by hand; the configuration view does not
touch it. Whether to verify at all is per context: `scan.image_verification`,
`on` or `off`, a string rather than a bool so a file written before the key
reads as `on` (D12) — and only `off` turns it off, so a typo verifies.

**`agent_usage.prices` — the price table of the cost estimate (DEVDESK-47).**
`internal/config/agent_usage.go`. DevDesk ships **no price**: the table is
written by hand in the context file, keyed by model name (kept exactly as
written; the matching rules are in `agent-usage.md`, "Pricing"). Each entry has
five rates, **all required**, in **USD per million tokens**:

```yaml
agent_usage:
  prices:
    claude-example-model:      # replace with the model id and the vendor's current prices
      input: 3
      output: 15
      cache_write_5m: 3.75
      cache_write_1h: 6
      cache_read: 0.3
```

The numbers above are an illustration of the shape, not prices to copy: take
the real ones from the vendor's price page. `0` is a valid explicit rate; a
missing rate is not 0, so an entry without all five keys is ignored (and logged),
which stops a forgotten `cache_read` from under-pricing silently.

- **`Rate` is tolerant.** A rate decodes through `config.Rate`, whose
  `UnmarshalYAML` never fails: an int or float is a number (`RateSet`), a
  missing key is `RateMissing`, anything else (`input: "5$"`, a list) is
  `RateInvalid`. A typo therefore costs **its own entry**, not the context:
  without this, a type error anywhere makes `LoadContext` fail with `failed to
  parse context`. An invalid value is kept as read and written back unchanged on
  save. `.nan`, `.inf` and negative numbers decode as values; rejecting them is
  `pricing.FromConfig`'s job, which logs `WARN [agentusageview]
  agent_usage.prices "<key>": <reason>` for each entry it leaves out.
- **No default value, no migration.** `applyDefaults` and `Default()` do not
  touch the section, and a context with no prices saves without an
  `agent_usage` key. What a new file does carry is a **commented example**
  (next bullet); no price is ever written. The format is user-written:
  renaming a key later needs a migration like `gitlab:` → `forge:`.
- **The commented example block (DEVDESK-314).** `internal/config/example_prices.go`.
  `ExamplePricesBlock` is the YAML above, every line a comment, appended after
  the marshalled config. Its first line, `examplePricesMarker`
  (`# Optional: price table for the :agent-usage cost estimate.`), is frozen:
  files on disk are recognized by a line equal to it (trailing spaces, tabs
  and `\r` ignored, leading ones not), so changing it would orphan every
  existing block.
  - *Who gets it.* A context file created by `CreateContext` (`:context new`),
    by `WriteNewContext` (`dk setup`, `internal/ui/setup/commands.go`), and
    `config.yaml` written on a first launch by `CreateDefaultContextIfMissing`
    (called from `ensureDefaultContext` in `main.go`). `CreateContext` and
    `CreateDefaultContextIfMissing` create the file exclusively (`O_EXCL`):
    an existing file is never overwritten, and of several concurrent first
    launches only one writes it. A failure on a first launch is logged
    (`WARN [config] could not create the default context file`) and does not
    stop the launch.
  - *Who never gets it.* A context that existed before this change, a file
    whose block was deleted by hand, and any context whose `prices` is not
    empty. `SaveContext` never adds a block to a file that lacks it.
  - *How a save treats it.* `SaveContext` reads the file again **only when
    `prices` is empty**: if it still holds the marker, the block is written
    back, otherwise not. A missing file gets no block. An unreadable file is
    saved without the block and logs `WARN [config] cannot read <path> to look
    for the price example, saving without it`. Once `prices` is non-empty the
    block is dropped, since the table itself is the example.
  - *Accepted race.* The read and the write are not atomic: a hand edit that
    removes the block between them can see it written back by that save. The
    window is small and the consequence is a comment, so it is accepted. The
    same class applies to a second process whose `Load` ran before the first
    created the file: it gets `created=false`, keeps `Default()`, and its first
    save rewrites `config.yaml` without the Docker Hub entry (next section).
  - `default` therefore exists on disk from the first launch and appears in
    `:context list` at once.
- **One block per context.** The section lives in the context file, so each
  context needs its own prices.
- **Not editable in `:cfg`.** There is no tab for it.
- **Read once.** The `:agent-usage` view reads the table when it is built:
  at startup, on a context switch and on `ConfigSavedMsg`. `ctrl+r` does not
  re-read it. **A save from `:cfg` rewrites the whole context file from the
  router's in-memory `Config`, so a hand edit made while DevDesk runs is
  overwritten by the next save** (comments in the file are lost on the first
  save too, except the example block above, which a save keeps while `prices`
  is empty). Edit the prices while DevDesk is closed, or restart afterwards.

Config is injected into views at creation. Use `config.Save()` to persist changes.

**`app.when_unfocused`** picks what DevDesk does while the terminal has no focus:
`pause` (the default) suspends the animation and the periodic reads of the
dashboard, `:oci`, `:containers`, the Ports tab and `:agent-usage`; `run`
suspends nothing. The default is never written. The case must be exact. Any
other value is kept as read, behaves as `pause` and logs
`WARN [config] app.when_unfocused "<v>" is not pause or run, pausing`. Read through
`AppConfig.EffectiveWhenUnfocused` / `RunsWhenUnfocused`; the values come from
`config.WhenUnfocusedModes()`.

In `:cfg` it is the **When unfocused** field of the `app` tab, group Appearance,
a `←→` cycle field declared with `cycleWithDefault` (absent reads
`pause (default)`). The setting applies without a restart: the router calls
`applyWhenUnfocused` on `ConfigSavedMsg` and on a context switch (see
`app-shell.md`, "Terminal focus").

### The default Docker Hub registry (DEVDESK-663)

`newContextConfig`, the config both context creators write (`CreateContext`,
`WriteNewContext`), carries one registry entry built by
`config.DefaultHubRegistry()` (`url: docker.io`, `slug: docker-io`,
`kind: registry`, `auth_mode: anonymous`, empty alias, no "built in" marker).
It is injected there and never in `Default()` or `applyDefaults`, which also
serve the load fallback: an entry the user deleted would come back. A context
that already exists is never given it.

On a first launch `main` loads the config before `config.yaml` exists, so the
session would run on `Default()` and its first `Save` would write the file
without the entry. `ensureDefaultContext` therefore reports whether it created
the file, and `sessionConfig` (`main.go`, called by `runTUI` and by the
first-launch tests) rereads the config through `reloadOnFirstLaunch` in that
case; a failed reread keeps the config the session had. `dk setup` builds its
config in `buildConfig` (`internal/ui/setup/answers.go`), from `Default()`, so
it sets the entry itself from the same constructor (DEVDESK-664): a wizard run
writes a new context with Docker Hub, and a confirmed overwrite replaces the
file with that new context, whatever registries the old one held
(`WriteNewContext` always replaces the file, DEVDESK-662). The one accepted
race is listed under the price example block above.

In short, for the registry list of a context: a **new** context (`:context
new`, a first launch, `dk setup`) contains the Docker Hub entry; an **existing**
context is not migrated and keeps the list it has; an entry the user removes
does not come back, since no load or save path adds it. The values of the entry
and the reasons for them are in `registries.md`, "The default Docker Hub
entry".

## Configuration view — `internal/ui/configuration`

Edits every **scalar** setting a context carries, in seven tabs (`app`, the
forge's own name, `scan`, `tools`, `network`, `mcp`, `status`). The second is titled after the
platform the context targets — `gitlab` or `github` — rather than after the
section key: `forge:` is what the file says, and no user calls it that. Lists stay where they are consulted: monitors keep
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
(`SubTitleStyle`, the same treatment the security form used). `group()` stamps the
heading onto a contiguous run rather than each field carrying its own, so a run
cannot be split by a typo and render its heading twice —
`TestEachTabRendersItsGroupHeadingsOnceInOrder` pins that.

**`scan` and `tools` are generated from `internal/scan`'s tables (§3.86),**
in `scan_fields.go`: a category or a tool added there appears here with no line
written for it.

- **`scan`** is what a scan looks for: one checkbox per category, and under it
  one per tool when there is a choice (`Depth` 1, and 2 for a renderer under
  kubeconform). A category with one tool has no sub-row; its tool is its dimmed
  `Note`. A tool row is a `get`/`set` pair over the category's list rather
  than a pointer — the one exception to the pointer rule, since membership has
  no address. A tool's `Args` is the other: a text field backed by a list
  (`field.list`), edited as one line (`scan.SplitArgs`/`JoinArgs`, quotes keep
  a value with spaces together) and refused by name when it carries a flag
  DevDesk sets itself.
- **A checkbox can be `locked`**, with a reason the footer says in place of the
  hint: a part of Trivy a server cannot do, a category that is off, a renderer
  whose kubeconform is not ticked. A locked box keeps its tick
  (`theme.RenderCheckboxLocked`), and the cursor stops on it. **A server no
  longer unticks anything**: the scanner skips what it cannot do
  (`Scanner.wants`), so switching the server off gives back what was there.
- **A `guard` refuses a value**: unticking the last tool of a category that is
  on (`reasonLastTool`) — the category's own box is the switch for "none".
- **A checkbox explains both of its states** (`hintOn`/`hintOff`, read by
  `Model.hint`): the footer says what the box does as it is, and changes as
  `space` is pressed. `TestEveryCheckboxExplainsBothStates` covers both tabs.
- **`tools`** is one group per tool, plus the platform tools (engine, git) as
  read-only rows. Each heading carries the tool's state — required and there,
  required and missing, or `not used` — from the router's detection
  (`shared.ScanToolsMsg`) and `scan.Required` over the categories being edited,
  so a box ticked on `scan` changes a heading at once. `ctrl+r` there asks the
  router to detect again; greyed on the other tabs (Rule 130).
- **Two columns** when the tab declares them and the width allows
  (`columnMinWidth` each), the groups split in order where the heights come
  closest: `↓` reads down the left column and on to the right, since the focus
  order is still the flat list. Each row is built by hand and fitted to its
  column (Rule 115).

**Every tab scrolls.** `View` lays the whole tab out (`layout`), and
`keepFocusVisible` moves `scroll` after each key and resize so the focused
field and its heading are on screen. The scan tab alone ran past seventy lines,
and its bottom was simply cut off.

**The header names the context, and the title does not.** A configuration
belongs to one, and editing `workspaces_dir` in the wrong context is otherwise
silent because the fields look identical in all of them — but that is what
`GetHeaderInfo` says here as it does in every other view, so a title repeating
it was the same fact twice on one screen. `GetTitle()` is `󰙨 Configuration`.

**The context's file path is a row in the form, not a header field.** It sits in
**Paths** under `workspaces_dir`, because that is what it belongs beside; the
header is for what changes as the user moves, and the file does not. It is the
one `kindStatic` field — shown, never written, and `Model.settleFocus` walks the
cursor past it in whichever direction it was already moving, since a focus
indicator on a row no key acts upon says the opposite of what is true. The
**Paths** group therefore reads three paths and then the checkbox that qualifies
them (`show_hidden_files`): a checkbox wedged between two value rows breaks the
column they share.

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

**"On blur" includes leaving the view, and that is what D62 was.** `commitFocused`
was reached from `switchTab` and `moveField` only, so `↑↓` and `tab` wrote and
nothing else did: a path typed and then abandoned with `ctrl+p` was never
applied. The router caches this view and `Init()` does nothing, so coming back
to `:cfg` showed the typed value while the file held the old one — the screen
confirmed the setting on every visit, which is what sent the search for the
defect into the scanner instead.

Two things close it:

- **`Leave()`**, the view's half of `app.LeavingView`. The router calls it
  before `switchView` and before `switchContext`, and a refusal **cancels the
  switch**. That is not a preference between the two options the backlog left
  open: the footer belongs to the view, so letting the user go and reporting the
  abandoned value there would post the message onto the screen that is leaving.
  The other answer cannot be *said*, and an unsayable answer is the same silence
  one layer down.
- **`esc` commits without moving.** It used to fall through to the input, which
  ignores it — the one gesture a user tries to close a field was the one that
  settled nothing. It re-binds the input afterwards, so `007` in an integer
  field becomes the `7` that was actually stored: telling a written value from a
  merely typed one is the other half of what made the defect durable.

`esc` is announced although Rule 138 calls the key obvious, because what it does
here is not, and it is greyed on the fields where it would do nothing —
`settlesOnBlur` is that question, and it is exactly the set `commitFocused` acts
on: text, integer, `Forge` and `Secret backend`. A checkbox and an ordinary
cycle field have already written by the time the cursor could leave.

Two settings are special-cased, matched by label:

- **Theme**, **Dark theme** and **Light theme** apply as they are cycled, not on
  blur — otherwise the user chooses blind. Theme offers `default`, `auto`, then
  the other themes (user files and embedded ones, `auto` once); Dark/Light theme
  are cycle fields on `app.theme_dark` / `app.theme_light` (defaults shown as
  `(default)`, never `auto`) and say in their hint that they apply when Theme is
  `auto`. Cycling Theme from `auto` writes the next named theme: `auto` is gone
  from the file.
- **Theme in use** is a read-only row (`kindStatic`, plain text, Rule 122) fed by
  `configuration.ThemeFacts{InUse, Reason}`, e.g. `github-light (auto: terminal is
  light)`; the router refreshes it with `ThemeFactsMsg`, which rebuilds the field
  table without moving the tab or the cursor and saves nothing (Rule 110).
- **Secret backend** is confirmed when focus *leaves* the field, not on every
  `←→`, and **nothing is migrated between backends**. §3.9 removed the option
  that wrote a token to two stores at once; copying one here would rebuild it.
  Declining restores the previous value.

**`forge.url` belongs to this view, not to the auth view.** Both used to write
it, so neither was authoritative and editing it in one left the other stale. The
auth view now shows it read-only, points at `:config`, and owns only the token
and the act of logging in — which is where the §3.9 line falls: this view's
contract is "everything here goes to `config.yaml`", and a token never does.

**`Forge` is the first field of the Connection group, above the URL.**
Everything below it reconfigures from it — the URL example, the visibility set,
two labels, the tab's own title and icon — so it has to be above them for the
user to watch that happen.

It is a cycle field (Rule 132) whose values are `config.ForgeTypes()`, and it is
**settled on blur**, not on every `←→`: the change closes the session, and
cycling through the list would close it once per keypress, including on the way
back to where it started. That is the secret backend's shape, minus the
question — changing the platform is neither forbidden nor confirmed, the user is
told what it did.

**Changing the URL *or* the platform closes the client-side session**
(`ForgeChanged` on the message) and says so. One flag for two settings, because
the consequence is one: the session was opened against something the config no
longer describes. Nothing is revoked and no token is deleted — the user changed
an address or a platform, not their credentials.

The URL field is recognised by **accessor identity**
(`f.str(cfg) == &cfg.Forge.URL`), not by label: two tabs could both hold a field
called "URL".

**The host is detected, and an explicit choice is never overwritten.**
`forge.DetectType` recognises `gitlab.com` and `github.com` and **nothing else**
— self-hosted is the case that matters and `git.acme.com` could be either, so an
unrecognised host changes nothing rather than guessing confidently. Detection
re-runs when the URL is committed, but only while `forgeTouched` is false: once
the user has moved the Forge field, typing a URL must not contradict them. That
flag is the whole difference between helpful and possessive. Probing
(`/api/v4/version` against `/api/v3/`) was rejected — a round trip per commit,
and it fails on instances that authenticate those endpoints.

**Two things follow a platform change, and both would be silent bugs without
it.** The field table is rebuilt, because it is computed once from the type and
the router *keeps* this view on a save — so nothing else would. And a
`default_visibility` the new platform does not have is coerced to its most
private: GitHub.com has no `internal`, and a context carrying it would hold a
value the server refuses while the cycle field opened on a value absent from its
own list.

`ConfigSavedMsg` goes to the router, which drops every view *except this one* so
they rebuild against the saved config — keeping the configuration view is what
stops a save throwing away the cursor after every keystroke. `BackendChanged`
is separate because it is the one change no view can rebuild itself into: the
router has to resolve a fresh `credentials.Selection`.

