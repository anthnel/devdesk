# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> [!IMPORTANT]
> **AI Collaboration Rule**:
> - **Planning**, **Brainstorming**, **Implementation** and **Bug fixes** are all
>   in scope. Planning is no longer delegated to Gemini (Antigravity).
> - Implementation plans go in `.claude/plans/` (see
>   [rules/planning.md](rules/planning.md)); design decisions that outlive a plan
>   are recorded in `docs/backlog.md`. Open work (stories, tasks, bugs) is tracked
>   in Plane, see the `## Plane` section of the root `CLAUDE.md`.
> - Always refer to [project-context.md](../project-context.md) for global project rules.
>   It sits in this repository, at the root. The absolute path this line used to
>   carry named a GitLab checkout that is no longer where DevDesk lives.

## Project Overview

DevDesk is a terminal-based TUI (Text User Interface) application built with Go and Bubble Tea framework. It provides DevSecOps functionality including:
- System status monitoring (HTTP/HTTPS, ICMP, DNS, SSL checks)
- GitLab integration (authentication, project explorer, multi-select clone)
- Security scanning (Trivy CVE/secret/license/misconfig + Gitleaks secrets)
- Container management with real-time metrics, on Docker or Podman (`app.container_engine`)
- OCI resource management (image scanning, container launching, network inspection)
- Network diagnostics (DNS, route, ICMP, TCP, TLS, HTTP; a Proxy row says whether a proxy variable applies, the checks themselves stay direct, DEVDESK-582) + real-time port monitoring (`K` stops a process: SIGTERM, up to 5 s, then a confirmed SIGKILL on Linux and macOS, at once on Windows; DevDesk itself, its direct parent, PID 1 and Windows PIDs 0 and 4 are refused; the identity is checked at each signal, DEVDESK-711) + this machine's interfaces + local port forwarding (unprivileged, loopback only) + a path trace under Diagnostics (Trace Path button, or `H` on the route row; no separate tab since DEVDESK-471) (native path trace, on native Linux, native Windows (IPv4 only, ICMP echoes through `iphlpapi`, DEVDESK-486) and WSL2: on macOS and WSL1 the button, `H`, `enter` and `ctrl+o` are greyed and the footer says why, DEVDESK-334; the column starts with this machine, its interface and its gateway; `ctrl+o` can start it from a running Docker Engine container instead (container and network rows, hops still traced from this machine); `H` on the Diagnostics route row opens the trace level on that host without starting it (greyed with the reason on an unsupported platform or a target the trace refuses; its interface and gateway rows reuse the Local route check's reading of the same address, read once, DEVDESK-473), and in the trace level `enter` starts or restarts it, `ctrl+o` picks the source, `esc` cancels then goes back where it came from; under WSL2 the source row says `WSL2 (VM), not the Windows host` with the networking mode, and in NAT mode the gateway and TTL 1 hop are tagged `Windows host (virtual switch)` only when proven, DEVDESK-333; the interface row says `no route` when the kernel proves there is none and `undetermined` when the routing table could not be read, a no-route error keeps the local rows, a trace that ends on the hop limit or the deadline folds its silent tail into one `silent` row and marks the `last to answer` hop, and the target refuses a `:port`, DEVDESK-335)
- Multi-context configuration management via YAML
- Local workspaces management with git metadata
- Claude Code session list (`:agent-usage` / `:au`): sessions found on this machine, by project and date, and per session the agent tree (orchestrator and sub-agents: types, models, tokens, share of the total; sortable by Total; also per agent a state (running / idle / done / unknown, idle after 5 min without a transcript line) shown as a status dot in the first column (the `running` dot blinks, DEVDESK-151) and as text, tool calls and duration; and an indicative cost per agent, `Est. cost` (orange from 75 % and red from 90 % of the session's estimated cost, when at least two agents are priced), from prices you write in the context YAML (`agent_usage.prices`, none shipped), with the session's `Billed` cost and `Unattributed` in the header, DEVDESK-47; a `Breakdown` colour bar per agent and, with `tab` inside a session, a Breakdown tab splitting the whole session's tokens by category, model and agent, as a percentage of tokens, DEVDESK-98). An open session updates live: read again every 2 s while active, only what was appended; paused after 5 min with no write and no unfinished sub-agent, when its transcript is gone, or while the terminal is not focused (header `Updates › paused`, then `paused (terminal not focused)`; `app.when_unfocused: run` keeps it live); `ctrl+r` reads it again in full. The sessions list itself is not live.

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
mise run ci           # the five checks in order (fmt-check, vet, lint, build, test-race), stops at the first failure; unlike check it rewrites no file and also runs build and test-race. `test-race` is the only test run (`-race` covers `go test`); `mise run test` stays available alone for the fast local loop. CI runs fmt-check, vet, lint and build in the `Lint and build` job and `test-race` in the `Test` job, on every pull request, docs-only ones included (the docs-only shortcut applies to `Lint and build` only); the whole CI targets under 10 minutes, so run this locally before a PR to catch a red check early

# Installation
mise run install      # Install to $GOPATH/bin
```

`test-race` needs cgo and therefore a C compiler (`gcc`/`clang`) on `PATH`.

## Git, worktrees, pull requests — absolute rules

- Work happens in a worktree under `.worktrees/<branch>`, created from `origin/main`; the main checkout stays clean, on `main`.
- Never push directly to `main`: the Entire mirror rejects it, and so does the `main-protection` ruleset. Everything goes through a PR, merged once `Lint and build` and `Test` are green.
- Always use `gh -R anthnel/devdesk` (`origin` is `entire://`). There is one remote, `origin`; do not add a second one pointing at GitHub.
- A red `Entire Gates` check caused by Approvals alone does not block a merge (Findings, Checks, Up to date must stay green).
- In a sandbox, only commit; the host pushes, opens the PR and merges. Whoever creates a worktree removes it (never `git worktree prune` from the other side). The sandbox creates its own worktree; the host never creates one for it.
- Before creating a worktree or branch, pushing, opening/merging a PR or removing a worktree, load the `git-worktree-pr` skill.

## Releases

Only merging the release-please PR publishes a release; never merge it without the user's explicit agreement. After every merge on main, the orchestrating session loads the `release-flow` skill and checks the triggers.

## Architecture

The deep notes live in `docs/architecture/`, one file per area, and they are
**not** `@`-imported: an `@` would inline all of it into every session, which is
the thing this split exists to avoid. Read the file that covers what you are
touching, and update it in the same commit as the code.

| File | Covers |
|---|---|
| [`app-shell.md`](../docs/architecture/app-shell.md) | the router and its views, `ctrl+p` and the command parser, the keyboard vocabulary (`internal/ui/keymap`), greyed shortcuts, `shared.State`, cross-view messages, `netdiag.BackgroundMsg` (netdiag's own work, routed on or off screen), the jobs registry (`internal/jobs`), the one animation tick (blink phase, `app.motion`), the failure memory (`jobs.FailureKey`, `ChangedMsg.Failures`), the activity-column guard (`activity_guard_test.go`), the tab progress bar (OSC 9;4, `terminalProgress`), the window title (`windowTitle`, restored by `internal/termtitle`), the terminal hand-off to child processes (the exec wrapper `internal/ui/teaexec`, use it instead of `tea.ExecProcess`, D121) and the `:jobs` view |
| [`configuration.md`](../docs/architecture/configuration.md) | the config schema, the contexts, the migrations (`gitlab:` → `forge:`, `docker:` → `network:`), `agent_usage.prices`, and the configuration view |
| [`forge.md`](../docs/architecture/forge.md) | `internal/forge` and its two backends, `forge.Vocabulary`, the explorer's clone pipeline, and how the explorer reads the forge index (`internal/forgeindex`) and its `g` |
| [`workspaces.md`](../docs/architecture/workspaces.md) | the `ws` file icons (`internal/ui/fileicon`) and the sync (`F`) |
| [`filebrowser.md`](../docs/architecture/filebrowser.md) | `:files` (`internal/ui/filebrowser`, `internal/fsbrowse`), the path picker a form's path field opens with enter, and `theme.ShortPath` |
| [`viewer.md`](../docs/architecture/viewer.md) | `internal/viewer` + `internal/ui/viewer` — kinds, displays, colouring, search, follow |
| [`scanning.md`](../docs/architecture/scanning.md) | Trivy, Gitleaks, plumber, `scan.Categorize`, the `:sec` inventory, the two scan caches, image signatures (`internal/trust`, the cosign verifier) |
| [`registries.md`](../docs/architecture/registries.md) | the registry/group model and the discovered-members cache |
| [`network.md`](../docs/architecture/network.md) | `internal/docker` and `internal/oci` (typed `StatusError`, `...Context` variants), `internal/netproxy` (the proxy entry point every HTTP and TLS connection goes through, and its guard test, DEVDESK-581), the proxy passed to scanner containers (`netproxy.Forward`, `scan.containerRun.cmd`, the `TestContainerGuard*` guard, DEVDESK-583), `internal/imagesearch` and the registry browser's image search level, image updates (`internal/imageupdate`, the Update column), verified pulls (`internal/imagepull`), the recorded engine fixtures and their replay (`internal/enginefixture`, `fakecli.Fixtures`, DEVDESK-805), the engine contract tests (`requireEngine`, `internal/testutil/enginecontract`, guard G5, `mise run engine-contract`, DEVDESK-806), the netdiag view, `internal/ports` (the signal seam `realOS.kill`, the process identity check, DEVDESK-709, DEVDESK-711), `internal/netiface`, `internal/nettrace` (the path trace: unprivileged UDP + `IP_RECVERR` on Linux, ICMP echo through `iphlpapi` on native Windows) |
| [`mcp.md`](../docs/architecture/mcp.md) | the MCP server the TUI serves over HTTP |
| [`templates.md`](../docs/architecture/templates.md) | `internal/template` + `internal/ui/templates` — the repository-template catalog, its three sources, how files are fetched, and the `:templates` view |
| [`ui-components.md`](../docs/architecture/ui-components.md) | `components.FooterMessage`, `internal/ui/datatable`, and the activity indicator column (`internal/ui/indicator`, `datatable.IndicatorColumn`, the row sentences `components.DescribeRow`) |
| [`agent-usage.md`](../docs/architecture/agent-usage.md) | `internal/agentusage` — per-agent token counts from Claude Code transcripts: format, aggregation rules, tolerance, privacy (`cwd` amendment), caps, ccusage cross-check; `ListSessions`; incremental reading (`ReadDelta`, `Aggregate`, `Activity.Settled`); tool calls and time span per agent (DEVDESK-46); per-model tokens, `Billed`, and the cost estimate (`internal/agentusage/pricing`, prices from `agent_usage.prices`, DEVDESK-47); the `:agent-usage` view and its agents level and State/Tools/Duration/Est. cost columns; the Breakdown column and tab, `Usage.Categories`/`ModelTotals` (DEVDESK-98) (`internal/ui/agentusageview`) |
| [`nexus-bench.md`](../docs/architecture/nexus-bench.md) | `internal/nexusbench` — the disposable Nexus Community bench on the k0s cluster (DEVDESK-504, NX-01; seed, DEVDESK-505; `cut-upstream`, `restore-upstream` and `reset`, DEVDESK-534; recorded fixtures `internal/nexusfixture` (campaigns `catalog`, `cachedonly`, `access`) and `nexus-bench record`, DEVDESK-511, DEVDESK-580): decisions, the `NEXUS_BENCH_*` parameters and their validation, manifests, preflight, deployment, provisioning, destruction, the `nexus-bench` command (`cmd/nexus-bench`) and its `mise run nexus-bench:*` tasks, the upstream cut, restore and reset, the recorded fixtures (format, redaction, the `fakenexus` replay server, `record`, the stale test), what is still to come (NX-03 onward); a development tool outside `mise run ci` |

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

### Component CRUD Operations

Status view supports adding/editing/deleting monitors:
- `internal/ui/status/components/component_form.go` - Form component
- `internal/ui/status/components/confirm_modal.go` - Confirmation dialog
- Changes persist to `~/.devdesk/config.yaml` via `config.Save()`

State flags in Model: `creating`, `editing`, `confirming`, `selectedIdx`

### Credentials Management

No secret DevDesk holds is written to a file DevDesk owns. `internal/credentials`
implements the `Storage` interface three ways and `Select()` picks exactly one —
writing to several at once is what let the old "secure" option store a token in
the credential manager *and* in plaintext:

| Storage | Backend | Notes |
|---------|---------|-------|
| `KeyringStorage` | Windows Credential Manager / macOS Keychain / Secret Service | via `zalando/go-keyring`, no cgo. The default. |
| `GitCredentialStorage` | git's configured credential helper | Only when no configured helper is `store` (plaintext) or `cache` (memory with a timeout), by name, path or shell command: `approve` reaches every helper. Context-aware. |
| `MemoryStorage` | this process | Last resort, and deliberately worse: the auth view says nothing was saved. |

`Select(context, preference)` returns a `Selection{Storage, Backend, Detail}`.
The order is keyring → git credential → memory; `app.secret_backend` in the
context config pins the head of it (`auto`, `keyring`, `git-credential`). A
pinned backend that is unreachable falls through to memory rather than silently
to the other one. Every host store operation (the probe, `Save`, `Load`,
`Delete`) is bounded to 3 s: a store that does not answer, such as a locked
Secret Service waiting on an unlock prompt over SSH, is skipped like an absent
one, `Detail` says so, and it is not asked again while that call is stuck
(`internal/credentials/hoststore_bound.go`, DEVDESK-745).

The router resolves one `Selection` per context and keeps it in
`shared.State.Secrets`. It must be reused for the lifetime of the context — a
second `Select()` call yields a fresh `MemoryStorage` that cannot see what the
first one holds.

`MigrateLegacySecrets` runs at startup and on every context switch: a
`gitlab.token` or `registry.password` left in a context file by an older build
is moved into the store and deleted from the YAML, with the result reported in
the auth view. It takes the `Selection` and moves nothing when it does not
persist (memory), and removes a secret from the YAML only once it reads back
from the store; otherwise it stays and the note says so (DEVDESK-745).

## Testing

Test files follow Go conventions (`*_test.go`):
- `internal/command/parser_test.go` - Command parser tests
- `internal/app/app_test.go` - App router tests

Use table-driven tests where appropriate.

**`go test` does not detect races or explore fuzz targets.** `mise run test` runs
only the fuzz seeds, so `internal/agentusage` concurrency tests
(`TestConcurrent*`) need `mise run test-race`. To explore `FuzzIncrementalMatchesFull`
run `go test -fuzz=FuzzIncrementalMatchesFull -fuzztime=60s ./internal/agentusage`.

**HOME is shared across a test process.** `TestMain` in `internal/app` sets
`HOME` once for the whole process, so under `go test -count=N` a test finds
the files its own previous run wrote (context config, forge index cache), and
it fails from `-count=2` while `-count=1` passes (DEVDESK-36). A test that
writes under HOME isolates it with `t.Setenv("HOME", t.TempDir())` and
`USERPROFILE` too; in `internal/app`, call `isolateHome(t)` from
`harness_test.go` (already done by `routerAt`). `internal/ui/setup` is fixed
(DEVDESK-38): its `isolateHome(t)` helper in `model_test.go` is called by the
tests that write under HOME. Other packages whose TestMain does
`os.Setenv("HOME", …)` may have the same default. To find them:
`grep -rn 'os.Setenv("HOME"' --include=*_test.go`, then `go test -count=2` on each package.

**Never get a "free" port by `net.Listen("tcp", "127.0.0.1:0")`, reading the
port, then `Close()`, and letting the code under test bind it later.** Between
the close and the bind, any `bind(0)` can be handed that very port: an
`httptest.NewServer` in the same test, another test, another process on the
runner. The symptom is an intermittent `address already in use`, mostly in CI
(Linux gave the just-closed port back to about 1 `bind(0)` in 7,000; DEVDESK-35,
D85 in `docs/backlog.md`).

- If the code under test takes a port, call `loopback.ReservePort(t)`
  (`internal/testutil/loopback`). On Linux it holds a socket bound with
  `SO_REUSEADDR`, never listening, until `t.Cleanup`: `bind(0)` and `connect()`
  cannot take the port, but `net.Listen` can.
- Otherwise let the server bind `:0` and read the real port, or keep the
  listener open and hand it to the code under test.
- Off Linux, `ReservePort` keeps the old behavior and the window stays open
  (on Windows Go does not set `SO_REUSEADDR`). That is known, not a local bug.
- `internal/testutil/loopback` is imported from `_test.go` files only; nothing
  enforces it yet.

**Nexus tests replay recorded fixtures (DEVDESK-511, DEVDESK-580).** Three campaigns
in `internal/nexusfixture/fixtures/` (`catalog`, `cachedonly`, `access`) replay through
`fakenexus`: `mise run test` needs no bench, cluster or secret. A `record: pending` file is never
versioned (`TestNoPendingFixtureIsVersioned`); `DEVDESK_FIXTURE_RECORDING=1` only in a local recording session.
`nexus-bench record` without `--state` applies no state filter and can overwrite an `upstream-cut` fixture.
Bench writes go only through `nexus-bench:*` tasks and the guarded `DEVDESK_NEXUS_BENCH_SCENARIOS=1` test.
Dated measures live in `docs/measurements/`; a measure contradicting a criterion or the code is frozen and noted, never fixed in the same PR.

**Scanner containers and the proxy (DEVDESK-583, D164).** Every `run --rm` a scanner
issues goes through `containerRun{…}.cmd()` (`internal/scan/container_run.go`): the proxy
variables travel as `-e NAME` with the value in the child environment, never in argv. The
guard `TestContainerGuard*` (`internal/netproxy/container_guard_test.go`) fails on any other
`"run"` literal; only the two permanent exceptions in that file are allowed. The tests of
`internal/scan`, `internal/app` and `internal/ui/security` neutralise the seams
`proxyForward` / `containerForward` in their `TestMain`, so a developer's `HTTPS_PROXY` does not
change an exact argv. The `TestMain` of `internal/app`
also replaces `detectTools` (`internal/app/scan_tools.go`, production value `scan.Detect`) with an
empty report, so `App.Init` never runs the `trivy`, `gitleaks` and `plumber` of the machine
(DEVDESK-645); `TestInitDoesNotRunTheScannersOfTheHost` and `TestDetectToolsIsScanDetectInProduction`
guard it.
A forge token follows the same rule (DEVDESK-945, S9): plumber's `GH_TOKEN` / `GITLAB_TOKEN` is `-e NAME` in argv and
`NAME=value` in `containerRun.Env`, never `-e NAME=value`; the static guard `TestNoSecretIsPutInAnArgv`
(`internal/engineguard/secret_argv_guard_test.go`, `docs/architecture/network.md`, "Guard S9") fails on a word list that puts a
dynamic value after `-e`, `--password`, `--token`, `-H Authorization...`, `--creds`, `--build-arg K=`, `-u user:` or `login -p`, with no exception today.
The renderers (helm, kustomize) run with the network cut (DEVDESK-767, D180):
`containerRun.Offline` (`--network none`, no proxy) from an image, `offlineEnviron` from a binary,
except kustomize with `scan.tools.kustomize.allow_remote_bases: true`; a refused remote base is the
K8S-RENDER finding `Overlay does not build: remote base blocked` (`remoteBlocked`). A native kustomize is first gated (`internal/scan/kustomize_gate.go`, DEVDESK-771): only local-path references pass, else that finding or `Overlay not built: its references could not be checked`, and kustomize is not started.

**Tests and the host secret store (DEVDESK-610).** By default `credentials.Select` and
`KeyringAvailable` reach the real keyring of the machine (Secret Service over D-Bus on
Linux); a locked keyring blocks the whole suite until the timeout. A test binary that can
reach them isolates the store: `fakekeyring.Install()` in `TestMain`, then
`fakekeyring.Fresh(t)` (or `FreshUnreachable(t, err)` for a locked keyring) per test, from
`internal/testutil/fakekeyring`. `isolateHome(t)` already calls `Fresh` in `internal/app` and
`internal/ui/setup`. Two guards enforce it: at run time, any keyring or `git credential` call
in a test binary without the fake panics with `DEVDESK-610` and the operation; statically,
`TestHostStoreGuard*` (`internal/credentials/hoststore_guard_test.go`) fails on K1 (go-keyring
imported outside `internal/credentials`), K2 (`SwapHostStoreForTest` in non-test code) and K3
(`fakekeyring` imported by non-test code). The seam is `credentials.HostStore` and
`SwapHostStoreForTest`, reserved for tests (it panics outside a test binary). Limits: the
runtime guard only sees the paths a test runs; the static one misses an aliased package
name, a dynamic import and an external binary; `gitHelperUsable` reads the git config
unguarded; `Install` and `Fresh` are global, so no `t.Parallel`. See
`docs/architecture/configuration.md`, "Testing code that reaches the secret store".

**Tests and real signals (DEVDESK-708, DEVDESK-711).** A test must never signal a real process. In
`internal/ports` the only place that does is `realOS.kill(pid, want, sig)` (`internal/ports/procos.go`),
reached through the `procOS` seam, which also holds `identity` and `probe` (`Probe` reads the state, it
sends no signal). `selfPID`, `parentPID`, `goos` and `now` are seams too. A test substitutes them
(`useFakeOS(t, self, parent, goos)` in `seams_test.go`); in `internal/ui/netdiag` it replaces `processOps`
on the model, so `ports.Kill`, `ports.Terminate` and `ports.Probe` are never reached.
`IdentityForTest` and `DeferredIdentityForTest` panic outside a test binary.
Two guards enforce it: at run time, a real signal in a test binary panics with `DEVDESK-708`
unless the test called `allowRealSignals(t)`, which only a test that kills a child it
started itself may do (`TestKillEndsARealChildProcess`, `TestTerminateEndsARealChildProcess` in
`stop_real_test.go`); statically, `TestOnlyRealOSSendsASignal`
fails on a signalling call (`os.FindProcess`, `syscall.Kill`, `unix.Kill`,
`windows.TerminateProcess`, `.Kill(` or `.Signal(`) outside `(realOS) kill`. Limits: the static
guard misses an aliased import, another package and an external binary; the seams are global, so
no `t.Parallel`. See `docs/architecture/network.md`, "Seams and the signal guard".

**Tests and the fake CLI (OC-02, DEVDESK-797).** A test that needs `docker`, `podman`, `kind`, `kubectl`
or a scanner on `PATH` installs `fakecli.Install(t, scenario, tools...)` from `internal/testutil/fakecli`;
the test binary answers as the tool (`os.Exit(fakecli.Main(m))` in `TestMain`, or `fakecli.AsTool()` first
when `TestMain` prepares state). Strict: an undeclared argv fails the test with the argv received;
`(*Fake).Invocations()` is the journal. Do not write another fake binary: `TestFakeCLIGuardWalk` fails on a
`_test.go` that sets `PATH` and copies the test binary or writes a `#!` script (F2; the exceptions are the
files of `legacyFakes`, which only shrinks, and a fake reached through `engine.Shape.Binary` or a configured
path rather than `PATH` is not seen by the guard) and on a non-test file importing `fakecli` (F1).
`Install` fails the test if the package has no `TestMain` calling `fakecli.Main` or `fakecli.AsTool`. `Install` uses
`t.Setenv`, so no `t.Parallel`. See `docs/architecture/network.md`, "Tests and the fake CLI".

**Tests and the real engine (DEVDESK-798, OC-03a; DEVDESK-799, OC-03b).** In a test binary, `cliRunner.LookPath`, `Run` and
`Build` call `engineguard.Check`: an engine binary resolved outside a directory `fakecli` declared
(`fakecli.AllowFakeCLIDir`, called by `Install` and `InstallAt`) **panics** with a message naming DEVDESK-799 and
`docs/architecture/network.md`, as the DEVDESK-610 and DEVDESK-708 guards do. A test installs `fakecli.Install` (or declares the
directory of a script engine with `fakecli.AllowFakeCLIDir`), or uses a seam of its package. The survey of OC-03a is opt-in: only
with `DEVDESK_ENGINE_SURVEY` set is a reach recorded instead of refused, which `mise run engine-survey` does to list every
offender at once (heavy: the whole suite, outside `ci`, `check` and `test`); `engineguard.SurveyMode(t)` is that mode for the
tests of the guard itself. A test that reaches a real engine on purpose calls `engineguard.AllowRealEngine(t)`,
which fails unless `DEVDESK_ENGINE_CONTRACT=1` and `CI` is empty. A panic in a goroutine ends the test binary. No `t.Parallel`. See
`docs/architecture/network.md`, "Tests and the real engine".

**Tests and engine launches (DEVDESK-802, OC-05, G2).** Guard G2 is static: `TestOnlyTheRunnersStartTheEngine`
(`internal/engineguard/launch_guard_test.go`) reads every non-test `.go` file of the module with `go/parser` and fails,
with file and line, on a site outside the runners: an `exec.Command`/`CommandContext` whose binary folds to `docker`,
`podman`, `kind` or `kubectl` (literal, file-level constant, path, `engine.Docker`, `config.EnginePodman`), or a read of
`engine.Shape.Binary`. The runners are `internal/docker/exec.go`, `internal/scan/container_run.go` and
`internal/kind/runner.go` (optional until the kind stories create it; its rules are already on). Three named exceptions in
`launchExceptions`: `imageIsLocal` (`internal/scan/scanner.go`) and `Detect` (`internal/scan/toolbox.go`), both
**temporary** (they start or probe the engine from `internal/scan`, to move behind the runner), and
`(*App).resolveContainerEngine` (`internal/app/configuration.go`), **permanent** (reads `Shape.Binary` for a log line only,
nothing is started). The log pager (`V`) is not an exception since OC-84: it goes through `docker.LogsCmd`. To add one: key =
file + function (not a line), `Sites` = the exact number of sites in it, a reason a reviewer can disagree with; the list is a
debt register and does not grow for convenience. To remove one: delete the entry; an exception whose code is gone, whose
count differs or which sits in a runner fails the test. Limits: a binary name built at run time (`exec.Command(tc.Name, ...)`),
a constant from another file, a dot import of `engine`, a function value, `syscall.Exec`, a shell line (`sh -c "docker ..."`)
and an external script are not seen; a runner is exempt as a whole; `_test.go` files are not scanned (they are G1's, at run
time). See `docs/architecture/network.md`, "Tests and the real engine".

**Tests and destructive verbs (DEVDESK-803, OC-06, G3).** Guard G3 is static and run-time.
`TestDestructiveVerbsStayInTheirPackage` (`internal/engineguard/destructive_guard_test.go`) fails, with file and line,
on a literal or a call that builds the argv of a destructive subcommand (`rm`, `stop`, `kill`, `rmi`, `prune`,
`volume rm`, `network rm`, `system prune`, kind `delete cluster`...) outside `internal/docker` and `internal/kind`;
`TestNoExportedDockerFunctionTakesAnArgv` keeps any exported `internal/docker` function from taking an argv. Other
packages call `docker.RemoveContainer(id)` and its siblings, which validate the identifier first (`IsContainerID`,
`IsImageID`, `IsVolumeName`, `IsNetworkRef`, each with its `ErrInvalid...` sentinel) and build nothing for a malformed
one (`internal/docker/destructive_ids_test.go`). A test passing an ID to these functions uses a valid one (12 to 64
lowercase hex for a container or an image; for a network that or a name; for a volume a name; not `abc`). The vocabulary includes `compose down`, `network disconnect`, `image untag`, and the verb is read at any position of the
constant words. `internal/kind` is exempt until it exists, then needs its own `TestEveryDestructiveFunctionIsCovered`
(`TestAnExemptHomeHasItsRunTimeInvariant`). Limits: an argv built word by word, a verb from a variable
and a shell line are not seen; `ShellCmd` and `DetectShell` validate the ID since OC-18. See `docs/architecture/network.md`, "Guard G3".

**Tests and interpreters (DEVDESK-816, OC-18, I1).** The shell of `T` is `<engine> exec -it <id> <shell>` as an argv array, never through an interpreter, and `DetectShell(id) (string, error)` / `ShellCmd(id, shell) (*exec.Cmd, error)` refuse a malformed ID with `ErrInvalidContainerID` before building or running anything; the containers view shows `container ID refused` at level Error and starts nothing. Guard I1 is static: `TestNoInterpreterIsGivenAConstructedCommandLine` (`internal/engineguard/interpreter_guard_test.go`) fails, with file and line, on a word list that holds a shell or Windows command interpreter (`sh`, `bash`, `cmd`, `powershell`..., any case), its flag (`-c`, `/c`, `-Command`, `-enc`...) and then a word that is not a constant. `ShellCmd` accepts only `/bin/bash` and `/bin/sh` (`ErrInvalidShell`). Register `interpreterExceptions` (key = file + function, `Sites` exact, a reason): `internal/testutil/stepshell` `Run` (permanent, test helper) and `internal/docker/launch.go` `VerifyEntrypoint` (**temporary**, the launch form's entrypoint spliced into `sh -c "command -v "+entrypoint` in a throwaway container; DEVDESK-947). Limits: a constant script, a binary or a flag in a variable, a list built word by word and a script file are not seen; only the word after the flag is judged (`cmd /c start <url>`, DEVDESK-940, is outside); `python -c`, `node -e`, `wsl -e`, `ssh` and the like are not interpreters here. See `docs/architecture/network.md`, "Guard I1".

**Tests and image pulls (DEVDESK-808, OC-10, G8).** Guard G8 is static and run-time: every pull goes through
`imagepull.Pull`, so no path skips the signature check or `trust.yaml` (S6). `TestEveryPullGoesThroughImagepull`
(`internal/engineguard/pull_guard_test.go`) fails, with file and line, on an argv that spells a pull outside
`docker.PullImageContext` (a `[]string` literal, `exec.Command(Context)` or an argv helper with the word `pull`, or
`--pull`/`--pull=<value>` other than `never` and `false`), and on a name of `docker.PullImage` or
`docker.PullImageContext` outside `internal/imagepull` (that package is the home of the reference rule only, not
exempt from the argv one). Three permanent exceptions in `pullExceptions`: `PullImageContext` (the one place that
spells the verb), `PullImage` (its context-free wrapper, no production caller) and the `pullImageContext` test seam of
`internal/ui/oci_resources/image_update.go` (feeds `imagepull.Deps` only). To add one: key = file + function, `Sites` =
the exact count, a reason a reviewer can disagree with; to remove one: delete the entry (an entry whose code is gone,
whose count differs or without a reason fails; `pullMustSee` fails when the detector stops seeing the module's two
known sites). `TestNoPullBypassesTheSignatureCheck` (`internal/ui/oci_resources/pull_verify_test.go`) is a textual test
that covers part of G8 on its own, for that one package. The paths to `imagepull.Pull` are `pullOneImageCmd` (registry
browser and MCP `image_pull_start`, via `ImagePullRequestedMsg`) and `G` (`updateImageCmd`). A launch never pulls: `run --pull=never` (`noImplicitPull`, `internal/docker/launch.go`) is held by
`internal/docker/launch_pull_test.go`, exact argv on both engines. Limits: the scanners' own `run --rm`
(`internal/scan/container_run.go`) pulls their tool images unverified, `scan.image_verification` off makes
`imagepull.Pull` pull the tag as before, kind's pulls (OC-71) and the `skopeo copy` of `internal/nexusbench` (a development tool) are out of scope,
a `[]string{"git", "pull"}` or the two-word `"--pull", "never"` is reported (words, not programs), and a verb from a variable, an argv built word by word, a shell line and a function value are
not seen. See `docs/architecture/network.md`, "Guard G8".

**Tests and launch environment values (DEVDESK-812, OC-14, S9).** A value typed in the launch form never enters argv:
`buildLaunchArgs` emits `-e NAME`, and `LaunchContainer` / `BuildLaunchCmd` give the child `os.Environ()` plus the typed
entries (`dockerCmd.Env`, `cmd.Env`). A test of a launch asserts the argv with `fakecli` (`Invocation.Args`) **and** the
child's environment (`Invocation.Env`), on both engines, never `-e KEY=VALUE`; an entry with no `=` or a name `docker.ValidEnvName`
refuses makes both functions return `docker.ErrInvalidLaunchEnv` and start nothing (`internal/docker/launch_env_test.go`; the
form half is `internal/ui/oci_resources/launch_env_test.go`). A name the engine client or the loader reads (`DOCKER_HOST`, `HOME`, `PATH`,
`LD_*`, `*_PROXY`, ...; `docker.ReservedEnvName`, case-insensitive, exact match plus the `LD_`/`DYLD_` prefixes and the `_PROXY` suffix) is
refused the same way with `docker.ErrReservedLaunchEnv` (OC-15, DEVDESK-813; `launch_reserved_test.go` in both packages): a test that types a
variable uses an ordinary name. A launch log line takes `docker.LaunchLogText(err)`, never the engine's
output. The preview and `ctrl+y` show `-e NAME` and never a value (OC-16, DEVDESK-814; an entry that is not `NAME=VALUE` shows as `-e <invalid>`; the clipboard seam is `writeClipboard`, `launch_preview_names_test.go`); the container inspect hides the values of the sensitive `Config.Env` entries behind `********` until `m` reveals them, and `Y`, `/` and the display only see what is shown, OC-13, DEVDESK-811, `internal/ui/containers/inspect_mask.go`, `viewer.Maskable`. The launch cache `launch-options.json` keeps the names of the variables and never a value (OC-17, DEVDESK-815; `LaunchOptionsEntry.EnvNames`, `cache.EnvNames`; an older file is migrated on the read and rewritten 0600; the form comes back with `NAME=` and a hint; `launch_options_names_test.go`, `launch_cache_names_test.go`). See
`docs/architecture/network.md`, "A launch passes environment values to the child".

**Tests and sensitive mounts (DEVDESK-818, OC-20, S4).** `docker.SensitiveMounts(opts)` (`internal/docker/launch_mounts.go`) is a reader that flags a bind mount equal to or under `/`, `/etc`, `$HOME` (itself), `~/.ssh`, `~/.devdesk` or an engine socket (Docker and Podman at their usual places, `$XDG_RUNTIME_DIR`, the `unix://` path of `DOCKER_HOST`/`CONTAINER_HOST`), on `Volumes` (`-v`) and the new `Mounts` (`--mount VALUE`, emitted after the `-v` pairs), after expansion of `~`, resolution of links (a `..` after a link applies to the target) and normalization; one fixed reason per category, nil on Windows. The launcher does not call it: the launch form does, at the submit, and opens a `ChoiceModal` over the form (Cancel by default, `Mount anyway`, or `Mount the engine socket` with an Error footer derived on every render when a socket is among the mounts; no letter answers). The Volumes field takes `--mount` values (a `type=` token starts one and runs on over `key=value`, `readonly` and `ro`; `splitVolumeField`). A test of a mount sets `HOME`, `XDG_RUNTIME_DIR`, `DOCKER_HOST` and the working directory itself (`newMountEnv`, `isolateMountHost`). See `docs/architecture/network.md`, "A launch asks before it mounts something sensitive of the host".

**Tests and engine contracts (DEVDESK-806, OC-09a1).** A contract is a `TestContract*` function of a
`*_contract_test.go` file of the package it contracts (today `internal/docker/engine_contract_test.go`); it reaches a real
docker or podman, outside CI, in the test VMs: `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- <docker|podman> <n|n1>
[--mode rootful|rootless] [--no-reset]` (`tools/engine-vms/contract.sh verify`, which acts on the Incus host; run by hand, never
by a test). The task never sets the variable; the user sets it for the one command and never exports it (an exported variable opens the gate for every later `go test`). The gate is `enginecontract.Decide`
(`internal/testutil/enginecontract`): the variable not `1` skips, `CI` (or `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE`, `TF_BUILD`) set refuses (even with the variable), then the tool
(`DEVDESK_ENGINE_CONTRACT_TOOL`, docker or podman, no default); a plain `go test` skips every contract. Every contract starts
with `requireEngine(t)` (defined in `contract_support_test.go`, its first statement `enginecontract.Require(t, probeEngine)`):
gate, `AllowRealEngine`, probe, N and N-1 check, banner. Do not call `engineguard.AllowRealEngine` yourself. Guards: G5
`TestGuardContractTestsCallRequireEngine` (`internal/engineguard/contract_guard_test.go`: R1 first statement, R2 only
`TestContract*` functions and no `t.Parallel` in a contract file, R3 where `requireEngine` and `AllowRealEngine` may be
called, R4 the definition starts with `enginecontract.Require(`; `contractExceptions` is empty), C1
`TestOnlyTestsImportEngineContract` (only `_test.go` files import `enginecontract`), `TestNothingOpensTheContractGate`
(`mise.toml` and the workflows never set the variable) and the child-process test
`TestEveryContractIsSkippedOrRefusedOutsideAContractRun`. The production function that reports a missing binary is
`engineAvailable()` (it was `requireEngine()`). The probe templates are the plan's, **to be confirmed on the test VMs**.
Recording (OC-09a3, DEVDESK-944): `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract:record -- <docker|podman> <n|n1> [--mode ...] [--keep-out <dir>]`
(`contract.sh record`; `--no-reset` is refused, `--keep-out` is the dry run) sets `DEVDESK_FIXTURE_RECORDING` and `DEVDESK_FIXTURE_OUT` in the VM only,
`c.exchange` redacts and writes through `Session.Record` (lab mark required, leak check before the write), and the one output directory comes back by
a tar that is **listed and refused when an entry is not `<campaign>/<name>.cli`, then extracted strictly** (new `mktemp -d`, no `--same-owner`)
before `engine-fixture-import --require-denylist` checks it all again (a recording needs `DEVDESK_FIXTURE_DENYLIST` with at least one term; the
strict extraction is `tools/engine-vms/unpack_recorded.sh`, with a 16 MiB cap on the archive and on the sizes it announces). In verify mode `Session.Verify` with fixtures for the engine but none for the scenario fails, and with **no** recorded fixture at all fails (`enginecontract.RequireFixtures` is true since OC-09b2; it used to log and go on, never skip: a skip ends
the contract). See `docs/architecture/network.md`, "Engine contracts" (Recording).
Resources (OC-09a2, DEVDESK-943): everything a contract creates carries the label `devdesk.test=<run id>` and the name
`ddc-<run id>-<role>` (`c.container`, `c.volume`, `c.network`, from `DEVDESK_ENGINE_CONTRACT_IMAGE`, never pulled), and every call
goes through the recorder of the contract and its allow list `contractArgvRule`, which reads options and not only verbs (`run` and
`create`: a list of options up to the image, `--name` under `ddc-<run>-`, the label, `--pull=never`, named volumes of the run only,
no `--privileged`, `--replace`, bind mount, host namespace, `--cap-add`, `--device`, `--security-opt`, published port; a destructive
verb names only resources of the run and takes only its own options, so no `--force` on `network rm` or `--depend` on `rm`;
`--host`, `-H`, `--context`, `--connection`, `--url`, `--remote`, `--root`, `--runroot`, `--storage-driver` are refused at any
position; every `prune`, `system`, `pull`, `rmi` and `pod create` are refused (but inside the three windows below); an unknown verb or option is refused: add it there
with its reason). `Require` also refuses `DOCKER_HOST`, `DOCKER_CONTEXT`, `CONTAINER_HOST` and `CONTAINER_CONNECTION` (not the
`~/.docker/config.json` context), and the image must be `repository@sha256:<64 hex>` (`contract.sh`: the exact alpine pin). The cleanup (`cleanupNow`, registered by `startContract`) lists by the
label filter, **inspects the label of each candidate again**, removes only a label equal to the run through `Remove*` (G3), and
reports anything else without touching it. Creating or removing needs the lab mark on top of the gate: `DEVDESK_ENGINE_CONTRACT_RUN`
valid and `/etc/dd/current` holding `n` or `n1` (`enginecontract.CheckLab`; root can forge the file on a dev machine, it stops an
accident, not an adversary). `contract.sh` pulls `CONTRACT_IMAGE` (`versions.env`, pinned by digest) into the VM, the only pull,
and checks a label-less sentinel container after the contracts (`vm_as_mode`). See `docs/architecture/network.md`, "Engine contracts".
First scenarios (OC-09b1, DEVDESK-807): nine more contracts in `containers_`, `images_`, `networks_`, `volumes_` and `launch_contract_test.go`
(`ps-health`, `inspect-absent`, `rmi-used`, `rm-used` for a network and a volume, `volumes/ls`, `volumes/prune-unused`, `name-taken`,
`port-taken`), with helpers in `contract_support_test.go`: `healthContainer`, `waitHealthy`, `portHolder` (port 18080 on loopback),
`usedVolume`, `usedNetwork`, `usedImage`, `public`, and three **windows** for the argv `contractArgvRule` refuses, each one argv, once, for one call, never a
loosening of the rule: `expectPrune` (`<kind> prune -f` of container, network or volume, only when a read right before shows the whole
store is this run's), `expectRefusal` (one `run` that names the window's name and optionally one loopback port; what the engine leaves
behind under that name is removed by id) and `expectRmiRefusal` (`rmi <id>` only after the ancestor filter shows a container of the run uses the image). Comparers
`enginecontract.Tolerating(Durations, Timestamps, Health)`, `SameUpToDurations` and `SameUpToDurationsAndHealth` leave the values that move with the clock out
of a comparison; `contract.sh` passes the sentinel's id as `DEVDESK_ENGINE_CONTRACT_SENTINEL` and the recorder redacts it to the role `sentinel`.
Their fixtures are versioned since OC-09b2 (40 files, ten scenarios, docker 29.10.1 and 28.5.2, podman 6.2.1 and 5.8.11; `minVersionedFixtures = 40`,
`RequireFixtures = true`): the verify mode compares. Recording adds `Redaction.Anonymous` (`c.anonymous(n)`: the endpoint ids of docker's refusals, replaced in
order of appearance, a word beyond n still refused), `FixedTimestamps`/`FixedDurations` (`c.normalize(...)`: fixed values in a recording only, `ps-health`) and the
comparer `SameUpToContainerIDs` (`rmi-used` names the sentinel or the container of the run). Never edit a `.cli` by hand: record again.

**Tests and recorded CLI exchanges (DEVDESK-805, OC-08a).** `internal/enginefixture` holds what a docker or podman
answered to one argv, one `.cli` text file per exchange (`# key: value` headers in a fixed order, stdout and stderr of
declared length, base64 for what is not safe text, LF only, `Format(Parse(b)) == b`; `record: pending` is never loaded).
`Parse`, `Load`/`LoadFS`, `ForConfig(fixtures, tool, version, mode)`, then `NewReplay`: the key is the tool and the exact
argv, the test picks its configuration, `Answer` returns a copy, `Unused()` lists what nothing asked. In `internal/docker`
a test calls `useFixtures(t, f...)` (`fixture_runner_test.go`, in memory; a non-zero exit is a `*fixtureExit`, read its code
through `interface{ ExitCode() int }`, not `ExitError.ExitCode()`, which is -1 there); a view test calls
`fakecli.Install(t, fakecli.Fixtures(t, f...), "docker")`, and `fixture_equivalence_test.go` holds the two paths equal.
`fakecli` carries the bytes of a `Reply` unchanged. Only `_test.go` files and `internal/testutil/fakecli` import
`enginefixture` (guard E1, `TestOnlyTestsImportEngineFixture`). **Real fixtures are versioned since OC-09b2** (`fixtures/<campaign>/`, 40 files): the
mechanisms also run on `internal/enginefixture/testdata/synthetic` (`recorded-by: synthetic`). The recorder (OC-09a3, DEVDESK-944) is
`enginefixture.Redact` (ids, home, run id, host, user to placeholders; a short user such as `dd` is left for `CheckLeaks` to refuse),
`enginecontract.Session.Record`/`Verify` and `enginefixture.Import`, run on the workstation by `internal/enginefixture/cmd/engine-fixture-import`
(all or nothing, one configuration, N and N-1, leak check with `DEVDESK_FIXTURE_DENYLIST`; an unchanged fixture is not rewritten, a `skip` one is
kept, nothing is deleted); `mise run engine-contract:record` runs the whole chain. See `docs/architecture/network.md`,
"Recorded fixtures".

**Guards on recorded fixtures (DEVDESK-942, OC-08b).** `TestFixturesLeakNothing` runs `enginefixture.CheckLeaksAs` on every
`.cli` file of the tree (`fixtures/*/*.cli`, `testdata/*/*.cli`, pending files included), with the external deny list
(`DEVDESK_FIXTURE_DENYLIST`, a file outside the repository, one term per line) and the identities of the machine
(`MachineIdentities`); a failure names file, line and rule, never the text found. Rules: `secret-token-format`,
`jwt-like-value`, `host-not-allowed`, `private-or-link-local-address` (the engines' default networks are allowed),
`public-address` (a public IPv4 or a global IPv6 address, documentation ranges and loopback allowed; OC-09a3), `internal-name`, `personal-path`
(`fixture-user` and `ImageHomes` allowed), `lab-identity`, `local-identity`, `exact-term`, `sensitive-env-value`, `invisible-character`; names are matched at the word, a dot included as a boundary. The detectors
are shared with Nexus in `internal/fixtureleak` (do not declare one again in either fixture package). `TestFixturesAreNotStale`
reads N and N-1 from `tools/engine-vms/versions.env` (`ParseVersions`): a bump edits that file, `versions_test.go` and
`network.md`, then records again; `minVersionedFixtures` is 40 (OC-09b2) and never goes down. `TestNoPendingFixtureIsVersioned`
refuses a `record: pending` file under `fixtures/`, and `DEVDESK_FIXTURE_RECORDING=1` when `CI` is set (a local session
only); `TestRecordingSessionIsNeverSetByTheStandardChecks` keeps that variable out of `mise.toml` (outside the recorder
tasks) and out of `.github/workflows`. A `record: skip: <reason>` fixture is kept and checked, left out of `ForConfig`
and refused by `NewReplay`. Exception lists, exported and kept by hand, each entry with its reason: `ImageHomes`,
`AllowedInternalNames`, `AllowedSensitiveEnvNames`. Limits: an arbitrary host name or an FQDN whose short name is not in
`MachineIdentities` is seen only through the deny list, a local network in `172.17.x` passes, the short lab term `dd` has strict
boundaries (and still matches the command `dd`), `GPG_KEY` is allowed by name. See `docs/architecture/network.md`, "Recorded fixtures".

**Test input and colour (DEVDESK-199).** Build keys only through
`internal/ui/testutil` (`Key`, `Keys`, `Type`, `Paste`, `Text`, `IsText`), never a
literal `tea.KeyPressMsg` or `tea.Key*`. `Key` returns a
`tea.KeyPressMsg` (`Keys` and `Type` return a message list), `Paste` a `tea.PasteMsg`; `Key` is strict: an unknown name
panics. The space key is built as a key (`Key("space")`), never matched through
`String()` (v1 `" "`, v2 `"space"`). Lip Gloss v2 has no global colour profile (`Style.Render` always emits full
colour), so a test needs no setup to check styling; it reads the SGR or strips it
with `ansi.Strip`. `internal/ui/testutil/source_guard_test.go` fails the suite on a
hand-built key, a `colorprofile` import outside `internal/app/profile_test.go` and
`internal/app/redraw_measure_test.go` or `SetColorProfile` in any `_test.go`. Paste tests
(`internal/app/paste_test.go`, `wrapped_input_test.go`, `component_form_test.go`):
a paste reaches the focused field, newlines become spaces in single-line fields,
and no key action fires (Rule 111).

**Space and key API (DEVDESK-200).** Production code matches the space key as
`keymap.Space`, never `" "`/`"space"`, and reads keys through `String()` only
(no `.Runes`, `.Alt`, `.Paste`, `tea.KeyType` or `tea.Key<Name>`). Update
handlers switch on `tea.KeyPressMsg`, never the `tea.KeyMsg` interface (it also
matches releases). A paste is a `tea.PasteMsg`, not a key (Rule 111). Guards in
`internal/ui/keymap` (`space_scan_test.go`, `legacy_key_scan_test.go`,
`viewport_keymap_scan_test.go`) fail the suite otherwise; limits are listed in
`docs/architecture/app-shell.md`.

## Dependencies

Go 1.27 (`go 1.27.3` in `go.mod`). Key libraries (versions as in `go.mod`):
- `charm.land/bubbletea/v2` (v2.1.1) - TUI framework (v2: `tea.KeyPressMsg`, `tea.PasteMsg`, `View() tea.View`)
- `charm.land/bubbles/v2` (v2.2.2) - Pre-built TUI components (table, textinput, viewport)
- `charm.land/lipgloss/v2` (v2.0.6) - Styling (no global colour profile; colours are `image/color.Color`)
- `github.com/charmbracelet/colorprofile` (v0.4.3) - colour profile `main.go` forces under Windows Terminal (replaces `termenv`)
- `github.com/NimbleMarkets/ntcharts/v2` (v2.9.0) - dashboard sparkline
- `github.com/charmbracelet/x/ansi` - ANSI-safe width, strip and cut
- `gitlab.com/gitlab-org/api/client-go` - GitLab API client
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality (maintained fork of go-ping/ping)
- `github.com/google/go-github/v68` - GitHub API client (§3.6)
- `github.com/libp2p/go-netroute` - which interface a destination leaves by, on all three platforms (§3.44)
- `golang.org/x/sys` - direct (Rule 303): the socket calls of `internal/nettrace` (`IP_TTL`, `IP_RECVERR`, `recvmsg` on the error queue); `x/sys/windows` serves `iphlpapi` (the Windows path trace, `IcmpSendEcho2`)
- `github.com/zalando/go-keyring` - host secret manager (wincred / Keychain / Secret Service), no cgo
- `gopkg.in/yaml.v3` - YAML configuration

## Code Conventions

- Comments: **English only** (Rule 307) — existing French comments are not rewritten wholesale, but any new or edited comment is in English
- Commit messages: **English only** (Rule 307)
- UI text and logs: **English US only** (Rule 129)
- Bubbletea models define their own message types
- Use `theme` package for consistent styling (`internal/ui/theme/`)
- Config changes must call `config.Save()` to persist
- All colors defined in `theme/colors.go`, all styles in `theme/styles.go`
- Message naming: `[ComponentName][Action]Msg` (e.g., `ComponentFormSubmitMsg`)
- Context names: the rule exists twice (`config.ValidateContextName`, `contextcmd.ValidName`); change both or neither (`TestContextNameRulesAgree`). Creation refuses `-`, `new`, `list` (`config.ValidateNewContextName`); loading does not (D91)
- Finding text: any new `Resolution`, `Steps` or `Repro` of a scan finding follows the convention in `docs/architecture/scanning.md` ("Writing a Resolution, Steps and Repro for a new finding"); `Repro` only through `localRepro`, `Steps` only from a `*Steps` function (guards in `internal/scan`)

## Critical Bubble Tea Rules

**NEVER modify model state inside a Cmd** (Rule 110) - Race condition:
- `Update()` is the ONLY place to modify model state
- `View()` is read-only
- `Cmd` functions do I/O and return messages

**NEVER use `style.Render()` inside `table.Row{}`** (Rule 122) - ANSI sequences corrupt all subsequent rows:
- Use plain text: `theme.IconError + " error"` not `theme.StatusErrorStyle.Render(...)`
- Apply row styling via `table.SetStyles(theme.TableStylesForState("error"))`

**Views render through `Render()`** (Bubble Tea v2): a top-level view implements
`Render() string` and `View() tea.View { return tea.NewView(m.Render()) }`; only the
router (`App.View()`) sets terminal state such as `AltScreen` and `BackgroundColor`. Components keep
`View() string`. Colours are `color.Color`; hex literals stay in `theme/colors.go`.

**Update() case extraction**: If a case block exceeds 5 lines, extract to `handle[MessageType]()` method returning `(tea.Model, tea.Cmd)`
