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
    ├── jobs            - Long-running work: runs, then their targets
    ├── templates       - Catalog of repository templates: git, local or OCI sources
    ├── files           - Whole-filesystem browser, and the path picker forms borrow
    ├── about           - Which build is running, and where it keeps its files
    └── viewer          - One document, read-only (router-only: no `:viewer`)
```

**Render and View (Bubble Tea v2, DEVDESK-201).** A top-level view keeps
`Render() string` and wraps it: `View() tea.View { return tea.NewView(m.Render()) }`.
`app.Screen` is `tea.Model` plus `Render() string`; the router holds
`map[ViewType]Screen` and installs every view through the single `setView`,
which panics naming the view when a model has no `Render()`. Only the router
sets terminal state: `App.View()` wraps `App.Render()` and sets `AltScreen`,
`ReportFocus`, `BackgroundColor`, `ProgressBar` and `WindowTitle` (`main.go` passes no `tea.WithAltScreen()`). The setup wizard is its own
program and sets `AltScreen` in its own `View()`. Components (datatable, forms,
`FilterBar`) keep `View() string`. A bracketed paste arrives as `tea.PasteMsg`
and goes through `handlePasteMsg`: it is text for the focused field (palette,
or the view when it is editing), never a key action, and is dropped behind an
overlay or a context switch. `handlePasteMsg` first turns `\r\n` into `\n`
(the one point every paste goes through), so a Windows paste does not leave two
spaces in a one-line field. The order is: context switch, help or context-list
overlay (dropped); command palette; the view while `inEditMode()`; otherwise
dropped.

**Colour profile.** Lip Gloss v2 has no global profile: `Style.Render` always
emits full colour, and Bubble Tea downsamples to the terminal's profile when it
writes. `main.go` removes the old `COLORTERM` block and, through
`forcedColorProfile(getenv)`, passes `tea.WithColorProfile(colorprofile.TrueColor)`
when `WT_SESSION` is set (Windows Terminal, which WSL does not advertise) unless
`NO_COLOR` is set; elsewhere Bubble Tea detects it. `termenv` is no longer a
dependency; tests need no profile setup and strip SGR with `ansi.Strip`.

Behaviour change: the old code forced TrueColor only when `COLORTERM` was
empty. `forcedColorProfile` forces it as soon as `WT_SESSION` is set and
`NO_COLOR` is empty; `COLORTERM` is no longer read.

Inside a view, `Update` has its own `case tea.PasteMsg` (`handlePaste`) that
routes the paste to the field that has the keyboard (filter bar, form field,
prompt) and drops it otherwise or behind a modal; it never reaches the key
handlers. `FilterBar.Update` takes a paste only while searching. Newlines become
spaces because the one-line `textinput` sanitizes them. `tea.PasteStartMsg` and
`tea.PasteEndMsg` fall through and are ignored.

**Terminal background (DEVDESK-209).** `App.View()` also sets
`tea.View.BackgroundColor`, so the terminal's own default background is the
theme's `ColorBackground` for the session. Rule 115 (`theme.Bg`, `PadWithBg`, ...)
stays mandatory: this is a safety net, not a replacement (see the gaps below).

- *Profile.* The router stores the last `tea.ColorProfileMsg` in
  `App.colorProfile` and does not forward it to the view (no view handles it).
  Bubble Tea sends the message at startup, even when the profile is forced, so
  the first frame may go out without the background and the next one carries it.
  `terminalBackground(profile, colour)` (`internal/app/terminal_background.go`)
  maps it:

  | Profile | `BackgroundColor` |
  |---|---|
  | `TrueColor` | the exact theme colour |
  | `ANSI256` | the colour converted with `colorprofile.ANSI256.Convert`, the same tone Bubble Tea gives painted cells (no seam) |
  | `ANSI` (16 colours), `ASCII` (including `NO_COLOR`), `NoTTY`, `Unknown` | `nil`: the terminal keeps its own background |

  A `nil` or fully transparent theme colour (`lipgloss.NoColor`) also gives `nil`.
  The result must be of a comparable type: the renderer compares frames with `!=`.
- *Theme changes.* `View()` reads `theme.ColorBackground` on every call, with no
  copy in the model, so the background follows `chooseTheme` (startup and a saved theme
  change) and any future path that calls `theme.ApplyTheme`. A theme that cannot
  be used gives the default theme and one footer warning (DEVDESK-258).
- *`app.theme: auto` (DEVDESK-258).* `newWithSize` applies `app.theme` itself
  (`App.chooseTheme`, `internal/app/theme_auto.go`; `main.go` no longer does).
  With `auto` it applies `theme_dark` at once and `Init` (`themeInitCmd`) sends
  one `tea.RequestBackgroundColor` plus a 500 ms `themeDetectTimeoutMsg{gen}`.
  While the answer is pending `View()` leaves `BackgroundColor` nil, so the
  terminal cannot answer with DevDesk's own colour. The router owns
  `tea.BackgroundColorMsg` (not forwarded): the first answer while pending picks
  `theme_light` or `theme_dark` and is kept for the process; a later one is
  ignored. A current timeout falls back to dark without a footer message; a stale
  `gen` does nothing. A named theme chosen in `:config` cancels the wait.
  `chooseTheme(app config.AppConfig, fresh bool) []string` returns the footer
  warnings, one per invalid candidate (`theme_dark` and `theme_light` both in
  `auto`). `fresh` starts a new choice (startup, a save) and clears
  `themeState.warned`, so the terminal's answer (`fresh` false) does not repeat a
  warning already given at startup. Whether to ask is derived from the cached
  answer (`themeState.answer`) and `pending`. `settleTheme` calls `rebuildViews()`
  when the effective theme name changed, since views capture styles with the
  palette in force. Choosing `auto` mid-session
  (`handleConfigSaved`) uses the cached answer at once; without one it starts
  from dark with `pending` set, so `View()` drops its background (OSC 111) and
  `themeQueryDueCmd` defers the query by `themeQueryDelay` (100 ms) as a
  `themeQueryDueMsg{gen}`. `handleThemeQueryDue` then sends the one query plus
  the 500 ms timeout; a stale `gen` or a settled wait does nothing.
  While the startup detection is pending, `Init` does not run the start view's
  `Init` (a theme change would rebuild that view and drop its load);
  `themeState.startInitDeferred` makes `settleTheme` run it once, after
  `rebuildViews()`, on the view then on screen. The flag is consumed by whoever
  runs an `Init` on the view on screen first: `switchView`, `reinitializeViews`,
  or `handleConfigSaved` (when the save leaves no answer pending, including with
  the kept `:config` view). A choice of `auto` in session
  never sets it.
  `ThemeFacts` (`themeFacts()`: `InUse` = the resolution's name, `Reason` =
  `"auto: " + reason` for auto, empty for a named theme) goes to `:config`:
  `configuration.New(..., a.themeFacts())` builds it, and `sendThemeFacts` hands
  a `configuration.ThemeFactsMsg` to the kept view when detection settles
  (answer or timeout) and after a theme save.
- *Sequences.* Bubble Tea writes OSC 11 (`ESC ] 11 ; #rrggbb BEL`) when the value
  changes and when it starts or resumes, never per frame. On close it writes
  OSC 111 (`ESC ] 111 BEL`, "reset to the terminal's default background") after
  leaving the alternate screen. Close runs on quit, `ctrl+c`, `SIGINT`,
  `SIGTERM`, `Kill()` and a panic in `Update`, `View` or a `Cmd`.
- *`tea.ExecProcess`.* Before the child starts, Bubble Tea sends OSC 111, so the
  child (pager, shell, `docker run -it`) sees the terminal's default background,
  not the theme's. When it returns, the renderer restarts, sends OSC 11 again and
  repaints in full. This also holds when the child fails.
  Every exec goes through `internal/ui/teaexec`, which closes a ticker race in
  Bubble Tea (see "Handing the terminal to a child").
- *What is verified.* `terminal_background_test.go` covers `terminalBackground`
  per profile, comparability on every shipped theme, the router per profile (with
  an overlay open, latest profile wins, `ColorProfileMsg` not forwarded, only
  `internal/app/view.go` assigns `BackgroundColor`, enforced by
  `TestOnlyTheRouterSetsTheTerminalBackground`) and theme changes.
  `terminal_background_program_test.go` runs a real `tea.Program` without a TTY
  and checks the bytes written: the sequence starts with the theme's OSC 11 and
  ends with OSC 111 on normal quit, `ctrl+c`, `InterruptMsg`, `Kill()` and a
  panic; OSC 11 then OSC 11 again then OSC 111 on a theme change in session;
  nothing at all under ASCII; the converted colour under ANSI256; and around a
  `tea.Exec`, OSC 111 before the child, OSC 11 after it, and OSC 111 at exit,
  also when the child returns an error. These tests pin Bubble Tea's contract: a
  version bump that changes it fails them.
  The exec test proves the SET is emitted again while the program runs after the
  child, at `GOMAXPROCS(1)`.
- *Limits.* Nothing restores the background after `kill -9`, after a panic in a
  goroutine that is not a `Cmd` (the MCP server), or when the terminal is closed
  (the alternate screen has the same gap). To recover, run `printf '\e]111\a'` or
  `reset`. The "original" background that OSC 111 restores is the default of the
  terminal's profile, not necessarily the colour at launch: a user who changed it
  on the fly (base16-shell, an OSC 11 script) gets the profile's default back.
  `ForegroundColor` is not set, so a light theme on a dark terminal keeps the
  terminal's text colour wherever a cell sets none. There is no config key to turn
  this off; `NO_COLOR` does. A terminal or tmux that ignores OSC 11 shows the
  Rule 115 rendering only. `dk setup` is a separate program and is not covered.
- *To verify (DEVDESK-209, step 6).* Not covered by any test and not documented
  as a finding yet: what the shell started by `tea.ExecProcess` shows on a real
  terminal (inherits the default background or repaints its own: prompt,
  `ls --color`, `less`), the absence of a seam at odd widths, and the behaviour
  under Windows Terminal, tmux and a terminal without OSC 11. To be run by hand
  on a built binary after the merge; the results go in a table here.

