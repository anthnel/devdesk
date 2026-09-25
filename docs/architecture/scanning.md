# Security scanning, the inventory, and the scan cache

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Security Scanning

**Categories and tools are declared once (§3.86).** `internal/scan/toolbox.go`
holds two tables: the tools (`Tool` — name, binary, default image, version
arguments) and the categories (`ToolCategory` — which tools serve each one, on
which targets, and which tool another only renders for). `scan.categories.<id>`
turns a category on and ticks its tools; `scan.Uses` answers "does this tool
run for this category", `scan.Required` "which tools does this context need",
and every stage gate, `missingToolErrors` and `Report.Missing` read those — no
copy of the category → tool link is written anywhere else. `config` declares the
identifiers and the default ticks because it cannot import `scan`;
`TestTheTablesAndTheConfigurationAgree` keeps the two in step.

**Where a scanner runs from is configured, not guessed.** Each
`scan.tools.<tool>.source` takes `auto | binary | image`:

| Value | Resolution |
|---|---|
| `auto` (default) | the binary when there is one, the Docker image otherwise |
| `binary` | `tools.<tool>.binary` when set, else the name on `PATH` — **fails rather than falling back to Docker** |
| `image` | `tools.<tool>.image`, even when a binary is installed |

`scan.Detect(cfg.Scan.Tools)` resolves every tool of the table into a `Report`
(`map[ToolID]ToolStatus`, plus the engine and its socket); `Scanner.spec(id)`
hands a `ToolSpec` to the command builders — where detection found the tool
(source, binary, image), plus what the context adds: `Args`, and for Trivy its
rules file `Config`.

**A tool's extra arguments go after its subcommand**, before DevDesk's own
flags and the target (`afterSubcommand`) — kubeconform, which has none, gets
them first: Go's `flag` package stops at the first positional argument, so a
flag after the files would be read as a file. The flags DevDesk sets itself are
`Tool.ReservedArgs`, refused by name in the configuration view (`CheckArgs`).
**Trivy's `trivy.yaml`** is passed as `--config` right after the subcommand and,
from an image, mounted at `/trivy.yaml` like gitleaks' and plumber's files; the
`--format json` DevDesk adds later wins over a `format:` in the file, since
Trivy lets the command line override it. `checkRulesFile` refuses an
unreadable one before anything starts (§3.50: `docker run -v` would create a
directory in its place). The command shown is built by the same builder, so
it carries the arguments too (D19). `ToolSpec` replaced the
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
an image scan had no secret stage at all before. Both are ticked
under `scan.categories.secret` and feed the one Secrets tab; the Source column names the
tool.

Two consequences worth keeping:

- The vulnerability stage passes `--scanners vuln` **explicitly for images**.
  Trivy's default there is `vuln,secret`, so that stage was running a secret
  scan whose output nothing read — and would now report each secret twice.
- `X` (exclude — add to `.gitleaksignore`) is offered for **Gitleaks findings only**. That
  file is matched on a Gitleaks fingerprint, which a Trivy secret does not have;
  `AddToGitleaksIgnore` would fabricate one and report success for a line
  nothing will ever match.

**Every Trivy process this application starts is serialized, through a
package-level semaphore in `runTrivy` (`trivy.go`).** `Scanner.Scan` runs its
vuln, misconfig and both secret stages concurrently for one target, and a
batch scan runs several targets concurrently on top of that (`batchScanCmd`,
`internal/ui/oci_resources/commands.go`) — so a single scan of one image can
already start three Trivy processes at once, and a batch multiplies that by
its worker count. Trivy's local cache — the vulnerability database and the fs
cache, both BoltDB — allows only one writer; two of those processes racing for
it do not queue behind each other, the loser fails outright with `unable to
acquire cache or database lock ... timeout`. The semaphore turns the race into
a queue instead, and the wait respects the scan's own context, so `K` in
`:jobs` still cancels a run that has not even reached its process yet. Gitleaks
and plumber do not share this cache and are not serialized by it.

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
no file of the repository lands beside it. `scan.tools.gitleaks.config` is also made
**absolute at load** (`config.ExpandPaths`): a relative path means DevDesk's
working directory in binary mode and the container's in Docker mode, and with
the file mounted those two readings would name different files.
`checkGitleaksConfig` refuses an unreadable one before anything starts — not as
a second guard against a bad configuration, but because `docker run -v` on a
host path that does not exist **creates a directory** there rather than failing
(measured on Docker Desktop 29.7.2). `scan.tools.plumber.config` (§3.42) is the same
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

- **The column exists only when `scan.categories.ci` is on.** Off by default,
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

### Kubernetes manifests — kubeconform (§3.80)

