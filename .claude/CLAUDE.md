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
the current `main`, **under `.worktrees/` inside the repository**:

```bash
git fetch origin main
git worktree add -b <branch> .worktrees/<branch> origin/main
cd .worktrees/<branch>
```

Answering a question, reading code, running the app — none of that needs one.
Editing does.

**Under the repository, not beside it.** This reverses what this file said until
now, and the reason it gave was wrong. It claimed a worktree at `.worktrees/x`
would be compiled by `go build ./...` and `go test ./...`. It would not: the go
tool skips every directory whose name begins with `.` or `_`, so `.worktrees/`
is invisible to `./...`. Verified rather than assumed — a package of deliberately
invalid Go placed under `.worktrees/` builds clean and `go list ./...` does not
name it. The old argument holds for `worktrees/` without the dot, which is
presumably where it came from.

What decides it is the **sandbox mount**, and it cost a full debugging session to
find. A direct-mode sandbox mounts *this directory only*. A sibling worktree at
`../devdesk-<branch>` is therefore outside the mount, so its **files** are written
into the container's private layer and never reach the host — while `.git` is
inside the mount and stays shared. Refs agree, files diverge, and nothing warns:
the branch tip moves, the host's copy of those files does not, and `git status`
on the host reports the *stale* content as uncommitted modifications. An agent
and the person testing its work end up compiling different code while both
believe they are on the same branch. Under `.worktrees/`, host and sandbox see
one filesystem.

Two effects worth knowing, neither blocking:

- `.worktrees/` is in `.gitignore`; a worktree there is not a change to the tree.
- Claude Code discovers the nested `.claude/` of each worktree, so its skills and
  rules appear a second time, prefixed by the worktree path. Harmless, and the
  price of the directory being inside the repo.

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
(`local/<branch>:Go:…`). Nothing breaks; do not be surprised by the first search
being slow or by ids that do not match another worktree's.

**Clean up when the PR is merged**, from the main checkout:

```bash
git worktree remove .worktrees/<branch>
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
git worktree add -b <branch> .worktrees/<branch> origin/main   # work happens here
git push -u origin <branch>                     # via the mirror — forwarded to GitHub
gh pr create -R anthnel/devdesk --base main --head <branch>
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
# then, from the main checkout:
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see below
git worktree remove .worktrees/<branch>
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

A direct-mode sandbox mounts this exact repo directory, so both the commits and
the working files land where the host sees them — provided the worktree is under
`.worktrees/`, which is why that is the rule. There is nothing to transfer; the
host only has to push, because the sandbox has no `entire://` auth:

```bash
# From the host, once the sandbox reports "done and committed":
git push origin <branch>
gh pr create -R anthnel/devdesk --base main --head <branch> --fill
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see above
git worktree remove .worktrees/<branch>
```

**If a worktree was made at the old sibling path** (`../devdesk-<branch>`), the
host cannot see its files and `git worktree list` shows it `prunable`. The
commits are still in the shared `.git/objects`, so nothing is lost — but the
host's copy of those files is stale, and `git status` there reports that stale
content as uncommitted modifications, which reads as if work were pending when
the opposite is true. Recover by taking the branch tip:

```bash
git stash push -m "stale files from a sandbox sibling worktree"   # reversible
git worktree prune -v                  # drop the unreachable admin entry
git push origin <sha>:refs/heads/<branch>
```

**A worktree created from inside the sandbox registers a Linux path the
host's git can't resolve — even under `.worktrees/`.** `.worktrees/` shares
*files* between host and sandbox, but each side's git still writes its own
`.git/worktrees/<name>/gitdir` pointer, and the sandbox's git records a Linux
path (`/c/Users/...`). The host's `git worktree remove` on that same
directory then fails with `fatal: '<path>' is not a working tree` — Windows
git cannot resolve the pointer left behind. The costlier part: a plain `git
worktree prune` run on the host right after that failure deregisters the
worktree **silently** — git treats an unresolvable pointer as a worktree
that's gone — while leaving alone any sibling worktree whose path it *can*
read. If the branch was already merged the content is safe either way, but a
host-run `prune` while an agent is still working inside a sandbox-created
worktree would deregister that worktree out from under it, mid-task, with
nothing on either side raising a warning. The rule this implies: **whoever
creates a worktree removes it** — do not `git worktree remove` or `prune`
from the other side of the host/sandbox split.

