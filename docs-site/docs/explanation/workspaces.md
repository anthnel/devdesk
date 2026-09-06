# Workspaces

The `ws` view lists local repositories and plain directories side by side, and lets you scan and sync git repositories in place. This page explains the reasoning behind its file icons and its sync behavior — the "why", not a field-by-field reference.

## The leftmost column: what a row *is*

Each row in `ws` starts with a glyph naming what the entry **is**: a git repository, a directory, or a plain file. This replaced an earlier column that answered a different question — whether a directory looked like a *project* (by sniffing for `go.mod` or `package.json`) — which wasn't useful enough to keep as a whole column.

The repository glyph is the one that earns its place: most of the row's actions (scan, sync, delete, open) only apply to git repositories, and shortcuts that don't apply get greyed out. Without a visual cue for "this is a repository," the user watches keys dim with nothing on the row explaining why.

A few implementation notes worth knowing if you're extending the icon table:

- **Basename is checked before extension.** `Dockerfile` has no extension at all, and `Dockerfile.dev` has `.dev`, which matches nothing — so an extension-first lookup would misclassify both.
- **Icons are colored by role, not by file type.** Coloring by file type (the way tools like `eza` do) tells you nothing actionable — `.go` vs `.rs` doesn't change what you can do with the file. Coloring by *role* (repository / directory / file) mirrors the same three-way split the action-availability logic already uses, so the color anticipates what the shortcut row is about to say.
- **Icons don't get invented for uncertain cases.** File types without a clearly appropriate glyph (`.kt`, `.scala`, `.hs`, `.zig`, `.tf`, …) fall back to a generic code-file icon rather than guessing.
- **The icon column carries no title and isn't searchable or sortable** — it adds no text a user could type, so filtering stays scoped to name and remote.

A git repository is given the *same* visual role wherever DevDesk shows one — in `ws`, in the security inventory, and in the forge explorer. One object, one color, across three otherwise-unrelated views.

## Syncing workspaces

The sync action (`F`) fetches a repository and fast-forwards it if it's safe to do so. It's the mirror image of cloning: the explorer creates repositories that don't exist yet, while workspaces reconciles ones that already do.

### No dedicated screen

Unlike cloning, which opens a selection list because its rows don't exist until the clone runs, sync has nothing new to show — every repository is already a row on screen. So sync decorates existing rows instead of opening a new view: a spinner appears in the row's Git Status cell, the same way a running scan shows in the Scanned cell, and the row's data refreshes once the sync completes.

### Progress vs. summary are different kinds of information

A batch sync updates many rows over time — that's an ongoing *state*, recomputed every frame from the underlying job registry. A completion message ("12 repositories synced"), by contrast, is a one-time *event*. Conflating the two used to mean a progress line could be posted when the first repository started and then silently expire (footer messages have a fixed lifetime) while the ninth repository was still fetching. Splitting them fixes that: progress is derived continuously, and the summary is posted once, when the whole batch settles.

### What "sync everything under here" means

Scan and sync share one targeting rule instead of each getting a separate selection mode: pressing the key on a git repository acts on that repository; pressing it on a plain directory acts on every repository nested underneath, at any depth; anything else does nothing. One rule for two actions is simpler to learn, and their shortcuts appear and disappear together.

The recursive walk stops as soon as it finds a repository — so a repository's own `node_modules` is never descended into. That pruning does the real work of keeping the walk cheap; an artificial depth limit isn't needed once the walk already stops at repository boundaries. The walk also follows Windows junctions/symlinks correctly (checking the actual target rather than trusting `IsDir()` on the link itself), with a cycle guard based on comparing files rather than resolved paths, since path resolution doesn't reliably unwind a junction.

A repository is recognized by *inspecting the directory's own listing* rather than by `os.Stat`-ing a fixed `.git` path — this also catches bare repositories (which have `HEAD`, `objects`, and `refs` at the root, but no `.git`), which would otherwise be silently skipped as scan/sync targets.

### Partial reads are surfaced, not swallowed

If a directory can't be read during the walk, that failure is counted, not silently dropped. The count reaches the screen two ways: a warning is shown when a scan or sync ran on a tree that could only be partly read, and a directory where *nothing* was found because reading failed gets a distinct "couldn't check" message rather than being reported as having no repositories. The reasoning: not being able to look somewhere is not the same claim as having looked and found nothing.

### Sync refuses more than it does

`git.Sync` is conservative by design — every refusal is deliberate, not a missing feature:

| Situation | Result |
|---|---|
| Behind, clean, no local commits | Fast-forwarded |
| Nothing to pull | Reported up to date — unpushed local commits don't change this; sync is strictly the pull direction |
| Local commits the remote lacks | Skipped: "diverged — N commits ahead" |
| Uncommitted changes (including untracked) | Skipped: "uncommitted changes" |
| Detached HEAD, or no upstream configured | Skipped, with the reason named |
| Remote unreachable | Reported as failed (distinct from "skipped" — the repository's state relative to its owner's intent is unknown) |

There is no merge, no rebase, no stash, and never a push. A divergence between local and remote history is a decision about someone's unpublished work; a tool that guesses at resolving it risks destroying hours of work irreversibly.

Notably, the fetch against the remote always runs first, regardless of what the sync ultimately decides to do — because the "how far behind" count comes from a *local* tracking ref that's only accurate immediately after a fetch. This means even a repository whose sync is declined (diverged, dirty, etc.) still has its ahead/behind counts refreshed, because the network round-trip already happened.

### Mutual exclusion

A repository is never scanned and synced at the same time, and neither operation runs on a repository that's mid-deletion: a scan reads the working tree while a fast-forward rewrites it, and running both would produce a report describing a tree that no longer exists by the time you read it. A single "is this path busy" check, backed by the application's shared job registry, guards scan, sync, and delete against each other — including jobs started from *other* views (e.g., a scan launched from the security view is visible to workspaces' guard too, because both read the same registry rather than each view tracking its own state).

### Credentials stay scoped to the configured host

The token used for git operations is only ever attached when the repository's remote host matches the configured forge host. A workspaces tree can hold clones from anywhere — GitHub, a client's self-hosted Gitea, a path on a network share — and attaching a personal access token to every outbound request regardless of destination would leak that token to unrelated hosts. This is also why the generic git-sync logic lives in its own package rather than inside the GitLab-specific integration: keeping the two separate avoids a default where a credential could leak by omission.

## What the "Scanned" column means

The `N/M` count on a directory row is **settled coverage** — how many of the nested repositories have a cached scan result — not live progress. It used to try to be both: during a batch scan, the same cell counted up in real time, which made a mid-batch `3/12` ambiguous (nine remaining? nine never scanned?) shown in a cell far from where the user was looking. Now the cell simply stops counting and spins while a scan is running underneath it, and progress is reported where batch progress belongs: the footer.

The footer message for a batch has three forms, and the third is a deliberate concession:

```
Scanning — 3/12                      one kind of job, started from here
Scanning — 3/12 · Syncing — 1/4      two kinds, both started from here
3 jobs running — see job list        mixed, or started elsewhere
```

A single line can't meaningfully summarize four batches launched from three different views, so when it can't identify the specific jobs, it says so honestly rather than showing a partial, misleading count.
