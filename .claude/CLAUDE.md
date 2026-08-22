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
    ├── gitlab-explorer - GitLab project/group browser + multi-select clone
    ├── workspaces      - Local workspace management + git metadata
    ├── security        - Trivy + Gitleaks scanner with multi-tab results
    ├── containers      - Docker container list + live metrics
    ├── oci-resources   - OCI resource list, scan, launch containers, network inspection
    ├── netdiag         - Network diagnostics (Docker-based tools) + real-time port monitor
    ├── configuration   - Every scalar setting in the current context
    └── viewer          - One document, read-only (router-only: no `:viewer`)
```

### View Switching & Command Mode

Press `ctrl+p` to enter command mode, then type:
- `dashboard` or `d` - Switch to dashboard view
- `status` or `s` - Switch to status view
- `gitlab-auth` or `gla` - Switch to GitLab auth view
- `gitlab-explorer` or `gle` - Switch to GitLab explorer view
- `workspaces` or `w` - Switch to workspaces view
- `security` or `sec` - Switch to security scanner view
- `containers`, `cont` or `ct` - Switch to containers view
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

**`ctrl+p` is the way in, and `:` is the convenience.** `ctrl+p` is handled
before any `InEditMode()` check, so no text field can claim it; a bare `:` opens
the command line too, but only when nothing is focused — inside a field it is an
ordinary character, which a value like `https://trivy-server:4954` needs.

It is not `ctrl+:` — `:` is 0x3A, outside the 0x40-0x5F range a terminal
encodes for Ctrl, so that combination never arrives. It was `alt+:` until
§3.26, which has the mirror defect: on Terminal.app and iTerm2, Option is not
Meta unless the user turns it on, so `Option+Shift+;` emits a literal character
and the key never arrives either. That was worse than a key that plainly does
not exist, because every view advertised it. The vocabulary and the reasoning
live in `internal/ui/keymap`.

**Important:** The `FormView` interface (`InEditMode()`) prevents command mode activation when forms are active. Views with active forms must implement this interface.

### The keyboard — `internal/ui/keymap`

**Four namespaces, and the whole point is that a test can check them.** The
package declares the vocabulary; three tests parse every `.go` under
`internal/app` and `internal/ui`, find the switches that decide keys, and fail
naming file, line and rule. A convention nothing verifies is what produced the
16 collisions §3.26 relieved.

| | Meaning | Checked by |
|---|---|---|
| **UPPERCASE** | an action, global to the application | `TestNoViewBindsAnUndeclaredUppercaseKey` |
| lowercase | a filter or display toggle, local but **declared** per surface | `TestEveryLowercaseBindingIsDeclared` |
| `Ctrl` | only `ctrl+c`, `ctrl+r`, `ctrl+p` survive | `TestOnlyThreeCtrlCombinationsSurvive` |
| a modal's `y`/`n` | a *mode*, not a case — it takes every key before the view | declared in `modalKeys` |

`Shift` carries the actions because the other families are amputated: `Ctrl`
encodes only ASCII 0x40–0x5F and the tty confiscates four of them, `Alt` is not
Meta on macOS by default, and `Ctrl+Shift` is indistinguable from `Ctrl` without
a keyboard protocol bubbletea v1 does not enable. The reasoning lives in the
package doc so it does not have to be rediscovered.

**No bare letter is navigation.** `h j k l g G` are gone application-wide — from
`datatable`, from every view, from the shared modals, and from
`bubbles/viewport`'s own default `KeyMap` in the help overlay, which was
scrolling on letters behind the application's back.

**Free letters are declared too** (`J Q Z`). A new action takes one of them;
it does not invent a key, and `TestFreeLettersAreActuallyFree` stops the list
going stale.

Three actions were dissolved rather than given a letter, and each fixed a defect
on the way out:

| Gone | Where it went |
|---|---|
| `r` — restart a container | a button in `K`'s modal. Both used to act with **no confirmation**, and with caps lock on a scrolling `k` stopped the selected container |
| `ctrl+a` — purge then scan all | a checkbox in `A`'s modal. The pair differed by a modifier alone, with nothing in their shape saying which one destroyed data |
| `ctrl+e` — launch a container | `N`. It is "create a resource from the selected row", and nothing else is created from the Images tab |

`i` moved to `enter` for the same reason — inspect was already `enter` in
OCI/Networks — which is what freed `I` for the dashboard's issues.

**`t`/`T` and `s`/`S` became one key and a setting.** `app.terminal_new_window`
decides whether `T` suspends the TUI or opens a window. The capability belongs
to the environment rather than to the moment: there is no window to open under
WSL or through SSH, and a key that is inert on two setups out of three is worse
than a setting that is simply off there.

**`HeaderView` is all four methods or none.** The router probes for it with a
type assertion and falls back silently, so a view supplying `GetTitle` and
`GetShortcuts` but not `GetIcon` and `GetHeaderInfo` satisfies nothing and
renders an empty viewport title — with nothing to say so.
`command.AllViewNames()` and `TestEveryViewSuppliesItsHeaderAndHelp` turn that
into a contract every view is checked against.

There are **three** name lists, and each answers a different question:

| | Answers | Read by |
|---|---|---|
| `ViewNames()` | what can a user type | completion, `app.default_view` |
| `AllViewNames()` | what can appear in the viewport | the router's contract tests |
| `FullNames()` | every command, views and actions | completion |

`FullNames()` also carries the action commands (`context`, `quit`), which is
right for completion and wrong for anything meaning "a view" — the configuration
view's `default_view` field offered `quit` as a landing view until they were
separated. `AllViewNames()` adds the **router-only** views: `viewer` is opened on
another view's request and `:viewer` resolves to nothing, but it renders in the
same viewport as the rest and fails in the same silence, so the contract has to
reach it.

### Multi-Context Configuration

The app supports multiple configuration contexts (e.g., work, personal, client-A):
- Contexts are stored in `~/.devdesk/contexts/<name>/config.yaml`
- Current context is tracked in `~/.devdesk/current-context`
- Each context has isolated GitLab credentials via Git Credential Manager
- Context switching reinitializes all views with new config

### Configuration System

Config loaded from `~/.devdesk/config.yaml` with schema defined in `internal/config/config.go`:
- `App` - Global settings (theme, default view, workspaces dir, `show_hidden_files`, `secret_backend`)
- `Status` - Monitoring settings (refresh interval, components)
- `Forge` - the platform this context targets: `type`, URL and clone settings
- `Registry` - OCI registry configuration (see Registry model below)
- `Scan` - Security scanning (Trivy, Gitleaks)
- `Network` - what the netdiag view runs on: the tool image, and the dials
  `internal/netcheck` used to hardcode

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

**`network:` was `docker:`, and the rename is migrated rather than announced.**
The one key it held, `network_tool_image`, was never a Docker setting — it names
the container the route traces and the ports table run in — and the four other
tabs of the configuration view are each named after the section they write, so
renaming the tab without the key would have left the one section about to grow
as the only one whose name says nothing about where its values land.
`applyDefaults` carries `docker.network_tool_image` into `network.tool_image`
**before** filling in defaults, then clears it so the key leaves the file on the
next save (the precedent is `RegistryItem.AuthEnabled`). The order is the whole
of it: `yaml.Unmarshal` is not strict here, so an un-migrated block is dropped in
silence and a user pointing at their own mirror would find their traces pulling
from Docker Hub. `TestANetworkToolImageSurvivesTheRename` is what makes that
checkable rather than commented.

**No secret goes in this file.** `GitLabConfig` has no `Token` and
`RegistryConfig` has no `Password`; both live in the host secret store (see
Credentials Management). Do not add a secret-bearing field back — the schema is
what makes the guarantee checkable.