#### Développer dans la sandbox, tester depuis l'hôte

C'est le mode normal, et le partage tombe bien : **le git est ce dont l'hôte
n'a pas besoin dans le worktree, le build est ce dont la sandbox n'a pas
besoin.** Mesuré dans un worktree dont le pointeur porte un chemin Linux,
depuis l'hôte :

| Depuis l'hôte, dans `.worktrees/<branche>/` | Résultat |
|---|---|
| n'importe quelle commande `git` | `fatal: not a git repository: (NULL)` |
| `go build ./...` | exit 0 |
| `go test ./internal/command/` | `ok … 0.239s` |
| `go build .` (avec stamping buildvcs) | exit 0 |
| `mise run build` | OK |

La casse est donc **entièrement** du côté git, pas seulement sur
`worktree remove` : Go n'a pas besoin du VCS, et `-buildvcs=auto` se dégrade
en silence plutôt que d'échouer quand il ne répond pas. Le checkout principal
n'est jamais affecté.

La répartition qui en découle :

1. **La sandbox crée le worktree** — c'est elle qui committera dedans, donc
   c'est elle qui doit garder un git fonctionnel.
2. **La sandbox édite et committe.** Tout le git se passe là.
3. **L'hôte construit, teste et lance l'application** dans ce même répertoire,
   les fichiers étant partagés par le montage. Aucune commande git ici — le
   `fatal` ci-dessus est attendu, il ne signale pas un dépôt cassé.
4. **Pour lire le diff depuis l'hôte, passer par le checkout principal** : les
   refs et `.git/objects` sont partagés, donc il voit tout ce que la sandbox a
   committé — `git log --oneline main..<branche>`, `git diff main...<branche>`.
5. **L'hôte pousse et merge depuis le checkout principal**, jamais depuis le
   worktree — c'est le host-relay ci-dessus.
6. **La sandbox retire son worktree.** Si elle n'existe plus, l'hôte peut
   supprimer les *fichiers* (ils sont dans le montage, contrairement au cas
   frère ci-dessus) puis `git worktree prune` — mais seulement quand aucune
   sandbox n'a de worktree vivant.

**Tester depuis l'hôte n'est pas un pis-aller.** DevDesk a besoin d'un vrai
terminal, du socket Docker de l'hôte, du gestionnaire de secrets de l'hôte
(`internal/credentials`), de `~/.devdesk/` et de binder des ports : rien de
tout cela n'est représentatif dans la sandbox. Ce que la sandbox fait mieux,
c'est écrire du code.

