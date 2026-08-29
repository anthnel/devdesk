# Workspaces — the file icons and the sync

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The leftmost column of `ws` — `internal/ui/fileicon`

A glyph naming what each row **is**: a git repository, a directory, or a file
(§3.46). It replaced a Type column that showed a *project* type —
`detectProjectType` looked for `go.mod` or `package.json` and answered "this is
a Go project", which is a different question and was judged not worth a column.

**The repository glyph is the one that earns the column.** Half the keys here
act on `IsGitRepo` — `S`, `F`, `A`, `D`, `enter` — and Rule 130 greys them
accordingly, so the user watches the shortcuts dim with nothing on the row
saying why. They used to *disappear*, which was the same complaint one degree
worse, and §3.48 is the other half of this fix. Git Status betrays a repository only when it has a readable
branch: a detached HEAD, an empty repository, one git refuses to read all
rendered an empty cell and looked like any other directory. A directory holding
*nested* repositories still reads as a plain directory — `S`, `F` and `A` act on
it too, so a fourth glyph would be defensible.

**The resolution rule is `internal/viewer/detect.go`'s**: the **basename is
consulted before the extension**, because `Dockerfile` has none and
`Dockerfile.dev` has `.dev`, which is in no table. `.gitlab-ci.yml` falls out of
it for free. Nothing is sniffed from content.

It is **not** the viewer's `Kind` table: a dozen Kinds pick a lexer, dozens of
icons name a file, and several share a Kind — `.js`, `.ts`, `.py` and `.rs`
would all be "text". Two questions about one entry, so two tables.

**The glyphs live in the package, not in `theme/icons.go`**, and it is the one
place this departs from the `ForgeIcon` precedent: there the glyph is in `theme`
and the vocabulary in `internal/forge` because a domain package must not import
the UI, whereas here both halves are UI and a table whose key and value sit in
different files cannot be read one entry at a time. `theme` keeps what the
application names — `IconDirectory`, `IconFile`, `IconGitBranch`.

Four things worth knowing before touching the table:

- **Colour, but three of them and not twenty** (§3.57). This reverses a
  decision, so the old one is worth stating: the column shipped uncoloured
  *against* `eza`, on the grounds that a colour on every row informs no one and
  would weaken the ones that signal something — severities, secrets, git status.

  What changed is the granularity, not the appetite. `eza`'s colour tracks the
  **file type**, which is what that argument was about: `.go` against `.rs`
  changes nothing a reader can act on. This one tracks the **action set** —
  repository, directory, file — and it is the same three-way split
  `availability.go` already makes when it greys `S`, `F` and `W`. The colour
  says what the shortcut column is about to say, one glance earlier, which was
  the complaint the glyph itself was added for.

  The old note is not wrong about the cost: every row does now carry a hue. It
  is paid down by keeping the file class **dim** — the row nothing applies to
  is the one that recedes — so what the eye still catches first is a severity
  count or a dirty git status, not the leftmost cell.
- **No title, no `Less`, no `Search`.** It adds no text anyone could type, so
  the filter stays on Name and Remote.
- **Twenty glyphs were checked by eye**, one at a time, in a terminal: a wrong
  codepoint breaks nothing and renders a tofu box, which no test can see. CSS
  and HTML went in the wrong way round on the first pass — MDI's four
  `language_*` glyphs are one contiguous alphabetical run, and that ordering is
  what settles which is which.
- **What has no certain glyph does not get an invented one.** `.kt`, `.scala`,
  `.hs`, `.zig` and `.tf` fall to `IconCodeFile`.

**The colour comes from a role, never from a glyph** (`theme.IconStyle`).
`entryIconRole` answers `IconRoleRepository`, `IconRoleDirectory` or
`IconRoleFile`, and the theme answers the hue — so `fileicon` itself stays a
pure name-to-glyph table with no opinion about colour, which is what lets the
twenty entries keep being read one line at a time.

A git repository here takes the **same role** the explorer paints a forge
repository with, and the `:sec` inventory a scanned one: one object listed by
three views, one colour. That is what a role is for, and it is not something a
per-view palette could have guaranteed.

Three tests keep the table reachable without knowing anything about glyphs:
`TestEveryExtensionKeyStartsWithADot` and `TestEveryTableKeyIsLowercase` catch
the entries that could never match — `filepath.Ext` returns `.go` and the lookup
lowercases first — and `TestNoGlyphIsEmptyOrCarriesStyling` forbids the ANSI
escape Rule 122 bans from a cell.

`.gitlab-ci.yml` is a **declared exception** in `vocabtest`: it is a filename,
not vocabulary — the file is called that whatever forge a context targets.