**Terminal progress (DEVDESK-211).** `App.View()` also sets
`tea.View.ProgressBar` (OSC 9;4), so the terminal's tab or taskbar shows the work
the jobs registry holds. `jobs.Summarize` (`internal/jobs/progress.go`, pure: no
clock, no registry) reduces runs and failures to a `jobs.Progress`;
`terminalProgress` (`internal/app/terminal_progress.go`) maps it to a
`*tea.ProgressBar`.

- *Scope.* Only the current context (D8): `summarizeJobs` filters runs and
  failures with `jobs.FilterContext` / `jobs.FailuresIn`, and only unfinished runs
  count. Failures of the `index` kind count like any other (`:jobs` lists them), so
  an offline start turns the tab red until `:jobs` is shown once.
- *Precedence* (strongest first, the order of Rule 128):

  | Condition | `Progress.State` | `tea.ProgressBar` |
  |---|---|---|
  | a failure with `Seq` above the read mark | `ProgressError` | `{Error, 100}` |
  | an unfinished run is `Cancelled()` | `ProgressWarning` | `{Warning, Percent}` when measured, else `{Warning, 100}` |
  | unfinished runs, all measured | `ProgressPercent` | `{Default, Percent}` |
  | unfinished runs, at least one not measured | `ProgressIndeterminate` | `{Indeterminate, 0}` |
  | nothing | `ProgressIdle` | `nil`: the bar is cleared |

  A run is *measured* when it is sealed (not open) and holds at least two targets
  (`measurable`): an open run has no total yet, and a one-target run has no
  halfway. `Percent` is `100 * sum(Done) / sum(Total)` over all unfinished runs
  (integer division, so it floors). There is no persistent green bar (Rule 125):
  idle is `nil`. Error and Warning are drawn full so the state is the message and
  the bar stays visible on terminals that hide a 0 value; the 100 is untested on
  real terminals (see below).
- *Acknowledgement.* A failure is "read" once `:jobs` is on screen **and the
  terminal has the focus** (`!terminalBlurred`). `Failure.Seq`
  is a registry-assigned, strictly increasing number (see "The last failure per
  target"). The router keeps one watermark per context
  (`App.failuresReadThrough`); `summarizeJobs` raises it to
  `jobs.LatestFailureSeq` while `currentView == command.ViewJobs` and the terminal is
  not blurred. A failure that arrives while blurred stays red even with `:jobs`
  shown, and opening `:jobs` while blurred reads nothing; on `tea.FocusMsg`,
  the `syncTerminalProgress` that follows every `Update` re-summarizes, so a
  still-shown `:jobs` reads them then. This holds with `app.when_unfocused: run`
  too (the early return in `handleTerminalFocus` changes nothing): the bar stays
  red while blurred and clears on focus.
  The blur does not suspend the bar itself (only the animation tick stops).
  Failures with a higher `Seq` are unread. The failures stay listed in `:jobs`; only the bar goes
  back. A new failure for the same target gets a new `Seq`, so it turns the bar
  red again. The watermark lives in the process, per context, and is not written
  to disk.
- *When it is computed.* `broadcastJobs` calls `summarizeJobs` (from `Update`
  only, Rule 110). `syncTerminalProgress`, run after every `Update`, is a level
  check: it re-summarizes only when the context changed or when `:jobs` is on
  screen, focused, with an unread failure, whatever path brought the view in. Otherwise a
  tick copies no snapshot. `View()` reads the stored `a.progress` and does no work.
- *Limit (criterion 7).* The aggregate percentage has no text equivalent, and the red bar has none outside `:jobs`.
- *One owner.* Only `internal/app/view.go` assigns `ProgressBar`
  (`TestOnlyTheRouterSetsTheTerminalProgress`), and only `terminal_progress.go`
  builds a `tea.ProgressBar` (`TestOnlyTerminalProgressBuildsAProgressBar`): a
  second source would overwrite the first on the next frame.
- *A fresh pointer per `View()`.* `terminalProgress` returns a new pointer on every
  call. The renderer keeps the pointer it last drew and compares through it, so a
  bar mutated in place would look unchanged and never be redrawn
  (`TestViewHandsOutAFreshProgressBarEveryCall`). The renderer writes OSC 9;4 only
  when the value changes, not per frame.
- *Exit and `tea.ExecProcess`.* Bubble Tea 2.1.1 resets the bar (OSC 9;4;0) on
  close, on the same paths as the background reset, and around an exec; it sends
  the bar again when the renderer restarts. DevDesk adds nothing. Exec still goes
  through `internal/ui/teaexec`.
- *Degradation.* A terminal without OSC 9;4 ignores the sequence, and there is no
  way to detect that, so DevDesk does not try. There is no config key to turn the
  bar off (a future `app.terminal_progress: off` is a separate story if a terminal
  is found that prints the sequence). tmux needs `allow-passthrough` to forward it.
- *Text equivalent* (the bar is not the only channel): the `:jobs` header shows
  `Failed N` (the context's remembered failures), the footer appends
  `· M stopping` to `N jobs running`, and the help has a "Terminal tab" section.
- *What is verified.* `internal/jobs/progress_test.go` covers `Summarize`
  (precedence, floor, measured or not, `Seq` mark); `terminal_progress_test.go`
  the mapping, the fresh pointer, a measured scan, an open run, acknowledgement,
  scoping per context, a cancelled sync, that the bar does not change the content,
  and the two source guards; `terminal_progress_program_test.go` runs a real
  `tea.Program` and checks the bytes written (needs `mise run test-race`);
  `terminal_progress_text_test.go` the `:jobs` text.
- *Limits.* Nothing resets the bar after `kill -9`, a panic in a goroutine that is
  not a `Cmd`, or a closed terminal: run `printf '\e]9;4;0\a'` to clear it.
- *To verify by hand* (not covered by any test): Windows Terminal (indeterminate,
  0 %, 50 %, full red, full yellow; cleared at the end; reset on exit and around
  `T`), WSL inside Windows Terminal, tmux without `allow-passthrough` (nothing
  printed, tab unchanged), and a terminal without support such as Terminal.app or
  the Linux console (nothing printed). Open question: whether a full bar at 100 for
  Error and Warning reads right, or looks like completion.

**Window title (DEVDESK-257).** `App.View()` also sets `tea.View.WindowTitle`, so the
terminal's tab or title bar reads `dk · <view> · <context>` (for example
`dk · security · prod`).

- *Content.* `windowTitle(view, context)` (`internal/app/window_title.go`) is a pure
  function. The view is its canonical command name (`string(a.currentView)`, so
  `security`, not the alias `sec`). An empty context name becomes `default`. Each
  segment goes through `titleSegment`: `ansi.Strip`, then every control rune and every
  `Cf`, `Zl` and `Zp` rune is removed (bidi overrides, zero-width characters, BOM, line
  and paragraph separators), then blanks are trimmed. A context name comes from a file
  on disk, so it must not inject a terminal sequence. A segment left empty is skipped
  (the context is the exception: it falls back to `default`). The whole string is cut
  on the right to 60 cells with `…`, so a long context name is shortened first.
- *Computed at render time.* The title is derived from `currentView` and
  `currentContext` in every `View()`, with no state of its own. Overlays (help,
  context list, palette) do not change it.
- *No re-emission.* Nothing writes the title again by hand: the Bubble Tea renderer
  writes it only when the string differs from the last one it wrote.
- *One owner.* Only the router sets `WindowTitle`, and a source guard
  (`TestOnlyTheRouterSetsTheWindowTitle`) fails the suite otherwise.
- *Restoring the previous title (`internal/termtitle`).* Bubble Tea clears the title
  when it exits, which would lose the one the shell or terminal had before. In
  `runTUI`, `termtitle.Preserve` writes XTWINOPS push (`CSI 22;2t`) before `p.Run()`
  and pop (`CSI 23;2t`) after it, in a `defer`, so the pop also runs on an error and
  while a panic unwinds (the panic keeps propagating). Only the two constant sequences
  are written. `main.go` does this **only when stdout is a terminal**
  (`term.IsTerminal`); on a pipe the sequences would arrive as text.
- *Around an exec.* Bubble Tea clears the title before it starts a child
  (`tea.ExecProcess`, and so `teaexec`) and writes it again when the child returns.
  The push and pop stay outside, around `Run()` as a whole: the child sees the
  terminal's own title.
- *Limits.* `kill -9`, or a panic that is not recovered by Bubble Tea (a goroutine that
  is not a `Cmd`), leaves the title cleared or as DevDesk last set it, and the pop
  does not run. A terminal without OSC 0/2 ignores the title, and one without the
  XTWINOPS stack ignores push and pop (the title then stays cleared after exit). Under
  tmux, the title is not passed to the outer terminal unless `set-titles on`.
- *What is verified.* `window_title_test.go` (format, fallback, injection, truncation,
  purity), `window_title_view_test.go` (the router sets it; follows context and view;
  not changed by render events or overlays; does not disturb the other terminal state;
  the guard) and `window_title_program_test.go` (a real `tea.Program` without a TTY:
  written once, cleared on quit and `ctrl+c`, not rewritten by frames, changes with
  the view, cleared then written again around an exec, including one that fails).
  `termtitle_test.go` covers `Preserve`.
- *To verify by hand* (not covered by any test): Windows Terminal, WSL inside Windows
  Terminal, tmux (with and without `set-titles on`) and a console without VT (legacy
  conhost). Check the title shown, the title after quitting (the previous one is back),
  and the title during `T` and any other exec.

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
- `jobs` or `j` - Switch to the jobs view: what is running, and what ran this session
- `agent-usage` or `au` - Switch to the agent usage view: Claude Code sessions found on disk, by project and date
- `templates` or `tpl` - Switch to the repository template catalog (see [templates.md](templates.md))
- `files` or `fs` - Browse the whole filesystem: create and delete files and directories; also the picker a form's path field opens with enter (see [filebrowser.md](filebrowser.md))
- `about` or `version` - Which build is running: version, commit, build date, and where the config, cache and log live
- `context <name>` or `ctx <name>` - Switch to an existing configuration context (never creates one)
- `context -` or `ctx -` - Return to the previous context of this session
- `context new <name>` or `ctx new <name>` - Create a context and switch to it
- `context list` - Show available contexts
- `quit` - Exit application

**The command palette (DEVDESK-109, after DEVDESK-89).** There is no permanent
command line any more. `ctrl+p` or `:` (`:` stays a character inside a field being
edited) opens a modal palette: one frame holding the input and the suggestion list.
The pieces:

- `command.Entries()` (`internal/command/registry.go`) is the one ordered
  registry — name, aliases, description, argument placeholder, kind. The parser,
  the listings and the suggestions all derive from it.
- `command.Suggester` (`internal/command/suggest.go`) ranks the registry for the
  input: exact name or alias, then name prefix, then alias prefix. Once the first
  word names a command that takes an argument and a space follows, it switches to
  the argument stage (`Suggestions.Stage`) with that single entry.
- `internal/ui/cmdsuggest` is pure (state in, lines out). `Geometry(width, height,
  headerLines, footerLines)` returns the `Rect`; `RenderPalette` returns exactly
  `Rect.Height` lines of `Rect.Width` cells, each with an explicit background.
- `internal/ui/overlay` (`Compose`, `Splice`) draws a box over already rendered
  lines with `x/ansi` (`Truncate`, `TruncateLeft`): it never cuts an escape
  sequence, and a row that cannot be spliced exactly falls back to the box cut to
  the room right of `x`, that row only.
- The router (`internal/app/command_suggestions.go`) holds the result, the
  highlighted row (`sugCursor`, -1 for none), the scroll offset and the notice.
  `paletteRect`, `paletteRows`, `paletteLines` and `paletteTitle` derive the frame
  from `a.width`, `a.height`, `a.lastHeaderHeight` and `a.lastFooterHeight`.

**Layout.** Closed, `View()` is the header (7 rows + 1 blank line), the title
line, the bordered viewport and the footer: the viewport has two more lines than
with the old command line. Open, `View()` is built the same way (so opening or
closing changes no height, sends no `WindowSizeMsg`, and `esc` leaves the view
byte-identical), then `View` composes the palette over the final `H` lines, after
`fitHeight` and `lipgloss.Place` (`withPalette`, `view.go`). The frame sits right
under the header's blank line (`y = headerLines`), centered, `min(W-4, 80)` cells
wide, the whole line when `W < 54`. With `V = H - headerLines - footerLines` free
lines, it has `n = clamp(min(8, V-4), 0, 8)` suggestion rows and is `n+4` lines
high, constant while typing: top border carrying the title (`Go to`, or `Contexts`
at the `:ctx` argument stage), input, separator, `n` rows (blank when there are
fewer matches), bottom rule carrying the count or the warning. With `n = 0`
(`V < 5`) the frame is reduced to title, input and rule. The view stays visible
left, right and below. Header rows 1-7 are never covered; the frame is pulled up
one row onto the blank line when it would not fit, and what still does not is
clipped at the bottom, so the input is visible down to a 9-line terminal and
`View()` is always exactly `H` lines, without panic. Below 9 lines the header
wins. The border is `ColorPaletteBorder` (a closed alias of `ColorPrimary`:
`ColorViewportBorder` is under 3:1 in 10 of the 12 shipped themes); text is
`ColorText`; the panel is `ColorBackground`, not `ColorCmdLineBg`, because the
selected row falls back to the latter. The header carries a permanent hint,
`ctrl+p Commands`, on its last row (`ShortcutHintStyle`, never `ColorDim`).
That hint is the only discoverability of the palette: views do not list `ctrl+p`
in `GetShortcuts()` (it would show twice and take room from real shortcuts),
only in their `?` help `KeyBindings`. `TestNoViewListsTheCommandModeKeyInItsShortcuts`
(`internal/ui/keymap/header_hint_scan_test.go`) scans the sources and rejects it
(DEVDESK-124).

**The input** is the router's `textinput.Model`, sized by `sizePaletteInput` (from
`resize`, an Update path) to the frame minus borders, margins, the prompt and the
cursor cell, so a long line scrolls instead of being cut; `paletteInputView` styles
a *copy* at render time (Rule 110).

