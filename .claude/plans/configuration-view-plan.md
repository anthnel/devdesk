# A configuration view, and `security` becomes an inventory

Status: **complete.** All five phases shipped, and the SBOM removal that was
deferred until after phase 3 has landed too -- `docs/backlog.md` §3.14.

| Phase | State |
|---|---|
| 0 — `IgnoreEOL` (D26) | shipped |
| 0b — per-context scan caches | shipped |
| 0c — tool source and paths (D27) | shipped |
| 1 — configuration view | shipped |
| 2 — the inventory | shipped, alongside the form |
| 3 — deleting the form | **shipped** |
| after 3 — removing SBOM generation | **outstanding**, see `docs/backlog.md` §3.14 |

## What was decided

| Question | Answer |
|---|---|
| Scope of the config view | **Scalars only.** The ~29 simple fields. Lists — monitors, registries — stay where they are consulted. |
| Fate of `security` | **Inventory of what has been scanned.** `:sec` opens a unified images + workspaces table read from the two caches. |
| Free-target picker (`b`) | **Removed.** What gets scanned is either a known image or something under `workspaces_dir`. |
| Scope of a configuration | **Per context.** One config per context, as today; the view edits the current one and must name it. |
| Reaching a result from its row | **Preserved.** `enter` on a scanned image or repository opens the findings, `esc` returns to that row. |
| `secret_backend` changed at runtime | **Re-resolve, confirm first, migrate nothing.** See below. |

## The fact this plan rests on

The flow being proposed is already 80 % built, and nobody noticed because the
form kept standing in front of it.

```go
// internal/ui/workspaces/actions.go:143
func (m Model) getScanOptions() scan.ScanOptions { … m.config.Scan.EnableVuln … }

// internal/ui/oci_resources/images.go:79
func (m Model) defaultScanOpts() scan.ScanOptions { … m.config.Scan.EnableVuln … }
```

Byte-for-byte identical, both reading the config. `ctrl+s` on an image or on a
repository has not gone through the security form for some time. And
`NewWithPreloadedResult` already opens straight on `StateResults`.

So this is not a redesign. It is deleting the one remaining path that reads
something *other* than the config, and giving `:sec` a landing page that is not
an empty form.

---

## Phase 0 — D26: `IgnoreEOL` is dropped on two of the three scan paths

Independent of everything else, and a real defect. Do it first, alone.

`internal/ui/security/scan.go:42` is the third options builder and it is not
identical to the other two:

```go
IgnoreEOL: m.ignoreEOL,   // ← set here, and nowhere else
```

The option is persisted to `config.Scan.IgnoreEOL` by the form, and wired all
the way through to `--ignore-status end_of_life` in `scanner.go:340`. Neither
`getScanOptions` nor `defaultScanOpts` sets it. Tick "ignore EOL", scan from the
images list, the flag silently does not apply.

Three copies, one divergent — the same shape as D24 and D25, which §2 closed
nine times over in the tables.

**Fix.** One builder, and delete the other three:

```go
// internal/scan/options.go
// OptionsFromConfig is the only way scan options are built. Three call sites
// used to assemble them by hand and one of them set IgnoreEOL (D26).
func OptionsFromConfig(c config.ScanConfig) ScanOptions
```

`internal/scan` does not import `internal/config` today and `internal/config`
does not import `internal/scan`, so this direction is acyclic. Verified.

**Test that bites first.** Assert `OptionsFromConfig` carries every field of
`ScanConfig` that `ScanOptions` has a home for — a table over the field names,
so adding a tenth option to the config without plumbing it fails the test rather
than shipping. This is the class of defect, not the instance.

---

## Phase 0c — D27: the custom tool paths are never read, and the source cannot be chosen

Independent of the config view, but a prerequisite for its `scan` section being
honest about what it offers.

### The defect

`trivy_path` and `gitleaks_path` are declared, defaulted and tilde-expanded, and
**nothing reads them**:

```
config.go:119   TrivyPath string `yaml:"trivy_path"`   // declared
config.go:302   TrivyPath: ""                          // defaulted
config.go:491   c.Scan.TrivyPath = expand(…)           // expanded on load
                                                       // ← and that is all
scanner.go:207  exec.LookPath("trivy")                 // hard-coded
scanner.go:210  exec.Command("trivy", "--version")     // hard-coded
gitleaks.go:73  toolCmd{Name: "gitleaks"}              // hard-coded
```

