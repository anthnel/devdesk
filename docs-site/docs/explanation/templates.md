# Repository templates

The `:templates` view is a catalog of starting points for new repositories: pick one when you create a project in the forge explorer and the repository is born with its files already in a single initial commit. This page explains how the catalog is built and why it behaves the way it does — for the steps, see [Create a repository from a template](../how-to/create-a-repo-from-a-template.md).

## An entry is a reference, not a copy

A catalog entry holds a name, a description, tags and a **source**. The files stay where they are and are fetched when needed, so a template can't quietly drift away from the repository it came from.

| Source kind | Clone URL / registry | Subdirectory / repository | Ref |
|---|---|---|---|
| `git` | clone URL | optional subdirectory, made the root of the template | branch, tag or SHA |
| `local` | — | a repository directory on this machine | branch, tag or SHA |
| `oci` | registry URL | repository | tag |

The catalog is **global**: one file, `~/.devdesk/templates.yaml`, shared by every context. Every entry is declared by hand (or through the view); nothing is discovered from a registry. The old `registry.templates_repository` key is still accepted so existing config files load, but nothing reads it.

## What a template contains

- **git and local sources go through `git archive`.** What you get is what was *committed*: no `.git`, no ignored files, no uncommitted edits. A template must not depend on someone's working tree, so a `local` directory that isn't a git repository is refused rather than copied raw. `export-ignore` in `.gitattributes` is honoured — it is git's own answer to "what belongs in a release".
- **A remote git source** is fetched shallowly at the exact ref. Pinning a SHA works, which makes a template reproducible.
- **A symlink or a submodule inside a template is refused**, by name. Both would otherwise arrive as an empty file or directory — a template silently missing something it claims to hold.
- **Limits: 500 files and 32 MiB.** They are enforced while reading, so an oversized repository is never held in memory, and before anything is created on the forge.

### Credentials follow the host

The forge token is sent only to a git source on the forge's own host, and the registry password only to an OCI source on the registry's host. Anything else is fetched anonymously, and nothing is ever sent over plain `http://`. A private repository on some other host therefore needs an SSH URL.

## The catalog file is not trusted

Because the file can be shared or hand-edited, every entry is validated when it is saved *and* when it is loaded. A bad entry — or a slug declared twice — doesn't break the catalog: it is set aside, the footer warns you, the rest stays usable, and the entry remains in the file so saving another one never deletes a line you edited by hand.

Accepted git URLs are `https`, `http`, `ssh`, `git` and scp-style. `ext::` (runs a command) and `file://` (reads this machine) are refused — a checkout on this machine is what the `local` kind is for. A URL or ref starting with `-` is refused too, since git would read it as an option.

If the file can't be parsed at all, writes are refused: saving over it would replace whatever you had. `N`, `E` and `D` are greyed out with the reason.

## Scanning a template

A secret in a template ends up in every repository made from it, so `S` scans what the template *would* put in a repository before it does. The template is fetched, written to `~/.devdesk/cache/template-scan/<slug>`, and scanned as an ordinary directory by Trivy and Gitleaks (whichever is installed). The result appears in the `:sec` inventory like any other scan and opens in the same results view.

- There is **no CI score**: a template has no pipeline to grade.
- The scan is a background job — `:jobs` lists it and `K` stops it.
- `S` is greyed out when neither scanner is available, or while that template is already being scanned.

## The cache, and syncing with `F`

What a source returns is kept in `~/.devdesk/cache/templates/<slug>.json`. Previewing, scanning and creating a repository all read through it, and the `:templates` view and the explorer share the same file.

- **It never notices the source moving.** A branch that gained a commit is served as first read until you press `F`. There is deliberately no expiry: a silent refetch would make "what will this repository contain?" depend on when it was last asked.
- **A copy answers only for the source it was made from.** Edit an entry to point elsewhere and the old copy is ignored.
- **A failed sync keeps the previous copy**, so an outage never costs you a template that worked yesterday.
- **A copy that can't be read back counts as a miss**, not an error — the template is simply fetched again.
- **Deleting an entry deletes its copy.**

`F` runs as a background job (`:jobs`), and is greyed out with no row selected or while that template is already syncing.

## Creating a repository from a template

The explorer's creation form has a **Template** field (projects only) that reads `none` — an empty repository — until you choose one. `enter` on the field opens the catalog as a picker; `backspace` puts it back to none.

**The template is fetched before the repository is created.** A bad ref, a network failure, a refused login, a template deleted meanwhile, or one over the limits all fail with *nothing created*, and the run detail says `template unavailable — nothing was created`. Only the initial commit itself can still fail once the repository exists; then the repository stays empty, the footer says so, and the run item is marked **failed**, since what you asked for was a repository with a template in it.

A create can't be cancelled (a request already sent can't be unsent), so it is bounded by a timeout instead: one minute for the repository, ten for the initial commit — GitHub needs one request per file. See [Forge](forge.md) for why the initial commit is a single parentless commit on both backends.

## Known gaps

- A private git repository on a host other than the forge's is fetched anonymously and fails with git's own message.
