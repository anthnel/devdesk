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
Entries read from a registry catalog (`FromOCI`) depend on the active context;
they are `Discovered`, never written to the file, and `Merge` hides one once a
declared entry has the same source — adopting it must not list it twice.

## Fetching

`Fetch(ctx, src, Credentials)` returns `[]File{Path, Content []byte, Executable}`.

- **git and local both go through `git archive`.** What comes back is what was
  *committed*: no `.git`, nothing ignored, no uncommitted edit — a template must
  not depend on someone's working tree. A directory that is not a repository is
  refused, not copied raw. `.gitattributes export-ignore` is honoured, which is
  git's own answer to "what is part of a release".
- **git remote** is `init` + `fetch --depth 1 <ref>` + `archive FETCH_HEAD`, in a
  temp directory removed before returning. `clone --branch` would refuse a SHA,
  and a template pinned to a SHA is the reproducible kind. Nothing is cached yet:
  `F` (sync) in the view is what will own a cache.
- **oci** reuses `oci.Client.DownloadTemplate`, now returning `Entries`
  (bytes + execute bit) beside the old string map.
- A symlink in a template is **refused**, naming it. It used to be read as an
  empty file — a template silently missing what it says it holds.

**Credentials are the caller's decision.** `Credentials{Token, Username,
Password}` is resolved for the source's own host; this package never chooses
which host a secret goes to, as `git.Clone` does not. A zero `Credentials` is
the safe default.

## The catalog file is not trusted

It can be shared or hand-edited, so `Entry.Validate` runs on `Put` **and on
load** (a bad entry fails `Open`, naming the file):

- git URLs must be `https`, `http`, `ssh`, `git` or scp-form. `ext::` runs a
  command and `file://` reads this machine; a checkout on this machine is the
  `local` kind precisely so neither is needed.
- A URL or ref beginning with `-` is refused — git would read it as an option.
  `internal/git` repeats that check, since it is the last stop before `exec`.

Writes go to a sibling file renamed over the catalog, and memory only changes if
the write succeeded, so a failure leaves both agreeing.

## The `:templates` view — `internal/ui/templates`

A `datatable` (icon, Name, Tags, Source, Ref, Origin) over the catalog merged
with what the context's registry lists. `Origin` is the first column dropped for
lack of room. The body is always the table (Rule 139); the count is in the
header and the registry listing is a footer status with a spinner (Rule 128).

| Key | Does |
|---|---|
| `N` | new entry — form in the viewport (Rule 112), source kind is a `←→` cycle field (Rule 132) |
| `E` | edit. On a **discovered** row this *adopts* it: saving writes a declared entry with the same source and `Merge` stops listing the registry's |
| `D` | delete after a confirmation that defaults to No. Greyed on a discovered row, with the reason |
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

**Credentials are resolved per host, in `Update`** (`Model.credentialsFor`): the
forge's token goes only to a git source on the forge's host, the registry's
password only to an OCI source on the registry's. Anything else is fetched
anonymously — a token authenticates one host, and the catalog can be shared, so
the entry cannot be what decides.

## Not done yet

- The picker in the creation form, and wiring `applyTemplate` to `Fetch`; until
  then the explorer keeps its own OCI-only path.
- `F` (sync) and the cache it would own: every preview refetches.
- `S` (scan) on a template's fetched content.
- A private git repository on a host that is not the forge's is fetched
  anonymously, so it fails with git's own reason.
