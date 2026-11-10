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
- Network diagnostics (DNS, route, ICMP, TCP, TLS, HTTP) + real-time port monitoring + this machine's interfaces + local port forwarding (unprivileged, loopback only)
- Multi-context configuration management via YAML
- Local workspaces management with git metadata
- Claude Code session list (`:agent-usage` / `:au`): sessions found on this machine, by project and date, and per session the agent tree (orchestrator and sub-agents: types, models, tokens, share of the total; sortable by Total; also per agent a state (running / idle / done / unknown, idle after 5 min without a transcript line) shown as a status dot in the first column (the `running` dot blinks, DEVDESK-151) and as text, tool calls and duration; and an indicative cost per agent, `Est. cost` (orange from 75 % and red from 90 % of the session's estimated cost, when at least two agents are priced), from prices you write in the context YAML (`agent_usage.prices`, none shipped), with the session's `Billed` cost and `Unattributed` in the header, DEVDESK-47; a `Breakdown` colour bar per agent and, with `tab` inside a session, a Breakdown tab splitting the whole session's tokens by category, model and agent, as a percentage of tokens, DEVDESK-98). An open session updates live: read again every 2 s while active, only what was appended; paused after 5 min with no write and no unfinished sub-agent, or when its transcript is gone (header `Updates › paused`); `ctrl+r` reads it again in full. The sessions list itself is not live.

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
| [`app-shell.md`](../docs/architecture/app-shell.md) | the router and its views, `ctrl+p` and the command parser, the keyboard vocabulary (`internal/ui/keymap`), greyed shortcuts, `shared.State`, cross-view messages, the jobs registry (`internal/jobs`), the one animation tick (blink phase, `app.motion`), the failure memory (`jobs.FailureKey`, `ChangedMsg.Failures`), the activity-column guard (`activity_guard_test.go`) and the `:jobs` view |
| [`configuration.md`](../docs/architecture/configuration.md) | the config schema, the contexts, the migrations (`gitlab:` → `forge:`, `docker:` → `network:`), `agent_usage.prices`, and the configuration view |
| [`forge.md`](../docs/architecture/forge.md) | `internal/forge` and its two backends, `forge.Vocabulary`, the explorer's clone pipeline, and how the explorer reads the forge index (`internal/forgeindex`) and its `g` |
| [`workspaces.md`](../docs/architecture/workspaces.md) | the `ws` file icons (`internal/ui/fileicon`) and the sync (`F`) |
| [`filebrowser.md`](../docs/architecture/filebrowser.md) | `:files` (`internal/ui/filebrowser`, `internal/fsbrowse`), the path picker a form's path field opens with enter, and `theme.ShortPath` |
| [`viewer.md`](../docs/architecture/viewer.md) | `internal/viewer` + `internal/ui/viewer` — kinds, displays, colouring, search, follow |
| [`scanning.md`](../docs/architecture/scanning.md) | Trivy, Gitleaks, plumber, `scan.Categorize`, the `:sec` inventory, the two scan caches, image signatures (`internal/trust`, the cosign verifier) |
| [`registries.md`](../docs/architecture/registries.md) | the registry/group model and the discovered-members cache |
| [`network.md`](../docs/architecture/network.md) | `internal/docker` and `internal/oci`, image updates (`internal/imageupdate`, the Update column), verified pulls (`internal/imagepull`), the netdiag view, `internal/ports`, `internal/netiface` |
| [`mcp.md`](../docs/architecture/mcp.md) | the MCP server the TUI serves over HTTP |
| [`templates.md`](../docs/architecture/templates.md) | `internal/template` + `internal/ui/templates` — the repository-template catalog, its three sources, how files are fetched, and the `:templates` view |
| [`ui-components.md`](../docs/architecture/ui-components.md) | `components.FooterMessage`, `internal/ui/datatable`, and the activity indicator column (`internal/ui/indicator`, `datatable.IndicatorColumn`, the row sentences `components.DescribeRow`) |
| [`agent-usage.md`](../docs/architecture/agent-usage.md) | `internal/agentusage` — per-agent token counts from Claude Code transcripts: format, aggregation rules, tolerance, privacy (`cwd` amendment), caps, ccusage cross-check; `ListSessions`; incremental reading (`ReadDelta`, `Aggregate`, `Activity.Settled`); tool calls and time span per agent (DEVDESK-46); per-model tokens, `Billed`, and the cost estimate (`internal/agentusage/pricing`, prices from `agent_usage.prices`, DEVDESK-47); the `:agent-usage` view and its agents level and State/Tools/Duration/Est. cost columns; the Breakdown column and tab, `Usage.Categories`/`ModelTotals` (DEVDESK-98) (`internal/ui/agentusageview`) |

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

**Test input and colour (DEVDESK-199).** Build keys only through
`internal/ui/testutil` (`Key`, `Keys`, `Type`, `Paste`, `Text`, `IsText`), never a
literal `tea.KeyPressMsg` or `tea.Key*`. `Key` returns a
`tea.KeyPressMsg` (`Keys` and `Type` return a message list), `Paste` a `tea.PasteMsg`; `Key` is strict: an unknown name
panics. The space key is built as a key (`Key("space")`), never matched through
`String()` (v1 `" "`, v2 `"space"`). Lip Gloss v2 has no global colour profile (`Style.Render` always emits full
colour), so a test needs no setup to check styling; it reads the SGR or strips it
with `ansi.Strip`. `internal/ui/testutil/source_guard_test.go` fails the suite on a
hand-built key, a `colorprofile` import outside `internal/app/profile_test.go`
or `SetColorProfile` in any `_test.go`. Paste tests
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

Key libraries (see `go.mod`):
- `charm.land/bubbletea/v2` - TUI framework (v2: `tea.KeyPressMsg`, `tea.PasteMsg`, `View() tea.View`)
- `charm.land/bubbles/v2` - Pre-built TUI components (table, textinput, spinner, viewport)
- `charm.land/lipgloss/v2` - Styling (no global colour profile; colours are `image/color.Color`)
- `github.com/charmbracelet/colorprofile` - colour profile `main.go` forces under Windows Terminal (replaces `termenv`)
- `github.com/NimbleMarkets/ntcharts/v2` - dashboard sparkline
- `github.com/charmbracelet/x/ansi` - ANSI-safe width, strip and cut
- `gitlab.com/gitlab-org/api/client-go` - GitLab API client
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality (maintained fork of go-ping/ping)
- `github.com/google/go-github/v68` - GitHub API client (§3.6)
- `github.com/libp2p/go-netroute` - which interface a destination leaves by, on all three platforms (§3.44)
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
router (`App.View()`) sets terminal state such as `AltScreen`. Components keep
`View() string`. Colours are `color.Color`; hex literals stay in `theme/colors.go`.

**Update() case extraction**: If a case block exceeds 5 lines, extract to `handle[MessageType]()` method returning `(tea.Model, tea.Cmd)`
