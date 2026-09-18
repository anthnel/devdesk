# Repository templates — the catalog

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

`internal/template` is the catalog of templates a new repository can be filled
from, and how their files are fetched. The view (`:templates`) and the picker in
the creation form come later and are built on this package; the plan is
`.claude/plans/2026-09-18-repo-templates.md`.

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

## Not done yet

The view, the picker, the cache, and wiring `applyTemplate` to `Fetch`. Until
then the explorer keeps its own OCI-only path.
