# Security scanning, the inventory, and the scan cache

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Security Scanning

**Where a scanner runs from is configured, not guessed.** `scan.trivy_source`,
`scan.gitleaks_source` and `scan.plumber_source` take `auto | binary | image`:

| Value | Resolution |
|---|---|
| `auto` (default) | the binary when there is one, the Docker image otherwise |
| `binary` | `trivy_path` when set, else the name on `PATH` — **fails rather than falling back to Docker** |
| `image` | `trivy_image`, even when a binary is installed |

`scan.CheckDependencies(cfg.Scan)` resolves the three tools and fills
`DependencyStatus`; `deps.TrivySpec()` / `GitleaksSpec()` / `PlumberSpec()`
hand a `ToolSpec`
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

**A gitleaks exit code says nothing on its own; the report is what separates a
result from a failure** (§3.50, D56). Measured on v8.30.1: a clean repository
exits **0** and writes `[]`, secrets found exit 1 **with** the report, and any
fatal — a `--config` that is missing, or one that will not parse — exits 1 with
**nothing** on stdout. `RunGitleaks` used to read that last case as a clean
repository, on a comment describing behaviour gitleaks does not have, so a scan
that read not one byte came back with `SecretsScanned` true and a green icon in
`ws`. Do not reintroduce a tolerance keyed on the exit code alone: the failure
carries gitleaks' own diagnostic, which is the only thing that can say which
fatal it was.

**A configured rules file is mounted, and the flag names the mount.**
`gitleaksConfigMount = "/gitleaks.toml"` sits at the container root because that
is the one place nothing else can be — the target is at `containerScanPath`, so
no file of the repository lands beside it. `scan.gitleaks_config` is also made
**absolute at load** (`config.ExpandPaths`): a relative path means DevDesk's
working directory in binary mode and the container's in Docker mode, and with
the file mounted those two readings would name different files.
`checkGitleaksConfig` refuses an unreadable one before anything starts — not as
a second guard against a bad configuration, but because `docker run -v` on a
host path that does not exist **creates a directory** there rather than failing
(measured on Docker Desktop 29.7.2). `scan.plumber_config` (§3.42) is the same
setting for another tool and copies all four points.

**The CI score — plumber, and only this context's forge** (§3.42). `plumber`
grades a repository's CI configuration: a letter A–E, points out of 100, and the
issues that explain them. Five rules hold it together, each measured on 0.4.40
and 0.4.42 rather than read out of the documentation, which this section was
twice wrong from:

- **A context targets one forge, so it grades that forge's repositories and no
  others.** A repository of another host is not scanned anonymously — it is not
  scannable, and the cell is empty. That is §3.17's rule (`git.SameHost`, which
  moved out of `internal/ui/workspaces` so a domain package could ask it too),
  and `LoadForgeToken` is called only once the host has matched, so a foreign
  repository never reaches the secret store.
- **It is resolved per target, never per batch.** One batch holds repositories
  with different remotes and different branches; `Scanner.ciOptions` reads *this*
  repository's own remote. `OptionsFromConfig` therefore takes the whole
  `*config.Config`, because which forge a context targets is not a scan setting.
- **`--provider` is declared from the configuration, never sniffed.** And a
  GitHub context never passes `--gitlab-url`: that flag is part of how plumber
  decides to take the GitLab path at all.
- **The exit code says which of three things happened**, and the report is not
  consulted to guess: `0`/`1` a grade, `3` a **withheld** score, `2` a failure.
  Only `2` fails the stage.
- **The letter of a withheld run is never quoted.** plumber writes one anyway,
  and it flatters: on the two committed fixtures the degraded run reads **B/79**
  where the complete run reads **E/30**, because a control that did not run found
  nothing. `Result.CIVerdict()` is the one place that decides, on
  `SecretVerdict()`'s model — nil when nobody graded, and a withheld run counts
  as nobody.

**The two screens.** `ws` gains a `CI` column — title `CI`, four cells, the
letter alone — and the results view a fifth tab, `CI (n)`, carrying the issue
count like its four neighbours. Four rules, and three of them are about the
absences:

