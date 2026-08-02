# DevDesk Backlog

**Last Updated:** 2026-08-02

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

D1, D3 and D6 (the byte-vs-rune lot), then D2, D5 and D7, were fixed; see
[§1.1](#11-fixed). **D4 is the only one left**, and it is waiting on a decision
rather than on work.

### D4 — `DeleteConfirmModal` uses `Tab` for field navigation

`internal/ui/components/delete_confirm_modal.go:89-97`

`tab` / `shift+tab` cycle checkbox → Yes → No. Rule 135 reserves those keys for
tab switching and assigns field navigation to `↑ / ↓` exclusively.

Cosmetic in isolation, but it is the kind of inconsistency Rule 135 exists to
prevent. Changing it alters muscle memory, so it needs a deliberate call rather
than a drive-by fix.

Note that the surrounding code moved when D2 was fixed: `tab` / `shift+tab` now
cycle through `cycleFocus()`, which skips the locked checkbox. Switching to
`↑ / ↓` remains a one-line change to the key names.

### D8 — Write-only CRUD flags and an unreachable handler in the status view

`internal/ui/status/model.go:64-67` and `internal/ui/status/update.go:57-60`

`Model.creating`, `Model.editing` and `Model.confirming` are assigned in five
places and read in none: the view keys off `componentForm != nil` and
`confirmModal != nil` instead. `HasActiveForm`, the one method that read them, is
commented out at `model.go:139`.

`ComponentFormCancelledMsg` is never sent either. `ComponentForm.handleKeyMsg`
answers `esc` by returning a nil form, and the caller stores that — so the
`case components.ComponentFormCancelledMsg` branch in `Update()` cannot run. Its
body is the only thing that would reset `creating` / `editing`, which is why they
would stay true after a cancellation if anything did read them.

Harmless today, but it is state that lies: a reader reasonably assumes those
flags mean something. Found while writing the phase 2 tests; the tests
deliberately assert on `componentForm` / `confirmModal` rather than on the flags,
so removing them breaks nothing.

**Fix:** delete the three fields, the unreachable case and the message type — or
give `esc` a message and make the flags load-bearing. Deleting is the smaller
change and matches how the view already works.

### D9 — The containers list opens sorted Z→A

`internal/ui/containers/model.go:140-147`

`New()` sets neither `sortColumn` nor `sortAsc`, so both take their zero value:
name, *descending*. The status view, which has the same cycle-sort mechanism,
sets `sortAsc: true` explicitly. The header does show `Name ▼`, so the view is
at least honest about it — but nobody chose it, and it disagrees with the other
list view in the application.

**Fix:** set `sortAsc: true` in `New()`, one line. Left for a deliberate call
because it changes what the user sees on open.
`TestDefaultSortIsNameDescending` pins the current behaviour and says so.

### 1.1 Fixed

**Two views' help advertised keys that do nothing.** Found while writing the
phase 2 tests, not previously recorded.

The status view's `GetHelpContent` documented `n` for "Add a new monitor" while
the binding has been `ctrl+n` since Rule 111 standardised it, and the `/` filter
was missing altogether; the empty-state message told the user to "Press [n]" too.
The containers view advertised `S` (shell in a new window) in `GetShortcuts()`
without documenting it at all.

Both are fixed, and each package now asserts that every key `GetShortcuts()`
advertises appears in `GetHelpContent()` — the check that would have caught the
drift when it was introduced. Worth adding to the remaining views as their
phases land.

**D7** (`extractTarGz` kept parent references in archive paths), **D2** (the
"permanent delete" checkbox was documented as locked but was not) and **D5** (an
unreachable focus clamp in `CreationForm`) were fixed together — three small,
independent defects with no shared code.

`extractTarGz` now routes every member name through `sanitizeArchivePath`, which
normalises the separators and rejects anything resolving outside the root.
Cleaning happens *after* the leading separator is stripped, not before:
`path.Clean("/../x")` returns `"/x"`, which would have absorbed the traversal
silently instead of exposing it. Traversal that resolves back inside the root
(`templates/../README.md`) is kept, normalised. Backslashes are folded to `/`
first, so `..\..\etc\passwd` cannot pass as an ordinary filename on a tar reader
that treats it as one.

The delete modal carries a `locked` flag, set only by
`NewDeleteConfirmModalPermanent`. Rather than merely making the toggle a no-op,
navigation skips the checkbox entirely (`minFocus()` / `cycleFocus()`) and the
line renders dimmed: a focusable control that ignores every key is more
confusing than one that is plainly not there.

The `CreationForm` clamp was deleted and replaced by a comment recording why it
cannot fire, so it is not reintroduced defensively. A test pins the invariant it
was guarding.

Both self-annulling tests became real assertions:
`TestDeleteConfirmModalPermanentCheckboxIsStillToggleable` →
`TestDeleteConfirmModalPermanentCheckboxIsLocked` (plus navigation and
confirmation cases), and `TestExtractTarGzPreservesParentTraversalInKeys` →
`TestExtractTarGzRejectsParentTraversal` (table-driven over five escape shapes)
alongside `TestExtractTarGzNormalisesContainedTraversal`.

**D1** (`ReportModal.View()` emitted invalid UTF-8), **D3** (`wrapInputLines`
looped forever when `wrapWidth <= 0`) and **D6** (`wordWrap` measured bytes) were
fixed together, since D1 and D6 were the same byte-vs-rune defect.

Rather than patch each call site, the truncation logic moved into
`internal/ui/theme/text.go` per Rule 117: `StringWidth`, `TruncateWidth` (keeps
the head) and `TruncateTailWidth` (keeps the tail, for paths). All three measure
terminal columns, so double-width glyphs are handled too, not just multibyte
ones.

A **fourth site carried the same defect** and was not recorded here: `truncate`
in `internal/ui/security/model.go` sliced bytes exactly like D1 and rendered
Trivy finding titles. It is now `theme.TruncateWidth` and the local helper is
gone.

A **fifth site** turned up later, during the phase 3 tests: `firstOutputLine` in
`internal/ui/netdiag` sliced `line[:maxLen-3]` to fill the Output column of the
results table, so any diagnostic whose first line contained a multibyte rune
could be cut in half and bleed across the rows below (Rule 122). Now
`theme.TruncateWidth`, pinned by `TestFirstOutputLineKeepsMultibyteRunesIntact`.
Worth grepping for `[:` on strings when the remaining phases land.

The two pinned tests that `t.Skip()`d became real assertions
(`TestReportModalViewKeepsMultibyteRunesIntact`,
`TestWordWrapMeasuresColumnsNotBytes`).

One call site was deliberately left alone: `truncateResultLine` in
`internal/ui/oci_resources/connectivity_form.go` counts runes rather than bytes,
so it is correct — merely imprecise on double-width glyphs. Folding it into the
theme helpers is a cleanup, not a defect fix.

**Context isolation in `GitCredentialStorage` never worked.** Found while
planning the coverage work, not previously recorded.

The three methods carried the DevDesk context in the `path` field of the git
credential protocol (`path=devdesk/context/<name>`). Git discards that field
unless `credential.useHttpPath` is set, which is off by default, so every
credential was keyed on `protocol://host` alone. Two contexts pointing at the
same GitLab host silently overwrote each other, and the last one to authenticate
won for all of them. Verified against a real `git credential` store before and
after the fix; it affects every helper (GCM, wincred, osxkeychain), since git
strips the path before the helper is ever called.

**Fix:** pass `-c credential.useHttpPath=true` on each invocation. Scoping it to
the call leaves the user's git configuration alone — setting it globally would
change credential resolution for every repository on the machine. Pinned by
`TestGitCredentialIsolatesContexts`.

Existing users must re-authenticate: credentials saved under the old
path-stripped key no longer match. Those credentials were ambiguous across
contexts anyway.

Two smaller defects went with it:

- `Delete` had no timeout, while `Save` and `Load` bounded themselves to 2 s
  precisely so an unconfigured helper could not freeze the TUI. Logout could
  hang indefinitely.
- The timeout killed only the direct `git` child, though its comment claimed
  otherwise. The helper git spawns survives and holds the output pipes open, so
  `Run` kept blocking. All three calls now share one `runCredential` helper built
  on `exec.CommandContext` plus `WaitDelay`, which bounds the wait on those pipes.

`internal/credentials/helper.go` (`HelperStorage`, 137 lines) was deleted rather
than fixed: nothing outside its own tests constructed it. It was also broken —
`detectHelper()` returns whatever `git config credential.helper` holds, so a
common value like `store --file=/path` became the single unfindable command
`credential-store --file=/path` — and it hardcoded `protocol=https`, unlike
`GitCredentialStorage`, which reads the scheme from the URL.

---

## 2. Technical debt

### Test coverage

Currently **37.7 %** overall; the agreed target is 80 %, which needs roughly
**+7 250 covered statements** over today's ~2 100.

Phased plan, with the harness and most of phase 1 delivered:

| Phase | Scope | Status |
|---|---|---|
| 0 | `internal/ui/testutil` Bubble Tea harness | **done** (100 %) |
| 1 | Leaf components and pure helpers | **done** except the `ui/theme` complement (~55 stmts) |
| 2 | Mid-size view state machines (`status`, `containers`, `dashboard`, `gitlab/auth`) | **done** |
| 3 | Large views (`workspaces`, `explorer`, `security`, `netdiag`) | **in progress** — `netdiag` done, three left |
| 4 | `ui/oci_resources` | pending (~1 995 stmts) |
| 5 | Router and I/O seams (`app`, `scan`, `docker`) | pending (~1 360 stmts) |
| 6 | Remainder to reach 80 % | pending (~400 stmts) |

Phase 1 progress:

| Package | Before | Now |
|---|---|---|
| `internal/ui/testutil` | — | **100 %** (new) |
| `internal/ui/shortcut` | 0 % | **100 %** |
| `internal/ui/help` | 0 % | **96.7 %** |
| `internal/ui/components` | 0 % | **80.2 %** |
| `internal/oci` | 0 % | **37.3 %** |
| `internal/docker` | 7.3 % | **65.4 %** (phase 5, pulled forward — see below) |
| `internal/gitlab` | 7.5 % | **100 %** |
| `internal/credentials` | 29.5 % | **98.2 %** |

`internal/oci` stops at 37.3 % because the remaining statements are registry HTTP
paths (`DownloadTemplate`, `listCatalog`, `ListTemplates`) that need a fuller
`httptest` fixture — a manifest plus a gzipped layer — rather than the
single-response stubs used so far.

Phase 5's blocker is cleared for `docker`: the package now routes every CLI
invocation through the `dockerRunner` seam in `internal/docker/exec.go`, so tests
drive argument building and output parsing against canned output. `scan` and
`app` remain.

`internal/gitlab` needed no seam: every function takes a `*gitlabclient.Client`
built from a base URL, so an `httptest` server standing in for the API covers
the whole package. `Clone` is exercised against a throwaway local repository
rather than mocked, and skips when no `git` binary is on `PATH`.

Phase 2, complete:

| Package | Before | Now |
|---|---|---|
| `internal/ui/status` | 0 % | **93.9 %** |
| `internal/ui/status/components` | 0 % | **93.2 %** |
| `internal/ui/containers` | 0 % | **87.2 %** |
| `internal/ui/dashboard` | 0 % | **81.5 %** |
| `internal/ui/gitlab/auth` | 0 % | **94.5 %** |

The view layer needed no seam either, for the reason `internal/ui/testutil`
documents: constructors are pure and `Update()` is a pure function, so feeding
synthetic messages fully determines the resulting state. No test executes a
command Update returns — they shell out to Docker, hit the GitLab API, sleep for
a second or open a browser — so the assertions are on model state instead. The
few places that must touch disk (`config.Load` after a save, `config.Save`)
redirect `HOME` and `USERPROFILE` at a temporary directory, the same trick
`internal/credentials` uses.

Three constraints the phase surfaced, worth knowing before phases 3–5:

- **Colour has to be forced to test styling.** Under `go test` lipgloss detects
  no TTY, falls back to the Ascii profile and strips every escape sequence — so
  any assertion about colour passes whatever the code does. `containers` calls
  `lipgloss.SetColorProfile(termenv.TrueColor)` for the tests that need it and
  restores it afterwards; that is what makes the Rule 122 check (no escape
  sequences in `table.Row` cells) real rather than vacuous. Verified by styling
  a cell on purpose and watching the test fail. `termenv` moved to a direct
  dependency for this.
- **`bubbles/table` keeps its styles unexported**, so `refreshSelectionStyle` is
  asserted by looking for the error-selection escape sequence in the rendered
  table rather than by reading `Styles()`.
- **`sort.Slice` is not stable**, so fixtures must give every sortable column a
  total order or the expected sequences are ambiguous.

Phase 3, in progress:

| Package | Before | Now |
|---|---|---|
| `internal/ui/netdiag` | 15.9 % | **86.1 %** |

The rest of phase 3 is `workspaces`, `gitlab/explorer` and `security`, in
ascending order of size.

Two handlers are deliberately left uncovered in `containers`: `s` and `S` call
`detectShell`, which runs `docker exec` synchronously *inside* `Update`. The
tests drive those keys only in states that return before reaching it. The same
shape appears in `status.reloadConfigAndCheck`, which calls `config.Load()` from
`Update`. Neither is a Rule 110 violation — nothing mutates the model from a
`Cmd` — but I/O in `Update` blocks the event loop and is untestable without a
seam. Worth a look when phase 5 gets to the router.

`internal/credentials` needed no seam either. `git credential` is steerable
through the environment, so the tests redirect `HOME`, `GIT_CONFIG_GLOBAL` and
`GIT_CONFIG_NOSYSTEM` at a temporary directory and run the real binary against a
throwaway `store` helper — nothing reaches the developer's keychain. Running the
real git is what surfaced the context-isolation defect recorded in §1.1; a stub
would have frozen the broken behaviour instead. The 2 s timeout is covered by
installing a credential helper that sleeps.

### Files over the 800-line ceiling

The project's own coding rules cap files at 800 lines. Six exceed it — the
figures recorded earlier were stale, and several files had grown:

| File | Lines |
|---|---|
| `internal/ui/security/model.go` | 1977 |
| `internal/ui/oci_resources/update.go` | 1556 |
| `internal/ui/gitlab/explorer/model.go` | 1402 |
| `internal/app/app.go` | 1333 |
| `internal/ui/workspaces/model.go` | 1299 |
| `internal/ui/oci_resources/registry_browser.go` | 822 |

`internal/ui/netdiag/model.go` (1114 lines) was split into `validation.go`,
`update.go`, `run.go`, `view.go` and `header.go`; the largest is now 314 lines
and `model.go` itself is 172. `topology_model.go` stays at 752 — under the
ceiling, and its parsers and renderer belong together.

`internal/docker/client.go` (1176 lines — the 1069 recorded earlier was stale)
was split into `exec.go`, `containers.go`, `images.go`, `networks.go`,
`volumes.go`, `registry.go`, `launch.go`, `system.go` and `parse.go`; the largest
is now 269 lines. The former `network.go` became `ports.go`, since it reports the
host's listening sockets via `ss` rather than Docker networks — the name was
free for the `docker network` family.

This interacts with the coverage work: writing several thousand statements of
tests against these files before splitting them freezes their current structure.
Decide the order deliberately. Splitting `docker` first was the cheap case — its
7.3 % coverage meant almost no tests were pinned to the old shape.

**The order settled on for phase 3 is: surface tests, then split, then complete
coverage** — per package, so each split has a net under it without the tests
being written against a layout that is about to change. It worked on `netdiag`:
the surface pass reached 58.8 % driving `Update()` and `View()` only, the split
moved five blocks of code with the coverage figure unchanged to the statement,
and the completion pass took it to 86.1 %. The discipline that makes it work is
asserting on behaviour rather than on internals — no test named a file, and the
only ones that reach into the model do so for state the view has no other way to
expose.

### Race detector cannot run locally

`mise run test-race` needs cgo and therefore a C compiler on `PATH`. Without one
it fails with `cgo: C compiler "gcc" not found`. Bubble Tea `Cmd`s run
concurrently, so this is the check most likely to catch a Rule 110 violation.

**Now covered by CI.** `.github/workflows/ci.yml` runs `mise run test-race` on
every push and pull request, on `ubuntu-latest`, which has a toolchain. The first
run reported no data race across all 17 packages.

That is a baseline, not a clean bill of health: the detector only sees code the
tests actually execute, and coverage is 37.7 %. Rule 110 violations in untested
paths — most of the view layer — remain invisible. The two efforts compound, so
this is an argument for the coverage phases rather than a substitute for them.
Phase 2 puts the first full view state machine under the detector.

Installing a local toolchain is still worth doing for anyone touching `Cmd`s, to
avoid learning about a race from CI after the fact.

---

## 3. Planned features

Carried over from `todo.md`.

### 3.1 Network diagnostics

- **Port-forwarding manager** — an interactive dashboard to manage port
  redirections to the host machine.

### 3.2 Interactive security remediation

- **Auto-patch assistance** — after a Trivy scan, offer to generate a patch or an
  updated `Dockerfile` that bumps the base image version to clear critical CVEs.
- **Local SAST** — wire security linters (Gitleaks for secrets, IaC linters)
  directly into the Workspace view.

### 3.3 OCI build and cache analyser

- **Layer visualisation** — analyse a local image and show the size of each
  layer, in the spirit of `dive`. Intended approach:
  - Integrated via `google/go-containerregistry` ("daemonless", no dependency on
    the Docker daemon).
  - Talk to OCI registries directly, pulling manifests and configs without
    downloading the whole image.
  - Stream layers in memory (`tar`) to rebuild the filesystem tree.
  - Compute wasted space by detecting whiteout files (`.wh.`).
- **Cache-miss analysis** — explain why a build's cache was invalidated, e.g.
  "`package.json` changed, invalidating the cache".

### 3.4 Spontaneous dev containers

- **Configuration injection** — inject the developer's dotfiles (vim, zsh,
  aliases) when opening a terminal inside a container.