Setting `trivy_path: /opt/trivy/bin/trivy` does nothing, silently. Somebody
thought the field mattered — it is expanded on load — and no reader was ever
written.

### The half that goes with it

`CheckDependenciesWithImages` resolves the binary first and only reaches for
Docker in the `else`. A binary on `PATH` therefore always wins, so a user who
wants the pinned image cannot have it while Trivy is installed.

"Either the image or the binary" needs a **choice**, not two fields configured
in the hope that the right one is picked.

### What ships

New per-tool source setting, `auto` by default so existing configs behave
identically:

```yaml
scan:
  trivy_source: auto      # auto | binary | image
  trivy_path: ""
  trivy_image: aquasec/trivy
  gitleaks_source: auto
```

| Value | Resolution |
|---|---|
| `auto` | today's behaviour — the binary if present, else the image |
| `binary` | `trivy_path` when set, else `trivy` from `PATH`. **Fails loudly when absent**, never falls back to Docker |
| `image` | `trivy_image`, even when a binary is on `PATH` |

The loud failure is the point. The silent fallback to Docker is what has kept
D27 invisible: scans keep working, with something other than what was asked for.

Plumbing:

- `CheckDependenciesWithImages(trivyImage, gitleaksImage)` takes only the
  images. It needs the paths and the preference — replace it with one that takes
  `config.ScanConfig`, alongside `OptionsFromConfig` from phase 0.
- `DependencyStatus` gains `TrivyBinary` / `GitleaksBinary` next to the existing
  `TrivyImage` / `GitleaksImage`.
- `GetTrivyCommand` already takes 8 positional arguments. Pass the whole
  `DependencyStatus` rather than adding a ninth — the argument list is why the
  binary path was never threaded through in the first place.

**Test that bites first.** A `trivy_source: image` config must produce a Docker
command even with a fake `trivy` on `PATH`, and `trivy_source: binary` with a
non-existent path must return an error rather than a Docker command.

## Phase 1 — the configuration view

Additive. Nothing is removed in this phase, so it ships green on its own.

### Fields

~29 scalars across five sections. Everything not listed is a list and stays put.

| Section | Fields |
|---|---|
| `app` | `theme`, `default_view`, `workspaces_dir`, `ide_command`, `terminal_command`, `secret_backend`, `log_file` |
| `gitlab` | `url`, `default_parent_group`, `default_visibility`, `clone_method`, `pull.parallel_jobs`, `pull.include_archived` |
| `scan` | the 9 option booleans, `trivy_source` + `trivy_path` + `trivy_image`, `gitleaks_source` + `gitleaks_path` + `gitleaks_image`, `trivy_server`, `gitleaks_config`, `sbom_output_dir`, `timeout`, `max_concurrent_scans`, `max_cached_reports` |
| `docker` | `network_tool_image` |
| `status` | `refresh_interval`, `timeout`, `auto_refresh` |

`secret_backend` is the field that justifies the view on its own: a documented
security decision, three values, a fallback — and the only way to change it
today is to open a YAML file.

### Layout — tabs, and the rules pick this for us

One flat form of 29 fields is unusable. Sections as tabs is not merely
convenient, it is the only shape that uses the keyboard correctly under Rule 135:

- `Tab` / `Shift+Tab` — switch section. Rule 135 reserves these **exclusively**
  for tabs, so a tabbed form is the one layout where Tab has a legitimate job.
- `↑` / `↓` — move between fields, which is what Rule 135 reserves them for.
- `←` / `→` — cycle a closed-set field (Rule 132).
- `Space` — toggle a checkbox, and only that.

Tabs render below the viewport and stay visible (Rule 123). Form gets exactly
one blank line of top padding (Rule 131).

### Widget per field kind

