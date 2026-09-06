# TUI — Forms & Focus

### Rule 103 : Standardized form navigation

- `↑ / ↓` to navigate between a form's fields
- A visual indicator (`theme.IconCircleSmall`) must show the focused element
- `Enter` to validate action buttons only (see Rule 135)

### Rule 104 : Modal window structure

- Rounded border (NormalBorder) with color appropriate to the context
- Title at the top using `theme.TitleStyle`
- Message in the center
- Buttons at the bottom with consistent spacing
- Help instructions with `theme.HelpStyle` at the very bottom
- Safe choice by default (e.g. "No" for delete confirmations)

### Rule 113 : Form field spacing

| Layout | Separator after the field |
|--------|--------------------------|
| **Multi-line**: label ↵ content | `\n\n` (blank line) |
| **Single-line**: label + content on the same line | `\n` |
| **Checkboxes** | `\n` between them, `\n\n` after the group |

`renderField()` functions must **NOT** include the trailing separator.

### Rule 114 : Built-in help system (`?`)

**Every view implements `help.Provider` via `GetHelpContent()`, always in English.**

Obligations:
- Mandatory update in the same commit whenever a feature / keybinding changes
- Every new view must implement `help.Provider` with complete content

Expected content: `Title`, `Description`, `KeyBindings`, `Sections`

Files:
- `internal/ui/help/help.go` — component and `Provider` interface
- `internal/ui/*/view.go` or `model.go` — per-view implementation

### Rule 120 : Focus indicator and form field rendering

**Indentation: 2 characters.** When focused, `theme.IconCircleSmall` sits at column 0.

| State | Prefix |
|------|---------|
| Focused | `IconCircleSmall + " "` → `● Label` |
| Not focused | `"  "` (2 spaces) → `  Label` |

`theme.IconCircleSmall` (`\ueb8a`) does NOT contain a space — always add `" "` after it.

**Label/value separator**: `theme.IconChevronRight` (`\uf054`) + `" "`. **No `:`**.

**A space before the chevron is mandatory**: there must always be a space between the label text and `IconChevronRight`.

```go
// ✅ Single-line, focused
theme.KeyStyle.Render(theme.IconCircleSmall + " Server " + theme.IconChevronRight + " ") + input.View()

// ✅ Multi-ligne
theme.KeyStyle.Render(theme.IconCircleSmall + " Name " + theme.IconChevronRight) + "\n"
theme.Bg("  ") + input.View()

// ❌ WRONG — no space before the chevron
theme.KeyStyle.Render(theme.IconCircleSmall + " Name" + theme.IconChevronRight) + "\n"
theme.KeyStyle.Render("▸ Server: ") + input.View()
```

**Selection lists**: `IconCircleSmall` for the selected item, `"  "` for the others.

**Focus color**: `theme.ColorHighlight` for all field types.

**TextInput cursor**: configure via `theme.StyleTextInput(&myInput)` (sets `Prompt = ""`).

**Checkboxes**: via `theme.RenderCheckbox()`.

**There are no radio buttons.** For a closed set of values, the control is
the `←→` cycle field (Rule 132) — regardless of how many values there are,
even two. `theme.RenderRadioButton()` was removed along with its last two
callers (§3.9 of the backlog); recreating it locally is forbidden.

Forbidden:
- ❌ `▸` as a focus indicator (use `theme.IconCircleSmall`)
- ❌ `:` as the label/value separator (use `theme.IconChevronRight`)
- ❌ `ColorPrimary` for focused elements (use `ColorHighlight`)
- ❌ Recreating `RenderCheckbox` locally
- ❌ Reintroducing radio buttons, in any form

### Rule 131 : Top padding for forms in the viewport

**Every form displayed in the main viewport must begin with exactly one blank line (1-line top padding).**

```go
// ✅ CORRECT — a single blank line
func (f *Form) View() string {
    return theme.EmptyLineBg(f.width) + "\n" +
        f.renderFields()
}

// ❌ WRONG — two blank lines (EmptyLineBg + "\n\n")
func (f *Form) View() string {
    return theme.EmptyLineBg(f.width) + "\n\n" + f.renderFields()
}

// ❌ WRONG — form stuck to the viewport's top edge
func (f *Form) View() string {
    return f.renderFields()
}
```

Does not apply to modals (positioned via `lipgloss.Place`).

### Rule 132 : Closed-list fields (value cycling)

**Any field whose value belongs to a finite set must use the ←→ cycle pattern.**

It is the **only** control allowed for a closed set. Radio buttons are not
an alternative for two or three values: they no longer exist in the
application (Rule 120). Checkboxes remain for independent booleans, which is
a different thing from an exclusive choice.

#### Visual

```
  Visibility  󰅂 private        ← not focused
● Visibility  󰅂 internal       ← focused (KeyStyle)
```

Line structure:

| Part | Value |
|--------|--------|
| Focus indicator | `theme.IconCircleSmall + " "` (focused) or `"  "` (not focused) |
| Label | field text, e.g. `"Visibility"` |
| Selection icon | `" " + theme.IconSelect + " "` |
| Separator | `theme.IconChevronRight + " "` |
| Current value | plain text with `ColorText` |

#### Navigation

- `←` / `→`: move to the previous / next value (cycling)
- `Tab` / `Shift+Tab`: navigate to the previous / next field

Do not use `Enter` to cycle — `Enter` confirms the form or advances to the next field.

#### Reference implementation

