# Forge (GitLab / GitHub)

DevDesk talks to exactly one code-hosting platform per configuration context — GitLab or GitHub, never both at once. This page explains the abstraction that makes the rest of the application backend-agnostic, and the reasoning behind its less obvious design choices.

## The forge abstraction — `internal/forge`

`internal/forge/gitlab` is the only package that knows the GitLab SDK exists; `internal/forge/github` is the only one that knows the GitHub SDK exists; `internal/forge/session` is the only package that knows both backends exist at all. Everything else in the application talks to the `forge.Forge` interface.

Five design decisions hold the abstraction together, each forced by what the rest of the code actually needs:

| Decision | Why |
|---|---|
| **Identity is opaque, the path is not** | `ID` addresses an object within its own backend and means nothing outside it — GitLab needs a numeric parent ID, GitHub addresses by owner name. `Path` is what clone URLs, display strings, and deletion-by-renamed-path are built from. `ID` is typed as an opaque string precisely so arithmetic can't accidentally be performed on it. |
| **A namespace and a repository are different types** | Only a namespace has children; only a repository has a CI status and a scheduled-deletion state. "Drill into a repository" simply isn't expressible in the type system, rather than merely discouraged. |
| **Decoration is an explicit option** (`BrowseOptions.Decorated`) | Fetching role and CI status costs two extra API requests *per repository* on GitLab. The explorer view asks for decoration; the clone's discovery walk doesn't — otherwise cloning a two-hundred-repository group would cost 400 extra calls for a badge nobody is looking at during a bulk clone. |
| **`Shape` is declared, never sniffed** | Nesting depth, the set of valid visibilities, whether a delete can be permanent — all declared per backend rather than inferred at runtime. This is also what lets a shape check correctly refuse "nest under the root namespace" on GitHub, where the maximum nesting depth is effectively 1, without special-casing it. |
| **Dashboard counters are `*int`, not `int`** | Five independent stat requests, each of which can fail on its own. A `nil` means "nobody could read this one," which is a different fact than "zero." |

Two structural properties come along with the abstraction: the backend paginates internally and no caller ever sees a raw page, and every call takes a `context.Context` — which is what makes cancellable discovery (the clone pipeline) possible at all.

`CurrentUser` is cached on the backend for the life of a session, since every decorated listing needs the caller's own ID for its role lookups; refetching it per listing would add a request the pre-abstraction code never made, and who a token belongs to doesn't change mid-session.

!!! note "A decoration failure and a listing failure are handled differently, on purpose"
    If decoration fails, the listing still returns — just with a column missing, rather than the whole browse failing over a badge. But if the *listing itself* fails, it must never come back as an empty list — an empty list reads as "this group contains nothing," which is a much stronger and potentially wrong claim than "we couldn't ask."

### Session and credentials

`internal/forge/session` is the one place that knows both backends exist — `internal/forge` itself must not import a specific implementation, and neither backend may import its sibling, so something above both has to make the choice. `session.Backend(forgeType, url, token)` does that: an unrecognized type falls back to GitLab, since that's the backend that has always existed and an error at this point isn't something the caller could act on anyway.

The credential store itself is not forge-shaped at all — it's keyed purely on URL, so loading and forgetting a token work identically regardless of backend. Only *opening* a session differs, in which constructor gets called. A useful side effect: switching a context's target platform without changing the host finds the token it already had, correctly, because it's the same host being asked.

### Where GitHub and GitLab genuinely diverge

GitHub refuses to do things it can't actually express, rather than approximating them. There's no REST endpoint for creating or deleting a GitHub *organization* — both come back as explicit errors, rather than DevDesk quietly creating a personal repository and calling it an organization. Permanent delete is refused for the same reason: GitHub deletes immediately and irrevocably, so there's no "permanent" flag to honor, and `Shape.PermanentDelete` is simply `false` for that backend.

Creating an initial commit from a template is one commit on purpose, which is why it takes four API calls rather than one: looping `CreateFile` once per template file would produce one commit per file, each depending on the SHA the previous call returned — a chain, not a batch. Using the git data API directly (a blob per file, one tree, one commit with **no parent**, then the ref) produces a proper single initial commit. The lack of a parent is what makes it initial — creating the ref fails outright against a repository that already has a commit, which is the correct outcome: this path is not meant to overwrite existing history.