- **The column exists only when `scan.enable_ci_score` is on.** Off by default,
  it would otherwise be four cells of nothing on every row for the life of the
  view. It does **not** declare `Optional`: its states already include two
  absences that differ, and a column that vanished on a narrow terminal would add
  a third that looks like them.
- **A dash is "not yet", an empty cell is "never, not from here".** A repository
  whose remote is not this context's forge cannot be graded, so it shows nothing;
  one nobody has scanned shows `-`. `theme.CIScoreVerdict` is the one place that
  decides, from the **state** and never from the rendered string — which is what
  `ws` did for secrets and had to undo.
- **A directory shows nothing.** Counts add up across nested repositories;
  letters do not — the worst of three grades is the grade of nothing.
- **The grade has no line of its own, and the five tabs are the same height.**
  It had one above the table on the CI tab, which cost that tab two of its rows
  on every open to state a letter the `:sec` inventory now carries per target,
  in its own `CI` column. One tab showing fewer findings than its four
  neighbours — and a layout that jumped on every switch onto it — was not worth
  a value already on the previous screen.

**The `:sec` inventory carries the same column**, under the same four rules: on
only when the setting is, never `Optional`, never sorted, and the three absences
told apart by the state. An image is never gradeable — it has no pipeline — so
its zero value is already the truth, while a repository needs its remote, which
is a `git remote get-url` per row at load. That call is what `ciForgeURL`
suppresses when the column is off: a column nobody is showing must not pay for
it.

**A grade is coloured on all five letters, in bold** — A and B green
(`ColorOK`), then the severity colours for C, D and E. `CIScoreStyle` builds its
own style rather than borrowing `SeverityTextStyle`: the colours are the
application's vocabulary, the weight is not, and `SeverityTextStyle` sets `Bold`
on CRITICAL and HIGH alone — so a borrowed C would render lighter than a D for a
reason belonging to a CVE table, and fixing that there would embolden every
MEDIUM finding in the application. Colouring the nominal grades is the documented
exception to Rule 122's colour discipline, and it earns it: this column's three
absences are all dim, so uncoloured is already taken. A letter the tool may add
later gets the weight and no colour — the nominal green is a claim, and claiming
it for an unknown grade would be a guess.

**`plumber analyze` takes no path argument: it works on the current directory.**
So `toolCmd` carries a `Dir` and the binary path sets it to the repository; the
container gets the same thing through `-w`. Without it the tool ran wherever
DevDesk had been launched from and answered
*--project is required (could not auto-detect from git remote)* — a failure
about a repository nobody had asked it to look at. Trivy and Gitleaks take their
target in argv and set nothing there.

**A stage that fails goes to the log, not to the screen** (Rule 128). The
results view rendered a warnings panel that replaced the whole table whenever
`Result.Errors` was non-empty, so a plumber failure took every CVE and every
secret down with it; folding it to a banner above the table only moved the
problem to all five tabs at once. There is no panel any more — the table is what
the results state shows, always — and the footer carries a **status** naming the
failed stages and saying to check the logs. A status rather than a message
because it is a state of the result: a message expires after three seconds, and
the user would be left with a result that looks complete.

`internal/scan.recordStageError` is what makes that honest. Every stage used to
append to `Result.Errors` and log **nothing**, so the only copy of the reason was
on screen, in the panel that hid the findings. It now logs at the point it
records, at all six sites.

Two smaller things it needed: `toolCmd` gained an `Env` so the token reaches
plumber through the environment rather than argv, which is readable from the
process list; and the security view gained the secret store, because a rescan
from `:sec` must grade what the same rescan from `ws` grades.

`--score` is not optional: an issue carries no severity of its own, it lives in
`plumberScore.codeLosses[]` indexed by `code`, so without it every finding would
be UNKNOWN. `--print=false` is what keeps stdout parseable. The JSON leaves by
two routes and the asymmetry is measured: `--output /dev/stdout` works in a
container and writes **nothing** from a native Windows binary, which therefore
writes to a temp file. A containerised plumber also needs `safe.directory`
through the environment, or git refuses the mount as dubious ownership (the image
runs as uid 65532) and plumber answers *not in a git repository*; `--provider`
alone does not lift it.

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
thing missing: the workspace scan cache is scoped to a context, so
identical-looking repository rows mean different things in two of them. Image
rows are shared (§3.39) and the header still names the context, because the row
that *is* scoped sits in the same table.

