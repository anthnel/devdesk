# Security Scanning

DevDesk orchestrates three external security tools — Trivy, Gitleaks, and
plumber — against local repositories and container images, caches their
results, and surfaces everything through a unified security inventory view.
This page explains the model behind that orchestration: how tools are
located, how results from different tools are merged into one taxonomy, and
how the two-tier cache stays consistent across scans launched from different
screens.

## Locating the tools

Each tool's location is a declared setting, not an autodetected fact.
`scan.tools.<tool>.source` — for `trivy`, `gitleaks`, `plumber`,
`kubeconform`, `helm` and `kustomize` alike — takes one of:

| Value | Resolution |
|---|---|
| `auto` (default) | the binary if present, else the Docker image |
| `binary` | the configured path, or the name on `PATH` — **fails outright** if not found, rather than silently falling back to Docker |
| `image` | always the configured Docker image, even if a binary is installed |

`scan.Detect` resolves every tool into a `Report`, and `Report.Spec(tool)`
produces a `ToolSpec` (source + binary path + image) that command builders consume directly, rather than threading
a raw `(source, image)` pair through several layers and hoping a configured
path gets read somewhere along the way.

The choice to fail loudly in `binary` mode is deliberate: silently falling
back to Docker is exactly what would make a misconfigured path invisible —
the scan keeps "working," just with a different tool than the one requested.

## Orchestration

`internal/scan/` runs Trivy and Gitleaks concurrently and streams progress:

- `scanner.go` — runs both tools, reports progress via a channel
- `trivy.go` — CVE, secret, license and misconfiguration detection
- `gitleaks.go` — secret detection with custom rule-file support
- `category.go` — the single place that decides which family a finding belongs to (see below)

### Secret scanning uses two tools on purpose

Gitleaks reads a repository's working tree and git history; Trivy reads a
target's content directly. Only Trivy's path applies to a container image —
Gitleaks has no way to scan one — so an image scan used to have no
secret-detection stage at all. Both tools are gated behind a single
`scan.categories.secret` setting and feed one Secrets tab, with a `Source` column
identifying which tool produced each finding.

Two details fall out of running both:

- The vulnerability stage explicitly passes `--scanners vuln` for image
  scans. Trivy's default there is `vuln,secret`, so without the explicit
  flag an image scan would silently also run a redundant secret pass whose
  output nothing consumed — and would now duplicate every secret finding.
- The "exclude" action (add to `.gitleaksignore`) only applies to Gitleaks
  findings. That ignore file matches on a Gitleaks-specific fingerprint that
  a Trivy secret finding doesn't carry; fabricating one would report success
  for an exclusion that will never actually match anything.

### Gitleaks' exit code is not enough on its own

A clean repository exits `0` and writes `[]`. Secrets found exits `1` *with*
a report on stdout. A fatal error — a missing or unparsable `--config` — also
exits `1`, but with **nothing** on stdout. The distinguishing signal is
whether a report was actually written, not the exit code alone: treating
"exit 1, no output" as "no secrets found" reports a scan that never ran at
all as clean, with a green icon to match.

### Mounting a custom rules file

