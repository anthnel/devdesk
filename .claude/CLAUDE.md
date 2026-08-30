# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> [!IMPORTANT]
> **AI Collaboration Rule**:
> - **Planning**, **Brainstorming**, **Implementation** and **Bug fixes** are all
>   in scope. Planning is no longer delegated to Gemini (Antigravity).
> - Implementation plans go in `.claude/plans/` (see
>   [rules/planning.md](rules/planning.md)); design decisions that outlive a plan
>   are recorded in `docs/backlog.md`.
> - Always refer to [project-context.md](../project-context.md) for global project rules.
>   It sits in this repository, at the root. The absolute path this line used to
>   carry named a GitLab checkout that is no longer where DevDesk lives.

## Project Overview

DevDesk is a terminal-based TUI (Text User Interface) application built with Go and Bubble Tea framework. It provides DevSecOps functionality including:
- System status monitoring (HTTP/HTTPS, ICMP, DNS, SSL checks)
- GitLab integration (authentication, project explorer, multi-select clone)
- Security scanning (Trivy CVE/secret/license/misconfig + Gitleaks secrets)
- Docker container management with real-time metrics
- OCI resource management (image scanning, container launching, network inspection)
- Network diagnostics (DNS, route, ICMP, TCP, TLS, HTTP) + real-time port monitoring + this machine's interfaces
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

## Work starts in a new worktree

**Any task that will produce a commit begins with its own worktree**, cut from
the current `main`, outside the repository directory:

```bash
git fetch origin main
git worktree add -b <branch> ../devdesk-<branch> origin/main
cd ../devdesk-<branch>
```

Answering a question, reading code, running the app — none of that needs one.
Editing does.

**Outside the repository, not under it.** A worktree placed at `.worktrees/x`
would be a second copy of every package inside the module root: `go build ./...`
and `go test ./...` would walk into it, compile it, and report failures from a
tree nobody is looking at. Siblings of the checkout (`../devdesk-<branch>`) keep
the module root holding one copy of the code, which is why they are not merely a
tidier choice.

**What isolates, and what does not.** git refuses to check out one branch in two
worktrees, so two agents cannot land on the same branch by accident — that is the
whole protection, and it is worth knowing what it does *not* cover:

| Shared by every worktree | Consequence |
|---|---|
| `~/.devdesk/` — contexts, `config.yaml`, scan caches | two agents editing settings or scanning at once overwrite each other's; nothing warns |
| the host keyring, Docker, the ports the app binds | one at a time, whoever gets there first |
| `.git/hooks` | verified: `git rev-parse --git-path hooks` resolves to the **main** checkout's, so the Entire checkpoint and pre-push hooks fire normally from a worktree |
| `origin` | inherited — `git push -u origin <branch>` goes through the mirror as usual |

`mise` tasks and `go build` work unchanged; both read the tree they are run in.

**`entire graph` indexes per directory.** A fresh worktree has no index, so the
first `entire graph search` there pays a full build (~5 s on this repo, measured)
and the symbol ids it returns are namespaced by the directory name
(`local/devdesk-<branch>:Go:…`). Nothing breaks; do not be surprised by the
first search being slow or by ids that do not match another worktree's.

**Clean up when the PR is merged**, from the main checkout:

```bash
git worktree remove ../devdesk-<branch>
git worktree list          # what is still out there
git worktree prune         # after a directory was deleted by hand
```

`git worktree remove` refuses a tree with uncommitted changes, which is the
point — look before forcing.

**The main checkout stays on `main` and stays clean.** It is what `git fetch
origin main && git merge --ff-only origin/main` needs after each merge, and what
every new worktree is cut from.

## Git workflow — pull requests only

**Never push directly to `main`.** The Entire mirror rejects it:

```
! [remote rejected] main -> main (protected branch)
```

This is enforced by the mirror, not by GitHub, and **Entire does not document it**
— neither the CLI help nor `docs.entire.io` mentions a protected default branch.
The only branch restriction Entire states is that `entire/unmirrored/*` is never
forwarded. Treat the rejection as observed behaviour, not as a specified one.

