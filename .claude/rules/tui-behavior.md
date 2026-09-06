# TUI — Bubble Tea behavior & architecture

### Rule 109 : Bubble Tea message naming convention

- Format: `[ComponentName][Action]Msg`
- Examples: `ComponentFormSubmitMsg`, `ConfirmModalYesMsg`
- Always document with a comment
- Group related messages together in the file

### Rule 110 : NEVER modify the model inside a Cmd ⚠️

**Fundamental principle of Bubble Tea (Elm architecture).**

`Update()`, `View()`, and `Cmd`s execute in parallel. Modifying the model inside a Cmd creates race conditions detectable with `go run -race`.

| Component | Role | Can modify the model? |
|-----------|------|---------------------------|
| `Update()` | Processes messages | ✅ YES (the only allowed place) |
| `View()` | Renders the UI | ❌ NO (read-only) |
| `Cmd` | Async I/O operations | ❌ NO (returns messages) |

```go
// ❌ WRONG — race condition
func (m *Model) logout() tea.Cmd {
    return func() tea.Msg {
        m.authenticated = false  // DANGER
        return nil
    }
}

// ✅ CORRECT — message + Update()
type LogoutCompleteMsg struct{}

func (m *Model) logout() tea.Cmd {
    url := m.urlInput.Value()  // copy the data we need
    return func() tea.Msg {
        auth := gitlabpkg.NewAuth(m.storage)
        _ = auth.Logout(url)
        return LogoutCompleteMsg{}
    }
}

case LogoutCompleteMsg:
    m.authenticated = false  // thread-safe inside Update()
    m.user = nil
    return m, nil
```

**Golden rule**: Cmds do the "dirty work" (I/O), then send a message to `Update()`.

### Rule 126 : Scan cache behavior (Images & Workspaces)

| Action | Disk cache | Memory cache | Scan triggered |
|--------|-------------|---------------|------------|
| `Enter` (scanned item) | Read | Read | No |
| `Enter` (not scanned) | — | — | No |
| `ctrl+s` (in progress) | Blocked | Blocked | No |
| `ctrl+s` (available) | Overwritten | Updated | Yes (item only) |
| `A` (unscanned) | Unchanged | Unchanged | Yes (unscanned) |
| `ctrl+a` (all) | **Purged** | **Purged** | Yes (all) |

**`ctrl+a` must clear the cache before relaunching the scans:**
```go
// ✅ CORRECT
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
    var keys []string
    for _, img := range m.images {
        delete(m.scanCache, name)  // in-memory purge inside Update()
        keys = append(keys, name)
    }
    return m, tea.Batch(deleteScanCacheCmd(keys), batchScanCmd(keys, m.defaultScanOpts()))
}
```

### Rule 128 : Footer messages — three levels, centered, one component

**`components.FooterMessage` is the only implementation.** A view does not
render its own message: it declares one, sets it from `Update()`, and
renders it via `m.footer.View(width, status)`.

There used to be eight of them, one per view, each with its own fields,
timer, and lipgloss block. That is what produced the flaw this component
removes: nobody ever centered the error branch, in any of the eight, so
errors were left-aligned everywhere while notices were centered.

#### The three levels

They are defined by **what happened**, not by how it feels:

| Level | Meaning | Color |
|--------|------|---------|
| `Error` | an operation failed, or the system refused it | `ColorFooterError` = the red of **CRITICAL** CVEs |
| `Warn` | the action cannot be honored as requested, but nothing failed: an unmet precondition, already in progress, not applicable here | `ColorFooterWarn` = the orange of **MEDIUM** CVEs |
| `Info` | a neutral fact, or a successful operation | `ColorFooterInfo` = `ColorText`, the ordinary text color |

Info is not bold, the other two are: the hierarchy runs through weight as
much as through hue, and a neutral info in bold would turn back into an
alert.

The colors are **semantic aliases** assigned in `ApplyTheme` (`colors.go`),
like the viewer's syntax colors: no theme file gains a key. They target the
**severity** names rather than `ColorError` / `ColorWarn` — the default
theme makes them coincide, so the choice is invisible today; it stops being
invisible in a theme that separates them.

#### What is forbidden

