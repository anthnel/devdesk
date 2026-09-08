# TUI — Tables

### Rule 106 : Table standardization

- Use `theme.DefaultTableStyles()` for all tables
- Bold headers with ColorSecondary (see Rule 118)
- Selected row with ColorHighlight
- Consistent borders

### Rule 116 : the rendered row occupies exactly the inside of the viewport

**The selected row must extend all the way to the viewport's right border.**

The calculation belongs to `internal/ui/datatable`, and to it alone. A view
passes the **full viewport width, borders included** — `Resize` strips out
the borders and the padding `bubbles/table` adds per cell (`Padding(0, 1)`,
i.e. +2 per rendered column).

```go
func (m *Model) resize(width, height int) {
    m.table.Resize(width, max(height-1, 1)) // no -2, no max(…, 20)
}
```

Subtracting the borders a second time is the rule's most discreet defect:
the table is then correct at *every* width and also ends two cells too soon
at every one of them. A local floor (`max(…, 20)`) is the mirror-image
mistake — it makes the table wider than the viewport when the terminal is
narrow, which is exactly the forbidden overflow.

#### The invariant is stated over the **rendered** extent

```go
// ✅ CORRECT
if got, want := m.table.RenderedWidth(), width-2; got != want { … }

// ❌ WRONG — wrong as soon as a column is dropped
total := 0
for _, col := range m.table.Table().Columns() { total += col.Width }
if want := width - 2 - len(cols)*2; total != want { … }
```

A column dropped for lack of room renders nothing — no header, no cell, no
padding — and its two cells go back into the budget. The sum of the declared
columns plus two each therefore asks for less than what the row occupies,
and fails on an otherwise correct layout. This is D61 seen from the other
side.

#### Every column declares its nature (§3.45)

`Sizing` has **no** default value: `SizingFixed` (exact width) or
`SizingContent` (content width, floored at `MinWidth`, capped at
`MaxWidth`). `TestEveryColumnDeclaresItsSizing` scans the sources and fails,
naming the file, line, and column.

`MinWidth` is a **floor**, not a request. What gives way beyond that is an
**entire** column: `Optional` ones first, rightmost first, then the others
if needed. Fewer correct columns beat all of them being wrong — truncating a
count column would render `142` as `14…`, and nothing on screen tells the
two apart.

Forbidden:
- ❌ Computing column widths inside a view
- ❌ Subtracting the borders before `Resize`, or clamping the width passed in
- ❌ Checking Rule 116 by summing the declared columns
- ❌ Leaving a column without a `Sizing`
- ❌ `Background()` on `s.Cell` (masks `s.Selected.Background()`)

### Rule 122 : `Cell` measures, `Style` colors — never the reverse ⚠️

**`Cell` returns plain text, with no ANSI sequence. Color goes through
`Style`, and through nothing else.**

**Why.** A cell is measured and truncated *before* it is dressed up, and the
measurement goes through `runewidth`, which counts an escape sequence's
bytes as width. A string of 7 visible cells carrying a color measures 28: it
therefore gets truncated in a column that is twice wide enough, and the cut
falls *inside* the escape sequence — `"\x1b[38;2;166;2…"`. The unterminated
sequence then bleeds onto every following row.

This is a limitation of `bubbles/table`, not of Bubble Tea or lipgloss, and
it is unchanged in `bubbles v1.0.0`. `internal/ui/datatable` therefore
renders its own rows (`render.go`): the text is measured while still plain,
color is applied afterward. **The prohibition therefore targets `Cell`, not
color.**

```go
// ✅ CORRECT — the text is measurable, the color is decided separately
{
    Title: "Severity", MinWidth: 10,
    Cell:  func(f scan.Finding) string { return string(f.Severity) },
    Style: func(f scan.Finding) lipgloss.Style {
        return theme.SeverityTextStyle(string(f.Severity))
    },
}

// ❌ WRONG — color becomes part of what gets measured
{
    Cell: func(f scan.Finding) string {
        return theme.SeverityTextStyle(string(f.Severity)).Render(string(f.Severity))
    },
}
```

