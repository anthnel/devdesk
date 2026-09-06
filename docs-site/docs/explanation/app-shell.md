# App Shell & Router

DevDesk is a multi-view terminal application built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), which implements the Elm Architecture (model, update, view) for terminals. This page explains how the application routes between its views, how its keyboard vocabulary is kept consistent, and how it tracks long-running work across screens that the user may not be looking at.

## Structure

`main.go` initializes configuration and starts the Bubble Tea program. `internal/app/app.go` is the router: it owns the current view, command-mode state, the viewport, and `shared.State` (data shared across views — the forge session, dashboard counters, cached listings). Individual views live under `internal/ui/*/` and are lazily constructed the first time they're navigated to.

```
App (Router)
├── Manages: currentView, commandMode, viewport, sharedState
├── Routes messages to the active view
├── Handles view switching via the command parser
├── Manages multi-context configuration
└── Views (lazy-loaded):
    ├── dashboard       - overview (stats, tools, service status)
    ├── status          - system monitoring (CRUD monitors)
    ├── git-auth        - forge authentication (GitLab or GitHub)
    ├── git-explorer    - forge namespace/repository browser + multi-select clone
    ├── workspaces      - local workspace management + git metadata
    ├── security        - Trivy + Gitleaks scanner with multi-tab results
    ├── containers      - Docker container list + live metrics
    ├── oci-resources   - OCI resource list, scan, launch, network inspection
    ├── netdiag         - network diagnostics + real-time port monitor
    ├── configuration   - every scalar setting in the current context
    ├── jobs            - long-running work: runs, then their targets
    ├── about           - build info and file locations
    └── viewer          - one document, read-only (opened by other views only)
```

## View switching and command mode

`ctrl+p` opens a command line from anywhere, including from inside a text field — it is intercepted before any field can claim it. A bare `:` does the same, but only when nothing is focused, since a value like `https://trivy-server:4954` needs the colon as an ordinary character. Command parsing lives in `internal/command/`; `ParseCommand()` returns a structured `Command{Type, View, Args}`.

The forge views are named `git-auth` and `git-explorer` rather than `gitlab-*` / `github-*`. A single context targets exactly one forge (GitLab or GitHub, never both at once), so there is one authentication screen and one explorer, and both adapt their wording to whichever backend is configured. Naming them after a specific backend would have meant typing `github-auth` half the time for what is, mechanically, the same view.

Forms block command mode: any view with an active form implements a small `FormView` interface (`InEditMode()`), and the router checks it before honoring `ctrl+p`.

### `Leave()` — telling a view it's about to lose focus

Early on, `switchView` simply swapped views without telling the outgoing one — a view holding an in-progress edit in a widget (rather than in its own model) could lose it silently. The fix is an optional interface:

```go
Leave() (tea.Model, tea.Cmd, bool)
```

A view that implements it returns its settled state, whatever command it still wants to run, and whether the switch may proceed. Returning `false` cancels the switch. `switchContext` calls it too, since switching contexts rebuilds every view against a different config file. The interface is probed for silently — most views don't implement it and are left alone.

## The keyboard vocabulary — `internal/ui/keymap`

The keyboard follows a small set of namespaces, enforced by tests that scan the source for keybinding switches rather than by convention alone (an unenforced convention is exactly what produced a wave of key collisions before this package existed):

| | Meaning |
|---|---|
| **UPPERCASE** | an action, global across the application |
| lowercase | a filter or display toggle, local to one view but explicitly declared |
| `Ctrl` | only `ctrl+c`, `ctrl+r`, and `ctrl+p` are used |
| a modal's `y`/`n` | belongs to a distinct input mode — it consumes every key before the underlying view sees it |

Actions live on `Shift` (i.e. uppercase letters) because the alternatives are all compromised: `Ctrl` only encodes a subset of ASCII and terminals reserve several combinations for flow control and control characters; `Alt` is not `Meta` on macOS by default; and `Ctrl+Shift` is indistinguishable from plain `Ctrl` without a keyboard protocol that this Bubble Tea version doesn't enable.

