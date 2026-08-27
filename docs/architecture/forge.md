# The forge abstraction, its vocabulary, and the explorer clone

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The forge abstraction — `internal/forge`

What DevDesk asks of a code-hosting platform, so a context can target GitLab or
GitHub — exactly one, never two (§3.6). `internal/forge/gitlab` is the only
package that knows go-gitlab exists, `internal/forge/github` the only one that
knows go-github, and `internal/forge/session` the only one that knows both
exist.

Five decisions hold it together, each forced by what the code consumes:

| | |
|---|---|
| **Identity is opaque, the path is not** | `ID` addresses an object with the backend and means nothing outside — GitLab needs a number for `ParentID`, GitHub addresses by owner. `Path` is what the clone URL, the display and GitLab's renamed-path deletion are built from. The type is a string so arithmetic on it cannot be written; only the backend reads it back |
| **A namespace and a repository are different types** | only a namespace has children, only a repository has a CI status and a scheduled deletion. "Drill into a repository" is unexpressible rather than forbidden by review |
| **Decoration is an option** (`BrowseOptions.Decorated`) | the role and CI status cost two requests **per repository** on GitLab. The explorer asks; the clone's walk does not, which is what keeps a two-hundred-repository group from costing 400 calls for a badge nobody reads |
| **`Shape` is declared, never sniffed** | nesting depth, the visibility set, whether a delete can be permanent. `CanNestUnder` reads the "unbounded" sentinel in one place — the obvious comparison refuses the *root* namespace on GitHub, where the maximum is 1 |
| **`DashboardStats` counters are `*int`** | five independent requests, any of which fails on its own. `nil` means nobody looked (D52) |

Two properties come with the seam: **the backend paginates** and no caller ever
sees a page (D34), and **every call takes a context**, which is what the clone's
cancellable discovery needs and `context.Background()` everywhere prevented.

`CurrentUser` is **memoised** on the backend. Every decorated listing needs the
caller's id for its role lookups, and asking the host each time would add a
request per listing that the pre-abstraction code did not make — it took the id
from a session the view was already holding. Who a token belongs to does not
change for the life of a session.

**A decoration that fails does not fail the listing; a listing that fails is
never an empty list.** Both directions have a test. It is one rule seen from two
sides: a column short beats an empty explorer, and an empty explorer must never
mean "this group contains nothing".

**`internal/forge/session` opens sessions, and it is the one place that knows
both backends exist.** `internal/forge` must not import an implementation and a
backend must not import its sibling, so something above both has to choose:
`session.Backend(forgeType, url, token)` is that switch, and an unknown type
falls through to GitLab — `applyDefaults` has already normalised the value, and
the one backend that has always existed beats an error nothing can act on.

The credential store is **not** forge-shaped: `Storage` is keyed on a URL and
knows nothing else, so loading and forgetting a token are the same code either
way. Only opening a session differs, and only in which constructor it calls —
which also means a context that switches platform without changing host finds
the token it already had, and that is right, because it is the same host asking.

**GitHub refuses what it cannot express, rather than doing something
adjacent.** There is no REST endpoint for creating or deleting an organisation
— both come back as errors naming the reason, where the tempting alternative
would be creating a repository under the user's own account and calling it an
organisation. A permanent delete is refused for the same reason: GitHub deletes
at once, so honouring the flag would report a distinction the platform does not
make, and `Shape.PermanentDelete` is false so the checkbox is never offered.

**An initial commit is one commit, and that is why it is four calls.**
`Repositories.CreateFile` in a loop makes one commit per file, so a three-file
template would arrive as three — and each would need the SHA the last returned,
which is a chain rather than a batch. The git data API takes it at once: a blob
per file, one tree, a commit with **no parent**, then the ref. No parent is what
makes it *initial*; on a repository that already has one the ref creation fails,
which is the honest outcome — this is not a way to overwrite history.

Three smaller decisions worth knowing:

- **The user's own account is the first root namespace, and leaving it out was
  a bug.** The reasoning that excluded it — "a personal namespace cannot be
  created or deleted, so half the actions would refuse the row" — is wrong on
  its own terms: *no* GitHub organisation can be created or deleted through the
  API either, so the personal account is not less capable but **more**. It is
  the one namespace where a repository can be created and deleted. The cost was
  the common case: a personal account belongs to no organisation, so the
  explorer opened empty on the account shape most people have. Its `ID` is the
  **empty string** — the interface's own word for "the user's own namespace",
  and what `Repositories.Create` takes for its `org` argument; the login would
  404, because GitHub refuses to treat a user as an organisation. Its children
  come from `/user/repos` with `Affiliation: owner`, without which the list also
  carries every repository the user collaborates on — which belongs under
  whoever owns it, and would appear twice in a tree that shows both.
