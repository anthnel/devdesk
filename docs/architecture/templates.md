# Repository templates — the catalog

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

`internal/template` is the catalog of templates a new repository can be filled
from, and how their files are fetched. `internal/ui/templates` is the
`:templates` view over it (below). The picker in the creation form comes later;
the plan is `.claude/plans/2026-09-18-repo-templates.md`.

## An entry is a reference

An `Entry` holds a slug, a name, a description, tags and a `Source`. The content
stays where it is and is fetched when a repository is created from it, so a
template cannot drift from its original.

| `Source.Kind` | `URL` | `Path` | `Ref` |
|---|---|---|---|
| `git` | clone URL | optional subdirectory, made the root | branch, tag or SHA |
| `local` | — | repository directory | branch, tag or SHA |
| `oci` | registry URL | repository | tag |

The catalog is **global**: `~/.devdesk/templates.yaml`, not one per context.
Every entry is declared. There used to be a second source, entries listed from a
registry's `/v2/_catalog` and merged in; it was removed because nobody used it,
and an OCI template is declared by hand like any other. `registry.templates_repository`
is therefore read by nothing and stays in the schema only so existing files
still load.

## Fetching

`Fetch(ctx, src, Credentials)` returns `[]File{Path, Content []byte, Executable}`.

- **git and local both go through `git archive`.** What comes back is what was
  *committed*: no `.git`, nothing ignored, no uncommitted edit — a template must
  not depend on someone's working tree. A directory that is not a repository is
  refused, not copied raw. `.gitattributes export-ignore` is honoured, which is
  git's own answer to "what is part of a release".
- **git remote** is `init` + `fetch --depth 1 <ref>` + `archive FETCH_HEAD`, in a
  temp directory removed before returning. `clone --branch` would refuse a SHA,
  and a template pinned to a SHA is the reproducible kind. What comes back is cached
  (below).
- **oci** reuses `oci.Client.DownloadTemplate`, which returns `Entries`
  (bytes + execute bit).
- A symlink in a template is **refused**, naming it and saying to replace it
  with a regular file. It used to be read as an empty file — a template silently
  missing what it says it holds. A **submodule** is refused the same way
  (`git ls-tree` finds the gitlink): `git archive` writes it as an empty
  directory, which would be the same silent gap.

**Credentials are resolved per host** by `CredentialsFor(cfg, secrets, src)`: the
forge's token goes only to a git source on the forge's host, the registry's
password only to an OCI source on the registry's, and anything else is fetched
anonymously — a token authenticates one host, and the catalog can be shared, so
the entry cannot be what decides. Nothing is sent over `http://` either, even to
the forge's own host. A private repository on any other host is not
reachable over HTTPS: use an SSH URL. Both the view and the explorer call it, so
the rule has one place to live.

**A template has limits: 500 files and 32 MiB** (`MaxFiles`, `MaxBytes`),
checked inside `Fetch` and so before anything is created on the forge. They are
enforced **while reading**, not after: `git archive`'s output is capped
(`git.ErrOutputTooLarge`) and the tar/gzip readers stop at the limits
(`oci.Limits`, `oci.ErrArchiveTooLarge`), so a huge repository or a gzip bomb
is never held in memory whole. `checkLimits` still names the biggest file for
what got that far. GitLab
takes every file in one request, so the body size decides; GitHub takes one
request per file, one after the other, so the count does. The numbers are
estimates: the one to verify against a real GitLab instance is its maximum
request body, which has to admit 32 MiB once base64-encoded (about 43 MiB).

## The catalog file is not trusted

It can be shared or hand-edited, so `Entry.Validate` runs on `Put` **and on
load**. A bad entry, or a slug declared twice, does not fail `Open`: it is set
aside (`Store.Problems()`, reported as a footer warning), the rest of the
catalog stays usable, and the entry stays in the file so saving another one
does not delete a hand-edited line:

- git URLs must be `https`, `http`, `ssh`, `git` or scp-form. `ext::` runs a
  command and `file://` reads this machine; a checkout on this machine is the
  `local` kind precisely so neither is needed.
- A URL or ref beginning with `-` is refused — git would read it as an option.
  `internal/git` repeats that check, since it is the last stop before `exec`.

Writes go to a sibling file renamed over the catalog, and memory only changes if
the write succeeded, so a failure leaves both agreeing.

## The `:templates` view — `internal/ui/templates`

A `datatable` (icon, Name, Tags, Source, Ref) over the catalog. The body is
always the table (Rule 139); the count is in the header.

| Key | Does |
|---|---|
| `N` | new entry — form in the viewport (Rule 112), source kind is a `←→` cycle field (Rule 132) |
| `E` | edit |
| `D` | delete after a confirmation that defaults to No |
| `S` | scan the template for vulnerabilities and secrets (below) |
| `F` | sync: read the source again and replace the cached copy (below) |
| `V` | preview: asks the router to open the viewer on a listing of the files the template would put in a repository (`previewSource`), so a fetch failure is reported by the viewer's own load path |
| `.` `/` | sort, filter (name, description, tags, source) |

**The slug is derived from the name on creation and fixed on edit.** Renaming a
template must not orphan whatever remembered its old slug.

**A local source has no URL field**; the same three inputs (`url`, `path`,
`ref`) serve every kind and only their labels change, so a value typed before
cycling the kind is kept.

**An unreadable catalog refuses every write.** Saving over a file that could
not be parsed would replace whatever the user had in it; the footer says so and
`N`/`E`/`D` are greyed with the reason. The catalog not being *open yet* is not
greyed — that is not knowing, and greying it for one read would look like a
glitch (Rule 130).