| Kind | Control | Fields |
|---|---|---|
| Closed set | cycle `←→` (Rule 132) | `theme`, `default_view`, `secret_backend`, `clone_method`, `default_visibility`, `trivy_source`, `gitleaks_source` |
| Boolean | checkbox, `Space` only | `auto_refresh`, `include_archived`, the 9 scan options |
| Free text | `textinput` | paths, URLs, image names |
| Integer | `textinput` + validation | `refresh_interval`, both `timeout`, `parallel_jobs`, `max_concurrent_scans`, `max_cached_reports` |

Two notes that will otherwise be got wrong:

- **`theme` is not a static list.** `theme.ListThemes()` reads a directory and
  can fail. The cycle field is built from its result at view creation; an error
  degrades to `["default"]` rather than an empty cycle.
- **Integers need a refusal, not a correction.** Follow `RegistryForm`: an
  unparseable or out-of-range value is reported and *not* persisted, rather than
  silently coerced to zero — which is exactly how `trivy_server: ":"` got into a
  config file in the first place.
- **`trivy_path` / `trivy_image` stay visible whatever the source is.** `auto`
  needs both, and dimming or hiding the inactive one would make the user believe
  their value was lost. The source field above them is what says which one wins.

### Persistence

Checkbox and cycle persist immediately. Text and integer persist on blur, and
only after validating. A validation failure keeps the field focused and reports
in the footer with the 3-second timer (Rule 128); it must not reach
`config.Save`.

No dirty state, no confirm modal, no explicit save button. This matches what the
security form already does and means there is no unsaved-changes state to get
wrong.

### Making a change take effect

Three fields change how the running application behaves: `theme`,
`workspaces_dir`, `secret_backend`. The router already has the machinery —
`reinitializeViews(cfg)`, used on context switch.

So: the view emits `ConfigSavedMsg`, the router handles it exactly as it handles
a context switch. `theme` additionally goes through `theme.ApplyTheme`, which
the `:theme` command already does.

### `secret_backend` changed at runtime — decided

Re-resolve immediately, confirm before committing, migrate nothing.

CLAUDE.md warns that the `Selection` must be reused for the lifetime of the
context, because a fresh `MemoryStorage` cannot see what the first one holds.
That trap only bites when the resolved backend *is* memory: keyring and
git-credential are backed by the OS, so a second `Select()` sees everything the
first one wrote.

And in every case, changing the backend **means** secrets now live somewhere
else. Losing sight of the old ones is the semantics of the change, not a defect.

1. `ConfigSavedMsg` carries a flag when `secret_backend` changed. The router
   calls `credentials.Select()` again, replaces `shared.State.Secrets`, then
   runs `reinitializeViews`.
2. **A confirm modal gates the change** (Rule 104, safe default "No"): the GitLab
   token and registry passwords will have to be entered again.
3. **No automatic migration between backends.**

Point 3 is the one that settles it. §3.9 started from the observation that
writing to several stores at once is exactly what let the old "secure" option
put a token in the credential manager *and* in plaintext. Copying a token from
one backend to another on change would rebuild that. `MigrateLegacySecrets` is a
one-way fix away from plaintext, not a precedent for backend-to-backend copying.

### One configuration per context

Nothing structural to build: `LoadContext` already reads `config-<name>.yaml`,
and `reinitializeViews` already drops every view on a context switch, so the
config view is rebuilt against the new file for free.

Two requirements that do not come for free:

- **The view must name the context it is editing.** Editing `workspaces_dir` in
  the wrong context is otherwise a silent mistake — the field looks the same in
  all of them. The context belongs in the view's own title line, not only in the
  application header.
- **`config.Save(cfg)` writes to the current context**, which is what it already
  does via `GetCurrentContext`. No call site changes; this is stated so nobody
  reaches for `SaveContext` with a captured name and reintroduces the bug where
  a switch mid-edit writes to the previous file.

### Registration

- `command.ViewConfiguration = "configuration"`, aliases `config`, `cfg`.
  Careful: `ctx`/`context` is already the context-switch command, and `c` is
  already `containers`.
- `createView` case, `GetHelpContent` (Rule 114), `GetShortcuts` (Rules 130,
  137, 138 — no `↑↓`, no `tab`, they are obvious).
- `InEditMode()` true whenever a text or integer field has focus.

---

## Phase 2 — `security` becomes the inventory — **shipped**

Still additive: the inventory is a new state alongside the form, which is not
removed until phase 3.