No bare letter is used for navigation — the familiar `h j k l g G` vim aliases are absent everywhere, including from the shared table component and from Bubble Tea's own default viewport keymap (which was previously scrolling on letters behind the application's back). The one exception is `g` in the document viewer, which isn't really navigation — it opens a prompt to jump to an arbitrary line number, something `home`/`end` can't express.

Every announced shortcut and every key a handler actually binds are checked against each other, so a shortcut that shows `esc/⌫ Back` in the header but is silently ignored by the handler — or a working key nobody advertised — gets caught mechanically rather than by review. A handful of letters (`J`, `Q`, `Z`) are kept deliberately free for future actions rather than assigned speculatively.

`T`/`t` and `S`/`s` collapse a single key plus a setting (`app.terminal_new_window`) rather than two separate bindings — whether a shortcut opens a new terminal window is an environment capability (there's no window to open under WSL or over SSH), not a per-press choice, so it's modeled as configuration instead of a dead key on two setups out of three.

### `HeaderView` — title, icon, shortcuts, header info

A view that wants a title, icon, shortcut list and header info bar must implement all four methods of this interface or none — implementing three out of four silently renders an empty title with nothing to say why. There are three separate name lists for views, each answering a different question: what a user can type (`ViewNames()`), what can appear in the viewport including router-only views like `viewer` (`AllViewNames()`), and every completable command including actions like `context`/`quit` (`FullNames()`).

## Shortcuts that don't apply are greyed, not hidden

When an action doesn't apply to the current state, its entry stays in the shortcut list but loses its color and weight rather than disappearing. The reasoning: a shortcut list is read out of the corner of the eye while scanning rows, and a list that reorders itself as the cursor moves defeats that.

This only applies to *state within a mode* — the selected row, or a tool the machine doesn't have. A different *mode* (a form instead of a table, a confirmation instead of a list) legitimately replaces the whole shortcut list, since it's a different vocabulary entirely. An operation actually in flight is neither: it changes every tick and the row's own spinner already communicates that, so greying and un-greying a key several times a second would just look broken.

The mechanism is centralized: `shortcut.Availability` carries a single `Reason string` (empty means available), read by both the header (to grey the key) and the handler (to refuse the action with that same reason in the footer). This guarantees the two can't disagree — a greyed key that still works, or a live key that silently does nothing, are both ruled out by construction. Reference implementations: `internal/ui/workspaces/availability.go` and `internal/ui/oci_resources/availability.go`.

A key that always applies regardless of the selected row (creating a new directory in the browsed folder, say) stays fully enabled rather than greyed for "not applicable to this row" — it isn't about the row at all.

## Multi-context configuration

DevDesk supports multiple configuration contexts (work, personal, a specific client, …):

- contexts live under `~/.devdesk/contexts/<name>/config.yaml`
- the active context is tracked in `~/.devdesk/current-context`
- each context has isolated forge credentials
- switching contexts reinitializes every view against the new config

## Shared state and sessions

`internal/shared/state.go` holds data injected into views at creation time: the secret store and any migration notices, the forge session (`Forge`, `IsAuthenticated`, `CurrentUser`), cached GitLab/GitHub listings, dashboard counters, service-monitoring results, and tool availability.

Sessions are set and torn down in exactly one place — `clearAuthenticated` in `internal/app/gitlab.go` — and every forge-backed view reads from shared state rather than keeping its own copy of "am I logged in". Earlier, logging out cleared some fields but not others by hand at three separate call sites, which meant the explorer kept browsing and the header kept showing a signed-out user's name after logout. `IsAuthenticated` is a plain boolean rather than being inferred from "is the client pointer non-nil", and `CurrentUser` is a value rather than a pointer, for the same underlying reason: two different ways of answering the same question are how the answers come to disagree.

## Cross-view communication

Key message types (`internal/app/messages.go`) include `SwitchViewMsg` for navigation, `SelectionRequestMsg`/`SelectionResultMsg` for cross-view selection (e.g. picking a repository from the workspaces view while inside the security view), and `*ResultLoadedMsg` variants that carry cached results back into a view.

### Long-running work reports without taking the screen

A scan, sync, or delete usually outlives the moment the user spends on the view that started it. Progress messages for such work are addressed **by name** to the owning view (`routeToView(target, msg)`), never to whatever view happens to be on screen, and routing one never changes which view is current. Three properties matter here:

- the *owning* view updates its own state (row markers, footer, cache) — if that update were dropped because the user navigated away, the row would stay marked "busy" forever
- the *active* view is untouched by a message that isn't its own
- `currentView` itself is never changed by a background message — an earlier version did this on every finished item, which meant a batch of twelve repository scans made every other view unusable until the last one landed

### The jobs registry — `internal/jobs`

A single, router-owned bookkeeping structure for everything long-running (`App.jobs`). It replaced four separate, mutually invisible tracking mechanisms scattered across views — which meant a scan started from the security view was invisible to the "is anything running" check in the workspaces view, and the two ended up racing to write the same cache entry.

The model has two levels: a **`Run`** is one batch started in one go from one view, and it holds an **`Item`** per target. A run's overall state is always *derived* from its items rather than stored separately, so the two can't drift out of sync.

| Method | Role |
|---|---|
| `Start(run)` | admits a batch — called only from `Update`, never from a `Cmd` |
| `Advance(id, target, state, detail)` | moves one item forward; returns false for unregistered work rather than swallowing it |
| `Attach` / `AttachRun` | stores a `context.CancelFunc`, delivered in the same message that reports the item started |
| `Cancel(id)` | stops the queue always; cancels work already in flight only when doing so is safe for that kind of work |
| `Snapshot()` | a read-only copy, with cancel functions stripped out |
| `Running()` | runs that haven't settled yet |

Single-item work (creating or deleting a single repository, say) is still registered as a run rather than tracked with a local flag, because the registry gives it three things a local flag can't: a shared spinner (rather than a per-view chain that stops the moment its own view settles), visibility to any "is something running" guard regardless of which view started it, and a context stamp fixed at launch time — so a create that outlives a context switch can't accidentally write into the newly-switched-to context.

**Open runs.** Most launches know their full target list up front, which is what lets the UI say "8 waiting". Cloning doesn't — discovering which repositories exist *is* the slow part — so `jobs.NewOpenRun` registers a run with no targets yet, and `Discover`/`Seal`/`CancelOpen` add targets as the walk finds them and eventually close it off. An open run is never considered "finished" just because its current items are — only an explicit seal closes it.

**Stopping work has two distinct scopes**, and both are exposed as separate keys where applicable: cancelling a *run* stops the queue (nothing further starts; anything still queued is marked skipped), while cancelling a *target* interrupts one piece of work already running. Not every kind of work can be safely interrupted mid-flight — a delete request already sent to a forge can't be un-sent — so the registry tracks per-kind whether cancellation-in-flight is even offered.

### One spinner, broadcast everywhere

Views never hold a pointer into the registry. The router hands out a read-only snapshot via `jobs.ChangedMsg`, broadcast to every held view (on screen or not) whenever anything changes — so navigating back to a view that missed some progress shows the current state without it having to ask. There is exactly one spinner tick chain for the whole application, owned by the router, identified by a sequence number so a stale tick from a superseded chain is simply dropped.

### The `:jobs` view

The reader's side of the registry — it starts nothing, holds no timer, fetches nothing; its rows are just the latest snapshot. Two levels: the list of runs, and drilling into one (via the standard drill-down key) to see its individual targets.

## Bubble Tea message flow

Views define their own message types following a `[ComponentName][Action]Msg` convention, e.g.:

```go
type TickMsg time.Time           // countdown timer
type CheckStartedMsg struct{}    // check initiated
type CheckCompleteMsg struct{}   // check results ready
```

Commands return these messages to signal that an asynchronous operation has progressed or completed; `Update()` is the only place that acts on them and mutates model state — a discipline the whole application follows to avoid data races between concurrently running Bubble Tea commands.
