# TUI — Layout & Navigation

### Rule 101 : TUI elements must have global consistency

- modal windows must follow the same layout
- buttons must be the same everywhere in the application
- keyboard keys must be consistent — where possible, the same functionality is assigned to the same key.
  Example: Enter = confirm, Esc = cancel, Space = toggle, etc.

### Rule 107 : Visual state indicators

- Active fields: `▸` indicator or a distinct style
- Focused buttons: bold + colored background
- Status states: use predefined styles (StatusOKStyle, StatusDownStyle, etc.)

### Rule 108 : Responsive design (terminals)

- All components must handle `tea.WindowSizeMsg`
- Store width/height in the model
- Adapt the display to the available dimensions

### Rule 111 : Standard keybindings

**No bare letter is navigation.** The vim aliases `h j k l g G` were removed
entirely (§3.26), including in shared components and in `bubbles/viewport`'s
default `KeyMap`. A letter belongs to the vocabulary of actions —
`internal/ui/keymap`, where the rule is declared and checked against the code
by `TestNoBareLetterIsNavigation`.

**`g` came back in the viewer, and that is not a bending of the rule**
(§3.53): there it opens a prompt, and the jump takes an **argument** — which
`home` and `end` do not cover and never will. What the rule forbids is a
letter *in place of* a structural key, not a letter that does what none of
them do. It was therefore removed from `retiredAliases`, its reason written
there; `j` and `k` stay in it, since they are only `down` and `up` under
another name. A letter that regains a meaning leaves the list of those that
no longer have one, otherwise the list would lie — that is §3.47 for `H`,
taken in the other direction.

What this buys is not space (the gain was concentrated on `l`, which carried
four meanings) but a checkable rule: keeping `j`/`k` would leave an
exception, and exceptions are what produced the survey's 16 collisions. The
cost is accepted — k9s, lazygit, and btop all keep `hjkl` — and it is paid
once.

- **Navigation (liste/table)**:
  - `↑ / ↓`: Move up/down in the list.
  - `PageUp / PageDown`: Scroll by page.
  - `Home`: Go to top.
  - `End`: Go to bottom.
- **Navigation (tabs & drill-down)**:
  - `Tab / Shift+Tab`: Move between breadcrumb tabs.
  - `←`: Go back to parent level (drill up).
  - `→`: Enter selected group/directory (drill down).
  - `Enter`: Select/confirm item (visible only in selection mode, e.g. when browsing from security view).
  - `Esc`: Go back to parent level, or cancel/close modal.
- **Resource Actions** — an **uppercase** letter, always, and its meaning is
  the same everywhere. The full vocabulary is declared in `internal/ui/keymap`,
  and `TestNoViewBindsAnUndeclaredUppercaseKey` scans the sources to verify
  that no view strays from it. The free letters are listed there (`H J Q Z`):
  a new action uses one from there rather than inventing a key. `H` came back
  there with §3.47: it used to trace the route, and the trace was removed
  because it answered for the Docker VM rather than for the machine (D57). A
  letter an action frees up is re-declared free, otherwise it stays reserved
  for a use that no longer exists.

  | | | | |
  |---|---|---|---|
  | `N` Create | `E` Edit | `D` Delete | `M` Rename (*mv*) |
  | `S` Scan | `A` Scan all | `F` Sync | `C` Clone selection |
  | `T` Terminal | `O` IDE | `W` Browser | `L` Logs |
  | `V` Pager | `K` Stop / kill | `P` Prune | `B` Registry browser |
  | `G` Pull (*get*) | `U` Login / logout | `X` Exclude | `R` MR · PR |
  | `I` Issues | `Y` Copy path | | |