- **`visibilityOf` reads `Visibility` before `Private`.** The first is what
  Enterprise fills with `internal`; reading only the boolean would report an
  Enterprise `internal` repository as `private`, which is a different thing.
- **A running workflow reports its *status*.** A run in flight has no
  conclusion, and falling through to empty would make a repository whose build
  is running read as having no CI at all.

The configured URL is the **web** host. On Enterprise the API lives under
`/api/v3/`, which `WithEnterpriseURLs` appends — conflating the two would send a
user to `https://git.acme.test/api/v3/acme/api` when they asked to open a
repository in a browser.

**One rough edge, left rough and written down.** The explorer's Type column
reads the vocabulary, so the personal account's row says "Organization". GitHub
calls the union "Owner", but taking that word into `Vocabulary.Namespace` would
make the configuration read "Default parent owner", which is worse — and a third
node kind would reintroduce the sum type the two-types decision exists to avoid.
One inaccurate cell beats either.

Cost: **+0.55 MB** on the binary (24.4 → 25.0).

Worth knowing: **go-gitlab retries 5xx** with an exponential backoff — a test
serving 500 took 35 seconds, measured. Not a regression (it is the SDK's
default), but an unreachable instance makes the user wait behind a spinner.

## The vocabulary — `forge.Vocabulary`

What a forge is **called**, as opposed to what it can do. The distinction is
§3.6's and it is the whole design: "Group" against "Organization" is a *word*,
while nesting depth and the visibility set are *shapes* — they change what the
application can promise, and no wording helps with them. `Shape` carries the
second, `Vocabulary` the first, and neither grows conditionals for the other.

**Per forge, not neutral.** A GitLab user says *group*, a GitHub user says
*repository*; "namespace" is a third language nobody speaks and makes the
application read as an abstraction layer rather than as a tool. The *field
names* are neutral because the code has to be — a view cannot switch on which
forge it is talking to — but nothing a user reads is.

**Resolved from the config, not from the session.** `forge.VocabularyFor(
cfg.Forge.Type)`, not a method on `Forge` and not a field on `shared.State`:
the explorer's "not authenticated" screen and the auth view's own title both
need the words *before* any session exists, so a value hanging off a live
backend would be missing exactly where it is needed most. Each view has a small
`vocab()` helper; the config is what every one of them already holds.