Config is injected into views at creation. Use `config.Save()` to persist changes.

### Configuration view — `internal/ui/configuration`

Edits every **scalar** setting a context carries, in five tabs (`app`, `gitlab`,
`scan`, `network`, `status`). Lists stay where they are consulted: monitors keep
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

Two settings are special-cased, matched by label:

- **Theme** applies as it is cycled, not on blur — otherwise the user chooses
  blind.
- **Secret backend** is confirmed when focus *leaves* the field, not on every
  `←→`, and **nothing is migrated between backends**. §3.9 removed the option
  that wrote a token to two stores at once; copying one here would rebuild it.
  Declining restores the previous value.

**`forge.url` belongs to this view, not to the auth view.** Both used to write
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
forge-backed view reads `sharedState` rather than holding its own answer. A
view resetting only its own fields is what D28 was: logging out left the client
and the user in place, so the explorer kept browsing and the header kept naming
a signed-out user.

**`clearAuthenticated` is the only place a session is torn down.** Three sites
used to write the same three or four fields by hand — the secret-backend change,
the GitLab-URL change and the context switch — which is the shape D28 came in.
They call it now.

**`IsAuthenticated` is the flag, and `Forge` is what you call.** The client used
to be both: `GitLabClient != nil` decided "is the user logged in" at three sites
while `IsAuthenticated` sat beside them saying the same thing. `CurrentUser` is
a value rather than a pointer for the same reason — a second way to ask a
question is how two answers come to disagree. The header shows the user when
there **is** a username, not when the flag is set, so a session whose user could
not be read does not print a bare `@`.

Clearing `sharedState` does not empty a table a view already loaded, so a
session ending also drops the views — all but the one on screen that reported
it.


`internal/shared/state.go` holds cross-view data injected at view creation:
- `Secrets`, `SecretNotices` — the context's secret store and what the migration off plaintext reported
- `Forge`, `IsAuthenticated`, `CurrentUser` — the forge session (§3.6)
- `CachedGroups`, `CachedProjects` — GitLab data cache
- `GitLabStats`, `DockerStats`, `OCIStats` — Dashboard counters. `GitLabStats`
  is a `*forge.DashboardStats`, whose five counters are each a `*int`: `nil`
  means nobody could read it, and the dashboard prints `-` rather than the `0`
  that read as "you have none" (D52)
- `ServiceStatus`, `ServiceComponents` — Status monitoring results
- `WorkspaceCount`, `Tools []ToolInfo` — Tool availability (Trivy, Gitleaks, Docker)

### The forge abstraction — `internal/forge`

What DevDesk asks of a code-hosting platform, so a context can target GitLab or
GitHub — exactly one, never two (§3.6). `internal/forge/gitlab` is the only
package that knows go-gitlab exists.

Five decisions hold it together, each forced by what the code consumes:

| | |
|---|---|
| **Identity is opaque, the path is not** | `ID` addresses an object with the backend and means nothing outside — GitLab needs a number for `ParentID`, GitHub addresses by owner. `Path` is what the clone URL, the display and GitLab's renamed-path deletion are built from. The type is a string so arithmetic on it cannot be written; only the backend reads it back |
| **A namespace and a repository are different types** | only a namespace has children, only a repository has a CI status and a scheduled deletion. "Drill into a repository" is unexpressible rather than forbidden by review |
| **Decoration is an option** (`BrowseOptions.Decorated`) | the role and CI status cost two requests **per repository** on GitLab. The explorer asks; the clone's walk does not, which is what keeps a two-hundred-repository group from costing 400 calls for a badge nobody reads |
| **`Shape` is declared, never sniffed** | nesting depth, the visibility set, whether a delete can be permanent. `CanNestUnder` reads the "unbounded" sentinel in one place — the obvious comparison refuses the *root* namespace on GitHub, where the maximum is 1 |
| **`DashboardStats` counters are `*int`** | five independent requests, any of which fails on its own. `nil` means nobody looked (D52) |

Two properties come with the seam: **the backend paginates** and no caller ever
sees a page (D34), and **every call takes a context**, which is what the clone's
cancellable discovery needs and `context.Background()` everywhere prevented.

`CurrentUser` is **memoised** on the backend. Every decorated listing needs the
caller's id for its role lookups, and asking the host each time would add a
request per listing that the pre-abstraction code did not make — it took the id
from a session the view was already holding. Who a token belongs to does not
change for the life of a session.

**A decoration that fails does not fail the listing; a listing that fails is
never an empty list.** Both directions have a test. It is one rule seen from two
sides: a column short beats an empty explorer, and an empty explorer must never
mean "this group contains nothing".

**`internal/forge/gitlab/auth.go` opens sessions.** Nothing about the credential
store is GitLab-shaped, but building a session means building a backend. When a
second backend exists, choosing between them is a switch on the context's
declared forge, and this is one of the two places it will live.

Worth knowing: **go-gitlab retries 5xx** with an exponential backoff — a test
serving 500 took 35 seconds, measured. Not a regression (it is the SDK's
default), but an unreachable instance makes the user wait behind a spinner.

### Cross-View Communication

Key messages in `internal/app/messages.go`:
- `SwitchViewMsg` — navigate to another view
- `SelectionRequestMsg` / `SelectionResultMsg` — selection mode (e.g., workspaces opened from security view to pick a repo)
- `ImageScanResultLoadedMsg` / `WorkspaceScanResultLoadedMsg` — cached results ready

### The explorer clone

`C` in the explorer opens a **selection mode** over the same tree, and `enter`
starts a **pipeline** that discovers and clones at once (§3.16). It replaced a
single `Cmd` covering a whole subtree behind a modal reading `"Pulling..."` —
several minutes indistinguishable from a freeze on a large group.

The line that holds it together: **the explorer creates what does not exist,
workspaces reconciles what does.** A repository already on disk is skipped
untouched, so the explorer never needs to know what a dirty working tree is and
the per-row states collapse to five — queued, cloning, cloned, already there,
failed. Updating an existing clone is §3.17's `sync`, in workspaces.

| Mode | Screen | Keys |
|---|---|---|
| `ModeSelecting` | the tree, with a checkbox on the Type cell | `space` ticks, `←→` drill, `enter` confirms, `esc` cancels |
| `ModeCloning` | a flat list, one row per repository | `esc` cancels, then closes |

**The selection is roots plus exclusions, never a list of repositories**
(`selection.go`). A positive list cannot be built when a group is ticked without
enumerating its children — the full API walk, run at selection time, which is
the freeze moved one screen earlier. "This group, minus these" needs to know
nothing about what the group contains, so a group nobody has expanded can still
be ticked, displayed with the right tri-state, and walked. It is the only
representation compatible with discovering as you clone.

The **nodes** are kept separately, in `Model.selectionNodes`: the walk has to
start from one, and a root ticked three levels down is no longer on screen once
the user has come back up. The selection itself stays paths-only, which is what
lets it answer for paths nobody has fetched.

**The check state travels on a row type.** `datatable` columns are built once in
`New` and close over nothing, so the table moved from `Model[*TreeNode]` to
`Model[explorerRow]` (`row.go`) — the `imageRow` pattern. The state must **not**
move into `datatable`: `SetItems` replaces the items on every drill-down, while
the selection spans levels the table has never shown. `RenderCheckboxTri` styles
its output and so cannot go in a cell; `checkboxIcon` is the glyph without it
(Rule 122).

The checkbox **rides on the Type cell** rather than taking a column of its own.
A column would cost four cells on every screen to say nothing on all but one of
them, and at 80 columns the explorer has none to spare.