## The workspaces sync

`F` fetches a repository and fast-forwards it (§3.17). It is the other half of
the line §3.16 drew: **the explorer creates what does not exist, workspaces
reconciles what does.**

**It has no screen of its own, and that is the whole difference from the clone.**
The clone opens a list because its rows do not exist yet — discovery invents
them. Here every repository is already a row the user is looking at, so a second
list would print the same names twice. Sync decorates instead: the spinner goes
in the Git Status cell, exactly as a scan's goes in Scanned, and the counts tell
the truth again when it lands.

**Progress and summary are two different things, and Rule 128 separates them.**
Progress is a *state* — `jobsStatusLine` derives it from the registry snapshot
every frame, because a batch outlives the three seconds a footer message gets,
so a line posted on the first repository would vanish while the tenth was still
fetching. The summary is an *event*, so it is an ordinary footer message posted
when the run settles, with the timer every message gets. They shared one
function while both came from the same `syncRun` struct; once progress came from
the registry they stopped needing to be the same thing.

**The target follows `S`'s rule rather than adding a selection mode**: a
git repository syncs itself, a plain directory syncs every repository nested
under it, anything else does nothing. Two actions with one targeting rule is one
thing to learn; the shortcuts appear and disappear together for the same reason.

**"Nested under it" means at any depth**, and it did not until D59 was fixed:
the walk carried a literal `3`, so a repository at `monorepos/client/2026/api`
was invisible to `S`, `F` and `A` while the directory holding it browsed
normally — and nothing on screen said a limit had been applied. What bounds the
walk is that **it stops at every repository it finds**, so a repository's own
`node_modules` is never entered; that prune was always the one doing the work,
and the depth limit was covering for nothing.

**It also means behind a link.** `DirEntry.IsDir()` reports on the link rather
than on its target, so a junction to a directory full of repositories said
`false` and the whole subtree was invisible — while the same filter in
`table.go` listed the link as a *file*, neither browsable nor scannable, with
nothing saying why. `leadsToDir` answers for both, and answers by `os.Stat`
because a Windows junction is `ModeIrregular` in one Go release and
`ModeSymlink` in another. A plain file costs no syscall, which is what keeps it
affordable on a tree with no links in it.

**The cycle guard is `os.SameFile`, and that is not a preference.**
`filepath.EvalSymlinks` does **not** resolve a junction — it hands back the
link's own path — so a resolved-path guard sees two names for one directory and
never fires; measured here, `os.Readlink` answers for a junction and
`EvalSymlinks` does not. `mayFollow` asks two questions, and only a link pays
for either: is the target on the path we came by (a loop), and has it been
entered through another link already (a duplicate — the same repositories under
a second name, and a second scan of each). The climb goes to the filesystem
root rather than to the base, because a link *above* the base drags the base
back in with it.

**A repository is recognised from the directory's own listing** (`holdsRepo`),
not from `os.Stat`. `.git` catches a working tree and a worktree alike; a
*bare* repository has `HEAD`, `objects` and `refs` at the root and was missed
entirely, which is a scan target quietly dropped. Three names instead of one
means three failed stats per directory on the worst case, and that measured
334 ms against the old walk's 75 ms over the Go module cache — reading them out
of the `ReadDir` that was happening anyway costs nothing, and brings the whole
walk to **65 ms while finding strictly more**. The price is one `ReadDir` on a
repository's own root, which is where the walk stops.

**A gap is counted, and the count is what makes it visible.** `subRepoScan`
carries `Skipped` beside `Repos`: a `ReadDir` that fails is logged and counted
instead of swallowed, and the number reaches the screen two ways —
`warnSkipped` posts a `Warn` when a scan or a sync runs on a tree that was only
partly read, and `reasonUnread` replaces the refusal for a directory where
*nothing* was found. "No repository nested under it" is a claim the walk is not
entitled to make when it could not look everywhere: not knowing is not knowing
there are none (Rule 130). A batch sync carries it on `Model.syncUnreadable`
until its summary is written, rather than as a footer message of its own: "12
repositories synced" is a different claim from "12 synced, and I could not look
in 3 places". It is deliberately **not** an item of the run — a repository under
an unreadable directory was never a target, so there is nothing for the registry
to hold.
There is deliberately no sync-all: at the root the user syncs each top-level
directory, and a second key for it is not worth `Shift+S`'s collision with
Rule 111's sort menu.

**`git.Sync` refuses more than it does**, and each refusal is the point:

| Situation | What happens |
|---|---|
| behind, clean, no local commits | fast-forwarded |
| nothing to pull | up to date — unpushed commits do not change that, sync is the pull direction |
| local commits the remote lacks | **skipped**, `diverged — N commits ahead` |
| uncommitted changes, untracked included | **skipped**, `uncommitted changes` |
| detached HEAD, or no upstream | **skipped**, named |
| the remote could not be reached | **failed** — the difference from a skip is whether the repository is as its owner left it, or DevDesk could not find out |

No merge commit, no rebase, no stash, and **never a push**. A divergence is a
decision about someone's unpublished work, and a tool that guesses at it
destroys hours in a keystroke that cannot be undone.

**The fetch runs first and always, whatever the tree looks like.** That is D35:
the "unpulled" count comes from `@{u}`, the *local* tracking ref, and nothing in
DevDesk moved it — so it read `0` on a repository forty commits behind. A sync
deciding from that number would be reconciling against an answer it had not
checked. The consequence worth keeping: a repository sync **declines** still
comes out of it knowing how far behind it is, because the fetch happened either
way. `readGitStatus` re-reads the repository on every completion, refusal
included, and `applyGitStatus` puts the fresh counts on the row.

**A repository is never scanned and synced at once, and neither runs on one
being deleted.** A scan reads the working tree while a fast-forward rewrites
it; the visible result is a report describing a tree that no longer exists. A
delete takes the tree away from under either of them. `Model.busy` is the one
predicate all three consult — `A`'s purge included, or a syncing row's counts
are blanked with nothing on the way to replace them.

**It answers from the jobs registry now** (§3.58, `internal/jobs`), which is what
makes the guard true rather than merely local. It used to read three maps of
paths this view maintained, and those knew only what *this view* had launched: a
scan started on the same repository from `:sec` walked straight past it, and the
two wrote the same cache entry. `scanning`, `syncing` and `deleting`
(`workspaces/jobs.go`) replace the three maps one for one and read the snapshot
the router broadcasts.

**`D` is guarded twice, and the second time is not redundant.** `startDelete`
refuses before the confirmation opens, because asking a question and then
declining the answer wastes the user's time; `handleConfirmDelete` asks again
because a batch sync marks its repositories from a `Cmd`, so one can take the
path while the modal is on screen — and that handler is where the irreversible
call is issued. The marker is cleared on **every** outcome, failure included:
`deleteEntry` builds its message at one site so the path cannot be omitted from
the failure, which is what would strand the row as busy for the life of the
view. Before this, a second `D` fired a second `os.RemoveAll` and reported its
failure to the user for a deletion that had in fact succeeded (§3.23).

**The token goes to the configured GitLab host and nowhere else.**
`tokenForRemote` compares the repository's remote host with `gitlab.url`'s and
returns `""` otherwise. This is not tidiness: the workspaces view holds whatever
the user has cloned — GitHub, a customer's Gitea, a path on a share — and
`http.extraHeader` would put DevDesk's personal access token on the wire to any
of them. It is also why `internal/git` exists as a package separate from
`internal/forge/gitlab`: deciding a credential's destination in a package named
after one forge invites the default that must not exist. A foreign remote never even
reaches the loader, so the secret store is not read for it.

The keyring is read **once per batch** (`tokenLoader`, a `sync.Once` closure) and
on a command's goroutine, not in `Update` — same reasoning as the clone
pipeline's. `gitlab.pull.parallel_jobs` bounds both: one number meaning "how many
git network operations at once" beats two the user has to keep in step.



## What the Scanned column means, and what the footer says

`N/M` on a directory row is **settled coverage**: how many of the repositories
nested under it have a cached scan result. It is not progress.

It used to be both. During a batch the cell counted up, one repository at a
time, so `3/12` meant "nine still to go" or "nine nobody has ever scanned"
depending on something the cell did not say — in six cells, at the far right of
a row the user is not looking at. A directory with a scan running under it now
spins and stops counting, and the batch reports in the footer, where a batch
belongs.

The footer line has three forms (D9), and the third is a deliberate surrender:

```
Scanning — 3/12                      one kind, started here
Scanning — 3/12 · Syncing — 1/4      two kinds, both started here
3 jobs running — :jobs for details   mixed, or started elsewhere
```

One line cannot carry four batches launched from three views, and a line showing
only the part it recognised would be worse than one that admits there is more.

**`queued` and `running` are not the same thing here either.** `scanOneRepoCmd`
emitted its starting message *before* waiting on the worker pool, so every
repository in a batch reported itself running the instant the batch was
dispatched: twelve spinners for four workers. The message now waits for its turn
in the pool, which is what moves the item from queued to running.