#### Selected row

**`Style` is not consulted for the row under the cursor.** It is handed
whole to `styles.Selected`, and a color inside it closes with a reset that
carries away the selection background for the rest of the row: the
highlight would stop halfway through. Highlighting answers "where am I,"
and no column color is worth losing that. A column cannot ask for the
reverse.

#### Background

lipgloss does not inherit a background (Rule 115), and the viewport's style
only covers cells that emit nothing. `render.go` therefore gives an explicit
background to **every** cell of an unselected row, colored or not —
otherwise a single colored cell would strip the background off everything
that follows it. A column that declares only a `Foreground` automatically
receives `ColorBackground`.

#### Color discipline

A color that appears everywhere tells you nothing:

- a counter at `0`, a `-`, a missing value → `theme.DimStyle`;
- the nominal, majority state (a `running` container) → the default text
  color, **not** green;
- color is reserved for what deserves to be spotted without reading.

**The exception, and what defines it: a column where the absence of color
is already taken.** The `CI` column has three absences — never scanned, not
gradable, score withheld — and all three render in `DimStyle`. An `A` in
ordinary text color is distinguishable from a grey `-` only by a shade,
across four cells. Green there separates **a grade from an absence**, not
two nominal values from each other: that is exactly what the rule above
forbids elsewhere, and exactly what it requires here. That is the
criterion, and not "it's important" — if a column's absences were already
distinguishable, green would go back to being noise.

**The second declared exception: a framed gauge (§3.71).** The `containers`
load bars are green below 75%, orange, then red. The same criterion applies for
a different reason — inside a frame, a bar left in the ordinary text color has
the color of the number, the name and the image beside it, so the *fill* stops
being distinguishable from the *frame*. The green is not saying "this container
is fine"; it is saying where the ink is, which is the one thing a bar exists to
say. Accepted cost, stated rather than discovered: most containers idle near
zero, so most rows carry a sliver of green — a sliver inside a frame reads as a
level, where a whole green cell would read as a status.

Checklist:
- [ ] No `style.Render(...)` inside what `Cell` returns
- [ ] Status icons as plain text: `theme.IconError + " error"`
- [ ] Spinners as plain text: `frame + " scanning"`
- [ ] Per-cell color via `Style`, never via `Cell`
- [ ] Selected-row color via `SelectedStyles` / `TableStylesForState/Severity()`
- [ ] Zeros and placeholders are `DimStyle`, not colored

### Rule 125 : Icon column alignment

**Any icon column (or icon + short text) must be left-aligned.**

Nerd Font icons have variable width depending on the terminal. Left alignment is the only one that guarantees consistent rendering.

| Content type | Alignment |
|-----------------|------------|
| Icon alone | **Left** |
| Icon + short text | **Left** |
| Alphanumeric text | Left (default) |

#### An icon in the first column **is** a column

When a `datatable`'s first column carries a glyph, it is a column in its own
right — not a prefix stuck onto the neighboring text cell.

| | |
|---|---|
| Title | **empty**. The glyph reads in a glance; a header would be naming something that does not need naming, and `eza` does not put one there either. |
| Width | `datatable.IconColumnWidth` (2): the glyph, plus one cell so it does not touch the text. Two, not one — a Nerd Font glyph renders double-width on some terminals, and a single cell would truncate it there. |
| `Sizing` | `SizingFixed`. Nothing to measure. |
| `Less` / `Search` | **none**. It adds no text anyone could type, so filtering stays on the name columns; and a comparator would cost two more cells to fit its sort arrow. |