**Two cancellation scopes, and the distinction is the point** (`pipeline.go`).
`cloneRun.cancel` is a `context.CancelFunc` covering **discovery only** — HTTP
reads, which cancel safely. A `git clone` is never interrupted: a context that
kills one leaves half a repository on disk, which is exactly what this avoids.
So `esc` cancels the walk and the scheduler stops issuing work, the running
clones are awaited, and the footer says `Cancelling — 3 clones finishing`. A
second `esc` must not force.

`cloneWalkFailed` is a kind of its own: a group that cannot be listed gets a
failed row naming the **group**, because the repositories under it were never
discovered and no other row can stand for them.

The **failures are named when the run ends**, in the footer and the log. The
list is discarded on `esc` and workspaces records only the successes — a clone
that failed wrote nothing, so that report is the only one there will be.

`gitlab.pull.include_archived` is read by `listGroupChildren` and **only by the
clone**: browsing lists everything the forge has.

**A clone may never prompt, and `gitlab.Clone` is where that is enforced.**
Sending git's streams to the null device does not prevent a credential prompt —
it prevents git asking *itself*, after which the **credential helper** takes
over, and a helper is a separate process. Git Credential Manager writes
`info: please complete authentication in your browser` to the console directly,
over the top of the rendered frame, then waits. Bubble Tea cannot recover a
frame something else has written into: two frames end up visible at once, and
the row spins with nothing on screen saying why. Observed, not theorised.

So `cloneEnv` shuts every interactive path — `GIT_TERMINAL_PROMPT=0`,
`GCM_INTERACTIVE=never`, both askpass hooks, ssh in `BatchMode` — and the token
DevDesk already holds is passed as `http.extraHeader` **through the
environment**: argv is readable from the process list, and the `user:token@host`
URL form is written into every cloned repository's `.git/config` and stays
there. A stalled transfer is bounded by `http.lowSpeedLimit`/`lowSpeedTime`
rather than by killing the process, so git cleans up after itself and decision
12 still holds.

The consequence to keep in mind: with the helper out of the loop, a context
whose stored token is missing or under-scoped **fails** rather than falling back
to a browser. That is the intended trade — a failed row naming git's reason
beats a spinner that never resolves — but it makes the token the only way in.

### The workspaces sync

`F` fetches a repository and fast-forwards it (§3.17). It is the other half of
the line §3.16 drew: **the explorer creates what does not exist, workspaces
reconciles what does.**

**It has no screen of its own, and that is the whole difference from the clone.**
The clone opens a list because its rows do not exist yet — discovery invents
them. Here every repository is already a row the user is looking at, so a second
list would print the same names twice. Sync decorates instead: the spinner goes
in the Git Status cell, exactly as a scan's goes in Scanned, and the counts tell
the truth again when it lands. The progress and the summary are one footer line
rendered from the run (`syncStatusLine`) rather than assigned to `footerInfo` —
a batch outlives the three seconds a footer message gets (Rule 128).

**The target follows `S`'s rule rather than adding a selection mode**: a
git repository syncs itself, a plain directory syncs every repository nested
under it, anything else does nothing. Two actions with one targeting rule is one
thing to learn; the shortcuts appear and disappear together for the same reason.
There is deliberately no sync-all: at the root the user syncs each top-level
directory, and a second key for it is not worth `Shift+S`'s collision with
Rule 111's sort menu.

**`git.Sync` refuses more than it does**, and each refusal is the point:

| Situation | What happens |
|---|---|
| behind, clean, no local commits | fast-forwarded |
| nothing to pull | up to date — unpushed commits do not change that, sync is the pull direction |
| local commits the remote lacks | **skipped**, `diverged — N commits ahead` |
| uncommitted changes, untracked included | **skipped**, `uncommitted changes` |
| detached HEAD, or no upstream | **skipped**, named |
| the remote could not be reached | **failed** — the difference from a skip is whether the repository is as its owner left it, or DevDesk could not find out |

No merge commit, no rebase, no stash, and **never a push**. A divergence is a
decision about someone's unpublished work, and a tool that guesses at it
destroys hours in a keystroke that cannot be undone.

**The fetch runs first and always, whatever the tree looks like.** That is D35:
the "unpulled" count comes from `@{u}`, the *local* tracking ref, and nothing in
DevDesk moved it — so it read `0` on a repository forty commits behind. A sync
deciding from that number would be reconciling against an answer it had not
checked. The consequence worth keeping: a repository sync **declines** still
comes out of it knowing how far behind it is, because the fetch happened either
way. `readGitStatus` re-reads the repository on every completion, refusal
included, and `applyGitStatus` puts the fresh counts on the row.

**A repository is never scanned and synced at once, and neither runs on one
being deleted.** A scan reads the working tree while a fast-forward rewrites
it; the visible result is a report describing a tree that no longer exists. A
delete takes the tree away from under either of them. `Model.busy` is the one
predicate all three consult — `A`'s purge included, or a syncing row's counts
are blanked with nothing on the way to replace them — and `deletingPaths` is
the third map beside `scanningPaths` and `syncingPaths`, kept separate for the
same reason: the view says *which* operation holds the row.

**`D` is guarded twice, and the second time is not redundant.** `startDelete`
refuses before the confirmation opens, because asking a question and then
declining the answer wastes the user's time; `handleConfirmDelete` asks again
because a batch sync marks its repositories from a `Cmd`, so one can take the
path while the modal is on screen — and that handler is where the irreversible
call is issued. The marker is cleared on **every** outcome, failure included:
`deleteEntry` builds its message at one site so the path cannot be omitted from
the failure, which is what would strand the row as busy for the life of the
view. Before this, a second `D` fired a second `os.RemoveAll` and reported its
failure to the user for a deletion that had in fact succeeded (§3.23).

**The token goes to the configured GitLab host and nowhere else.**
`tokenForRemote` compares the repository's remote host with `gitlab.url`'s and
returns `""` otherwise. This is not tidiness: the workspaces view holds whatever
the user has cloned — GitHub, a customer's Gitea, a path on a share — and
`http.extraHeader` would put DevDesk's personal access token on the wire to any
of them. It is also why `internal/git` exists as a package separate from
`internal/forge/gitlab`: deciding a credential's destination in a package named
after one forge invites the default that must not exist. A foreign remote never even
reaches the loader, so the secret store is not read for it.

The keyring is read **once per batch** (`tokenLoader`, a `sync.Once` closure) and
on a command's goroutine, not in `Update` — same reasoning as the clone
pipeline's. `gitlab.pull.parallel_jobs` bounds both: one number meaning "how many
git network operations at once" beats two the user has to keep in step.

### The document viewer — `internal/viewer` + `internal/ui/viewer`

One document, read-only. A **destination with three producers**, the shape the
security view already has:

| Producer | Key | Source | Document |
|---|---|---|---|
| `workspaces` | `enter` on a file | `viewer.FileSource` | detected |
| `containers` | `enter` | `inspectSource` | JSON |
| `containers` | `L` | `logsSource` | log |

Two axes, and they are the whole interface: **display** (`f`) and **highlight**
(`c`). "Plain text" is text with the colour off; a display of its own for it
would be one screen reachable two ways — what §3.9 removed from the secret
backend and what took the `:theme` command with it.

**`f` is one axis with one meaning: the document as it is, against the one view
its kind derives from it.** A kind derives at most one — a tree when
`Kind.Structured()`, a rendered form when `Kind.Renderable()`, never both, and
`TestNoKindHasTwoDerivedDisplays` says so. That is what keeps the toggle binary
at every moment even though `display` has three values, and why `Model.derived()`
is one function rather than a switch at each of the three call sites that need
it: `f`, the opening display and `GetShortcuts` have to give the same answer or
the view offers a display it will then refuse to show (Rule 130).