| State | Info |
|---|---|
| Inventory | `Context`, `Targets` |
| Results / Details | `Context`, `Findings` |
| Form / Scanning | `Context` |

## The security inventory

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

**The kind glyph is a column of its own**, untitled and `datatable.IconColumnWidth`
wide, ahead of Target — the workspaces and containers shape, and what Rule 125
now requires of every icon-first table. It used to be `IconDocker + " " + name`
inside the Target cell, which spent two cells of the most disputed column in
the narrowest table on something that is not the name, and made a `SizingContent`
column measure the glyph along with it.

**And it is coloured by role** (§3.57): `IconRoleImage` against
`IconRoleRepository`, through `theme.IconStyle`. The repository takes the same
role `ws` and the explorer use — one object listed by three views, one colour.

An image takes the **highlight** rather than a third purple, and that is a
decision about this table specifically: `ColorPrimary` and `ColorSecondary` are
a mauve and a lavender one notch apart, and this column has exactly two values,
so it is the one place where they would sit on adjacent rows with nothing else
to separate them.

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

- **It never reads a failure as an absence.** `docker.ImageNames` returns a
  second value saying whether it could find out, and a failed enumeration keeps
  every image — a stopped daemon would otherwise empty the inventory, and on the
  dashboard, where nothing is listed row by row, it would read as `0 CRITICAL`:
  the one wrong answer nobody would question. `cache.RepositoryGone` tests
  `os.IsNotExist` and nothing else, so a permission error or an unmounted share
  keeps the row.
- **It hides, it does not delete.** The entry and its stored result stay on
  disk: a transient answer must not destroy a scan nobody asked to purge. `A`
  only rescans the rows that are there, so a hidden entry costs nothing while it
  waits.

**The rule belongs to the caches, not to any one reader.** There are three, and
`:sec` was only the loudest: `internal/mcp`'s `scan_inventory` had its own copy
of all of it, and the dashboard's `readPosture` — which sums the same two files
to fill the Repositories and Images trees — had none. So the dashboard counted
what the other two dropped. A repository deleted after its scan kept
contributing its CRITICALs to a box no other view could corroborate: `ws` lists
the disk and never showed it, `:sec` had already dropped it, and the one screen
announcing the number was the one you cannot drill into (D69).

One home each, and the split is by what the thing needs:

| | Where | Why there |
|---|---|---|
| `RepositoryGone(path)` | `internal/cache` | pure; `os.Stat` and `os.IsNotExist`, nothing else |
| `ImageGone(name, present, known)` | `internal/cache` | pure; the caller supplies the enumeration, so the cache stays daemon-free |
| `docker.ImageNames()` | `internal/docker` | the daemon call, and the `(nil, false)` answer that keeps everything |

`docker.ImageNames` is a package **var**, and that is the test seam all three
packages were each declaring for themselves: a test cannot pull an image, so
without it a reconciliation could only be asserted by checking that a fixture
happens to be absent, which it would pass for the wrong reason. `internal/mcp`
keeps a separate `listImages` seam because `images_list` projects the whole
`Image` — size, age, container count — and a set of names answers none of that.

The posture reconciles both families, and the asymmetry in *where* is
deliberate: `os.Stat` runs inside `readPosture`, because reading a path is the
same kind of act as reading the two cache files, while the daemon call is made
by the Cmd and passed in (Rule 110). That is also what makes `readPosture`
testable without touching the seam.

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
- **"Nothing scanned yet" waits for the caches to answer** (`inventoryLoading`,
  D65). An empty table covers two opposite facts — both caches replied and there
  is nothing, or `loadInventoryCmd` is still in flight — and branching on
  `len(Items()) == 0` alone asserts the first about the second. The table stays
  on screen while it loads and the footer carries the spinner (Rule 139).

