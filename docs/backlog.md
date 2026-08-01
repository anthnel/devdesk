# DevDesk Backlog

**Last Updated:** 2026-08-01

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

None of these are fixed. Each is reachable from the current code unless noted.

### D1 — `ReportModal.View()` emits invalid UTF-8

`internal/ui/components/report_modal.go:136-139`

Long entries are truncated with `item[len(item)-52:]`, which slices **bytes**, not
runes. When the resulting offset falls inside a multi-byte codepoint the rendered
output is no longer valid UTF-8 and the terminal shows replacement characters.

Reachable with any non-ASCII repository path. Pinned by
`TestReportModalViewTruncationSplitsMultibyteRunes`, which `t.Skip()`s with
instructions once the truncation becomes rune-aware.

**Fix:** truncate on `[]rune`, and measure width with `runewidth` rather than
`len()`.

### D2 — The "permanent delete" checkbox is documented as locked but is not

`internal/ui/components/delete_confirm_modal.go:35` and `:99-105`

`NewDeleteConfirmModalPermanent` pre-checks *Immediate deletion* and its doc
comment states the box is non-modifiable ("pré-cochée et non modifiable"). Nothing
enforces that: `space` and `enter` on the checkbox still toggle it.

A project already marked for deletion can therefore be sent through a
grace-period delete that the caller does not expect. Pinned by
`TestDeleteConfirmModalPermanentCheckboxIsStillToggleable`.

**Fix:** carry a `locked` flag on the modal and make the toggle a no-op when set,
or drop the claim from the doc comment.

### D3 — `wrapInputLines` loops forever when `wrapWidth <= 0`

`internal/ui/components/wrapped_input.go:135`

With a non-positive wrap width the loop makes no progress: `breakAt` collapses to
`start`, an empty line is appended, and `start` never advances — the line slice
grows until the process runs out of memory.

**Not reachable today.** Every caller passes 60, 80 or 100 per Rule 133. Latent
only, which is why no test covers it (the test would hang).

**Fix:** guard the entry point — return a single line when `wrapWidth < 1`.

### D4 — `DeleteConfirmModal` uses `Tab` for field navigation

`internal/ui/components/delete_confirm_modal.go:89-97`

`tab` / `shift+tab` cycle checkbox → Yes → No. Rule 135 reserves those keys for
tab switching and assigns field navigation to `↑ / ↓` exclusively.

Cosmetic in isolation, but it is the kind of inconsistency Rule 135 exists to
prevent. Changing it alters muscle memory, so it needs a deliberate call rather
than a drive-by fix.

### D5 — Unreachable focus clamp in `CreationForm`

`internal/ui/components/creation_form.go:221-223`

After cycling the resource type, the code clamps `focusedField` down to
`maxField()`. The surrounding branch only runs when `focusedField == 0`, and
`maxField()` is never below 4, so the clamp can never fire.

Dead defensive code. Harmless, but it implies a state transition that cannot
happen and is misleading to read.

### D6 — `wordWrap` measures bytes, not runes

`internal/ui/help/help.go:123`

Word widths come from `len(word)`, which counts bytes. Accented text therefore
wraps earlier than its rendered width requires: two 5-rune words occupy 11
columns but 21 bytes, so a width of 12 splits them apart.

Low impact today — Rule 129 keeps help content in English US — but it is the same
byte-vs-rune class as [D1](#d1--reportmodalview-emits-invalid-utf-8). Unlike
[D3](#d3--wrapinputlines-loops-forever-when-wrapwidth--0), `wordWrap` does guard
a non-positive width, and that guard is covered by a test so it does not get
"tidied" away.

Pinned by `TestWordWrapMeasuresBytesNotRunes`.

**Fix:** measure with `utf8.RuneCountInString`, or `runewidth.StringWidth` if
double-width glyphs ever appear.

### D7 — `extractTarGz` keeps parent references in archive paths

`internal/oci/oci.go:262`

Extraction strips leading `./` and `/` but does not reject `..`, so an archive
member named `../../etc/passwd` survives as a map key containing parent
references.

**Not exploitable today.** The sole consumer, `applyTemplate` in
`internal/ui/gitlab/explorer/model.go:1053`, turns the map into GitLab commit
actions rather than writing to disk, and the server validates the paths.

Recorded because the hazard is latent: any future caller that writes these keys
under a target directory inherits a Zip Slip. Pinned by
`TestExtractTarGzPreservesParentTraversalInKeys`.

**Fix:** reject entries whose cleaned path escapes the root, before returning
them.

---

## 2. Technical debt

### Test coverage

Currently **13.6 %** overall; the agreed target is 80 %, which needs roughly
**+7 700 covered statements** over today's ~1 570.

Phased plan, with the harness and most of phase 1 delivered:

| Phase | Scope | Status |
|---|---|---|
| 0 | `internal/ui/testutil` Bubble Tea harness | **done** (100 %) |
| 1 | Leaf components and pure helpers | **mostly done** — see the table below; `credentials`, `gitlab` and the `ui/theme` complement remain (~255 stmts) |
| 2 | Mid-size view state machines (`status`, `containers`, `dashboard`, `gitlab/auth`) | pending (~1 240 stmts) |
| 3 | Large views (`workspaces`, `explorer`, `security`, `netdiag`) | pending (~2 470 stmts) |
| 4 | `ui/oci_resources` | pending (~1 995 stmts) |
| 5 | Router and I/O seams (`app`, `scan`, `docker`) | pending (~1 360 stmts) |
| 6 | Remainder to reach 80 % | pending (~400 stmts) |

Phase 1 progress:

| Package | Before | Now |
|---|---|---|
| `internal/ui/testutil` | — | **100 %** (new) |
| `internal/ui/shortcut` | 0 % | **100 %** |
| `internal/ui/help` | 0 % | **96.7 %** |
| `internal/ui/components` | 0 % | **79.8 %** |
| `internal/oci` | 0 % | **33.6 %** |
| `internal/credentials` | 29.5 % | unchanged |
| `internal/gitlab` | 7.5 % | unchanged |

`internal/oci` stops at 33.6 % because the remaining statements are registry HTTP
paths (`DownloadTemplate`, `listCatalog`, `ListTemplates`) that need a fuller
`httptest` fixture — a manifest plus a gzipped layer — rather than the
single-response stubs used so far.

Phase 5 requires a refactor before it can start: `internal/docker/client.go`
shells out to the Docker CLI directly, so it needs an injectable execution seam
to be testable.

### Files over the 800-line ceiling

The project's own coding rules cap files at 800 lines. Eight exceed it:

| File | Lines |
|---|---|
| `internal/ui/security/model.go` | 1785 |
| `internal/ui/oci_resources/update.go` | 1556 |
| `internal/app/app.go` | 1333 |
| `internal/ui/gitlab/explorer/model.go` | 1214 |
| `internal/ui/workspaces/model.go` | 1133 |
| `internal/docker/client.go` | 1069 |
| `internal/ui/netdiag/model.go` | 995 |
| `internal/ui/oci_resources/registry_browser.go` | 822 |

This interacts with the coverage work: writing several thousand statements of
tests against these files before splitting them freezes their current structure.
Decide the order deliberately.

### Race detector cannot run locally

`mise run test-race` needs cgo and therefore a C compiler on `PATH`. Without one
it fails with `cgo: C compiler "gcc" not found`. Bubble Tea `Cmd`s run
concurrently, so this is the check most likely to catch a Rule 110 violation —
it should run in CI even if local machines lack a toolchain.

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

---

## 4. Existing plans

Detailed plans live in `.claude/plans/`. One is referenced directly from the old
`todo.md` and is still outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability.