| Kind | `f` switches between | Derived by |
|---|---|---|
| JSON, XML | the tree and the text | `parseJSON` / `parseXML` |
| Markdown | the rendered form and the raw source | `RenderMarkdown` |
| everything else | nothing — `f` is hidden (Rule 130) | — |

**A rendered Markdown is the same token stream with its markers removed**, and
nothing more. Every marker `RenderMarkdown` takes off is one chroma *already
identified* — a heading, a strong, an emphasis, a strikethrough, a bullet, a
quote prefix, a fence. Nothing there parses, and nothing decides from what a
character looks like: that is why **links, tables and thematic breaks survive
untouched**. A link's `[` and `]` arrive as bare `Text`, indistinguishable from a
bracket in prose, so rebuilding one would be exactly the guesswork this package
refuses everywhere else — the link text and its URL are coloured apart instead,
which is most of what rendering it would have bought.

The one invariant it breaks is Tokenize's: **the concatenation no longer
reproduces the input**. It is the point. Everything downstream copes because it
reads the tokens rather than the document — `splitTokenLines` rebuilds each
line's plain text from them, so the search filters and highlights what is
actually on screen. The raw display is still the source to the character, which
is what makes the omissions above limits rather than losses.

`c` stays orthogonal to all of it: turning the colour off in a rendered document
does **not** put the asterisks back. The rendering is a display, the colouring is
a colouring, and a `c` that revealed markers would be the second way to reach one
screen that the paragraph above rules out.

**A document carries its `Source`, not its bytes.** Reload, follow and the
timestamps toggle are questions for the origin. Three **single-method** optional
interfaces, probed by type assertion like `FooterView`:

| Interface | Unlocks | Implemented by |
|---|---|---|
| `Timestamped` | `t` | `logsSource` |
| `Followable` | `F` | `logsSource` |
| `Pageable` | `V` | `logsSource` |

One method each on purpose: `HeaderView`'s warning is about a view supplying two
of four and satisfying none in silence — a one-method interface has no
half-satisfied state. `WithTimestamps` returns a *new* source rather than
mutating one, so nothing is shared with a command in flight (Rule 110).

**`F` follows in the pane, and `Followable` returns an interval rather than a
command.** It used to return an `*exec.Cmd` run through `tea.ExecProcess`, on
the argument that `docker logs -f` already does this and re-implementing it
against a viewport would be re-implementing `less +F` badly. The argument was
true and beside the point: **the only way out of `docker logs -f` is ctrl+c, and
a suspended TUI does not intercept it** — so leaving the follow killed DevDesk
outright, and gave the terminal back in whatever mode the child had left it,
after which keys stopped answering. Observed, not theorised. A capability whose
only exit kills the application is not one.

Following is therefore `ctrl+r` on a timer: the same `loadCmd`, the same
`DocumentLoadedMsg`, the same handler, plus a `followTickMsg` — the ports tab's
shape. Three things follow from it, each with a test:

- **The generation ends a loop, not the flag.** `tea.Tick` blocks its whole
  interval, so a tick scheduled before `F` stopped still arrives; restarting
  before it lands would leave two loops reading for the life of the view.
  `stopFollowing` bumps `followGen` and is the one place following is turned
  off — `esc` included, since the router keeps this view and a loop left behind
  it would shell out every interval for a document nobody is looking at.
- **A followed document lands at the bottom**, where the new lines are. Every
  other arrival lands at the top, because a first load, a reload and a
  timestamps toggle all mean "here is the document".
- **`m.loading` stays false on a follow read.** The footer spinner belongs to a
  load the user waits on, and one blinking every two seconds reads as a fault in
  the thing that is working. `Following — F to stop` is what says the pane is
  live, derived per frame rather than posted (Rule 128).

The trade is stated rather than discovered: this polls, so a line can wait up to
one interval and a burst longer than `logsTail` is missed between two reads. `V`
still streams, and `less` is left with `q` rather than ctrl+c.

**A log line with no level inherits the one above it.** This is what the filter
rests on: a stack trace is a dozen unlevelled lines, and `≥ warn` swallowing them
destroys exactly what the log was opened for. The chain breaks on a blank line —
otherwise one ERROR colours half the file. A line before any level is
`LevelUnknown` and passes **every** filter. The cost, stated rather than
discovered: a genuinely unrelated unlevelled line after an INFO goes with it.

`v` cycles a **minimum** (`all → trace → debug → info → warn → error`), not four
toggles: that is what "verbosity" means and log levels are monotone. One
`FilterBar` token (Rule 136), gone at `all`.

**Six kinds are declared, never sniffed**: `KindLog`, `KindYAML`, `KindTOML`,
`KindMarkdown`, `KindDockerfile` and `KindShell`. "This looks like a log" is not
decidable, and neither is "this looks like YAML" — a file opening with `---` is
Markdown front matter as often as a YAML stream, and a `[section]` line is prose
in half the files that hold one. The registry `provider` field is the precedent.
JSON and XML keep a content sniff, but only for a file with **no extension**: a
`.md` opening with a tag is not a broken XML document, and saying so would be
noise. Hence `detection.Declared` — a parse failure is reported only when the
*name* claimed the kind.

**A name declares in two ways, and the basename is read first.** `Dockerfile`
carries no extension at all, and `Dockerfile.dev` carries `.dev` — which is in no
table, so an extension-first lookup would settle it as plain text before the name
was ever read. Hence `basenameKinds` and the `dockerfile.` prefix ahead of
`extensionKinds`. A dotfile goes the other way round and lands in the extension
table, because Go's `filepath.Ext(".bashrc")` returns the whole name; which table
holds it is an implementation detail, that it is found is not.

**The shebang is the one exception, and it is written down as one** — in
`sniff()`, in `TestAShebangDeclaresAShellScript`, and here. `#!/usr/bin/env bash`
is not an indication of what a file resembles: it is the file naming the
interpreter it is to be run by, which is an extension's standing minus the
extension. It applies only where `sniff` already did — a file with no extension
at all — and only to a short list of shells, because answering a
`#!/usr/bin/env python` with the bash lexer would colour a Python file wrong,
which is worse than leaving it plain since it looks deliberate.

**YAML, TOML, Dockerfiles and shell scripts are coloured, and have no tree.**
`Structured()` excludes YAML and TOML by decision, not by oversight: both have a
structure, but neither has a parser *here* that preserves the file's order, and
order is content (below). `yaml.v3` could do it — `yaml.Node` keeps order and
comments, and it is already a dependency — while TOML would cost another one.
Until that is worth doing they are text with the colour on, and `f` is hidden for
them (Rule 130). A Dockerfile and a shell script are not excluded from anything:
they are programs, and a program is read in its own order.

The class mapping is **read off the lexers**, not guessed, and each kind added
has said something the guess would have missed:

| Kind | What the reading found |
|---|---|
| YAML | nothing to do — keys arrive as `NameTag`, scalars as `Literal`, `true`/`null` as `KeywordConstant` |
| TOML | every key, table headers included, arrives as `NameOther`, which fell through to `ClassText` — a TOML coloured everywhere except the thing worth colouring |
| Markdown | the whole `Generic` category was unmapped, so a Markdown arrived very nearly colourless |
| Dockerfile, shell | instructions and `if`/`fi` arrive as a **bare `Keyword`**, and a shell variable as `NameVariable`, which fell through to ordinary text |

Neither the JSON nor the XML lexer emits `NameOther`, so the mapping is not
guarded on the kind; the classification tests are what keep that true. No
dependency and no binary growth for any of it: chroma embeds every lexer already,
which is what the +4.0 MB below bought.

