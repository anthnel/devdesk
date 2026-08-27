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

- **No colour**, against `eza`. A colour on every row informs no one, and it
  would weaken the ones that signal something — severities, secrets, git status.
- **No title, no `Less`, no `Search`.** It adds no text anyone could type, so
  the filter stays on Name and Remote.
- **Twenty glyphs were checked by eye**, one at a time, in a terminal: a wrong
  codepoint breaks nothing and renders a tofu box, which no test can see. CSS
  and HTML went in the wrong way round on the first pass — MDI's four
  `language_*` glyphs are one contiguous alphabetical run, and that ordering is
  what settles which is which.
- **What has no certain glyph does not get an invented one.** `.kt`, `.scala`,
  `.hs`, `.zig` and `.tf` fall to `IconCodeFile`.

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
the truth again when it lands. The progress and the summary are one footer line
rendered from the run (`syncStatusLine`) rather than assigned to `footerInfo` —
a batch outlives the three seconds a footer message gets (Rule 128).

**The target follows `S`'s rule rather than adding a selection mode**: a
git repository syncs itself, a plain directory syncs every repository nested
under it, anything else does nothing. Two actions with one targeting rule is one
thing to learn; the shortcuts appear and disappear together for the same reason.

**"Nested under it" means at any depth**, and it did not until D59 was fixed:
`detectSubRepoPaths` carried a literal `3`, so a repository at
`monorepos/client/2026/api` was invisible to `S`, `F` and `A` while the
directory holding it browsed normally — and nothing on screen said a limit had
been applied. What bounds the walk is that **it stops at every repository it
finds**, so a repository's own `node_modules` is never entered; that prune was
always the one doing the work, and the depth limit was covering for nothing.
Measured before removing it: unbounded is as fast or faster over `~/projects`,
and 283 ms against 37 ms over the Go module cache — tens of thousands of
directories with no repository anywhere to prune it, which is the worst case and
runs in a `Cmd`.

The other half of D59 is still open: the walk filters on `DirEntry.IsDir()`,
which reports on a symlink rather than on its target, so a repository behind one
is not found — and the same filter in `table.go` lists the link as a *file*.
Whoever fixes that needs a cycle guard, which the depth limit used to provide by
accident.
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
are blanked with nothing on the way to replace them — and `deletingPaths` is
the third map beside `scanningPaths` and `syncingPaths`, kept separate for the
same reason: the view says *which* operation holds the row.

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