- **Hot volume mount** — mount the current working directory into a running
  container on the fly, to test a local script without rebuilding the image.

### 3.5 Resource dashboard (embedded mini-htop)

- **TUI charts** — braille-character charts showing live CPU, RAM and network I/O
  for every container in the selected project.
- **Visual alerts** — user-defined thresholds (e.g. memory saturation).

### 3.6 GitHub support alongside GitLab, one active forge per context

Support GitHub as well as GitLab, with **exactly one backend active per
configuration context**. A context targets one forge; switching forge means
switching context.

Not started. The notes below are what the current code imposes on the design,
not a chosen design.

#### What is coupled to GitLab today

| Surface | Size |
|---|---|
| `internal/gitlab` + `internal/ui/gitlab/{auth,explorer}` | ~3 900 lines |
| Direct uses of `*gitlabclient.Client` outside `internal/gitlab` | 66, across 8 files |

The concrete SDK type leaks into `internal/shared/state.go:57`
(`GitLabClient *gitlabclient.Client`), so every consumer is bound to go-gitlab
rather than to a DevDesk abstraction. That field is the load-bearing change: an
interface there is what makes a second forge possible at all.

Also GitLab-shaped: `GitLabConfig` in `internal/config/config.go:41` (URL, token,
clone method, pull settings), the `gitlab-auth` / `gitlab-explorer` view names
and their `gla` / `gle` aliases in `internal/command/parser.go`, and
`shared.GitLabStats`.