**`Keyword` and `KeywordConstant` are separated, and the test is equality.**
`KeywordConstant` is `true`, `false`, `null` — a value, so `ClassLiteral`; every
other keyword is a word of the language, so `ClassKeyword`. The split could not
disturb what already worked, and that is checked rather than hoped: the JSON,
YAML, TOML and XML lexers emit `KeywordConstant` and never a bare `Keyword`, and
`TestAKeywordConstantIsStillALiteral` is what keeps it so. Written the obvious
way it would have been wrong — chroma implements a sub-category as
`t/100 == other/100`, so `InSubCategory(KeywordConstant)` answers **true for every
keyword there is** and silently swallows the other branch. `ColorSyntaxKeyword`
aliases `ColorPrimary`, the literal's colour: invisible in the default theme, and
a name for a theme that wants to separate them — the argument already made for
`ColorFooterError` against `ColorError`.

**Five classes carry a text attribute rather than a colour**, and Markdown is why:
once the rendered display has taken the `**` and the `~~` away, weight and
strikethrough are the only thing left saying two runs were ever different, and a
hue would say it less — a bold word is bold in any theme. `ClassStrong`,
`ClassEmph` and `ClassStrike` therefore keep `ColorText` and set `Bold`, `Italic`
and `Strikethrough`; `ClassHeading` takes `ColorSyntaxHeading` and bold both.
They still set a background like every other style (Rule 115), which is exactly
what an attribute-only style invites you to forget —
`TestEveryRenderedStyleCarriesTheAppBackground` asks each of them directly.

**`Tokenize` coalesces adjacent runs of one class**, and that is not tidiness. The
markdown lexer ends its inline rules with a catch-all single-character
alternative, so ordinary prose comes back **one token per character**: at the
5 MiB `MaxSize` ceiling that is millions of `Token` values for a paragraph that
is a handful of runs. `TestAdjacentRunsOfOneClassAreCoalesced` counts them. It
preserves the concatenation invariant — only the boundaries move — and drops the
empty runs several lexers emit between groups. `Match` is deliberately not
consulted: `Tokenize` never sets it, `MarkMatches` does, after the filter.

**Order is content.** Both parsers read a token stream (`json.Decoder.Token`,
`xml.Decoder.Token`), never a decoded value: `map[string]any` loses the file's
order. That is also why the tree's columns declare **neither `Less` nor
`Search`** — sorting would destroy what the parser took care to keep, and a text
filter would hide parents and orphan their children. `.` and `/` are unbound in
the tree, and Rule 138 says so by omission.

**The tree is a `datatable`, the text pane a `viewport`.** The tree qualifies for
a reason worth stating: **a tree cell carries exactly one syntax class**, so one
`Style` per cell is enough — `datatable` cannot express several colours in one
cell and never has to here. The text pane is not a table, so Rule 122 does not
apply; Rule 115 does, and every token style sets its background.

**Wrapping happens on tokens, never on coloured text.** A styled line cannot be
cut: the measure counts an escape's bytes as width and the cut lands inside the
sequence — Rule 122's hazard, one layer up. `docLine` keeps a line as spans,
`wrapTokens` splits it while it is still plain, and the colour goes on after.

**chroma is a lexer and nothing else.** Its formatters emit their own ANSI and
resets, and a reset mid-line takes the app background to the margin (Rule 115).
The `TokenType → TokenClass` mapping was **read off the lexers**, not guessed:
JSON and XML both emit `NameTag`, for a key and for a tag respectively, hence the
`kind` parameter.
The invariant everything rests on — concatenating the tokens reproduces the input
exactly — has a test of its own. Cost: **+4.0 MB** on the binary (19.9 → 24.0),
because chroma embeds every lexer.

Syntax colours are **semantic aliases assigned in `ApplyTheme`**, like
`ColorChartBg`: no theme file gains a key. Log levels get none —
`StatusErrorStyle`, `StatusWarningStyle` and `DimStyle` already mean that.
`ColorSearchMatch`/`Fg` are the same kind of alias.

**A search filters and highlights, and one rule decides both.** `MatchRanges` is
the only thing that says a line matched, and it says *where* in the same answer:
the filter is `len(ranges) > 0`, so "a line the search kept carries at least one
highlighted occurrence" holds by construction. Two calculations for one question
is what `scan.Categorize` and `Result.SecretVerdict` each had to undo, and the
symptom was identical both times — something counted in one place and absent from
the other. Here it would be a line kept with nothing visible in it.

The consequences, each with a test:

- **A match is one more span, never a colour laid over a finished line.**
  `MarkMatches` cuts the tokens while they are still plain, for the reason
  `docLine` exists at all. It runs *after* the filter, so only on the lines about
  to be drawn, and it keeps Tokenize's byte-for-byte invariant.
- **`matchStyle` overrides the class and the level both.** A log line comes out as
  level, match, level: the level still carries the rest of it, so an ERROR is
  still picked out at a glance, and a search invisible in a log would be missing
  precisely where the lines are longest.
- **`c` has no say over it.** An occurrence is not syntax colouring; turning the
  colours off is how someone reads a document as plain text, and a search they
  then could not see would be the one thing that cost them.
- **Cutting a token copies it** (`withText`, `appendSpan`) rather than rebuilding
  a literal. A literal has to name every field to keep it, which is how a
  highlight works right up until the line is long enough to wrap — the one case it
  exists for. Byte offsets, not runes: a token holds a string. And when
  `strings.ToLower` changes a string's length in bytes (`İ`), the search falls
  back to a case-sensitive one rather than pointing beside the match.

Three key collisions were resolved rather than accepted: follow moved `f` →
`F` (`ctrl+r` and `F` read as *reload once* / *keep reloading*); `h`/`l` are
unbound because no bare letter is navigation and no lowercase letter acts
(§3.26), hence `c` for coloration; and `q` closes nothing — it is the
application's quit key, and the logs pane was the one screen that ate it.

**What this deleted.** The containers logs pane — `viewState`, its `viewport`,
wrap, ANSI stripping, scroll keys, reload, follow, timestamps and the external
pager — moved here whole; `containers/update.go` went from 707 to 548 lines.
`wrapLines` and the ANSI/CR normalisation were **moved**, comments included:
their reasons apply to any text this application shows. Every `docker inspect`
pager path is gone, Windows temp files included. `V` survives for logs alone,
because `less` handles a gigabyte and follows it.