What rules GitHub out is that branch protection is *unavailable* on this
repository: it is private on a free personal plan, so both endpoints answer
`403 Upgrade to GitHub Pro or make this repository public` —
`/repos/anthnel/devdesk/branches/main/protection` and `/repos/anthnel/devdesk/rulesets`
alike. Do not cite `gh api repos/anthnel/devdesk/branches/main` reporting
`"protected": false` as the proof: that field only ever reflects legacy branch
protection, so it reads `false` under a ruleset too, and on this plan it would
read `false` whatever the configuration. The 403 is the evidence; the `false` is
not.

Every other branch, including `entire/checkpoints/v1`, pushes through the mirror
and is forwarded to GitHub.

**Pull requests are GitHub's, not Entire's.** Entire has no merge-request
concept — it layers checkpoints and sessions over git and forwards pushes. So
the PR steps below are `gh`, and the branch they review is one `origin` carried
to GitHub. `entire review` (labs) runs a multi-agent review against the current
branch and is a pre-merge step, not a substitute for the PR.

```bash
git worktree add -b <branch> ../devdesk-<branch> origin/main   # work happens here
git push -u origin <branch>                     # via the mirror — forwarded to GitHub
gh pr create -R anthnel/devdesk --base main --head <branch>
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
# then, from the main checkout:
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see below
git worktree remove ../devdesk-<branch>
```

**`-R anthnel/devdesk` is not optional.** `gh` infers the repository from a
remote pointing at a GitHub host, and `origin` is an `entire://` URL, so it
finds none and fails with *"none of the git remotes configured for this
repository point to a known GitHub host"*. The flag names the repo directly.
Do not solve this by adding a GitHub remote — see below.

**After a merge, `origin` can read stale for a minute or two.** The mirror lags
GitHub, so `git fetch origin` right after `gh pr merge` may return the *previous*
`main` — with no error, which is the part that misleads. `gh pr view <n> --json state`
says `MERGED` while `git rev-parse origin/main` still points at the commit before
it. Re-run the fetch a moment later; `gh` is what to trust in the meantime,
because it talks to GitHub directly and needs no remote.

**Do not add a second remote pointing at GitHub to work around that.** It would
be the one path that defeats the only protection there is: `main` cannot be
protected on GitHub here (see the 403 above), so the mirror's refusal is the
whole of it, and `git push github main` would simply succeed. A direct push also
bypasses the mirror, and with it the Entire hook that records checkpoints and
sessions — which is not visible until much later. `origin` is the only remote,
deliberately.

**Merging several branches cut from the same commit conflicts in
`docs/backlog.md`.** Every fix inserts its entry at the top of §1.1, so the
second and third merges land on the same anchor. The resolution is always to
keep both sides — they are independent entries, not competing edits.

### Landing a sandbox-made commit (host-relay)

A Docker Sandbox (`sbx`) has no `entire://` auth of its own — `ENTIRE_TOKEN`
via `sbx secret set-custom` doesn't work (`git-remote-entire` parses the
JWT's claims locally, before any network call, but the secret proxy only
substitutes the real token on the wire — the sandbox process itself only
ever sees an unparseable placeholder), and an interactive `entire login`
inside the sandbox trades that for a persistent, harder-to-reason-about
credential sitting in a less-trusted place. So the sandbox edits and commits
locally only; the host does every `origin` operation — fetch, push, PR,
merge. `git pull`/`git fetch` run *inside* the sandbox will fail with `no
auth context for cluster ...` — that's expected, not a setup bug; don't
`entire login` there to fix it (see `~/projects/github/anthnel/sbx-kits/entire/README.md`
for the full rationale).

A direct-mode sandbox mounts this exact repo directory, so its commits land
straight in the shared `.git` — the host sees them immediately, no transfer
needed. But `git worktree add -b <branch> ../devdesk-<branch> ...` run
*inside* the sandbox creates that sibling path **outside** the single
mounted directory, so it lands in the sandbox's own private container
layer — invisible to the host (`git worktree list` shows it `prunable`).
The commit object itself is still in the shared `.git/objects` though, so
nothing is actually lost:

```bash
# From the host, once the sandbox reports "done and committed":
git worktree list                      # confirm the sibling worktree, prunable, HEAD sha
git cat-file -t <sha>                  # confirm the commit is really in this .git (it is)
git worktree prune -v                  # drop the stale, unreachable admin entry

git push origin <sha>:refs/heads/<branch>
gh pr create -R anthnel/devdesk --base main --head <branch> \
  --title "<subject line>" --body "$(git show -s --format=%b <sha>)"
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see above
```