- **Lowercase letters** — a filter or a display toggle, never an action. It
  changes nothing, so its meaning is **local**, and two views can use the same
  letter: `l` is the protocol in netdiag and the LOW severity in security.
  "Local" means **declared**: each surface lists its keys in
  `keymap.localToggles`, and `TestEveryLowercaseBindingIsDeclared` rejects
  ones that are not there.
  - `r` `p` `s` `t` `z` (containers): state filters, **cumulative**, plus the
    reset. Nothing active means `running` — that is the view's resting state,
    so it opens **with no bar**, and `z` brings it back there: arriving and
    pressing `z` produce the same screen. "All" is the four tokens together;
    there is no `all` token, that would be a fifth state to select alongside
    four real ones. That is the difference from netdiag/Ports, where nothing
    active means "no opinion" and shows everything.
  - `f` `c` `w` `v` `t` `n` `g` `s` (viewer): display, coloring, line
    wrapping, verbosity, timestamps, line numbers, go-to-line, search case
    sensitivity.
  - `t` `u` `l` `e` `n` `z` (netdiag/Ports): protocol and state filters.
  - `r` (OCI browser): registry shown.
  - `c` `h` `m` `l` (security): severities, **cumulative** — `c`+`h` asks for
    "CRITICAL **or** HIGH", which a single threshold cannot express.

- **Two exceptions, declared** in `keymap.DeclaredExceptions()`: `o` (open the
  pipeline the forge resolves, security/results/ci-tab) and `ctrl+y` (copy
  the `docker run` command). Burning a global uppercase letter for an action
  present in a single sub-screen would cost more than it gains. They are
  written up as exceptions so the next survey does not mistake them for
  drift.

- **A modal is a fourth space**, set apart by *mode* rather than by case: it
  claims every key before the view sees it, so its `y`/`n` never collides
  with any action.

- **Document viewer** (opened from another view; `esc` returns there):
  - `f`: Toggles between the document as it is and the **one** view its kind
    derives — the tree for JSON and XML, the rendered form for Markdown. A
    kind never derives two, so the key is never ambiguous; for a kind that
    derives none, it is hidden (Rule 130).
  - `c`: Syntax coloring on/off. The reasoning changed with §3.26: it used to
    invoke `h`/`l` as the reserved aliases of `←`/`→`, which ruled out a
    *highlight* key on `h` — those aliases no longer exist, so that argument
    fell away. `c` stays because *coloring* is a better landmark than
    *highlight* anyway, and moving a key to chase a pattern that was removed
    would just be noise. **`c` is orthogonal to `f`**: turning off the color
    of a rendered Markdown does not bring its markers back — rendering is one
    display, coloring is another, and a `c` that revealed the markers would
    be a second path to the same screen.
  - `w`: Soft wrap (text display).
  - `v`: Cycle the minimum log level shown (logs only).
  - `/`: Search — filters **and** highlights. Lines with no match disappear
    and every match in the remaining lines is highlighted: that is what says
    *where* in a long line. The highlighting does not depend on `c` (a match
    is not syntax coloring), survives `w`, and in a log the level keeps the
    rest of the line.
  - `s`: Whether case matters. Off by default, an `Aa` token in the bar when
    it is on (Rule 136). It applies to the query **already entered**, without
    retyping it: comparing the two readings is the point of pressing it. It
    is a parameter of the single computation that decides both the filter
    *and* the highlighting — a second reading of the flag could only be a way
    to make them diverge.
  - `n`: Line numbers, in a left-hand gutter. These are the **document's**:
    under a filter they keep their gaps, which is the only reading that lets
    a line be cited by its number. A wrapped line numbers its first row and
    leaves the others blank. The gutter is not part of the searched text, and
    it is removed from the width *before* wrapping.
  - `g`: Go to a line. The prompt is a **mode** — it takes every key before
    the panel does, so a digit does not also scroll and `esc` closes it
    instead of leaving the view — and it occupies the filter bar's slot, so
    the footer height does not change. An out-of-range number is refused, and
    it says so; a line hidden by the filter as well, **and nothing moves**:
    jumping elsewhere while showing a different number would be doing
    something adjacent in silence.
  - `ctrl+r` / `F`: Reread once / reread in a loop. `F` is a **toggle**, and
    following happens **inside the viewport** — the followed document stays
    pinned to the bottom. It used to open `docker logs -f` via
    `tea.ExecProcess`, which you only leave via ctrl+c: the suspended TUI does
    not intercept that, so it killed the application and left the terminal in
    the child process's mode.
  - `V`: Open in the system pager (the one surviving pager path, container logs
    only). It is the one that genuinely *streams*, and you leave it via `q`.