**`PagerCmd` carries no quote, on either branch.** Go's `exec.Command` escapes
an argument's inner quotes as `\"` when it builds a Windows command line, and
`cmd.exe` does not understand that escaping — it reads the backslashes as part
of the path. So `more "%TEMP%\devdesk-logs.txt"` reached cmd as
`C:\C:\Users\...\devdesk-logs.txt\`, which it refused, and `V` returned to
DevDesk instantly with an exit status nobody rendered: on Windows the pager
never once worked. Measured by running the exact string through
`exec.Command`, not deduced.

The temp file went with the quotes, because its stated reason was false: **`more`
reads a pipe** — `dir | more` is its canonical use. The two branches now differ
only in the shell and the pager's name, nothing touches disk, and no path needs
quoting. The container ID is still interpolated into a shell string, which is
safe only because it comes from `docker ps`; do not extend that to a value the
user types.

A failed pager is now **reported** (`Pager failed — check logs`). One that
cannot start comes back in milliseconds and is indistinguishable from one the
user quit at once, which is exactly how a command line broken since the day it
was written went unnoticed — the log line was there all along, and nobody reads
a log to find out why a key did nothing.

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
- `trivy.go` — CVE, secret, license, misconfiguration detection
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
- `X` (exclude — add to `.gitleaksignore`) is offered for **Gitleaks findings only**. That
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

**`Result.SecretVerdict()` is the only thing that decides whether a target
carries a secret**, and it has three answers. There were two calculations, the
same loop copied — "a finding whose `Source` is `gitleaks`" — and both went wrong
the day Trivy started finding secrets too: a repository whose only secrets were
Trivy's read as clean, and an image had no secrets field at all. The verdict is a
`*bool` because a secret stage can fail to run three ways — the option is off,
the tool is absent (both stages require `deps.*Available`), or the stage errored
— and `false` in any of them is a green icon on a scan that **looked at
nothing**, which is D20 in one field. `Result.SecretsScanned` carries the fact,
set by a stage that *succeeds*.

Both caches hold it as `Sensitive *bool`, `nil` meaning nobody looked. The
workspace side migrated for free: its old field was always written
(`json:"sensitive"`, no `omitempty`), so an existing file decodes to a non-nil
pointer. An image entry has no key at all and decodes `nil` — the truth about it.

`theme.SecretsState` is the one iconography, used by `ws`, `oci/images` and the
`:sec` inventory. Do not decide the colour from the rendered icon string, which
is what `ws` did.

**Security view** (`internal/ui/security/model.go`) has three states:
`StateInventory` (the landing page), `StateResults` and `StateDetails` (with
remediation info).

**The findings table has one filter bar and it is drawn** (D45). The bar carries
the four severity tokens (`c` `h` `m` `l`, cumulative), the search and — since
`.` went back to the sort — the arrow saying which column is sorted. Three of
those were declared and unreachable: `RenderFooter` drew no bar in the results
state while `GetFooterHeight` counted one, so the router took two lines off the
viewport and nothing filled them; no column declared a `Less`, so `CycleSort`
returned on its first line; no column declared a `Search`, so `/` — which the
tokens alone make available — opened a query that matched nothing.

`severityRank` is what the Severity column sorts by. Alphabetically, CRITICAL
sits between no two levels it belongs with, so a descending sort would put
MEDIUM on top and bury what the view was opened for. UNKNOWN ranks below LOW: it
is the absence of a score, not a claim of something worse than critical — which
is also why it has no token.

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
| `S` | rescan the row, **overwriting** its entry |
| `A` | rescan every target; its confirmation carries a **purge** checkbox |
| `ctrl+r` | reload from the caches |

**A target is displayed folded and resolved whole.** An image shows its registry
prefix replaced by the configured alias (`nx/agent-base:1.0`), exactly as the
images tab does — `internal/ui/registryalias` is the adapter both call, and it
lives above `config` and `docker` because neither imports the other and neither
should. A repository still folds its home directory to `~`. Both foldings obey
one rule: **`scanTarget.Name` is the cache key and never moves.** It is what
`enter`, `S` and `A` resolve, and what `AddToGitleaksIgnore` writes into via
`m.targetPath` — hence `targetLabel` as a second field rather than a folding
applied in place, since an aliased reference names a directory that does not
exist. The alias rides on the row (`Display`), stamped by `setInventory` beside
the spinner frame, for the same two reasons: the rows arrive from a `Cmd`, and a
column function is built once in `New` and can reach neither.

The Target column therefore **sorts on the key and searches both names**: an
alias is a display name the user can rename, so sorting by it would move every
row of a registry the day they do, while a column showing one name and matching
only the other reads as a bug.

**The inventory runs its own scans.** With the options in the config there is
nothing to carry to whoever would run one — which is the only reason the
cross-view delegation exists. It writes to the same two caches, so a rescan here
and `S` in the images list are the same operation.

**The load reconciles: a target with nothing behind it is not listed.** A cache
outlives what it describes, and a deletion reaches it from four directions — `D`
on an image, `P`, `D` on a repository, and any `docker rmi` or `rm -rf` outside
the application, which no cascade can observe. One rule at the load covers all
four: an image absent from `docker image ls` and a repository path that is gone
are dropped from `InventoryLoadedMsg`.

Two things it deliberately does not do:

- **It never reads a failure as an absence.** `localImages` returns a second
  value saying whether it could find out, and a failed enumeration keeps every
  image — a stopped daemon would otherwise empty the inventory. `isGone` tests
  `os.IsNotExist` and nothing else, so a permission error or an unmounted share
  keeps the row.
- **It hides, it does not delete.** The entry and its stored result stay on
  disk: a transient answer must not destroy a scan nobody asked to purge. `A`
  only rescans the rows that are there, so a hidden entry costs nothing while it
  waits.

`listImages` is a package var only because of this — a test cannot pull an image,
and the cache round trips would otherwise be reduced to asserting a fixture is
absent, which they would pass for the wrong reason.

Three invariants, each with a test that fails without it:

- **The purge clears the counts, not the rows.** The rows *are* the list of what
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
- **A picker entry is identified by `browserRegistryEntry.key`, never by its
  URL** (D40). Two registries may be declared on one host — the form enforces
  slug uniqueness, not URL uniqueness — so a URL ticks and unticks both, and the
  exclusion outlives the session. The key is the slug for a standalone registry
  and `memberKey(groupSlug, memberURL)` for a member; `Slug` alone will not do,
  since a member carries its *group's*. Read it through `selected(entry)` rather
  than indexing `selectedRegs` at a new site.

### Scan Cache

Two independent disk+memory caches in `internal/cache/`:
- `ImageScanCache` — keyed by `"repo:tag"`, metadata at `~/.devdesk/cache/image-scans.json`, full results in `image-results/<sha256>.json`
- `WorkspaceScanCache` — keyed by absolute repo path, metadata at `~/.devdesk/cache/workspace-scans.json`, full results in `workspace-results/<sha256>.json`

Both entries carry the four severity counts and `Sensitive *bool`, the secret
verdict written by `scan.Result.SecretVerdict()` (see Security Scanning). `nil`
is a value: it means no stage looked, and it is what every image entry written
before there was an image secret stage decodes to.

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

Cache invalidation: `S` (single) overwrites; `A` (all) rescans, and purges the cache first when its checkbox is ticked.

### Docker / OCI Integration

- `internal/docker/client.go` — wraps Docker CLI (exec-based): list, metrics, stop, restart, pause, remove, prune
- `internal/docker/netdiag.go` — ephemeral container runners with `--network host`: `RunPing`, `RunDNS`, `RunTraceroute`, `RunTCPTraceroute`, `RunNetcat`, `RunCurl`, `RunSSLCert` → returns `DiagResult{Success, Output}`
- `internal/docker/network.go` — `RunSS(image, numeric)` for real-time port table (mounts host DNS files, uses `--privileged --net=host --pid=host`), `KillProcess(image, pid)`, `PortInfo` struct, `parseSSOutput()` multi-format parser
- `internal/oci/oci.go` — OCI registry HTTP client: list tags/templates, download + extract tar.gz

### Network Diagnostics View

`internal/ui/netdiag/` — three tabs:
- **Diagnostics tab** (`model.go`): target, port and resolver, then
  `internal/netcheck`'s pipeline — resolve, reach, connect, TLS, HTTP — chained
  one stage per message so the footer can name the question being asked. The
  seven tool checkboxes are gone (§3.33): the checks follow from the target and
  from what has already failed.
- **Ports tab** (`ports_model.go`): live `ss` monitoring with filtering by
  protocol, state and text. `K` kills a process, after a confirmation (requires
  a privileged container).
- **Topology tab** (`topology_model.go`): Docker network inspection.

**What is configurable, and what is not.** `internal/netcheck` held its timeouts
as constants with a comment saying they would become settings when somebody
asked; `network:` is what they became.

| Setting | Replaces |
|---|---|
| `check_timeout` | five per-stage constants — 5 s for DNS and the dial, 8 s for TLS and HTTP, 4 s for the ping |
| `ping_count` | `pingCount` |
| `cert_expiry_warn_days` | `expiryWarnWindow` |
| `traceroute_max_hops` | `-m 30` in `docker/netdiag.go` |
| `ports_refresh_interval` | the 2 s tick |

**One timeout replaces five**, and the default is the old *maximum* so nothing
that answers today starts failing. The 5 / 5 / 8 / 8 / 4 split was never argued
anywhere — five rows for one idea, and each of the five a separate guess. The
cost is stated rather than discovered: an unreachable host now spends 8 s on DNS
instead of 5, which the staged progress line makes legible rather than a hang.

Three things stay hardcoded, each for a reason:

- **The traceroute wait per hop (`-w 1`)** multiplies with the hop limit, so a
  second setting would let a user build a fifteen-minute trace out of two
  numbers that each look reasonable.
- **`MinVersion: VersionTLS10`** — the handshake is *probing*, not securing.
  Reporting an old version is the point; a setting could only make the tool
  blind to what it exists to find.
- **`InsecureSkipVerify`** — the TLS stage verifies the chain itself so it can
  say *which* part failed. A setting here would collapse four checks into one
  error string.

`netcheck.Settings` travels **beside** `Env`, not on it: `Env` is the seam to
the network, while `PingCount` and `ExpiryWarnWindow` are read by stages and
never by a network call — putting a preference behind the seam would make every
fake answer for one. `Settings.Normalized()` fills in anything non-positive at
every entry point, so a hand-edited `0` becomes the default rather than a dial
with no deadline. The view calls `checkSettings()` per run rather than capturing
at construction, so a run already in flight and the config cannot disagree
halfway down the pipeline.

A route trace is the one probe that still shells out — it needs raw sockets and
a tool worth not reimplementing — so it answers for the *container's* view of
the network, and `renderTraceHeader` says so on screen. Everything else answers
from the DevDesk process, because `--network host` on Docker Desktop is the VM's
namespace and not the machine's.

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

## Le footer — `components.FooterMessage`

One line of state below every viewport, and **one implementation**. There were
eight, one per view, each with its own pair of fields, its own clear message and
its own lipgloss block — which is precisely how the errors ended up left-aligned
everywhere while the notices were centred: nobody ever centred the error branch,
in any of the eight.

```go
type Model struct { footer sharedcomponents.FooterMessage }