#### Model mismatches to settle before coding

These are not implementation details; they decide what the abstraction can even
promise.

- **Nesting.** The explorer is a tree of groups → subgroups → projects. GitHub
  has no nested groups: an organisation holds repositories, flat. Either the
  tree degrades to two levels for GitHub, or the abstraction exposes a depth the
  backend declares.
- **Deletion.** `DeleteGroup` / `DeleteProject` implement GitLab's two-step
  permanent delete (schedule, then purge under the renamed
  `-deletion_scheduled-<id>` path). GitHub deletes immediately and has no
  equivalent, so the "permanent" checkbox is meaningless there. The `locked`
  flag added in §1.1 is the hook for that: a GitHub backend would set it and
  leave the box out of reach, as the already-scheduled GitLab case does.
- **Dashboard counters.** `FetchDashboardStats` reads `X-Total` from five list
  endpoints. GitHub has no equivalent header for these; the counts come from the
  search API (`search/issues?q=is:open+is:pr+assignee:@me`), with different rate
  limits and semantics.
- **Vocabulary.** Group/project/merge request vs organisation/repository/pull
  request. The UI must pick per-backend labels or a neutral vocabulary; Rule 129
  applies either way.

#### Open decision: Go SDKs or the `gh` / `glab` CLIs

Worth deciding before any code is written, because it determines whether the
package needs a seam.