- **Control**:
  - `Enter`: Validate, Execute, or Open.
  - `Esc`: Close modal, cancel, or go back.
  - `Space`: Toggle, Select, or Pause/Resume.
- **Sorting**:
  - `.` (dot): Cycle sort column. This is the only sort control; the "sorting
    menu" on `Shift+S` this rule used to announce never existed in the code,
    and `S` belongs to the vocabulary of actions.
- **Global**:
  - `ctrl+p`: Open command mode — from anywhere, including a focused field.
    Replaces `alt+:`, which did not reach Terminal.app or iTerm2 (Option is
    not Meta there by default) even though it was the only path that crossed
    a field. See `internal/ui/keymap.CommandMode`.
  - `:`: Open command mode, except while editing — there it is a character,
    which values like `https://trivy-server:4954` need.
  - `/`: Search/Filter.
  - `?`: Open help menu.
  - `q`: Quit view/app.
  - `ctrl+r`: **Refresh, and nothing else.** It used to also mean "go back"
    in security and netdiag results, where `esc` is enough.

**Only three `Ctrl` combinations remain**, and `TestOnlyThreeCtrlCombinationsSurvive`
verifies it: `ctrl+c` (SIGINT), `ctrl+r` (refresh), and `ctrl+p` (the command
line). The budget is roughly fourteen keys and each one drags a constraint —
`ctrl+a` is screen's prefix, `ctrl+b` is tmux's, `ctrl+s`/`ctrl+q` are flow
control, `ctrl+i`/`ctrl+m`/`ctrl+j`/`ctrl+h` are TAB, Enter, LF, and
Backspace. An action that resettled behind `Ctrl` would be reclaiming a spot
`Shift` gives away for free.

`Ctrl+Shift` is not an option: the control code erases case, so `ctrl+a` and
`ctrl+shift+a` both emit 0x01. Telling them apart requires the Kitty keyboard
protocol or `modifyOtherKeys`, which bubbletea v1.3.10 does not enable — and
even then the terminal emulator claims some first (`ctrl+shift+c/v/t/w/n`).

### Rule 112 : Forms in the viewport (no modals)

**Creation/edit forms must be displayed in the main viewport, not as a modal.**

Modals are reserved for confirmations and short messages.

```go
// In View() — display priority
func (m Model) View() string {
    if m.creationForm != nil { return m.creationForm.View() }
    if m.confirmModal != nil {
        return lipgloss.Place(m.width, m.height,
            lipgloss.Center, lipgloss.Center, m.confirmModal.View())
    }
    return m.renderNormalView()
}
```

| Type | Display | Examples |
|------|-----------|----------|
| **Form** | Full viewport | Group/project creation, adding a monitor, editing |
| **Confirmation** | Centered modal | Deletion, dangerous actions |
| **Information** | Centered modal | Reports, detailed error messages |

### Rule 123 : Placement of the tab row attached to a table

**The tab row must sit immediately below the table, always visible (never scrolled).**

```
┌─────────────────────────────────┐
│  ┌───────────────────────────┐  │
│  │   TABLE (viewport)        │  │
│  └───────────────────────────┘  │  ← bottom edge of the viewport
│  [tab1]  [tab2 active]  [tab3]  │  ← tab row, always visible
│  content of the active tab...   │
└─────────────────────────────────┘
```

**Two modes:**
- **Navigation**: Tab/Shift+Tab or ←/→ to switch tabs, active tab in `ColorSecondary`
- **Breadcrumb**: no navigation, active one highlighted in `ColorSecondary`, parents in `ColorDim`

