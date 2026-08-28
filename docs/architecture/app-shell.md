# The application shell — router, keyboard, shortcuts

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Bubble Tea Application Structure

This is a **multi-view TUI application** using the Elm Architecture (TEA) pattern via Bubble Tea. Understanding the routing and view lifecycle is critical:

**Main Components:**
- `main.go` - Entry point, initializes config and Bubble Tea program
- `internal/app/app.go` - **Router/orchestrator** that manages view switching and command mode
- `internal/app/messages.go` - Cross-view messages (GitLab, scan results, pull, etc.)
- `internal/shared/state.go` - SharedState for cross-view data (GitLab client, user, stats)
- `internal/ui/*/` - Individual views

**Key Architecture Pattern:**
```
App (Router)
├── Manages: currentView, commandMode, viewport, sharedState
├── Routes messages to active view
├── Handles view switching via command parser
├── Manages multi-context configuration
└── Views (lazy-loaded):
    ├── dashboard       - Overview (stats, tools, service status)
    ├── status          - System monitoring (CRUD monitors)
    ├── git-auth        - forge authentication form (GitLab or GitHub)
    ├── git-explorer    - forge namespace/repository browser + multi-select clone
    ├── workspaces      - Local workspace management + git metadata
    ├── security        - Trivy + Gitleaks scanner with multi-tab results
    ├── containers      - Docker container list + live metrics
    ├── oci-resources   - OCI resource list, scan, launch containers, network inspection
    ├── netdiag         - Network diagnostics (Docker-based tools) + real-time port monitor
    ├── configuration   - Every scalar setting in the current context
    └── viewer          - One document, read-only (router-only: no `:viewer`)
```

## View Switching & Command Mode

Press `ctrl+p` to enter command mode, then type:
- `dashboard` or `d` - Switch to dashboard view
- `status` or `s` - Switch to status view
- `git-auth` or `ga` - Switch to the forge authentication view
- `git-explorer` or `ge` - Switch to the forge explorer view
- `workspaces` or `w` - Switch to workspaces view
- `security` or `sec` - Switch to security scanner view
- `containers`, `cont` or `ct` - Switch to containers view
- `oci-resources` or `oci` - Switch to OCI resources view
- `netdiag` or `net` - Switch to network diagnostics view
- `configuration`, `config` or `cfg` - Switch to the configuration view
- `context <name>` or `ctx <name>` - Switch configuration context
- `context list` - Show available contexts
- `quit` - Exit application

Command parsing and tab-completion live in `internal/command/`. `ParseCommand()` returns a structured `Command{Type, View, Args}` supporting `CommandView`, `CommandContext`, `CommandQuit`, `CommandUnknown`.

**The forge views are `git-auth` and `git-explorer`, named after the role.**
A context targets one forge (§3.6), so there is one authentication screen and
one explorer, and both adapt to whichever it is — a `gitlab-` prefix would have
to be typed as `github-` half the time for the same view. Their packages live
under `internal/ui/forge/`, which is what the import line says too.

**Ten spellings still parse and none of them is suggested.** `gitlab-auth`,
`gla`, `github-auth`, `gha` and their explorer counterparts, plus the older
`explorer` / `exp`, resolve through `legacyNames`. Keeping them parseable is
deliberately permissive — there is one authentication view, so `gla` typed out
of habit should go there rather than fail. Keeping them *unsuggested* is what
makes the new names the ones a user learns, because the completion list is the
only place most people read a command.

The GitHub spellings were never accepted before and are there for the same
reason as the GitLab ones: someone whose context targets GitHub will guess `gha`
before `ga`, and being right is worth more than being consistent about what used
to exist.

**The filtering happens in completion, not in parsing.** That split is the whole
of it — it makes a rename feel like a rename rather than a removal — and it is
the client §3.6 step 0 deferred the mechanism for, rather than building it with
an empty exception list.

**There is no `:theme` command.** The theme is a setting, so the configuration
view owns it — the picker wrote `app.theme` behind the settings form's back,
which is one setting with two writers. Its overlay, `internal/app/theme.go` and
`CommandTheme` are all gone; `applyThemeNow` in `internal/app/configuration.go`
is what swaps the palette now.

**`ctrl+p` is the way in, and `:` is the convenience.** `ctrl+p` is handled
before any `InEditMode()` check, so no text field can claim it; a bare `:` opens
the command line too, but only when nothing is focused — inside a field it is an
ordinary character, which a value like `https://trivy-server:4954` needs.

