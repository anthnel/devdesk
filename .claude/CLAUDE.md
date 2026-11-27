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
| [`network.md`](../docs/architecture/network.md) | `internal/docker` and `internal/oci` (typed `StatusError`, `...Context` variants), `internal/netproxy` (the proxy entry point every HTTP and TLS connection goes through, and its guard test, DEVDESK-581), the proxy passed to scanner containers (`netproxy.Forward`, `scan.containerRun.cmd`, the `TestContainerGuard*` guard, DEVDESK-583), `internal/imagesearch` and the registry browser's image search level, image updates (`internal/imageupdate`, the Update column), verified pulls (`internal/imagepull`), the recorded engine fixtures and their replay (`internal/enginefixture`, `fakecli.Fixtures`, DEVDESK-805), the netdiag view, `internal/ports` (the signal seam `realOS.kill`, the process identity check, DEVDESK-709, DEVDESK-711), `internal/netiface`, `internal/nettrace` (the path trace: unprivileged UDP + `IP_RECVERR` on Linux, ICMP echo through `iphlpapi` on native Windows) |
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

**Tests and the real engine (DEVDESK-798, OC-03a).** In a test binary, `cliRunner.LookPath`, `Run` and
`Build` call `engineguard.Check`: an engine binary resolved outside a directory `fakecli` declared
(`fakecli.AllowFakeCLIDir`, called by `Install` and `InstallAt`) is **recorded, not refused** (survey mode;
OC-03b will make it a panic). `mise run engine-survey` lists the offenders (heavy: the whole suite, outside
`ci`, `check` and `test`). A test that reaches a real engine on purpose calls `engineguard.AllowRealEngine(t)`,
which fails unless `DEVDESK_ENGINE_CONTRACT=1` and `CI` is empty. No `t.Parallel`. See
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
and a shell line are not seen; `ShellCmd` does not validate yet (DEVDESK-939). See `docs/architecture/network.md`, "Guard G3".

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

**Tests and recorded CLI exchanges (DEVDESK-805, OC-08a).** `internal/enginefixture` holds what a docker or podman
answered to one argv, one `.cli` text file per exchange (`# key: value` headers in a fixed order, stdout and stderr of
declared length, base64 for what is not safe text, LF only, `Format(Parse(b)) == b`; `record: pending` is never loaded).
`Parse`, `Load`/`LoadFS`, `ForConfig(fixtures, tool, version, mode)`, then `NewReplay`: the key is the tool and the exact
argv, the test picks its configuration, `Answer` returns a copy, `Unused()` lists what nothing asked. In `internal/docker`
a test calls `useFixtures(t, f...)` (`fixture_runner_test.go`, in memory; a non-zero exit is a `*fixtureExit`, read its code
through `interface{ ExitCode() int }`, not `ExitError.ExitCode()`, which is -1 there); a view test calls
`fakecli.Install(t, fakecli.Fixtures(t, f...), "docker")`, and `fixture_equivalence_test.go` holds the two paths equal.
`fakecli` carries the bytes of a `Reply` unchanged. Only `_test.go` files and `internal/testutil/fakecli` import
`enginefixture` (guard E1, `TestOnlyTestsImportEngineFixture`). **No real fixture is versioned before OC-09b**: the
mechanisms run on `internal/enginefixture/testdata/synthetic` (`recorded-by: synthetic`). See `docs/architecture/network.md`,
"Recorded fixtures".

**Guards on recorded fixtures (DEVDESK-942, OC-08b).** `TestFixturesLeakNothing` runs `enginefixture.CheckLeaksAs` on every
`.cli` file of the tree (`fixtures/*/*.cli`, `testdata/*/*.cli`, pending files included), with the external deny list
(`DEVDESK_FIXTURE_DENYLIST`, a file outside the repository, one term per line) and the identities of the machine
(`MachineIdentities`); a failure names file, line and rule, never the text found. Rules: `secret-token-format`,
`jwt-like-value`, `host-not-allowed`, `private-or-link-local-address` (the engines' default networks are allowed),
`internal-name`, `personal-path` (`fixture-user` and `ImageHomes` allowed), `lab-identity`, `local-identity`, `exact-term`,
`sensitive-env-value`, `invisible-character`; names are matched at the word, a dot included as a boundary. The detectors
are shared with Nexus in `internal/fixtureleak` (do not declare one again in either fixture package). `TestFixturesAreNotStale`
reads N and N-1 from `tools/engine-vms/versions.env` (`ParseVersions`): a bump edits that file, `versions_test.go` and
`network.md`, then records again; `minVersionedFixtures` is 0 until OC-09b raises it. `TestNoPendingFixtureIsVersioned`
refuses a `record: pending` file under `fixtures/`, and `DEVDESK_FIXTURE_RECORDING=1` when `CI` is set (a local session
only); `TestRecordingSessionIsNeverSetByTheStandardChecks` keeps that variable out of `mise.toml` (outside the recorder
tasks) and out of `.github/workflows`. A `record: skip: <reason>` fixture is kept and checked, left out of `ForConfig`
and refused by `NewReplay`. Limits: an arbitrary host name is seen only through the deny list, a local network in `172.17.x`
passes, `ImageHomes` is kept by hand. See `docs/architecture/network.md`, "Recorded fixtures".

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