Arguments for the CLIs:

- Authentication is already solved, including OAuth device flow, self-hosted
  hosts and token storage. DevDesk's own credential handling could shrink — and
  it has already proven fragile (see §1.1).
- `gh api` and `glab api` are raw REST/GraphQL passthroughs, so no SDK is needed
  for coverage of endpoints the abstraction does not model.
- No SDK version churn to track for two forges.

Arguments against:

- **Per-context isolation conflicts with how these tools store auth.** Both keep
  global per-host state (`~/.config/gh/hosts.yml`). DevDesk contexts want
  *different tokens for the same host*. Driving `gh auth switch` from the TUI
  would mutate the user's global CLI state — the exact mistake avoided in §1.1 by
  scoping `credential.useHttpPath` to the invocation. The clean route is
  `GH_TOKEN` / `GITLAB_TOKEN` per invocation, but then DevDesk still owns the
  tokens and the main benefit is gone.
- **Two more hard dependencies.** Today DevDesk needs `git`, and `docker` only
  for the features that use it. Requiring `gh` and `glab` for the core forge
  feature is a real setup-friction regression.
- **Cost per call.** A process spawn per request, against a reused HTTP
  connection today. The dashboard alone issues five calls, and the explorer
  paginates.
- **Testability regresses.** `internal/gitlab` reaches 100 % with no seam,
  because the SDK takes a base URL that an `httptest` server can stand in for.
  Shelling out would put it back in the position `internal/docker` was in, needing
  a manufactured seam — and stubbed CLI output encodes assumptions about the tool
  rather than testing against it.

**Current recommendation:** keep Go SDKs (go-gitlab, go-github) for the API
surface, and use the CLIs only as an *optional* token source — when a context has
no token, offer to read one from `gh auth token --hostname <host>` or
`glab auth status`. That takes the convenience without the coupling. Recorded as
a recommendation, not a decision.

#### Sketch of the work

1. Define a `forge` abstraction from what the code actually consumes: current
   user, namespace tree, create/delete namespace and repository, initial commit,
   dashboard counters, clone URL.
2. Replace `shared.State.GitLabClient` with that interface. This is the change
   the other 65 call sites follow from.
3. Generalise `GitLabConfig` into a per-context forge config carrying a
   `type: gitlab | github` discriminator, and migrate existing config files.
4. Implement the GitLab backend by moving the existing code behind the
   interface — behaviour-preserving, and covered by the tests added in #7.
5. Implement the GitHub backend.
6. Rename the views and commands, keeping `gla` / `gle` as aliases so muscle
   memory survives.

Steps 1–4 are worth doing on their own: they are a refactor of working code with
tests already in place, and they are what makes step 5 tractable.

---

## 4. Existing plans

Detailed plans live in `.claude/plans/`. One is referenced directly from the old
`todo.md` and is still outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability.