`spinnerAlive()` — `inventoryLoading || inventoryScanning()` — is the single
predicate deciding whether frames keep coming. `handleSpinnerTick` reads it to
schedule the next one, `spinnerTickIfIdle` to refuse starting a second chain
alongside a live one; asking the two conditions separately in the two places is
what would double the frame rate when a rescan starts during a load. For the
same reason `reloadInventory` takes its tick *before* raising the flag.

`esc` and `ctrl+r` return to the inventory, or to the list the results were
opened from when `OriginView` is set.


## Scan Cache

Two independent disk+memory caches in `internal/cache/`:
- `ImageScanCache` — keyed by `"repo:tag"`, metadata at `~/.devdesk/cache/image-scans.json`, full results in `image-results/<sha256>.json`
- `WorkspaceScanCache` — keyed by absolute repo path, metadata at `~/.devdesk/cache/workspace-scans.json`, full results in `workspace-results/<sha256>.json`

Both entries carry the four severity counts and `Sensitive *bool`, the secret
verdict written by `scan.Result.SecretVerdict()` (see Security Scanning). `nil`
is a value: it means no stage looked, and it is what every image entry written
before there was an image secret stage decodes to.

**One is scoped to a configuration context and the other is not, and the
asymmetry is the point** (§3.39). What decides it is whether the *key* is
per context:

| Cache | Key | Scoped |
|---|---|---|
| `WorkspaceScanCache` | absolute repo path, reached through `workspaces_dir` | **yes** — `workspaces_dir` is per context, so the same path may be different work |
| `ImageScanCache` | a local Docker reference | **no** — `docker image ls` answers for the machine, not for a configuration |

Scoping the image cache was the mistake. Two contexts looking at `nginx:1.25`
are looking at the same bytes, so a context switch dropped counts for images
that had not moved — and the full results beside the index were **never** scoped
at all: they are content-addressed by image name, with no context anywhere. The
index and the blobs disagreed, and the index was the one that was wrong.

So `NewImageScanCache()` takes no context — a parameter that has to be ignored
is worse than none — while `NewWorkspaceScanCache(contextName)` keeps its own.

**Which name, and when it is read** (D68). A scan's writer takes the context it
was *launched* in, handed down from `jobs.Run.Context`; it does not read the
current one. A batch of twelve repositories runs for minutes and a context
switch during it is ordinary — that is what a background scan is for — so a
writer reading `config.CurrentContextName()` when Trivy returned put the counts
under whichever context happened to be on screen. Both halves of that are
silent: the launching context loses results it asked for and rescans forever,
and the arriving one gains results for paths that may mean nothing there.

Three writers shared it — `scanOneRepoCmd`, `storeRescan`, and
`deleteScanCacheCmd`, the purge half of `ctrl+a`. The purge was the worst of the
three: it travelled in the same `tea.Batch` as the scan replacing it, each
reading the name on its own goroutine, with nothing making them agree. They now
travel in one builder (`purgeAndScan` in `ws`), so there is one name between
them rather than two that happen to match.

Reading the current context is still right for a **load** —
`loadScanCacheCmd`, `loadInventoryCmd` — and for anything that *names* the
context. The line is not "inside a `Cmd`"; it is whether the code writes a
result someone asked for under a name.

`internal/cache/scan_file.go` owns the scoped shape
(`{version, contexts: {name: {key: entry}}}`): the workspace cache writes it, and
the image cache still *reads* it in order to fold it back.
`image_scan.go` owns the flat one (`{version: 2, entries: {key: entry}}`) and
understands all three shapes it has had — v2 as it stands, v1 folded into one
map, and the bare `{key: entry}` map that predates contexts, which is already
this shape and needs only a version stamped on it.

**A collision in the fold is settled, not reported**: two contexts holding the
same image is the ordinary case — it is one image, and both saw it — so the more
recent `ScannedAt` wins. The image did not change between them; the vulnerability
database did.

The fold is **written back on the first open** rather than deferred to the next
`Set`, for the reason the context migration was: a file left in the old shape is
folded again on every open, so what it holds would depend on when it was last
read.