Shipped as described, in `inventory.go`, `inventory_table.go` and
`inventory_commands.go`. Three things the plan did not anticipate, all recorded
in `docs/backlog.md` §3.11:

- `ctrl+a` purges the **counts**, not the rows — the rows are the list of what
  has been scanned, so dropping them empties the view for the length of the
  scans.
- A reload has to keep an in-flight scan's marker, or a refresh landing
  mid-rescan clears the spinner.
- `InventoryScanFinishedMsg` needs routing to the security view wherever the
  user has gone, exactly as `ociresources.ImageScanFinishedMsg` already does.

`homeState` is the one field added beyond the plan; it goes with the form in
phase 3.

### Data

Both caches already expose what is needed:

```go
(*cache.ImageScanCache).GetAll()     map[string]ImageScanEntry
(*cache.WorkspaceScanCache).GetAll() map[string]WorkspaceScanEntry
```

A `scanTarget` row type carries kind (image or repo), display name, the severity
counts and the scan timestamp — decoration that lives on neither cache entry, so
it follows the `imageRow` / `workspaceRow` pattern §2 settled on. One
`datatable`, sorted by CRITICAL descending by default, `theme.TimeAgo` for the
age column (Rule 127).

```
Target                    CRIT  HIGH  Scanned
nexus/api:1.4              3     11   2 hr ago
~/workspaces/devdesk       0      2   now
```

- `enter` → `LoadImageScanResult` / `LoadWorkspaceScanResult` → `StateResults`,
  which already exists and needs no change.
- `ctrl+s` → rescan the selected target.
- `ctrl+a` → rescan all, purging the cache first (Rule 126).

### The delegation dies with the form

Today `startScan` ships options back to the originating view
(`LaunchSingleImageScanMsg`, `LaunchBatchScanMsg`) because the options lived in
the form and had to be carried to whoever would run the scan. Once options come
from the config, there is nothing to carry: `security` runs its own scan with
`scan.OptionsFromConfig` and writes to the same caches, using the
`StateScanning` machinery it already has.

Removable as a consequence: `isImageScan`, `returnToOCIImages`,
`returnToWorkspaces`, `LaunchSingleImageScanMsg`, `LaunchBatchScanMsg`, and the
`handleLaunchScan` routing in `app/scan_details.go`.

`oci_resources` and `workspaces` keep running their own scans — they are the
primary trigger points by design. It is the cross-view round trip that goes.

### `OriginView` stays — it carries navigation, not options

An earlier draft of this plan listed `OriginView` as removable. That was wrong,
and it is the mechanism behind a hard requirement: **a scan launched from a row
must be readable from that same row when it finishes.**

The round trip already works and is not touched by any phase:

```
image/repo row → enter → loadImageScanResultCmd
                       → ImageScanResultLoadedMsg
                       → openSecurityView(NewWithPreloadedResult, origin)
                       → view.OriginView = origin
          esc → BackToOriginMsg{Origin} → back to the row
```

What dies is the transport of *options* to whoever will run the scan. What
stays is the record of where `esc` returns to — and it gets more use, not less,
since the inventory becomes a third origin.

**One consequence to handle.** `handleWorkspaceScanResultLoaded` currently falls
back to the form when the result file is missing, so the user can rescan instead
of hitting a dead end. With no form, that fallback re-triggers the scan in the
origin view, where `batchScanCmd` already lives.

### The scan caches must become per-context

`config.yaml` is per context; the caches are not. All six live flat under
`~/.devdesk/cache/`, and `browser-selection.json` is the only one with a context
dimension (`map[string][]string`, context → entries).

Today this is invisible because the caches are only ever *queried* — you ask
about the image in front of you. **Phase 2 makes it visible**: the inventory
lists everything the cache holds, so in context `work` the user would see
targets scanned under `perso`. Not corruption, but an accuracy regression that
this phase introduces and must therefore fix.

`workspaces_dir` being per context makes it concrete: two contexts legitimately
point at different roots and land in one namespace.

- `ImageScanCache` and `WorkspaceScanCache` metadata gain a context dimension,
  shaped exactly like `browser_selection.go`.