**Mandatory left padding** (1 character to align with the viewport):
```go
// ✅ CORRECT
return theme.PadWithBg(theme.Bg(" ") + theme.RenderTabs(tabs, activeIdx), width)
// ❌ WRONG: tabs stuck to the left edge
return theme.PadWithBg(theme.RenderTabs(tabs, activeIdx), width)
```

Forbidden:
- ❌ Tabs ABOVE the table
- ❌ Tabs INSIDE the viewport (would get scrolled)
- ❌ Tab navigation for a breadcrumb

### Rule 124 : General application layout

Strict vertical structure (top to bottom):

1. **Header** (fixed): 7 lines
2. **Blank line**: 1 line
3. **Command line**: 1 line
4. **Viewport** (dynamic): `NormalBorder` border, takes the remaining space
5. **Footer** (fixed):
   - **Without tabs**: blank line (1 line) + info line (1 line) = **2 lines**
   - **With tabs**: tab row (1 line) + blank line (1 line) + info line (1 line) = **3 lines**

Footer rules:
- **Mandatory blank line** between the viewport and the footer's first line (info line or tab row)
- Info line always rendered, even when empty
- Text horizontally centered (`lipgloss.Center`)
- `ColorHighlight` color (except errors → `StatusErrorStyle`)
- **Mandatory blank line** between the tab row and the info line

```go
// Without tabs: blank line + info line
footerHeight := 2
// With tabs: tab row + blank line + info line
if showTabs { footerHeight = 3 }
viewportHeight := windowHeight - 7 - 1 - 1 - footerHeight
```

Footer rendering:
```go
// Without tabs
return theme.EmptyLineBg(width) + "\n" + infoLine

// With tabs
return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
```

### Rule 130 : a shortcut with nothing to act on is greyed out, not removed

**`GetShortcuts()` reflects the current state of the view and of the
selected row. What varies is `Disabled`, not whether the entry is present.**

#### Mode versus state

This is the distinction that decides between the two, and it holds for every
view:

| | What changes | Why |
|---|---|---|
| **Mode** — form, confirmation, selection | the **entire list** is replaced | it is not the same vocabulary; greying out `enter → Create` while inside a table would show the union of every mode |
| **State** within a mode — selected row, missing tool | the entry **stays**, `Disabled: true` | the column is read out of the corner of the eye, and a list that reorganizes itself under the reader's gaze stops being readable |

Two causes of greying out, and only two: the state of the **selected row**,
and a **global** unavailability (a tool the machine does not have). An
operation in progress is not one of these: it changes on every tick, the row
already says so with its spinner, and a blinking entry says the opposite of
what this rule is after.

#### Rendering

`shortcut.Shortcut.Disabled` is the only mechanism; it is implemented once,
in `Shortcuts.ToStrings()`. The key loses its color and its weight
(`theme.ShortcutKeyDisabledStyle`, an alias of `ColorDim`), the description
does not change — it is already `ColorDim`, so the line becomes a uniform
grey. `maxLenKey()` counts disabled entries: alignment must not depend on
what is available, otherwise the column shifts anyway.

A greyed-out entry **keeps the label of the action it would perform**: a
blank in a column where every other row reads fine would be worse than the
word.

#### One computation, two readers

The reason is computed once and read by both halves of the view: the header
to grey out, the handler to refuse. `shortcut.Availability` carries **a
single field** — an empty reason means available — so the boolean and the
reason can never diverge.

```go
// shortcut.Availability, shared by every view
type Availability struct{ Reason string }
func (a Availability) Enabled() bool { return a.Reason == "" }
func Unavailable(reason string) Availability

// GetShortcuts
{Key: "S", Description: "Scan", Disabled: !a.Scan.Enabled()},

// le handler
case keymap.Scan:
    return m.guard(a.Scan, m.startSecurityScan)
```

The reasons are **named constants** of the view's package (`reasonNoScanner`,
`reasonInsideGroup`, …): the header, the footer, and the tests all read them
from the same place, so none of them can drift on the wording.

