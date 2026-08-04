# DevDesk Backlog

**Last Updated:** 2026-08-04

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

**Four open.** Three are in the registry browser and were found while reviewing
the design for §3.8 rather than by a test; the fourth is dead code found by the
phase 6 coverage pass. See [§1.3](#13-open). D1–D11, D15–D20 and D22 are fixed;
§1.1 records what each was and why the chosen fix was the right one.

The five that stayed open longest — D4, D8, D9, D10 and D11 — were parked not
because they were hard but because each altered something the user already saw,
so they needed a deliberate call rather than a drive-by fix. All five were then
decided together and fixed in one pass; see [§1.2](#12-the-five-parked-defects).

### 1.1 Fixed

**The command line took focus from the render path.** Found in phase 5.
`renderHeader` called `a.commandInput.Focus()` whenever `commandMode` was set —
a mutation inside `View()`, which Rule 110 makes read-only. A
`bubbles/textinput` drops every key it receives while blurred, so the command
line only accepted typing because a render happened to have run first. Focus is
now taken in `enterCommandMode` and released alongside the existing `Blur()`
calls on the way out. In production the ordering held, so nothing was visibly
broken; the coupling surfaced the moment a test drove the router without
rendering, and the same latent bug would bite anyone reordering the loop.

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
advertises, across all four states.

`oci_resources` then produced the worst instance, in phase 4. The Networks and
Volumes tabs documented `n` for a binding that has been `ctrl+n` since Rule 111
renamed it — the same drift `status` and `workspaces` carried — but the **whole
Registries tab was undocumented**: `ctrl+n`, `e`, `l` and `L` appeared nowhere,
so four actions on a tab were reachable only by guessing.

That view needed one adjustment to the check: its help qualifies most keys with
the tab they belong to (`enter (Images)`, `ctrl+n (Networks)`), so the
parenthetical comes off before matching. That convention is worth keeping — with
four tabs sharing a keymap, an unqualified `enter` would be ambiguous.

So the pattern across five views: the drift is a Rule 111 rename the help did
not follow, plus whole surfaces added later and never documented at all.

All five are fixed, and every view in `internal/ui` now asserts that each key
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

**Two tabs reported nothing when Docker was down.** Found in phase 4.
`handleImagesList` has always set a footer message on failure;
`handleNetworksList` and `handleVolumesList` only logged, so with the daemon
stopped those two tabs showed an empty table — indistinguishable from "you have
no networks". Both now match their sibling.

**The Registries tab opened empty.** `switchTab` focused the table and fired the
`docker login` status check, but never called `updateRegistryTable()` — so the
tab stayed blank until an asynchronous Docker call answered, even though the
registries come from the config file and were available immediately.

**A search counter with no floor.** `RegistryBrowser.AddRegistryTags`
decremented `pendingSearches` unconditionally. A duplicate or late response drove
it negative, and the *next* search then started from that base: `IsSearching()`
stayed false while requests were genuinely in flight, so the spinner never
showed. Floored at zero.

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

**Every scan came back empty.** Introduced by the `internal/scan` split itself
and caught within the hour by the coverage pass that followed it — the clearest
argument yet for the surface → split → cover order.

Extracting the subprocess seam collapsed two statements into one return:

```go
return stdout.Bytes(), waitErr(tc.Name, cmd.Wait(), stderr.String())
```

Operands are evaluated before the call, so `stdout.Bytes()` snapshots the buffer
**before** `cmd.Wait()` runs — and `os/exec` fills that buffer from goroutines
only Wait is guaranteed to have finished. `Bytes()` returns a slice header with
the length at that moment, so later writes are invisible: the report was always
empty, every scan reported no findings, and the exit code alone survived. The
code it replaced got this right by accident of being written as separate
statements.

The read now lives in `finish`, after Wait, with the reasoning recorded next to
it so it is not re-collapsed. Pinned by `TestTheReportOnStdoutIsWhatComesBack`,
which is the test that failed first when the seam was finally exercised.

Worth stating plainly: this is the one defect in this list that the tests
*introduced* rather than merely found, and it never reached a commit. A
mechanical-looking refactor changed evaluation order, which is exactly what a
package with no tests under it cannot tell you.

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

**D15 — `esc` never reached a view that was not editing.** The router answered
`esc` itself through `maybeQuitCommandMode`, forwarding it only when the view
reported `InEditMode()`. Every esc-to-go-back handler behind that gate was dead
code: the explorer's `esc` → `handleDrillUp` could not fire, though Rule 111
lists `esc` as the third way up.

**Fix:** the router no longer names `esc` at all — it falls through to the
default branch and is forwarded like any other key. What made this smaller than
it looked is that the router had nothing left to do with `esc` by the time the
branch ran: `handleCommandMode` answers first whenever the command line is open,
so `maybeQuitCommandMode` was resetting an already-closed line and asking for a
redundant resize. It is deleted.

The interesting half is what the gate had done to the views. Two of them
declared themselves *editing* in states holding no field at all, for one reason
— it was the only way to be handed `esc`. The security view said so outright:
`InEditMode()` was documented as *"returns true when the view needs to handle
ESC key"*, and claimed `StateScanning`, `StateResults` and `StateDetails`;
netdiag claimed `StateRunning` and `StateDetails`. That claim costs more than it
buys, because `InEditMode()` also governs `:`, `q` and `?` — so the security
results screen, the one place a user most wants to jump elsewhere, was the one
place `:` did nothing. Both predicates are now about focus and nothing else, and
those states get the command line, the help overlay and quit back.

Left alone deliberately: the containers view claims `stateLogs`, where `q`
really is the view's own key for leaving the log pane. That is key ownership,
not a workaround for `esc`.

**D16 — `:netdiag` was documented but not accepted.** One missing map entry.
**D17 — the completion catalogue was a subset of the parser.** Eight commands
listed against fourteen views plus aliases, kept by hand in two places.

**Fix, for both:** one map. `viewNames` in `parser.go` maps every accepted
spelling to its view, the key equal to the `ViewType` being the full name and
every other key an alias; `GetAliases()` and the new `FullNames()` derive from
it, and the completion engine derives from those. Two tests hold the two ends
together — everything the parser accepts is suggested, everything suggested
parses — so the lists cannot drift apart again. `ParseCommand` lost its chain of
`if mainCmd ==` comparisons to the same treatment, `actionNames` and
`actionAliases`, and `Parse` is now a lookup in `viewNames` rather than a third
copy of it.

Two aliases documented in `CLAUDE.md` but accepted nowhere came out of this —
`gle` for the explorer and `w` for workspaces — the same defect as D16, found by
the test rather than by reading. `ViewNet` was renamed `ViewNetdiag` and its
value changed from `net` to `netdiag`, so the full name matches the view, the
package and the documentation; `net` remains an alias.

**D18 — the header overflowed a narrow window.** `buildShortcutLines` meant to
clip the shortcut block to its column — the comment said *"Truncate if wider
than col2Width"* — but called `theme.PadWithBg`, which returns content untouched
once it is already at or past the target. Below roughly 100 columns the header
rendered wider than the window and wrapped, costing the viewport a row.

**Fix:** `lipgloss.NewStyle().MaxWidth(col2Width)`, which truncates on rune
boundaries without cutting an escape sequence in half — the corruption Rule 122
is about. Width 80 is now part of `TestEveryHeaderRowFillsTheWidth`, and a
second test checks no row ends inside an escape sequence, which is the failure a
naive slice would have produced.

**D20 — an image scanned with no scanner installed was reported as clean.**
Found by the phase 6 coverage pass, and the most serious defect this repository
has recorded: a security feature that says an image is fine when nothing looked
at it.

`Scanner.Scan` **skips** a stage whose tool is unavailable rather than failing
it (`if s.options.EnableVuln && s.deps.TrivyAvailable`), so with no trivy the
result carried **no errors and no findings**. `scanOneImageCmd` reports a
failure only when there are errors *and* no findings, so the image came back
with zero counts, those counts were written to the scan cache with a fresh
timestamp, and Rule 126 kept them until an explicit rescan. Enter then opened an
empty report.

Nothing upstream caught it: `internal/ui/oci_resources` performs no dependency
check at all, unlike the security view, which gates its scan on
`canStart := m.deps.TrivyAvailable || m.deps.GitleaksAvailable`
(`security/view.go:147`). Every entry point was affected — `ctrl+s`, `A`,
`ctrl+a` and the delegated `LaunchBatchScanMsg` all reach the same
`batchScanCmd`.

**Fix:** `Scan` now records a missing tool as an error, in
`missingToolErrors`, before any stage starts. That is one change in the package
where the knowledge lives and it closes the defect for every caller — the OCI
images view needed no change at all, because `scanOneImageCmd`'s existing
"errors and no findings" condition then reports the failure by itself. Adding a
dependency check to the view was considered and dropped: with no scanner the
scan returns instantly, so a pre-flight guard buys nothing a clear error does
not.

The precision that makes it usable is in **what is not reported**. A stage
skipped because it does not apply to the target type is not missing anything —
gitleaks scans a working tree, so a secret scan of an image was never going to
run — and reporting those would train the user to ignore the warnings panel.
Only stages that would otherwise have run are named, and each message says what
to install (`install trivy or pull aquasec/trivy`).

This reversed a decision the tests had recorded.
`TestAStageWithoutItsToolIsSkippedSilently` asserted that a missing tool was
*not* an error, on the grounds that "the dashboard already says the tool is
absent". That reasoning does not survive contact with the result panel, where
"no secrets found" and "nothing looked for secrets" are the same screen. It is
now `TestAStageWithoutItsToolIsReportedRatherThanSkippedSilently`, and the
partial case is reported for the same reason as the total one.

Pinned by `TestAScanThatCouldRunNoScannerIsNotACleanScan` and, in the view,
`TestAScanWithNoScannerInstalledIsReportedAsAFailure` — the inverted test from
phase 6, turned around, now also asserting that **nothing reaches the cache**.

**D22 — one stray `:` broke every scan, from every view, permanently.** Reported
from a real run, as a Trivy failure nobody could trace back to DevDesk:

```
trivy vuln
exit status 1: FATAL Fatal error flag error: unable to convert flags to
options: invalid server address format: parse ":": missing protocol scheme
```

The Trivy server field held `":"`. Nothing validated it: the value went straight
from `m.trivyServerInput.Value()` into `--server` and into `config.yaml`.

Three things compounded, and the middle one is the reason this was worth
recording rather than just fixing:

- **A `:` typed into that field is a character, not the command line.** That is
  correct and deliberate — §3.7 records that the field's placeholder is
  `https://trivy-server:4954`, so it *has* to accept two colons to hold a valid
  value, which is precisely why `alt+:` exists. Pinned by
  `TestAColonTypedIntoTheTrivyServerFieldIsACharacter`, so nobody "fixes" the
  wrong end of this.
- **The field is persisted on every option toggle** (`saveOptionsToConfig` calls
  `config.Save`), so one keystroke reached the config file.
- **Every view reads it from there** — `oci_resources/images.go:87` and
  `workspaces/actions.go:158` both take `config.Scan.TrivyServer` — so a field
  the user only ever saw in the security view broke image scans and workspace
  scans too, until the value was found by reading the YAML.

**Fix**, in the three places that each own part of it:

- `serverAddr` in `internal/scan/trivy_args.go` trims the value, treats
  all-space as *unset*, and refuses anything that is not an absolute URL —
  checking `Hostname()` rather than `Host`, because `http://:` parses with a
  host of `":"` and no hostname. All three builders (`trivyArgs`,
  `trivyMisconfigArgs`, `sbomArgs`) go through it, so no stage can be the one
  that was not checked.
- The message names the setting rather than the parser:
  `trivy server address ":" is not a URL (want http://host:port) — fix or clear
  scan.trivy_server`. That is what makes it actionable from the OCI images view,
  where there is no field to look at.
- The security form refuses to start a scan while the address is unusable
  (`scan.ValidateTrivyServer`, sharing the builders' rule so the two cannot
  disagree), and trims both the server and the Gitleaks config path before
  persisting them.

Deliberately not done: silently dropping an unusable address, or rewriting the
config on load. The user asked for client-server mode; running locally instead
without saying so is the same class of quiet substitution as D20.

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

D12–D14 sit in `internal/ui/oci_resources` and are cheap on their own, but
§3.8 rewrites the code path each of them lives in. Fix them **as part of** that
work rather than ahead of it, and write each one's test inverted first, per the
pattern above. D14's inverted test now exists.

**D21** also sits in that package but is **independent of §3.8** and should not
wait for it. It is three lines of dead code with its invariant already pinned,
so it belongs in whatever next touches `connectivity_form.go`.

D20 and D22 are fixed — see §1.1.

D15–D19 were found by the phase 5 pass, were unrelated to §3.8, and are fixed —
see §1.1. Each had been recorded with an inverted test asserting the broken
behaviour; those tests are what failed when the fix landed, and each has been
turned around to assert the fixed behaviour instead.

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

Pinned inverted by `TestAGroupMembersFilterLabelIsStillARawURL`, which asserts
the raw URL today and asserts the configured registry's alias alongside it as
the contrast.

**D21 — an unreachable focus clamp in `ConnectivityTestForm`.** Dead code, not a
user-visible defect, and the same shape as D5 in `CreationForm`.

The `left` and `right` handlers (`connectivity_form.go:242`, `:253`) clamp the
focus after cycling the test type, guarding against the field count shrinking
from four to three when the port field disappears. It cannot fire: both branches
are inside `if f.focusedField == cFieldType`, so `focusedField` is 1, and
`numFields()` is never below 3.

Fix it the way D5 was: delete both clamps and record why they cannot fire, so
they are not reintroduced defensively. The invariant they were guarding is
already pinned by `TestCyclingTheTypeNeverStrandsTheFocus`, which passes before
and after.

---

## 2. Technical debt

### Test coverage

**80.7 % overall — the agreed 80 % target is met.**

| Phase | Scope | Status |
|---|---|---|
| 0 | `internal/ui/testutil` Bubble Tea harness | **done** (100 %) |
| 1 | Leaf components and pure helpers | **done** except the `ui/theme` complement (~55 stmts) |
| 2 | Mid-size view state machines (`status`, `containers`, `dashboard`, `gitlab/auth`) | **done** |
| 3 | Large views (`workspaces`, `explorer`, `security`, `netdiag`) | **done** |
| 4 | `ui/oci_resources` | **done** — 43.4 %, the rest deferred to phase 6 |
| 5 | Router and I/O seams (`app`, `scan`, `docker`) | **done** — `app` 90.5 %, `scan` 99.2 %, `docker` 65.4 % |
| 6 | Remainder to reach 80 % | **done** — 73.8 % → 80.7 % |

**Phase 5 was complete when phase 6 began.** The three packages needed three
different seams: `app` needed only a constructor that does not read the
terminal, `docker` needed `dockerRunner`, and `scan` needed `commandRunner` plus
pure argument builders. None of them needed a mocking library.

**Phase 6 was one package.** `internal/ui/oci_resources` held 1 509 of the
2 900 uncovered statements in the project, so it was the only one that had to
move: 43.4 % → **73.0 %**, which carried the total from 73.8 % to 80.7 % on its
own. It was left at 43.4 % in phase 4 on the grounds that the rest "wants the
seam phase 5 builds"; that turned out to be half right. What `commands.go`
needed was not the `dockerRunner` seam — which is unexported and therefore
unreachable from this package — but the *other* technique phase 5 produced.

**A fake tool on PATH reaches further than a seam does.** `internal/scan`
installed copies of the test binary as `trivy` and `docker` to steer detection;
the same trick covers `commands.go` end to end, including the error handling in
`internal/docker` underneath it, with no export added anywhere. One extension
was needed: a single fake answers many different commands here — `docker image
ls` and `docker network inspect` reach the same file — so the reply is selected
by **the longest matching invocation prefix** (`faketool_test.go`) rather than
being fixed for the process. A second env var makes each fake append its
invocation to a file, which is what lets a test assert *what docker was asked*;
that is how the "untagged images are scanned by ID, cached by name" rule is
pinned.

The registry half needed no seam at all: every entry point takes the base URL as
an argument, so `httptest` covers the tag search, the bearer-token exchange and
Nexus group detection. `fetchDockerHubTagsMeta` is the one exception — it builds
a `hub.docker.com` URL itself — and swapping `ociHTTPClient` for one whose
transport rewrites the host covers it without changing production code.

`tea.Sequence` had to be taught to `testutil.Msgs`. `tea.Batch` answers with the
exported `tea.BatchMsg`, but the sequence equivalent is unexported, so a
sequenced command reported as one opaque message and the commands inside it
never ran — which is why `scanOneImageCmd` sat at 4.3 % with tests around it.
Its underlying type is `[]tea.Cmd`, so reflection recovers them without
depending on the name (`testutil.sequenced`). That is what made the scan
commands testable, and D20 is what fell out of testing them.

What is deliberately left uncovered in `oci_resources`, at 73.0 %: the
launch-form renderers and the remaining `keys.go` / `view.go` branches. Those
are layout, and pinning them means pinning pixels.

The packages still below target are `internal/registrymgr` (18.5 %),
`internal/oci` (37.3 %), `internal/status` (64.8 %), `internal/docker` (65.4 %)
and `internal/cache` (71.3 %) — 470 uncovered statements between them, none of
which the 80 % figure needs. Two are worth doing on their own merits rather than
for the number: `registrymgr`, because §3.8 step 4 rewrites `CanHandle`, and
`oci`, whose gap is the registry HTTP paths that need a manifest-plus-gzipped-
layer fixture rather than the single-response stubs used so far.

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
| `internal/credentials` | 29.5 % | **95.9 %** (98.2 % before §3.9 doubled the package) |

`internal/oci` stops at 37.3 % because the remaining statements are registry HTTP
paths (`DownloadTemplate`, `listCatalog`, `ListTemplates`) that need a fuller
`httptest` fixture — a manifest plus a gzipped layer — rather than the
single-response stubs used so far.

Phase 5's blocker is cleared for `docker`: the package now routes every CLI
invocation through the `dockerRunner` seam in `internal/docker/exec.go`, so tests
drive argument building and output parsing against canned output.

`internal/scan` (0 % → **99.2 %**) closed the phase, and it is the package where
the three-step order earned its keep most visibly. The split extracted
`commandRunner` and the pure argument builders — which is what closed D19 — and
the completion pass immediately failed on a defect the split had just
introduced: the report was read before `cmd.Wait()`, so every scan came back
empty (§1.1). The seam existed for a full commit before anything used it, and
that gap is exactly where the defect lived.

Two techniques from it are reusable:

- **The test binary stands in for the tool.** `TestMain` notices a set of
  environment variables and, instead of running the suite, behaves as a scanner
  does — a report on stdout, progress on stderr, a chosen exit code. That is what
  covers the *production* runner (`cliRunner`) rather than only the code above
  the seam, with no trivy or gitleaks installed and no compiler at test time.
- **Detection is steered through `PATH`.** `CheckDependenciesWithImages` probes
  the machine, which is precisely what a test must not do. Copying the test
  binary into `t.TempDir()` as `trivy`, `gitleaks` or `docker` and pointing
  `PATH` at it makes every branch reachable and deterministic — binary preferred
  over image, image absent, daemon unreachable, version unreadable. A per-argument
  refusal knob is what separates `docker images` from `docker run` when both are
  the same fake.

Five statements are left uncovered and stay that way: two error paths in
`AddToGitleaksIgnore` that need an injected filesystem, and a `StderrPipe`
fallback that `Run` cannot reach. Covering them costs more structure than the
branches are worth.

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

Phase 4, `internal/ui/oci_resources`, complete: 0 % → **43.4 %**, with the
surface pass at 25.1 % and the split leaving it unchanged to the statement.
Phase 6 took it the rest of the way, to **73.0 %**.

It was the one package that stopped short of the 80 % target, and deliberately.
What remained was `commands.go` (every `docker` invocation),
`connectivity_form.go` and the launch-form renderers — roughly 1 200 statements
judged at the time to want the seam phase 5 builds rather than more view tests.

Worth correcting, because the reasoning was half wrong and the correction is
reusable: `connectivity_form.go` (196 statements, 0 %) needed **nothing at
all**. It is a self-contained form whose only I/O is one command it returns and
never runs, so it was testable the whole time and was skipped by association
with the files around it. Judge a file by its own dependencies, not by the
package it sits in.

The three-step order held here too, on the largest package of the lot: 6 039
lines across eleven files, of which `update.go` (1 707) and
`registry_browser.go` (918) were the last two over the ceiling. The surface pass
was 25.1 %, well below the 52–60 % the phase 3 packages reached, and that turned
out not to matter: what a split needs is a net under *the code being moved*, and
`update.go` was at 60/86 functions when it was cut. Judge the surface pass by
the target file, not by the package total.

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

The host secret store added by §3.9 has a seam already, supplied by the library:
`keyring.MockInit()` swaps `zalando/go-keyring`'s provider for an in-memory one
and `MockInitWithError` for one that fails, which is how "no D-Bus session on a
headless box" is tested on a developer machine that has a working keychain. It
is a package-level global, so no test in `internal/credentials` may run in
parallel — the cost of the seam being free.

### Files over the 800-line ceiling

The project's own coding rules cap files at 800 lines. **None now exceeds it.**

`internal/app/app.go` (1552 lines — the 1333 recorded earlier was stale) was the
last one. It was split into `keys.go`, `command_line.go`, `context.go`,
`theme.go`, `gitlab.go`, `scan_details.go`, `selection.go`, `help_overlay.go`
and `view.go`; the largest is 242 and `app.go` itself is 337. Unlike the earlier
splits this one was not purely mechanical, and the coverage figure moved with it
(72.4 % → 74.7 %): the two `maybe*` handlers, the two cached-result paths, the
two delegated-scan handlers and the two picker overlays were each one function
duplicated twice, and folding them together removed statements rather than
covering them. `New()` also gained a `newWithSize(cfg, w, h)` seam so the
constructor can be exercised — `New` itself reads the terminal size from
`os.Stdout` and panics when there is none, which is always the case under
`go test`.

`internal/ui/oci_resources/update.go` (1707 lines — the 1556 recorded earlier
was stale) was split into `keys.go`, `images.go`, `resources.go`,
`registries.go`, `results.go`, `launch.go`, `table.go`, `layout.go` and
`browser_bridge.go`; `update.go` itself is 364. `registry_browser.go` (918) was
split into `browser_keys.go`, `browser_tags.go`, `browser_state.go` and
`browser_view.go`; it is 243. The largest file in the package is now
`commands.go` at 598, which was already under the ceiling.

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

It held for `internal/app` too (9.3 % → 72.4 % → split → 90.5 %), with one
qualification worth recording: the coverage figure moved across that split, from
72.4 % to 74.7 %. That is not drift in the tests — it is the only split so far
that also deduplicated, and removing a duplicated branch removes uncovered
statements. When a split is purely mechanical the figure should still be
identical to the statement; when it is not, say which it was.

The router is where this pass paid for itself. Four of the five defects it found
(D15–D18) are invisible from any single view: `esc` swallowed before it is
forwarded, a documented command the parser rejects, a completion catalogue that
has drifted from the parser, a header that overflows its window. A view's own
tests drive its `Update` directly and so never see the router at all. All five
are now fixed (§1.1); the `esc` one turned out to be holding two views' notion of
"editing" hostage, which no view could have reported on its own either.

### Race detector cannot run locally

`mise run test-race` needs cgo and therefore a C compiler on `PATH`. Without one
it fails with `cgo: C compiler "gcc" not found`. Bubble Tea `Cmd`s run
concurrently, so this is the check most likely to catch a Rule 110 violation.

**Now covered by CI.** `.github/workflows/ci.yml` runs `mise run test-race` on
every push and pull request, on `ubuntu-latest`, which has a toolchain. The first
run reported no data race across all 17 packages.

That is a baseline, not a clean bill of health: the detector only sees code the
tests actually execute, and coverage is 80.7 %. Rule 110 violations in untested
paths remain invisible. The two efforts compound, so this is an argument for the
coverage phases rather than a substitute for them.

`internal/scan` is the package the detector has most to say about, since
`Scanner.Scan` is the only place in the application that fans out to concurrent
goroutines writing one shared result. Its tests now drive all five stages at
once, so that fan-out is under the detector for the first time — but only on CI,
which is where the confirmation has to be read.

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
- A per-member `credentials` override is **not storable today**. It would need a
  secret keyed by slug rather than by registry URL, which `docker login` cannot
  represent.

  §3.9 changes what this costs, without changing the conclusion. DevDesk now has
  a store — the host's, keyed by whatever string it likes — so a slug-keyed
  password is no longer unstorable in principle, and it would not put plaintext
  anywhere. What it would still do is take DevDesk out of `docker login`'s model
  and into maintaining its own registry auth, for a case (§3.8) that has not been
  shown to exist. `RegistryItem` still has no password field by construction, and
  that is still the right default.
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

#### Sequencing — **prerequisite met**

This lands in `internal/ui/oci_resources`, and the rule was: surface tests, then
split, then complete coverage, and only then the feature. All three are done.
Phase 4 split the package (no file is over the 800-line ceiling; the former
822-line `registry_browser.go` is 243) and phase 6 finished the coverage, so
**§3.8 is unblocked and can start on the feature directly.**

What is under it now, in the files this feature rewrites:

| File | Coverage | Pinned by |
|---|---|---|
| `browser_keys.go` | 78.1 % | the search form and the tags keymap |
| `browser_tags.go` | ~97 % | sort, both filters, tag actions |
| `browser_view.go` | ~99 % | the grouped form, the results table, the filter bar |
| `commands.go` | 88.0 % | group detection against `httptest`, incl. the management-URL credential lookup |

Two of those tests are the inverted kind and are meant to fail when this feature
lands: `TestAGroupMembersFilterLabelIsStillARawURL` (D14) and, in
`registry_http_test.go`, `TestManagementCredentialsAreLookedUpByHostAlone`,
which pins today's credential behaviour that step 2 changes. Turn both around
rather than deleting them.

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

### 3.9 Every secret goes to a host secret manager, and radio buttons go away — **done**

No secret DevDesk holds is written to a file DevDesk owns. Tokens and registry
passwords go to the Windows Credential Manager, the macOS Keychain or a Secret
Service implementation on Linux, through one storage and one only.

#### What was wrong

Three of the five storage paths were plaintext on disk, and the option the UI
labelled "secure" was one of them.

| Secret | Destination | Protection |
|---|---|---|
| Forge token | `~/.devdesk/credentials-<ctx>.json` | **plaintext JSON**, 0600 |
| Forge token | `gitlab.token` in `contexts/<ctx>/config.yaml` | **plaintext YAML** |
| Forge token | git credential helper | whatever the helper does |
| Registry password | `~/.docker/config.json` via `docker login` | whatever Docker's `credsStore` does |
| Registry password | `registry.password` in `config.yaml` | **plaintext YAML**, read at `explorer/create.go:43,132` |

`ChainStorage.Save` wrote to **every** storage in the chain, and all three
construction sites built it as `NewChainStorage(FileStorage,
GitCredentialStorage)`. Choosing **"Save to Git Credential Manager (secure)"**
therefore stored the token in the credential manager *and* in
`~/.devdesk/credentials-<context>.json` in plaintext. Reads made it worse:
`FileStorage` was first and `ChainStorage.Load` returned the first hit, so the
plaintext file was authoritative and the credential manager was never consulted
while it existed. The secure backend was decorative in both directions.

The other option was no better in a different way: **"Save token to config file
(less secure)"** set `saveToHelper = false`, so nothing reached the chain and
the token landed only in `config.yaml`. Both options put the token in plaintext;
the "secure" one did it twice.

And logout did not clean up: `handleLogoutComplete` cleared
`m.config.GitLab.Token` in memory with no `config.Save` behind it, so the token
survived in `contexts/<ctx>/config.yaml`.

#### The distinction that decided the design

Delegating to `git credential` delegates to **whatever helper git happens to be
configured with**. If that is `store`, the token lands in `~/.git-credentials`
in plaintext — the same failure, relocated. Only `manager` (GCM),
`osxkeychain`, `libsecret` and `wincred` reach a real OS store. "Goes through
git credential" is not the same claim as "encrypted at rest", and the
requirement was the second one.

So the host store is the primary path and git credential is kept as an explicit
alternative, for users who want their tokens where GCM already puts everything
else. Only the first can promise what the requirement asks; the second is the
pragmatic option and stays reachable without becoming the default.

#### What shipped

**`KeyringStorage` over `zalando/go-keyring`** (`credentials/keyring.go`), one
implementation covering all three platforms with no cgo: wincred on Windows, the
`security` binary on macOS, D-Bus Secret Service elsewhere. Entries are filed
under service `devdesk`, account `<context>/<url>`, so two contexts pointing at
the same host keep separate secrets — the property `GitCredentialStorage` needed
`credential.useHttpPath` to get (§1.1).

`KeyringAvailable()` probes with a **read** of an account that is never written.
A miss proves the backend answered and simply holds nothing; anything else is
the backend being absent. It runs on every launch, including on machines where
the store turns out to be unusable, so it must not be able to leave anything
behind.

**`Select(context, preference)`** (`credentials/select.go`) returns a
`Selection{Storage, Backend, Detail}` and picks exactly one destination — host
store, else git credential, else memory. Writing to several at once is what
produced the defect above, so the chain is gone rather than reordered.
`app.secret_backend` pins the head of that list (`auto`, `keyring`,
`git-credential`). A pinned backend that turns out to be unreachable falls
through to memory rather than silently to the other one: someone who asked for
the keyring should not be handed a git helper without being told.

`gitHelperUsable()` refuses `store` by name and refuses an unset helper, and
accepts everything else. There is no list of good helpers to check against —
enumerating them would only mean rejecting the next one someone installs.

**The fallback is worse on purpose.** With no store reachable the answer is
`MemoryStorage` — session-only, re-authenticate each launch — and the auth view
says so in `ColorWarn`. That is deliberately worse UX than a file, and that is
the point: a fallback that is silently insecure is how the "secure" option came
to exist.

**`FileStorage` and `ChainStorage` are deleted.** `MemoryStorage` gained a mutex:
it is now one instance shared by the Cmd goroutines of every view, which the
per-call construction it used to get had hidden.

**`GitLabConfig.Token` and `RegistryConfig.Password` are out of the schema.**
Parsing and ignoring them would have left the secret on disk forever for every
existing user, so `MigrateLegacySecrets` runs on load and on every context
switch: it reads the two fields straight from the YAML —
`config.ReadLegacySecrets` — moves them into the store, and deletes them from
the file. The rewrite edits the parsed YAML tree rather than round-tripping
through `Config`, which would rewrite every key including the defaults
`applyDefaults` filled in. A secret the store refused to take stays in the file;
losing it would be worse than leaving it. A secret with no URL has no key to be
filed under and no host it could be used against, so it is dropped — and the
auth view reports every one of these outcomes in words.

`Auth.Authenticate(url, token)` lost its `saveCredentials` parameter and always
stores; `AuthenticateOnly` is the auto-login path, whose token already came out
of the store. Logout deletes from the store, and there is no longer a config
copy to forget about — which is what closes the last defect above.

`registry.password` had no UI to set it and one reader, `explorer/create.go`.
That reader now asks the store, keyed by the registry URL, and treats a miss as
"anonymous registry" — which is the common case.

**The radios are gone**, with `SaveToHelper` / `SaveToConfig`, the `saveOption`
field, `theme.RenderRadioButton` (its only two call sites), and the `ctrl+s` /
`ctrl+f` shortcuts and their help section. The form went from five fields to
three, now named `fieldURL` / `fieldToken` / `fieldSubmit` instead of the
integers 0–4. Removing the `case " "` had a side effect worth recording: space
could not previously be typed into the URL or token field, because the radio
handler swallowed it before the input saw it.

Rules 120 and 132 in `.claude/rules/tui-forms.md` now say cycle fields are the
only control for a closed set, whatever its size, and that checkboxes remain for
independent booleans.

#### What a user has to do

Nothing, on any platform. The migration is automatic and the auth view reports
what it did. Two consequences are worth knowing:

- On a headless Linux box with no D-Bus session and no git helper, DevDesk now
  asks for the token on each launch instead of reading it from a plaintext file.
  That is the intended trade, and it is stated on screen rather than inferred.
- The entry is visible in the host's own UI (`Credential Manager`, `Keychain
  Access`, `seahorse`) under `devdesk`, which is where a user should be able to
  revoke it.

#### One rough edge, left rough on purpose

On Linux, `go-keyring` calls `Unlock` on the login collection before every read,
including the availability probe. On a desktop whose keyring unlocks with the
session password — the default on GNOME and KDE — that returns immediately. On
one configured with a separately-locked keyring, it raises the agent's unlock
prompt at startup, before the TUI has drawn its first frame, and blocks until it
is answered.

Wrapping the probe in a timeout would make this worse, not better: it would
leave a prompt on screen that nobody is waiting on, and answer "no store
available" for a machine that has a perfectly good one — sending the user to the
memory fallback because their keyring was locked. Every other client of the
Secret Service behaves the same way, including git's own `libsecret` helper. The
prompt is the user's keyring policy working; suppressing it is not DevDesk's
call to make.

Windows and macOS have no equivalent: `CredRead` is silent for the current user,
and `security find-generic-password` only prompts for items the calling binary
is not on the ACL of — which, for items DevDesk itself wrote, it is.

---

### 3.10 An inference-backed explainer for network diagnostics

Netdiag runs the tests but leaves the interpretation to the user. The first —
and for now only — AI feature is an **explainer over diagnostic results that
DevDesk already holds**. It sends a few kilobytes of structured facts, needs no
new privilege, and touches no packet payload.

#### Settled

| # | Question | Decision |
|---|---|---|
| 1 | Scope of v1 | **Explainer only.** No capture. tcpdump is a later input to the same explainer, not part of this. |
| 2 | Local server lifecycle | **DevDesk consumes an endpoint.** It never starts, stops or supervises an inference server. |
| 3 | Protocol | **One OpenAI-compatible client** (`POST /v1/chat/completions`). Covers ollama, llama.cpp, vLLM, LM Studio and hosted providers. No second native client. |
| 4 | Redaction | **Nothing sensitive reaches a model, local or remote.** Not a per-provider policy — one rule, no exception for `localhost`. |
| 5 | Snaplen | Headers-only is the default **when capture arrives**. Out of scope for v1 by decision 1. |
| 6 | Provider scope | **One per context.** |

Decisions 2 and 6 cost nothing. Consuming an endpoint means the provider is a
URL, a model name and a token — a `AIConfig` block in the context's
`config.yaml`, which is per-context by construction. The token goes to the store
from §3.9 like any other; it must never land in the YAML. The GPU question that
motivated hosting (Docker on macOS runs a Linux VM with no Metal access, so
unified memory is unreachable from a container) disappears with it: the user
runs `ollama serve` natively, or a container, or nothing, and DevDesk does not
care.

#### The hard part is decision 4, and Presidio only half-answers it

Reference: [Docker agent PII protection with
Presidio](https://k33g.org/p/20260716-docker-agent-pii-protection-presidio) —
an analyzer/anonymizer pair behind HTTP, hooked on `before_llm_call`, replacing
detected spans with typed tokens.

Two things transfer and one does not.

**What does not transfer: the entity set.** Presidio detects *PII* — names,
emails, phone numbers, credit cards, IBANs, national IDs, IP addresses. What
DevDesk holds is mostly not PII: internal hostnames, private ranges, resolver
addresses, listening sockets and process names, registry and forge URLs,
Gitleaks matches. `IP_ADDRESS` is the only real overlap. A DevDesk payload could
pass Presidio clean while still describing the whole internal network. Presidio
is therefore a **second net, not the mechanism**.

**What transfers: fail-closed, and the deterministic layers.** The post notes
its own default is fail-open and that production needs `PII_FAIL_CLOSED=1`.
Given §3.9, fail-closed is the only acceptable mode here: if the redactor is
unreachable, nothing is sent, and the user is told why. And the post's two
non-NER layers — a deny-list and structural rules by column — are the parts
that actually caught things reliably. That is the direction to build in.

#### Allow-list construction beats scrubbing

The post scrubs because it hooks arbitrary agent traffic and cannot know what is
in it. DevDesk is not in that position: it **assembles the payload itself** from
`m.results`, which is already typed and structured. So the payload should be
built field by field from an explicit allow-list, not produced as a blob and
then cleaned.

The difference matters: **a field never included cannot fail to be redacted.**
Scrubbing is a filter that can miss; construction is a whitelist that cannot.
Presidio then runs over the assembled payload as a check on the construction,
and any hit is a bug in the allow-list, not a routine save.

#### The tension: the sensitive data *is* the diagnostic data

Redacting addresses out of a network diagnostic destroys the diagnostic. A model
told `[IP_REDACTED]` resolves to `[IP_REDACTED]` can conclude nothing, and the
answer that comes back is unreadable.

The resolution is **consistent pseudonymisation that preserves the analytically
relevant class**, not blanket redaction:

| Real | Sent | Preserved |
|---|---|---|
| `api.corp.internal` | `host-1` | identity across the payload |
| `10.2.3.4` | `private-a` | RFC1918, and same-subnet relations |
| `203.0.113.9` | `public-b` | routable, distinct from private |
| `10.2.3.7` | `private-c` | same /24 as `private-a` |

The hypotheses the model is asked to rank depend on structure — private vs
public, same subnet or not, resolves or not, port open or filtered, which TLS
stage failed — never on the literal octets. So this loses nothing. "host-1
resolves to private-a but hop 5 is public-c, so the route leaves your network"
is exactly as useful as the version with real addresses.

The mapping stays in memory, and the response is **restored locally before
display**, so the user reads real names. The blog post does not do this
round-trip — its tokens are one-way — but DevDesk needs it, because unlike a
CSV of customers its payload is *entirely* made of identifiers.

#### Confirmation before send

The assembled, pseudonymised payload is rendered in the viewport before it
leaves, for every provider. Decision 4 says local and remote are treated alike,
so there is no "trusted endpoint" shortcut. This costs one keypress and is what
makes the feature auditable without reading the source.

#### Bubble Tea shape

Streaming is the only delicate part. Rule 110 forbids mutating the model inside
a `Cmd`, so the pattern is a channel plus a `Cmd` that reads one chunk and
returns a `streamChunkMsg{gen, text}` which re-arms itself — the same shape as
the spinner, reusing the generation counter already in `run.go:33` to discard a
superseded stream.

Cancellation is mandatory, not optional: a local model on CPU can take minutes.
A `context.CancelFunc` lives in the model and `esc` cancels. **D13 is the
warning** — a state the user cannot leave while something resolves is a defect
this repository already has once.

No new view and no command. The explanation is a results tab under the table
(Rule 123), triggered by a key on the results screen. A dedicated view turns
this into a chat product, which is not what is being asked for. With no provider
configured the shortcut is absent rather than erroring (Rule 130), and all
strings are English US (Rule 129).

#### Sketch of the work

1. `internal/ai`: config block, an OpenAI-compatible client behind a small
   interface so tests inject a fake — the `runner` indirection in
   `internal/docker` is the precedent — and the token read from the §3.9 store.
2. The payload builder: allow-listed fields out of `m.results`, plus the
   pseudonymiser and its inverse. Table-driven tests, like the existing
   `dns_formatter` and `traceroute_formatter` parsers. **This is the feature; do
   it first and it is testable with no endpoint at all.**
3. The prompt: observed facts only, and an instruction to name which test each
   claim rests on. The raw results stay on screen next to the explanation —
   the narrative never replaces the data.
4. Streaming, cancellation, and the results tab.
5. The confirmation pane.
6. Optional and last: Presidio as a fail-closed second net over the assembled
   payload, behind a config flag. Two containers and a spaCy model is heavy
   for a few kilobytes of already-structured text, and step 2 is what actually
   provides the guarantee.

Later, and explicitly not now: capture as an additional input (bounded by `-c`
and `-G`, `-s 96` by default so payloads cannot be captured at all), and the
deterministic flow summariser that would have to precede it — a pcap does not
fit in a context window, and once the summariser exists it answers most of the
question without a model. `nicolaka/netshoot` (`config.go:218`) already ships
`tcpdump` and `tshark`, and the privileged host-namespace runner exists
(`ports.go:36`, `topology.go:47`), so the missing piece is the analysis, not the
plumbing.

Adjacent candidates, ranked, none settled: Trivy remediation (§3.2 — low
sensitivity, but any suggested base-image bump must be verified by a re-scan,
never trusted); Gitleaks triage (highest value since false positives dominate,
highest risk since the payload *is* the secret — possibly viable by sending rule
name, path and entropy with the match withheld); container log explanation
(logs carry env vars and DSNs routinely).

---

## 4. Existing plans

Detailed plans live in `.claude/plans/`. One is referenced directly from the old
`todo.md` and is still outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability.
