# DevDesk Backlog

**Last Updated:** 2026-08-03

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

**Three open**, all in the registry browser and all found while reviewing the
design for §3.8 rather than by a test; see [§1.3](#13-open). D1–D11 are fixed;
§1.1 records what each was and why the chosen fix was the right one.

The five that stayed open longest — D4, D8, D9, D10 and D11 — were parked not
because they were hard but because each altered something the user already saw,
so they needed a deliberate call rather than a drive-by fix. All five were then
decided together and fixed in one pass; see [§1.2](#12-the-five-parked-defects).

### 1.1 Fixed

**Two views' help advertised keys that do nothing.** Found while writing the
phase 2 tests, not previously recorded.

The status view's `GetHelpContent` documented `n` for "Add a new monitor" while
the binding has been `ctrl+n` since Rule 111 standardised it, and the `/` filter
was missing altogether; the empty-state message told the user to "Press [n]" too.
The containers view advertised `S` (shell in a new window) in `GetShortcuts()`
without documenting it at all.

The workspaces view had it too, found in phase 3: `n` and `Enter` documented
where the bindings are `ctrl+n` and `enter`, `/` undocumented, and the same
stale "Press [n]" in its empty state.

`explorer` made it four in a row, though only just: every key was documented
except `/`. The check caught it on its first run.

`security` broke the streak — its help documents every key its header
advertises, across all four states. So the drift was not universal after all;
three of the five views that carried it were the ones where a binding had been
renamed by Rule 111 and the help was not updated with it.

All four are fixed, and every view in `internal/ui` now asserts that each key
`GetShortcuts()` advertises appears in `GetHelpContent()` — the check that would
have caught the drift when it was introduced.

**The explorer acted on a different row than the one highlighted.** Found in
phase 3, and the most serious defect the coverage work has turned up.

`handleKeyMsg` resolved the cursor against `sortedItems(currentItems())` — the
unfiltered list — while the table was built from the filtered one. With a filter
active the two indices disagree, so `ctrl+d`, `p`, `→` and `ctrl+w` all acted on
whatever happened to sit at that index in the *unfiltered* list. Filtering to a
single project and pressing `ctrl+d` scheduled a different group for deletion.
`GetShortcuts` had it too, so even the advertised `ctrl+w` keyed off the wrong
node.

Both now go through `visibleItems()`, the single list `updateTableRows` builds
from. Pinned by `TestActionsResolveTheRowTheUserCanSee`, which filters to a row
that sits at a different index in each list — the fixtures were chosen so the
two cannot coincide.

**A stranded cursor in the workspaces table.** `bubbles/table.SetRows` does not
clamp the cursor when the row count shrinks. Drilling into a directory with
fewer entries — or narrowing the filter — left the cursor past the end: nothing
highlighted, and `enter`, `ctrl+d`, `r` and `ctrl+s` all silently did nothing
until the user pressed an arrow key. `updateTableData` now clamps.

The containers view avoids this by calling `GotoTop()` after every filter
change; workspaces had no equivalent. **`explorer` had it too**, and only on the
filter path — its drill-down was already safe because it calls `GotoTop()`.
Fixed the same way.

**Footer messages with no timer.** `explorer.handleDeleteComplete` set
`footerError` and returned `nil`, so a failed delete left "Delete failed — check
logs" on screen until something else overwrote it. Rule 128 caps footer messages
at three seconds, and the same file's `BrowserOpenedMsg` handler already did it
correctly. Now returns `clearFooterErrorCmd()`.

The security view had it worse: **Rule 128 was not honoured anywhere in it.**
`statusMessage` was set in three places — "Added x to .gitleaksignore", "Failed
to ignore secret", "No references available" — and there was no clear timer in
the package at all, so whichever happened last stayed on screen until a tab
switch happened to reset it. `clearStatusCmd` / `clearStatusMsg` added and all
three wired, pinned by `TestFooterMessagesExpire`.

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

A **sixth site** closed the family out, in the last package of phase 3:
`parseVersion` in `internal/ui/security` truncated its fallback with
`result[:15]`. Version strings are ASCII in practice, so this was the
lowest-risk of the six — but it is the same pattern, and it is now
`theme.TruncateWidth`. Grepping for `[:` on strings across `internal/ui` now
returns nothing but slice indexing.

The same function had a second defect: it scanned for the version number by
taking the first whitespace-separated token starting with `v`, so
`gitleaks version 8.18.2` reported its version as **"version"**. A leading `v`
now only counts when a digit follows it.

A **fifth site** turned up during the phase 3 tests: `firstOutputLine` in
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

### 1.2 The five parked defects

D4, D8, D9, D10 and D11 were each recorded rather than fixed on discovery,
because each changed something the user already saw. They were decided together
and fixed in one pass. Three of the five had a test written to fail *on the
fix*, and all three did.

**D4 — `tab` navigated the delete confirmation.** Rule 135 reserves `tab` for
switching tabs and assigns field navigation to `↑ / ↓` exclusively; the modal
had it exactly inverted, cycling on `tab` and clamping on `↑ / ↓`. `↑ / ↓` now
cycle, which is what keeps every control reachable in one direction, and the
`tab` cases are gone. The explorer no longer advertises "tab Navigate" while the
modal is open, and `↑↓` is not advertised in its place — Rule 138 calls it
obvious. The permanent variant still skips its locked checkbox, so cycling there
toggles between the two buttons.

**D8 — write-only CRUD flags.** `creating`, `editing` and `confirming` were
assigned in five places and read in none, and `ComponentFormCancelledMsg` — the
only thing that would have reset two of them — could never be sent. Deleted, all
of it. Nothing else changed: the phase 2 tests had deliberately asserted on
`componentForm` / `confirmModal` rather than on the flags, which is what made
this safe a phase later.

**D9 — the containers list opened Z→A.** `New()` set neither `sortColumn` nor
`sortAsc`, so both took their zero value. Both are now set explicitly, so the
default is stated rather than inherited. `TestDefaultSortIsNameDescending`
became `TestDefaultSortIsNameAscending`, and the cycle test walks forward from
ascending. The `loadedModel` helper still sets the sort itself even though it
now matches the constructor: those tests should say which order they rely on.

**D10 — a registry failure was invisible.** `renderTemplateList` returned early
when the field was unfocused and the warning sat below that return, so a form
opened after the OCI registry failed offered a Template field reading "none"
with nothing to distinguish "the registry is down" from "there are no
templates". The warning moved into `renderTemplateWarning` and is appended in
both branches — it explains why the list is empty, so it belongs wherever the
list is.

**D11 — CRITICAL and HIGH rendered identically.** `getSeverityStyle` composed
CRITICAL by hand as `ColorError` + `Bold`, which is byte-for-byte the
`theme.StatusErrorStyle` it returned for HIGH. The palette moved to
`theme.SeverityTextStyle`, next to the `TableStylesForSeverity` it draws from,
and the view delegates (Rule 102). An unrecognised severity now falls back to
the info colour rather than to `DimStyle`, so it is still legible.

The pattern worth keeping: when a defect is recorded rather than fixed, write
the test **inverted** — asserting the current behaviour and saying so. D9, D10
and D11 each had one, and each failed the moment the fix landed, which is how
the stale test and the stale backlog entry got found together.

### 1.3 Open

All three sit in `internal/ui/oci_resources` and are cheap on their own, but
§3.8 rewrites the code path each of them lives in. Fix them **as part of** that
work rather than ahead of it, and write each one's test inverted first, per the
pattern above.

**D12 — `AuthEnabled` has no effect on browse or discovery.** The flag is
honoured in exactly three places: the `docker login` fired on form submit, the
`Logged` column, and the URL list `registryLoginStatusCmd` checks. Neither code
path that actually talks to a registry consults it. `submitSearch`
(`browser_keys.go:142`) calls `docker.GetStoredCreds(credURL)`
unconditionally and hands the result to `searchRegistryTagsCmd`;
`detectRegistryGroupCmd` (`commands.go:563`) does the same before calling into
`registrymgr`. A registry the user has marked as needing no authentication will
still have the host's stored credentials sent to it whenever any other registry
on that host has been logged into — which, given that Docker keys credentials by
host, is the normal case for a Nexus instance. This is the defect the
`anonymous` mode in §3.8 exists to make expressible; today there is no way to
say "do not send credentials here" at all.

**D13 — the browser cannot be dismissed while it is resolving.**
`handleKeyMsg` returns `nil` for every key in `browserStateResolving`
(`browser_keys.go:70`), `esc` included, and that state is entered
unconditionally on open whenever any registry is configured. The detections run
concurrently with an 8 s timeout each, so the wedge is bounded at roughly eight
seconds — but it is eight seconds during which the application ignores the user,
on a screen they may have opened by mistake. §3.8 removes the state rather than
the symptom: with members read from config and cache, there is nothing to
resolve and the form renders immediately.

A smaller thing in the same handler, not worth its own entry:
`HandleGroupDetected` matches the incoming result against `reg.URL`, so two
registries configured with the same URL collide and the second result overwrites
the first. The slug introduced in §3.8 is the natural key to match on instead.

**D14 — the registry filter shows a raw URL for group members.**
`registryFilterLabel()` (`browser_tags.go:63`) resolves the active filter by
searching `b.registries`, which holds only the configured top-level entries.
Discovered members are not in that list, so filtering to one falls through to
returning `b.registryFilter` — the full synthesised URL — where every other row
in the same view shows a short alias. The fix follows from §3.8 rather than
preceding it: once members are persisted they are resolvable, and the filter
gains a group level at the same time.

---

## 2. Technical debt

### Test coverage

Currently **55.3 %** overall; the agreed target is 80 %, which needs roughly
**+6 000 covered statements** over today's ~3 400.

Phased plan, with the harness and most of phase 1 delivered:

| Phase | Scope | Status |
|---|---|---|
| 0 | `internal/ui/testutil` Bubble Tea harness | **done** (100 %) |
| 1 | Leaf components and pure helpers | **done** except the `ui/theme` complement (~55 stmts) |
| 2 | Mid-size view state machines (`status`, `containers`, `dashboard`, `gitlab/auth`) | **done** |
| 3 | Large views (`workspaces`, `explorer`, `security`, `netdiag`) | **done** |
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
| `internal/ui/workspaces` | 0 % | **81.3 %** |
| `internal/ui/gitlab/explorer` | 0 % | **91.5 %** |
| `internal/ui/security` | 0 % | **83.7 %** |

**Phase 3 is complete.** The three-step order held on all four: surface pass
58.8 % / 60.3 % / 58.1 % / 52.9 %, split with the figure unchanged to the
statement every time, completion pass to 86.1 % / 81.3 % / 91.5 % / 83.7 %.

`workspaces` added one technique worth reusing: its filesystem commands
(`createWorkspace`, `deleteEntry`, `renameEntry`, `loadEntries`, `enrichEntry`)
are **executed** rather than asserted on identity, against `t.TempDir()` and a
throwaway git repository. That is what proves the branch, remote and dirty-tree
counters are read correctly; a stub would only prove the stub works. It skips
when `git` is not on `PATH`, like `internal/gitlab` does. Only the Docker- and
desktop-backed commands are left alone.

`explorer` extended that to the API layer, which is why it reaches 91.5 % —
higher than either of the others despite having the largest untestable-looking
surface. The rule that emerged: **execute the command whenever the dependency
can be stood up locally**, and only fall back to asserting on model state when
it cannot.

| Dependency | Treatment |
|---|---|
| GitLab API | executed against `httptest` — the SDK takes a base URL |
| OCI registry | executed against `httptest` |
| git | executed against a seeded repository, skipped without `git` on `PATH` |
| Desktop browser | not executed; the guard branches are driven, the launch is not |

The pull tests are the clearest case: the "GitLab host" is a local directory
holding seeded repositories, so `recursivePull` really clones and really writes
the directory tree. That is what proves the tree mirrors the group hierarchy,
that an existing checkout is skipped rather than clobbered, and that a group
whose children were never browsed is fetched mid-pull. `cloneURL` was extracted
from `pullProject` to make the SSH and HTTPS URL shapes assertable — `gitlab.Clone`
reports only an exit status, so the URL it was handed is not observable through
the error.

`security` added the last variant of the same idea: its cache commands write to
`~/.devdesk`, so `TestMain` redirects `HOME` and `USERPROFILE` at a temporary
directory for the whole package and the purge and save commands are **executed**.
That redirect is not optional there — every checkbox toggle calls `config.Save`,
so without it the tests would rewrite the developer's own configuration.

Its most worthwhile tests are not about the state machine at all: they are about
`extractMeaningfulLines`, which reduces a wall of Trivy and Gitleaks stderr to
the one line that explains a failure. What it *discards* — INFO lines, progress
bars, the doubled `Fatal error / run error:` wrapping — is the whole feature,
and the fallback that shows the raw text rather than an empty panel is what
stops a novel log format leaving the user with nothing.

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

The project's own coding rules cap files at 800 lines. Three still exceed it,
all outside the views phase 3 covered:

| File | Lines |
|---|---|
| `internal/ui/oci_resources/update.go` | 1556 |
| `internal/app/app.go` | 1333 |
| `internal/ui/oci_resources/registry_browser.go` | 822 |

`internal/ui/security/model.go` (1991 lines, the largest file in the project)
was split into `update.go`, `form.go`, `scan.go`, `findings.go`, `details.go`,
`view.go`, `warnings.go`, `header.go` and `messages.go`; the largest is 299 and
`model.go` itself is 245. `warnings.go` is the one worth noticing: the
scan-error parser is pure string handling with no dependency on the model at
all, and pulling it out of a 2000-line file is what made it obvious it deserved
tests of its own.

`internal/ui/netdiag/model.go` (1114 lines) was split into `validation.go`,
`update.go`, `run.go`, `view.go` and `header.go`; the largest is now 314 lines
and `model.go` itself is 172. `topology_model.go` stays at 752 — under the
ceiling, and its parsers and renderer belong together.

`internal/ui/workspaces/model.go` (1299 lines) was split into `messages.go`,
`table.go`, `entry.go`, `actions.go` and `update.go`; the largest is 450 and
`model.go` itself is 129. `entry.go` is the one worth noticing: the pure and
filesystem-only helpers now sit together instead of at the bottom of a
1300-line model, which is what made them straightforward to cover.

`internal/ui/gitlab/explorer/model.go` (1402 lines) was split into `update.go`,
`table.go`, `navigation.go`, `pull.go`, `create.go`, `delete.go`, `api.go` and
`messages.go`; the largest is 227 and `model.go` itself is 206. `api.go` is the
one worth noticing: gathering every GitLab call into one file is what made the
`httptest` pass straightforward — the routes to fake are visible in one place
rather than scattered through a 1400-line model.

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
being written against a layout that is about to change. It held across all four
packages, with the coverage figure unchanged to the statement every time
(58.8 %, 60.3 %, 58.1 %, 52.9 %), including on the 1991-line `security/model.go`.
Use it for phases 4 and 5. The discipline that makes it work is
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
tests actually execute, and coverage is 55.3 %. Rule 110 violations in untested
paths — most of the view layer — remain invisible. The two efforts compound, so
this is an argument for the coverage phases rather than a substitute for them.
Phase 2 puts the first full view state machine under the detector.

Installing a local toolchain is still worth doing for anyone touching `Cmd`s, to
avoid learning about a race from CI after the fact.

---

## 3. Planned features

Carried over from `todo.md`, except §3.7.

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

Not started. The presentation layer — vocabulary, command names and how the
forge gets chosen — is settled and recorded below; the abstraction underneath it
is not.

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

Two findings from the design review that the count above does not capture:

**The client doubles as the authentication flag.** `explorer/view.go:153`,
`:174` and `GetShortcuts()` all branch on `m.shared.GitLabClient != nil` to
decide whether the user is logged in, even though `shared.IsAuthenticated`
exists and says exactly that. Those sites are inside the 66, but they need a
semantic change rather than a type substitution.

**The command surface is already duplicated four times, and already drifting.**

| Location | Role |
|---|---|
| `parser.go:78` `viewMap` | `ParseCommand()` — the authoritative one |
| `parser.go:120` `commands` | `Parse()`, legacy, duplicates the above |
| `parser.go:155` `GetAliases()` | feeds completion |
| `completion.go:47` `buildCommands` | a fourth hardcoded list, **stale today** |

`buildCommands` offers `gitlab-auth` but not `gitlab-explorer`, and omits
`workspaces`, `security` and `net` entirely — so those commands work but are
never suggested. Adding a forge dimension to four unsynchronised tables
guarantees the drift gets worse. **Collapsing them to one source table is a
prerequisite**, and it is worth doing on its own: it fixes the stale completion
list today, independently of GitHub.

#### Model mismatches to settle before coding

These are not implementation details; they decide what the abstraction can even
promise.

- **Nesting — settled.** The explorer is a tree of groups → subgroups →
  projects. GitHub has no nested groups, but it does have **organisations**, and
  a user can belong to several. Flattening everything to one level was
  considered and rejected: someone in five orgs would get a wall of repositories
  with no way to tell them apart by owner. The abstraction therefore **declares
  its depth** — `MaxDepth: 1` for GitHub (orgs at level 1, repositories at level
  2), unbounded for GitLab. This costs nothing in the UI: the drill-down already
  handles two levels, and a user with no organisation simply sees a flat list.
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

#### Settled: vocabulary, commands and forge selection

The organising distinction, which the mismatches above blur: the two forges
differ in **words** and in **shapes**, and only the first is a presentation
problem.

| Difference | Kind | Handled by |
|---|---|---|
| "Group" vs "Organization" | word | vocabulary table |
| "Merge Request" vs "Pull Request" | word | vocabulary table |
| Token label, placeholder, help URL | word | vocabulary table |
| Icon and display name | word | vocabulary table |
| Nested namespaces | **shape** | declared depth (above) |
| Visibility set — 3 values vs 2 | **shape** | forge-supplied option list |
| Role model — int levels vs strings | **shape** | backend returns a humanised role |
| Two-step permanent delete | **shape** | the `locked` flag, §1.1 |
| Dashboard counters | **shape** | different endpoints, above |

Words are cheap and settle in one pass. Shapes are the actual work, and neither
neutral nor per-forge wording helps with them. Keeping the two apart is what
stops the vocabulary layer from quietly growing conditionals.

**Vocabulary is per-forge, not neutral.** A GitLab user says *group*, a GitHub
user says *repository*; "namespace" is a third language nobody speaks, and it
makes the application read as an abstraction layer rather than a tool. The
wording lives in a single `Vocabulary` value per forge — name, icon, namespace
singular/plural, repository, change-request, token label and placeholder, help
URL, visibility set, role names — resolved once from the active context and
carried on `shared.State`, which every view already receives.

The rule that keeps it maintainable: **no view interpolates a forge name into a
string literal.** That is enforceable the way this project already pins
invariants — a test grepping `internal/ui` for `"GitLab"` / `"GitHub"`, on the
model of the existing check that every key in `GetShortcuts()` appears in
`GetHelpContent()`.

Sites to move, none of them subtle:

| Location | Literal |
|---|---|
| `explorer/view.go:342` | `IconGitlab + " GitLab Explorer"` |
| `explorer/view.go:258` | `"GitLab not authenticated … with :gitlab-auth (or :gla)"` — name **and** command |
| `explorer/view.go:267` | `"No groups found … any GitLab groups."` |
| `explorer/view.go:81` | `"Loading GitLab groups..."` |
| `explorer/view.go:210` | `nodeTypeLabel()` → `"Group"` / `"Project"` |
| `components/creation_form.go:26` | `resourceTypes = []string{"Group", "Project"}` |
| `auth/view.go:46` | `IconUser + " Gitlab Authentication"` — wrong icon *and* wrong casing next to the explorer's |
| `auth/view.go:132,140` | `"GitLab URL"`, `"Personal Access Token"` |
| `auth/view.go:81` | help text asserting the token starts with `glpat-` |
| `dashboard/view.go:80,85` | `IconGitlab + " GitLab"`, `"Authenticate with :gitlab-auth"` |
| `dashboard/view.go:104` | `"Merge Requests:"` |

Two of these are shapes wearing a word's clothes. `AccessLevelName()`
(`tree.go:52`) maps GitLab's numeric levels to Owner/Maintainer/…; GitHub uses
`admin`/`maintain`/`push`/`triage`/`pull`, which do not align one-to-one — so the
**backend returns an already-humanised role string** rather than an integer the
UI translates. And `internal` visibility does not exist on GitHub.com, so
`CreationForm`'s three hardcoded values have to come from the forge. Nothing to
undo for the token prefix: `glpat-` appears only in help text, never validated.

**Routing identity is forge-neutral; only aliases and titles vary.** `ViewType`
is a map key in `a.views` and a `switch` case in `app.go:1480,1496`, so it stays
stable — otherwise every new forge touches the router. Canonical names become
neutral (`explorer` is already an alias and becomes the name; `auth` for the
other), and `gitlab-auth` / `gla` **and** `github-auth` / `gha` all parse, to the
same view.

Deliberately permissive: there is only one authentication view, so `gla` typed
in a GitHub context should go there rather than fail. Punishing muscle memory
buys nothing. **The filtering happens in completion, not in parsing** — `gla`
always works, but is never *suggested* while the active forge is GitHub. That
split is what makes it feel fluid without breaking anything existing.

Messages that quote a command (`explorer/view.go:258`,
`dashboard/view.go:85`) must quote the active forge's spelling; once the
vocabulary is centralised that is one more field on the same struct.

**Forge selection: detect, and let the user take it back.** URL sniffing alone is
unreliable — `github.com` and `gitlab.com` are trivial, but self-hosted is the
case that matters and `git.acme.com` could be either. Probing (`/api/v4/version`
vs `/api/v3/`) costs a round-trip and fails on instances that require auth on
those endpoints. A mandatory picker alone is friction on the two most common
cases, where the URL is unambiguous. So:

- The forge is **field 0** of the auth form, above the URL. It governs the URL
  placeholder, the token placeholder and label, and the scope help — putting it
  first is what lets everything below it reconfigure live.
- It is a **cycle field** (`←` / `→`, Rule 132), pre-filled by host detection.
- Detection re-runs as the URL is typed, **but only while the user has not
  touched the forge field** — a dirty flag. Without it, detection overwrites an
  explicit choice, which is the difference between helpful and possessive.
- The token prefix (`glpat-` vs `ghp_` / `github_pat_`) is a second signal used
  to **warn**, never to switch: by then the user has already chosen above.
- **Once authentication succeeds the forge is frozen for that context**, per the
  one-forge-per-context decision. Changing it requires an explicit logout, or a
  new context. Before a successful login it stays freely editable.

This makes the auth form mix a cycle field with the radio buttons it uses for
the save options (`auth/view.go:146`) — themselves a closed two-value set, so
the form was already at odds with Rule 132. **Settled in §3.9**: those radios are
deleted outright, along with the choice they present, so the form is left with a
cycle field and nothing else.

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

0. Collapse the four command tables into one source, and let `Parse`,
   `GetAliases` and `buildCommands` derive from it. Independent of everything
   else, and it fixes the stale completion list today.
1. Define a `forge` abstraction from what the code actually consumes: current
   user, namespace tree, create/delete namespace and repository, initial commit,
   dashboard counters, clone URL. It also declares its **shape** — depth,
   visibility set, humanised roles — not just its data.
2. Replace `shared.State.GitLabClient` with that interface, and switch the
   authenticated-or-not branches to `IsAuthenticated` while passing through.
   This is the change the other 65 call sites follow from.
3. Generalise `GitLabConfig` into a per-context forge config carrying a
   `type: gitlab | github` discriminator, and migrate existing config files.
4. Extract the `Vocabulary` table and move every literal in the table above onto
   it, with the grep test that keeps them from coming back. Doable against
   GitLab alone, before any GitHub code exists — which is what makes it a
   refactor rather than a rewrite.
5. Implement the GitLab backend by moving the existing code behind the
   interface — behaviour-preserving, and covered by the tests §2 phase 5 adds.
6. Implement the GitHub backend.
7. Rename the views and commands, keeping `gla` / `gle` as aliases so muscle
   memory survives, and make completion forge-aware.

Steps 0 and 4 stand alone and improve the code with no GitHub in sight. Steps
1–5 are a refactor of working code with tests already in place, and they are
what makes step 6 tractable.

### 3.7 Command mode from inside a text field — **done**

`alt+:` now opens the command line from anywhere, including a focused text
input. A bare `:` keeps its old, conditional behaviour, so muscle memory
survives.

The problem: `handleKeyMsg` routed `:` through `maybeEnterInCommandMode`, which
asked the view whether it was in edit mode (`FormView.InEditMode()`) and, if it
was, forwarded the keystroke to the active input. Command mode was therefore
unreachable from any form, filter box or search field — most of the application.

The security view is the case that settles it. `InEditMode()` is true there for
the target path, Trivy server and Gitleaks config fields, and the Trivy server
placeholder is `https://trivy-server:4954` — the field has to accept **two**
colons to hold a valid value. Forwarding `:` to the input is not a bug; it is
the only correct behaviour, which is precisely why `:` cannot be the
authoritative key. The only way in was to move focus to a control that takes no
text and press `:` there, so reachability depended on which widget was focused
and nothing said so.

It was worse in `StateScanning`, `StateResults`, `StateDetails` and while a
confirm modal is open: `InEditMode()` is true and there is no field to move
focus to. `:` was forwarded to the view, which has no `case` for it — no view in
the application handles `:` itself — and dropped silently.

#### Why `alt+`, not `ctrl+`

`ctrl+:` cannot be made to work, and this is the note that should stop anyone
reintroducing it. A terminal encodes Ctrl by clearing bits, which only covers
ASCII `@` through `_` (0x40–0x5F). `:` is 0x3A: Ctrl+: sends a plain `:` or
nothing. Reporting it as a distinct key needs the Kitty keyboard protocol or
xterm's `modifyOtherKeys`, and bubbletea v1.3.10 implements neither. A
`case "ctrl+:"` would be dead code.

Alt has no such limit — a terminal sends ESC then the key, and bubbletea reports
that as the key carrying `Alt`. The alternative considered was a free
`ctrl+<letter>` (`b g l p t u v x z` are unused; `ctrl+i m j h [` are Tab, Enter,
LF, Backspace and Esc and must be left alone), which is marginally more portable
but loses the `:` in the gesture.

#### What shipped

- `altCommandModeKey` is handled in `handleKeyMsg` **before** the `InEditMode()`
  fork, so no view can claim it. The router sees every `tea.KeyMsg` first, which
  is what makes the binding unconditional.
- `enterCommandMode()` extracted; `maybeEnterInCommandMode` now only answers the
  bare `:`.
- `testutil.Key` understands an `alt+` prefix, building the key with the `Alt`
  modifier rather than the five literal runes `alt+:`. Both round-trip through
  `String()`, but only one is the message the application actually receives.
- Every `GetShortcuts()` and `GetHelpContent()` advertising `:` now advertises
  `alt+:` (Rules 114, 130, 137). The dashboard's Navigation help section carries
  the nuance in prose, so the other eight sites stay one line each.
- The header still renders `:` as the inactive prompt: it is the command line's
  visual marker, not a key legend, and `:` remains valid whenever no field has
  focus.

Two things the ripple list got wrong, corrected here: `app_test.go:30` is a
layout fixture for `buildShortcutLines`, not an assertion about the binding, so
it was left alone. And `CommandModeView` / `AllowCommandMode` was not merely
made redundant by this change — **it was already dead code**. The router only
consulted it when `InEditMode()` was true, and netdiag's implementation returns
true only on the topology tab, where `InEditMode()` is unconditionally false. It
could never fire. Deleted, along with its one implementation;
`TestCommandModeOnTheTopologyTab` became
`TestTheTopologyTabNeverBlocksCommandMode` and records why.

### 3.8 Docker registry groups

Two shapes have to be supported: a remote registry with or without
authentication, and a **group** fronting several remote registries, itself
reachable anonymously or not. The second is the one the current model cannot
express.

Not started. The decisions below are settled; one is not, and is marked as such.

#### What the current code does

| Concern | Today | Where |
|---|---|---|
| Parent/child relation | in memory only, for the lifetime of one browser session | `registry_browser.go:26` (`browserRegistryEntry`) |
| Group discovery | Nexus REST, re-run on **every** browser open, 8 s timeout each, behind a blocking spinner | `registry_browser.go:153`, `registrymgr/nexus.go` |
| Member URLs | synthesised as `host + /repository/<memberName>`, alias stripped of `-proxy`/`-hosted`/`-local` | `nexus.go:111`, `nexus.go:200` |
| Credential inheritance | member searches use the **parent's** URL as the `~/.docker/config.json` lookup key | `browser_keys.go:130` |
| Registries tab | flat table, no indication a group was ever found | `table.go:182` |
| Result filter (`r`) | cycles member URLs; no group level | `browser_tags.go:25` |

`RegistryConfig.Registries` is a flat `[]RegistryItem` and carries no parent
field. Nothing about a group survives closing the browser: it is rediscovered,
over the network, next time.

The credential inheritance is the one piece that already behaves correctly, and
the reason it does is the same reason the open decision below is hard.

#### Settled

| Ref | Decision |
|---|---|
| A | **One list, one discriminator, one parent pointer** — `kind: registry \| group` plus `parent` on `RegistryItem`. Two parallel lists and a recursive `Children` tree were both rejected. |
| 1 | The parent is referenced by **slug**, not by URL. |
| 3 | Discovered members live in a **disk cache**, not in `config.yaml`. |
| 4 | The Registries tab gets **drill-down** navigation (`←` / `→`), not an indented tree and not a `Group` column. |
| 5 | **No purely logical groups.** A group always corresponds to a real repository-manager group. |
| F | `provider` is a **declared field**, not sniffed from the URL. |

**A — one list.** A Nexus group *is* a pullable registry as well as a container,
so splitting `registries` and `registry_groups` would have duplicated the form,
the table and the credential handling to model a distinction the server does not
make. A single list with a discriminator keeps one table, one form (fields shown
per `kind`), and leaves sorting and filtering intact.

**1 — slug over URL.** The URL is today's de facto identifier and is threaded
through `registryLoginStatus`, the scan cache keys and the message types, which
is exactly why it is the wrong thing to hang a parent link on: editing a group's
URL would silently orphan its members. The slug replaces the URL **only as the
parent link and the cache key** — everything Docker-facing stays keyed on the
URL, because that is what Docker itself is keyed on. Existing configs migrate by
deriving a slug from the alias, falling back to the URL host; uniqueness has to
be enforced at load, and the form needs to reject a collision rather than accept
a config that will not round-trip.

**3 — cache, not config.** Discovered members are derived data with a server as
their source of truth, and `config.yaml` is what the user declares. Putting them
in the cache mirrors `ImageScanCache` and `WorkspaceScanCache` (Rule 126) rather
than inventing a fourth persistence shape:
`~/.devdesk/cache/registry-groups.json`, keyed by group slug, refreshed
explicitly with `ctrl+r` on a group row. The browser then reads config plus
cache and opens instantly and offline, which is what removes D13's blocking
state rather than papering over it. A `Members` column showing the count and
`theme.TimeAgo(discovered_at)` (Rule 127) is what keeps staleness visible —
without it, a silently stale cache is worse than the current re-detection.

**4 — drill-down.** Level 1 lists groups and standalone registries; `→` enters a
group and lists its members; `←` goes back, with a breadcrumb below the table
(Rules 111, 123). The indented-tree alternative reads faster at five registries
but breaks the moment the table is sorted or the FilterBar (Rule 136) narrows
it, and drill-down is already the pattern `gitlab-explorer` uses. Proposed
columns: `Alias | URL | Kind | Auth | Login | Members`.

**5 — no logical groups.** Grouping unrelated registries under a user-invented
name was considered and dropped: it has no server to discover from, no shared
credential to inherit, and no group URL to pull through, so it would be a
display-only concept carrying the weight of a real one. If the need for
arbitrary grouping appears later it is a saved-selection feature in the browser,
not a change to the registry model.

**F — declared provider.** `NexusDetector.CanHandle` currently returns true
whenever `ManagementURL` is non-empty (`nexus.go:31`), which makes that field do
double duty as an implicit "this is Nexus" flag and makes the detector list
effectively single-vendor. A `provider` field on the group — `nexus`, `harbor`,
`artifactory`, `gitlab`, `generic` — turns `CanHandle` into a match on a
declared value, with the generic detector last. It is a cycle field in the form
(Rule 132) and it is what makes a second implementation possible without
guessing.

#### Open: how far credential inheritance goes

The concrete case: a Nexus group **with** authentication fronting eight Nexus
proxies. Browsing the proxy that points at `dhi.io` has to query the *proxy's*
URL while authenticating with the *group's* credentials.

That much the code already does — `submitSearch` sets
`credURL = parentURL ?: entry.URL` — and the reason it works is worth stating
explicitly, because it constrains everything else:

> **`docker login` takes a registry host, not a path.** `~/.docker/config.json`
> is keyed on `host[:port]`, so the group and all eight proxies share one single
> credential entry, because they share `nexus.example.com`.

Three consequences:

- `inherit` is not a convenience for path-based groups, it is the **only** thing
  the credential store can represent. Every member of such a group authenticates
  identically whether the model says so or not.
- A per-member `credentials` override is **not storable today**. It would need
  DevDesk to own a credential store keyed by slug, which puts passwords back in
  DevDesk's custody — something `RegistryItem` avoids by construction (it has no
  password field; §1.1 records how badly the last credential-storage bug went).
- What a member *can* usefully override is **`anonymous`**: do not send the
  group's credentials to this one. That is representable, costs nothing, and is
  the safety valve — and it is the same mechanism D12 needs.

Proposed, pending the discussion: replace `AuthEnabled bool` with an `AuthMode`
cycle field where a group is `anonymous | credentials` and a member is
`inherit | anonymous`. The four cases in scope then map cleanly:

| Case | Group | Member |
|---|---|---|
| Remote registry, no auth | — | `kind: registry`, `anonymous` |
| Remote registry, auth | — | `kind: registry`, `credentials` |
| Group with auth, 8 proxies (the case above) | `credentials` | `inherit` |
| Anonymous group | `anonymous` | `inherit` |

What still needs deciding is whether a member-level `credentials` is worth
supporting at all. It only becomes meaningful for a group whose members are on
different hosts, which decision 5 has just ruled out of the model — so the
current reading is that it should not exist, and that a genuine need for it is a
separate decision about who stores the password.

**Adjacent question to settle before the model is frozen: browse and pull may
not share a URL.** Path-based Nexus access answers the registry API at
`/repository/<name>/v2/...`, which is what `registryAPIURL` builds and what the
tag search relies on. Whether `docker pull` accepts the same path-form reference
depends on the deployment — a dedicated HTTP connector port per repository is
the older Nexus arrangement, path routing needs a reverse proxy in front. If
they differ, a member needs a third URL alongside `url` and `management_url`,
and `multiImageName` (`browser_tags.go:125`) is building an unpullable
reference today. **This should be checked against the actual Nexus instance**
rather than reasoned about; it is one `docker pull` away from being answered.

#### Sequencing

This lands in `internal/ui/oci_resources`, which is **phase 4** of the coverage
plan (§2, ~1 995 statements, pending) and holds two of the three files still
over the 800-line ceiling — including `registry_browser.go` at 822, which this
feature grows.

Do it in the phase-3 order that held on all four large views: **surface tests,
then split, then complete coverage**, and only then the feature. Writing the
group model into an 822-line file with no tests under it repeats the mistake
that order exists to prevent.

#### Sketch of the work

1. Add `slug`, `kind`, `parent` and `provider` to `RegistryItem`; migrate
   existing configs and enforce slug uniqueness at load.
2. Replace `AuthEnabled` with `AuthMode`, and make **both** registry-facing
   paths honour it — this is D12, and it is the step that gives `anonymous`
   meaning.
3. Add `internal/cache/registrygroups.go` alongside the two existing caches;
   move discovery behind it and give the Registries tab an explicit refresh.
4. Turn `CanHandle` into a match on the declared `provider`, with a generic
   detector last.
5. Drill-down in the Registries tab, with the `Members` column and breadcrumb.
6. Rework the browser to read config plus cache: no resolving state (D13),
   group-level checkboxes with a tri-state, a group level in the result filter
   and resolvable member labels (D14), and a remembered selection per context.

Steps 1–4 are worth doing on their own — they are what make the group model
expressible — and step 6 is the one that needs step 3 finished first.

### 3.9 Every secret goes to a host secret manager, and radio buttons go away

**Requirement:** no secret DevDesk holds is written to disk in plaintext. Today
three of the five storage paths are exactly that, and the option the UI labels
"secure" is one of them.

The radio-button removal rides along because it is the same code: once there is
one storage path, the choice the radios present stops existing.

#### Where secrets go today

| Secret | Destination | Protection |
|---|---|---|
| Forge token | `~/.devdesk/credentials-<ctx>.json` | **plaintext JSON**, 0600 |
| Forge token | `gitlab.token` in `contexts/<ctx>/config.yaml` | **plaintext YAML** |
| Forge token | git credential helper | whatever the helper does |
| Registry password | `~/.docker/config.json` via `docker login` | whatever Docker's `credsStore` does |
| Registry password | `registry.password` in `config.yaml` | **plaintext YAML**, read at `explorer/create.go:43,132` |

`RegistryItem` is the one part that already gets this right: it has no password
field by construction, and the multi-registry code relies on `docker login`
instead. The legacy `RegistryConfig.Password` is the outlier, and it is still
read.

#### The option labelled "secure" is not

`ChainStorage.Save` writes to **every** storage in the chain
(`credentials/storage.go:37`), and all three construction sites build it as
`NewChainStorage(FileStorage, GitCredentialStorage)` (`app.go:245`, `:383`,
`:1483`). So choosing **"Save to Git Credential Manager (secure)"** stores the
token in the credential manager *and* in
`~/.devdesk/credentials-<context>.json` in plaintext. The label is not merely
optimistic, it is wrong about the option it describes.

Reads make it worse. `FileStorage` is **first** in the chain and
`ChainStorage.Load` returns the first hit, so the plaintext file is
authoritative and the credential manager is never consulted while that file
exists. The secure backend is decorative in both directions.

The other option is no better, in a different way: **"Save token to config file
(less secure)"** sets `saveToHelper = false` (`auth/update.go:213`), so nothing
reaches the chain at all and the token lands only in `config.yaml`. Both
options put the token in plaintext on disk; the "secure" one does it twice.

**And logout does not clean up.** `handleLogoutComplete` (`auth/update.go:191`)
clears `m.config.GitLab.Token` in memory with no `config.Save` behind it, so the
token survives in `contexts/<ctx>/config.yaml`. `ChainStorage.Delete` does clear
both storages, so the credentials file copy goes — the config copy does not.

#### What "host secret manager" can honestly promise

The distinction that decides the design: delegating to `git credential`
delegates to **whatever helper git happens to be configured with**. If that is
`store`, the token lands in `~/.git-credentials` in plaintext — the same
failure, relocated. `cache` keeps it in memory only. Only `manager` (GCM),
`osxkeychain` and `libsecret` reach a real OS store. So "goes through git
credential" is not the same claim as "encrypted at rest", and the requirement
above is the second one.

| Route | Gets | Costs |
|---|---|---|
| **A — keep `git credential`, inspect the helper** | no new dependency; reuses machinery just hardened for context isolation (§1.1) | must read `git config credential.helper` and refuse or warn on `store` and on empty; still trusts the user's git config |
| **B — talk to the OS store directly** (`zalando/go-keyring`: wincred / Keychain / Secret Service, no cgo; or `99designs/keyring` for more backends) | guarantees the store; independent of git configuration | a dependency, and new failure modes — no D-Bus in a headless Linux session being the usual one |

**Recommendation: B as the primary path, A kept as an explicit alternative** for
users who want their tokens where GCM already puts everything else. Only B can
promise what the requirement asks; A is the pragmatic option and should stay
reachable, not become the default.

#### The fallback has to be worse, on purpose

When no store is reachable, the current answer is a plaintext file. The new
answer is `MemoryStorage` — session-only, re-authenticate on each launch — with
a visible indication that the token is not being persisted.

That is deliberately worse UX than a file, and that is the point: a fallback
that is silently insecure is how the current "secure" option came to exist. The
user should be able to tell, without reading the source, that nothing was saved.

#### Config schema

`GitLabConfig.Token` and `RegistryConfig.Password` come out of the schema
entirely. Parsing them and ignoring them is not enough — that leaves the secret
on disk forever for every existing user. Migration runs on load: if either
field holds a value, move it into the store, rewrite the file without it, and
say so. Note `config.CreateContext` (`config.go:564`) already blanks
`GitLab.Token` for new contexts, so only existing ones need the sweep.

#### Radio buttons, all of them

`auth/view.go:146-147` are the **only** two `RenderRadioButton` call sites in the
application. With a single storage path there is no choice left to present, so
they go, and with them:

- the `SaveToHelper` / `SaveToConfig` constants and the `saveOption` field
  (`auth/model.go:17-20`, `:30`);
- `theme.RenderRadioButton` (`styles.go:377`), which becomes dead code;
- the `ctrl+s` "Toggle save to helper" and `ctrl+f` "Toggle save to config"
  entries in `GetShortcuts()` (`auth/view.go:28-29`) and the "Save Options"
  section of `GetHelpContent()`;
- form fields 2 and 3, which renumbers the form — `nextField` / `prevField`
  clamp at 4 and `updateFocus` switches on 0/1 (`auth/update.go:199-232`).

`RenderCheckbox` stays: checkboxes model independent booleans, which is a
different thing from a closed set of mutually exclusive values.

**Two rule files ripple, in the same commit.** Rule 120 in
`.claude/rules/tui-forms.md` names `theme.RenderRadioButton()` as the sanctioned
way to render radio buttons, and Rule 132 implies they are acceptable for closed
lists. Both should say that **cycle fields are the only control for a closed
set**, and that checkboxes remain for independent booleans.

#### Sketch of the work

1. Add `KeyringStorage` over the OS store, and a probe that reports whether one
   is reachable. Route A becomes a second implementation of the same interface.
2. Delete `FileStorage` and `NewFileStorageForContext`. This is the change that
   makes the "secure" label true, and nothing else in the chain matters until it
   is gone.
3. Replace `ChainStorage` with explicit selection — store, else git credential
   if configured with a real helper, else memory with a warning. Writing to
   every backend at once is what produced the current defect; keep one
   destination.
4. Remove `GitLabConfig.Token` and `RegistryConfig.Password`, with the migration
   sweep on load.
5. Persist the logout: clear the store *and* save the config.
6. Delete the radios and everything listed above, and update Rules 120 and 132.

Steps 2 and 5 are small and fix live defects; they need none of the rest.

---

## 4. Existing plans

Detailed plans live in `.claude/plans/`. One is referenced directly from the old
`todo.md` and is still outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability.