- ❌ `theme.StatusErrorStyle`, `StatusOKStyle`, `StatusWarningStyle`, or
  `ColorHighlight` inside a `RenderFooter`, a `renderInfoLine`, or a
  `renderInfoText` — `TestNoViewStylesItsOwnFooterMessage` scans the sources
  and fails, naming the file, line, and function.
- ❌ **Green** in a footer. It is reserved for status icons (Rule 121).
- ❌ A **left-aligned** message. `View` centers, always, across the full
  width.
- ❌ A local timer (`clearFooterCmd`, `clearInfoMsgCmd`, …): the component
  carries it.

#### Mandatory pattern

```go
// 1. The model declares a field.
type Model struct {
    footer sharedcomponents.FooterMessage
}

// 2. Update() sets the message and returns its timer. A message set without
//    its timer never disappears.
case SomeErrorMsg:
    log.Printf("ERROR [package/view] action: %v", msg.Err)
    return m, m.footer.Error("Failed to load data — check logs")

case ScanAlreadyRunningMsg:
    return m, m.footer.Warn("Scan already in progress")

// 3. Update() offers unhandled messages to the component, which consumes
//    the expiry addressed to it.
m.footer.Handle(msg)
return m, nil

// 4. RenderFooter() renders the line, blank included — Rule 124 budgets it.
func (m Model) RenderFooter(width int) string {
    return theme.EmptyLineBg(width) + "
" + m.footer.View(width, m.status())
}
```

**The expiry carries an id** (`ClearFooterMsg{ID}`): a stale timer does not
clear the message that took its predecessor's place. That is what makes the
type shareable across packages, and it fixes, in passing, a flaw all eight
implementations shared — a message set at t+2.9s was cleared at t+3s by the
previous one's timer.

#### `Status` — what has no timer

A progress indicator, a hint, a loading state: these are **states**, not
events. They are derived on every frame and passed as `View`'s second
argument, never set as a message — a line set when the first repo starts
would be cleared while the tenth is still running.

```go
func (m Model) status() sharedcomponents.Status {
    if m.loading && len(m.table.Items()) == 0 {
        return sharedcomponents.Status{Text: "Loading images...", Spinner: true}
    }
    return sharedcomponents.Status{Text: m.actionLine()}
}
```

Precedence in `View`: **error → warning → info → status**. A failure the
user has not read takes priority over the progress of whatever is still
running.

`Spinner: true` prefixes the current frame, which the view pushes from its
`spinner.TickMsg` handler via `m.footer.SetSpinnerFrame(m.spinner.View())`.
This is the **rendered** spinner, not a raw frame: each view already gives
its spinner the `theme.SpinnerStyle()` style, and restyling it would nest
one escape sequence inside another. The measurement goes through
`lipgloss.Width`, which ignores escape sequences — the opposite of the rule
for a table cell (Rule 122), and the difference comes down to who does the
measuring.

#### A table's loading state belongs to the footer

**When a `datatable` is loading, the table stays on screen** and the
loading state is an info message with a spinner, in the footer only. A body
that replaces itself with a spinner loses its header and columns for the
duration of every `ctrl+r`, then regains them: a layout jump on every
refresh.

Mandatory consequence: the empty message ("No images found") is
**conditioned on loading being finished**, otherwise the table announces the
absence of what it is still looking for.

`TestNoTableViewRendersALoadingBody` rejects a `theme.SpinnerMessage` in the
views it covers. The exceptions — an operation screen with no table behind
it — are **declared** in the test, the same way as
`keymap.DeclaredExceptions()`.

#### Tests — the timer really does sleep

`tea.Tick` blocks for its entire duration, and `testutil.Msgs` runs the
whole batch it is given: a test that inspects a `Cmd` carrying the timer
pays the full three seconds.

- To verify that a message **disappears**, build the expiry rather than
  running the `Cmd`:
  `feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})`.
- For a test that must drain the `Cmd` (because it is looking for another
  one inside it), shorten the timer:
  `testutil.FastTimers(t, &components.FooterMsgDuration)`.

`FooterMsgDuration` is an exported `var` for this reason alone; its
production value is fixed by `TestAMessageGetsThreeSeconds`.

#### Other prohibitions

Mandatory log format: `log.Printf("ERROR [package/view] action: %v", err)`

- ❌ `err.Error()` directly in the UI (except a validation message already
  written to be read)
- ❌ Replacing the view with an error screen (except fatal initialization
  errors)
- ❌ Ignoring an error without `log.Printf`