It is not `ctrl+:` — `:` is 0x3A, outside the 0x40-0x5F range a terminal
encodes for Ctrl, so that combination never arrives. It was `alt+:` until
§3.26, which has the mirror defect: on Terminal.app and iTerm2, Option is not
Meta unless the user turns it on, so `Option+Shift+;` emits a literal character
and the key never arrives either. That was worse than a key that plainly does
not exist, because every view advertised it. The vocabulary and the reasoning
live in `internal/ui/keymap`.

**Important:** The `FormView` interface (`InEditMode()`) prevents command mode activation when forms are active. Views with active forms must implement this interface.

**`LeavingView` is the point the router did not have.** `switchView` told
nobody it was switching, so a view holding an edit in a widget rather than in
its model lost it without a word — §1.3 D62, in the configuration form.
`Leave() (tea.Model, tea.Cmd, bool)` returns the settled view, whatever it wants
to say, and whether it may be left; `false` cancels the switch and passes the
view's own `Cmd` on in place of it. `switchContext` calls it too, since it
rebuilds every view against a different file.

The interface is optional and probed for silently, like the other four — the
configuration view is the only one that implements it, and every other view is
left without ceremony. Re-entering the view already on screen settles nothing:
`:cfg` from `:cfg` is not a save.

## The keyboard — `internal/ui/keymap`

**Four namespaces, and the whole point is that a test can check them.** The
package declares the vocabulary; the tests parse every `.go` under
`internal/app` and `internal/ui`, find the switches that decide keys, and fail
naming file, line and rule. A convention nothing verifies is what produced the
16 collisions §3.26 relieved.

| | Meaning | Checked by |
|---|---|---|
| **UPPERCASE** | an action, global to the application | `TestNoViewBindsAnUndeclaredUppercaseKey` |
| lowercase | a filter or display toggle, local but **declared** per surface | `TestEveryLowercaseBindingIsDeclared` |
| `Ctrl` | only `ctrl+c`, `ctrl+r`, `ctrl+p` survive | `TestOnlyThreeCtrlCombinationsSurvive` |
| a modal's `y`/`n` | a *mode*, not a case — it takes every key before the view | declared in `modalKeys` |

`Shift` carries the actions because the other families are amputated: `Ctrl`
encodes only ASCII 0x40–0x5F and the tty confiscates four of them, `Alt` is not
Meta on macOS by default, and `Ctrl+Shift` is indistinguable from `Ctrl` without
a keyboard protocol bubbletea v1 does not enable. The reasoning lives in the
package doc so it does not have to be rediscovered.

**No bare letter is navigation.** `h j k l g G` are gone application-wide — from
`datatable`, from every view, from the shared modals, and from
`bubbles/viewport`'s own default `KeyMap` in the help overlay, which was
scrolling on letters behind the application's back.

**`g` came back in the viewer, and it is not an exception** (§3.53). What the
rule forbids is a letter standing *in place of* a structural key; there it opens
a prompt, and the jump takes an **argument** — which is exactly what `home` and
`end` cannot express. So it leaves `keymap_test.go`'s `retiredAliases` with its
reason written beside it: a letter that regains a meaning leaves the list of
those that have none, or the list lies. That is `H` returning to `free` in §3.47,
taken the other way round. `j` and `k` stay retired — they are `down` and `up`
under another name.

**A key is announced by the token that binds it** (§1.3 D63). `keymap_test.go`
checked that no view *binds* a key outside the vocabulary and never that a view
*announces* what it binds — and the security view showed `o Open ref` while its
handler read `case keymap.Web:`, and `esc/⌫ Back` while it answered `esc` alone.
Both keys were dead on arrival, and the ones that worked were never advertised.

Nothing mechanical relates the two halves: **the announced key is a display
string and the bound key is a bubbletea key name.** `↑↓` binds as `case "up"`,
`esc/⌫` as `case "esc"`. So the relation is *created* where it can be — an
action is announced as `keymap.Scan`, never as `"S"` — and three tests in
`announced_test.go` hold it:

| | |
|---|---|
| `TestNoAnnouncementSpellsAnActionOutInFull` | the half that makes the others possible: `"S"` is a string like any help label, `keymap.Scan` is the handler's own token |
| `TestEveryAnnouncedActionIsBoundInItsPackage` | a view showing `S Scan` answers `keymap.Scan` somewhere in the same package |
| `TestEveryAnnouncedToggleIsBoundInItsPackage` | the lowercase half, which has no constant but is still one letter — this is the one that catches the reported `o` |

An announcement is recognised by carrying **both** `Key` and `Description`,
rather than by its type name: `shortcut.Shortcut` and `help.KeyBinding` both
have the pair, an element inside a slice literal has no type to read, and
`shortcut.HeaderInfo` — the other thing with a `Key` — has `Value` instead. A
`Key` written as a package constant is resolved, which is how the hand fix for
`o` (`openPipelineKey`) becomes checkable rather than merely tidy.

