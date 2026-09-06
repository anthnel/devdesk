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
`scan.trivy_source`, `scan.gitleaks_source` and `scan.plumber_source` each
take one of:

| Value | Resolution |
|---|---|
| `auto` (default) | the binary if present, else the Docker image |
| `binary` | the configured path, or the name on `PATH` — **fails outright** if not found, rather than silently falling back to Docker |
| `image` | always the configured Docker image, even if a binary is installed |

`scan.CheckDependencies` resolves all three tools into a `DependencyStatus`;
helpers like `deps.TrivySpec()` each produce a `ToolSpec` (source + binary
path + image) that command builders consume directly, rather than threading
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
`scan.enable_secret` setting and feed one Secrets tab, with a `Source` column
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

When `scan.gitleaks_config` is set, it's expanded to an absolute path at
config-load time — a relative path would mean different things depending on
whether the tool runs as a native binary (DevDesk's working directory) or
inside a container (the container's working directory), and since the file
gets bind-mounted in, those two readings would resolve to different files.
The path is validated as readable before the scan starts, because
`docker run -v` on a host path that doesn't exist silently **creates a
directory** there instead of failing — which would otherwise surface as a
much stranger error later. `scan.plumber_config` follows the same rules for
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
gated behind `scan.enable_ci_score` — off by default, since otherwise it
would render as an empty column on every row for the life of the view. Two
absences are deliberately distinguished:

- a **dash** means "not yet scanned"
- an **empty cell** means "not scannable from here" (wrong forge)

A directory aggregates its nested repositories differently for counts versus
grades: severity counts sum across nested repositories, but a CI letter
doesn't compose the same way — a directory shows the worst grade among its
repositories rather than an average or a sum.

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
    settings (`enable_vuln`, `ignore_unfixed`, and similar are per-context).
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