```go
// ✅ CORRECT — the glyph has its own column
{
    Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
    Cell: func(r row) string { return r.icon() },
},
{
    Title: "Name", Sizing: datatable.SizingContent, MinWidth: 16,
    Cell:   func(r row) string { return r.Name },
    Search: func(r row) string { return r.Name },
},

// ❌ WRONG — the glyph stuck into the identifying column
{
    Title: "Name", Sizing: datatable.SizingContent, MinWidth: 18,
    Cell: func(r row) string { return theme.IconDocker + " " + r.Name },
},
```

**What sticking it in costs**, and this is what settles it: the identifying
column spends its width on something that is not the name, and a
`SizingContent` then measures the glyph along with it — so the table's most
contested column reserves two cells for an icon, at every width.

#### An icon's color goes through a **role**, never through the glyph

Rule 122 applies here as everywhere: `Cell` renders the bare glyph, `Style`
colors it. What Rule 125 adds is **where the color comes from** —
`theme.IconStyle(role)`, and a role declared in `theme/iconcolors.go`.

```go
// ✅ CORRECT — the view names a meaning, the theme answers with a color
Cell:  func(r row) string { return nodeKindIcon(r.node) },
Style: func(r row) lipgloss.Style { return theme.IconStyle(nodeKindRole(r.node)) },

// ❌ WRONG — the view decides the hue
Style: func(r row) lipgloss.Style {
    return lipgloss.NewStyle().Foreground(theme.ColorSecondary)
},
```

**A codepoint is not a name.** A table of `U+F0849 → ColorSecondary` cannot
be reviewed: nothing on the line says whether the entry is correct, so an
error there is indistinguishable from a deliberate choice. A role can be
discussed, which is why it is the key.

The five roles are overridable via a theme file (`icon_namespace`,
`icon_repository`, `icon_vis_*`) — unlike syntax colors, which are closed
aliases: an icon is the first thing seen on a row, so it is the part of the
palette a user is most likely to have an opinion about.

**All three icon tables declare a `Style`** — `ws`, `:sec`, and the
explorer — and a fourth that did not declare one would be the application's
only monochrome icon column. A missing role shows: the cell falls back to
`ColorText` (see `IconColor`), so an oversight renders ordinary text rather
than nothing.

**The same object keeps its color from one view to another.** A git repo is
`IconRoleRepository` in `ws`, in `:sec`, and in the explorer — the role is
shared, not duplicated. This is the property for which the key is a meaning
rather than a glyph: the three views do not even show the same glyph for it.

**The granularity is that of the action, not that of the type.** `ws`
colors in three classes — repo, directory, file — which are exactly the
three branches of `availability.go`, and **not** a shade per language the
way `eza` does: `.go` versus `.rs` changes no shortcut on the row, so the
color would say nothing. An icon column is colored by what the row lets you
do.

**The color disappears under the cursor**, like that of any colored column:
the selected row is rendered whole by `styles.Selected` (Rule 122). An icon
column therefore cannot be the *only* carrier of a piece of information —
which is why the explorer's kind stays readable another way (the glyph
outside selection, the position in the tree inside it).

The three tables concerned are `ws`, `containers`, and the `:sec` inventory,
and they declare the same constant. `TestAnIconColumnIsUntitledAndTwoCellsWide`
(`internal/ui/datatable`) scans the sources and rejects a titleless column
that invents its own width. It does not see the other half — a glyph stuck
into a text cell is indistinguishable from a name that starts with a
glyph — which stays a matter for review.

Forbidden:
- ❌ `.Align(lipgloss.Center)` on an icon column
- ❌ `.Align(lipgloss.Right)` on an icon column
- ❌ A title above a glyph column
- ❌ A local width (`colIconFixed`, `statusColumnWidth`, a literal `2`) instead of `datatable.IconColumnWidth`
- ❌ A glyph prefixed inside a text column's cell
- ❌ An icon color chosen in the view instead of a `theme.IconStyle` role
- ❌ A color table indexed by the glyph rather than by meaning
- ❌ An icon column with no `Style`, while the other three have one
- ❌ A role per file type where the row offers the same actions