Trivy's misconfiguration stage already lints Kubernetes manifests and Helm
charts for security (`KSV-*`), and the finding says so through `IaCType`.
What it does not answer is whether the API server would **accept** the
manifest at all — a wrong type, a missing required field, an unknown field,
an `apiVersion` the cluster's release no longer serves. That is kubeconform's
job: a tool of the Misconfiguration category, ticked in
`scan.categories.misconfig`, resolved like every other tool
(`scan.tools.kubeconform`, image `ghcr.io/yannh/kubeconform`).
kube-linter was weighed and left out: it overlaps Trivy on security and reports
no line, so it could feed neither the table's jump-to-line nor a fix.

- **Which files.** `k8s.Discover` (`internal/k8s`) lists them **by content**
  — a document with an `apiVersion` and a `kind` — and kubeconform is handed
  those files by name, never the directory: on a directory it reports
  "missing 'kind' key" on every CI file and Helm values file. Hidden
  directories, `node_modules` and `vendor` are skipped. A repository with no
  manifest runs nothing — kubeconform with no file argument reads stdin and
  would wait there.
- **Charts and Kustomize roots are set apart.** A template is not YAML until
  helm renders it, and a Kustomize patch is a fragment that fails the schema
  on its own. Their files are never validated raw. Only the **outermost**
  chart is rendered (a subchart is rendered by its parent, with its values)
  and only the Kustomize **leaves** — a base is validated through the overlays
  that use it.
- **helm and kustomize are renderers**, resolved like every tool
  (`scan.tools.helm`, image `alpine/helm`; `scan.tools.kustomize`, image
  `registry.k8s.io/kustomize/kustomize:v5.8.1`) and used only when ticked
  beside kubeconform (§3.86 revised §3.80's "never reported missing": ticked,
  they are required, and the dashboard says so). A scan does not list one
  among its errors — its charts or overlays land in `K8sUnrendered`, which is
  more precise than one line per scan.
  With helm, each chart gets `helm lint` (WARNING → LOW, ERROR → HIGH,
  `Source: helm`, shown as `helm lint`) then `helm template`; with kustomize,
  each overlay gets `kustomize build`. The rendered YAML goes to kubeconform
  **on stdin** (`toolCmd.Stdin`, `-i` in a container, no repository mount),
  and each finding is pointed back at a file: the template named by helm's
  `# Source:` comment — under the chart's *name*, re-rooted on its directory
  — or the overlay's kustomization. No line: a rendered line is no line of
  either. Without the renderer, the directory is listed in
  `Result.K8sUnrendered` and logged, which is what keeps "nothing found
  there" apart from "nobody looked there".
- **A chart or overlay that does not render is a finding, not a failed
  scan** (`K8S-RENDER`, HIGH, on `Chart.yaml` or the kustomization). Measured
  on helm 4.3.0: a missing dependency is only a WARNING for `helm lint`, and
  it is `helm template` that fails — so the render failure is reported unless
  lint already raised an ERROR. `helm lint` also names that chart by its
  **absolute** path (`/scan/charts/x` in a container), which `helmLintPath`
  brings back to the repository.
- **The finding's line comes from the file, not the tool.** kubeconform
  reports a JSON pointer; `k8s.Locate` finds the document by kind and
  `metadata.name`, then walks the pointer through `yaml.v3` nodes. An unknown
  field is reported at its parent with the key in the message, so the key is
  located first. A pointer that does not resolve gives line 0, never the
  nearest line that exists.
- **A missing schema is two different things.** No `-ignore-missing-schemas`:
  kubeconform then answers "could not find schema for X", and the group
  decides. A group Kubernetes serves itself (a closed list in
  `kubeconform_parse.go`) means the `apiVersion` was removed from the target
  release — `K8S-API-REMOVED`, on the `apiVersion` line. Any other group is a
  custom resource whose schema lives in a CRD kubeconform does not read: it is
  skipped and counted in the log. The list is closed rather than a
  `.k8s.io` suffix rule because the Gateway API and the snapshot controller
  are CRDs in `*.k8s.io` groups.
- **Exit 1 means two things too.** kubeconform exits 1 when it found problems
  and when it could not run; the JSON report on stdout is what separates them.