**A follow-up commit on the same branch, after the first one already got
squash-merged, will conflict on a plain `git merge`** — squashing rewrites
history, so `git merge origin/main` computes the wrong common ancestor
(the branch's *own* pre-squash parent) and re-diffs files that already
landed, even when nothing actually conflicts in substance. Don't hand-edit
through that; cherry-pick the new commit straight onto current `origin/main`
instead — its real parent already matches what's on `main`, so it applies
clean:

```bash
git worktree add -b <tmp-branch> ../devdesk-<tmp-branch> <new-sha>
cd ../devdesk-<tmp-branch>
git reset --hard origin/main
git cherry-pick <new-sha>              # clean apply — verify before trusting this
go build ./... && go test ./<touched-packages>/...
git push origin HEAD:refs/heads/<new-branch-name>
# gh pr create / gh pr merge as above, then from the main checkout:
git worktree remove ../devdesk-<tmp-branch>
```

### Remotes

| Remote | URL | Use |
|--------|-----|-----|
| `origin` | `entire://aws-eu-central-1.entire.io/gh/anthnel/devdesk` | Entire mirror — everything: clone, fetch, push |

**There is one remote, and there should stay one.** The repository is hosted on
GitHub and mirrored to EntireDB in `aws-eu-central-1` (a second placement exists
in `aws-us-east-2`), but every git operation goes through the mirror: it is the
regional path, it is what keeps agent reads fast, and it is where the checkpoint
hook runs. Anything needing GitHub's own answer — PR state, review threads —
goes through `gh`, which authenticates directly and does not need a remote.

Semantic search (`entire search`, and the `entire:*` skills that depend on it) is
**not available in the EU region** — the server answers `semantic search is not
yet available in the region(s) hosting this search`. Nothing in the repo config
fixes this.

## Architecture

The deep notes live in `docs/architecture/`, one file per area, and they are
**not** `@`-imported: an `@` would inline all of it into every session, which is
the thing this split exists to avoid. Read the file that covers what you are
touching, and update it in the same commit as the code.

| File | Covers |
|---|---|
| [`app-shell.md`](../docs/architecture/app-shell.md) | the router and its views, `ctrl+p` and the command parser, the keyboard vocabulary (`internal/ui/keymap`), greyed shortcuts, `shared.State`, cross-view messages, the jobs registry (`internal/jobs`), the one spinner chain and the `:jobs` view |
| [`configuration.md`](../docs/architecture/configuration.md) | the config schema, the contexts, the migrations (`gitlab:` → `forge:`, `docker:` → `network:`), and the configuration view |
| [`forge.md`](../docs/architecture/forge.md) | `internal/forge` and its two backends, `forge.Vocabulary`, and the explorer's clone pipeline |
| [`workspaces.md`](../docs/architecture/workspaces.md) | the `ws` file icons (`internal/ui/fileicon`) and the sync (`F`) |
| [`viewer.md`](../docs/architecture/viewer.md) | `internal/viewer` + `internal/ui/viewer` — kinds, displays, colouring, search, follow |
| [`scanning.md`](../docs/architecture/scanning.md) | Trivy, Gitleaks, plumber, `scan.Categorize`, the `:sec` inventory, the two scan caches |
| [`registries.md`](../docs/architecture/registries.md) | the registry/group model and the discovered-members cache |
| [`network.md`](../docs/architecture/network.md) | `internal/docker` and `internal/oci`, the netdiag view, `internal/ports`, `internal/netiface` |
| [`mcp.md`](../docs/architecture/mcp.md) | the read-only MCP server and `dk mcp` |
| [`ui-components.md`](../docs/architecture/ui-components.md) | `components.FooterMessage` and `internal/ui/datatable` |

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

## Dependencies

Key libraries (see `go.mod`):
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - Pre-built TUI components (table, textinput, spinner)
- `github.com/charmbracelet/lipgloss` - Styling
- `gitlab.com/gitlab-org/api/client-go` - GitLab API client
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality (maintained fork of go-ping/ping)
- `github.com/google/go-github/v68` - GitHub API client (§3.6)
- `github.com/libp2p/go-netroute` - which interface a destination leaves by, on all three platforms (§3.44)
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