**Le sens inverse est interdit** : l'hôte ne crée pas le worktree pour que la
sandbox y travaille. Par symétrie, le `C:/Users/...` qu'écrit git Windows
n'est pas résolvable sous Linux — la sandbox perdrait le git, dont elle a
besoin pour committer. (Cette direction-là n'a pas été testée ; c'est le même
mécanisme lu à l'envers.)

**A follow-up commit on the same branch, after the first one already got
squash-merged, will conflict on a plain `git merge`** — squashing rewrites
history, so `git merge origin/main` computes the wrong common ancestor
(the branch's *own* pre-squash parent) and re-diffs files that already
landed, even when nothing actually conflicts in substance. Don't hand-edit
through that; cherry-pick the new commit straight onto current `origin/main`
instead — its real parent already matches what's on `main`, so it applies
clean:

```bash
git worktree add -b <tmp-branch> .worktrees/<tmp-branch> <new-sha>
cd .worktrees/<tmp-branch>
git reset --hard origin/main
git cherry-pick <new-sha>              # clean apply — verify before trusting this
go build ./... && go test ./<touched-packages>/...
git push origin HEAD:refs/heads/<new-branch-name>
# gh pr create / gh pr merge as above, then from the main checkout:
git worktree remove .worktrees/<tmp-branch>
```

### Publier une version — release-please, puis goreleaser

**Une release se fait en deux merges, et c'est le point.** `release-please`
surveille `main` et tient ouverte une **pull request de release** : il lit les
conventional commits depuis le dernier tag, décide la version et écrit le
`CHANGELOG.md`. Merger cette PR crée le tag et la GitHub Release ; `goreleaser`
construit alors les six binaires (linux, darwin, windows × amd64, arm64) et les
attache à cette Release.

```bash
# rien à lancer : la PR de release s'ouvre et se met à jour toute seule
gh pr list -R anthnel/devdesk --label "autorelease: pending"
gh pr merge -R anthnel/devdesk <n> --squash   # ← c'est ce merge qui publie
```

**Pourquoi pas semantic-release**, qui ferait la même chose en un seul merge :
il pousse le tag directement sur `main`, et ce dépôt l'interdit — le mirror
Entire rejette ce push, et ce refus est la *seule* protection que `main` ait,
puisque la branch protection est indisponible sur ce plan (le 403 plus haut). Un
merge de plus par release est le prix de cette règle.

**Le mirror réplique les tags, vérifié le 2026-09-06.** Le tag est posé par
GitHub Actions, donc il naît sur GitHub et non via le mirror — mais
`git fetch origin --tags` le ramène, comme il ramène un squash-merge fait par
`gh`. C'était la seule inconnue de la chaîne, et elle est levée : rien n'oblige
à poser un tag à la main.

| Fichier | Rôle |
|---|---|
| `release-please-config.json` | le type (`go`), les sections du CHANGELOG |
| `.release-please-manifest.json` | la version courante — **écrite par le robot**, jamais à la main |
| `.goreleaser.yaml` | les six cibles, les `-ldflags`, `mode: append` |
| `.github/workflows/release.yml` | les deux jobs, enchaînés par `release_created` |

**Un réglage de dépôt qu'aucun workflow ne peut se donner** : *Settings →
Actions → Allow GitHub Actions to create and approve pull requests*. Sans lui le
premier run échoue sur la PR qu'il ne peut pas ouvrir, et le message ne dit pas
que c'est ça. **Déjà actif ici** — `gh api repos/anthnel/devdesk/actions/permissions/workflow`
répond `can_approve_pull_request_reviews: true`, vérifié le 2026-09-06. Le même
appel montre `default_workflow_permissions: read`, ce qui n'est pas un problème :
c'est le défaut quand un workflow ne dit rien, et `release.yml` déclare les
siennes.

**La version du binaire vient des `-ldflags`**, pas d'un fichier committé
(§3.62). `release-please` sait écrire dans un fichier Go (`versionFile`) et on
ne s'en sert pas : ce serait une seconde source de vérité pour une réponse que
le build donne déjà, et un binaire de dev afficherait alors le numéro de la
dernière release plutôt que `dev`.

**Avant de pousser un changement à la chaîne**, `mise run release-check` valide
la configuration et `mise run release-snapshot` construit les six cibles sans
rien publier — c'est le seul moyen de découvrir qu'une cible est cassée avant
qu'un tag ne soit posé.

**La première release listera toute l'histoire** (66 `feat:` et 50 `fix:` au
2026-09-06). C'est voulu — un premier CHANGELOG vide serait pire — et la PR est
éditable avant le merge. Pour partir à `1.0.0` plutôt qu'à `0.1.0`, un
`"release-as": "1.0.0"` ponctuel dans la config, retiré ensuite.

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
| [`mcp.md`](../docs/architecture/mcp.md) | the MCP server the TUI serves over HTTP |
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