**No view writes anything specific to one forge into a string, and a test says
so.** `internal/ui/vocabtest` parses every `.go` under `internal/ui` and fails
on a string literal containing a forge **marker** — import paths excluded, and
one declared exception (`theme.ForgeIcon`'s own switch), on the model of
`keymap.DeclaredExceptions()`. A wording table nothing enforces drifts back one
message at a time, and the messages that drift are the ones nobody reads until a
GitHub context renders "GitLab not authenticated".

The markers are the two **names** *and* the token **prefixes** (`glpat-`,
`ghp_`, `github_pat_`, …), and the second half was added after the first half
missed something. The auth view carried `glpat-xxxxxxxxxxxxxxxxxxxx` as its
token placeholder for the whole of §3.6, and this test walked past it every
time: a token prefix is forge-specific **without naming a forge**, which is
exactly the shape a guard on names cannot see. `Vocabulary.TokenPlaceholder`
carries it now — shaped like a real value, because that is what a placeholder
is, while the prose about which prefixes exist lives in `TokenHelp`, where there
is room to name more than one.

**A command name is not vocabulary.** It is routing identity, the same for both
forges (§3.6 step 8 makes it `git-auth`), so a message quoting one builds it
from `command.ViewGitlabAuth` rather than writing it out. That is also what
makes the message survive the rename instead of quietly outliving it.

**The icon is the theme's, keyed on the same constant.** `theme.ForgeIcon` and
`forge.VocabularyFor` are two tables because a domain package must not import
the UI — but both switch on `config.ForgeGitLab` / `config.ForgeGitHub` rather
than on a literal, so there is one spelling of "gitlab" in the application and a
new forge cannot be half-added.

What is *not* in the vocabulary, and deliberately: the visibility set (a shape,
on `Shape`), the humanised role (the backend's, because GitLab's numbers and
GitHub's words do not align), and the token prefix as a *check* — it is a hint,
`TokenPlaceholder`, and DevDesk validates nothing, so saying "must" about it is
how a user comes to believe a working token is broken.

Sites it took over: the explorer's title, its loading line, its empty and
signed-out screens, its Type column and its help; the auth view's title, help,
URL label and both "not configured" messages; the dashboard's Code box, its
tree root and its help; the configuration view's forge tab — its title, its
icon, its URL example and two field labels; and `CreationForm`'s two resource
types.


## The explorer clone

`C` in the explorer opens a **selection mode** over the same tree, and `enter`
starts a **pipeline** that discovers and clones at once (§3.16). It replaced a
single `Cmd` covering a whole subtree behind a modal reading `"Pulling..."` —
several minutes indistinguishable from a freeze on a large group.

The line that holds it together: **the explorer creates what does not exist,
workspaces reconciles what does.** A repository already on disk is skipped
untouched, so the explorer never needs to know what a dirty working tree is and
the per-row states collapse to five — queued, cloning, cloned, already there,
failed. Updating an existing clone is §3.17's `sync`, in workspaces.

| Mode | Screen | Keys |
|---|---|---|
| `ModeSelecting` | the tree, with a checkbox on the Type cell | `space` ticks, `←→` drill, `enter` confirms, `esc` cancels |
| `ModeCloning` | a flat list, one row per repository | `esc` cancels, then closes |

**The selection is roots plus exclusions, never a list of repositories**
(`selection.go`). A positive list cannot be built when a group is ticked without
enumerating its children — the full API walk, run at selection time, which is
the freeze moved one screen earlier. "This group, minus these" needs to know
nothing about what the group contains, so a group nobody has expanded can still
be ticked, displayed with the right tri-state, and walked. It is the only
representation compatible with discovering as you clone.

The **nodes** are kept separately, in `Model.selectionNodes`: the walk has to
start from one, and a root ticked three levels down is no longer on screen once
the user has come back up. The selection itself stays paths-only, which is what
lets it answer for paths nobody has fetched.

**The check state travels on a row type.** `datatable` columns are built once in
`New` and close over nothing, so the table moved from `Model[*TreeNode]` to
`Model[explorerRow]` (`row.go`) — the `imageRow` pattern. The state must **not**
move into `datatable`: `SetItems` replaces the items on every drill-down, while
the selection spans levels the table has never shown. `RenderCheckboxTri` styles
its output and so cannot go in a cell; `checkboxIcon` is the glyph without it
(Rule 122).

The checkbox **rides on the Type cell** rather than taking a column of its own.
A column would cost four cells on every screen to say nothing on all but one of
them, and at 80 columns the explorer has none to spare.

**Two cancellation scopes, and the distinction is the point** (`pipeline.go`).
`cloneRun.cancel` is a `context.CancelFunc` covering **discovery only** — HTTP
reads, which cancel safely. A `git clone` is never interrupted: a context that
kills one leaves half a repository on disk, which is exactly what this avoids.
So `esc` cancels the walk and the scheduler stops issuing work, the running
clones are awaited, and the footer says `Cancelling — 3 clones finishing`. A
second `esc` must not force.

`cloneWalkFailed` is a kind of its own: a group that cannot be listed gets a
failed row naming the **group**, because the repositories under it were never
discovered and no other row can stand for them.

The **failures are named when the run ends**, in the footer and the log. The
list is discarded on `esc` and workspaces records only the successes — a clone
that failed wrote nothing, so that report is the only one there will be.

`gitlab.pull.include_archived` is read by `listGroupChildren` and **only by the
clone**: browsing lists everything the forge has.

**A clone may never prompt, and `gitlab.Clone` is where that is enforced.**
Sending git's streams to the null device does not prevent a credential prompt —
it prevents git asking *itself*, after which the **credential helper** takes
over, and a helper is a separate process. Git Credential Manager writes
`info: please complete authentication in your browser` to the console directly,
over the top of the rendered frame, then waits. Bubble Tea cannot recover a
frame something else has written into: two frames end up visible at once, and
the row spins with nothing on screen saying why. Observed, not theorised.

So `cloneEnv` shuts every interactive path — `GIT_TERMINAL_PROMPT=0`,
`GCM_INTERACTIVE=never`, both askpass hooks, ssh in `BatchMode` — and the token
DevDesk already holds is passed as `http.extraHeader` **through the
environment**: argv is readable from the process list, and the `user:token@host`
URL form is written into every cloned repository's `.git/config` and stays
there. A stalled transfer is bounded by `http.lowSpeedLimit`/`lowSpeedTime`
rather than by killing the process, so git cleans up after itself and decision
12 still holds.

The consequence to keep in mind: with the helper out of the loop, a context
whose stored token is missing or under-scoped **fails** rather than falling back
to a browser. That is the intended trade — a failed row naming git's reason
beats a spinner that never resolves — but it makes the token the only way in.