#### An action justifies itself, a control does not

The footer refusal applies to an **action** — the uppercase vocabulary,
`enter`, `→`. For a key whose applicability is **structural** — `←→` on a
field that is not a cycle field, `space` on something that is not a
checkbox, `tab` when there is only one tab — greying out is enough: there is
nothing to explain, and a footer line for every lost arrow key in a form
would be noise.

**Grey says "not now," the pressed key says why.** The header has no room to
carry a reason; the footer does, and it is a `Warn` in the sense of Rule
128 — the action cannot be honored as requested, nothing failed. The refusal
is never silent: that is exactly what the `return m, nil` this rule replaces
used to do.

**Not knowing is not knowing it is a no.** An availability that arrives via
a `Cmd` leaves the action offered as long as the answer is not in yet:
greying out only to un-grey three frames later reads like a glitch.

**The header answers what the application can *attempt*, the footer what the
system answered.** `K` in `net`/Ports is greyed out when the socket carries
no PID — there is nothing to report, and it is known before the keypress. It
stays lit on a row whose kill the OS will refuse: knowing that would require
trying it, and greying it out based on a guess about permissions would lie
in the other direction. The failure is then classified and named (§3.49),
never reduced to "Failed".

**A key that applies regardless of the row does not enter this machinery.**
`N` creates a directory in the directory being browsed — it does not act on
the selection, so hiding it said "not applicable" about an action that
worked, and greying it out would repeat that.

#### Where the line falls, view by view

All views have migrated (§3.48). What stays hidden is hidden because the
screen changes:

| Still replaces the list | Greyed out |
|---|---|
| a mode: form, confirmation, modal, selection | the selected row: `enter`, `W`, `S`, `F`, `X`, `o`, `→` |
| a tab, a view state (inventory / results / details), a search holding the keyboard | a tab inside the same screen: `X` outside the Secrets tab |
| a disconnected screen, a `docker pull` in progress | a global unavailability: the scanners, a session |
| the document's **kind** in the viewer — a Markdown has no verbosity, and never will | a load in flight: the table is the same screen on both sides of it |
| | a form's focused field: `←→`, `space`, `enter` |

The reference implementations are `internal/ui/workspaces/availability.go`
and `internal/ui/oci_resources/availability.go`. The test helpers are
`testutil.ShortcutDisabled`, `ShortcutEnabled`, `HasShortcut`, and
`ShortcutKeys` — the last one serves the test every view must have: **the
set of keys does not change from one state to another within the same
screen.**

Forbidden:
- ❌ Hiding an entry because the action does not apply to the row
- ❌ Greying out a key that still acts, or refusing one that is not greyed out
- ❌ Refusing silently — a `return m, nil` with no reason given at the footer
- ❌ Two computations for one question: one for display, one for the handler
- ❌ A static list that ignores the selected row's type

### Rule 134 : Keyboard shortcuts belong to the header, never to the viewport

**Never embed `[key] action` text in the viewport's content.**

The header already shows every shortcut via `GetShortcuts()` (Rule 130). Duplicating this information in the viewport creates visual noise, reduces usable space, and lets the two sources drift apart on updates.

| Forbidden (in viewport) | Correct (in header) |
|--------------------------|----------------------|
| `[↑↓/jk] navigate  [c] filter  [esc] close` | `GetShortcuts()` returns `{Key: "↑↓/jk", Description: "Navigate"}`, etc. |
| `[↑↓/jk] scroll  [g/G] top/bottom  [enter] new test` | `GetShortcuts()`'s results state returns the scroll shortcuts |
| `Press Enter to confirm` at the bottom of a form | `GetShortcuts()` returns `{Key: "enter", Description: "Confirm"}` |

**Mandatory pattern:**