Two limits, both deliberate. `↑↓`, `enter/esc`, `tab / shift+tab` are display
strings with no counterpart to compare against — a test that guessed would
report the 52 help labels the naive version does, and Rule 138 keeps most of
them off the screen anyway. And the granularity is the **package**, not the
view's state: a key bound in one tab and announced in another passes. What these
catch is the key bound *nowhere*, which is what both instances were.

**Free letters are declared too** (`J Q Z`). A new action takes one of them;
it does not invent a key, and `TestFreeLettersAreActuallyFree` stops the list
going stale.

Three actions were dissolved rather than given a letter, and each fixed a defect
on the way out:

| Gone | Where it went |
|---|---|
| `r` — restart a container | a button in `K`'s modal. Both used to act with **no confirmation**, and with caps lock on a scrolling `k` stopped the selected container |
| `ctrl+a` — purge then scan all | a checkbox in `A`'s modal. The pair differed by a modifier alone, with nothing in their shape saying which one destroyed data |
| `ctrl+e` — launch a container | `N`. It is "create a resource from the selected row", and nothing else is created from the Images tab |

`i` moved to `enter` for the same reason — inspect was already `enter` in
OCI/Networks — which is what freed `I` for the dashboard's issues.

**`t`/`T` and `s`/`S` became one key and a setting.** `app.terminal_new_window`
decides whether `T` suspends the TUI or opens a window. The capability belongs
to the environment rather than to the moment: there is no window to open under
WSL or through SSH, and a key that is inert on two setups out of three is worse
than a setting that is simply off there.

**`HeaderView` is all four methods or none.** The router probes for it with a
type assertion and falls back silently, so a view supplying `GetTitle` and
`GetShortcuts` but not `GetIcon` and `GetHeaderInfo` satisfies nothing and
renders an empty viewport title — with nothing to say so.
`command.AllViewNames()` and `TestEveryViewSuppliesItsHeaderAndHelp` turn that
into a contract every view is checked against.

There are **three** name lists, and each answers a different question:

| | Answers | Read by |
|---|---|---|
| `ViewNames()` | what can a user type | completion, `app.default_view` |
| `AllViewNames()` | what can appear in the viewport | the router's contract tests |
| `FullNames()` | every command, views and actions | completion |

`FullNames()` also carries the action commands (`context`, `quit`), which is
right for completion and wrong for anything meaning "a view" — the configuration
view's `default_view` field offered `quit` as a landing view until they were
separated. `AllViewNames()` adds the **router-only** views: `viewer` is opened on
another view's request and `:viewer` resolves to nothing, but it renders in the
same viewport as the rest and fails in the same silence, so the contract has to
reach it.

## A shortcut that does not apply is greyed, not dropped

`shortcut.Shortcut.Disabled` (§3.48). The entry keeps its place and the key
loses its colour and its weight, so the column no longer re-orders itself as the
cursor moves — which is what it is for: it is read out of the corner of the eye,
and a list that rearranges under the gaze cannot be.

**A mode still replaces the list.** A form has different keys from a table, so
greying them would show the union of every mode. What is greyed is a *state*
inside one mode: the selected row, or a tool the machine does not have. An
operation in flight is neither — it changes every tick, the row's spinner
already says so, and an entry that blinks says the opposite of the point.

The mechanism lives in one place: `Shortcuts.ToStrings()` picks
`theme.ShortcutKeyDisabledStyle` (an alias of `ColorDim`, assigned in
`ApplyTheme` like the footer colours). The description is left alone — it is
already dim, so a disabled line reads as one uniform grey. `maxLenKey()` counts
the disabled entries, or the alignment would depend on availability and the
column would move anyway.

**One calculation, two readers.** `shortcut.Availability` carries a single
`Reason string`, empty meaning available; `GetShortcuts` reads it to grey and
the handler reads it to refuse — so a greyed key that still acts is not
expressible, and the refusal cannot be silent. That silence is what it replaced:
`openInBrowser` returned `m, nil` on a repository with no remote, and `W` was
advertised anyway because the shortcut keyed on `IsGitRepo`. The reference
implementations are `internal/ui/workspaces/availability.go` and
`internal/ui/oci_resources/availability.go`.

**A footer reason is for an action, not for a control.** `←→` on a field that is
not a cycle, `space` on what is not a checkbox, `tab` with one tab — greying is
the whole of it: there is nothing to explain, and a footer line on every stray
arrow key in a form would be noise.

Two rules the entry earned:

- **Not knowing is not knowing that not.** `scan.CheckDependencies` shells out,
  so it runs in a `Cmd` (`DepsCheckedMsg`) and `S`/`A` stay lit until it lands.
  Greying for three frames and un-greying reads as a fault — D20 at the scale of
  a key.
- **A key that applies whatever the row is stays out of the set.** `N` creates a
  directory in the *browsed* directory, so hiding it on a repository said "does
  not apply" about an action that worked; greying would repeat that, and
  refusing would be a regression.

**Every view is migrated**, and what still replaces the list is the screen
changing: a mode, a tab, a state of the view, a signed-out screen — and, in the
viewer, the **document kind**. A Markdown file has no verbosity and never will,
which is a difference between openings rather than a "not now"; what varies
*within* one document — the tree against the text — is greyed like everything
else.

Three things it turned up on the way through:

- The Images tab advertised a **spinner frame as a key**: while a scan ran, `N`,
  `S` and `D` were replaced by an entry reading `󰑐  scanning...`, in the column
  that lists bindings. The row's own Scanned cell already carries that spinner
  (Rule 139). `TestNoShortcutAdvertisesAGlyphAsAKey` checks the Private Use
  Area rather than a list of icons, so nothing has to be kept in step with
  `theme/icons.go`.
- The **dashboard had no footer message** at all. It budgets an info line
  (Rule 124) and left it permanently empty, so `R` and `I` without a session
  fell through in silence with nowhere to say why.
- `R` **checked less than `I` did**: it opened a URL from a backend whose
  session was never verified. One `forgeLinks()` for both keys is what fixed it.

## Multi-Context Configuration

The app supports multiple configuration contexts (e.g., work, personal, client-A):
- Contexts are stored in `~/.devdesk/contexts/<name>/config.yaml`
- Current context is tracked in `~/.devdesk/current-context`
- Each context has isolated GitLab credentials via Git Credential Manager
- Context switching reinitializes all views with new config


## Shared State

**A session is set and cleared by the router, both ways.** `setAuthenticated`
and `clearAuthenticated` in `internal/app/gitlab.go` are mirrors, and every
forge-backed view reads `sharedState` rather than holding its own answer. A
view resetting only its own fields is what D28 was: logging out left the client
and the user in place, so the explorer kept browsing and the header kept naming
a signed-out user.

**`clearAuthenticated` is the only place a session is torn down.** Three sites
used to write the same three or four fields by hand — the secret-backend change,
the GitLab-URL change and the context switch — which is the shape D28 came in.
They call it now.

**`IsAuthenticated` is the flag, and `Forge` is what you call.** The client used
to be both: `GitLabClient != nil` decided "is the user logged in" at three sites
while `IsAuthenticated` sat beside them saying the same thing. `CurrentUser` is
a value rather than a pointer for the same reason — a second way to ask a
question is how two answers come to disagree. The header shows the user when
there **is** a username, not when the flag is set, so a session whose user could
not be read does not print a bare `@`.

Clearing `sharedState` does not empty a table a view already loaded, so a
session ending also drops the views — all but the one on screen that reported
it.


`internal/shared/state.go` holds cross-view data injected at view creation:
- `Secrets`, `SecretNotices` — the context's secret store and what the migration off plaintext reported
- `Forge`, `IsAuthenticated`, `CurrentUser` — the forge session (§3.6)
- `CachedGroups`, `CachedProjects` — GitLab data cache
- `GitLabStats`, `DockerStats`, `OCIStats` — Dashboard counters. `GitLabStats`
  is a `*forge.DashboardStats`, whose five counters are each a `*int`: `nil`
  means nobody could read it, and the dashboard prints `-` rather than the `0`
  that read as "you have none" (D52)
- `ServiceStatus`, `ServiceComponents` — Status monitoring results
- `WorkspaceCount`, `Tools []ToolInfo` — Tool availability (Trivy, Gitleaks, Docker)


## Cross-View Communication

Key messages in `internal/app/messages.go`:
- `SwitchViewMsg` — navigate to another view
- `SelectionRequestMsg` / `SelectionResultMsg` — selection mode (e.g., workspaces opened from security view to pick a repo)
- `ImageScanResultLoadedMsg` / `WorkspaceScanResultLoadedMsg` — cached results ready


## Bubble Tea Message Flow

Custom messages defined in view models (e.g., `internal/ui/status/model.go`):
```go
type TickMsg time.Time           // Countdown timer
type CheckStartedMsg struct{}    // Check initiated
type CheckCompleteMsg struct{}   // Check results ready
```

Commands return these messages to trigger async operations. The Bubble Tea `Update()` method handles them.