```go
// Rendu
func (f *Form) renderCycleField(label string, options []string, idx int, fieldIdx int) string {
    selectLabel := label + " " + theme.IconSelect + " "
    value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(options[idx])
    if f.focusedField == fieldIdx {
        return theme.KeyStyle.Render(theme.IconCircleSmall+" "+selectLabel+theme.IconChevronRight+" ") + value
    }
    return theme.Bg("  "+selectLabel+theme.IconChevronRight+" ") + value
}

// Key handling
case "left":
    if f.focusedField == fieldIdx {
        f.idx = (f.idx - 1 + len(f.options)) % len(f.options)
    }
case "right":
    if f.focusedField == fieldIdx {
        f.idx = (f.idx + 1) % len(f.options)
    }
```

Forbidden:
- ❌ Using `Enter` to cycle values
- ❌ Opening a dropdown list for 2–4 values (reserve that for long lists, cf. `renderTemplateList`)
- ❌ Hardcoding the value's color (use `ColorText`)
- ❌ Using radio buttons for a closed set (Rule 120)

### Rule 121 : Circular iconography for statuses

Status indicators in tables and lists must use circular icons:

| State | Icon |
|------|-------|
| Success / OK | `theme.IconOK` (`\uf058`) |
| Error / Failure | `theme.IconError` (`\uf057`) |
| Warning | `theme.IconWarning` (`\uf071`) |

Applies to: scan results, pipeline statuses, UP/DOWN indicators.
Does not apply to: object-type icons (folders, files, GitLab logo).

### Rule 133 : Multi-line text fields — `WrappedInput` mandatory

**`bubbles/textarea` is forbidden.** Its internal background is not controllable by lipgloss and produces visual artifacts (a `┃` gutter, terminal background between lines).

Use **`components.WrappedInput`** (`internal/ui/components/wrapped_input.go`) for any text field that must display across multiple lines.

#### API

```go
// Creation
field := components.NewWrappedInput(wrapWidth, charLimit, "placeholder text")

// Resizing — call inside the tea.WindowSizeMsg handler
field.SetDisplayWidth(terminalWidth)

// Focus / Blur
field.Focus()   // called from updateFocus()
field.Blur()    // called from updateFocus()

// Dans Update() — passer les messages clavier quand le champ a le focus
field, cmd = field.Update(msg)

// In View() — returns the multi-line content block, ready to concatenate
return labelStr + "\n" + field.View()

// Retrieving the value on submit
value := field.Value()
```

#### `wrapWidth` parameter

`wrapWidth` is the number of **runes** per visual line before wrapping (word-wrap at the last space, hard-break if there is no space).

| Context | Recommended value |
|----------|--------------------|
| GitLab description (group/project) | `80` |
| Short notes field | `60` |
| Long text field | `100` |

```go
// ✅ CORRECT
const descWrapWidth = 80
descInput := components.NewWrappedInput(descWrapWidth, 250, "description (optional)")

// ❌ WRONG
descInput := textarea.New()
```

#### Behavior

- Empty field + focused → only the block cursor is shown
- Empty field + not focused → placeholder in `ColorDim`
- Filled field → word-wrapped text, each line padded with `theme.PadWithBg` up to `displayWidth`
- Inverted block cursor (`ColorText` bg / `ColorBackground` fg) at the exact caret position

Forbidden:
- ❌ `bubbles/textarea` in any view
- ❌ Recreating the word-wrap or cursor logic locally
- ❌ Forgetting to call `SetDisplayWidth` in the `tea.WindowSizeMsg` handler

### Rule 135 : Strict keybinding assignment in forms ⚠️

**Every key has a single, unambiguous role.** These rules apply to every form in the application without exception.

| Key | Exclusive role |
|--------|---------------|
| `↑ / ↓` | Navigate between a form's fields |
| `Tab / Shift+Tab` | Switch between tabs **only** — never for navigation between fields |
| `Space` | Toggle a checkbox — the **only** key allowed for this role |
| `Enter` | Validate an action button or trigger a specific action — **never** to toggle a checkbox |
| `← / →` | Change level in a table (drill-down/up) **and** cycle the values of a closed-list field |
| `Esc` | Cancel / close / return to the parent level |

#### Detailed rules

**`Tab / Shift+Tab` — tabs only**
- Reserved for switching between tabs (`bubbles/table` breadcrumb, result tabs, etc.)
- Must **never** be used to move forward/backward between form fields
- If a view has no tabs, `Tab` must do nothing (or be ignored)

**`↑ / ↓` — navigation between fields**
- The only keys to move focus from one field to another in a form
- In a list/table outside a form: row-by-row movement (Rule 111)

**`Space` — toggle checkbox**
- The only valid key to check/uncheck a checkbox
- `Enter` on a checkbox must be ignored or advance to the next field, never toggle it

**`Enter` — validation / action**
- Validates the currently focused button
- Triggers a contextual action (e.g. launch a scan, open a detail view)
- Must **not** toggle a checkbox

**`← / →` — table navigation and value cycling**
- In a table: `←` moves up a level (drill up), `→` moves down a level (drill down)
- On a closed-list field (cycle field): `←` previous value, `→` next value (Rule 132)
- Must **not** move focus between form fields

#### Common violations to avoid

```go
// ❌ WRONG — Tab to navigate between fields
case "tab":
    m.focusedField = (m.focusedField + 1) % fieldCount

// ❌ WRONG — Enter to toggle a checkbox
case "enter":
    if m.focusedField == fieldMyCheckbox {
        m.myCheckbox = !m.myCheckbox
    }

// ✅ CORRECT — Space to toggle
case " ":
    if m.focusedField == fieldMyCheckbox {
        m.myCheckbox = !m.myCheckbox
    }

// ✅ CORRECT — ↑/↓ to navigate between fields
case "up":
    m.focusedField = max(m.focusedField-1, 0)
case "down":
    m.focusedField = min(m.focusedField+1, fieldCount-1)
```