// From Update() only (Rule 110). The Cmd is the expiry timer; a message set
// without it never clears.
return m, m.footer.Error("Failed to load — check logs")
return m, m.footer.Warn("Scan already in progress")
return m, m.footer.Info("Image pulled: " + name)

m.footer.Handle(msg)                              // consumes its own expiry
m.footer.SetSpinnerFrame(m.spinner.View())        // from the spinner tick

// From View(), read-only. Always one full-width, centred line — empty included,
// because Rule 124 budgets one whatever it holds.
m.footer.View(width, m.status())
```

**Three levels, defined by what happened rather than by how it feels.** An
`Error` is an operation that failed or was refused by the system; a `Warn` is an
action that cannot be honoured as asked while nothing failed — a precondition
unmet, something already running, a setting with no meaning here; `Info` is a
neutral fact or a success.

| Level | Colour |
|---|---|
| Error | `ColorFooterError`, the **CRITICAL** severity red |
| Warning | `ColorFooterWarn`, the **MEDIUM** severity orange |
| Info | `ColorFooterInfo` — `ColorText`, the ordinary text colour |

The three are **semantic aliases assigned in `ApplyTheme`**, like the viewer's
syntax colours: no theme file gains a key. They alias the **severity** colours
rather than `ColorError`/`ColorWarn`, which the default theme makes identical —
the choice is invisible today and stops being so in a theme that separates them.
Info is `ColorText` because `ColorHighlight` carried it before and is a yellow
one notch from the warning's orange: the two levels were indistinguishable.
Info is not bold, the other two are.

**Green is gone from every footer.** It belongs to status icons (Rule 121); the
security view's status line was the one place it leaked in.

**The expiry is identified.** `ClearFooterMsg{ID}` only clears the message it
was started for, which is what makes the type safe to share between packages —
and what fixes the defect all eight local implementations had: a message set at
t+2.9s was wiped at t+3s by its predecessor's timer.

**`Status` is the second argument, and it has no timer.** A progress line, a
hint, a table loading its rows — these are states the view derives on every
frame, not events. A batch outlives the three seconds a message gets, so a line
set when the first repository started would clear while the tenth was still
fetching. It is a parameter rather than a field because it is derived: the
containers action line comes from `BusyLabels()`, which changes with no event to
push it on.

Precedence inside `View`: **error → warning → info → status**. A failure the
user has not read yet matters more than the progress of what is still running.

**A table's load is a footer status with a spinner** (Rule 139). The table stays
on screen: a body that swapped itself for a spinner lost its header and its
columns for the length of every `ctrl+r`, then got them back. The empty state
(`No images found`) is therefore conditional on the load being over, or the
table announces the absence of what it is in the middle of fetching.

The spinner frame is the **rendered** `spinner.View()`, not a bare frame: every
view already styles its spinner with `theme.SpinnerStyle()`, and restyling would
nest one escape inside another. It is measured with `lipgloss.Width`, which
ignores escapes — the opposite of a table cell's rule (Rule 122), and the
difference is which measurer is doing the work.

**`FooterMsgDuration` is an exported var, and only tests touch it.** `tea.Tick`
blocks for its whole duration and `testutil.Msgs` runs every command it is
handed, so a test inspecting a Cmd that carries the timer paid three real
seconds — four did. A test that wants to see a message expire builds
`components.ClearFooterMsg{ID: m.footer.ID()}` instead; one that must drain the
Cmd calls `testutil.FastTimers(t, &components.FooterMsgDuration)`.
`TestAMessageGetsThreeSeconds` pins the production value.

Two source-level tests hold the line, on the model of `internal/ui/keymap`'s:
`TestNoViewStylesItsOwnFooterMessage` fails on a `StatusErrorStyle`,
`StatusOKStyle`, `StatusWarningStyle` or `ColorHighlight` inside a
`RenderFooter`/`renderInfoLine`/`renderInfoText`, and
`TestNoTableViewRendersALoadingBody` fails on a `theme.SpinnerMessage` in a
table view. The one screen that legitimately fills itself with a spinner — the
registry browser mid-pull, which has no table behind it — is written down as a
declared exception, the way `keymap.DeclaredExceptions()` is.

## Tables — `internal/ui/datatable`

The shared mechanism behind the application's tables: column widths, sorting,
sort arrows, filter matching, cursor clamping and cursor-to-object resolution.
`theme.DefaultTableStyles()` and friends still own the *look*.

```go
datatable.New(datatable.Config[T]{
    Columns: []datatable.Column[T]{{
        Title: "Name", MinWidth: 20, Flex: 1,
        Cell:   func(x T) string { … },  // plain text — Rule 122 by construction
        Style:  func(x T) lipgloss.Style { … }, // nil = the table's own colours
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

**The package renders its own rows** (`render.go`), and that is what makes
`Style` possible at all. `bubbles/table` measures a cell with `runewidth`
*before* styling it, and runewidth counts an escape sequence's bytes as width: a
seven-cell string carrying a colour measures 28, so it is truncated in a column
twice wide enough and the cut lands inside the escape — the unterminated
sequence then bleeds over every row below. That is Rule 122, it is a
`bubbles/table` limitation rather than a Bubble Tea one, and `bubbles v1.0.0`
has the same line. Inverting the order — `Cell` is measured while plain, `Style`
is applied to the finished cell — makes the failure unexpressible instead of
forbidden by review.

bubbles is still the state: rows, columns, cursor, focus and height. What moved
here is the drawing and the scroll offset (`clampOffset`), which its viewport
kept unexported. `Table()` reports the same thing it always did.

Two consequences worth keeping:

- **`Style` is not consulted for the selected row.** That row goes to
  `styles.Selected` whole, and a colour inside it closes with a reset that takes
  the selection background with it for the rest of the line. The highlight
  answers "where am I"; no per-cell colour is worth ending it mid-row.
- **Every cell on an unselected row carries an explicit background.** lipgloss
  does not inherit one (Rule 115) and the app's viewport style only reaches
  cells that emit nothing, so one coloured cell would otherwise strip the
  background from everything to its right. A column declaring only a foreground
  gets `ColorBackground` filled in.

A colour that appears on every row informs no one: a zero count, a `-` and a
never-scanned target are `DimStyle`, the nominal majority state (a `running`
container) keeps the default text colour, and the colour is spent on what is
worth spotting without reading.

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

**A cell's colours are decided here, not in `theme.DefaultTableStyles()`.**
Neither it nor bubbles sets a foreground on `Cell`, so a column with no `Style`
used to render in the terminal's own colour, whatever the theme said — four views
had copied `Foreground(theme.ColorText)` into a `Style` to get it back. It cannot
be fixed on `Cell`: the cells are rendered and *then* the whole line is handed to
`styles.Selected`, so a colour inside it opens a sequence whose reset ends the
highlight mid-row. `cellStyle` fills in both `ColorText` and `ColorBackground` on
unselected rows only, and a column declaring one of the two gets the other.

A column with no opinion should return the zero `lipgloss.Style` rather than
naming the theme's text colour itself.

### A row an action is running on (§3.22)

A row says two things: what the object **is**, and what is **happening** to it.
They share one glyph on purpose — one column answers "what about this row" — and
`datatable` owns the one rule that follows: **busy wins over state**, because
`exited` is precisely what the action is about to change.

```go
datatable.Config[T]{
    Key:          func(c docker.Container) string { return c.ID }, // nil ⇒ facility off
    StatusColumn: 0,                                               // the cell the spinner replaces
}

m.table.MarkBusy(id, "Stopping web")  // from Update, never a Cmd (Rule 110)
m.table.ClearBusy(id)                 // on *every* outcome, failures included
m.table.IsBusy(id)                    // the guard
m.table.BusyLabels()                  // sorted, for the footer
m.table.AdvanceSpinner()              // from the view's own spinner tick
```

**It cannot be a `Busy func(T) bool` on the config.** Columns are built once in
`New` and close over nothing — that is why `imageRow` exists — so a predicate
there would have to close over the view's map of in-flight actions. Separating
the identity (`Key`) from the state is what avoids it, and it buys the thing
that matters most: **`IsBusy` answers for an object whose row does not exist
yet**, so it is also the guard on a confirm path. It is also what survives the
periodic refresh, which replaces the items mid-action.

The spinner's **frame lives in the table, its tick stays in the view**: a
spinner needs a `Cmd`, and this package returns none.

Rendering, and each part is a decision:

- The status cell shows the spinner **on the selected row too** — a signal that
  vanishes under the cursor is lost exactly when it is being looked at.
- Off the cursor the row goes `DimStyle`, the spinner `ColorHighlight`, and the
  column's own `Style` is dropped.
- **On** the cursor the highlight itself changes
  (`theme.TableStylesForState("busy")`), overriding whatever `SelectedStyles`
  returned — a colour inside the joined row would close with a reset and end the
  highlight mid-row (`render.go`).
- The override is applied **when the row is drawn**, not when it is built, so
  the stored cells do not go stale as the frame advances. Tests assert on
  `View()`, not on `Table().Rows()`.

The status column must **not be sortable**: `askFor` reserves `width(Title)+2`
for a column carrying a `Less`, which is expensive for a glyph.

**Which cell the spinner spends is a decision per table**, and it is the same
decision every time: the one the user does not need while the action runs. Only
`containers` gained a column, because only it has a state worth a column of its
own; everywhere else the spinner rides an existing cell, which is §3.16's
argument about the clone checkbox — a column costs cells on every screen to say
nothing on all but one row.

| Table | Key | Cell the spinner takes | Actions |
|---|---|---|---|
| `containers` | container ID | a status column of its own (the state icon left the Image cell) | stop, restart, pause/resume, remove |
| `oci` images | image ID | `ID` — it neither sorts nor searches | remove |
| `oci` networks | network ID | `ID` | remove |
| `oci` volumes | name | `Driver` — a volume has no ID, so its **name** is the one cell that cannot go | remove |
| `oci` registries | URL | `Logged` — precisely what the operation is about to change | login, logout |
| `netdiag` ports | **PID** | `State` | kill |

Two of those keys are worth the note. The ports table keys on the **PID, not the
socket**, so every row of a process spins at once — which is what happens, the
kill takes them all. The registries table keys on the **URL**, which is the one
place D40's rule does not apply, and it does not apply because the *operation*
is host-scoped: one `docker login` really does change the answer for every entry
on that host.

`oci_resources` gathers `BusyLabels()` from **all four tabs**, not the active
one: an action started on Images goes on running after `tab`, and a spinner that
stopped turning because the user looked elsewhere would read as a hang on the
way back. The spinner tick has to keep being scheduled while anything is busy —
that is what `advanceBusySpinners()` reports.

Deliberately outside this:

- **Scans** — their spinner is in the Scanned column and they do not block the
  object the same way.
- **`prune`** — it acts on no row, so marking every row would say something
  false. It gets a footer line rendered from the view's own state (`m.pruning`),
  like `syncStatusLine` (§3.17), because a footer *message* expires after three
  seconds (Rule 128) and `docker stop` outlives that by seven.
- **`workspaces`** — it already had all of this, its own way (`scanningPaths`,
  `syncingPaths`, `deletingPaths`, `busy(path)`), and is in fact where the
  design came from. Its delete was the one action its own machinery did not
  know about, and §3.23 step 1 taught it rather than migrating the view.
  Migrating would be a refactor of working code across the one busy notion that
  is *not* one object, one action: scan and sync exclude each other across
  nested paths, a sync targets a tree rather than a row, and the spinner does
  not spend the same cell for every operation.

**Every table in the application is a `datatable`** (§3.21 moved the last four:
Registries, the registry browser's tags, network-inspect and netdiag's results).
A new table uses it; there is no second way to build one, and `bubbles/table` is
imported outside this package only for its `Styles` type.

The Registries tab is the one worth knowing about, because it shows **two
populations in one table** — the configured entries, and one group's discovered
members after `→`. `datatable.Model[T]` is generic over a single `T`, so
`registryRow` carries both and holds the index of the config entry it stands
for, `-1` for a member. That index is what `getSelectedRegistry` resolves
through: indexing `m.registries` by row number is right only while the table
neither sorts nor filters, which is exactly the dependency nothing signalled.
`explorerRow` exists for the same reason.

Three views keep a filter of their own, and deliberately. `security` selects
findings **by tab**, and `status` drives both its tables from one search box so
the header counts agree — in both cases the view filters and calls `SetItems`,
because a `FilterBar` query narrows a list that is already settled and these
decide which rows exist at all. `security` also calls `GotoTop` explicitly on a
tab change, which is the reset `SetItems` does not make. Its **severity** goes
the other way: four cumulative tokens the table owns, and its sort and its
search are the table's too. The
registry browser is the third: its text filter and its registry filter narrow
the tags *before* the table sees them, and its bar is drawn in the OCI view's
own footer rather than the table's. Its **sort** is the table's — `.` cycles
Tag and Updated through `CycleSort`, and `SetSort` is what puts a new search
back on the default order.

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