- Existing flat entries migrate into whichever context is current on first load.
- The result blobs under `image-results/` and `workspace-results/` are content-
  addressed by hash and need no change; only the metadata index is keyed.

Do this **before** the inventory ships, not after — otherwise the first thing
the new view does is show the user another context's findings.

### The dependency banner does not move — decided

`checkDependencies` / `deps` told the user whether Trivy and Gitleaks were
reachable, rendered in the form. The dashboard already carries
`shared.State.Tools []ToolInfo` and shows exactly this. It is not duplicated on
the inventory; `checkDependencies` and the `deps` field go with the form.

---

## Phase 3 — deletion — **shipped**

Only once phases 1 and 2 are merged.

| Goes | Size |
|---|---|
| `internal/ui/security/form.go` | 271 lines |
| `renderInputView`, `formLeftColumn`, `formRightColumn`, `renderCheckbox`, `renderTargetTypeField`, `renderTargetPathField`, `renderStartButton`, `renderAdvancedTextInput` | ~120 lines |
| `StateInput`, and the ~15 model fields mirroring the config | — |
| `SelectionRequestMsg` / `SelectionResultMsg` / `SelectionCancelledMsg` and the browser bridge | — |
| The cross-view scan delegation (above) | — |

Roughly 450–500 lines of production code, plus the form half of
`model_test.go` (877 lines today).

**Two things must land somewhere before the form is deleted, not after:**

1. `applyServerModeConstraints` — a Trivy server URL forces `misconfig`,
   `license` and `sbom` to false, because the client-server protocol does not
   support them. That is a genuine cross-field constraint, not a defect, and it
   moves to the config view's `scan` tab intact.
2. `scan.ValidateTrivyServer` — must be called on the config view's
   `trivy_server` field. This is the check that stops `":"` reaching Trivy and
   failing the whole scan.

**Three fields die with the form, and only with it.** Each is written where it
is, and read by something that goes in this phase:

| Field | Read by | Becomes |
|---|---|---|
| `homeState` | `goHome` | unnecessary — the inventory is the only landing state left, so `goHome` stops branching |
| `deps` (+ `checkDependencies`, `DepsCheckedMsg`) | `renderStartButton` only, since the header stopped showing tool versions (§3.12) | dead — the dashboard already reports tool availability from `shared.State.Tools` |
| `generateSBOM` and the ~15 other option mirrors | the form's checkboxes | dead — the options come from the config view |

**And one thing to do straight after this phase, not before:** removing SBOM
generation — `docs/backlog.md` §3.14. It is deferred precisely because the form
addresses its fields by index and SBOM is index 6 of thirteen, so doing it first
renumbers seven fields and their tests for code this phase deletes.

---

## Order, and why

| Phase | Ships green alone | Depends on |
|---|---|---|
| 0 — `IgnoreEOL` (D26) | yes | — |
| 0b — per-context scan caches | yes, invisible until phase 2 | — |
| 0c — tool source and paths (D27) | yes | — |
| 1 — config view | yes, purely additive | 0, 0c |
| 2 — inventory | yes, alongside the form | 0b, 1 |
| 3 — deletion | yes | 1, 2 |

The three phase-0 items are independent of each other and of everything else,
and each is worth doing whatever happens to the config view. 0b fixes a latent
inaccuracy that phase 2 would otherwise put on screen; 0c is what makes the
config view's `scan` section able to offer a choice that currently does not
exist.

## Defects this plan opens

Both found while planning, both independently fixable, both to record in
`docs/backlog.md` §1.3.

- **D26** — `IgnoreEOL` is set on one of three scan-option builders, so the
  option applies from the security form and silently does not from the images or
  workspaces lists. Phase 0.
- **D27** — `trivy_path` and `gitleaks_path` are declared, defaulted and
  expanded on load, and read by nothing. A custom tool path does nothing,
  silently. Phase 0c.

The form only dies once everything it does has somewhere else to live. No phase
leaves the application in a state where an option can be set but not read, which
is precisely the condition D26 describes.

## Coverage

Project total is 81.5 % and each phase must not lower it. `security` is at
85.5 % today, much of it covering the form; phase 3 removes tests along with
code, so the inventory needs its own before the deletion lands, not after.