### Rule 139 : A `datatable`'s body is never replaced — not by a spinner, not by a message

**A `datatable` stays a table, whatever it contains.** Whether it is
loading its data, empty because nothing has run yet, or empty because a
filter no longer matches anything, the rendered body is always
`m.table.View()` — header included, no rows. `datatable.View()` already
does this on its own: a zero-element table renders its header and fills
the rest with the background (`internal/ui/datatable/render.go`). Nothing
in a view therefore needs to detect this case.

What a body would have said instead — loading, the row count, a verdict —
is **the footer's and header's business**, never the body's:

- loading is a `components.Status{Text: "...", Spinner: true}` in the
  footer (Rule 128);
- the row count, or whatever stands in for it (a verdict, a total), is a
  field of `GetHeaderInfo` (`shortcut.HeaderInfo{Key: "Images", Value: "0"}`) —
  it, not the body, answers "what am I looking at."

**Why.** A body that substitutes itself for the table — with a spinner or
with text — loses its header and its columns for as long as the condition
holds, then regains them: the layout jumps on every refresh, every
`ctrl+r`, every keystroke in the filter. The header and the footer, on the
other hand, have a fixed height (Rule 124): what they display changes
without ever moving the table.

```go
// ✅ CORRECT — the body is always the table; the footer and header speak
func (m Model) renderImagesView() string {
    return m.imageTable.View()
}

func (m Model) status() sharedcomponents.Status {
    if text, ok := m.loadingLabel(); ok {
        return sharedcomponents.Status{Text: text, Spinner: true}
    }
    return sharedcomponents.Status{Text: m.actionLine()}
}

func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
    return []shortcut.HeaderInfo{
        {Key: "Images", Value: fmt.Sprintf("%d", len(m.imageTable.Visible())), Style: theme.HeaderValueStyle},
    }
}

// ❌ WRONG — the body replaces itself with a spinner
if m.loading && len(m.images) == 0 {
    return theme.SpinnerMessage(m.spinner.View(), "Loading images...")
}

// ❌ WRONG — the body replaces itself with a message, empty or filtered
if len(m.imageTable.Visible()) == 0 {
    return theme.DimStyle.Render("No images found")
}
```

The spinner's frame is pushed from the `spinner.TickMsg` handler:
`m.footer.SetSpinnerFrame(m.spinner.View())`. Without this call the spinner
stays on frame zero, which reads as a hang.

`TestNoTableViewRendersALoadingBody` (`internal/ui/components`) rejects a
`theme.SpinnerMessage` in table views. An operation screen with no table
behind it (a `docker pull` in progress) is an exception **declared in the
test**.

**Declared exception: a welcome screen, not a status message.** The empty
`:sec` inventory (`internal/ui/security/inventory.go`, `renderEmptyInventory`)
replaces the body with a paragraph that explains *where scans come from* —
this is not "0 rows," it is first-use onboarding, with more content than a
header field can carry. The distinction: a message that fits in
`GetHeaderInfo` (a count, a status) goes there; text that explains an
entire application flow stays an explicit exception, like the one already
provided for loading.

### Rule 136 : Filter Bar for Tables

**Every table view must use `components.FilterBar` for search and toggle filters. No ad-hoc filter implementations.**

#### Behaviour

| State | Bar visible | Description |
|-------|-------------|-------------|
| No filter | ❌ | Bar is completely hidden |
| Toggle filter active | ✅ | Bar shows dimmed placeholder + active token(s) |
| Search mode (after `/`) | ✅ | Bar shows focused text input + active token(s) |
| Search confirmed (query set) | ✅ | Bar shows dimmed placeholder + query token + toggle tokens |

#### Visual

The filter bar forms a **closed rectangle** that visually connects with the viewport's bottom border:

```
┌─────────────────────────────────────────┐   ← viewport top border
│  table rows...                          │
└─────────────────────────────────────────┘   ← viewport bottom border (= rectangle top)
│  / cursor...         [tcp]  ·  [LISTEN] │   ← content line with │ sides
└─────────────────────────────────────────┘   ← rectangle bottom border
```

Both lines use `theme.ColorViewportBorder` for `│`, `└`, `┘`, `─` characters.

#### Placement — Footer, not viewport

The filter bar is rendered in `RenderFooter()`, **not** in the viewport content.

- `GetFooterHeight()` returns `baseFooterHeight + filterBar.ExtraHeight()`
- `RenderFooter()` prepends `filterBar.View()` when `filterBar.IsVisible()`
- Table height formula does **not** subtract `filterBar.ExtraHeight()` — the app router handles that via `GetFooterHeight()`

#### Integration Pattern

```go
// In Model struct
filterBar components.FilterBar  // or components.NewFilterBarWithTokens(tokens)

// In GetFooterHeight()
func (m Model) GetFooterHeight() int {
    return 2 + m.filterBar.ExtraHeight() // base (empty line + info) + filter bar
}

// In RenderFooter()
func (m Model) RenderFooter(width int) string {
    var parts []string
    if m.filterBar.IsVisible() {
        parts = append(parts, m.filterBar.View())
    }
    parts = append(parts, theme.EmptyLineBg(width), infoLine)
    return strings.Join(parts, "\n")
}

// In resize() / WindowSizeMsg handler — NO ExtraHeight subtraction
tableHeight := m.height - overhead  // overhead = header row only, NOT filterBar.ExtraHeight()
m.table.SetHeight(tableHeight)

// In Update() / key handler — forward keys when in search mode
if m.filterBar.InEditMode() {
    m.filterBar, cmd = m.filterBar.Update(msg)
    m.applyFilters()
    return m, cmd
}
// "/" key activates search
case "/":
    return m, m.filterBar.ActivateSearch()

// In InEditMode() — propagate to app router
func (m Model) InEditMode() bool {
    return m.filterBar.InEditMode() || /* other edit states */
}

// In View() / renderNormalView() — table only, NO filter bar here
func (m Model) renderNormalView() string {
    return m.table.View()
}

// In applyFilters() — use filterBar.SearchQuery() for text filtering
query := strings.ToLower(m.filterBar.SearchQuery())
```

#### Toggle filter tokens (views with dedicated filter keys)

Use `components.NewFilterBarWithTokens()` and update token active state when toggle keys are pressed:

```go
// Init
fb := components.NewFilterBarWithTokens([]components.FilterToken{
    {Label: "tcp"},
    {Label: "udp"},
})

// On key "t"
fb.SetTokenActive("tcp", !fb.IsTokenActive("tcp"))
```

#### Rules

- ✅ Use `components.FilterBar` — never re-implement the filter bar locally
- ✅ The frame itself is `components.BarFrame(width, inner)`: that is what
  `FilterBar.View()` calls, and the only path for whatever shares this slot
  without being a filter — the viewer's go-to-line prompt (§3.53). Two
  implementations of the rectangle would be free to diverge on where the
  corners sit.
- ✅ Only one occupant of the slot at a time, and the height does not depend
  on which one — otherwise the panel resizes under the reader whenever one
  opens on top of the other.
- ✅ `filterBar.ExtraHeight()` must be added to `GetFooterHeight()` return value
- ✅ `filterBar.View()` must be prepended in `RenderFooter()` when `filterBar.IsVisible()`
- ✅ `filterBar.InEditMode()` must propagate via the view's `InEditMode()` method
- ✅ `"/"` key must always call `filterBar.ActivateSearch()`
- ❌ Filter bar must NOT be rendered inside the viewport (`View()` / `renderNormalView()`)
- ❌ `filterBar.ExtraHeight()` must NOT be subtracted from table height (app router handles it)
- ❌ `filterInput textinput.Model` fields in view models (use FilterBar instead)