- **The target release** is `scan.tools.kubeconform.kubernetes_version` (x.y.z or `master`,
  checked in the configuration view with kubeconform's own pattern),
  `config.DefaultKubernetesVersion` when unset — one minor behind the newest
  for which schemas exist.
- **Schemas are cached** in `~/.devdesk/cache/kubeconform`, mounted at
  `/cache` in a container; the repository is mounted read-only at `/scan`,
  as for the other tools.

Every kubeconform finding is `Source: kubeconform`, `IaCType: kubernetes`,
severity HIGH — the API server would refuse the resource — and lands on the
Misconfigurations tab, where the Source column reads `schema`. The ids
(`K8S-SCHEMA`, `K8S-API-REMOVED`, `K8S-PARSE`) are DevDesk's: kubeconform has
none, and a re-scan needs a stable one to say a finding went away.

### The build context — DevDesk's own check (§3.81)

`internal/scan/buildcontext.go`. A `COPY . .` takes whatever no `.dockerignore`
leaves out into a layer of the image, and nothing else in the pipeline sees it:
`trivy config` and hadolint have no rule for it (verified on Trivy 0.71.2 and
hadolint 2.14.0), and the secret stages read the working tree, where `.git`
matches no pattern. It is not a secret but the vector — the history, with every
secret ever committed and "removed", is readable from the layer.

It is a stage of its own (`build-context`), and the first that runs **no
tool**: `Scanner.checksBuildContext` gates it on the Misconfiguration category
being on and the target being a directory, nothing else. It is therefore not in
`categoryTable` — there is no tool to tick, detect or report missing. It does
**not** set `MisconfigScanned`: it reads a few paths, not the Dockerfile's
rules, and a target it found nothing in has not been checked for
misconfigurations by it.

Every finding rests on three facts the check establishes, never supposes:

| Fact | Where |
|---|---|
| a `COPY`/`ADD` takes the whole context (`.`, `./`), with no `--from` and no `--exclude`, in the final stage or a stage it is built `FROM` | `dockerfile.Copy.TakesWholeContext`, `shippedStages`. A builder stage's layers are not the image's; `COPY --from` of a whole stage is left alone, since what it carries depends on that stage's paths. `COPY *` is left alone too — whether `*` takes dot files depends on the builder |
| the path exists | `.git` must be a **directory** — a worktree's `.git` is a file naming the real one, and copying it exposes a path, not a history. Sensitive files are walked like `dockerfile.Find` walks (depth 4, `node_modules`/`vendor`/… skipped) |
| no ignore file that may apply excludes it | `dockerfile.IgnoreFiles` returns the Dockerfile's own `<name>.dockerignore` (BuildKit reads it first) and the context's `.dockerignore` (every builder), whichever exist; a path is sent only if **none** excludes it, since which one applies depends on the builder |

`dockerfile.ParseIgnore` implements Docker's syntax (moby/patternmatcher): `#`
in the first column, `!`, a leading `/` dropped, `*`/`?` within a segment, `**`
across them, last match wins, a directory taking its contents. Every answer
comes with **whether it is certain**, and only a certain "not excluded" is
reported: a pattern `path.Match` rejects or a `**` inside a segment makes the
whole file uncertain, and `ExcludesTree` answers uncertain when a negation
*after* the last pattern excluding `.git` could reach below it
(`!.git/config`). Uncertain is silence — the inverse would be the false
positive the check exists not to produce.

**Only a Dockerfile at the root is checked.** Nothing in a Dockerfile says
where its build context is — `docker build -f sub/Dockerfile .` puts it
elsewhere — and at the root "the context is this directory" is the one safe
assumption. `logUncheckedDockerfiles` logs each deeper Dockerfile that copies
its whole context, with that reason. Same reflex as `fixRootUser` declining a
base it cannot identify.

| Id | Finding | Fix |
|---|---|---|
| `DEVDESK-CTX-001` | `.git` copied into the image | `ctrl+o` appends `.git` to the one ignore file that applies (`remediation.fixIgnoreGit`); declines with none — DevDesk does not **create** a `.dockerignore`, whose content would be a policy it invented — and with two, since which one the builds read is not in the files |
| `DEVDESK-CTX-002` | sensitive files copied, the first five named | none — which of them the image needs is not something the files say |

Both are `Source: build-context` (Source column `context`), `IaCType:
dockerfile`, severity HIGH, and point at the `COPY` line. The ids are DevDesk's,
in a namespace no Trivy id can collide with.

**The fix edits another file than the finding's.** `remediation.Rule.Target`
names the file to edit (nil = `f.File`), and it may read the disk, so it is
called in the Cmd that computes the fix, never by `canFixMisconfig` — the
declines it produces reach the footer as warnings. The UI carries the finding's
file apart from the written one (`MisconfigFixPreparedMsg.FindingFile`,
`misconfigFindingRef`): the verification re-scan looks for the rule in the
Dockerfile, where it was reported, not in the `.dockerignore` it wrote.

`dockerfile.IsDockerfileName` no longer takes `Dockerfile.dockerignore` for a
Dockerfile, which it did by its `Dockerfile.` prefix — `dockerfile.Find` and the
Remediation tab listed it until now.

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

### The misconfiguration column (`CFG`)

**`Result.MisconfigVerdict()` is the third verdict of this shape**, after
`SecretVerdict()` and `CIVerdict()`, and it answers the same three-state
question: `nil` when no stage read the target, a `MisconfigSummary` otherwise.
`Result.MisconfigScanned` carries the fact, written by a stage that *succeeds* —
either of the two, Trivy's rules or kubeconform's schema validation, since both
serve the one Misconfiguration category.

Until §3.87 `MisconfigCount` existed and **nothing in the UI read it**: only
`internal/mcp/scan_tools.go` exposed it. A repository with forty misconfigurations
— Kubernetes manifests, a Dockerfile, Terraform — showed `0 0 0 0` in `ws` and in
`:sec`, because `Counts` holds vulnerabilities and nothing else.

| | |
|---|---|
| Shape | **a count**, not the verdict glyph secrets get. One secret is already an alarm, so a glyph says all there is to say; almost any repository with a Dockerfile carries misconfigurations, and a glyph would read the same on one and on two hundred |
| Colour | `SeverityTextStyle` of `MisconfigSummary.Worst`, the highest severity among them. **One column rather than four**: splitting it the way CVEs are split would spend sixteen cells in the narrowest table of the application on a question nobody asks of a misconfiguration backlog, which is read whole |
| `-` | no stage read this target — the category is off, Trivy is missing, the run failed. Dim, like a `0` |
| `0` | a stage looked and found nothing |
| `N?` | the count is **partial**. `MisconfigSummary.Unrendered` is `len(K8sUnrendered)` — a Helm chart or Kustomize overlay nothing rendered — and on a `ws` directory row, a sub-repository nobody scanned counts the same way |

`?` is the glyph the `CI` column already uses, for the same meaning: this did not
conclude. **`0?` is the case the whole marker exists for**: a bare `0` on a
repository of charts reports it clean when nothing in it was ever read, which is
D20 one category over. Before this, `K8sUnrendered` was visible only in the
*results* header (`headerUnrendered`) — invisible from the list, which is where
one decides what to open.

**Placement: after the four severity counters, before `CI`.** In `:sec` that is
not a preference — `inventoryColumnCritical` is an index, and slotting a column
in front of `CRIT` would make the column the table opens sorted by depend on a
setting. `ws` and `oci/images` follow the same order so the six columns read
alike in all three.

`theme.MisconfigState` / `MisconfigCell` / `MisconfigStyle` are the one
rendering, and `theme` takes plain values — `MisconfigVerdict(has, scanned bool,
count int)` — because it does not import `internal/scan`, which is also why
`SeverityTextStyle` takes a string. `MisconfigSummary`'s three accessors
(`Total`, `WorstSeverity`, `UnrenderedCount`) are nil-safe so a caller never
unwraps the pointer itself.

Both caches hold it as `Misconfig *scan.MisconfigSummary`, and neither has ever
written the key, so every existing file decodes to `nil` — the truth about it.
An image gets the column too: `trivy image --scanners misconfig` reads the
Dockerfile instructions baked into the layers, unlike the CI grade, which an
image has no pipeline for.

**A `ws` directory sums, unlike the `CI` column.** Counts add up across nested
repositories; letters do not — the worst of three grades is the grade of nothing.
The fold orders severities through `scan.WorseSeverity`, exported for exactly
that caller so the directory and the scanner cannot disagree on which of two
severities is worse.

The column follows `scan.categories.misconfig.enabled` and is built once, in each
view's `New`; the router drops the views on a save, so the setting and the column
stay in step. It does **not** declare `Optional`, for `CI`'s reason: its states
already include an absence, and a column that vanished on a narrow terminal would
add a second one that looks like it.

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

**The explorer is the only borrower now, and it borrows two views.** The form
borrowed the workspaces view for a directory and the images view for an image;
the explorer borrows workspaces for a clone destination and, since the template
catalog, the templates view for a template (`templates.NewForSelection`, see
`templates.md`). So `ociresources.NewForSelection`, `ImageSelectedMsg`,
`SelectionCancelledMsg`, `ResetSelectionMsg` and the OCI view's `selectionMode`
are gone. `app/selection.go` remembers which view it lent (`selectionLent`) so
leaving drops that one, and each borrow has its own request and its own two
answers.

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
| Results / Details | `Context`, `Findings`, `Fixable` |
| Form / Scanning | `Context` |

### What can be fixed — `Class`, `Ecosystem`, `internal/remediation` (§3.2)

A vulnerability carries Trivy's `Result.Class` (`os-pkgs` or `lang-pkgs`) and
`Result.Type` (`alpine`, `debian`, `gomod`, `npm`, …) as `Finding.Class` and
`Finding.Ecosystem`. The class is what decides the fix: a base image bump
clears an `os-pkgs` CVE and does nothing for a `lang-pkgs` one, whose fix is
the dependency itself. Both are `omitempty`, so a result cached before they
were recorded reads as **unclassified** — counted apart, never as either class,
and gone at the next scan. Nothing migrates the cache.

`scan.FixCommand(ecosystem, pkg, version)` is a table, and answers `false`
rather than inventing a command for an ecosystem outside it; the parser then
keeps the older plain sentence (`Update pkg to X`). Trivy lists one fixed
version per maintained branch (`"5.7.2, 6.3.1, 7.5.2"`), so the command uses
`scan.PickFixed`: the lowest one on the installed major line, the smallest
change that clears the CVE. Versions that cannot be ordered — a Debian epoch,
a name — give no target rather than a guess; `scan.CompareVersions` is a lenient
numeric ordering, not semver.

`internal/remediation` is pure — no I/O — and has two entry points:
`Summarize` (the header's `Fixable`: a count split into base image,
dependencies and unclassified) and `Group` (one `Fix` per ecosystem, package
and installed version, carrying the highest version any of its CVEs needs, so
one bump clears them all). Whether a proposed bump *actually* clears the CVEs
is not decided there: that is measured by re-scanning, and it is what the next
phases of §3.2 add.

### What a misconfiguration carries — `EndLine`, `Message`, `Status` (§3.78)

A misconfiguration is the one finding whose **exact extent** is known. Trivy
reports `CauseMetadata.StartLine` *and* `EndLine`, a per-instance `Message`, a
`Status`, a `Resolution` and the rule's AVD id. All of it was decoded into
`TrivyMisconfiguration` and only `StartLine` survived into the `Finding` — a
caller could read where the problem began and not where it ended, which is
enough to display and not enough to replace.

| `Finding` field | Source |
|---|---|
| `Line` / `EndLine` | `CauseMetadata.StartLine` / `EndLine` — the faulted block |
| `Message` | the instance's wording; `Description` stays the rule's generic text |
| `Status` | what Trivy concluded for the rule on this target |
| `ID` | `AVDID` — the stable identity a re-scan is checked against |
| `IaCType` | the **result's** `Type` — `dockerfile`, `kubernetes`, `helm`, `terraform`… (§3.80). Not the misconfiguration's own `Type`, which is a label ("Kubernetes Security Check") |

`IaCType` is what tells a Deployment's `KSV-*` rule from a Dockerfile's `DS*`
rule without guessing from the file name. The Misconfigurations tab shows it in
the Source column — every finding there is Trivy's, so the tool's name said
nothing — and `scan_result` projects it as `iac_type`. `helm` means the file is
a template: its lines are not the YAML the rule was evaluated on.

The new ones are `omitempty` and empty on a result cached before they were
recorded, which reads as **unknown** — an `EndLine` of zero is never a line
number. Same convention as `Class` and `Ecosystem` above, and nothing migrates
the cache for the same reason.

**DevDesk does not turn this into a patch, and for most rules it never will**:
`Resolution` is a sentence, not a replacement. What it does is hand the whole
thing to a calling agent through `scan_result` and stay the thing that measures
the result — see `mcp.md`. A finite catalog of fixes for the recurring Dockerfile
rules is phase B of §3.78 and does not exist yet.

### Fixing a misconfiguration in place — the catalog (§3.78, phase B)

`internal/remediation/misconfig.go` holds a **deliberately finite** catalog of
rules this application fixes on its own. `FixFor(finding)` returns a `Rule`, and
`Rule.Fix(content, finding)` returns either `patch.Edit`s or **a reason for
declining**. Declining is a first-class answer: an absence sends the user to the
agent path, which works, where a doubtful edit sends them to a diff they have to
second-guess.

Ids are matched through `ruleKey`, which reduces `AVD-DS-0002` and `DS002` to
one key. Trivy uses both spellings in different fields, and matching only one
would leave the catalog silently inert the day the other reached a `Finding`.

The one rule today is the root user. It inserts the `adduser` block before the
final stage's first `CMD`/`ENTRYPOINT` — after it, the `USER` would change
nothing — and switches to the account it just created. A bare `USER 10001` is
*not* what it emits: that uid has no passwd entry, so anything asking the system
who it is degrades at run time, far from the edit. The flags are Debian's, so a
base whose distribution cannot be identified is declined; Alpine is a known gap,
not an oversight.

**`ctrl+o` is one key with one verb — write the file — and the object is the
tab's.** On the Remediation tab it writes a base image, on the Misconfigurations
tab a built-in fix; the label in the shortcut column changes with it, and
everywhere else the refusal names the tab it belongs to (Rule 130). The
confirmation shows **the diff itself** rather than a summary: a block insertion
has no one-line form that conveys what will land in the file.

**A written file is not a fixed one, and the write starts the scan that decides**
(phase B3). The finding on screen was measured before the edit, so the catalog's
own confidence is worth nothing: the verdict comes from a re-scan, exactly as a
base image bump is judged by re-scanning the candidate rather than by trusting
the tag. It is binary — the AVD id is reported for that file, or it is not — and
the comparison goes through `remediation.RuleKey`, since a match that missed
Trivy's other spelling would report every fix as successful.

**The verdict is about the occurrence that was fixed** (§3.80). A Deployment
with two containers is reported for `KSV-0001` twice in one file, and a fix
edits one of them; judged on the rule and the file alone, the untouched one
would read as the fix having failed. `holdsRule` also compares `instanceOf` —
the finding's `Title` and `Message`, which Trivy (`Container 'api' of …`) and
kubeconform (`Deployment/web: …`) make specific — and never the line, which the
fix itself moves.

#### Kubernetes manifests in the catalog (§3.80)

Three rules, in `internal/remediation/k8s.go`, and they only ever touch a
finding whose `IaCType` is `kubernetes` — a Helm template or a Kustomize
overlay is declined, since the file the finding names is not the YAML that was
evaluated.

| Rule | Edit |
|---|---|
| `KSV-0017` privileged | `privileged: true` → `false` |
| `KSV-0001` allowPrivilegeEscalation | `true` → `false`; when absent, a line inserted in the container's `securityContext`, or a `securityContext` block inserted in the container |
| `K8S-API-REMOVED` | the `apiVersion` renamed, **only** for the kinds the deprecation guide marks "No notable changes" (CronJob, RBAC, storage, Lease, IngressClass, PriorityClass, RuntimeClass, APIService, CSIStorageCapacity) |

The edits are still byte ranges: `internal/k8s` turns yaml.v3's node positions
(line, and a column counted in **characters**) into offsets, so comments,
anchors and the file's line endings survive, and nothing is re-serialised. An
insertion goes above a key that begins its own line, at that key's indentation
— never above the key on a `- name:` line, which would land outside the
container. The container is found by the name in Trivy's message, inside the
document and the lines the finding reported; a file that no longer matches is
declined as stale.

Two declines are the API server's own validation rather than caution:
`allowPrivilegeEscalation: false` is refused on a privileged container or one
adding `CAP_SYS_ADMIN`, so the fix would pass Trivy's rule and produce a
manifest nobody can apply. Left out entirely, for §3.78's `USER 1000` reason:
`runAsNonRoot`, `readOnlyRootFilesystem` and dropping `ALL` capabilities pass
their rule and can stop the container from starting. Resource limits have no
universal value, and a seccomp profile can sit at the pod or the container
level.

Starting it on the user's behalf is acceptable because it is an ordinary job
(§3.58): it appears in `:jobs` labelled `verify fix`, and `K` stops it. A scan of
that target started from anywhere else answers the pending verification too —
the run that reports does not have to be the one the fix launched.

Three outcomes, and none of them is silent:

| | |
|---|---|
| the rule is gone | `Info`, and the new result replaces the one on screen — a footer saying "cleared" above a table still listing the rule would contradict itself |
| the rule survived its own fix | `Warn`. Not a failure: the file was written and the rule still fires, which is what the scan exists to find out |
| the result cannot be read | `Error`, logged |

### The Remediation tab — base images, and the tags they could move to (§3.2, phase B)

The sixth tab of the results (`TabRemediation`, `internal/ui/security/remediation.go`)
is not a category of finding: it has no entry in `tabCategory` and its body is a
table of its own, so the findings table, its filter bar and its keys sit behind
it, unused. The keys that filter or open findings are refused on it with a
reason (`reasonFindingsOnly`) rather than reaching that table — a search opened
there would take the keyboard for a table nobody can see. `esc`, `tab` and
`ctrl+r` are not the tab's and work as everywhere. The set of shortcuts is the
same on every tab; `S` (Scan) is greyed off this one and the findings keys are
greyed on it (Rule 130).

**Opening the tab reads, `S` measures.** Opening it — once per result — reads
the Dockerfiles under the repository (`dockerfile.Find`, by name, bounded in
depth and count) and lists each base image's tags; that is cheap. Nothing is
scanned until `S`, which scans every image with no result, or one older than 24
hours, once each however many stages name it. A result stands for a day because
the vulnerability database moves daily and an older count is about another one.

Four packages, each with one job:

- `internal/patch` — `Rewrite` applies `Edit{Span, Old, New}` to bytes, `Diff`
  is what the user is shown, `WriteIfUnchanged` puts it on disk only if the file
  still holds what the edit was computed from. **It knows nothing about what it
  is editing**: it lived in `internal/dockerfile` and never read a Dockerfile,
  which is why §3.78 moved it out rather than writing a second copy for
  misconfigurations.
- `internal/dockerfile` — `Parse` reads the `FROM`s with the **byte range that
  spells each image**, so a later edit replaces those bytes and nothing else. An
  image that comes from a single `ARG` default is located at that default; one
  assembled from several pieces (`node:${V}-alpine`) resolves but is not
  editable. `FROM scratch` and a reference to an earlier stage are not images.
  Every stage is read: a CVE in a build stage can reach the image that ships.
- `internal/remediation` — `ParseRef` and `Candidates`, the tag policy: a
  candidate keeps the current tag's **variant** (alpine stays alpine) and its
  **precision** (`3.18` is offered `3.21`, not `3.21.1`, which would pin what it
  left floating), is strictly newer, and — under `scan.base_image_track`
  `same-line`, the default — stays on the same major. `next-major` also takes the
  smallest higher major that exists. When there is no candidate it says why. `Discover`
  walks it, asking the registry once per repository.
- `internal/oci` — `ListRegistryTags`, the one tag lister: Bearer flow, and it
  **follows `Link: rel="next"`** across pages. Docker Hub answers a whole list in
  one response when no page size is asked (9 125 tags for `library/node`), but a
  registry that caps a response would otherwise hide the newest tags.

A candidate is scanned with `Scanner.ScanRemoteImage`: `trivy image --image-src
remote`, the vulnerability stage only, never pulled into the engine, no engine
socket mounted in container mode. Its result goes to `remediation-scans.json`,
**a cache of its own**: the inventory drops an image the engine no longer holds
(`cache.ImageGone`), which a remote-scanned candidate never is, so its entry in
the shared image cache would be invisible to the inventory yet counted by
whatever reads the file without that filter. The file is written atomically
under a lock, since scans of several candidates finish together.

The scans are not in the jobs registry (`:jobs`): the tab keeps its own set of
images in flight and its own spinner, like the inventory's load. Trivy runs one
process at a time (`scan.trivySem`), so the scans queue there rather than in
parallel, and `max_concurrent_scans` does not apply to them.

The comparison is in **CRITICAL + HIGH**, against the image as written; only a
candidate scanned on the same day as the current image has a delta, because a
count from another database says nothing about a bump. A candidate is evidence,
not a verdict: the scan says the CVEs are gone, not that the application still
runs on the new base.

The **Update** column puts an arrow on a base image whose registry holds a newer
patch tag or new content behind the same tag (§3.88, `internal/imageupdate` —
see `network.md`). It compares with the digest the Dockerfile pins, or with the
image the engine holds under that name.

#### A floating tag (§3.79)

`Ref.Floats` is true for a reference with no digest whose tag carries no
version (`SplitTag` finds none): `latest`, an untagged reference (the implicit
latest), `main`, a codename such as `bookworm-slim`, a vendor tag rebuilt in
place (`dhi.io/node:dev`). `Entry.Floating` carries it to the tab, and the
reason is `remediation.ReasonFloating` — it no longer shares one sentence with a
digest-only reference, which has its own (`ReasonPinnedByDigest`) since pinned
content is the opposite of floating content.

What changes is the cache, not the table: `remediation-scans.json` is keyed by
the reference as written, and a floating reference never changes when the
registry replaces what it points to. So `refsToScan` ignores
`remediationFreshFor` for a floating image — `S` scans it again every time — and
the count shown until then is the last one, with its age in the Scanned column.
Nothing is written for it: rewriting it to `image:tag@sha256:…` would stop it
following upstream fixes, which is a pinning policy, not a remediation
(Dependabot and Renovate keep the two apart).

The detection is a reading of the tag, not a proof. A **versioned** tag rebuilt
in place — `alpine:3.20` moving to the next patch, a `dhi.io/python:3.13`
republished — is not floating here: it gets candidates as before and keeps the
24-hour window. Knowing it changed would take the registry's digest for the tag,
compared with the one the scan measured; that is not done.

### Writing the chosen bases — `space`, `enter`, `ctrl+o` (§3.2, phase C)

DevDesk proposes; it does not decide. `space` chooses the candidate under the
cursor for its stage — **only one that has been scanned**, since a bump is
proposed with its evidence — at most one per stage, and one that cannot be
edited in place (a reference assembled from several build args) is refused with
the parser's reason. Two stages that read one `ARG` are one place in the file:
choosing different bases for both is refused when the second is chosen, and
`patch.Rewrite` refuses it again as `ErrConflict`. `enter` opens the diff in
the viewer. `ctrl+o` writes.

`ctrl+o` never acts on the key alone. It computes the write first — every file
as it would become, and `git.StateOf` for each — and only then opens a
confirmation, whose default answer is No. The confirmation names each file, line
and change, and says what git will and will not be able to undo, because
"nothing is lost" is true of the ordinary case only:

| State of the file | What the confirmation says |
|---|---|
| tracked, clean | review with `git diff`, undo with `git checkout` |
| tracked, uncommitted changes | `git checkout` would discard them along with this edit |
| untracked or ignored | not tracked by git — the write cannot be undone with git |
| outside a repository | same |
| git could not be read | the write may not be undoable |

None of these blocks the write: the user decides, knowing. DevDesk makes no
commit, branch or push.

The write is `patch.Rewrite` — the bytes at the ranges `dockerfile.Parse`
located and nothing else, so comments, CRLF and a missing final newline
survive — through `WriteIfUnchanged`: the file must still hold **exactly** what
the diff was computed from (whole-content comparison, stronger than a hash), or
the write is refused with a footer error and nothing of that file is touched. It
goes through a temporary file and a rename in the same directory, keeps the
permission bits and follows a symbolic link to its target. Several files are
written one after the other, stopping at the first failure; each is whole or
absent, and the message says how many were written before it. A reference pinned
by digest loses the pin, which the confirmation says. A stage that reads its
image from an `ARG` has that ARG's default changed — a `--build-arg` on the
command line can still override it, which a file cannot show.

The write is **not exposed over MCP**: it is a gesture of the user in the TUI,
and an agent has its own tools for editing a file.

### Image signatures — `internal/trust` and the cosign verifier (§3.82)

Whether an image's content is the one its publisher released — a question no
CVE scan answers, since a tag republished with altered content shows nothing
catalogued. The decisions are §3.82's; this is where they live.

**`internal/trust` holds the policy and the decision, not the tool.** It
imports nothing of DevDesk's own:

| File | Holds |
|---|---|
| `rule.go` | `Rule`, `Source` (C user, B built-in, A continuity), `Mode` (key, keyless, `expect: none`), `Rule.Fingerprint` |
| `policy.go` | `~/.devdesk/trust.yaml`, **strict**: `KnownFields(true)`, `version: 1`, exactly one mode per rule, `notation:` refused, any error rejects the whole file |
| `match.go` | `Repository` (normalized, Docker Hub spelled out), `Lookup` — user rules first, then built-in, first match wins; `*` is one segment, a trailing `/**` any depth |
| `builtin.go` | B: distroless and Chainguard keyless, DHI in key mode with the **embedded** `keys/dhi-2.pub` |
| `verdict.go` | `Verdict` and `Decide` — §3.82's table, the only copy |
| `evaluate.go` | `Verifier`, `Evaluate` (pinned references only — a tag can move between check and use), `Continuity` |
| `cache.go` | `Cached` and `FileStore`, `~/.devdesk/cache/signature-verdicts.json` |

Three things the code does that are easy to undo by accident:

- **`trust` cannot import `internal/remediation` or `internal/cache`**: both
  import `internal/scan`, which implements `trust.Verifier`. So `Repository`
  re-reads a reference the way `remediation.ParseRef` does
  (`TestRepositoryAgreesWithRemediation` holds them in step), and the verdict
  cache lives in `trust`.
- **Continuity never believes an identity it read.** `Identities` returns
  *claims*; each is verified strictly on the image in use before being asked of
  the candidate. A permissive check passes on *one* valid signature, so a forged
  certificate next to the attacker's own valid signature would otherwise read as
  Verified (`TestContinuityNeverBelievesAClaimedIdentity`).
- **A Failed verdict is never cached**: under a user rule it blocks, and must
  not outlast the network coming back.

**`internal/scan/cosign.go` is the `Verifier`**, on the model of plumber:
`toolCmd`, the package `runner`, binary or container. Everything below was
measured on cosign v3.1.3 (§3.82, "Mesure"):

- **Only exit codes 0, 10, 11 are read as they are.** Keyless: any other code
  (1, 12…) goes through a permissive check — 0 is a mismatch, 10 unsigned,
  anything else a failure. 12 is not a mismatch: a signature blob the proxy
  would not serve gives 12 even when every identity is accepted.
- **Key mode fails closed**: every code but 0 and 11 is Unsigned. A wrong key
  exits 1 on the bundle format, the same as an unreachable registry, and there
  is no permissive check for a key. A tool that did not start, or a cancelled
  context, is still Failed — that is not an exit code.
- `--experimental-oci11` on every `verify` (DHI keeps its signature in the OCI
  referrers) and never on `download attestation`, which refuses it.
- **Credentials through the environment**, `COSIGN_REGISTRY_USERNAME` /
  `_PASSWORD`; a container gets `-e NAME` with the name alone.
- **A key is always a file DevDesk writes**, 0644 — the container's user is not
  its owner, and 0600 is refused (measured).
- `DefaultCosignImage` is **pinned by digest**: the verifier is the trust
  anchor. Bumping it means measuring the exit codes again.
- No TUF cache is mounted into the container: a fresh root is fetched each run
  (measured ~3 s, and 30 concurrent runs on an empty root all passed), which
  avoids a writable host mount the container's user could not write anyway.

**In the Remediation tab** (`remediation_signatures.go`), every image is
checked in the background once the candidates are known — `imagepull.Check`,
the pull's own sequence without the pull: the base against the rules alone, a
candidate also by continuity with the base's tag as the registry resolves it
now. The `Sig` column shows it; a `Block` greys `space` with the reason, and a
verdict landing after a choice releases it — or, under an open confirmation,
stops that image from being written. A `Warn` is repeated in the `ctrl+o`
confirmation. Four entries at a time (`signatureSlots`).

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
`categories`, `tools.trivy.ignore_unfixed` and the rest are per context, so counts written
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