A few smaller decisions worth knowing about:

- **The user's own personal account is treated as the first root namespace.** It was originally excluded on the theory that "you can't create or delete a personal account, so half the row's actions would be dead" — but that reasoning cuts the wrong way: *no* GitHub organization can be created or deleted through the API either, so the personal account isn't less capable, it's the *only* namespace where a repository actually can be created and deleted. Its `ID` is the empty string, which the interface treats as "the caller's own namespace" and which `Repositories.Create` accepts directly as its `org` argument.
- **`visibilityOf` checks the `Visibility` field before falling back to the `Private` boolean**, because GitHub Enterprise fills `Visibility` with `internal` for some repositories — reading only the boolean would misreport those as merely `private`.
- **A workflow run in progress reports its own in-flight status** rather than falling through to "no CI," which would make an actively-building repository look like it has no CI configured at all.

The configured host is always the **web** host — on GitHub Enterprise, the API lives under a different path (`/api/v3/`), appended internally, so a user asking to open a repository in a browser is never sent to an API URL by mistake.

!!! tip "Cost of the abstraction"
    Adding the forge abstraction added roughly 0.5 MB to the compiled binary. Worth noting separately: the GitLab SDK retries 5xx responses with exponential backoff by default, which means an unreachable GitLab instance can leave the user waiting behind a spinner for tens of seconds before the request actually fails.

## Vocabulary — `forge.Vocabulary`

There's a clean separation between what a forge is *called* and what it can *do*. "Group" versus "Organization" is just a word; nesting depth and the set of valid visibilities are *shapes* that actually constrain what the application can promise. `Shape` carries the second kind of fact, `Vocabulary` the first, and neither is allowed to grow special cases for the other's job.

Vocabulary is resolved per-forge, never rendered as some neutral third language — a GitLab user reads "group," a GitHub user reads "repository," and a made-up neutral term like "namespace" would read as an abstraction leaking through rather than as the tool speaking the user's own vocabulary. Internal field names stay neutral (code can't switch behavior on which forge it's talking to), but anything a user actually reads is forge-specific.

Vocabulary is resolved from the saved configuration (`forge.VocabularyFor(cfg.Forge.Type)`) rather than from a live session, because some screens — the "not authenticated" empty state, the auth view's own title — need the right words *before* any session exists at all.

No view is allowed to hard-code forge-specific wording as a raw string; this is checked mechanically by scanning source for text containing forge-identifying markers. That check widened after a real miss: the auth view's example token placeholder (`glpat-xxxxxxxxxxxxxxxxxxxx`) was a GitLab-specific string that named no forge by name, so a check looking only for the words "GitLab"/"GitHub" walked right past it. Token placeholder text is now itself part of `Vocabulary`.

Deliberately *not* part of the vocabulary: the visibility set (that's a `Shape` concern), the human-readable role name (GitLab uses numbers, GitHub uses words — they don't line up), and the token prefix as a validation *check* — it's shown only as a hint, since DevDesk never validates tokens itself, and treating the prefix as a hard requirement risks telling a user their working token is broken.

## CI status vocabulary

`Repository.CIStatus` is a closed, GitLab-shaped vocabulary — `created`, `waiting_for_resource`, `preparing`, `pending`, `running`, `success`, `failed`, `canceled`, `skipped`, `manual`, `scheduled` — declared centrally in `internal/forge/cistatus.go`. This is a historical choice rather than a principled one: GitLab was the only backend when the field was introduced, so GitLab's own words became the vocabulary directly. It's still the right choice going forward, since GitLab's eleven values distinguish more states (e.g. "queued" from "not started yet") than GitHub's nine, so mapping GitHub onto it loses less information than the reverse would.

GitHub's outcomes are folded onto this vocabulary, each on its own reasoning: `timed_out`/`startup_failure` become `failed` (GitLab itself reports a killed timeout as failed); `action_required` becomes `manual` (a run waiting on approval is what GitLab calls manual); `neutral`/`stale` become `skipped` (GitHub visually renders both the same way skipped is rendered, and skipped is the only "grey" outcome in this vocabulary); and GitHub's four pre-run states (`queued`, `requested`, `waiting`, `pending`) all collapse to `pending`, since the distinction matters to a scheduler but not to someone reading a status column. An unrecognized value from either backend becomes an empty string rather than being passed through verbatim — an empty cell communicates "unknown" honestly, where six characters of a platform's internal spelling do not.