Keys in the palette: typing ranks again and highlights the best row; `↑`/`↓`
move the highlight without wrapping (from no highlight, `↓` takes the first
visible row and `↑` the last); `tab` writes the highlighted name into the line,
plus a space when it takes an argument, and runs nothing; `enter` opens the
highlighted row, or at the argument stage runs the text as typed (`ctx work`
switches; `ctx new work` creates); `esc` closes the palette. `shift+tab` does
nothing. With nothing highlighted `tab` and `enter` are greyed in the header
(`commandLineShortcuts`, which replaces the view's own shortcuts), and `enter`
says why on the bottom rule: `Type a name or choose one` for an empty line (it
never quits and changes nothing), `"zz" is not a view or a command` for no match.
With no row drawn (`n = 0`) nothing is highlighted and `enter` runs the text as
typed, like the argument stage: `containers` opens the view, `co` warns on the
rule and the palette stays open. **The refusal is never silent:** the frame always
has a bottom rule, so the former declared exception to Rule 130 is gone.
`enter` is greyed or lit by `runnableText` (`command_line.go`), the one function
`commandLineShortcuts` and `runCommand` both read. `fitHeight` (`view.go`) takes the
surplus off the bottom of the viewport first, then cuts the stack to `H` lines. At
13 lines or fewer the FilterBar's top edge can disappear with the viewport, and
while the palette is open on a very short terminal it may cover the footer (the
view's footer is back on close). The notice lasts until the next key, with no timer.
The ghost completion text and the `[i/n]` position that used to follow the input,
and the breadcrumb of views `tab` used to open, are gone. `tab` here is a declared
exception to Rule 135 (`keymap.DeclaredExceptions()`, surface `app/command-line`,
id kept).

**`:ctx` (DEVDESK-88).** `ctx <name>` switches to an *existing* context and never
creates one; creation is `ctx new <name>`, and `ctx -` returns to the previous
context. The pieces:

- `internal/contextcmd` is pure (no I/O, no UI imports). `Resolve(args,
  highlighted, Snapshot)` returns an `Action` (`List`, `Switch`, `Create`,
  `Refuse`, `KeepOpen`) with the footer level and text; `Match`, `DidYouMean`
  (edit distance <= min(2, half the shorter length), or a common start of 3+
  characters) and `ValidName` live there too. `new`, `list` and `-` are
  reserved words: refused as names to create, and `config.CreateContext` refuses
  them as well (`ValidateNewContextName`, `ErrReservedContextName`,
  `ErrContextExists`). Loading does not refuse them, so a context file that
  already carries one of these names still loads (from the list overlay).
- The router keeps the state `Resolve` decides against (`internal/app/context_args.go`):
  `previousContext` (memory only, set in `handleContextSwitchComplete` when the
  context changes; never written to `~/.devdesk/`, so two instances do not share
  it) and `contextArgs`, a snapshot of the context names. The snapshot is read
  off the disk by `loadContextNamesCmd` when the palette opens and after
  each switch, and written only from `Update()` (Rule 110). Until it is `ready`,
  `Resolve` answers `Contexts are still loading — try again`.
- `command.ArgProvider` (`internal/command/suggest.go`) is how the suggester asks
  a command for argument rows without knowing about contexts; `*contextArgs`
  implements it from the snapshot alone, so `SuggestArgs` never touches the disk.
  The rows are the matching contexts (the current one flagged `(current)`), then
  `-` (`Previous context`) and `new` (`Create a context`).
- `internal/app/command_args.go` draws and drives the argument stage: the list is
  `internal/ui/cmdsuggest` rows in the same frame (titled `Contexts`), the rule reads `Contexts · n of
  m`, and `tab` first highlights a row, then writes it into the line. `enter`
  runs `Resolve` with the highlighted row as a hint, and a `tab`-chosen row wins
  over a prefix that matches several contexts. `enter` on an ambiguous prefix
  (`ctx w` with `web` and `work`) switches nothing: `KeepOpen` leaves the line
  open and puts the warning on the list's rule. Commands that take no argument
  keep the old rule (`enter` opens the highlighted row).
- Results are posted with `components.PostFooter` at one of the three levels of
  Rule 128 (not found and invalid name: error; ambiguous, no previous context,
  already exists: warning; already on the context, created: info). A failure
  after the decision (a race with the disk) goes through `contextErrorFooter`.
- A context created by `ctx new` has no forge credentials, so the switch lands on
  the forge authentication screen. That view owns a `components.FooterMessage`
  (`internal/ui/forge/auth`) so the `Context "x" created` notice posted at the
  same moment is shown there instead of being dropped.

Command parsing lives in `internal/command/`. `ParseCommand()` returns a structured `Command{Type, View, Args}` supporting `CommandView`, `CommandContext`, `CommandQuit`, `CommandUnknown`.

**The forge views are `git-auth` and `git-explorer`, named after the role.**
A context targets one forge (§3.6), so there is one authentication screen and
one explorer, and both adapt to whichever it is — a `gitlab-` prefix would have
to be typed as `github-` half the time for the same view. Their packages live
under `internal/ui/forge/`, which is what the import line says too.

**The ten forge-prefixed spellings — `gitlab-auth`, `gla`, `github-auth`, `gha`
and their explorer counterparts, plus the older `explorer` / `exp` — no longer
parse at all.** They lived in a `legacyNames` table for a transition period
after §3.6 renamed the views, resolving without being suggested so muscle
memory would not break outright. That table has been removed: one spelling per
view, no exceptions, and `git-auth`/`ga`, `git-explorer`/`ge` are the only ones
a client gets.

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
configuration view and the agent usage view are the only two that implement it,
and every other view is left without ceremony. The configuration view uses it to
settle an edit; the agent usage view uses it for something else, to stop a timer
and abandon a read in flight (`Leave` bumps its generation, see `agent-usage.md`),
and it never refuses. Re-entering the view already on screen settles nothing:
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
a keyboard protocol DevDesk does not request (Bubble Tea v2 can, Rule 101 forbids relying on it). The reasoning lives in the
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

**The space key is `keymap.Space`** (DEVDESK-200). Bubble Tea v1 reported it as
`" "` in `KeyMsg.String()`, v2 reports `"space"` in `KeyPressMsg.String()`, and nothing flags the change: a
`case " "` just stops matching and every checkbox stops toggling. Production
code binds it as `case keymap.Space:`, so a v2 move changes one line. Two
source guards in `internal/ui/keymap` hold this:

| | |
|---|---|
| `TestNoKeyIsComparedToALiteralSpace` (`space_scan_test.go`) | rejects `" "` and `"space"` as a `case` clause of any switch, or as an operand of `==`/`!=` in a file that imports bubbletea. It also fails if it sees no `keymap.Space` at all, so a broken scan cannot pass for a clean tree |
| `TestNoProductionCodeReadsLegacyKeyFields` (`legacy_key_scan_test.go`) | rejects the v1-only key API in production code: `.Runes`, `.Alt`, `.Paste`, `tea.KeyType` and every `tea.Key<Name>` constant except the v2 types `tea.Key`, `tea.KeyPressMsg`, `tea.KeyReleaseMsg`, `tea.KeyboardEnhancementsMsg` (and `tea.KeyMsg`, see next row). Code reads a key by `String()` only |
| `TestNoProductionCodeReadsKeyPressFields` (`keypress_field_scan_test.go`) | rejects a read of the v2 fields `.Code`, `.Text` and `.Mod` on a `tea.KeyPressMsg` or `tea.Key` value (DEVDESK-279, D117 a). Syntactic, like its siblings: per top-level declaration it tracks identifiers typed as those types (parameter, result, receiver, `var`), names assigned from a key value to a fixed point (`x, ok := m.(tea.KeyPressMsg)`, a composite literal, `k := msg`, `k := msg.Key()`, since `KeyPressMsg.Key()` returns a `tea.Key`), the type-switch binding only inside the clause whose single type is a key type, and direct assertions or `.Key()` calls. Other types with `Code`/`Text`/`Mod` are not flagged. Not covered: a struct embedding or holding a `tea.KeyPressMsg`/`tea.Key`; a type alias of a key type; a key obtained from a function result other than `.Key()` (or a range/channel receive); a name reused for another type in the same declaration outside a type-switch clause (tracking is by name, so that is a false positive) |
| `TestNoProductionCodeMatchesTheKeyMsgInterface` (`legacy_key_scan_test.go`) | rejects any use of `tea.KeyMsg` (case, assertion, parameter, result): in v2 it is an interface that also matches key releases. Handlers use `tea.KeyPressMsg` |
| `TestEveryViewportFedKeysSetsAKeyMap` (`viewport_keymap_scan_test.go`) | a `bubbles/viewport` v2 scrolls horizontally on `left`/`right`/`h`/`l` by default, which Rule 135 and the no-bare-letter rule forbid. Every `X.Update(` on a viewport-named `X` needs an `X.KeyMap = …` in the same package (the help overlay's `arrowOnlyScroll`). Limit: it keys on the name (`viewport`/`vp`), so a viewport stored under another name, or a KeyMap set through a helper, escapes it |

Both skip `internal/ui/keymap` (it declares the vocabulary) and
`internal/ui/testutil` (it builds test keys; DEVDESK-199's source guard owns it).
Each has a detector test on inline sources, so the rule is checked on both sides.

The vocabulary scanner (`keymap_test.go`) reads `case keymap.X:` too.
`keymapConstants` parses the top-level string constants of `keymap.go` and
`clausesOf` resolves `keymap.X` selectors through them. Without that, a switch
whose only structural key became `keymap.Space` would stop being recognised as a
key switch and its letters would leave the vocabulary tests.
`TestTheKeyScanReadsKeymapConstants` fails if the scanner skips such clauses.

Known limits of the space guard (stated, not hidden). It reads switch clauses
and `==`/`!=` only, so it does **not** see:

- `slices.Contains(keys, " ")` and other function-call comparisons (`strings.EqualFold`);
- a map keyed by `" "`;
- `key.WithKeys(" ")` (bubbles key bindings);
- a local constant (`const sp = " "`) used in a `case`.

The `!=`/`==` check applies only in files importing bubbletea. Review these
four by hand.

**Free letters are declared too** (`Q Z` — `J` went to `Jump`, §3.89). A new action takes one of them;
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

**`FrameTrailView` is optional.** A view that implements
`FrameTrail(maxWidth int) string` (`internal/app/app_header.go`) puts a trail on
the viewport's top border, in parentheses right after the title.
`App.renderTitleLine` computes the budget with `theme.BorderTrailBudget(title,
width)` (`width - W(title) - 9`, where 9 is `theme.BorderTrailChrome`: `┌─ `,
` (`, `) `, at least one `─`, `┐`) and passes it to the view. The view returns a
styled string, measured plain first (Rule 122), or `""` for none.
`theme.RenderBorderTitleTrail` adds the parentheses and spaces in the border's
style and pads with `─` to exactly `width` cells. The router never cuts a trail:
one wider than its budget is dropped, and so is one when the budget is 0 or
less. With no trail the line is byte-identical to `RenderBorderTitle`. A
`FramelessView` gets none (its title is a rule, not a border). Only
`agentusageview` implements it, at the agents level:
`┌─ 󰚩 Agent usage › app (Sessions › ~/w/app › 1a2b3c4d) ───┐`.

There are **three** name lists, and each answers a different question:

| | Answers | Read by |
|---|---|---|
| `ViewNames()` | what can a user type | completion, `app.default_view` |
| `AllViewNames()` | what can appear in the viewport | the router's contract tests |
| `FullNames()` | every command, views and actions | completion |

All three, and the parser maps, derive from one ordered registry
(`internal/command/registry.go`, `command.Entries()`): 15 views A-Z, then the
actions `context` and `quit`, each with its aliases, a description of at most 45
runes and an optional argument placeholder. `FullNames()` follows that order.

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
operation in flight is neither — it changes every tick, the row's activity dot
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

- **Not knowing is not knowing that not.** `scan.Detect` shells out,
  so it runs in the router's `Cmd` (`shared.ScanToolsMsg`) and `S`/`A` stay lit
  until it lands. Once it has, they are greyed on `Report.CanScan` — some tool
  ticked in an enabled category, applicable to the target, and there.
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
  that lists bindings. The row's own activity dot already says it (Rule 139).
  `TestNoShortcutAdvertisesAGlyphAsAKey` checks the Private Use
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
- Context switching reinitializes all views with new config. While the switch
  loads (config, secret store, auto-login over the network) `switchingTo` is
  set: an overlay replaces the whole interface and only `ctrl+c` gets through,
  so the first views drawn afterwards are the new context's (D76)


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
- `ForgeIndex *forgeindex.Index` — everything the session can see on the
  forge, at every depth, below. It is *not* the `CachedGroups`/`CachedProjects`
  D36 removed: those were never written; this one is written at every session
  start and never blocks anything
- `GitLabStats`, `DockerStats`, `OCIStats` — Dashboard counters. `GitLabStats`
  is a `*forge.DashboardStats`, whose five counters are each a `*int`: `nil`
  means nobody could read it, and the dashboard prints `-` rather than the `0`
  that read as "you have none" (D52)
- `ServiceStatus`, `ServiceComponents` — Status monitoring results
- `WorkspaceCount` — the dashboard's workspace counter
- `Tools *scan.Report` — the one detection of the scanners and the platform
  tools (§3.86), below

### The scanners are detected once, by the router

`internal/app/scan_tools.go`. `ws`, `oci`, `templates` and the dashboard used
to run a detection each — a LookPath and a `--version` per tool, a `docker
images` per tool in image mode, and four answers free to disagree. The router
now runs `scan.Detect` in a `Cmd` against a **copy** of `config.Scan.Tools`
(Rule 110), keeps the answer in `shared.State.Tools`, and hands it to every
view it holds as `shared.ScanToolsMsg` — `broadcastJobs`'s pattern, and
`createView` sends it to a view built afterwards.

| Trigger | Where |
|---|---|
| start | `Init` |
| context switch | `handleContextSwitchComplete` — `forgetScanTools` first, so the new views say "not known yet" rather than show the old context's answer |
| a saved tool source, binary or image, or the engine | `handleConfigSaved` → `redetectIfToolsMoved`, comparing against the settings the last detection ran on (`scan.SameDetection`) — the configuration view edits the router's own `*config.Config`, so there is no "before" to compare to otherwise |
| `ctrl+r` on the dashboard | `shared.ScanToolsDetectRequestMsg` |

**Not on a timer.** A tool is installed far less often than the dashboard
ticks; `ctrl+r` is the way to see one installed while DevDesk runs.

**Every request is numbered** (`toolsGen`), and an answer that is not the latest
is dropped: a slow detection started before a source change would otherwise
land after the next one and put the old state back.

**A category change does not re-detect.** What is *required* is worked out from
the configuration at read time (`scan.Required`, `Report.Missing(categories)`,
`Report.CanScan(categories, target)`); the report only says what is there.

A scan runs on the same answer: each view sets `ScanOptions.Detected` where it
assembles its options, so `NewScanner` does not probe the machine again. A tool
that vanished in between fails at its stage, with the stage's own message.

### The forge index is built once per session, by the router

`internal/app/forge_index.go`, `internal/forgeindex`. The explorer's `g` needs
every path the session can see, and a drill-down is faster when the level is
already known — so the router builds one list for both, the way it detects the
scanners once for every view.

| When | What |
|---|---|
| a session opens — auto-login, manual login, context switch (`setAuthenticated`) | the file the last walk left (`~/.devdesk/cache/forge/<context>.json`) is read at once; the walk waits for it |
| the file lands | installed, unless it was written for another host or account (`Index.Matches`) or a walk already landed. A walk starts as a job (`jobs.KindIndex`, cancellable in `:jobs`) only when the file cannot stand in for one (`forgeIndexNeedsWalk`): missing or not this account's, older than `forgeIndexMaxAge` (15 min), or carrying `Unlisted` namespaces — and not beside a walk already out. Leaving a context and coming back within minutes walks nothing (D77) |
| the walk lands | installed, written back to disk; a namespace whose listing failed is kept but marked unknown (`Unlisted`), and the run's detail says how many |
| a view edits it (`shared.ForgeIndexEditMsg`) | a create, a delete, a level re-read — the edit is a **function** applied to the router's current index. While a walk is out it is also kept (`forgeIndexPending`) and replayed on the walk's answer, which was read before it: a create or a delete confirmed during a walk is not undone when the walk lands. An edit that returns the same index (a level re-read unchanged, the common case) writes and broadcasts nothing |
| `ctrl+r` in the explorer (`shared.ForgeIndexRefreshMsg`) | a new walk, unless one is running; the index in place stays meanwhile |
| the session closes (`clearAuthenticated`) | the walk is cancelled, the index dropped; the file stays for the next session |

**Every change is broadcast** as `shared.ForgeIndexChangedMsg` — no payload, the
index is on the shared state. **Answers are numbered** (`forgeIndexGen`) like the
scanners' detection, so a walk from a closed session cannot land in the next one.

**The walk is undecorated.** No role, no CI status: on GitLab those cost two
requests per repository, and a pipeline status is stale within the minute. The
explorer decorates the level it shows (`docs/architecture/forge.md`).

**An index is a value.** `With`, `Without` and `ReplaceLevel` return a new one:
the router hands the same index to a `Cmd` writing it to disk and to the views
reading it, and an in-place edit would race one against the other.

## Cross-View Communication

Key messages in `internal/app/messages.go`:
- `SwitchViewMsg` — navigate to another view
- `SelectionRequestMsg` / `SelectionResultMsg` — selection mode (e.g., workspaces opened from security view to pick a repo)
- `ImageScanResultLoadedMsg` / `WorkspaceScanResultLoadedMsg` — cached results ready

### Long-running work reports without taking the screen

A scan, a sync or a delete outlives the moment the user spends looking at the
view that started it. Its progress messages are therefore addressed **by name**
to that view — `routeToView(target, msg)` in `app.go` — and never to the view on
screen, and routing one never changes `currentView`.

Three properties, and each is load-bearing:

| | |
|---|---|
| The owning view is updated | it holds the row's marker, its footer state and its cache write; a message dropped because the user walked away leaves the row marked busy for the life of the view |
| The active view is *not* | it would act on progress that is not its own |
| `currentView` is untouched | `handleWorkspaceScanComplete` used to set it on every finished repository, so a batch of twelve made every other view unusable until the last one landed (D67) |

The router does not re-measure the layout on these, unlike
`forwardToActiveView`: a view the user is not looking at cannot change the
footer's shape on screen, and it is measured on its way back in — `switchView`
asks for a resize.

Making the marker survive re-entry is what makes the routing safe: `setEntries`
deliberately does not touch `scanningPaths`, so the reload `Init` triggers on
the way back in leaves in-flight work alone.

### The jobs registry — `internal/jobs`

One bookkeeping of everything long-running, owned by the router
(`App.jobs`). Before it there were four, and none could see the others:
`workspaces` kept three maps of paths, `oci_resources` a map of image names, the
security inventory a flag per row, and the clone screen five states per
repository. A scan started from `:sec` was therefore invisible to the `busy()`
guard in `ws`, and the two wrote the same cache entry.

Two levels: a **`Run`** is one batch started in one go from one view, and holds
an **`Item`** per target. `RunState` is *derived* from the items and never
stored — a field would be a second answer to a question the items already
answer, and the two drift the first time a transition is missed.

| | |
|---|---|
| `Start(run)` | admits a batch, from `Update` and never from a `Cmd` (Rule 110). Every launch site knows its full target list when it dispatches, which is what makes "8 waiting" sayable |
| `Advance(id, target, state, detail)` | one item moves. Returns false for work nobody registered, rather than swallowing it |
| `Attach` / `AttachRun` | store a `context.CancelFunc`; it arrives in the message that says the item started, because the context is created inside the `Cmd` |
| `Cancel(id)` | stops the queue always; cuts work in flight only where that leaves nothing behind (`Kind.Cancellable`) |
| `Snapshot()` | a **copy**, cancel functions cleared |
| `Running()` | runs that have not settled |
| `Failures()` | a copy of the remembered failures, oldest first |
| `Forget(target, ctx) bool` | drops the failure memorised for that target in that context; reports whether one was removed |

`queued` and `running` are kept apart: the semaphore already knows the
difference and the clone screen already shows it, so collapsing them would make
that screen the only honest one.

**A kind is not reserved for batches.** `create` and `delete` in the explorer
are single-item runs against a forge, and they are registered for the three
things the registry gives that a local flag does not: the blink phase comes
from `jobs.FrameMsg` rather than from a view's own chain — the explorer's stops on
`!m.loading`, so a phase taken from it would freeze the moment the tree settled;
the `busy()` guard sees work started anywhere; and `Run.Context` is stamped at
launch, so a create that outlives a context switch cannot write into the tree it
switched to. What decides is whether the work is a network call the user has to
be told about, not how many targets it has.

`index` is the one kind no view starts: the router registers the forge walk
itself when a session opens (above). It is cancellable — the walk only reads,
and stopping it leaves the previous index in place.

Runs live for the session, capped at the last `MaxFinishedRuns` settled ones; a
run still going is never pruned. Each is stamped with the context it started in,
and views **filter** (`jobs.FilterContext`) rather than the registry purging on a
switch — purging would contradict keeping them for the session.

#### The stamp is what the work runs under

A launch is two things and the router sequences them: it admits the run, then it
*builds* the work with the name it just stamped.

```go
type StartMsg struct {
	Run  Run
	Work func(contextName string) tea.Cmd
}
```

`Work` is a builder rather than a command because of D68. Stamping the run alone
left the registry telling the truth while every command of the batch read
`config.CurrentContextName()` for itself, minutes later — so a scan launched in
one context and finished in another wrote its counts into the cache of the
context the user had switched **to**. The builder is called once, in `Update`, so
the name is read once and every command of the launch shares the string.

`jobs.Start` remains for work that does not depend on the context; a launch site
with no use for the name reads exactly as it did.

What must carry the stamp is what **writes a result someone asked for under a
name** — a scan's counts, and the purge that clears them for it. What must not is
what *shows* the current context: a load (`loadScanCacheCmd`, `loadInventoryCmd`)
and a title both mean "current" by definition, and reading it is right there.

#### An immediate run — admitted already running

`jobs.NewImmediateRun(kind, origin, context, label, target)` is `NewRun` with the
single item set to `ItemRunning`. It is for work that goes out with its start
message (an explorer create or delete): nothing queues in front of it, and a
`queued` item that no message ever advances showed `◴` in `:jobs` and made
`Run.Stoppable()` true, so `K` would have marked a delete in flight `skipped`.
With it the run shows `●`, `Stoppable()` is false and `K` is greyed. The
constructor does not decide stoppability: that stays with `Kind.Cancellable`.
The explorer uses it (DEVDESK-147). `ws` delete and `:files` delete still use
`NewRun` and keep the defect (DEVDESK-227).

#### An open run — the one whose targets arrive as it goes

Every launch site knows its full target list when it dispatches, which is what
makes "8 waiting" sayable (D10). The clone does not: the walk that discovers
repositories *is* the slow part, so a run that waited for the list would show
nothing for the minutes that matter.

`NewOpenRun` registers a run with no targets, and three methods carry it:

| | |
|---|---|
| `Discover(kind, target, state, detail)` | appends a target, or advances one already there — the walk reports a group it could not list, and that path may also turn up as a repository |
| `Seal(kind)` | says the walk has found everything it is going to |
| `CancelOpen(kind)` | stops it, and seals — what would have closed the run is what was stopped |

An open run is **never finished**, whatever its items say. `Transition.Discover`
is how a message asks for the first; `jobs.Sealer` is a second interface for the
second, because a closed channel names no target.

All three name a **kind** rather than an identifier: a progressive run belongs
to a screen that owns the display while it goes, so there is one per kind at a
time and a view can name it without holding a `JobID` — the registry state D1
keeps out of views.

#### Stopping work — the two halves of D7

They are not the same question, and the key is offered on each independently:

| | What it does | Offered when |
|---|---|---|
| `K` on a **run** (`Cancel`) | the queue stops — nothing further starts, whatever the kind; queued targets become `skipped: cancelled` | `Run.Stoppable()` |
| `K` on a **target** (`CancelItem`) | that one piece of work is cut | `Run.ItemStoppable(item)` — i.e. `Kind.Cancellable()` |

`Run.Stoppable()` is the question about *this run*, not about its kind, and the
delete is the case it exists for: one item, in flight, of a kind that must never
be cut. `!Finished()` would offer the key and then refuse it, which is exactly
the silent refusal Rule 130 removes. The **create** answers the same way and for
a related reason — a request already sent cannot be un-sent, and a project whose
template is half applied is a state the forge holds, not one we can roll back.

**A running target is asked to stop, never declared stopped.** It reports its
own outcome when it gets there; settling it here would race the message that
says how it actually ended. A *queued* one is skipped outright — it will not
run, and leaving it queued would keep the run reading as running forever.

**The cancel travels on the transition that says the work started**
(`Transition.Cancel`). The context is created inside the `Cmd`, so it reaches
`Update` in that very message and the registry stores it in the same step that
marks the item running. Two steps would leave a window where the row spins and
the key does nothing — which is why `Attach` was folded into `Advance` and
removed.

`CancelMsg` and `CancelItemMsg` name a `JobID`, where `CancelOpenMsg` names a
kind. The difference is which view is asking: a launch site cannot hold an
identifier — it would have to before the registry allocated one — but `:jobs`
reads it off the row it is rendering.

#### The last failure per target

The registry remembers the last failure of each target, so a view can show it
after the run that produced it was pruned or the view was rebuilt. It lives in
`internal/jobs/failure.go` and `registry.go`.

**The key** is `FailureKey{Kind, Target, Context}`: the run's kind, `TargetKey(item.Target)`,
and `Run.Context`. One entry per key; a new failure for a key replaces the old one
and moves it to the newest position. A failure that lands after a context switch
is filed under the context it ran in, not the current one. `Item.Target` itself
is not rewritten; only the key is normalized.

**`TargetKey` cleans absolute paths and nothing else** (`filepath.Clean`). Image
references, forge paths and slugs are kept raw, because two spellings are not
provably the same target:

- `nginx` and `docker.io/library/nginx` are different keys: only the registry
  knows they resolve alike, and the memory does not ask it;
- an image is not a repository: a failure on `nginx` never shows on a repo of the
  same name, and the other way round (the kind is part of the key too).

**Transitions** are decided in one place, `Registry.record`, called from `Advance`
and `Discover`:

| Event | Effect |
|---|---|
| item `failed` | poses the failure (`Display`, `Detail`, `At`) |
| item `done` | clears the key |
| `Start` / `Discover` admitting a target | clears the key: a new attempt replaces the last word |
| `delete` item `done` | clears the target for every kind, and its descendants when it is an absolute path |
| item `skipped`, `Cancel` | nothing: a refusal or a stop is not a failure |
| `ForgetTargetMsg` | same as a done delete, for a removal no run did (an image remove) |

**`Failure.Seq`** (DEVDESK-211) is a number the registry assigns, strictly
increasing across all contexts, each time a failure is posed (a replacement gets a
new one). A reader keeps the highest `Seq` it has shown and treats anything above it
as unread: the terminal progress bar does this (see "Terminal progress").
`jobs.LatestFailureSeq` and `jobs.FailuresIn(failures, context)` serve it.

`ForgetTargetMsg` is stamped with the **current** context by the router
(`handleForgetTarget`); it never touches another context's failures, and
broadcasts only when something went. Descendants are matched on path only: a
non-absolute target is matched exactly.

**Bounds.** At most `MaxFailures` (500) entries; the oldest goes first. They are
independent of `MaxFinishedRuns`: pruning a settled run keeps its failures. The
memory lives for the process and is never written to disk. `Kind.Remembered()`
exists as the one place a policy could narrow this, and today every kind is
remembered, `index` included.

**Reading.** `ChangedMsg.Failures` carries a copy, oldest first, every context
included, like `Runs`. The view filters: `jobs.FailuresOn(failures, context, target, kinds...)`
returns what is on a target, and `jobs.FailureFor(failures, kind, target, context)`
one entry. Which kinds a view shows (D5) is the reader's choice, passed as `kinds`;
the registry keeps all of them.

**`ChangedMsg.Context`** (DEVDESK-145) is the router's current context when the
snapshot is sent, stamped in `changedMsg()` (`internal/app/jobs.go`), the only
builder. A view reads `Runs` and `Failures` against it, so it needs no
constructor argument: `:sec` stores it in `handleJobsChanged`. An unfed view has
an empty context, which matches no run. `ws` and `:jobs` still take the name as a
constructor argument (same value).

`security.scanTarget.Failed` was one of the two manual copies this replaces; it
was removed by DEVDESK-145 (A-08), and `:sec` reads `Failures` (scan kind only).
OCI's `failedScans` was the other; DEVDESK-146 (A-09a) removed it, and the
Images table reads `Failures` too (scan and pull kinds).

### A progress message needs a case in the router

A message reports to the registry by implementing `jobs.Reporter`, and the
registry hears it only when `routeWork` runs — which is called from a `case`
naming the message in `App.Update`. **Implementing the interface is not enough.**
A message with no case is still delivered to its view, which is what hides the
omission: the footer says the work finished while the registry keeps the run in
progress, and `:jobs` and the footer's status line show it running for the life
of the session. The scan and then the sync of the templates view each shipped
that way.

`TestEveryJobReporterIsRoutedToTheRegistry` scans `internal/ui` for
`_ jobs.Reporter = X{}` declarations and fails naming any message the router has
no case for. Declare the compile-time proof next to the message, as the views
do, and the test finds it.

### The broadcast, and the animation tick

Views never hold a pointer to the registry. The router hands them a snapshot in
`jobs.ChangedMsg`, so nothing is read from `View()` while something else writes
it, and a view holding a snapshot cannot write back — every mutation goes
through the one owner. `ChangedMsg.Failures` follows the same rule: a copy of
the failure memory (above), never the registry's slice.

**`ChangedMsg` is for changes of the registry, and carries no animation.**
`broadcastJobs` updates **every held view**, on screen or not, so coming back to
one shows what happened while it was away without it having to ask; the layout
is re-measured once at the end, since only the view on screen can change the
footer's height. A view built lazily on the way in was there for none of it, so
every path that installs a view hands it the snapshot (`sendJobsTo`).

**The animation travels in `jobs.FrameMsg`, to the view on screen only.** It
carries `Blink` only (true is the "on" half): no router frame text exists any
more, rows and footers draw the activity dot from the phase (DEVDESK-341).
Only the router builds one (`frameMsg`, in `internal/app/animation.go`).
Before DEVDESK-139 the frame rode in `ChangedMsg` and every tick made every held
view rebuild its rows; now a tick touches one view. `enterView` (snapshot plus
current frame) is called wherever a view takes the screen: `switchView`,
`openBrowser`, the path picker, `openSecurityView`, `rescanInOrigin` and
`returnToOrigin`. Without it a view would show the previous frame, or an "off"
phase, until the next tick.

#### One tick, one chain

There is **one** animation chain for the whole application (D5, renamed
"animation" by D2; it used to be the spinner chain): `animationTickMsg{seq, at}`,
started only by `ensureTick`, renewed only by `handleAnimationTick`
(`TestTheRouterHasOneAnimationChain` enforces both). It is built on `tea.Every`,
so ticks land on the clock's boundaries and the blink alternates strictly.

`App.Update` is a thin wrapper: it runs `update` (the dispatch), then
`ensureTick`. The check is a **level**, not an edge — it runs after every
message, so no handler has to remember to start a chain, and it is idempotent
within one `Update`.

| Need | Cadence |
|---|---|
| `jobs.Running() > 0` (the views refresh durations and the dot), `motionStatic` included | 500 ms |
| the view on screen has nothing running but `WantsBlink()` (`BlinkingView`) | 500 ms |
| the terminal is blurred (`terminalBlurred`), whatever the two above say | no chain |
| neither, or `motionStatic` with no running work | no chain |

`BlinkingView` sits beside `FormView` and `LeavingView`: an optional interface for
a row that blinks for a reason the registry does not know (an instant action, an
agent running, children loading). The router asks the view on screen after every
`Update`.

- **Seq.** A tick of a chain that was replaced is dropped without a Cmd, so a
  second chain is impossible rather than merely unlikely.
- **One interval.** Every need runs at 500 ms, so a live chain is never replaced
  when a job starts (`ensureTick` keeps a chain whose interval is `<=` the wanted
  one). When no cadence is wanted it is not renewed and dies.
- **Phase.** Read off the tick's own time, not a counter:
  `(t.UnixMilli()/500)%2 == 0` is on. Every view blinks in step whatever the
  interval the chain runs at (1 Hz).
- **At rest the phase is forced "on".** When the chain stops on an "off" phase,
  the router pins it on and sends one last `FrameMsg` to the view on screen: a
  settled row is left on a steady glyph, never a half-blinked one.
- **`motionStatic`.** Pins the phase on and keeps blink-only chains from
  starting (`setMotionStatic` hands the pinned frame to the view on screen at
  once). `app.motion` feeds `App.motionStatic` through `applyMotion`: at
  startup (`newWithSize`), after a context switch (`handleContextSwitchComplete`,
  once the views are rebuilt) and on `ConfigSavedMsg` (`handleConfigSaved`).

`jobsChanged` is what a launch site or a transition calls after writing to the
registry: `ChangedMsg` to every held view, `FrameMsg` to the view on screen. The
`Update` wrapper then makes sure a chain is alive; the three are separate, and
none implies another.

This is what removes the four hand-stamped frames, and with them the failure
each of them could produce: a chain that died on the first idle tick with
nothing able to restart it, leaving the spinner frozen on the frame it died at.

#### Terminal focus (DEVDESK-210)

Bubble Tea v2 reports whether the terminal has the focus (xterm mode 1004) when
the view asks for it. The router uses it to stop the animation chain while the
terminal is blurred, and to tell the view on screen so it can skip its periodic
reads (see "Routing the level to a view").

- **Asking.** `App.View` is the only place that sets `ReportFocus`. A view's
  `View()` never does, like `AltScreen` and `BackgroundColor`.
- **The level.** `App.terminalBlurred` is a bool whose zero value means
  focused. `tea.BlurMsg` sets it (`handleTerminalBlur`), `tea.FocusMsg` clears
  it (`handleTerminalFocus`). Neither returns a Cmd.
- **The cadence.** `ensureTick` and `handleAnimationTick` read `wantedCadence`,
  not `cadence` directly. While blurred it answers "no chain", whatever the
  registry or the view on screen asks for; otherwise it is `cadence(...)`.
  `cadence()` itself is unchanged.
- **No seq bump on blur.** The in-flight tick still arrives, finds that no
  cadence is wanted and does not renew the chain, so the chain dies by itself.
  A blur always hands the view one last `FrameMsg` with the phase pinned on,
  so a row is never left on a half-blinked glyph. After a focus, the `Update`
  wrapper's `ensureTick` starts one chain. If the focus comes before the
  in-flight tick lands, that tick renews the chain: still one chain.
- **Not relayed.** `BlurMsg` and `FocusMsg` are not forwarded to any view.
  Forwarding a raw focus would make `bubbles/cursor` restart its blink on
  cursors the view never meant to show.
- **Jobs are untouched.** A blur never cancels, pauses or delays a job:
  `jobs.Running()`, the registry and its broadcasts (`ChangedMsg`, failures
  included) work as usual. Only the animation stops.

**Routing the level to a view (step 2).** The router does not relay the raw
events (see "Not relayed"). It sends the view its own message,
`shared.TerminalFocusMsg{Focused, Resume}` (`internal/shared/focus.go`), built
by `App.sendFocusTo` (`internal/app/focus.go`). Five views consume it:
`:agent-usage`, `:containers`, `:net` (Ports poll), the dashboard and `:oci`;
their behaviour is in `agent-usage.md`, `network.md` and `ui-components.md`.

- **Who receives it.** Only the view on screen (`a.currentView`). A view the
  router keeps but does not show gets nothing, so a retained view never learns
  of a blur while it is hidden.
- **`Focused`** is `!suspended()` at the time of sending (see "The
  `when_unfocused` policy" below): true whenever the terminal has the focus, and
  also while it is blurred under `run`.
- **`Resume`** is true only on the focus edge that follows a blur. It tells
  the view it may catch up on what it paused (a refresh tick, for example). It
  is never true while blurred.
- **When it is sent.**
  - On the blur edge: `Focused:false, Resume:false` (`handleTerminalBlur`).
  - On the focus edge after a blur: `Focused:true, Resume:true`
    (`handleTerminalFocus`).
  - When a view enters the screen: `enterView` sends the current level with
    `Resume:false`, next to the jobs snapshot and the frame.
  - After `reinitializeViews` rebuilds the views, with the current level and
    `Resume:false`, so a rebuilt view does not start unaware of a blur still
    in effect.
- **A key or a paste while blurred** counts as the focus coming back (see
  below). `update` keeps the Cmd of that `Resume:true` message and batches it
  with the Cmd of the key or paste handler (`tea.Batch`), so neither is lost.
- **What it never does.** It calls no `Init`, starts no scan and sets no
  footer message. What a view does with it is the view's choice.

*Known limit.* Only the paths that go through `enterView` hand the level to
the incoming view. These paths change `a.currentView` without it, so the view
they show is not told the level: the viewer (`viewer.go`), netdiag
(`netdiag.go`), the context switch (`context.go`) and the returns from the
file picker and from selection mode (`picker.go`, `selection.go`). If the
terminal is blurred when one of them runs, that view assumes it is focused
until the next edge. Closing this gap is not part of step 2.

Two hypotheses keep a lost or missing event from leaving the router wrong:

- **A `FocusMsg` with no prior blur does nothing.** `Update` returns at once,
  with no Cmd: an unknown focus means focused.
- **A key press or a paste while blurred counts as the focus coming back.**
  `update` runs `handleTerminalFocus` before dispatching it. This heals a lost
  focus-in (a tmux pane switch, or a hand-off to a child process, see
  [Handing the terminal to a child](#handing-the-terminal-to-a-child--internaluiteaexec)).

**The `when_unfocused` policy.** `app.when_unfocused` (`pause`, the default, or
`run`; see `configuration.md`) decides whether a blur suspends anything.
`App.runWhenUnfocused` holds it, read from the config by `RunsWhenUnfocused()`.
`App.suspended()` is `terminalBlurred && !runWhenUnfocused`. Everything above
that stops on a blur reads `suspended()`, not `terminalBlurred`: `wantedCadence`,
the pinned last frame, and the `Focused` and `Resume` fields of
`TerminalFocusMsg` (`Resume` is `resume && !suspended()`). With `run`, nothing is
suspended: the animation chain keeps going and every view gets `Focused:true`.
`terminalBlurred` itself still tracks the real focus.

The setting applies without a restart. `applyWhenUnfocused` runs on
`ConfigSavedMsg` and on a context switch. If the policy changes while the
terminal is blurred, the view on screen is told at once (`pause` to `run`:
resumed; `run` to `pause`: suspended).

**Degradation.** A terminal without mode 1004 never sends `BlurMsg`, so
`terminalBlurred` stays false and the application behaves as before. Under tmux
the host needs `set -g focus-events on`; without it tmux swallows the events and
DevDesk never sees a blur. A terminal that is visible but not focused (a side
pane) is treated as blurred: its animation and periodic reads stop, unless
`app.when_unfocused` is `run`.

#### A row dot or a footer mark

Two things move on screen, and they answer different questions. The footer's braille spinner is gone (DEVDESK-377): `Status.Spinner`, `FooterMessage.spinnerFrame`, `SetSpinnerFrame`, `theme.SpinnerMessage` and `theme.SpinnerStyle` no longer exist, and the dot is the only mark.

| | Says | Where | Driven by |
|---|---|---|---|
| activity dot (`●`/`○`) in a row | an operation is running **on this row's target** | column 0 of a table (Rule 125) | the registry (`jobs`), or a `MarkBusy` key, read at render; blink phase from the shared tick |
| activity dot in the footer | **work the router tracks is running**, or the view is loading data it asked the router to animate (`WantsBlink()`) | the footer, `components.Status{Activity: true}` (Rules 128, 139) | the same shared tick: the view calls `footer.SetBlinkPhase(msg.Blink)` from its `jobs.FrameMsg` handler |

Every loading state uses the shared dot: `:status` (DEVDESK-369), the viewer (DEVDESK-370), `:oci` (DEVDESK-374: a tab's load, the tag search, the network inspection), `:containers` and `:agent-usage` (DEVDESK-371), `:sec` (DEVDESK-372), `:net` diagnostics, ports and interfaces (DEVDESK-373), and the forge sign-in (DEVDESK-375: `Authenticating...` in the footer, the form stays visible). Two details:

- `:containers` stays silent during `ctrl+r` while rows are on screen: the dot shows only when the table is empty and loading. `:agent-usage` keeps its own loading condition.
- The `dk setup` wizard is its own program, with no router and no `FrameMsg`. It shows a fixed `●` before the pending text (`Checking whether it's running...`), with no animation (DEVDESK-376).

The footer dot has the glyph and colour of the row dots (`indicator.Running`),
A view never draws a spinner in a row: the
per-row spinner chains (`AdvanceSpinner`, `busyTick`, a node spinner) were
deleted by the migrations (DEVDESK-139 to 153) and `StatusColumn` with them
(DEVDESK-143). When the selected row has a footer detail with a glyph, the
footer shows no dot: the row's dot already moves.

The router arms its chain for a footer dot through `jobs.Running() > 0` or
through `WantsBlink()` on the view on screen. A view whose `Activity` status
does not come from a job (the forge explorer's load lines) must implement
`WantsBlink()`, returning `status().Activity`; without it the dot stays fixed on
`●`. Under `app.motion: static` a blink-only need starts no chain, so that dot
is steady.

**Guards** (`internal/app/footer_activity_test.go`, `activity_guard_test.go`):

- `TestActivityGuardFooterHasNoBraille`: each view seeded in
  `footerActivitySeeds` (dashboard, `:jobs`, workspaces, templates, `:sec`, OCI,
  explorer) and in `footerLoadingSeeds` (`:status`, viewer, `:containers`,
  `:agent-usage`, `:sec`, `:net`, forge sign-in, `:oci` loads) renders a footer
  with no braille character and exactly one `●`.
- `TestActivityGuardFrameConsumersAreClassified`: every package under
  `internal/ui` with a `case jobs.FrameMsg` is in `footerFrameConsumers`, either
  with its seeded view (the footer shows the dot) or with an `Exempt` reason.
  No consumer is exempt now: all thirteen packages name a view. The `Exempt`
  field stays for a future case. A new consumer fails until it is classified.
- `TestNoViewImportsTheBubblesSpinner`: no non-test file under `internal/`
  imports `charm.land/bubbles/v2/spinner`, `dk setup` included (DEVDESK-376,
  DEVDESK-377). There is no exception list.
- `TestRemovedSpinnerSymbolsStayGone`: no `Spinner`, `spinnerFrame`,
  `SetSpinnerFrame`, `SpinnerMessage` or `SpinnerStyle` identifier in a file
  under `internal/ui`.
- `TestActivityGuardNoFrameFieldAnywhere`: no identifier named `RenderedFrame`
  under `internal/` (the field no longer exists).

#### Row chains outside the router

No row-animation chain lives in a view: `TestNoRowAnimationChainOutsideTheRouter`
scans every source file outside `ui/datatable/` and fails on a row chain
(`AdvanceSpinner`, `portsSpinnerCmd`, a `tagScanSpinnerIdx` step), with no list
of declared chains to extend. `TestOnlyTheRouterBuildsAFrameMsg` and
`TestTheAnimationChainUnderARealProgram` (a real `tea.Program`, meant for
`-race`) complete the guard rails.

#### What the redraw costs

`DEVDESK_MEASURE_REDRAW=1 go test ./internal/app -run TestMeasureRedrawCost -v`
counts the bytes Bubble Tea's renderer writes to an `io.Writer` on a simulated
120x40 terminal — not a real terminal. Opt-in; the test is skipped otherwise.

| Animation | Bytes emitted |
|---|---|
| no animated row | 0 B/s |
| 1 row blinking at 1 Hz | 376 B/s |
| 20 rows blinking at 1 Hz | 5 809 B/s |
| reference: spinner at 10 Hz, 1 row (historical) | 1 880 B/s |
| reference: spinner at 10 Hz, 20 rows (historical) | 29 049 B/s |

The two "spinner at 10 Hz" lines are historical measurements: the router no
longer runs a 10 Hz chain (DEVDESK-341) and the spinner mode of
`TestMeasureRedrawCost` was removed, so they cannot be reproduced. Nothing
animated costs nothing, and a 1 Hz blink costs about a fifth of the 10 Hz
spinner it was measured against.


### The `:jobs` view — `internal/ui/jobsview`

The reader's side of the registry, and it owns nothing: it starts no work, holds
no timer and fetches nothing. Its rows are the snapshot, so there is nothing to
refresh — `ctrl+r` is absent rather than greyed, because there is no operation
behind it to be unavailable.

Two levels (D2): the runs, and `→` on one of them for its targets. A level
rather than a pair of tabs — the second table is *about* a row of the first, so
it is reached the way every drill-down in the application is (Rule 111), and the
trail is a breadcrumb below the table (Rule 123).

| | Columns |
|---|---|
| runs | activity indicator (untitled, `IconColumnWidth`), Kind, Label, Progress, State, Started |
| targets | activity indicator, Target, State, Detail |

**Column 0 is the activity indicator** (DEVDESK-142, Rule 125), built by
`datatable.IndicatorColumn` over `components.RunIndicator` / `ItemIndicator`
(`job_activity.go`): queued `◴`, running `●`/`○`, done `◉`, failed `⊗`,
cancelled `⊘`, skipped `⊖`. It is the state, not the kind: a jobs list is
scanned for the run that failed and the one still going. The Kind column
carries the state in its `Search` so `/failed` and `/scan` both work while the
glyph column stays out of the filter. The State word takes the dot's colour
(`indicator.Style`), so the row has one colour source; `done` is neutral, not
green, and queued, cancelled and skipped now share that neutral colour.

**The blink phase is read at render.** The rows carry nothing animated:
`runRow`/`itemRow` wrap the record only. `jobs.FrameMsg` calls
`SetBlinkPhase(msg.Blink)` on both tables and the footer; no row is rebuilt (`SetItems` stays the only way rows are rebuilt, from
`ChangedMsg`). A queued run no longer spins: it shows a fixed `◴`.

**The footer says the selected row.** `status()` fills `Status.Detail` with
`components.DescribeRun` (or `DescribeItem` at the target level) of the row
under the cursor, and keeps `N jobs running` as the text. The footer dot is
shown only when no detail is, since the row's own dot moves. A `Warn` (a refused
`K` or `.`) keeps priority over the detail and the detail returns when it
expires. The help ends with `indicator.Legend(indicator.JobsLegend...)`.

**The context is passed, not read.** `jobsview.New(cfg, contextName)` takes the
name from the router, for D68's reason one screen over: the router is what knows
which context is current. The view filters the snapshot on it (`FilterContext`),
so a switch hides runs rather than dropping them — and the router rebuilds every
view on a switch anyway, so the cached name cannot go stale.

`K` is state-aware at both levels (Rule 130) and greyed with a **named** reason:
"This run has already finished", "A delete cannot be stopped once it has
started". The refusal is never silent — pressing a greyed key says why in the
footer.

**The dashboard gained the counter** the same way, and it is the one screen that
always gets D9's degraded form: it launches nothing, so it passes no origin, and
`2 jobs running — :jobs for details` is the whole of what it can honestly say.
The sentence itself is `components.JobsStatusLine`, shared with the workspaces
footer — two copies of a deliberate surrender would each have been free to
surrender differently.

### Handing the terminal to a child — `internal/ui/teaexec`

**The race (Bubble Tea v2.1.1, DEVDESK-290).** Bubble Tea's exec path is
`stopRenderer`, `ExecCommand.Run`, `startRenderer`. `stopRenderer` sends on an
unbuffered channel: it returns when the render goroutine has received the signal,
not when it has finished, and that goroutine then runs `ticker.Stop()` on its
own. `startRenderer` reuses the same ticker (`Reset`). When the child returns at
once (failed start, missing binary), `Reset` can run before the late `Stop()`:
the ticker stays stopped and the screen never repaints while the program keeps
handling input. Nothing in the `ExecCallback` can fix it, because the callback
runs after the race is decided.

**The workaround.** `settled.Run` runs the child, then sleeps for whatever remains
of `settleDelay` (50 ms since `Run` started), also when the child failed, and
returns the child's error unchanged. The old render goroutine has time to stop
the ticker before `startRenderer` resets it. The delay is in `Run`, not in the
callback, for the reason above. A child that lasts 50 ms or more pays nothing;
the worst cost is 50 ms. The delay is probabilistic, but the tests below check
it.

**The API.** `teaexec.Process(*exec.Cmd, tea.ExecCallback)` replaces
`tea.ExecProcess`; `teaexec.Command(tea.ExecCommand, tea.ExecCallback)` replaces
`tea.Exec`. Both return a `tea.Cmd`. `Process` sets a stream only when it is
nil, like Bubble Tea's own `osExecCommand`.

**The rule.** No code outside the package calls `tea.ExecProcess`, `tea.Exec` or
`tea.Suspend`. `source_guard_test.go` (`TestNoDirectTeaExecOutsideTeaexec`) scans
the repository's Go files and fails on a direct call, by file and line; its
helper is unit-tested on positive and negative cases. A comment or an unrelated
`Exec` method does not trip it.

**The tests.** `program_test.go` runs a real `tea.Program` at `GOMAXPROCS(1)`,
where the loss is deterministic. `TestARedrawFollowsAnInstantChild` (20 runs) and
`TestARedrawFollowsAChildThatFailsAtOnce` check that the view is drawn again
after the child. Trap: `tea.WithInput(nil)` makes every exec wait 500 ms before
the child starts, so the waits in those tests are generous.

**The sentinel.** `TestUpstreamTickerRaceStillExists` runs a bare `tea.Exec` and
expects the redraw to be lost. When it fails ("Bubble Tea no longer loses the
renderer ticker"), upstream fixed the race: the wrapper can be removed.
Replace `teaexec.Process` and `teaexec.Command` with the Bubble Tea calls, delete
the package and the source guard, and update this section.

### The activity-column guard (DEVDESK-142, A-16 skeleton)

`internal/app/activity_guard_test.go` keeps every view on the activity column
(Rule 125) and keeps it there. It classifies every `command.ViewType` into one of
two lists, **per table**: a view is migrated or exempt, never both and never
neither. There is no list of tables waiting for their story any more (DEVDESK-143
closed it) and no exception to shrink:

| List | Meaning |
|---|---|
| `migratedTables` | the view's tables are on the activity column (`:jobs`: `jobs/runs`, `jobs/targets`; ws: `ws`; `:sec`: `sec/inventory`; OCI: `oci/images`, `oci/networks`, `oci/volumes`, `oci/registries`, `oci/registry-browser`, `oci/registry-search` (6 tables for `:oci`; the guard also scans `browser_flow.go` and `browser_images.go`); explorer: `explorer/tree`, `explorer/clone-list`; templates: `templates`; netdiag: `netdiag/ports`; `:agent-usage`: `agent-usage/agents`; containers: `containers`) |
| `exemptViews` | views with no activity to show (dashboard, status, git-auth, configuration, about, files, viewer), each with its reason |

A view in neither list, or an exempt view that is also migrated, fails by name (`classificationProblems`, unit-tested on fictitious classifications). A migrated view
exposes `ActivityTables() map[string]datatable.ActivityTable` (a read-only face:
`Shapes()` and `RenderedWidth()`, see `ui-components.md`); an exempt view that reports any table fails with "remove the exception".

The netdiag view's `migratedSources` are two files only, `ports_model.go` and `ports_activity.go` (DEVDESK-150): netdiag migrates its Ports tab alone, and the guard scans the files that belong to it. `ports_commands.go` is left out: its `portsTickCmd` is the data refresh tick, not a row animation (same reasoning as OCI's `commands.go`). The other tabs (`forward_model.go`, checks, interfaces) are not migrated. It reports `netdiag/ports` from `ActivityTables()`, and its footer seed (`seedNetdiagFooter`) opens the Ports tab with one socket whose kill is in flight, driving the view's `Update` directly (no `Cmd` is run: no socket table read, no signal sent).

The `:agent-usage` view's `migratedSources` is the whole `ui/agentusageview/` package (no `tea.Tick` in it: `pollTickCmd` sleeps inside a `Cmd`), it reports `agent-usage/agents` from `ActivityTables()` and implements `WantsBlink()`; its footer seed (`seedAgentUsageFooter`) puts a transcript with a fresh line on disk and expects `indicator.Running`, without running the 2 s poll `Cmd`.

The templates view's `migratedSources` is the whole `ui/templates/` package; it reports `templates` from `ActivityTables()` and has a footer seed (a sync running on the selected row).

The explorer's `migratedSources` is the whole `explorer` package (no frame or `jobFrame` left in it). The explorer reports `explorer/clone-list` from `ActivityTables()` even with no clone running (same constructor), so the table is checked at 60 columns. The registry side adds `jobs.SafeCause` (credentials removed from a cause) and `jobs.CancelledDetail`.

**Checked at run time**, on each view built by the router: column 0 of every
migrated table is the indicator (untitled, fixed, `IconColumnWidth`, no `Less`,
no `Search`); `RenderedWidth()` equals `width-2` after a 60-column resize
(Rule 116); the help holds the `indicator.Legend` section.

**Checked by source analysis**, on migrated packages only (`ui/jobsview/`, `ui/workspaces/`, `ui/security/`): no
`AdvanceSpinner` call, no `StatusColumn:` key, no `Frame`/`frame`/`SpinnerFrame`
field or selector (`RenderedFrame` is banned too, everywhere under `internal/`: `TestActivityGuardNoFrameFieldAnywhere`), no string or rune literal
holding an indicator glyph. Verified to fail on a positive case by injecting a `Frame`
field, an `AdvanceSpinner` call and a `"◉"` literal into `jobsview`.

**The footer half** is `migratedFooterSeeds` (a view seeds a row at a state and the
guard checks its footer): `:sec` is seeded by `seedSecurityFooter`, a running
scan on the selected row.

## Bubble Tea Message Flow

Custom messages defined in view models (e.g., `internal/ui/status/model.go`):
```go
type TickMsg time.Time           // Countdown timer
type CheckStartedMsg struct{}    // Check initiated
type CheckCompleteMsg struct{}   // Check results ready
```

Commands return these messages to trigger async operations. The Bubble Tea `Update()` method handles them.