```go
// ❌ WRONG — inline help in the viewport
func (f *Form) View() string {
    lines = append(lines,
        theme.EmptyLineBg(w),
        theme.PadWithBg(theme.HelpStyle.Render("  [↑↓/jk] scroll  [enter] confirm  [esc] back"), w),
    )
    return strings.Join(lines, "\n")
}

// ✅ CORRECT — viewport with no inline help, GetShortcuts() handles everything
func (f *Form) View() string {
    // content only, no help line
    return strings.Join(lines, "\n")
}

// Dans la vue parente (view.go)
func (m Model) GetShortcuts() shortcut.Shortcuts {
    if m.myForm != nil {
        if m.myForm.state == stateResults {
            return []shortcut.Shortcut{
                {Key: "↑↓/jk", Description: "Scroll"},
                {Key: "enter", Description: "Run new test"},
                {Key: "esc", Description: "Go back"},
            }
        }
        return []shortcut.Shortcut{
            {Key: "tab", Description: "Next field"},
            {Key: "enter", Description: "Confirm"},
            {Key: "esc", Description: "Go back"},
        }
    }
    // ...
}
```

**Consequences:**
- The viewport gains usable height (removal of 1–2 fixed lines)
- `resultVisibleLines()` / `viewportOverhead` must be updated accordingly
- `GetShortcuts()` must be state-aware (Rule 130) to reflect the shortcuts available depending on the current state

Forbidden:
- ❌ `theme.HelpStyle.Render("  [key] action  [key] action")` in the rendering of a form or a list
- ❌ A static help line at the bottom of the viewport
- ❌ `GetShortcuts()` not updated when the state changes (e.g. results vs. input)

### Rule 137 : Shortcut description format

**All shortcut descriptions must start with a capital letter and use an imperative verb (action form).**

| ❌ Wrong | ✅ Correct |
|---------|-----------|
| `"scroll"` | `"Scroll"` |
| `"back"` | `"Go back"` |
| `"confirm"` | `"Confirm"` |
| `"new test"` | `"Run new test"` |
| `"next field"` | `"Next field"` |
| `"navigate"` | `"Navigate"` |
| `"quit"` | `"Quit"` |

Applies to all `shortcut.Shortcut{Key: "...", Description: "..."}` definitions across all views.

### Rule 138 : Only non-obvious shortcuts in GetShortcuts()

**`GetShortcuts()` must only surface shortcuts that are not self-evident to the user. Omit universally-known navigation shortcuts.**

#### Always omit from GetShortcuts()

| Shortcut | Reason |
|----------|--------|
| `↑ / ↓` or `j / k` | Universal list navigation — obvious |
| `j / k` alone | Vim aliases — redundant alongside arrow keys |
| `PageUp / PageDown` | Universal scrolling — obvious |
| `Tab / Shift+Tab` | Use `Tab` only, drop `Shift+Tab` — direction is implied |
| `g / G`, `Home / End` | Universal top/bottom — obvious |

#### Key display rules

- Show only **one** key when a shortcut has an alias: prefer the primary key
  - ✅ `↑↓` (not `↑↓ / jk`)
  - ✅ `tab` (not `tab / shift+tab`)
  - ✅ `←→` (not `←→ / hl`)

#### What to show

Only include shortcuts that are **specific to the current view or state**, and that a user cannot reasonably guess:

```go
// ✅ CORRECT — only non-obvious, context-specific shortcuts
return []shortcut.Shortcut{
    {Key: "ctrl+n", Description: "New resource"},
    {Key: "ctrl+s",  Description: "Scan"},
    {Key: "ctrl+d",  Description: "Delete"},
    {Key: "enter",   Description: "Open details"},
    {Key: "/",       Description: "Filter"},
    {Key: "?",       Description: "Help"},
}

// ❌ WRONG — cluttered with obvious navigation
return []shortcut.Shortcut{
    {Key: "↑↓/jk",       Description: "Navigate"},
    {Key: "pgup/pgdown",  Description: "Scroll page"},
    {Key: "tab/shift+tab", Description: "Switch tab"},
    {Key: "ctrl+n",        Description: "New resource"},
}
```