## The explorer table

The namespace/repository browser renders eight columns: an icon, Name, Slug, Visibility, Role, CI, Created, Activity. The leading icon column carries the node kind (namespace vs. repository) as a glyph rather than as a text label — it replaced a much wider text column that literally printed "Group" or "Organization," freeing that width for the two columns that actually identify a row.

Visibility is similarly a glyph (globe for public, shield for internal, lock for private) with an empty cell for any unrecognized value — a forge that introduces a fourth visibility must never be shown as one of the three existing icons. The explorer intentionally uses a different icon set from the local workspace browser: one browses what the forge holds, the other browses what's actually on disk, and a row that looked identical in both would wrongly imply they're the same object.

The table opens unsorted, matching the natural order the discovery walk already produces (namespaces, then repositories) — which also means the forge's own listing order is reachable again by cycling the sort column all the way around.

## Creating and deleting — the row shows progress in place

Creating or deleting an item is a real network round-trip, and the tree used to go blank for its entire duration — a full re-fetch on completion, with nothing on screen tying the empty state to the request that caused it. Instead, the moment a create request is sent, a placeholder row is inserted immediately at a *predicted* path (built from the parent path and the new slug), with a spinner riding in the icon column where the kind glyph will eventually go. When the forge responds, the placeholder is swapped in place for the real node — no full refresh, since the forge has just reported exactly what it made. A failure removes the placeholder and reports why in the footer.

The placeholder row is inert while pending: it can't be acted on again (no identifier exists yet to act on), and its own presence conveys that pressing another action key on it doesn't apply. Both create and delete are modeled as single-item entries in the shared jobs registry described in [App Shell & Router](app-shell.md), rather than as local view state — deletes in particular keep their row visible and spinning rather than disappearing early, since removing the row before the forge has actually confirmed the deletion would claim something that hasn't happened yet.

## The explorer clone

Selecting repositories to clone opens a dedicated selection mode over the same tree; confirming starts a pipeline that discovers matching repositories and clones them concurrently. This replaced an earlier single blocking command covering an entire subtree behind a generic "Pulling..." modal — indistinguishable from a hang for several minutes on a large group.

The governing principle: **the explorer creates what doesn't exist locally; the workspaces view reconciles what already does.** A repository already cloned is skipped untouched during the clone pipeline, so the explorer never has to reason about dirty working trees — that's the workspaces view's job (its own sync feature). This keeps the clone pipeline's per-row states to five: queued, cloning, cloned, already-present, failed.

The selection itself is stored as roots plus exclusions — never as a flattened list of repository paths. A flattened list can't be built when a whole group is ticked without fully enumerating its children up front, which is exactly the slow API walk this design avoids. "This group, minus these specific exclusions" needs no knowledge of what the group actually contains, so a group nobody has even expanded yet can still be selected, displayed with the correct tri-state checkbox, and walked lazily by the clone pipeline itself.

Two cancellation scopes are exposed, deliberately distinct: cancelling *discovery* is safe (it's just HTTP reads being abandoned), so the walk stops immediately. A `git clone` already in progress is never forcibly interrupted, because killing it mid-transfer would leave a half-written repository on disk — cancellation instead stops the scheduler from starting new work and waits for in-flight clones to finish, with the footer reporting "Cancelling — N clones finishing."

Cloning is engineered to never prompt for credentials interactively. Every interactive git path is disabled (`GIT_TERMINAL_PROMPT=0`, disabling the credential-manager's own interactive mode, both askpass hooks, SSH batch mode), and the token DevDesk already holds is passed through the environment as an HTTP extra header rather than on the command line, where it would be readable from the process list. This matters in practice: with interactive credential prompts fully disabled, a context whose stored token is missing or insufficiently scoped **fails outright** rather than silently falling back to opening a browser for interactive login — a failed row that names git's own error is considered better than a spinner that can never resolve on its own, but it does mean the stored token is the only way in.