**A shared entry may have been produced under another context's scan options.**
`enable_vuln`, `ignore_unfixed` and the rest are per context, so counts written
elsewhere can differ from what this context would produce. That is the accepted
cost: the `Scanned` column carries the age, and `S` rescans. The secret verdict
is unaffected — `Sensitive` is `nil` when no stage looked, which is what a scan
run with secrets off writes anyway.

Cache invalidation: `S` (single) overwrites; `A` (all) rescans, and purges the cache first when its checkbox is ticked.



## Who knows a scan is running — `internal/jobs`

**One bookkeeping, and it is not the view's** (§3.58). Three of them ran in
parallel before: `workspaces` kept a map of paths, `oci_resources` a map of
image names, and the `:sec` inventory a flag per row. Each knew only what *its*
view had launched, so the same repository could be scanned twice — once from
`ws`, once from `:sec` — with both runs writing the same cache entry and neither
guard seeing the other.

Every view now reads the same snapshot, broadcast by the router in
`jobs.ChangedMsg`, and asks it the same question:

| View | Reads |
|---|---|
| `ws` | `scanning(path)` — and `busy()` folds sync and delete in beside it |
| `oci` | `scanningImage(name)`, `anyScanRunning()` |
| `:sec` | `scanningTarget(name)`, `inventoryScanning()` |

The consequence worth stating plainly: **a scan started in one view marks the
row in every other view that lists the same target.** The `:sec` inventory lists
exactly what the other two scan, so this is not a corner case — it is the normal
reading.

`:sec` gained the most. `scanTarget.Scanning` was a flag the view set and
cleared, and `handleInventoryLoaded` carried it across every reload by hand: the
caches say nothing about a scan that has not finished writing to them, so a
refresh landing mid-rescan cleared the spinner and left the row looking settled.
That reconciliation is *gone* rather than fixed — the flag is derived in
`setInventory`, so a reload has nothing to preserve.

**A load is not a job.** Both views keep their own `spinner.Model` and frame
index, and both keep animating with them: reading the caches, fetching the image
list, a `docker` action on a row. What moved to the router's single chain (D5)
is the frame the *scan* cells carry. `:sec`'s `spinnerAlive` therefore answers
for the load alone — keeping the rescan in it would be a second chain beside the
router's, which is the failure D5 removed.

**`rescanCmd` had no starting message** — only a finished one — so a target went
from queued straight to done and nothing said which of a batch was actually
running. `InventoryScanStartingMsg` exists for that (D10), and like the others
it is emitted *after* the worker pool hands out a slot, which is what makes
`queued` and `running` mean different things.


## A scan can be stopped — `K` in `:jobs` (D7)

`scanner.Scan` has always taken a context and `internal/scan` has always run its
tools through `exec.CommandContext`, so cancelling one kills the Trivy and
Gitleaks processes it started. What was missing until §3.58 poste 8 is that
every launch site passed `context.Background()`, so there was nothing to cancel.

The three of them now create a cancellable context **in the Cmd** and put the
function on the message that says the scan started:

| | |
|---|---|
| `ws` | `scanOneRepoCmd` → `WorkspaceScanStartingMsg.Cancel` |
| `:sec` | `rescanOneCmd` → `InventoryScanStartingMsg.Cancel` |
| `oci` | `scanOneImageCmd` → `ImageScanStartingMsg.Cancel` |

It rides on that message and not on one of its own because the two are a single
event: the registry stores the function in the same `Update` that marks the item
running (`Transition.Cancel`), so there is no window where the row spins and `K`
does nothing.

Each body also `defer cancel()`s, whether the scan was cut or ran to the end —
the registry drops its own copy when the item settles, and a context nobody
releases holds what it closes over for the session.

**A cancelled scan reports itself failed**, through the ordinary path: the
context dies, `RunTrivy` returns the error, and `WorkspaceScanCompleteMsg`
carries it. The registry is not told twice, which is the point of asking rather
than declaring.

`scanOneImageCmd` also had the defect poste 3 fixed in `ws` and poste 4 in
`:sec`, and it is fixed here for the same reason: the starting message was
emitted **before** the semaphore, so every image in a batch reported itself
running the instant the batch was dispatched — twelve rows spinning on four
workers. D6 is decorative without that ordering.