## Creating a repository from a template

The `Template` field of the explorer's creation form (projects only) shows
`none` — an empty repository — until one is chosen.

- **`enter` on the field opens the catalog**, lent by the router in selection
  mode (`templates.NewForSelection`), the second borrow the explorer makes (see
  `scanning.md`). The form is kept as it is meanwhile; `enter` in the picker
  answers `TemplateSelectedMsg{Slug, Name}`, `esc` answers
  `SelectionCancelledMsg`, and the router turns them into the explorer's own
  `TemplateChosenMsg` / `TemplateChoiceCancelledMsg`. `backspace` on the field
  puts it back to none.
- **The picker is the whole catalog**, filterable and sortable, with `V` to look
  inside. N, E and D are not bound there — a mode replaces the list (Rule 130).
  The form keeps the slug and shows the name; the slug is what is submitted.
- **The template is fetched before the repository is created.** A bad ref, a
  network failure, a refused login, a template that is gone from the catalog or
  one over the limits all fail with *nothing created*: the footer says so and the
  run detail reads `template unavailable — nothing was created`. Only the initial
  commit can still fail once the repository exists, and that keeps the old
  outcome: the repository stays, empty, and the footer says it was created empty.
  The run item is **failed** (`created empty — the template's commit failed`), not
  done: what was asked for was a repository with a template in it.
- **A create is bounded, not cancellable.** `jobs.Kind.Cancellable` says no for
  `create` (a request already sent cannot be un-sent), so the guard against a
  stalled forge is a timeout: one minute for the repository, ten for the initial
  commit (GitHub makes one request per file).
- The catalog is read when the template is applied, not when the form opens, so
  a template deleted while the form was open is reported as no longer in the
  catalog rather than applied from a stale copy.

## Scanning a template

A secret in a template ends up in every repository made from it, so `S` scans
what the template would put in a repository — before it does.

The template is fetched, written to **`~/.devdesk/cache/template-scan/<slug>`**
(`template.Materialize`, which replaces whatever a previous scan left there), and
that directory is scanned as a plain directory by the configured scanners. It is
reused as a directory scan on purpose: the result is cached by path
(`cache.StoreWorkspaceScan`, now shared with the workspaces view), so it appears
in the **`:sec` inventory** and opens in the same results view, with nothing new
to build. The footer here says how it ended — the counts, or `no findings`, or a
warning when a stage failed and found nothing (which is not the same claim). The
workspaces list is not involved: the copy is not in it, and nothing pretends it is.

- **Fixed directory, not a temp one**, because the scan is cached by path: a
  fresh directory per scan would leave a dead `:sec` entry behind for each.
- **`Materialize` computes and empties only its own directory.** It takes a slug,
  validates it, and derives the path — a caller cannot point the removal
  elsewhere — and refuses a file whose path would land outside it.
- **No CI score.** A template has no pipeline to grade, and plumber would look
  for a remote it does not have.
- **It is registry work** (`jobs.KindScan`, origin `:templates`), so `:jobs`
  lists it, `K` stops it, and the router stamps the context the counts are stored
  under (D68). `ScanStartingMsg` and `ScanCompleteMsg` implement `jobs.Reporter`
  and are routed through `routeWork`; a test pins that, because implementing the
  interface proves nothing if the router never calls it.
- **`S` is greyed** when neither Trivy nor Gitleaks is available, and while a
  scan of that template is running (whoever started it). Until the scanner check
  comes back it stays lit — greying it for a few frames would read as a glitch
  (Rule 130). The picker has no `S`.

## The cache and `F`

`template.Cache` keeps what a source last returned, in
**`~/.devdesk/cache/templates/<slug>.json`**: the source it answers for, when it
was read, and the files (bytes base64 in the JSON, the execute bit kept). A
preview, a scan and a repository creation read through `Cache.Fetch`, which
serves the copy and reads the source only when there is none. The view and the
explorer share the one file, so a sync in one is seen by the other.

- **It never notices the source moving.** A branch, or a local repository that
  gained a commit, is served as first read until `F` calls `Cache.Sync`. A
  template pinned to a SHA never needs it. There is no expiry on purpose: a
  silent refetch would make "what will this repository contain" depend on when
  it was last asked.
- **A copy answers only for the source it was made from.** The record carries
  the `Source`; a template edited to point elsewhere misses and is read again
  rather than served from the old address.
- **A failed sync keeps the previous copy.** Sync writes only after a successful
  read, through a temporary file and a rename, so an outage never costs a
  template that worked yesterday and a crash never leaves half a record.
- **A record that cannot be read back is a miss**, not an error — bad JSON, or
  content over `MaxFiles`/`MaxBytes`. The cache is an optimisation and no state
  of it may keep a template from being used.
- **Deleting the entry deletes its copy** (`Cache.Forget`, best effort).
- A cache with no directory (no home) stores nothing and always fetches; tests
  give the view and the explorer one of their own (`NewCacheAt`) so none writes
  to the real `~/.devdesk`.

`F` is registry work (`jobs.KindSync`, origin `:templates`, target the slug), so
`:jobs` shows it and the footer status carries it. It is greyed with no row and
while a sync of that template is already running, and absent from the picker.
It is separate from the scan's directory (`cache/template-scan`), which is a
materialised tree for the scanners, not a cache of the fetch.

## Not done yet

- The view does not show how old the cached copy is (`Cache.FetchedAt` has the
  answer; no column reads it).
- A private git repository on a host that is not the forge's is fetched
  anonymously, so it fails with git's own reason: use an SSH URL.
- No spinner on the row itself: the footer status carries the live scan.