When `scan.tools.gitleaks.config` is set, it's expanded to an absolute path at
config-load time — a relative path would mean different things depending on
whether the tool runs as a native binary (DevDesk's working directory) or
inside a container (the container's working directory), and since the file
gets bind-mounted in, those two readings would resolve to different files.
The path is validated as readable before the scan starts, because
`docker run -v` on a host path that doesn't exist silently **creates a
directory** there instead of failing — which would otherwise surface as a
much stranger error later. `scan.tools.plumber.config` follows the same rules for
the same reasons.

## CI scoring via plumber

`plumber` grades a repository's CI configuration: a letter A–E, a numeric
score, and the issues behind it. A few design points hold this integration
together:

- **Scoped to the current context's forge.** A repository hosted elsewhere
  isn't scanned anonymously — it's simply not scannable from this context,
  and its cell is left empty rather than attempted. The forge match is
  checked before any token lookup, so a foreign repository's credentials are
  never touched.
- **Resolved per repository, not per batch.** A batch can span repositories
  on different remotes; each repository's own remote decides which forge and
  token apply.
- **The provider is passed explicitly**, never inferred from the URL — a
  GitHub context never passes a GitLab-specific flag, because that flag is
  part of how plumber decides which API to speak at all.
- **The exit code carries real meaning:** `0`/`1` mean a grade was produced,
  `3` means the score was *withheld*, `2` means the run failed. Only `2` is
  treated as a failure.
- **A withheld run's letter is never shown**, even though plumber writes one
  anyway — and it tends to flatter, since a control that never ran finds no
  issues. A verdict of "nobody graded this" is kept distinct from any actual
  letter grade.

### The CI column

Both the workspace list and the security inventory carry a `CI` column,
gated behind `scan.categories.ci` — off by default, since otherwise it
would render as an empty column on every row for the life of the view. Two
absences are deliberately distinguished:

- a **dash** means "not yet scanned"
- an **empty cell** means "not scannable from here" (wrong forge)

A directory aggregates its nested repositories differently for counts versus
grades: severity counts sum across nested repositories, but a CI letter
doesn't compose the same way — a directory shows the worst grade among its
repositories rather than an average or a sum.

### The CFG column

When `scan.categories.misconfig` is on, the workspace list, the security
inventory and the OCI view's Images tab gain a `CFG` column, placed after the
four severity counters and before `CI`. It is the number of misconfigurations,
coloured by the worst severity among them:

| Cell | Meaning |
|---|---|
| `-` | no stage read this target — the category was off, Trivy was missing, or the run failed |
| `0` | a stage looked and found nothing |
| `12` | twelve misconfigurations |
| `12?` | the count is **partial** — a Helm chart or Kustomize overlay nothing rendered, or, on a directory, a repository below it that was never scanned |

It is a count rather than the glyph the secrets column uses. A single secret
is already an alarm, but almost every repository with a Dockerfile carries some
misconfigurations, and a glyph would look the same on one as on two hundred.
It is a single column rather than four because a misconfiguration backlog is
read as a whole.

`0?` is the case the marker exists for. A bare `0` on a repository made of
charts would report it clean when nothing in it was ever read. A directory
adds up its repositories' counts — unlike a CI letter, a count composes — and
is partial as soon as any of them is. An image gets the column too: Trivy reads
the Dockerfile instructions baked into its layers.

## Kubernetes manifests

Two questions are asked of a repository's Kubernetes manifests, by two tools.
Trivy's misconfiguration scan asks whether they are **safe** — root user,
privileged container, missing limits — and reads Helm charts too. kubeconform,
ticked under `scan.categories.misconfig`, asks whether the API server would **accept**
them: a wrong type, a missing required field, an unknown field, an
`apiVersion` that `scan.tools.kubeconform.kubernetes_version` no longer serves. Both land on the
Misconfigurations tab; the Source column says which dialect (`kubernetes`,
`helm`, `dockerfile`…) or `schema` for kubeconform.

Only files that are manifests are validated — a YAML document with an
`apiVersion` and a `kind` — so a CI file or a Helm values file is never
reported as an invalid resource. Helm charts and Kustomize overlays are
validated once rendered, when `helm` or `kustomize` is available: the chart is
linted and rendered, the overlay built, and each finding points at the
template or the kustomization it came from. Without them, those directories
are left unvalidated and the log says so. Custom resources are skipped: their schema is
in a CRD kubeconform does not read. Nothing ever connects to a cluster.

## The build context

A `COPY . .` sends the whole directory to the builder, and what no
`.dockerignore` leaves out ends up in a layer of the image. Neither Trivy nor
hadolint reports it, and the secret scanners cannot: `.git` matches no secret
pattern, yet a secret committed then deleted is still in its history. So
DevDesk checks it itself, whenever the Misconfiguration category is on — no
tool to install.

A finding needs three facts, all checked: a `COPY` or `ADD` of the whole
context in a stage the final image is built from; a `.git` directory, or a
file such as `.env`, `*.pem`, `*.key`, `id_rsa`, `credentials.json` or
`.npmrc`, actually on disk; and no ignore file that may apply excluding it —
the context's `.dockerignore`, or the `<Dockerfile>.dockerignore` BuildKit
reads first. What cannot be established for certain is silence: a pattern
DevDesk cannot evaluate, or a `!` that may bring part of `.git` back.

Only a Dockerfile at the root of the scanned directory is checked. Nothing in a
Dockerfile says where its build context is, and at the root "the context is
this directory" is the one safe assumption; a Dockerfile further down is
logged as not checked.

`ctrl+o` fixes the `.git` finding by appending `.git` to the existing
`.dockerignore`, after the usual confirmation. It does not create one: what
belongs in an image is a policy, not something the Dockerfile says. The
sensitive-files finding has no built-in fix.

## Remediation

DevDesk proposes fixes; you decide on them. It fixes two things itself — a
base image, and a short list of misconfigurations — and for everything else it
hands the finding to an agent over [MCP](mcp.md#handing-a-misconfiguration-to-an-agent).
Whichever path produced the fix, it is judged by a re-scan, never by the
confidence of whoever wrote it.

### What a fix would take

Each vulnerability records Trivy's class — `os-pkgs` or `lang-pkgs` — and its
ecosystem (`alpine`, `debian`, `gomod`, `npm`…). The class decides the fix:
moving to a newer base image clears an `os-pkgs` CVE and does nothing for a
`lang-pkgs` one, whose fix is the dependency itself. The results header shows a
`Fixable` count split along those lines. Where the ecosystem is known, a
finding's resolution is a command rather than a sentence, targeting the lowest
fixed version on the installed major line — the smallest change that clears the
CVE.

### Base images — the Remediation tab

The results of a repository scan have a sixth tab, **Remediation**. Opening
it reads every `FROM` of every Dockerfile under the repository, build stages
included, and lists the tags each base image could move to. A candidate keeps
the current tag's variant (alpine stays alpine) and precision (`3.18` is offered
`3.21`, not `3.21.1`). It stays on the same major version unless
`scan.base_image_track` is `next-major`. When there is no candidate, the tab
says why.

Opening the tab is cheap; `S` measures. It scans each candidate straight from
the registry — never pulled into the engine — and compares CRITICAL + HIGH
counts against the image as written. A result counts for 24 hours, because the
vulnerability database changes daily. Each candidate also gets a
[signature verdict](signatures.md) in the `Sig` column.

| Key | Action |
|---|---|
| `S` | Scan the candidates not scanned in the last 24 hours |
| `space` | Choose the candidate under the cursor for its stage — only a scanned one, at most one per stage |
| `enter` | Open the diff in the viewer |
| `ctrl+o` | Write the chosen bases into the Dockerfiles, after a confirmation |

`ctrl+o` never acts on the key alone. The confirmation, which defaults to No,
names each file, line and change, and says what git will be able to undo. For a
tracked, clean file, that is a `git checkout`. A file with uncommitted changes
would lose them along with the edit, and an untracked one cannot be restored
at all. None of this blocks the write. DevDesk makes no commit, branch or push.

Only the bytes that spell each image change, so comments, CRLF line endings and
a missing final newline all survive. The file must still hold exactly what the
diff was computed from, or the write is refused. An image that comes from an
`ARG` default has that default edited; one assembled from several pieces
(`node:${V}-alpine`) cannot be edited in place and says so.

### Built-in misconfiguration fixes

On the Misconfigurations tab, `ctrl+o` writes a built-in fix for the selected
finding when there is one, and the confirmation shows the diff itself. The
catalog is short on purpose: each entry is an edit whose correctness does not
depend on guessing what the image is for.

| Rule | Fix |
|---|---|
| `AVD-DS-0002` | Create an unprivileged `appuser` in the final stage and switch to it, before the first `CMD`/`ENTRYPOINT` (Debian-based images only) |
| `AVD-DS-0005` | Replace `ADD` with `COPY` |
| `AVD-DS-0011` | Add a trailing slash to a multi-source `COPY` destination |
| `AVD-DS-0015` / `0019` / `0020` / `0027` | Clean the `yum` / `dnf` / `zypper` / `microdnf` cache after an install |
| `AVD-DS-0021` | Add `-y` to `apt-get install` |
| `AVD-DS-0022` | Replace `MAINTAINER` with `LABEL` |
| `AVD-DS-0025` | Add `--no-cache` to `apk add` |
| `AVD-DS-0029` | Add `--no-install-recommends` to `apt-get install` |
| `KSV-0001` | Set `allowPrivilegeEscalation: false` on the container |
| `KSV-0017` | Set `privileged: false` on the container |
| removed Kubernetes API | Move the resource to the `apiVersion` that replaced it |
| `.git` in the build context | Add `.git` to `.dockerignore` |

A rule may still decline for a particular file — for example, a base image
whose distribution cannot be identified gets no `adduser` block. Declining
says why and points you to the agent path. A doubtful edit would leave you
second-guessing a diff.

**A written file is not yet a fixed one.** Writing starts a re-scan of that
target — an ordinary job, shown in `:jobs` as `verify fix` — and the verdict is
binary: the rule is still reported for that file, or it is not. If it is gone,
the footer says so and the new result replaces the one on screen. If the rule
survived its own fix, you get a warning, not a failure: the file was written,
and the scan is there to find out whether that was enough.

## Image signatures

With the Misconfiguration category on, a repository scan also checks the
signature of every base image its Dockerfiles name, against your rules and
the built-in ones. Only a proven violation becomes a finding: `DEVDESK-SIG-001`
(signed by someone else, CRITICAL) or `DEVDESK-SIG-002` (unsigned where a
rule requires a signature, HIGH). See [Image signatures](signatures.md).

## One rule decides a finding's family

`scan.Categorize` is the single function that assigns a finding to a
category (CVE, secret, license, misconfiguration). Earlier, two different
pieces of code classified findings independently — the summary counters
switched on `Source` alone, while the results-view tabs also considered
`PkgName` and `Match`. Small disagreements between those two classifications
produced findings that were counted in the header total but displayed in no
tab. Classifying strictly by `Source` closed that gap, which is also why
Trivy's secret findings got an explicit source value of their own
(`trivy-secret`) instead of being distinguished by inspecting `Match`.

## The secret verdict is tri-state

`Result.SecretVerdict()` is the one place that decides whether a target
carries a secret, and it returns a `*bool`:

- `nil` — no secret stage ran (feature disabled, tool missing, or the stage errored)
- `false` — ran, found nothing
- `true` — ran, found something

Collapsing "didn't look" and "looked and found nothing" into the same
`false` is the trap this avoids: a repository whose only secrets were
Trivy's used to read as clean under a Gitleaks-only check, and an image had
no secrets field at all. Both scan caches persist `Sensitive *bool` for
exactly this reason, and a separate `SecretsScanned` flag records whether a
stage actually completed.

## Findings and errors are shown separately

A stage that fails is logged, not surfaced as a panel that replaces the
findings table — previously, a single failed stage (say, plumber) took the
entire results table down with it. Now the table always renders, and the
footer names which stage(s) failed and points at the logs, since a
transient toast message that clears after a few seconds would leave the
user looking at a result that appears complete but isn't.

## The security inventory

The security inventory opens on everything the current context has already
scanned, drawn from the two on-disk caches described below — one table over
images and repositories, sorted by CRITICAL count descending.

| Action | Effect |
|---|---|
| Open a row | shows its stored findings (reads the cache — never triggers a scan) |
| Rescan one row | overwrites that row's cache entry |
| Rescan all | rescans every row, with an optional cache purge |
| Reload | re-reads the caches from disk |

A target is displayed with a friendly alias (a configured registry alias for
images, `~` for the home directory in paths), but the **cache key never
changes** — every action resolves against it, independent of how the row is
currently rendered. Sorting and filtering use the raw key; only the display
uses the alias, so renaming an alias never silently reshuffles the table.

### Reconciling with reality

A target with nothing behind it anymore — a deleted image, a removed
repository directory — isn't listed, even though its cache entry may still
exist on disk. Two rules matter here:

- **A failed existence check is never read as "it's gone."** If the
  mechanism used to enumerate images can't reach the daemon, every image
  stays in the inventory rather than the list going empty — which would
  otherwise read as "zero critical findings," the one wrong answer nobody
  would think to question.
- **Reconciliation hides, it doesn't delete.** The cache entry and stored
  result remain on disk; a transient failure to detect a target must never
  destroy a scan nobody asked to remove.

## Two independent scan caches

`internal/cache/` holds two caches:

- `ImageScanCache` — keyed by `repo:tag`, index at `~/.devdesk/cache/image-scans.json`, full results content-addressed under `image-results/<sha256>.json`
- `WorkspaceScanCache` — keyed by absolute repository path, index at `~/.devdesk/cache/workspace-scans.json`, results under `workspace-results/<sha256>.json`

Both store the four severity counts plus the tri-state secret verdict.

**Only the workspace cache is scoped to a configuration context; the image
cache deliberately is not.** What decides scoping is whether the *key*
itself is context-dependent:

| Cache | Key | Scoped to context? |
|---|---|---|
| `WorkspaceScanCache` | absolute path, reached through a per-context `workspaces_dir` | yes — the same path can mean different work in different contexts |
| `ImageScanCache` | a local Docker reference | no — `docker image ls` answers for the machine, not for any one configuration |

Image results used to be scoped per context, which was a bug: two contexts
inspecting the same `nginx:1.25` are looking at literally the same image, so
switching contexts made previously-scanned images look unscanned even though
nothing had changed. The content-addressed result blobs were never actually
scoped by context in the first place, so the index and the blobs disagreed —
and the index was the one that was wrong.

### Which context a background scan writes under

A scan's result is written under the context it was *launched* in, not
whatever context happens to be active when the tool finishes. A batch scan
over many repositories can run for minutes, and switching context mid-scan
is ordinary; if the writer read the *current* context instead, results
could land under the wrong entry entirely — the context that launched the
scan would see its result vanish and appear unscanned forever, while
whichever context happened to be on-screen when the scan finished would
silently gain results for paths that mean nothing there. Reading the
*current* context is still correct for a plain load — there, the context
genuinely is what the user asked to see.

### Merging duplicate entries

Because the image cache is unscoped, the same image can be independently
discovered from two different contexts' history. A collision there is
resolved by keeping whichever entry has the more recent scan timestamp — the
image itself is identical either way; only the vulnerability database behind
the scan may have moved on.

!!! note
    A cached result may have been produced under a different context's scan
    settings (`categories`, `tools.trivy.ignore_unfixed`, and similar are per-context).
    That's an accepted tradeoff — the entry's age is visible in the UI, and a
    manual rescan is always one keystroke away.

## One shared job registry

Scan-in-progress bookkeeping lives in `internal/jobs`, shared by every view.
Previously, each list view (workspaces, images, the security inventory)
tracked its own in-flight scans independently — which meant the same
repository could be scanned twice concurrently, once launched from the
workspace list and once from the inventory, with both writing the same
cache entry and neither aware of the other.

Every view now reads from one broadcast snapshot and asks it the same kind
of question ("is this path scanning right now?"), so a scan started from
any view marks the corresponding row as busy in every other view that lists
the same target.

## Cancellation

Every scan launch site creates a cancellable context and attaches the cancel
function to the "scan starting" message. Cancelling kills the underlying
tool process directly, and the resulting failure flows through the ordinary
error path — a cancelled scan simply reports itself as a failed one, rather
than needing a second, separate "was it cancelled" signal.
